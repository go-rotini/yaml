package yaml

import (
	"encoding/binary"
	"unicode/utf8"
)

func detectAndConvertEncoding(src []byte) ([]byte, error) {
	if len(src) >= 4 {
		if src[0] == 0x00 && src[1] == 0x00 && src[2] == 0xFE && src[3] == 0xFF {
			return decodeUTF32(src[4:], binary.BigEndian)
		}
		if src[0] == 0xFF && src[1] == 0xFE && src[2] == 0x00 && src[3] == 0x00 {
			return decodeUTF32(src[4:], binary.LittleEndian)
		}
	}
	if len(src) >= 2 {
		if src[0] == 0xFE && src[1] == 0xFF {
			return decodeUTF16(src[2:], binary.BigEndian)
		}
		if src[0] == 0xFF && src[1] == 0xFE {
			return decodeUTF16(src[2:], binary.LittleEndian)
		}
	}
	if len(src) >= 4 {
		if src[0] == 0x00 && src[1] == 0x00 && src[2] == 0x00 {
			return decodeUTF32(src, binary.BigEndian)
		}
		if src[1] == 0x00 && src[2] == 0x00 && src[3] == 0x00 {
			return decodeUTF32(src, binary.LittleEndian)
		}
	}
	if len(src) >= 2 {
		if src[0] == 0x00 && src[1] != 0x00 {
			return decodeUTF16(src, binary.BigEndian)
		}
		if src[0] != 0x00 && src[1] == 0x00 {
			return decodeUTF16(src, binary.LittleEndian)
		}
	}
	return src, nil
}

func decodeUTF16(data []byte, order binary.ByteOrder) ([]byte, error) {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	var buf []byte
	for i := 0; i+1 < len(data); i += 2 {
		code := order.Uint16(data[i : i+2])
		if code >= 0xD800 && code <= 0xDBFF {
			if i+3 < len(data) {
				low := order.Uint16(data[i+2 : i+4])
				if low >= 0xDC00 && low <= 0xDFFF {
					r := 0x10000 + (rune(code-0xD800)<<10 | rune(low-0xDC00))
					buf = utf8.AppendRune(buf, r)
					i += 2
					continue
				}
			}
			buf = utf8.AppendRune(buf, 0xFFFD)
			continue
		}
		buf = utf8.AppendRune(buf, rune(code))
	}
	return buf, nil
}

func decodeUTF32(data []byte, order binary.ByteOrder) ([]byte, error) {
	if len(data)%4 != 0 {
		data = data[:len(data)&^3]
	}
	var buf []byte
	for i := 0; i+3 < len(data); i += 4 {
		code := order.Uint32(data[i : i+4])
		if code > 0x10FFFF {
			buf = utf8.AppendRune(buf, 0xFFFD)
			continue
		}
		buf = utf8.AppendRune(buf, rune(code))
	}
	return buf, nil
}

func isPrintableYAML(r rune) bool {
	if r == 0x09 || r == 0x0A || r == 0x0D {
		return true
	}
	if r >= 0x20 && r <= 0x7E {
		return true
	}
	if r == 0x85 {
		return true
	}
	if r >= 0xA0 && r <= 0xD7FF {
		return true
	}
	if r >= 0xE000 && r <= 0xFFFD {
		return true
	}
	if r >= 0x10000 && r <= 0x10FFFF {
		return true
	}
	return false
}
