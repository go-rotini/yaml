package yaml

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewEncoderInternal(t *testing.T) {
	opts := &encoderOptions{indent: 2}
	enc := newEncoder(opts)
	if enc == nil {
		t.Fatal("expected non-nil encoder")
	}
	if enc.opts != opts {
		t.Error("encoder options not set")
	}
	if enc.ctx == nil {
		t.Error("expected non-nil context")
	}
}

func TestEncodeTrailingNewline(t *testing.T) {
	enc := newEncoder(&encoderOptions{indent: 2})
	data, err := enc.encode(reflect.ValueOf("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if data[len(data)-1] != '\n' {
		t.Errorf("expected trailing newline, got %q", data)
	}
}

func TestEncodeInvalidValue(t *testing.T) {
	enc := newEncoder(&encoderOptions{indent: 2})
	data, err := enc.encode(reflect.Value{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "null") {
		t.Errorf("expected null for invalid value, got %q", data)
	}
}

func TestEncodeWithComments(t *testing.T) {
	enc := newEncoder(&encoderOptions{
		indent: 2,
		comments: map[string][]Comment{
			"$.name": {{Position: LineCommentPos, Text: "the name"}},
		},
	})
	data, err := enc.encode(reflect.ValueOf(map[string]string{"name": "alice"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# the name") {
		t.Errorf("expected comment in output, got %q", data)
	}
}

func TestApplyCommentsAllPositions(t *testing.T) {
	buf := []byte("name: alice\n")
	cs := map[string][]Comment{
		"$.name": {
			{Position: HeadCommentPos, Text: "head"},
			{Position: LineCommentPos, Text: "line"},
			{Position: FootCommentPos, Text: "foot"},
		},
	}
	result := applyComments(buf, cs)
	s := string(result)
	if !strings.Contains(s, "# head") {
		t.Errorf("expected head comment, got:\n%s", s)
	}
	if !strings.Contains(s, "# line") {
		t.Errorf("expected line comment, got:\n%s", s)
	}
	if !strings.Contains(s, "# foot") {
		t.Errorf("expected foot comment, got:\n%s", s)
	}
}

func TestInsertHeadCommentEmptyPath(t *testing.T) {
	buf := []byte("name: alice\n")
	result := insertHeadComment(buf, "$", "comment")
	if !bytes.Equal(result, buf) {
		t.Errorf("expected no change for root path, got:\n%s", result)
	}
}

func TestInsertHeadCommentKeyNotFound(t *testing.T) {
	buf := []byte("name: alice\n")
	result := insertHeadComment(buf, "$.missing", "comment")
	if !bytes.Equal(result, buf) {
		t.Errorf("expected no change for missing key, got:\n%s", result)
	}
}

func TestInsertLineCommentEmptyPath(t *testing.T) {
	buf := []byte("name: alice\n")
	result := insertLineComment(buf, "$", "comment")
	if !bytes.Equal(result, buf) {
		t.Errorf("expected no change for root path, got:\n%s", result)
	}
}

func TestInsertLineCommentKeyNotFound(t *testing.T) {
	buf := []byte("name: alice\n")
	result := insertLineComment(buf, "$.missing", "comment")
	if !bytes.Equal(result, buf) {
		t.Errorf("expected no change for missing key, got:\n%s", result)
	}
}

func TestInsertFootCommentEmptyPath(t *testing.T) {
	buf := []byte("name: alice\n")
	result := insertFootComment(buf, "$", "comment")
	if !bytes.Equal(result, buf) {
		t.Errorf("expected no change for root path, got:\n%s", result)
	}
}

func TestInsertFootCommentKeyNotFound(t *testing.T) {
	buf := []byte("name: alice\n")
	result := insertFootComment(buf, "$.missing", "comment")
	if !bytes.Equal(result, buf) {
		t.Errorf("expected no change for missing key, got:\n%s", result)
	}
}

func TestInsertHeadCommentIndented(t *testing.T) {
	buf := []byte("outer:\n  name: alice\n")
	result := insertHeadComment(buf, "$.outer.name", "about name")
	s := string(result)
	if !strings.Contains(s, "  # about name") {
		t.Errorf("expected indented head comment, got:\n%s", s)
	}
	headIdx := strings.Index(s, "# about name")
	nameIdx := strings.Index(s, "name:")
	if headIdx > nameIdx {
		t.Error("head comment should appear before key")
	}
}

func TestInsertFootCommentIndented(t *testing.T) {
	buf := []byte("outer:\n  name: alice\n")
	result := insertFootComment(buf, "$.outer.name", "after name")
	s := string(result)
	if !strings.Contains(s, "  # after name") {
		t.Errorf("expected indented foot comment, got:\n%s", s)
	}
	footIdx := strings.Index(s, "# after name")
	nameIdx := strings.Index(s, "name:")
	if footIdx < nameIdx {
		t.Error("foot comment should appear after key")
	}
}

func TestPathToKey(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"$.name", "name"},
		{"$.outer.inner.key", "key"},
		{"$.", ""},
		{"name", "name"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := pathToKey(tt.path)
			if got != tt.want {
				t.Errorf("pathToKey(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestMarshalValueNil(t *testing.T) {
	data, err := Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "null") {
		t.Errorf("expected null for nil, got %q", data)
	}
}

func TestMarshalValueNilPointer(t *testing.T) {
	type Config struct {
		Name  string  `yaml:"name"`
		Value *string `yaml:"value"`
	}
	data, err := Marshal(Config{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "value: null") {
		t.Errorf("expected null for nil pointer, got:\n%s", data)
	}
}

func TestMarshalValueNilInterface(t *testing.T) {
	type Config struct {
		Val any `yaml:"val"`
	}
	data, err := Marshal(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "val: null") {
		t.Errorf("expected null for nil interface, got:\n%s", data)
	}
}

func TestMarshalValuePointerDeref(t *testing.T) {
	s := "hello"
	data, err := Marshal(&s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "hello" {
		t.Errorf("expected hello, got %q", data)
	}
}

func TestMarshalValueInterfaceDeref(t *testing.T) {
	var v any = map[string]int{"x": 1}
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "x: 1") {
		t.Errorf("expected x: 1, got:\n%s", data)
	}
}

type encodeMarshalerContext struct {
	Val string
}

func (e encodeMarshalerContext) MarshalYAML(ctx context.Context) (any, error) {
	prefix := ctx.Value(encodeCtxKey("prefix"))
	if prefix != nil {
		return prefix.(string) + e.Val, nil
	}
	return e.Val, nil
}

type encodeCtxKey string

func TestMarshalMarshalerContextInterface(t *testing.T) {
	v := encodeMarshalerContext{Val: "world"}
	ctx := context.WithValue(context.Background(), encodeCtxKey("prefix"), "hello-")
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.EncodeContext(ctx, v); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "hello-world") {
		t.Errorf("expected hello-world, got %q", buf.String())
	}
}

type encodeMarshalerBasic struct {
	value string
}

func (e encodeMarshalerBasic) MarshalYAML() (any, error) {
	return "custom:" + e.value, nil
}

func TestMarshalMarshalerInterface(t *testing.T) {
	data, err := Marshal(encodeMarshalerBasic{value: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	s := strings.TrimSpace(string(data))
	if !strings.Contains(s, "custom:hello") {
		t.Errorf("expected custom:hello, got %q", s)
	}
}

type encodeMarshalerError struct{}

func (e encodeMarshalerError) MarshalYAML() (any, error) {
	return nil, fmt.Errorf("marshal error")
}

func TestMarshalMarshalerReturnsError(t *testing.T) {
	_, err := Marshal(encodeMarshalerError{})
	if err == nil {
		t.Error("expected error from MarshalerError")
	}
}

type encodeMarshalerContextError struct{}

func (e encodeMarshalerContextError) MarshalYAML(ctx context.Context) (any, error) {
	return nil, fmt.Errorf("context marshal error")
}

func TestMarshalMarshalerContextReturnsError(t *testing.T) {
	_, err := Marshal(encodeMarshalerContextError{})
	if err == nil {
		t.Error("expected error from MarshalerContextError")
	}
}

type encodeBytesMarshaler struct {
	data string
}

func (e encodeBytesMarshaler) MarshalYAML() ([]byte, error) {
	return []byte(e.data), nil
}

func TestMarshalBytesMarshalerInterface(t *testing.T) {
	data, err := Marshal(encodeBytesMarshaler{data: "raw-yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "raw-yaml") {
		t.Errorf("expected raw-yaml, got %q", data)
	}
}

type encodeBytesMarshalerError struct{}

func (e encodeBytesMarshalerError) MarshalYAML() ([]byte, error) {
	return nil, fmt.Errorf("bytes marshal error")
}

func TestMarshalBytesMarshalerReturnsError(t *testing.T) {
	_, err := Marshal(encodeBytesMarshalerError{})
	if err == nil {
		t.Error("expected error from BytesMarshalerError")
	}
}

func TestMarshalTextMarshaler(t *testing.T) {
	data, err := Marshal(big.NewInt(999999999999))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "999999999999") {
		t.Errorf("expected big int in output, got:\n%s", data)
	}
}

type encodePtrMarshaler struct {
	Val string
}

func (p *encodePtrMarshaler) MarshalYAML() (any, error) {
	return p.Val, nil
}

func TestMarshalPointerReceiverMarshalerEncode(t *testing.T) {
	type S struct {
		M encodePtrMarshaler `yaml:"m"`
	}
	v := S{M: encodePtrMarshaler{Val: "hello"}}
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Errorf("expected hello, got:\n%s", data)
	}
}

type encodePtrBytesMarshaler struct {
	raw string
}

func (p *encodePtrBytesMarshaler) MarshalYAML() ([]byte, error) {
	return []byte(p.raw), nil
}

func TestMarshalPointerReceiverBytesMarshaler(t *testing.T) {
	v := &encodePtrBytesMarshaler{raw: "raw-value"}
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "raw-value") {
		t.Errorf("expected raw-value, got:\n%s", data)
	}
}

type encodePtrTextMarshaler struct {
	text string
}

func (p *encodePtrTextMarshaler) MarshalText() ([]byte, error) {
	return []byte(p.text), nil
}

func TestMarshalPointerReceiverTextMarshaler(t *testing.T) {
	v := &encodePtrTextMarshaler{text: "text-value"}
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "text-value") {
		t.Errorf("expected text-value, got:\n%s", data)
	}
}

func TestMarshalCustomMarshalerOption(t *testing.T) {
	type Color struct {
		R, G, B uint8
	}
	c := Color{R: 255, G: 128, B: 0}
	out, err := MarshalWithOptions(c, WithCustomMarshaler(func(c Color) ([]byte, error) {
		return []byte(fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "#ff8000") {
		t.Fatalf("expected #ff8000, got %s", out)
	}
}

func TestMarshalString(t *testing.T) {
	data, err := Marshal("hello world")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "hello world" {
		t.Errorf("expected hello world, got %q", data)
	}
}

func TestMarshalBooleans(t *testing.T) {
	type Config struct {
		A bool `yaml:"a"`
		B bool `yaml:"b"`
	}
	data, err := Marshal(Config{A: true, B: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "a: true") {
		t.Errorf("expected 'a: true', got:\n%s", data)
	}
	if !strings.Contains(string(data), "b: false") {
		t.Errorf("expected 'b: false', got:\n%s", data)
	}
}

func TestMarshalIntegers(t *testing.T) {
	type Nums struct {
		I8  int8  `yaml:"i8"`
		I16 int16 `yaml:"i16"`
		I32 int32 `yaml:"i32"`
		I64 int64 `yaml:"i64"`
	}
	data, err := Marshal(Nums{I8: -1, I16: -2, I32: -3, I64: -4})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "i8: -1") {
		t.Errorf("expected i8: -1, got:\n%s", s)
	}
}

func TestMarshalUnsignedIntegers(t *testing.T) {
	type Nums struct {
		U8  uint8  `yaml:"u8"`
		U16 uint16 `yaml:"u16"`
		U32 uint32 `yaml:"u32"`
		U64 uint64 `yaml:"u64"`
	}
	data, err := Marshal(Nums{U8: 1, U16: 2, U32: 3, U64: 4})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "u8: 1") {
		t.Errorf("expected u8: 1, got:\n%s", s)
	}
}

func TestMarshalDurationEncode(t *testing.T) {
	type Config struct {
		Timeout time.Duration `yaml:"timeout"`
	}
	data, err := Marshal(Config{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "5s") {
		t.Errorf("expected 5s, got:\n%s", data)
	}
}

func TestMarshalSpecialFloats(t *testing.T) {
	type Config struct {
		Inf    float64 `yaml:"inf"`
		NegInf float64 `yaml:"neg_inf"`
		NaN    float64 `yaml:"nan"`
	}
	data, err := Marshal(Config{
		Inf:    math.Inf(1),
		NegInf: math.Inf(-1),
		NaN:    math.NaN(),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "inf: .inf") {
		t.Errorf("expected .inf, got:\n%s", s)
	}
	if !strings.Contains(s, "neg_inf: -.inf") {
		t.Errorf("expected -.inf, got:\n%s", s)
	}
	if !strings.Contains(s, "nan: .nan") {
		t.Errorf("expected .nan, got:\n%s", s)
	}
}

func TestMarshalRegularFloat(t *testing.T) {
	data, err := Marshal(3.14)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "3.14") {
		t.Errorf("expected 3.14, got %q", data)
	}
}

func TestMarshalFloat32(t *testing.T) {
	data, err := Marshal(float32(1.5))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "1.5") {
		t.Errorf("expected 1.5, got %q", data)
	}
}

func TestMarshalNilSlice(t *testing.T) {
	type Config struct {
		Slice []string `yaml:"slice"`
	}
	data, err := Marshal(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "slice: null") {
		t.Errorf("expected null for nil slice, got:\n%s", data)
	}
}

func TestMarshalByteSliceBinary(t *testing.T) {
	v := struct {
		Data []byte `yaml:"data"`
	}{Data: []byte("hello")}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "!!binary") {
		t.Errorf("expected !!binary tag, got:\n%s", s)
	}
	if !strings.Contains(s, "aGVsbG8=") {
		t.Errorf("expected base64 content, got:\n%s", s)
	}
}

func TestMarshalEmptySlice(t *testing.T) {
	data, err := Marshal([]int{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "[]" {
		t.Errorf("expected [], got %q", data)
	}
}

func TestMarshalSliceBlockStyle(t *testing.T) {
	data, err := Marshal([]string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "- a") || !strings.Contains(s, "- b") || !strings.Contains(s, "- c") {
		t.Errorf("expected block sequence, got:\n%s", s)
	}
}

func TestMarshalSliceWithCompoundElements(t *testing.T) {
	input := []map[string]int{{"a": 1}, {"b": 2}}
	data, err := Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "a: 1") || !strings.Contains(out, "b: 2") {
		t.Errorf("expected compound elements, got:\n%s", out)
	}
}

func TestMarshalFlowSequence(t *testing.T) {
	data, err := MarshalWithOptions([]int{1, 2, 3}, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(data))
	if out != "[1, 2, 3]" {
		t.Errorf("expected [1, 2, 3], got %s", out)
	}
}

func TestMarshalFlowSequenceStrings(t *testing.T) {
	data, err := MarshalWithOptions([]string{"a", "b", "c"}, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(data))
	if out != "[a, b, c]" {
		t.Errorf("expected [a, b, c], got %s", out)
	}
}

func TestMarshalArray(t *testing.T) {
	arr := [3]int{1, 2, 3}
	data, err := Marshal(arr)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "- 1") || !strings.Contains(s, "- 2") || !strings.Contains(s, "- 3") {
		t.Errorf("expected array items in output:\n%s", s)
	}
}

func TestMarshalIndentSequence(t *testing.T) {
	type S struct {
		Items []string `yaml:"items"`
	}
	v := S{Items: []string{"a", "b"}}
	data, err := MarshalWithOptions(v, WithIndentSequence(true))
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "  - a") {
		t.Errorf("expected indented sequence items, got:\n%s", out)
	}
}

func TestMarshalNilMap(t *testing.T) {
	type Config struct {
		Map map[string]string `yaml:"map"`
	}
	data, err := Marshal(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "map: null") {
		t.Errorf("expected null for nil map, got:\n%s", data)
	}
}

func TestMarshalEmptyMap(t *testing.T) {
	data, err := Marshal(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "{}" {
		t.Errorf("expected {}, got %s", data)
	}
}

func TestMarshalMapBlockStyle(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	data, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "a: 1") || !strings.Contains(s, "b: 2") {
		t.Errorf("expected block mapping, got:\n%s", s)
	}
}

func TestMarshalMapSorted(t *testing.T) {
	m := map[string]int{"z": 1, "a": 2, "m": 3}
	data, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	aIdx := strings.Index(s, "a:")
	mIdx := strings.Index(s, "m:")
	zIdx := strings.Index(s, "z:")
	if aIdx > mIdx || mIdx > zIdx {
		t.Errorf("expected sorted keys, got:\n%s", s)
	}
}

func TestMarshalMapWithCompoundValues(t *testing.T) {
	m := map[string]any{
		"outer": map[string]any{"inner": "val"},
	}
	data, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "inner:") {
		t.Errorf("expected nested mapping, got:\n%s", data)
	}
}

func TestMarshalFlowMapping(t *testing.T) {
	data, err := MarshalWithOptions(map[string]int{"a": 1, "b": 2}, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(data))
	if !strings.HasPrefix(out, "{") || !strings.HasSuffix(out, "}") {
		t.Errorf("expected flow mapping, got: %s", out)
	}
}

func TestMarshalStruct(t *testing.T) {
	type Person struct {
		Name string `yaml:"name"`
		Age  int    `yaml:"age"`
	}
	data, err := Marshal(Person{Name: "alice", Age: 30})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "name: alice") || !strings.Contains(s, "age: 30") {
		t.Errorf("expected struct fields, got:\n%s", s)
	}
}

func TestMarshalNestedStruct(t *testing.T) {
	type Inner struct {
		Value string `yaml:"value"`
	}
	type Outer struct {
		Name  string `yaml:"name"`
		Inner Inner  `yaml:"inner"`
	}
	data, err := Marshal(Outer{Name: "test", Inner: Inner{Value: "nested"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "name: test") || !strings.Contains(s, "value: nested") {
		t.Errorf("expected nested struct, got:\n%s", s)
	}
}

func TestMarshalStructOmitEmpty(t *testing.T) {
	type Config struct {
		Str   string            `yaml:"str,omitempty"`
		Int   int               `yaml:"int,omitempty"`
		Uint  uint              `yaml:"uint,omitempty"`
		Float float64           `yaml:"float,omitempty"`
		Bool  bool              `yaml:"bool,omitempty"`
		Ptr   *string           `yaml:"ptr,omitempty"`
		Slice []string          `yaml:"slice,omitempty"`
		Map   map[string]string `yaml:"map,omitempty"`
		Iface any               `yaml:"iface,omitempty"`
		Time  time.Time         `yaml:"time,omitempty"`
	}
	data, err := MarshalWithOptions(Config{}, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(data))
	if out != "{}" {
		t.Errorf("expected empty struct to marshal as {}, got:\n%s", out)
	}
}

func TestMarshalStructOmitEmptyPopulated(t *testing.T) {
	s := "hello"
	data, err := Marshal(struct {
		Str string  `yaml:"str,omitempty"`
		Ptr *string `yaml:"ptr,omitempty"`
	}{Str: "x", Ptr: &s})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "str: x") {
		t.Errorf("expected populated fields, got:\n%s", data)
	}
}

func TestMarshalStructInlineMap(t *testing.T) {
	type Config struct {
		Name  string         `yaml:"name"`
		Extra map[string]any `yaml:",inline"`
	}
	c := Config{Name: "test", Extra: map[string]any{"custom": "val"}}
	data, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "name: test") || !strings.Contains(s, "custom: val") {
		t.Errorf("expected inline map fields, got:\n%s", s)
	}
}

func TestMarshalStructInlineMapSorted(t *testing.T) {
	type S struct {
		Name  string            `yaml:"name"`
		Extra map[string]string `yaml:",inline"`
	}
	v := S{Name: "test", Extra: map[string]string{"foo": "bar", "baz": "qux"}}
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	bazIdx := strings.Index(s, "baz:")
	fooIdx := strings.Index(s, "foo:")
	if bazIdx > fooIdx {
		t.Errorf("expected sorted inline map keys, got:\n%s", s)
	}
}

func TestMarshalStructInlineMapCompoundValues(t *testing.T) {
	type S struct {
		Extra map[string][]int `yaml:",inline"`
	}
	v := S{Extra: map[string][]int{"items": {1, 2, 3}}}
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "items:") {
		t.Errorf("expected inline map with compound values, got:\n%s", data)
	}
}

func TestMarshalStructFlowTag(t *testing.T) {
	type Config struct {
		Labels map[string]string `yaml:"labels,flow"`
	}
	c := Config{Labels: map[string]string{"app": "web"}}
	data, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "{") {
		t.Errorf("expected flow-style map, got:\n%s", data)
	}
}

