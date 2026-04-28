package yaml

import (
	"encoding/binary"
	"testing"
)

func TestDetectAndConvertUTF8(t *testing.T) {
	input := []byte("hello")
	out, err := detectAndConvertEncoding(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello" {
		t.Errorf("expected hello, got %q", out)
	}
}

func TestDetectAndConvertEmpty(t *testing.T) {
	out, err := detectAndConvertEncoding(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty, got %q", out)
	}
}

func TestDetectAndConvertOneByte(t *testing.T) {
	out, err := detectAndConvertEncoding([]byte{0x41})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "A" {
		t.Errorf("expected A, got %q", out)
	}
}

func TestDetectAndConvertUTF16BEWithBOM(t *testing.T) {
	var data []byte
	data = append(data, 0xFE, 0xFF)
	for _, b := range []byte("AB") {
		data = append(data, 0x00, b)
	}
	out, err := detectAndConvertEncoding(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "AB" {
		t.Errorf("expected AB, got %q", out)
	}
}

func TestDetectAndConvertUTF16LEWithBOM(t *testing.T) {
	var data []byte
	data = append(data, 0xFF, 0xFE)
	for _, b := range []byte("AB") {
		data = append(data, b, 0x00)
	}
	out, err := detectAndConvertEncoding(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "AB" {
		t.Errorf("expected AB, got %q", out)
	}
}

func TestDetectAndConvertUTF32BEWithBOM(t *testing.T) {
	var data []byte
	data = append(data, 0x00, 0x00, 0xFE, 0xFF)
	for _, b := range []byte("Hi") {
		data = append(data, 0x00, 0x00, 0x00, b)
	}
	out, err := detectAndConvertEncoding(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "Hi" {
		t.Errorf("expected Hi, got %q", out)
	}
}

func TestDetectAndConvertUTF32LEWithBOM(t *testing.T) {
	var data []byte
	data = append(data, 0xFF, 0xFE, 0x00, 0x00)
	for _, b := range []byte("Hi") {
		data = append(data, b, 0x00, 0x00, 0x00)
	}
	out, err := detectAndConvertEncoding(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "Hi" {
		t.Errorf("expected Hi, got %q", out)
	}
}

func TestDetectAndConvertUTF16BENoBOM(t *testing.T) {
	data := []byte{0x00, 0x41, 0x00, 0x42}
	out, err := detectAndConvertEncoding(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "AB" {
		t.Errorf("expected AB, got %q", out)
	}
}

func TestDetectAndConvertUTF16LENoBOM(t *testing.T) {
	data := []byte{0x41, 0x00, 0x42, 0x00}
	out, err := detectAndConvertEncoding(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "AB" {
		t.Errorf("expected AB, got %q", out)
	}
}

func TestDetectAndConvertUTF32BENoBOM(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x41, 0x00, 0x00, 0x00, 0x42}
	out, err := detectAndConvertEncoding(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "AB" {
		t.Errorf("expected AB, got %q", out)
	}
}

func TestDetectAndConvertUTF32LENoBOM(t *testing.T) {
	data := []byte{0x41, 0x00, 0x00, 0x00, 0x42, 0x00, 0x00, 0x00}
	out, err := detectAndConvertEncoding(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "AB" {
		t.Errorf("expected AB, got %q", out)
	}
}

func TestDecodeUTF16SurrogatePair(t *testing.T) {
	// U+1F600 (😀) = surrogate pair D83D DE00
	data := make([]byte, 4)
	binary.BigEndian.PutUint16(data[0:2], 0xD83D)
	binary.BigEndian.PutUint16(data[2:4], 0xDE00)
	out, err := decodeUTF16(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "😀" {
		t.Errorf("expected emoji, got %q", out)
	}
}

func TestDecodeUTF16OrphanHighSurrogate(t *testing.T) {
	data := make([]byte, 2)
	binary.BigEndian.PutUint16(data[0:2], 0xD800)
	out, err := decodeUTF16(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "�" {
		t.Errorf("expected replacement char, got %q", out)
	}
}

func TestDecodeUTF16OrphanHighSurrogateFollowedByNonLow(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint16(data[0:2], 0xD800)
	binary.BigEndian.PutUint16(data[2:4], 0x0041) // 'A', not a low surrogate
	out, err := decodeUTF16(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
	// Should produce replacement char for orphan high surrogate
	if out[0] != 0xEF { // first byte of U+FFFD in UTF-8
		t.Errorf("expected replacement char first, got %x", out[0])
	}
}

func TestDecodeUTF16OddLength(t *testing.T) {
	data := []byte{0x00, 0x41, 0xFF}
	out, err := decodeUTF16(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "A" {
		t.Errorf("expected A, got %q", out)
	}
}

func TestDecodeUTF16Empty(t *testing.T) {
	out, err := decodeUTF16(nil, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty, got %q", out)
	}
}

func TestDecodeUTF16LittleEndian(t *testing.T) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint16(data[0:2], 0x0048) // H
	binary.LittleEndian.PutUint16(data[2:4], 0x0069) // i
	out, err := decodeUTF16(data, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "Hi" {
		t.Errorf("expected Hi, got %q", out)
	}
}

func TestDecodeUTF32Basic(t *testing.T) {
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[0:4], 0x48) // H
	binary.BigEndian.PutUint32(data[4:8], 0x69) // i
	out, err := decodeUTF32(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "Hi" {
		t.Errorf("expected Hi, got %q", out)
	}
}

func TestDecodeUTF32InvalidCodepoint(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data[0:4], 0x110000)
	out, err := decodeUTF32(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "�" {
		t.Errorf("expected replacement char, got %q", out)
	}
}

func TestDecodeUTF32OddLength(t *testing.T) {
	data := make([]byte, 6)
	binary.BigEndian.PutUint32(data[0:4], 0x41)
	data[4] = 0xFF
	data[5] = 0xFE
	out, err := decodeUTF32(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "A" {
		t.Errorf("expected A, got %q", out)
	}
}

func TestDecodeUTF32Empty(t *testing.T) {
	out, err := decodeUTF32(nil, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty, got %q", out)
	}
}

func TestDecodeUTF32LittleEndian(t *testing.T) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data[0:4], 0x41)
	out, err := decodeUTF32(data, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "A" {
		t.Errorf("expected A, got %q", out)
	}
}

func TestIsPrintableYAML(t *testing.T) {
	if !isPrintableYAML('A') {
		t.Error("A should be printable")
	}
	if !isPrintableYAML('\t') {
		t.Error("tab should be printable")
	}
	if !isPrintableYAML('\n') {
		t.Error("LF should be printable")
	}
	if !isPrintableYAML('\r') {
		t.Error("CR should be printable")
	}
	if isPrintableYAML(0x01) {
		t.Error("0x01 should not be printable")
	}
	if isPrintableYAML(0x00) {
		t.Error("NUL should not be printable")
	}
	if !isPrintableYAML(0x85) {
		t.Error("NEL should be printable")
	}
	if !isPrintableYAML(0xA0) {
		t.Error("NBSP should be printable")
	}
	if !isPrintableYAML(0xD7FF) {
		t.Error("0xD7FF should be printable")
	}
	if isPrintableYAML(0xD800) {
		t.Error("surrogate 0xD800 should not be printable")
	}
	if !isPrintableYAML(0xE000) {
		t.Error("private use area start should be printable")
	}
	if !isPrintableYAML(0xFFFD) {
		t.Error("0xFFFD should be printable")
	}
	if isPrintableYAML(0xFFFE) {
		t.Error("0xFFFE should not be printable")
	}
	if !isPrintableYAML(0x10000) {
		t.Error("supplementary plane should be printable")
	}
	if !isPrintableYAML(0x10FFFF) {
		t.Error("max codepoint should be printable")
	}
	if isPrintableYAML(0x110000) {
		t.Error("beyond max codepoint should not be printable")
	}
	if isPrintableYAML(0x9F) {
		t.Error("0x9F (between NEL and NBSP) should not be printable")
	}
	if !isPrintableYAML(0x20) {
		t.Error("space should be printable")
	}
	if !isPrintableYAML(0x7E) {
		t.Error("tilde should be printable")
	}
	if isPrintableYAML(0x7F) {
		t.Error("DEL should not be printable")
	}
}

func TestIsPrintableYAMLBoundaries(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{0x08, false},
		{0x09, true},  // TAB
		{0x0A, true},  // LF
		{0x0B, false}, // VT
		{0x0C, false}, // FF
		{0x0D, true},  // CR
		{0x0E, false},
		{0x1F, false},
		{0x20, true},
		{0x7E, true},
		{0x7F, false},
		{0x84, false},
		{0x85, true}, // NEL
		{0x86, false},
		{0x9F, false},
		{0xA0, true},
		{0xD7FF, true},
		{0xD800, false},
		{0xDFFF, false},
		{0xE000, true},
		{0xFFFD, true},
		{0xFFFE, false},
		{0xFFFF, false},
		{0x10000, true},
		{0x10FFFF, true},
		{0x110000, false},
	}
	for _, tt := range tests {
		got := isPrintableYAML(tt.r)
		if got != tt.want {
			t.Errorf("isPrintableYAML(0x%X) = %v, want %v", tt.r, got, tt.want)
		}
	}
}

func TestDetectEncodingExactly4Bytes(t *testing.T) {
	src := []byte{0x00, 0x00, 0xFE, 0xFF}
	_, err := detectAndConvertEncoding(src)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDetectEncodingExactly3Bytes(t *testing.T) {
	src := []byte{0x41, 0x42, 0x43}
	result, err := detectAndConvertEncoding(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != "ABC" {
		t.Errorf("3-byte UTF-8 should pass through, got %q", string(result))
	}
}

func TestDetectEncodingExactly2Bytes(t *testing.T) {
	src := []byte{0xFE, 0xFF}
	_, err := detectAndConvertEncoding(src)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDetectEncodingExactly1Byte(t *testing.T) {
	src := []byte{0x41}
	result, err := detectAndConvertEncoding(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != "A" {
		t.Errorf("1-byte should pass through, got %q", string(result))
	}
}

func TestDecodeUTF16SurrogateBoundaryLow(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint16(data[0:2], 0xD800)
	binary.BigEndian.PutUint16(data[2:4], 0xDC00)
	out, err := decodeUTF16(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 || out[0] == 0xEF {
		t.Error("valid surrogate pair at boundary should not produce replacement char")
	}
}

func TestDecodeUTF16SurrogateBoundaryHigh(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint16(data[0:2], 0xDBFF)
	binary.BigEndian.PutUint16(data[2:4], 0xDFFF)
	out, err := decodeUTF16(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 || out[0] == 0xEF {
		t.Error("valid surrogate pair at high boundary should not produce replacement char")
	}
}

func TestDecodeUTF16HighSurrogateWithInsufficientData(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint16(data[0:2], 0x0041)
	binary.BigEndian.PutUint16(data[2:4], 0xD800)
	out, err := decodeUTF16(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if s[0] != 'A' {
		t.Errorf("first char should be A, got %q", s)
	}
}

func TestDecodeUTF32BoundaryCodepoint(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data[0:4], 0x10FFFF)
	out, err := decodeUTF32(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) == "�" {
		t.Error("U+10FFFF is valid and should not produce replacement char")
	}
}

func TestDecodeUTF32JustAboveBoundary(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data[0:4], 0x110000)
	out, err := decodeUTF32(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "�" {
		t.Errorf("U+110000 is invalid, expected replacement char, got %q", string(out))
	}
}

func TestDecodeUTF32LoopBoundary(t *testing.T) {
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[0:4], 0x41)
	binary.BigEndian.PutUint32(data[4:8], 0x42)
	out, err := decodeUTF32(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "AB" {
		t.Errorf("expected AB, got %q", string(out))
	}
}

func TestDecodeUTF16LoopBoundary(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint16(data[0:2], 0x0041)
	binary.BigEndian.PutUint16(data[2:4], 0x0042)
	out, err := decodeUTF16(data, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "AB" {
		t.Errorf("expected AB, got %q", string(out))
	}
}
