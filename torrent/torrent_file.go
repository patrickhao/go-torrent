package torrent

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"

	"github.com/patrickhao/go-torrent/bencode"
)

// 原始文件中该key有空格，因此不能依靠将名字转为小写来定位到key，只能手动打tag
type rawInfo struct {
	Length      int    `bencode:"length"`
	Name        string `bencode:"name"`
	PieceLength int    `bencode:"piece length"`
	Pieces      string `bencode:"pieces"`
}

type rawFile struct {
	Announce string  `bencode:"announce"`
	Info     rawInfo `bencode:"info"`
}

// SHA值放入数组中，方便使用
const SHALEN int = 20

// torrent file的原生格式不太好用，转成下面的struct
// InfoSHA是文件的唯一标识，通信的时候通过其确定文件有没有
type TorrentFile struct {
	Announce string
	InfoSHA  [SHALEN]byte
	FileName string
	FileLen  int
	PieceLen int
	PieceSHA [][SHALEN]byte
}

func ParseFile(r io.Reader) (*TorrentFile, error) {
	// raw是一个指针
	raw := new(rawFile)
	err := bencode.Unmarshal(r, raw)
	if err != nil {
		fmt.Println("Fail to parse torrent file")
		return nil, err
	}

	ret := new(TorrentFile)
	ret.Announce = raw.Announce
	ret.FileName = raw.Info.Name
	ret.FileLen = raw.Info.Length
	ret.PieceLen = raw.Info.PieceLength

	// 计算SHA
	buf := new(bytes.Buffer)
	wlen := bencode.Marshal(buf, raw.Info)

	if wlen == 0 {
		fmt.Println("raw file info error")
	}

	ret.InfoSHA = sha1.Sum(buf.Bytes())

	// 计算每一块的SHA
	// 这里做了一个类型转换，将raw.Info.Pieces转换为byte slice
	// raw.Info.Pieces是一个string，转成byte slice方便处理
	bys := []byte(raw.Info.Pieces)
	cnt := len(bys) / SHALEN
	hashes := make([][SHALEN]byte, cnt)
	for i := 0; i < cnt; i++ {
		copy(hashes[i][:], bys[i*SHALEN:(i+1)*SHALEN])
	}
	ret.PieceSHA = hashes
	return ret, nil
}
