package torrent

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

type MsgId uint8

const (
	MsgChoke         MsgId = 0
	MsgUnchoke       MsgId = 1
	MsgInterested    MsgId = 2
	MsgNotInterested MsgId = 3
	MsgHave          MsgId = 4
	MsgBitfield      MsgId = 5
	MsgRequest       MsgId = 6
	MsgPiece         MsgId = 7
	MsgCancel        MsgId = 8
)

type PeerMsg struct {
	Id      MsgId
	Payload []byte
}

type PeerConn struct {
	net.Conn // 这里直接嵌入net.Conn，PeerConn能够直接使用其方法，有点继承的感觉
	Chocked  bool
	Field    Bitfield
	peer     PeerInfo
	peerId   [IDLEN]byte
	infoSHA  [SHALEN]byte
}

// 与peer建立连接的过程
func handshake(conn net.Conn, infoSHA [SHALEN]byte, peerId [IDLEN]byte) error {
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	defer conn.SetDeadline(time.Time{})

	// send HandshakeMsg
	req := NewHandShakeMsg(infoSHA, peerId)
	_, err := WriteHandShake(conn, req)
	if err != nil {
		fmt.Println("send handshake failed")
		return err
	}

	// read HandshakeMsg
	res, err := ReadHandshake(conn)
	if err != nil {
		fmt.Println("read handshake failed")
		return err
	}

	// check HandshakeMsg
	// 检查对方有的文件的类型是否和要下载的相同
	if !bytes.Equal(res.InfoSHA[:], infoSHA[:]) {
		fmt.Println("check handshake failed")
		return fmt.Errorf("handshake msg error: " + string(res.InfoSHA[:]))
	}
	return nil
}

// 从c发回的消息中获取bitfiled，即当前peer有哪些piece，每个piece用一个bit标识
func fillBitfield(c *PeerConn) error {
	c.SetDeadline(time.Now().Add(5 * time.Second))
	defer c.SetDeadline(time.Time{})

	msg, err := c.ReadMsg()
	if err != nil {
		return err
	}

	if msg == nil {
		return fmt.Errorf("expected bitfield")
	}

	if msg.Id != MsgBitfield {
		return fmt.Errorf("expected bitfield, get" + strconv.Itoa(int(msg.Id)))
	}

	fmt.Println("fill bitfield: " + c.peer.Ip.String())

	// 设置当前连接peer的bitfield
	c.Field = msg.Payload
	return nil
}

func (c *PeerConn) ReadMsg() (*PeerMsg, error) {
	// read msg length
	lenBuf := make([]byte, 4)
	_, err := io.ReadFull(c, lenBuf)
	if err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lenBuf)

	// keep alive msg
	if length == 0 {
		return nil, nil
	}

	// read msg body
	msgBuf := make([]byte, length)
	_, err = io.ReadFull(c, msgBuf)
	if err != nil {
		return nil, err
	}

	return &PeerMsg{
		Id:      MsgId(msgBuf[0]),
		Payload: msgBuf[1:],
	}, nil
}

const LenBytes uint32 = 4

func (c *PeerConn) WriteMsg(m *PeerMsg) (int, error) {
	var buf []byte
	if m == nil {
		buf = make([]byte, LenBytes)
	}
	length := uint32(len(m.Payload) + 1) // +1 for id
	buf = make([]byte, LenBytes+length)
	binary.BigEndian.PutUint32(buf[0:LenBytes], length)
	buf[LenBytes] = byte(m.Id)
	copy(buf[LenBytes+1:], m.Payload)

	return c.Write(buf)
}

func CopyPieceData(index int, buf []byte, msg *PeerMsg) (int, error) {
	if msg.Id != MsgPiece {
		return 0, fmt.Errorf("expected MsgPiece (Id %d), got Id %d", MsgPiece, msg.Id)
	}

	if len(msg.Payload) < 0 {
		return 0, fmt.Errorf("payload too short. %d < 8", len(msg.Payload))
	}

	parsedIdnex := int(binary.BigEndian.Uint32(msg.Payload[0:4]))
	if parsedIdnex != index {
		return 0, fmt.Errorf("expected index %d, got %d", index, parsedIdnex)
	}

	offset := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
	if offset > len(buf) {
		return 0, fmt.Errorf("offset too high. %d >= %d", offset, len(buf))
	}

	data := msg.Payload[8:]
	if offset+len(data) > len(buf) {
		return 0, fmt.Errorf("data too large [%d] for offset %d with lenth %d", len(data), offset, len(buf))
	}

	copy(buf[offset:], data)
	return len(data), nil
}

func GetHaveIndex(msg *PeerMsg) (int, error) {
	if msg.Id != MsgHave {
		return 0, fmt.Errorf("expected MsgHave (Id %d), got Id %d", MsgHave, msg.Id)
	}

	if len(msg.Payload) != 4 {
		return 0, fmt.Errorf("expected payload length 4, got length %d", len(msg.Payload))
	}

	index := int(binary.BigEndian.Uint32(msg.Payload))
	return index, nil
}

func NewRequestMsg(index, offset, length int) *PeerMsg {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(offset))
	binary.BigEndian.PutUint32(payload[8:12], uint32(length))

	return &PeerMsg{MsgRequest, payload}
}

// PeerInfo为想要通信的peer的信息
// infoSHA表示要下载文件的信息，相当于文件的唯一标识
// peerId表示下载器客户端表示，这里用的是随机生成的
func NewConn(peer PeerInfo, infoSHA [SHALEN]byte, peerId [IDLEN]byte) (*PeerConn, error) {
	// setup tcp conn
	addr := net.JoinHostPort(peer.Ip.String(), strconv.Itoa(int(peer.Port)))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		fmt.Println("set tcp conn failed: " + addr)
		return nil, err
	}

	// torrent p2p handshake
	// 经过handshake，conn中已经是建立好并通过握手的连接了
	err = handshake(conn, infoSHA, peerId)
	if err != nil {
		fmt.Println("handshake failed")
		conn.Close()
		return nil, err
	}

	c := &PeerConn{
		Conn:    conn, // 将c中的Conn设置为已经建立连接的conn
		Chocked: true,
		peer:    peer,
		peerId:  peerId,
		infoSHA: infoSHA,
	}

	// fill bitfield
	err = fillBitfield(c)
	if err != nil {
		fmt.Println("fill bitfield failed, " + err.Error())
		return nil, err
	}
	return c, nil
}