func TestMarshalFlowStruct(t *testing.T) {
	type Config struct {
		Name  string `yaml:"name"`
		Value int    `yaml:"value"`
	}
	data, err := MarshalWithOptions(Config{Name: "a", Value: 1}, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.TrimSpace(string(data))
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		t.Errorf("expected flow struct, got %q", s)
	}
}

func TestMarshalFlowStructOmitEmpty(t *testing.T) {
	type Config struct {
		Name  string `yaml:"name"`
		Value int    `yaml:"value,omitempty"`
	}
	data, err := MarshalWithOptions(Config{Name: "a"}, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.TrimSpace(string(data))
	if strings.Contains(s, "value") {
		t.Errorf("expected omitted empty value, got %q", s)
	}
}

func TestWriteScalarPlain(t *testing.T) {
	data, err := Marshal("hello")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "hello" {
		t.Errorf("expected plain scalar, got %q", data)
	}
}

func TestWriteScalarJSONCompat(t *testing.T) {
	data, err := MarshalWithOptions(map[string]string{"key": "value"}, WithJSON(true))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"key"`) || !strings.Contains(s, `"value"`) {
		t.Errorf("expected quoted keys and values in JSON mode, got:\n%s", s)
	}
}

func TestWriteScalarLiteralBlock(t *testing.T) {
	m := map[string]string{"config": "line one\nline two\nline three\n"}
	data, err := MarshalWithOptions(m, WithLiteralStyle(true))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "|") {
		t.Errorf("expected literal block style indicator, got:\n%s", s)
	}
}

func TestWriteScalarLiteralBlockNoTrailingNewline(t *testing.T) {
	m := map[string]string{"config": "line one\nline two"}
	data, err := MarshalWithOptions(m, WithLiteralStyle(true))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "|-") {
		t.Errorf("expected literal strip style |- for no trailing newline, got:\n%s", s)
	}
}

func TestWriteScalarAutoInt(t *testing.T) {
	out, err := MarshalWithOptions("42", WithAutoInt(true))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.TrimSpace(string(out))
	if s != "42" {
		t.Errorf("expected unquoted 42 with AutoInt, got %q", s)
	}
}

func TestWriteScalarAutoIntNonNumeric(t *testing.T) {
	out, err := MarshalWithOptions("hello", WithAutoInt(true))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.TrimSpace(string(out))
	if s != "hello" {
		t.Errorf("expected plain hello, got %q", s)
	}
}

func TestWriteScalarSingleQuote(t *testing.T) {
	m := map[string]string{"key": "needs: quoting"}
	data, err := MarshalWithOptions(m, WithSingleQuote(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "'needs: quoting'") {
		t.Errorf("expected single-quoted value, got:\n%s", data)
	}
}

func TestWriteScalarSingleQuoteFallbackForNewline(t *testing.T) {
	m := map[string]string{"key": "has\nnewline"}
	data, err := MarshalWithOptions(m, WithSingleQuote(true))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "'has") {
		t.Errorf("single quote should not be used for strings with newlines, got:\n%s", s)
	}
	if !strings.Contains(s, `"has\nnewline"`) {
		t.Errorf("expected double-quoted for newline, got:\n%s", s)
	}
}

func TestWriteScalarSingleQuoteFallbackForApostrophe(t *testing.T) {
	m := map[string]string{"key": "it's"}
	data, err := MarshalWithOptions(m, WithSingleQuote(true))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "'it's'") {
		t.Errorf("single quote should not be used for strings with apostrophes, got:\n%s", s)
	}
}

func TestWriteQuotedScalarEscapeSequences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"tab", "hello\tworld", `"hello\tworld"`},
		{"carriage return", "hello\rworld", `"hello\rworld"`},
		{"bell", "hello\aworld", `"hello\aworld"`},
		{"backspace", "hello\bworld", `"hello\bworld"`},
		{"vertical tab", "hello\vworld", `"hello\vworld"`},
		{"form feed", "hello\fworld", `"hello\fworld"`},
		{"escape", "hello\x1bworld", `"hello\eworld"`},
		{"backslash", `hello\world`, `"hello\\world"`},
		{"double quote", `say "hello"`, `"say \"hello\""`},
		{"newline", "hello\nworld", `"hello\nworld"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.TrimSpace(string(data))
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMarshalIndent(t *testing.T) {
	type Inner struct {
		Key string `yaml:"key"`
	}
	type Outer struct {
		Inner Inner `yaml:"inner"`
	}
	data, err := MarshalWithOptions(Outer{Inner: Inner{Key: "val"}}, WithIndent(4))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "    key:") {
		t.Errorf("expected 4-space indent, got:\n%s", data)
	}
}

func TestEncodeNode(t *testing.T) {
	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "key"},
			{kind: nodeScalar, value: "value"},
		},
	}
	enc := newEncoder(&encoderOptions{indent: 2})
	data, err := enc.encodeNode(n)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "key: value") {
		t.Errorf("expected 'key: value', got %q", s)
	}
}

