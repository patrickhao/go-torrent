package torrent

import "strconv"

type Bitfield []byte

// Bitfield中的方法，不一定传入的要是指针
func (field Bitfield) HasPiece(index int) bool {
	// 在byte数组中的位置
	byteIndex := index / 8
	// 在单个byte中的位置
	offset := index % 8

	if byteIndex < 0 || byteIndex >= len(field) {
		return false
	}

	// 右移到当前位置并与1按位与，看当前值是否为1
	return field[byteIndex]>>uint(7-offset)&1 != 0
}

func (field Bitfield) SetPiece(index int) {
	byteIndex := index / 8
	offset := index % 8

	if byteIndex < 0 || byteIndex >= len(field) {
		return
	}

	field[byteIndex] |= 1 << uint(7-offset)
}

func (field Bitfield) String() string {
	str := "piece# "
	// 因为一个byte中存储了8位，代表着8个piece的有无
	for i := 0; i < len(field)*8; i++ {
		if field.HasPiece(i) {
			str = str + strconv.Itoa(i) + " "
		}
	}

	return str
}
