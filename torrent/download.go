package torrent

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"time"
)

// 整个种子任务
type TorrentTask struct {
	PeerId   [IDLEN]byte
	PeerList []PeerInfo
	InfoSHA  [SHALEN]byte
	FileName string
	FileLen  int
	PieceLen int
	PieceSHA [][SHALEN]byte
}

// 每一片的任务
type pieceTask struct {
	index  int
	sha    [SHALEN]byte
	length int
}

type taskState struct {
	index      int
	conn       *PeerConn
	requested  int
	downloaded int
	backlog    int
	data       []byte
}

type pieceResult struct {
	index int
	data  []byte
}

const BLOCKSIZE = 16384
const MAXBACKLOG = 5

func (state *taskState) handleMsg() error {
	msg, err := state.conn.ReadMsg()
	if err != nil {
		return err
	}

	// handle keep-alive
	if msg == nil {
		return nil
	}

	switch msg.Id {
	case MsgChoke:
		state.conn.Chocked = true
	case MsgUnchoke:
		state.conn.Chocked = false
	case MsgHave:
		index, err := GetHaveIndex(msg)
		if err != nil {
			return err
		}
		state.conn.Field.SetPiece(index)
	case MsgPiece:
		n, err := CopyPieceData(state.index, state.data, msg)
		if err != nil {
			return err
		}
		state.downloaded += n
		state.backlog--
	}
	return nil
}

func downloadPiece(conn *PeerConn, task *pieceTask) (*pieceResult, error) {
	state := &taskState{
		index: task.index,
		conn:  conn,
		data:  make([]byte, task.length),
	}
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer conn.SetDeadline(time.Time{})

	for state.downloaded < task.length {
		// 如果Chocked为false，表示愿意上传数据
		if !conn.Chocked {
			for state.backlog < MAXBACKLOG && state.requested < task.length {
				length := BLOCKSIZE
				// 对最后一段，可能长度会短一些，做一下特殊处理
				if task.length-state.requested < length {
					length = task.length - state.requested
				}
				msg := NewRequestMsg(state.index, state.requested, length)
				_, err := state.conn.WriteMsg(msg)
				if err != nil {
					return nil, err
				}
				state.backlog++
				state.requested += length
			}
		}
		err := state.handleMsg()
		if err != nil {
			return nil, err
		}
	}

	return &pieceResult{state.index, state.data}, nil
}

func checkPiece(task *pieceTask, res *pieceResult) bool {
	sha := sha1.Sum(res.data)
	if !bytes.Equal(task.sha[:], sha[:]) {
		fmt.Printf("check integrity failed, index: %v\n", res.index)
		return false
	}
	return true
}

func (t *TorrentTask) peerRoutine(peer PeerInfo, taskQueue chan *pieceTask, resultQueue chan *pieceResult) {
	// set up conn with peer
	conn, err := NewConn(peer, t.InfoSHA, t.PeerId)
	if err != nil {
		fmt.Println("fail to connect peer: " + peer.Ip.String())
		return
	}
	defer conn.Close()

	fmt.Println("complete handshake with peer: " + peer.Ip.String())
	// 开始给对方发请求
	conn.WriteMsg(&PeerMsg{MsgInterested, nil})
	// 得到所有的piece任务，并且开始下载
	for task := range taskQueue {
		// 没有这一片，放回taskQueue，准备从其他peer处下载
		if !conn.Field.HasPiece(task.index) {
			taskQueue <- task
			continue
		}
		fmt.Printf("get task, index: %v, peer: %v\n", task.index, peer.Ip.String())
		res, err := downloadPiece(conn, task)
		if err != nil {
			// 出现错误，放回taskQueue，从其他peer处再下载
			taskQueue <- task
			fmt.Println("fail to download piece " + err.Error())
			return
		}
		if !checkPiece(task, res) {
			// 下下来校验不对，放回taskQueue，从其他peer处再下载
			// 总之出现任何问题都放回taskQueue
			taskQueue <- task
			continue
		}
		// 下载成功，放入result，等待后续组装
		resultQueue <- res
	}
}

func (t *TorrentTask) getPieceBounds(index int) (begin, end int) {
	begin = index * t.PieceLen
	end = begin + t.PieceLen
	if end > t.FileLen {
		// 对最后的一片做特殊处理，因为其长度可能不足一个piece了
		end = t.FileLen
	}
	return
}

func Download(task *TorrentTask) error {
	fmt.Println("start downloading " + task.FileName)

	// 划分piece任务并初始化task，result channel
	// task数量与SHA的数量相同
	taskQueue := make(chan *pieceTask, len(task.PieceSHA))
	resultQueue := make(chan *pieceResult)

	// 创建所有Task
	for index, sha := range task.PieceSHA {
		begin, end := task.getPieceBounds(index)
		taskQueue <- &pieceTask{index, sha, (end - begin)}
	}

	// 对每一个peer都起一个TorrentTask，下载整个任务中需要的部分
	for _, peer := range task.PeerList {
		go task.peerRoutine(peer, taskQueue, resultQueue)
	}

	// 收集结果
	buf := make([]byte, task.FileLen)
	count := 0
	for count < len(task.PieceSHA) {
		res := <-resultQueue
		begin, end := task.getPieceBounds(res.index)
		copy(buf[begin:end], res.data)
		count++

		// 打印任务进度
		percent := float64(count) / float64(len(task.PieceSHA)) * 100
		fmt.Printf("downloading, progress: (%0.2f%%)\n", percent)
	}
	close(taskQueue)
	close(resultQueue)

	// 创建文件并复制buf中的数据
	file, err := os.Create(task.FileName)
	if err != nil {
		fmt.Println("fail to create file: " + task.FileName)
		return err
	}

	_, err = file.Write(buf)
	if err != nil {
		fmt.Println("fail to write data")
		return err
	}

	return nil
}
