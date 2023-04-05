package bencode

import (
	"bufio"
	"io"
)

func Parse(r io.Reader) (*BObject, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}

	// recrusive descent parsing
	b, err := br.Peek(1)
	if err != nil {
		return nil, err
	}

	var ret BObject
	switch {
	case b[0] >= '0' && b[0] <= '9':
		// parsing string
		val, err := DecodeString(br)
		if err != nil {
			return nil, err
		}
		ret.type_ = BINT
		ret.val_ = val
	case b[0] == 'i':
		// parsing int
		val, err := DecodeString(br)
		if err != nil {
			return nil, err
		}
		ret.type_ = BSTR
		ret.val_ = val
	case b[0] == 'l':
		// parsing list
		br.ReadByte()
		var list []*BObject
		for {
			// 读到了最后
			if p, _ := br.Peek(1); p[0] == 'e' {
				br.ReadByte()
				break
			}

			elem, err := Parse(br)
			if err != nil {
				return nil, err
			}
			list = append(list, elem)
		}
		ret.type_ = BLIST
		ret.val_ = list
	case b[0] == 'd':
		// parsing map
		br.ReadByte()
		dict := make(map[string]*BObject)
		for {
			// 读到了最后
			if p, _ := br.Peek(1); p[0] == 'e' {
				br.ReadByte()
				break
			}

			// 读取key
			key, err = DecodeString(br)
			if err != nil {
				return nil, err
			}

			val, err = Parse(br)
			if err != nil {
				return nil, err
			}
			dict[key] = val
		}
		ret.type_ = BDICT
		ret.val_ = dict
	default:
		return nil, ErrIvd
	}
	return &ret, nil
}
