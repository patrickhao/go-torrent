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

// 用来描述下载中间过程的结构体，是针对一个piece来说
// 因为一个piece也挺长的
type taskState struct {
	index      int
	conn       *PeerConn
	requested  int // 表示请求了多少个byte
	downloaded int // 表示已经下载了多少个byte，结合长度可以算出还有多个byte在路上
	backlog    int // 并发度
	data       []byte
}

type pieceResult struct {
	index int
	data  []byte // 传过来当前piece的数据
}

// 将一个piece分块下载，这里是每一块的最大byte长度
const BLOCKSIZE = 16384

// 最大的并发度，用来控制网络带宽的占用
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
		// 将收到的数据拷贝到state的data中
		// 这里的index用来做校验，保证传过来的数据的index是想要的那块piece
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

	// 对于当前piece来说，可能是分块下载的，每一次下载MAXBACKLOG个bytes
	// 因此当所有的piece块都没下载完的时候要继续下载
	for state.downloaded < task.length {
		// 如果Chocked为false，表示愿意上传数据
		if !conn.Chocked {
			// 当前并发的数量小于上限，并且请求的数量小于piece的总长度
			// 如果请求的数量大于piece的总长度了，就只需要等路上的都传回来即可
			// 这里每个task只会被一个go routine取走，因此每个task最多只会将请求数量增加1
			for state.backlog < MAXBACKLOG && state.requested < task.length {
				length := BLOCKSIZE
				// 对一个piece中的最后一段，可能长度会短一些，做一下特殊处理
				if task.length-state.requested < length {
					length = task.length - state.requested
				}

				// 新建一个request信息，告诉对方我要下这个piece的这个block了
				// 因为这里是将一个piece分成块，并且按顺序下载piece的每个块
				// 因此这里直接将requested的长度作为index即可
				// requested表示已经申请的块，在满足并发度的条件下继续申请后续的新的块
				// 感觉这里可以用滑动窗口来优化
				msg := NewRequestMsg(state.index, state.requested, length)
				_, err := state.conn.WriteMsg(msg)
				if err != nil {
					return nil, err
				}

				// 这里是串行的修改该值，不是多个go routine并发执行，不用考虑加锁
				state.backlog++
				state.requested += length
			}
		}

		// 发送了满足并发度的若干请求，等有数据回来，即ReadMsg()读到数据
		// 对读到的数据进行处理，注意这里和上面的循环都在一个大的循环中
		// 两者交替执行，一个不停的发请求，请求数量满足并发度，一个不停的读数据
		// 然后处理完减少并发度，使得又可以发新的请求，在发请求和处理数据的时候有任何Error
		// 都会返回，并将该piece的task放回队列中，等其他的peer处理
		// 感觉这里可以用go routine优化
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

// 这里的PeerInfo是准备建立连接的peer，要从该peer处下载
func (t *TorrentTask) peerRoutine(peer PeerInfo, taskQueue chan *pieceTask, resultQueue chan *pieceResult) {
	// set up conn with peer
	conn, err := NewConn(peer, t.InfoSHA, t.PeerId)
	if err != nil {
		fmt.Println("fail to connect peer: " + peer.Ip.String())
		return
	}
	defer conn.Close()

	fmt.Println("complete handshake with peer: " + peer.Ip.String())
	// 开始给对方发请求，表示想要从那里下载
	// 当前请求数据没有payload，只有Msg
	conn.WriteMsg(&PeerMsg{MsgInterested, nil})
	// 拿出taskQueue中的每一个task，判断对方有没有，如果有则开始下载
	// 这里是串行的，对单个peer来说只能一块一块的下
	// 而且这里是从channel中拿数据，可以保证多个go routine不会出问题
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
	// 注意这里的处理和对每个piece的分块下载处理不同
	// 一个是针对task中每个piece的长度，一个是单个piece中每次传输的块的长度
	if end > t.FileLen {
		// 对最后的一片做特殊处理，因为其长度可能不足一个piece了
		end = t.FileLen
	}
	return
}

func Download(task *TorrentTask) error {
	fmt.Println("start downloading " + task.FileName)

	// 划分piece任务并初始化task，result channel
	// task数量与SHA的数量相同，每个task的piece都有其对应的SHA
	// 指定channel的长度，如果channel已满，则新的go routine必须等待通道中的元素被取出后才能往其中加入新的元素
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

	// 起完上面的go routine，代码继续向下执行，进入for中
	// for中count一旦超过上限会结束，循环中从resultQueue中取出数据放入result的特定位置上

	// 收集结果
	buf := make([]byte, task.FileLen)
	count := 0
	// 这个for实际上是while的用法
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
