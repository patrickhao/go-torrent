package torrent

import (
	"bufio"
	"fmt"
	"math/rand"
	"net"
	"os"
	"testing"
)

func TestPeer(t *testing.T) {
	var peer PeerInfo
	peer.Ip = net.ParseIP("80.246.246.22")
	peer.Port = uint16(17948)

	file, _ := os.Open("../testfile/debian-iso.torrent")
	tf, _ := ParseFile(bufio.NewReader(file))

	var peerId [IDLEN]byte
	_, _ = rand.Read(peerId[:])

	conn, err := NewConn(peer, tf.InfoSHA, peerId)
	if err != nil {
		t.Error("new peer err: " + err.Error())
	}
	fmt.Println(conn)
}