func TestEmitNodeSequence(t *testing.T) {
	n := &node{
		kind: nodeSequence,
		children: []*node{
			{kind: nodeScalar, value: "a"},
			{kind: nodeScalar, value: "b"},
		},
	}
	enc := newEncoder(&encoderOptions{indent: 2})
	data, err := enc.encodeNode(n)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "- a") || !strings.Contains(s, "- b") {
		t.Errorf("expected sequence items, got %q", s)
	}
}

func TestEmitNodeDocument(t *testing.T) {
	n := &node{
		kind: nodeDocument,
		children: []*node{
			{kind: nodeScalar, value: "hello"},
		},
	}
	enc := newEncoder(&encoderOptions{indent: 2})
	data, err := enc.encodeNode(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Errorf("expected hello, got %q", data)
	}
}

func TestEmitNodeNestedMapping(t *testing.T) {
	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "outer"},
			{
				kind: nodeMapping,
				children: []*node{
					{kind: nodeScalar, value: "inner"},
					{kind: nodeScalar, value: "val"},
				},
			},
		},
	}
	enc := newEncoder(&encoderOptions{indent: 2})
	data, err := enc.encodeNode(n)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "outer:") || !strings.Contains(s, "inner: val") {
		t.Errorf("expected nested mapping, got %q", s)
	}
}

