package torrent

import (
	"fmt"
	"io"
)

const (
	Reserved int = 8                         // 保留位，为以后扩充协议准备
	HsMsgLen int = SHALEN + IDLEN + Reserved // 包括InfoSHA的长度和PeerId的长度，用于标识文件信息和下载器信息
)

type HandshakeMsg struct {
	PreStr  string
	InfoSHA [SHALEN]byte
	PeerId  [IDLEN]byte
}

func NewHandShakeMsg(infoSHA [SHALEN]byte, peerId [IDLEN]byte) *HandshakeMsg {
	return &HandshakeMsg{
		PreStr:  "BitTorrent protocol",
		InfoSHA: infoSHA,
		PeerId:  peerId,
	}
}

func WriteHandShake(w io.Writer, msg *HandshakeMsg) (int, error) {
	buf := make([]byte, len(msg.PreStr)+HsMsgLen+1) // 最开始有一个byte用来描述PreStr的长度
	buf[0] = byte(len(msg.PreStr))

	cur := 1
	// 不断将信息加入buf slice的尾部
	cur += copy(buf[cur:], []byte(msg.PreStr)) // 这里做了一个类型转换，将PreStr转为byte slice
	cur += copy(buf[cur:], make([]byte, Reserved))
	cur += copy(buf[cur:], msg.InfoSHA[:])
	cur += copy(buf[cur:], msg.PeerId[:])

	return w.Write(buf)
}

func ReadHandshake(r io.Reader) (*HandshakeMsg, error) {
	lenBuf := make([]byte, 1)
	_, err := io.ReadFull(r, lenBuf)
	if err != nil {
		return nil, err
	}

	prelen := int(lenBuf[0])
	if prelen == 0 {
		err := fmt.Errorf("prelen cannot be 0")
		return nil, err
	}

	msgBuf := make([]byte, HsMsgLen+prelen)
	_, err = io.ReadFull(r, msgBuf)
	if err != nil {
		return nil, err
	}

	var peerId [IDLEN]byte
	var infoSHA [SHALEN]byte

	copy(infoSHA[:], msgBuf[prelen+Reserved:prelen+Reserved+SHALEN])
	copy(peerId[:], msgBuf[prelen+Reserved+SHALEN:])

	return &HandshakeMsg{
		PreStr:  string(msgBuf[0:prelen]),
		InfoSHA: infoSHA,
		PeerId:  peerId,
	}, nil
}
