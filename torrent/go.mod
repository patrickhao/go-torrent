module github/patrickhao/go-torrent/torrent

go 1.20

require (
	github.com/patrickhao/go-torrent/bencode v0.0.0
	github.com/stretchr/testify v1.8.2
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/patrickhao/go-torrent/bencode => ../bencode