func TestEmitNodeNestedSequence(t *testing.T) {
	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "items"},
			{
				kind: nodeSequence,
				children: []*node{
					{kind: nodeScalar, value: "a"},
					{kind: nodeScalar, value: "b"},
				},
			},
		},
	}
	enc := newEncoder(&encoderOptions{indent: 2})
	data, err := enc.encodeNode(n)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "items:") || !strings.Contains(s, "- a") {
		t.Errorf("expected nested sequence, got %q", s)
	}
}

func TestNeedsQuoting(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"null", true},
		{"Null", true},
		{"NULL", true},
		{"~", true},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"false", true},
		{"False", true},
		{"FALSE", true},
		{"123", true},
		{"-17", true},
		{"3.14", true},
		{"1.5e10", true},
		{"-start", true},
		{".start", true},
		{" leading", true},
		{"trailing ", true},
		{"has:colon", true},
		{"has\nnewline", true},
		{"has\ttab", true},
		{"has#hash", true},
		{"has{brace", true},
		{"has[bracket", true},
		{"has|pipe", true},
		{"has>angle", true},
		{"has&amp", true},
		{"has*star", true},
		{"has!bang", true},
		{"has%pct", true},
		{"has@at", true},
		{"has`tick", true},
		{"has,comma", true},
		{"has?question", true},
		{`has"quote`, true},
		{`has'quote`, true},
		{`has\backslash`, true},
		{"has\x01control", true},
		{"has\x7fdelete", true},
		{"hello", false},
		{"simple_value", false},
		{"CamelCase", false},
		{"with123numbers", false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.input), func(t *testing.T) {
			got := needsQuoting(tt.input)
			if got != tt.want {
				t.Errorf("needsQuoting(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsCompound(t *testing.T) {
	tests := []struct {
		name string
		v    reflect.Value
		want bool
	}{
		{"invalid", reflect.Value{}, false},
		{"string", reflect.ValueOf("hello"), false},
		{"int", reflect.ValueOf(42), false},
		{"bool", reflect.ValueOf(true), false},
		{"nil pointer", reflect.ValueOf((*int)(nil)), false},
		{"non-nil pointer to struct", reflect.ValueOf(&struct{ X int }{X: 1}), true},
		{"nil interface", reflect.ValueOf((*any)(nil)).Elem(), false},
		{"nil map", reflect.ValueOf(map[string]int(nil)), false},
		{"empty map", reflect.ValueOf(map[string]int{}), false},
		{"non-empty map", reflect.ValueOf(map[string]int{"a": 1}), true},
		{"nil slice", reflect.ValueOf([]int(nil)), false},
		{"empty slice", reflect.ValueOf([]int{}), false},
		{"non-empty slice", reflect.ValueOf([]int{1}), true},
		{"struct", reflect.ValueOf(struct{ X int }{X: 1}), true},
		{"empty array", reflect.ValueOf([0]int{}), false},
		{"non-empty array", reflect.ValueOf([1]int{1}), true},
		{"time.Time", reflect.ValueOf(time.Now()), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCompound(tt.v)
			if got != tt.want {
				t.Errorf("isCompound(%v) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		name string
		v    reflect.Value
		want bool
	}{
		{"empty string", reflect.ValueOf(""), true},
		{"non-empty string", reflect.ValueOf("hello"), false},
		{"false bool", reflect.ValueOf(false), true},
		{"true bool", reflect.ValueOf(true), false},
		{"zero int", reflect.ValueOf(0), true},
		{"non-zero int", reflect.ValueOf(1), false},
		{"zero int8", reflect.ValueOf(int8(0)), true},
		{"zero int16", reflect.ValueOf(int16(0)), true},
		{"zero int32", reflect.ValueOf(int32(0)), true},
		{"zero int64", reflect.ValueOf(int64(0)), true},
		{"zero uint", reflect.ValueOf(uint(0)), true},
		{"non-zero uint", reflect.ValueOf(uint(1)), false},
		{"zero uint8", reflect.ValueOf(uint8(0)), true},
		{"zero uint16", reflect.ValueOf(uint16(0)), true},
		{"zero uint32", reflect.ValueOf(uint32(0)), true},
		{"zero uint64", reflect.ValueOf(uint64(0)), true},
		{"zero float32", reflect.ValueOf(float32(0)), true},
		{"zero float64", reflect.ValueOf(float64(0)), true},
		{"non-zero float", reflect.ValueOf(1.5), false},
		{"nil slice", reflect.ValueOf([]int(nil)), true},
		{"empty slice", reflect.ValueOf([]int{}), true},
		{"non-empty slice", reflect.ValueOf([]int{1}), false},
		{"nil map", reflect.ValueOf(map[string]int(nil)), true},
		{"empty map", reflect.ValueOf(map[string]int{}), true},
		{"non-empty map", reflect.ValueOf(map[string]int{"a": 1}), false},
		{"nil pointer", reflect.ValueOf((*int)(nil)), true},
		{"zero time", reflect.ValueOf(time.Time{}), true},
		{"non-zero time", reflect.ValueOf(time.Now()), false},
		{"non-time struct", reflect.ValueOf(struct{ X int }{}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEmpty(tt.v)
			if got != tt.want {
				t.Errorf("isEmpty(%v) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestMarshalChannel(t *testing.T) {
	ch := make(chan int)
	data, err := Marshal(ch)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) == "" {
		t.Error("expected some output for channel type")
	}
}

func TestMarshalMapSlice(t *testing.T) {
	ms := MapSlice{
		{Key: "z", Value: 1},
		{Key: "a", Value: 2},
	}
	out, err := Marshal(ms)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	zIdx := strings.Index(s, "z:")
	aIdx := strings.Index(s, "a:")
	if zIdx > aIdx {
		t.Errorf("MapSlice should preserve insertion order, got %q", s)
	}
}

func TestMarshalInterfaceValues(t *testing.T) {
	m := map[string]any{
		"name":   "test",
		"count":  42,
		"active": true,
		"empty":  nil,
	}
	data, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "test" {
		t.Errorf("expected name=test, got %v", out["name"])
	}
}

func TestMarshalNestedPointerStruct(t *testing.T) {
	type Inner struct {
		X int `yaml:"x"`
	}
	type Outer struct {
		Inner *Inner `yaml:"inner"`
	}
	v := Outer{Inner: &Inner{X: 42}}
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "inner:") {
		t.Errorf("expected nested struct output, got:\n%s", data)
	}
}

func TestMarshalInterfaceMapValue(t *testing.T) {
	type Outer struct {
		Val any `yaml:"val"`
	}
	inner := map[string]int{"x": 1}
	v := Outer{Val: inner}
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "x: 1") {
		t.Errorf("expected nested map output, got:\n%s", data)
	}
}

func TestMarshalEmptyCollections(t *testing.T) {
	type Config struct {
		EmptyMap   map[string]string `yaml:"emptyMap"`
		EmptySlice []string          `yaml:"emptySlice"`
	}
	data, err := Marshal(Config{
		EmptyMap:   map[string]string{},
		EmptySlice: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "emptyMap: {}") {
		t.Errorf("expected emptyMap: {}, got:\n%s", s)
	}
	if !strings.Contains(s, "emptySlice: []") {
		t.Errorf("expected emptySlice: [], got:\n%s", s)
	}
}

func TestMarshalNilMapAndSlice(t *testing.T) {
	type Config struct {
		Map   map[string]string `yaml:"map"`
		Slice []string          `yaml:"slice"`
	}
	data, err := Marshal(Config{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "map: null") || !strings.Contains(s, "slice: null") {
		t.Errorf("expected null values, got:\n%s", s)
	}
}

func TestMarshalWithOmitEmptyOption(t *testing.T) {
	m := map[string]string{"key": "val", "empty": ""}
	data, err := MarshalWithOptions(m, WithOmitEmpty(true))
	if err != nil {
		t.Fatal(err)
	}
	_ = string(data)
}

func TestMarshalValuesNeedingQuoting(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", `""`},
		{"null", `"null"`},
		{"true", `"true"`},
		{"false", `"false"`},
		{"123", `"123"`},
		{"1.5", `"1.5"`},
		{"-start", `"-start"`},
		{".start", `".start"`},
		{" leading", `" leading"`},
		{"trailing ", `"trailing "`},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			data, err := Marshal(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.TrimSpace(string(data))
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMarshalQuotedStringsRoundTrip(t *testing.T) {
	tests := map[string]string{
		"plain":      "hello",
		"with_colon": "a: b",
		"newline":    "line1\nline2",
		"tab":        "col1\tcol2",
		"empty":      "",
		"true_str":   "true",
		"null_str":   "null",
	}
	for name, val := range tests {
		t.Run(name, func(t *testing.T) {
			m := map[string]string{"v": val}
			data, err := Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var out map[string]string
			if err := Unmarshal(data, &out); err != nil {
				t.Fatal(err)
			}
			if out["v"] != val {
				t.Errorf("round-trip failed: expected %q, got %q", val, out["v"])
			}
		})
	}
}

func TestMarshalFlowOption(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	data, err := MarshalWithOptions(m, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "{") {
		t.Errorf("expected flow mapping, got:\n%s", data)
	}
}

func TestMarshalLineWidthOption(t *testing.T) {
	out, err := MarshalWithOptions(map[string]string{"key": "value"}, WithLineWidth(120))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "key:") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestMarshalWithCommentLine(t *testing.T) {
	v := map[string]int{"name": 1}
	out, err := MarshalWithOptions(v, WithComment(map[string][]Comment{
		"$.name": {{Position: LineCommentPos, Text: "the name"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "# the name") {
		t.Errorf("expected line comment, got:\n%s", out)
	}
}

func TestMarshalWithCommentHead(t *testing.T) {
	v := map[string]string{"name": "alice"}
	out, err := MarshalWithOptions(v, WithComment(map[string][]Comment{
		"$.name": {{Position: HeadCommentPos, Text: "user name"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "# user name") {
		t.Errorf("expected head comment, got:\n%s", s)
	}
	headIdx := strings.Index(s, "# user name")
	nameIdx := strings.Index(s, "name:")
	if headIdx > nameIdx {
		t.Error("head comment should appear before key")
	}
}

func TestMarshalWithCommentFoot(t *testing.T) {
	v := map[string]string{"name": "alice"}
	out, err := MarshalWithOptions(v, WithComment(map[string][]Comment{
		"$.name": {{Position: FootCommentPos, Text: "end of name"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "# end of name") {
		t.Errorf("expected foot comment, got:\n%s", s)
	}
	footIdx := strings.Index(s, "# end of name")
	nameIdx := strings.Index(s, "name:")
	if footIdx < nameIdx {
		t.Error("foot comment should appear after key")
	}
}

func TestMarshalRoundTripStruct(t *testing.T) {
	type Port struct {
		Name          string `yaml:"name"`
		ContainerPort int    `yaml:"containerPort"`
	}
	type Container struct {
		Name  string `yaml:"name"`
		Image string `yaml:"image"`
		Ports []Port `yaml:"ports"`
	}
	original := Container{
		Name:  "webapp",
		Image: "nginx:latest",
		Ports: []Port{{Name: "http", ContainerPort: 80}},
	}
	data, err := Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Container
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Errorf("round-trip failed:\noriginal: %+v\ndecoded:  %+v", original, decoded)
	}
}

func TestMarshalRoundTripMap(t *testing.T) {
	original := map[string]any{
		"name":    "test",
		"count":   int64(42),
		"enabled": true,
		"tags":    []any{"a", "b", "c"},
	}
	data, err := Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["name"] != "test" {
		t.Errorf("expected name=test, got %v", decoded["name"])
	}
}

func TestMarshalRoundTripNumericTypes(t *testing.T) {
	type Nums struct {
		I8  int8    `yaml:"i8"`
		I16 int16   `yaml:"i16"`
		I32 int32   `yaml:"i32"`
		I64 int64   `yaml:"i64"`
		U8  uint8   `yaml:"u8"`
		U16 uint16  `yaml:"u16"`
		U32 uint32  `yaml:"u32"`
		U64 uint64  `yaml:"u64"`
		F32 float32 `yaml:"f32"`
		F64 float64 `yaml:"f64"`
	}
	original := Nums{I8: -1, I16: -2, I32: -3, I64: -4, U8: 1, U16: 2, U32: 3, U64: 4, F32: 1.5, F64: 2.5}
	data, err := Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Nums
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Errorf("round-trip failed:\noriginal: %+v\ndecoded:  %+v", original, decoded)
	}
}

func TestMarshalRoundTripByteSlice(t *testing.T) {
	type Config struct {
		Data []byte `yaml:"data"`
	}
	original := Config{Data: []byte("hello world")}
	data, err := Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Config
	if err := Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded.Data) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(decoded.Data))
	}
}

func TestMarshalRoundTripBigInt(t *testing.T) {
	v := struct {
		V *big.Int `yaml:"v"`
	}{V: big.NewInt(42)}
	data, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		V *big.Int `yaml:"v"`
	}
	out.V = new(big.Int)
	if err := Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.V.Cmp(v.V) != 0 {
		t.Errorf("round-trip: expected %s, got %s", v.V, out.V)
	}
}

func TestMarshalRoundTripBinary(t *testing.T) {
	original := []byte{0, 1, 2, 255, 254, 253}
	v := struct {
		Data []byte `yaml:"data"`
	}{Data: original}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var v2 struct {
		Data []byte `yaml:"data"`
	}
	if err := Unmarshal(out, &v2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v2.Data, original) {
		t.Errorf("round-trip failed: %v != %v", v2.Data, original)
	}
}

func TestMarshalConcurrent(t *testing.T) {
	type Data struct {
		Name  string `yaml:"name"`
		Value int    `yaml:"value"`
	}
	done := make(chan bool, 20)
	for i := range 20 {
		go func(idx int) {
			defer func() { done <- true }()
			d := Data{Name: "test", Value: idx}
			data, err := Marshal(d)
			if err != nil {
				t.Errorf("marshal error: %v", err)
				return
			}
			var out Data
			if err := Unmarshal(data, &out); err != nil {
				t.Errorf("unmarshal error: %v", err)
				return
			}
			if out.Name != "test" || out.Value != idx {
				t.Errorf("round-trip mismatch: got %+v", out)
			}
		}(i)
	}
	for range 20 {
		<-done
	}
}

func TestEncoderWithIndentOption(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf, WithIndent(4))
	type Inner struct {
		Key string `yaml:"key"`
	}
	type Outer struct {
		Inner Inner `yaml:"inner"`
	}
	if err := enc.Encode(Outer{Inner: Inner{Key: "val"}}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "    key:") {
		t.Errorf("expected 4-space indent in output:\n%s", buf.String())
	}
}

func TestEncodeContextBasic(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.EncodeContext(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "test") {
		t.Errorf("expected 'test', got %q", buf.String())
	}
}

func TestMarshalEmptySliceInStruct(t *testing.T) {
	v := struct {
		Items []int `yaml:"items"`
	}{Items: []int{}}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "[]") {
		t.Errorf("expected empty sequence, got %q", out)
	}
}

func TestMarshalEmptyMapInStruct(t *testing.T) {
	v := struct {
		Data map[string]string `yaml:"data"`
	}{Data: map[string]string{}}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "{}") {
		t.Errorf("expected empty mapping, got %q", out)
	}
}

type encodeErrorMarshaler struct{}

func (e encodeErrorMarshaler) MarshalYAML() (any, error) {
	return nil, fmt.Errorf("intentional error")
}

func TestMarshalSliceErrorPropagation(t *testing.T) {
	v := []encodeErrorMarshaler{{}}
	_, err := Marshal(v)
	if err == nil {
		t.Error("expected error from slice element marshal")
	}
}

func TestMarshalFlowSequenceErrorPropagation(t *testing.T) {
	v := []encodeErrorMarshaler{{}}
	_, err := MarshalWithOptions(v, WithFlow(true))
	if err == nil {
		t.Error("expected error from flow sequence element marshal")
	}
}

func TestMarshalMapErrorPropagation(t *testing.T) {
	m := map[string]encodeErrorMarshaler{"key": {}}
	_, err := Marshal(m)
	if err == nil {
		t.Error("expected error from map value marshal")
	}
}

func TestMarshalFlowMappingErrorPropagation(t *testing.T) {
	m := map[string]encodeErrorMarshaler{"key": {}}
	_, err := MarshalWithOptions(m, WithFlow(true))
	if err == nil {
		t.Error("expected error from flow mapping value marshal")
	}
}

func TestMarshalStructErrorPropagation(t *testing.T) {
	type S struct {
		Field encodeErrorMarshaler `yaml:"field"`
	}
	_, err := Marshal(S{})
	if err == nil {
		t.Error("expected error from struct field marshal")
	}
}

func TestMarshalFlowStructErrorPropagation(t *testing.T) {
	type S struct {
		Field encodeErrorMarshaler `yaml:"field"`
	}
	_, err := MarshalWithOptions(S{}, WithFlow(true))
	if err == nil {
		t.Error("expected error from flow struct field marshal")
	}
}

func TestMarshalStructInlineMapErrorPropagation(t *testing.T) {
	type S struct {
		Extra map[string]encodeErrorMarshaler `yaml:",inline"`
	}
	v := S{Extra: map[string]encodeErrorMarshaler{"key": {}}}
	_, err := Marshal(v)
	if err == nil {
		t.Error("expected error from inline map value marshal")
	}
}

func TestMarshalStructFlowFieldErrorPropagation(t *testing.T) {
	type S struct {
		Field encodeErrorMarshaler `yaml:"field,flow"`
	}
	_, err := Marshal(S{})
	if err == nil {
		t.Error("expected error from flow field marshal")
	}
}

func TestMarshalMapKeyError(t *testing.T) {
	m := map[encodeErrorMarshaler]string{{}: "val"}
	_, err := Marshal(m)
	if err == nil {
		t.Error("expected error from map key marshal")
	}
}

func TestMarshalFlowMapKeyError(t *testing.T) {
	m := map[encodeErrorMarshaler]string{{}: "val"}
	_, err := MarshalWithOptions(m, WithFlow(true))
	if err == nil {
		t.Error("expected error from flow map key marshal")
	}
}

func TestMarshalCustomMarshalerError(t *testing.T) {
	type Color struct{ R uint8 }
	_, err := MarshalWithOptions(Color{R: 1}, WithCustomMarshaler(func(c Color) ([]byte, error) {
		return nil, fmt.Errorf("custom error")
	}))
	if err == nil {
		t.Error("expected custom marshaler error")
	}
}

func TestIsEmptyInterface(t *testing.T) {
	var iface any
	v := reflect.ValueOf(&iface).Elem()
	if !isEmpty(v) {
		t.Error("expected nil interface to be empty")
	}
	iface = 42
	v = reflect.ValueOf(&iface).Elem()
	if isEmpty(v) {
		t.Error("expected non-nil interface to not be empty")
	}
}

func TestPathToKeyEmpty(t *testing.T) {
	got := pathToKey("$.")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

type encodeTextMarshalerError struct{}

func (e encodeTextMarshalerError) MarshalText() ([]byte, error) {
	return nil, fmt.Errorf("text marshal error")
}

func TestMarshalTextMarshalerReturnsError(t *testing.T) {
	_, err := Marshal(encodeTextMarshalerError{})
	if err == nil {
		t.Error("expected error from TextMarshaler")
	}
}

func TestMarshalStructCompoundFieldError(t *testing.T) {
	type Inner struct {
		Field encodeErrorMarshaler `yaml:"field"`
	}
	type Outer struct {
		Inner Inner `yaml:"inner"`
	}
	_, err := Marshal(Outer{})
	if err == nil {
		t.Error("expected error from compound struct field")
	}
}

func TestMarshalMapCompoundValueError(t *testing.T) {
	type Inner struct {
		Field encodeErrorMarshaler `yaml:"field"`
	}
	m := map[string]Inner{"key": {}}
	_, err := Marshal(m)
	if err == nil {
		t.Error("expected error from compound map value")
	}
}

func TestInsertHeadCommentEmptyKey(t *testing.T) {
	buf := insertHeadComment([]byte("key: val\n"), "$", "comment")
	if strings.Contains(string(buf), "comment") {
		t.Error("expected no comment inserted for root path")
	}
}

func TestInsertLineCommentEmptyKey(t *testing.T) {
	buf := insertLineComment([]byte("key: val\n"), "$", "comment")
	if strings.Contains(string(buf), "comment") {
		t.Error("expected no comment inserted for root path")
	}
}

func TestInsertFootCommentEmptyKey(t *testing.T) {
	buf := insertFootComment([]byte("key: val\n"), "$", "comment")
	if strings.Contains(string(buf), "comment") {
		t.Error("expected no comment inserted for root path")
	}
}

type ptrMarshaler struct {
	val string
}

func (p *ptrMarshaler) MarshalYAML() (any, error) {
	return p.val, nil
}

func TestMarshalPtrMarshaler(t *testing.T) {
	type S struct {
		M ptrMarshaler `yaml:"m"`
	}
	s := &S{M: ptrMarshaler{val: "hello"}}
	data, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Errorf("expected hello in output, got %q", data)
	}
}

type ptrMarshalerError struct{}

func (p *ptrMarshalerError) MarshalYAML() (any, error) {
	return nil, fmt.Errorf("ptr marshal error")
}

func TestMarshalPtrMarshalerError(t *testing.T) {
	type S struct {
		M ptrMarshalerError `yaml:"m"`
	}
	_, err := Marshal(&S{M: ptrMarshalerError{}})
	if err == nil {
		t.Fatal("expected error from pointer marshaler")
	}
}

type ptrBytesMarshaler struct {
	val string
}

func (p *ptrBytesMarshaler) MarshalYAML() ([]byte, error) {
	return []byte(p.val), nil
}

func TestMarshalPtrBytesMarshaler(t *testing.T) {
	type S struct {
		M ptrBytesMarshaler `yaml:"m"`
	}
	data, err := Marshal(&S{M: ptrBytesMarshaler{val: "raw"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "raw") {
		t.Errorf("expected raw in output, got %q", data)
	}
}

type ptrBytesError struct{}

func (p *ptrBytesError) MarshalYAML() ([]byte, error) {
	return nil, fmt.Errorf("ptr bytes error")
}

func TestMarshalPtrBytesMarshalerError(t *testing.T) {
	type S struct {
		M ptrBytesError `yaml:"m"`
	}
	_, err := Marshal(&S{M: ptrBytesError{}})
	if err == nil {
		t.Fatal("expected error from pointer bytes marshaler")
	}
}

type ptrTextMarshaler struct {
	val string
}

func (p *ptrTextMarshaler) MarshalText() ([]byte, error) {
	return []byte(p.val), nil
}

func TestMarshalPtrTextMarshaler(t *testing.T) {
	type S struct {
		M ptrTextMarshaler `yaml:"m"`
	}
	data, err := Marshal(&S{M: ptrTextMarshaler{val: "text"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "text") {
		t.Errorf("expected text in output, got %q", data)
	}
}

type ptrTextError struct{}

func (p *ptrTextError) MarshalText() ([]byte, error) {
	return nil, fmt.Errorf("text marshal error")
}

func TestMarshalPtrTextMarshalerError(t *testing.T) {
	type S struct {
		M ptrTextError `yaml:"m"`
	}
	_, err := Marshal(&S{M: ptrTextError{}})
	if err == nil {
		t.Fatal("expected error from pointer text marshaler")
	}
}

type errorInner struct{}

func (e errorInner) MarshalYAML() (any, error) {
	return nil, fmt.Errorf("inner error")
}

func TestMarshalSliceElementError(t *testing.T) {
	_, err := Marshal([]errorInner{{}})
	if err == nil {
		t.Fatal("expected error from slice element marshal")
	}
}

func TestMarshalMapValueError(t *testing.T) {
	_, err := Marshal(map[string]errorInner{"k": {}})
	if err == nil {
		t.Fatal("expected error from map value marshal")
	}
}

func TestMarshalStructInlineMapError(t *testing.T) {
	type S struct {
		Extra map[string]errorInner `yaml:",inline"`
	}
	_, err := Marshal(S{Extra: map[string]errorInner{"k": {}}})
	if err == nil {
		t.Fatal("expected error from inline map value marshal")
	}
}

func TestMarshalStructFieldError(t *testing.T) {
	type S struct {
		F errorInner `yaml:"f"`
	}
	_, err := Marshal(S{F: errorInner{}})
	if err == nil {
		t.Fatal("expected error from struct field marshal")
	}
}

func TestIsEmptyUnknownKind(t *testing.T) {
	ch := make(chan int)
	result := isEmpty(reflect.ValueOf(ch))
	if result {
		t.Error("expected false for chan kind")
	}
}

type valTextMarshaler string

func (v valTextMarshaler) MarshalText() ([]byte, error) {
	return []byte("text:" + string(v)), nil
}

func TestMarshalValueTextMarshaler(t *testing.T) {
	data, err := Marshal(valTextMarshaler("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "text:hello") {
		t.Errorf("expected text:hello, got %q", data)
	}
}

type valTextMarshalerError string

func (v valTextMarshalerError) MarshalText() ([]byte, error) {
	return nil, fmt.Errorf("text marshal error")
}

func TestMarshalValueTextMarshalerError(t *testing.T) {
	_, err := Marshal(valTextMarshalerError("x"))
	if err == nil {
		t.Fatal("expected error from value text marshaler")
	}
}

type errScalar string

func (e errScalar) MarshalYAML() (any, error) {
	return nil, fmt.Errorf("scalar error")
}

func TestMarshalSliceScalarElementError(t *testing.T) {
	_, err := Marshal([]errScalar{"x"})
	if err == nil {
		t.Fatal("expected error from scalar slice element")
	}
}

func TestMarshalMapScalarValueError(t *testing.T) {
	_, err := Marshal(map[string]errScalar{"k": "v"})
	if err == nil {
		t.Fatal("expected error from scalar map value")
	}
}

func TestMarshalStructInlineMapCompoundError(t *testing.T) {
	type S struct {
		Extra map[string]errorInner `yaml:",inline"`
	}
	_, err := Marshal(S{Extra: map[string]errorInner{"k": {}}})
	if err == nil {
		t.Fatal("expected error from inline compound map value")
	}
}

func TestMarshalStructFieldCompoundError(t *testing.T) {
	type Inner struct {
		Bad errorInner `yaml:"bad"`
	}
	type Outer struct {
		F Inner `yaml:"f"`
	}
	_, err := Marshal(Outer{F: Inner{Bad: errorInner{}}})
	if err == nil {
		t.Fatal("expected error from compound struct field")
	}
}

func TestMarshalStructCompoundFieldErrorNested(t *testing.T) {
	type Inner struct {
		Bad errorInner `yaml:"bad"`
	}
	type Outer struct {
		Inner Inner `yaml:"inner"`
	}
	_, err := Marshal(Outer{Inner: Inner{Bad: errorInner{}}})
	if err == nil {
		t.Fatal("expected error from nested struct field marshal")
	}
}

func TestInsertCommentEmptyPath(t *testing.T) {
	type S struct {
		Key string `yaml:"key"`
	}
	comments := map[string][]Comment{
		"": {
			{Position: HeadCommentPos, Text: "head"},
			{Position: LineCommentPos, Text: "line"},
			{Position: FootCommentPos, Text: "foot"},
		},
	}
	out, err := MarshalWithOptions(S{Key: "val"}, WithComment(comments))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "key: val") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestMarshalStructInlineMapNonCompoundError(t *testing.T) {
	type S struct {
		Extra map[string]errScalar `yaml:",inline"`
	}
	_, err := Marshal(S{Extra: map[string]errScalar{"k": "v"}})
	if err == nil {
		t.Fatal("expected error from inline map non-compound value marshal")
	}
}

func TestMarshalStructFieldNonCompoundError(t *testing.T) {
	type S struct {
		Bad errScalar `yaml:"bad"`
	}
	_, err := Marshal(S{Bad: "x"})
	if err == nil {
		t.Fatal("expected error from non-compound struct field marshal")
	}
}

func TestMarshalBigIntValue(t *testing.T) {
	bi, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	data, err := Marshal(*bi)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "123456789012345678901234567890") {
		t.Errorf("expected big.Int value in output, got:\n%s", data)
	}
}

func TestMarshalBigFloatValue(t *testing.T) {
	bf, _, _ := big.ParseFloat("3.14159265358979323846", 10, 256, big.ToNearestEven)
	data, err := Marshal(*bf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "3.14159") {
		t.Errorf("expected big.Float value in output, got:\n%s", data)
	}
}

func TestMarshalBigRatValue(t *testing.T) {
	br := new(big.Rat).SetFrac64(1, 3)
	data, err := Marshal(*br)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "1/3") {
		t.Errorf("expected big.Rat value in output, got:\n%s", data)
	}
}

func TestMarshalBigOmitemptyZero(t *testing.T) {
	type S struct {
		I big.Int   `yaml:"i,omitempty"`
		F big.Float `yaml:"f,omitempty"`
		R big.Rat   `yaml:"r,omitempty"`
	}
	data, err := Marshal(S{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "i:") || strings.Contains(s, "f:") || strings.Contains(s, "r:") {
		t.Errorf("expected zero big values to be omitted, got:\n%s", s)
	}
}

func TestMarshalBigOmitemptyNonZero(t *testing.T) {
	type S struct {
		I big.Int   `yaml:"i,omitempty"`
		F big.Float `yaml:"f,omitempty"`
		R big.Rat   `yaml:"r,omitempty"`
	}
	s := S{}
	s.I.SetInt64(42)
	s.F.SetFloat64(3.14)
	s.R.SetFrac64(1, 2)
	data, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "i:") {
		t.Errorf("expected big.Int in output, got:\n%s", out)
	}
	if !strings.Contains(out, "f:") {
		t.Errorf("expected big.Float in output, got:\n%s", out)
	}
	if !strings.Contains(out, "r:") {
		t.Errorf("expected big.Rat in output, got:\n%s", out)
	}
}

func TestInsertHeadCommentIndentCalc(t *testing.T) {
	buf := []byte("  name: foo\n  age: 30\n")
	result := insertHeadComment(buf, "$.name", "about name")
	s := string(result)
	if !strings.Contains(s, "  # about name\n  name: foo") {
		t.Errorf("head comment should match key indentation, got:\n%s", s)
	}
}

func TestInsertFootCommentIndentCalc(t *testing.T) {
	buf := []byte("  name: foo\n  age: 30\n")
	result := insertFootComment(buf, "$.name", "after name")
	s := string(result)
	if !strings.Contains(s, "  name: foo\n  # after name") {
		t.Errorf("foot comment should match key indentation, got:\n%s", s)
	}
}

func TestMarshalCustomMarshalerNilMap(t *testing.T) {
	type Foo struct {
		Name string `yaml:"name"`
	}
	f := Foo{Name: "test"}
	data, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: test") {
		t.Errorf("expected 'name: test', got:\n%s", string(data))
	}
}

func TestMarshalSequenceNewlineBefore(t *testing.T) {
	type S struct {
		Items []string `yaml:"items"`
	}
	s := S{Items: []string{"a", "b", "c"}}
	data, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if strings.Count(out, "- ") != 3 {
		t.Errorf("expected 3 sequence items, got:\n%s", out)
	}
	if !strings.Contains(out, "- a") || !strings.Contains(out, "- b") || !strings.Contains(out, "- c") {
		t.Errorf("missing sequence items, got:\n%s", out)
	}
}

func TestMarshalSequenceSingleElement(t *testing.T) {
	type S struct {
		Items []string `yaml:"items"`
	}
	s := S{Items: []string{"only"}}
	data, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- only") {
		t.Errorf("expected '- only', got:\n%s", string(data))
	}
}

func TestMarshalInlineMapNewlineBefore(t *testing.T) {
	type S struct {
		Name  string         `yaml:"name"`
		Extra map[string]any `yaml:",inline"`
	}
	s := S{
		Name:  "test",
		Extra: map[string]any{"x": "1", "y": "2"},
	}
	data, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), string(data))
	}
}

func TestMarshalFlowStructOmitEmptyField(t *testing.T) {
	type S struct {
		A string `yaml:"a"`
		B string `yaml:"b,omitempty"`
		C string `yaml:"c"`
	}
	s := S{A: "1", B: "", C: "3"}
	data, err := MarshalWithOptions(s, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if strings.Contains(out, "b:") {
		t.Errorf("omitempty field should be excluded in flow mode, got: %s", out)
	}
	if !strings.Contains(out, "a:") || !strings.Contains(out, "c:") {
		t.Errorf("non-empty fields should be present, got: %s", out)
	}
}

func TestMarshalFlowStructAllFields(t *testing.T) {
	type S struct {
		A string `yaml:"a"`
		B string `yaml:"b,omitempty"`
		C string `yaml:"c"`
	}
	s := S{A: "1", B: "2", C: "3"}
	data, err := MarshalWithOptions(s, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "a:") || !strings.Contains(out, "b:") || !strings.Contains(out, "c:") {
		t.Errorf("all fields should be present, got: %s", out)
	}
}

func TestMarshalLiteralScalarIndent(t *testing.T) {
	m := map[string]any{"text": "line1\nline2\nline3\n"}
	data, err := MarshalWithOptions(m, WithLiteralStyle(true))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	lines := strings.Split(s, "\n")
	indented := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "" {
			indented++
		}
	}
	if indented < 3 {
		t.Errorf("expected at least 3 indented content lines, got %d:\n%s", indented, s)
	}
}

func TestMarshalLiteralScalarCustomIndent(t *testing.T) {
	m := map[string]any{"text": "line1\nline2\n"}
	data, err := MarshalWithOptions(m, WithLiteralStyle(true), WithIndent(4))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "    line1") {
		t.Errorf("expected 4-space indent for literal content, got:\n%s", s)
	}
}

func TestInsertHeadCommentArithmetic(t *testing.T) {
	buf := []byte("    deep:\n      key: val\n")
	result := insertHeadComment(buf, "$.deep", "test comment")
	s := string(result)
	if !strings.Contains(s, "    # test comment\n    deep:") {
		t.Errorf("head comment indent should be 4 spaces, got:\n%s", s)
	}
}

func TestInsertFootCommentArithmetic(t *testing.T) {
	buf := []byte("    deep:\n      key: val\n")
	result := insertFootComment(buf, "$.deep", "test comment")
	s := string(result)
	if !strings.Contains(s, "    deep:\n    # test comment") {
		t.Errorf("foot comment indent should be 4 spaces, got:\n%s", s)
	}
}

func TestMarshalMapMultipleKeys(t *testing.T) {
	m := map[string]string{"a": "1", "b": "2", "c": "3"}
	data, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Count(s, ": ") != 3 {
		t.Errorf("expected 3 key-value pairs:\n%s", s)
	}
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d:\n%s", len(lines), s)
	}
}

func TestMarshalMapNewlinesBetweenEntries(t *testing.T) {
	m := map[string]int{"x": 1, "y": 2}
	data, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.TrimSpace(string(data))
	lines := strings.Split(s, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), s)
	}
}

func TestMarshalStructMultipleFields(t *testing.T) {
	type S struct {
		A string `yaml:"a"`
		B int    `yaml:"b"`
		C bool   `yaml:"c"`
	}
	s := S{A: "hello", B: 42, C: true}
	data, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(out, "a: hello") {
		t.Errorf("missing field a:\n%s", out)
	}
	if !strings.Contains(out, "b: 42") {
		t.Errorf("missing field b:\n%s", out)
	}
}

func TestMarshalStructInlineMapNewlines(t *testing.T) {
	type S struct {
		Name  string         `yaml:"name"`
		Extra map[string]any `yaml:",inline"`
	}
	s := S{
		Name:  "test",
		Extra: map[string]any{"alpha": "a", "beta": "b", "gamma": "c"},
	}
	data, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (name + 3 inline), got %d:\n%s", len(lines), string(data))
	}
}

func TestMarshalStructInlineMapCompoundIndent(t *testing.T) {
	type S struct {
		Name  string           `yaml:"name"`
		Extra map[string][]int `yaml:",inline"`
	}
	s := S{
		Name:  "test",
		Extra: map[string][]int{"nums": {1, 2, 3}},
	}
	data, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "nums:") {
		t.Errorf("missing inline map key:\n%s", out)
	}
	if !strings.Contains(out, "- 1") {
		t.Errorf("missing sequence items:\n%s", out)
	}
}

func TestMarshalStructFlowFieldIndent(t *testing.T) {
	type S struct {
		Name  string   `yaml:"name"`
		Items []string `yaml:"items,flow"`
	}
	s := S{Name: "test", Items: []string{"a", "b"}}
	data, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "items: [a, b]") {
		t.Errorf("expected flow sequence for items:\n%s", out)
	}
}

func TestMarshalStructCompoundFieldIndent(t *testing.T) {
	type Inner struct {
		X int `yaml:"x"`
	}
	type S struct {
		Name  string `yaml:"name"`
		Inner Inner  `yaml:"inner"`
	}
	s := S{Name: "test", Inner: Inner{X: 1}}
	data, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "inner:\n") {
		t.Errorf("compound field should have newline after colon:\n%s", out)
	}
	if !strings.Contains(out, "  x: 1") {
		t.Errorf("nested field should be indented:\n%s", out)
	}
}

func TestMarshalLiteralScalarLastLineBreak(t *testing.T) {
	m1 := map[string]any{"text": "line1\nline2\n"}
	data1, _ := MarshalWithOptions(m1, WithLiteralStyle(true))
	if !strings.Contains(string(data1), "|") {
		t.Error("trailing newline should use | indicator")
	}

	m2 := map[string]any{"text": "line1\nline2"}
	data2, _ := MarshalWithOptions(m2, WithLiteralStyle(true))
	if !strings.Contains(string(data2), "|-") {
		t.Error("no trailing newline should use |- indicator")
	}
}

func TestMarshalLiteralScalarMultipleLines(t *testing.T) {
	m := map[string]any{"text": "a\nb\nc\nd\n"}
	data, err := MarshalWithOptions(m, WithLiteralStyle(true))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Count(s, "\n") < 4 {
		t.Errorf("expected at least 4 newlines in output:\n%s", s)
	}
}

func TestMarshalEmitNodeMapping(t *testing.T) {
	input := "a: 1\nb: 2\nc: 3\n"
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	out, err := NodeToBytes(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), s)
	}
}

func TestMarshalEmitNodeSequence(t *testing.T) {
	input := "- x\n- y\n- z\n"
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	out, err := NodeToBytes(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Count(s, "- ") != 3 {
		t.Errorf("expected 3 sequence entries:\n%s", s)
	}
}

func TestMarshalEmitNodeNestedMapping(t *testing.T) {
	input := "outer:\n  inner: val\n"
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	out, err := NodeToBytes(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "outer:") || !strings.Contains(s, "inner: val") {
		t.Errorf("nested mapping not preserved:\n%s", s)
	}
}

func TestMarshalFlowMappingSortBoundary(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	data, err := MarshalWithOptions(m, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	aIdx := strings.Index(s, "a:")
	bIdx := strings.Index(s, "b:")
	cIdx := strings.Index(s, "c:")
	if aIdx >= bIdx || bIdx >= cIdx {
		t.Errorf("keys should be sorted: %s", s)
	}
}

func TestMarshalQuoteAll(t *testing.T) {
	type Config struct {
		Name   string            `yaml:"name"`
		Port   int               `yaml:"port"`
		Debug  bool              `yaml:"debug"`
		Tags   []string          `yaml:"tags"`
		Labels map[string]string `yaml:"labels"`
	}

	c := Config{
		Name:   "nginx",
		Port:   8080,
		Debug:  true,
		Tags:   []string{"v1", "prod"},
		Labels: map[string]string{"app": "web"},
	}

	b, err := MarshalWithOptions(c, WithQuoteAllStrings(true))
	if err != nil {
		t.Fatal(err)
	}

	s := string(b)

	if !strings.Contains(s, `name: "nginx"`) {
		t.Errorf("string value should be quoted, got:\n%s", s)
	}
	if !strings.Contains(s, "port: 8080") {
		t.Errorf("int value should not be quoted, got:\n%s", s)
	}
	if !strings.Contains(s, "debug: true") {
		t.Errorf("bool value should not be quoted, got:\n%s", s)
	}
	if strings.Contains(s, `"name":`) || strings.Contains(s, `"labels":`) {
		t.Errorf("struct keys should not be quoted, got:\n%s", s)
	}
	if strings.Contains(s, `"app":`) {
		t.Errorf("map keys should not be quoted, got:\n%s", s)
	}
	if !strings.Contains(s, `- "v1"`) {
		t.Errorf("sequence string elements should be quoted, got:\n%s", s)
	}
}
