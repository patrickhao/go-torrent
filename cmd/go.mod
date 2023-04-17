module github.com/patrickhao/go-torrent/main

go 1.20

require (
	github.com/patrickhao/go-torrent/torrent v0.0.0
	github.com/patrickhao/go-torrent/bencode v0.0.0
)

replace (
	github.com/patrickhao/go-torrent/torrent => ../torrent
	github.com/patrickhao/go-torrent/bencode => ../bencode
)
