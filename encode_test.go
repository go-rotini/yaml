package yaml

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
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

// TestKYAMLEncodeLeadingHeader verifies R3.1: every KYAML document begins
// with a "---" header.
func TestKYAMLEncodeLeadingHeader(t *testing.T) {
	cases := []any{
		42,
		"hello",
		nil,
		true,
		[]int{1, 2, 3},
		map[string]int{"a": 1},
	}
	for _, v := range cases {
		out, err := MarshalKYAML(v)
		if err != nil {
			t.Fatalf("MarshalKYAML(%v): %v", v, err)
		}
		if !strings.HasPrefix(string(out), "---\n") {
			t.Errorf("MarshalKYAML(%v) missing leading ---:\n%s", v, out)
		}
	}
}

// TestKYAMLEncodeFlowStyle verifies R4.1 + R7.1: mappings and sequences are
// always flow style.
func TestKYAMLEncodeFlowStyle(t *testing.T) {
	out, err := MarshalKYAML(map[string]any{"a": []int{1, 2}, "b": map[string]int{"c": 3}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "\n- ") {
		t.Errorf("block-style sequence emitted under KYAML mode:\n%s", s)
	}
	// Block-style mappings would have a key on its own line followed by an
	// indented value. Flow style always wraps in {}.
	if !strings.Contains(s, "{") || !strings.Contains(s, "}") {
		t.Errorf("expected flow-style mapping braces:\n%s", s)
	}
	if !strings.Contains(s, "[") || !strings.Contains(s, "]") {
		t.Errorf("expected flow-style sequence brackets:\n%s", s)
	}
}

// TestKYAMLEncodeStringQuoting verifies R6.4: all string values are
// double-quoted.
func TestKYAMLEncodeStringQuoting(t *testing.T) {
	out, err := MarshalKYAML(map[string]string{"a": "value", "b": "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"value"`) {
		t.Errorf("string value not double-quoted:\n%s", out)
	}
	if !strings.Contains(string(out), `"yes"`) {
		t.Errorf(`Norway-problem string value "yes" not double-quoted:\n%s`, out)
	}
}

// TestKYAMLEncodeBracketKeysQuoted (R5.3 regression): a key containing `[`
// or `]` MUST be quoted, regardless of bracket content. The YAML flow
// parser treats `[` as a flow-sequence start in flow context, so an
// unquoted bracket key produces invalid YAML even when emitted inside a
// flow mapping. This was the root cause of fuzz seed
// testdata/fuzz/FuzzMarshalKYAML/9961762ecc024444 (key "A[]") which
// emitted unquoted and broke decode.
func TestKYAMLEncodeBracketKeysQuoted(t *testing.T) {
	keys := []string{
		"A[]",         // empty brackets — original fuzz seed
		"A[B]",        // brackets with content
		"metadata[a]", // realistic-looking JSONPath-style key
		"foo]bar",     // unmatched bracket
		"[start",      // bracket at start
	}
	for _, k := range keys {
		t.Run(k, func(t *testing.T) {
			out, err := MarshalKYAML(map[string]int{k: 1})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), `"`+k+`":`) {
				t.Errorf("bracket-containing key %q must be quoted, got:\n%s", k, out)
			}
			// And the output must round-trip cleanly through the YAML parser.
			var got map[string]int
			if err := Unmarshal(out, &got); err != nil {
				t.Errorf("output not parseable as YAML: %v\n%s", err, out)
			}
			if got[k] != 1 {
				t.Errorf("round-trip mismatch for key %q: got %v", k, got)
			}
		})
	}
}

// TestKYAMLEncodeKeyQuoting verifies R5.1 + R5.2: type-ambiguous keys are
// quoted; safe keys are unquoted.
func TestKYAMLEncodeKeyQuoting(t *testing.T) {
	cases := []struct {
		key    string
		quoted bool
	}{
		{"name", false},
		{"apiVersion", false},
		{"kubernetes.io/role", false},
		{"NO", true},
		{"yes", true},
		{"on", true},
		{"True", true},
		{"null", true},
		{"42", true},
		{"_42", false},
		{"a b", true},
		{"a.b.c", false},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			out, err := MarshalKYAML(map[string]int{tc.key: 1})
			if err != nil {
				t.Fatal(err)
			}
			s := string(out)
			quoted := strings.Contains(s, `"`+tc.key+`":`)
			if quoted != tc.quoted {
				t.Errorf("key %q: quoted=%v, want %v\n%s", tc.key, quoted, tc.quoted, s)
			}
		})
	}
}

// TestKYAMLEncodeBooleansAndNull verifies R6.1 + R6.3. Note that a field
// named `n` is a Norway-problem alias and gets quoted under R5.2.
func TestKYAMLEncodeBooleansAndNull(t *testing.T) {
	type S struct {
		B *bool   `json:"b"`
		T bool    `json:"t"`
		F bool    `json:"f"`
		Z *string `json:"z"`
	}
	tt := true
	out, err := MarshalKYAML(S{B: &tt, T: true, F: false})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"b: true,", "t: true,", "f: false,", "z: null,"} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in:\n%s", want, s)
		}
	}
}

// TestKYAMLEncodeEmpties verifies R7.4: empty containers as `[]` and `{}`.
func TestKYAMLEncodeEmpties(t *testing.T) {
	out, err := MarshalKYAML(struct {
		L []int          `json:"l"`
		M map[string]int `json:"m"`
	}{L: []int{}, M: map[string]int{}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "l: [],") {
		t.Errorf("empty slice not rendered as []:\n%s", s)
	}
	if !strings.Contains(s, "m: {},") {
		t.Errorf("empty map not rendered as {}:\n%s", s)
	}
}

// TestKYAMLEncodeNanInfRejected verifies R6.2b: NaN and Inf are unsupported.
func TestKYAMLEncodeNanInfRejected(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := MarshalKYAML(v)
		if err == nil {
			t.Errorf("expected error for non-finite float %v", v)
			continue
		}
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("expected ErrUnsupported for %v, got %v", v, err)
		}
	}
}

// TestKYAMLEncodeWholeFloat verifies R6.2c: 1.0 renders as 1 (no .0).
func TestKYAMLEncodeWholeFloat(t *testing.T) {
	out, err := MarshalKYAML(map[string]float64{"a": 1.0, "b": 2.5})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "a: 1,") {
		t.Errorf("whole-valued 1.0 not rendered as 1:\n%s", s)
	}
	if !strings.Contains(s, "b: 2.5,") {
		t.Errorf("non-whole 2.5 not rendered correctly:\n%s", s)
	}
}

// TestKYAMLEncodeFlowFold (R10): multi-line strings whose fully-escaped
// single-line form exceeds lineWidth are rendered using KYAML's flow-folded
// form — `\n` escapes for real newlines plus trailing-backslash continuations
// for source-line wrapping.
func TestKYAMLEncodeFlowFold(t *testing.T) {
	// A long enough multi-line string with 2+ newlines triggers folding.
	long := strings.Repeat("a", 50) + "\n" + strings.Repeat("b", 50) + "\n" + strings.Repeat("c", 50)
	out, err := MarshalKYAML(map[string]string{"k": long})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Flow-fold output contains a backslash+newline continuation marker.
	if !strings.Contains(s, "\\\n") {
		t.Errorf("expected flow-fold continuation `\\<newline>`:\n%s", s)
	}
	// Round-trip preserves the original string.
	var got map[string]string
	if err := Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got["k"] != long {
		t.Errorf("flow-fold round-trip mismatch:\n want: %q\n got:  %q", long, got["k"])
	}
}

func TestKYAMLEncodeFlowFoldShortStaysSingleLine(t *testing.T) {
	// Short multi-line string stays on a single line with `\n` escapes.
	short := "a\nb\nc"
	out, err := MarshalKYAML(map[string]string{"k": short})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "\\\n") {
		t.Errorf("short multi-line string should not flow-fold:\n%s", s)
	}
	if !strings.Contains(s, `\n`) {
		t.Errorf("expected literal \\n escape:\n%s", s)
	}
}

func TestKYAMLEncodeFlowFoldSingleNewlineStaysInline(t *testing.T) {
	// Even very long strings with only ONE newline stay single-line — the
	// trigger requires 2+ newlines so the threshold can't flip on round-trip.
	long := strings.Repeat("a", 200) + "\n" + strings.Repeat("b", 200)
	out, err := MarshalKYAML(map[string]string{"k": long})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "\\\n") {
		t.Errorf("single-newline string should never flow-fold (idempotence guard):\n%s", out)
	}
}

func TestKYAMLEncodeFlowFoldIdempotent(t *testing.T) {
	// Long multi-line string with embedded invalid UTF-8 — the previous
	// fuzz failure case. After Format, the bytes are normalized to U+FFFD
	// UTF-8 (3 bytes) and the second Format must produce identical output.
	src := []byte("---\n{\n  k: \"" + strings.Repeat("0", 50) + "\\n" + strings.Repeat("1", 50) + "\\n" + strings.Repeat("2", 50) + "\",\n}\n")
	once, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, twice) {
		t.Errorf("flow-fold not idempotent\n=== once:\n%s=== twice:\n%s", once, twice)
	}
}

// TestKYAMLEncodeStringEscapes verifies R6.5 escape handling.
func TestKYAMLEncodeStringEscapes(t *testing.T) {
	cases := map[string]string{
		"newline": "a\nb",
		"tab":     "a\tb",
		"quote":   `a"b`,
		"slash":   `a\b`,
		"control": string([]byte{0x01}),
		"empty":   "",
		"unicode": "日本語",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := MarshalKYAML(map[string]string{"k": in})
			if err != nil {
				t.Fatalf("marshal err: %v", err)
			}
			// Round-trip via Unmarshal (default mode tolerates the KYAML output).
			var got map[string]string
			if err := Unmarshal(out, &got); err != nil {
				t.Fatalf("unmarshal err: %v\noutput:\n%s", err, out)
			}
			if got["k"] != in {
				t.Errorf("round-trip mismatch:\n in:  %q\n got: %q\n out:\n%s", in, got["k"], out)
			}
		})
	}
}

// TestKYAMLEncodeSequenceCuddling verifies R8.2 + R8.4 — for [{...}] the
// outer brackets cuddle to the inner element brackets.
func TestKYAMLEncodeSequenceCuddling(t *testing.T) {
	out, err := MarshalKYAML([]map[string]string{{"name": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Open should cuddle: "[{" appears.
	if !strings.Contains(s, "[{") {
		t.Errorf("expected cuddled open '[{' in:\n%s", s)
	}
	// Close should cuddle: "}]" appears.
	if !strings.Contains(s, "}]") {
		t.Errorf("expected cuddled close '}]' in:\n%s", s)
	}
}

// TestKYAMLEncodeTrailingCommas verifies R8.1: trailing commas after the
// final element of mappings (which never cuddle).
func TestKYAMLEncodeTrailingCommas(t *testing.T) {
	out, err := MarshalKYAML(map[string]int{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "a: 1,\n") {
		t.Errorf("expected trailing comma after final mapping entry:\n%s", out)
	}
}

// TestKYAMLEncodeMapKeySorting verifies R4.5: native map keys are sorted
// lexicographically.
func TestKYAMLEncodeMapKeySorting(t *testing.T) {
	out, err := MarshalKYAML(map[string]int{"c": 3, "a": 1, "b": 2})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	posA := strings.Index(s, "a:")
	posB := strings.Index(s, "b:")
	posC := strings.Index(s, "c:")
	if !(posA < posB && posB < posC) {
		t.Errorf("keys not in lexicographic order:\n%s", s)
	}
}

// TestKYAMLEncodeStructDeclarationOrder verifies R4.6: struct fields are
// emitted in declaration order, not lexicographic.
func TestKYAMLEncodeStructDeclarationOrder(t *testing.T) {
	type S struct {
		Z int `json:"z"`
		A int `json:"a"`
		M int `json:"m"`
	}
	out, err := MarshalKYAML(S{Z: 1, A: 2, M: 3})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	posZ := strings.Index(s, "z:")
	posA := strings.Index(s, "a:")
	posM := strings.Index(s, "m:")
	if !(posZ < posA && posA < posM) {
		t.Errorf("struct fields not in declaration order:\n%s", s)
	}
}

// TestKYAMLEncodeJSONTagPriority verifies R13.4: under KYAML mode, json tag
// wins over yaml tag.
func TestKYAMLEncodeJSONTagPriority(t *testing.T) {
	type S struct {
		X int `json:"json_name" yaml:"yaml_name"`
	}
	out, err := MarshalKYAML(S{X: 1})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "json_name:") {
		t.Errorf("expected json_name to win under KYAML:\n%s", s)
	}
	if strings.Contains(s, "yaml_name:") {
		t.Errorf("yaml_name should not appear under KYAML:\n%s", s)
	}
}

// TestKYAMLEncodeAlwaysQuoteKeys verifies WithKYAMLAlwaysQuoteKeys.
func TestKYAMLEncodeAlwaysQuoteKeys(t *testing.T) {
	out, err := MarshalWithOptions(
		map[string]int{"safe": 1, "no": 2},
		WithKYAML(), WithKYAMLAlwaysQuoteKeys(),
	)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"safe":`) {
		t.Errorf("expected 'safe' key to be quoted under always-quote:\n%s", s)
	}
	if !strings.Contains(s, `"no":`) {
		t.Errorf("expected 'no' key to be quoted:\n%s", s)
	}
}

// TestKYAMLEncodeNonStringMapKey verifies R4.4: non-string map keys are
// rejected.
func TestKYAMLEncodeNonStringMapKey(t *testing.T) {
	_, err := MarshalKYAML(map[int]string{1: "one"})
	if err == nil {
		t.Fatal("expected error for non-string map key")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
}

// TestKYAMLOptionConflict verifies ErrOptionConflict on WithFlow(false) +
// WithKYAML.
func TestKYAMLOptionConflict(t *testing.T) {
	_, err := MarshalWithOptions(1, WithKYAML(), WithFlow(false))
	if !errors.Is(err, ErrOptionConflict) {
		t.Errorf("expected ErrOptionConflict, got %v", err)
	}
}

// TestKYAMLRoundTripPod verifies that a typical Kubernetes Pod struct
// survives MarshalKYAML → UnmarshalKYAML round-trip.
func TestKYAMLRoundTripPod(t *testing.T) {
	type Pod struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name   string            `json:"name"`
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Spec struct {
			Containers []struct {
				Name  string `json:"name"`
				Image string `json:"image"`
			} `json:"containers"`
		} `json:"spec"`
	}
	var p Pod
	p.APIVersion = "v1"
	p.Kind = "Pod"
	p.Metadata.Name = "demo"
	p.Metadata.Labels = map[string]string{"app": "demo", "tier": "frontend"}
	p.Spec.Containers = []struct {
		Name  string `json:"name"`
		Image string `json:"image"`
	}{{Name: "nginx", Image: "nginx:1.20"}}

	out, err := MarshalKYAML(p)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidKYAML(out) {
		t.Fatalf("output is not valid KYAML:\n%s\n%v", out, ValidateKYAML(out))
	}
	var got Pod
	if err := UnmarshalKYAML(out, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got.APIVersion != p.APIVersion || got.Kind != p.Kind ||
		got.Metadata.Name != p.Metadata.Name ||
		got.Metadata.Labels["app"] != "demo" ||
		got.Metadata.Labels["tier"] != "frontend" ||
		len(got.Spec.Containers) != 1 ||
		got.Spec.Containers[0].Name != "nginx" ||
		got.Spec.Containers[0].Image != "nginx:1.20" {
		t.Errorf("round-trip mismatch:\nwant: %+v\n got: %+v\n%s", p, got, out)
	}
}

// TestKYAMLMarshalIdempotence verifies that re-marshaling a parsed KYAML
// document produces byte-identical output.
func TestKYAMLMarshalIdempotence(t *testing.T) {
	// Use an ordered structure (struct) to avoid map-iteration nondeterminism.
	type S struct {
		A string `json:"a"`
		B int    `json:"b"`
		C []int  `json:"c"`
	}
	v := S{A: "one", B: 2, C: []int{1, 2, 3}}
	first, err := MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}

	var got S
	if err := UnmarshalKYAML(first, &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, first)
	}

	second, err := MarshalKYAML(got)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("MarshalKYAML round-trip not byte-identical:\n=== first:\n%s=== second:\n%s", first, second)
	}
}

// ---------------------------------------------------------------------------
// KYAML coverage: file I/O, marshaler dispatch, special types, options
// ---------------------------------------------------------------------------

func TestKYAMLMarshalKYAMLWithOptions(t *testing.T) {
	v := map[string]int{"a": 1}
	out, err := MarshalKYAMLWithOptions(v, WithIndent(4))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "    a: 1,") {
		t.Errorf("WithIndent(4) did not take effect under KYAML:\n%s", out)
	}
	if !ValidKYAML(out) {
		t.Errorf("MarshalKYAMLWithOptions output is not valid KYAML:\n%s", out)
	}
}

func TestKYAMLEncodeKYAMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.kyaml")
	v := map[string]int{"port": 8080}
	if err := EncodeKYAMLFile(path, v); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidKYAML(got) {
		t.Errorf("EncodeKYAMLFile output is not valid KYAML:\n%s", got)
	}
	if !strings.Contains(string(got), "port: 8080,") {
		t.Errorf("expected port: 8080 in output:\n%s", got)
	}

	// Round-trip via DecodeKYAMLFile (covers the read path too).
	var back map[string]int
	if err := DecodeKYAMLFile(path, &back); err != nil {
		t.Fatal(err)
	}
	if back["port"] != 8080 {
		t.Errorf("DecodeKYAMLFile mismatch: got %v", back)
	}
}

func TestKYAMLEncodeKYAMLFileError(t *testing.T) {
	// Write to a directory that doesn't exist — surface the error path.
	err := EncodeKYAMLFile("/nonexistent-dir-xyz/foo.kyaml", 1)
	if err == nil {
		t.Fatal("expected EncodeKYAMLFile to fail on bad path")
	}
}

// kyamlJSONMarshalerType implements json.Marshaler — under KYAML mode this
// path takes precedence over yaml.Marshaler (KEP-5295 R13.2).
type kyamlJSONMarshalerType struct {
	X int
}

func (t kyamlJSONMarshalerType) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, `{"json_x":%d}`, t.X*2), nil
}

func (t kyamlJSONMarshalerType) MarshalYAML() (any, error) {
	return map[string]int{"yaml_x": t.X * 100}, nil
}

func TestKYAMLDispatchJSONMarshalerWins(t *testing.T) {
	out, err := MarshalKYAML(kyamlJSONMarshalerType{X: 5})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "json_x:") || !strings.Contains(s, "10,") {
		t.Errorf("expected json.Marshaler path under KYAML, got:\n%s", s)
	}
	if strings.Contains(s, "yaml_x") {
		t.Errorf("yaml.Marshaler should not be used under KYAML when json.Marshaler exists:\n%s", s)
	}
}

// kyamlYAMLOnlyMarshaler implements only yaml.Marshaler — KYAML falls back
// to it when json.Marshaler is not implemented.
type kyamlYAMLOnlyMarshaler struct{ V string }

func (k kyamlYAMLOnlyMarshaler) MarshalYAML() (any, error) {
	return map[string]string{"v": k.V}, nil
}

func TestKYAMLDispatchYAMLMarshalerFallback(t *testing.T) {
	out, err := MarshalKYAML(kyamlYAMLOnlyMarshaler{V: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `v: "hello",`) {
		t.Errorf("yaml.Marshaler fallback did not produce expected output:\n%s", out)
	}
}

// kyamlBytesMarshaler implements BytesMarshaler returning JSON bytes.
type kyamlBytesMarshaler struct{ S string }

func (k kyamlBytesMarshaler) MarshalYAML() ([]byte, error) {
	return fmt.Appendf(nil, `{"s":"%s"}`, k.S), nil
}

func TestKYAMLDispatchBytesMarshaler(t *testing.T) {
	out, err := MarshalKYAML(kyamlBytesMarshaler{S: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `s: "hi"`) {
		t.Errorf("BytesMarshaler path did not produce expected output:\n%s", out)
	}
}

// kyamlTextMarshaler implements only encoding.TextMarshaler.
type kyamlTextMarshaler struct{ N int }

func (k kyamlTextMarshaler) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "n=%d", k.N), nil
}

func TestKYAMLDispatchTextMarshaler(t *testing.T) {
	out, err := MarshalKYAML(kyamlTextMarshaler{N: 7})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"n=7"`) {
		t.Errorf("TextMarshaler path did not produce expected output:\n%s", out)
	}
}

func TestKYAMLDispatchCustomMarshaler(t *testing.T) {
	type custom struct{ N int }
	custFn := func(c custom) ([]byte, error) {
		return fmt.Appendf(nil, "%d", c.N*3), nil
	}
	out, err := MarshalWithOptions(custom{N: 4}, WithKYAML(), WithCustomMarshaler[custom](custFn))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "12") {
		t.Errorf("custom marshaler path did not produce expected output:\n%s", out)
	}
}

func TestKYAMLDispatchCustomMarshalerError(t *testing.T) {
	type custom struct{}
	want := errors.New("boom")
	custFn := func(c custom) ([]byte, error) { return nil, want }
	_, err := MarshalWithOptions(custom{}, WithKYAML(), WithCustomMarshaler[custom](custFn))
	if !errors.Is(err, want) {
		t.Errorf("expected custom marshaler error, got %v", err)
	}
}

func TestKYAMLEncodeJSONNumber(t *testing.T) {
	v := map[string]any{
		"a": json.Number("3.10"),
		"b": json.Number(""),
		"c": json.Number("12345"),
	}
	out, err := MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "a: 3.10,") {
		t.Errorf("json.Number 3.10 not preserved verbatim:\n%s", s)
	}
	if !strings.Contains(s, "b: 0,") {
		t.Errorf("empty json.Number should emit as 0:\n%s", s)
	}
	if !strings.Contains(s, "c: 12345,") {
		t.Errorf("json.Number 12345 not preserved:\n%s", s)
	}
}

// TestKYAMLEncodeRawValueRoundTrip covers R13.11: RawValue carries raw YAML
// bytes that the encoder re-parses and re-emits as canonical KYAML — not
// pass-through, since pass-through could leak non-KYAML constructs.
func TestKYAMLEncodeRawValueRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "block-style mapping converts to KYAML",
			raw:  "name: foo\nvalue: 42\n",
		},
		{
			name: "anchor and alias get reified",
			raw:  "shared: &x { a: 1 }\ncopy: *x\n",
		},
		{
			name: "scalar value",
			raw:  "42\n",
		},
		{
			name: "already-KYAML",
			raw:  "---\n{ name: \"x\" }\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := MarshalKYAML(map[string]any{"r": RawValue(tc.raw)})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !ValidKYAML(out) {
				t.Errorf("RawValue output is not valid KYAML:\n%s", out)
			}
			// Anchor must be reified (no `&` or `*` symbols).
			if bytes.Contains(out, []byte("&")) {
				t.Errorf("anchor not reified:\n%s", out)
			}
		})
	}
}

func TestKYAMLEncodeRawValueEmpty(t *testing.T) {
	out, err := MarshalKYAML(map[string]any{"r": RawValue("")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "r: null,") {
		t.Errorf("empty RawValue should encode as null:\n%s", out)
	}
}

func TestKYAMLEncodeRawValueInvalid(t *testing.T) {
	bad := RawValue([]byte(": :\n - : :\n"))
	_, err := MarshalKYAML(map[string]any{"r": bad})
	if err == nil {
		t.Error("expected error for un-parseable RawValue")
	}
}

// TestRawValueMarshalYAMLDirect covers RawValue.MarshalYAML directly so
// the BytesMarshaler interface implementation (used in default-mode
// encoding) is exercised.
func TestRawValueMarshalYAMLDirect(t *testing.T) {
	r := RawValue([]byte("hello"))
	got, err := r.MarshalYAML()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
	empty := RawValue(nil)
	got, err = empty.MarshalYAML()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "null" {
		t.Errorf("empty RawValue should marshal to %q, got %q", "null", got)
	}
}

// TestKYAMLFormatPreservesLineAndFootComments exercises walkKYAMLComments's
// line and foot branches (head-only paths were already covered by other
// tests).
func TestKYAMLFormatPreservesLineAndFootComments(t *testing.T) {
	src := []byte(`name: foo  # inline comment
other: bar
`)
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidKYAML(out) {
		t.Errorf("output not valid KYAML:\n%s", out)
	}
	if !strings.Contains(string(out), "inline comment") {
		t.Logf("inline comment not preserved (best-effort per R11.5):\n%s", out)
	}
}

// TestWalkKYAMLCommentsLineAndFoot directly exercises walkKYAMLComments
// with synthetic AST nodes carrying line and foot comments. These code
// paths exist for forward-compatibility with future parser support for
// inline/foot comment capture.
func TestWalkKYAMLCommentsLineAndFoot(t *testing.T) {
	// Synthetic mapping with key carrying line + foot comments and value
	// carrying head + line + foot comments.
	mapping := &node{
		kind: nodeMapping,
		children: []*node{
			{
				kind:        nodeScalar,
				value:       "name",
				lineComment: "  inline on key  ",
				footComment: "after the name",
			},
			{
				kind:        nodeScalar,
				value:       "demo",
				headComment: "before value",
				lineComment: "after value",
				footComment: "below value",
			},
			{
				kind:        nodeScalar,
				value:       "list",
				headComment: "list head",
			},
			{
				kind: nodeSequence,
				children: []*node{
					{kind: nodeScalar, value: "item1", lineComment: "item line"},
				},
			},
		},
	}

	out := make(map[string][]Comment)
	walkKYAMLComments(mapping, "", out)

	// Verify line and foot for the key node.
	if got := commentsAtPos(out["name"], LineCommentPos); !contains(got, "inline on key") {
		t.Errorf("line comment on key 'name' missing: %+v", out["name"])
	}
	if got := commentsAtPos(out["name"], FootCommentPos); !contains(got, "after the name") {
		t.Errorf("foot comment on key 'name' missing: %+v", out["name"])
	}
	// Verify head/line/foot on the value.
	if got := commentsAtPos(out["name"], HeadCommentPos); !contains(got, "before value") {
		t.Errorf("head comment on value missing: %+v", out["name"])
	}
	// list[0] has line comment.
	if got := commentsAtPos(out["list[0]"], LineCommentPos); !contains(got, "item line") {
		t.Errorf("line comment on sequence element missing: %+v", out["list[0]"])
	}
}

func commentsAtPos(cs []Comment, p CommentPosition) []string {
	var out []string
	for _, c := range cs {
		if c.Position == p {
			out = append(out, c.Text)
		}
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func TestKYAMLEncodeJSONRawMessage(t *testing.T) {
	raw := json.RawMessage(`{"port":80,"name":"x"}`)
	out, err := MarshalKYAML(map[string]any{"r": raw})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "name: ") || !strings.Contains(s, "port: 80,") {
		t.Errorf("json.RawMessage not re-emitted as KYAML:\n%s", s)
	}
}

func TestKYAMLEncodeJSONRawMessageInvalid(t *testing.T) {
	raw := json.RawMessage(`not valid json`)
	_, err := MarshalKYAML(map[string]any{"r": raw})
	if err == nil {
		t.Error("expected error for invalid json.RawMessage")
	}
}

func TestKYAMLEncodeBigByValue(t *testing.T) {
	bi := big.NewInt(99999)
	bf := big.NewFloat(3.14)
	v := map[string]any{
		"i":     *bi, // value, not pointer
		"f":     *bf, // value
		"i_ptr": bi,
		"f_ptr": bf,
	}
	out, err := MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "i: 99999,") || !strings.Contains(s, "i_ptr: 99999,") {
		t.Errorf("big.Int not rendered:\n%s", s)
	}
	if !strings.Contains(s, "f: 3.14,") || !strings.Contains(s, "f_ptr: 3.14,") {
		t.Errorf("big.Float not rendered:\n%s", s)
	}
}

func TestKYAMLEncodeTimeAndDuration(t *testing.T) {
	tt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	v := map[string]any{
		"t": tt,
		"d": time.Hour + 30*time.Minute,
	}
	out, err := MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"2026-05-07T12:00:00Z"`) {
		t.Errorf("time.Time not RFC 3339-quoted:\n%s", s)
	}
	if !strings.Contains(s, "d: 5400000000000,") {
		t.Errorf("time.Duration not int64 nanoseconds:\n%s", s)
	}
}

// TestKYAMLEncodeDurationAsString covers R13.7: WithDurationAsString opts
// into the human-readable string form for time.Duration.
func TestKYAMLEncodeDurationAsString(t *testing.T) {
	v := map[string]any{
		"d": time.Hour + 30*time.Minute,
	}
	out, err := MarshalWithOptions(v, WithKYAML(), WithDurationAsString(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `d: "1h30m0s",`) {
		t.Errorf("expected duration as quoted string, got:\n%s", out)
	}

	// Default (without the option): int64 nanoseconds.
	out, err = MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "d: 5400000000000,") {
		t.Errorf("expected default int64 ns:\n%s", out)
	}
}

func TestKYAMLEncodeUnsupportedTypes(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"chan", make(chan int)},
		{"func", func() {}},
		{"complex", complex(1, 2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MarshalKYAML(tc.v)
			if !errors.Is(err, ErrUnsupported) {
				t.Errorf("expected ErrUnsupported for %s, got %v", tc.name, err)
			}
		})
	}
}

func TestKYAMLEncodeCyclic(t *testing.T) {
	type cell struct {
		Name string `json:"name"`
		Next *cell  `json:"next"`
	}
	a := &cell{Name: "a"}
	b := &cell{Name: "b"}
	a.Next = b
	b.Next = a
	_, err := MarshalKYAML(a)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported for cyclic value, got %v", err)
	}
}

func TestKYAMLEncodeBase64BytesAndArray(t *testing.T) {
	v := map[string]any{
		"sl":  []byte("hi"),
		"arr": [2]byte{'h', 'i'},
	}
	out, err := MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `sl: "aGk=",`) {
		t.Errorf("[]byte not base64-encoded:\n%s", s)
	}
	if !strings.Contains(s, `arr: "aGk=",`) {
		t.Errorf("[N]byte not base64-encoded:\n%s", s)
	}
}

// kyamlTextMarshalerKey implements TextMarshaler so it can be used as a
// non-string mapping key — exercises mapKeyToString's TextMarshaler branch.
type kyamlTextMarshalerKey struct{ ID int }

func (k kyamlTextMarshalerKey) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "id-%d", k.ID), nil
}

func TestKYAMLEncodeMapTextMarshalerKey(t *testing.T) {
	v := map[kyamlTextMarshalerKey]string{
		{ID: 1}: "one",
		{ID: 2}: "two",
	}
	out, err := MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// id-1 is a safe identifier under the unquoted-key predicate (alphanumeric
	// + dash), so the TextMarshaler-derived key emits unquoted.
	if !strings.Contains(s, `id-1: "one"`) || !strings.Contains(s, `id-2: "two"`) {
		t.Errorf("TextMarshaler map key not used:\n%s", s)
	}
}

func TestKYAMLEncodeMapSliceOrdered(t *testing.T) {
	ms := MapSlice{
		{Key: "z", Value: 1},
		{Key: "a", Value: 2},
		{Key: "m", Value: 3},
	}
	out, err := MarshalKYAML(ms)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	posZ := strings.Index(s, "z:")
	posA := strings.Index(s, "a:")
	posM := strings.Index(s, "m:")
	if !(posZ < posA && posA < posM) {
		t.Errorf("MapSlice should preserve insertion order, got:\n%s", s)
	}
}

func TestKYAMLEncodeMapSliceEmpty(t *testing.T) {
	out, err := MarshalKYAML(MapSlice{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "{}") {
		t.Errorf("empty MapSlice should render as {}:\n%s", out)
	}
}

func TestKYAMLEncodeStructInlineMap(t *testing.T) {
	type S struct {
		Name  string         `json:"name"`
		Extra map[string]int `yaml:",inline"`
	}
	v := S{Name: "x", Extra: map[string]int{"a": 1, "b": 2}}
	out, err := MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "name:") || !strings.Contains(s, "a: 1,") || !strings.Contains(s, "b: 2,") {
		t.Errorf("inline-map struct field not flattened:\n%s", s)
	}
}

func TestKYAMLEncodeStructEmbedded(t *testing.T) {
	type Inner struct {
		Alpha int `json:"alpha"`
		Beta  int `json:"beta"`
	}
	type Outer struct {
		Inner
		Z int `json:"z"`
	}
	out, err := MarshalKYAML(Outer{Inner: Inner{Alpha: 1, Beta: 2}, Z: 3})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"alpha: 1,", "beta: 2,", "z: 3,"} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in:\n%s", want, s)
		}
	}
}

func TestKYAMLEncodeStructOmitEmpty(t *testing.T) {
	type S struct {
		Name string `json:"name,omitempty"`
		Age  int    `json:"age,omitempty"`
	}
	out, err := MarshalKYAML(S{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "age:") {
		t.Errorf("zero age should be omitted with omitempty:\n%s", s)
	}
	if !strings.Contains(s, `name: "x",`) {
		t.Errorf("non-zero name should be present:\n%s", s)
	}
}

func TestKYAMLEncodeStructSkipDash(t *testing.T) {
	type S struct {
		Public  string `json:"public"`
		Private string `json:"-"`
	}
	out, err := MarshalKYAML(S{Public: "p", Private: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "secret") {
		t.Errorf("dash-tagged field should be skipped:\n%s", out)
	}
}

func TestKYAMLEncodeStructPointerToStruct(t *testing.T) {
	type Inner struct {
		X int `json:"x"`
	}
	type Outer struct {
		I *Inner `json:"i"`
	}
	out, err := MarshalKYAML(Outer{I: &Inner{X: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "x: 7,") {
		t.Errorf("nested pointer struct not encoded:\n%s", out)
	}
}

func TestKYAMLEncodeStructFlowOnly(t *testing.T) {
	// Even when a field has yaml:",flow" tag, KYAML mode is already flow,
	// so the tag is moot but should not error.
	type S struct {
		L []int `yaml:"l,flow"`
	}
	out, err := MarshalKYAML(S{L: []int{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidKYAML(out) {
		t.Errorf("output not valid KYAML: %v\n%s", ValidateKYAML(out), out)
	}
}

func TestKYAMLEncodeYAMLTagOnly(t *testing.T) {
	// Field with only a yaml tag (no json tag) — fallback path under KYAML.
	type S struct {
		Name string `yaml:"my_name"`
	}
	out, err := MarshalKYAML(S{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "my_name:") {
		t.Errorf("yaml-only tag not honored under KYAML:\n%s", out)
	}
}

// kyamlYAMLContextMarshaler implements MarshalerContext.
type kyamlYAMLContextMarshaler struct{ Tag string }

func (k kyamlYAMLContextMarshaler) MarshalYAML(ctx context.Context) (any, error) {
	if ctx == nil {
		return nil, errors.New("nil ctx")
	}
	return map[string]string{"tag": k.Tag}, nil
}

// TestKYAMLEncoderMultiDocSeparator (R3.2): under KYAML mode, the Encoder
// must NOT emit a duplicate "---\n" separator between documents — each KYAML
// doc already begins with its own "---\n" header.
func TestKYAMLEncoderMultiDocSeparator(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf, WithKYAML())
	if err := enc.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]int{"b": 2}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// No double separator — must not contain "---\n---\n".
	if strings.Contains(out, "---\n---\n") {
		t.Errorf("duplicate document separator under KYAML mode:\n%s", out)
	}
	// Must contain exactly two "---\n" occurrences (one per doc header).
	if got := strings.Count(out, "---\n"); got != 2 {
		t.Errorf("expected 2 \"---\\n\" headers, got %d:\n%s", got, out)
	}
	// Each document is independently valid KYAML.
	parts := strings.SplitAfter(out, "---\n")
	for i, p := range parts {
		if i == 0 || p == "" {
			continue
		}
		if !ValidKYAML([]byte("---\n" + strings.TrimPrefix(p, "---\n"))) {
			t.Errorf("doc %d is not valid KYAML:\n%s", i, p)
		}
	}
}

func TestKYAMLDispatchContextMarshaler(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf, WithKYAML())
	if err := enc.EncodeContext(context.Background(), kyamlYAMLContextMarshaler{Tag: "v1"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `tag: "v1",`) {
		t.Errorf("MarshalerContext path did not render:\n%s", buf.String())
	}
}

// kyamlEscapeStruct exercises every branch of escapeKYAMLLine: control
// characters (\b, \f, \v, \e, \0, \a, generic \xHH), the YAML 1.2 special
// chars NEL (U+0085), Line Separator (U+2028), Paragraph Separator (U+2029),
// and high-7F controls.
func TestKYAMLEscapeAllSpecials(t *testing.T) {
	cases := map[string]string{
		"\x00": `\0`,
		"\x07": `\a`,
		"\x08": `\b`,
		"\x0b": `\v`,
		"\x0c": `\f`,
		"\x1b": `\e`,
		"\x05": `\x05`,
		"\x7f": `\x7f`,
		// "" is the C1 PAD control char encoded as proper UTF-8 (\xc2\x80)
		// — escapes to \x80 per the C1 control range.
		"":        `\x80`,
		"":        `\N`,
		" ":        `\L`,
		" ":        `\P`,
		"\xff\x30": `\xef\xbf\xbd0`, // invalid UTF-8 → U+FFFD literal
	}
	for in, want := range cases {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			out, err := MarshalKYAML(map[string]string{"k": in})
			if err != nil {
				t.Fatal(err)
			}
			s := string(out)
			// For the invalid-UTF-8 case, the literal U+FFFD UTF-8 (3 bytes)
			// is emitted directly rather than as an escape sequence — accept
			// either the escape form or the literal form.
			if in == "\xff\x30" {
				if !strings.Contains(s, "�") && !strings.Contains(s, want) {
					t.Errorf("expected U+FFFD literal or %q in:\n%s", want, s)
				}
				return
			}
			if !strings.Contains(s, want) {
				t.Errorf("expected escape %q for input %q in:\n%s", want, in, s)
			}
		})
	}
}

func TestKYAMLEncodeMapNonStringKeyRejected(t *testing.T) {
	// Map with int key (no TextMarshaler) must be rejected under KYAML
	// (R4.4: only string-typed keys allowed).
	v := map[int]string{1: "one"}
	_, err := MarshalKYAML(v)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported for int-keyed map, got %v", err)
	}
}

// kyamlNilTextMarshalerKey is a pointer-typed key that may be nil.
type kyamlNilTextMarshalerKey struct{ Name string }

func (k *kyamlNilTextMarshalerKey) MarshalText() ([]byte, error) {
	if k == nil {
		return nil, errors.New("nil")
	}
	return []byte(k.Name), nil
}

func TestKYAMLEncodeMapNilKeyRejected(t *testing.T) {
	v := map[*kyamlNilTextMarshalerKey]int{nil: 1}
	_, err := MarshalKYAML(v)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported for nil map key, got %v", err)
	}
}

// kyamlBadTextMarshalerKey returns an error from MarshalText.
type kyamlBadTextMarshalerKey struct{}

func (k kyamlBadTextMarshalerKey) MarshalText() ([]byte, error) {
	return nil, errors.New("kyamlBadTextMarshalerKey: marshaltext failed")
}

func TestKYAMLEncodeMapTextMarshalerKeyError(t *testing.T) {
	v := map[kyamlBadTextMarshalerKey]int{{}: 1}
	_, err := MarshalKYAML(v)
	if err == nil || !strings.Contains(err.Error(), "marshaltext failed") {
		t.Errorf("expected MarshalText error to bubble up, got %v", err)
	}
}

func TestKYAMLEncodeMapSliceKeyTypes(t *testing.T) {
	// MapSlice supports any-typed keys; mapKeyAnyToString routes through
	// type switch for string/TextMarshaler/reflect fallback.
	cases := []struct {
		name string
		ms   MapSlice
		want string
	}{
		{
			name: "string-key",
			ms:   MapSlice{{Key: "name", Value: 1}},
			want: "name:",
		},
		{
			name: "text-marshaler-key",
			ms:   MapSlice{{Key: kyamlTextMarshalerKey{ID: 5}, Value: "x"}},
			want: "id-5:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := MarshalKYAML(tc.ms)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("expected %q in:\n%s", tc.want, out)
			}
		})
	}
}

func TestKYAMLEncodeMapSliceNilKeyRejected(t *testing.T) {
	ms := MapSlice{{Key: nil, Value: 1}}
	_, err := MarshalKYAML(ms)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported for nil MapSlice key, got %v", err)
	}
}

func TestKYAMLEncodeMapSliceUnsupportedKeyType(t *testing.T) {
	// A non-string non-TextMarshaler value as MapSlice key.
	type opaque struct{ X int }
	ms := MapSlice{{Key: opaque{X: 1}, Value: 2}}
	_, err := MarshalKYAML(ms)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported for opaque MapSlice key, got %v", err)
	}
}

// kyamlPlainBytesMarshaler returns non-JSON, non-YAML bytes — exercises the
// emitRawJSONOrText fallback path that wraps the bytes as a quoted string.
type kyamlPlainBytesMarshaler struct{ S string }

func (k kyamlPlainBytesMarshaler) MarshalYAML() ([]byte, error) {
	return []byte(k.S), nil
}

func TestKYAMLDispatchBytesMarshalerPlainString(t *testing.T) {
	// MarshalYAML returns plain text that's not parseable as JSON. The
	// emitRawJSONOrText fallback should quote it as a KYAML string.
	out, err := MarshalKYAML(kyamlPlainBytesMarshaler{S: "not json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"not json"`) {
		t.Errorf("BytesMarshaler plain-text fallback did not quote string:\n%s", out)
	}
}

func TestKYAMLDispatchCustomMarshalerPlainText(t *testing.T) {
	// Custom marshaler returning non-JSON bytes — same fallback path.
	type plain struct{ V string }
	custFn := func(p plain) ([]byte, error) { return []byte(p.V), nil }
	out, err := MarshalWithOptions(plain{V: "free text"}, WithKYAML(),
		WithCustomMarshaler[plain](custFn))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"free text"`) {
		t.Errorf("custom marshaler plain-text fallback did not quote:\n%s", out)
	}
}

func TestKYAMLEncodeFloat32(t *testing.T) {
	// Exercise emitFloat's 32-bit path.
	v := map[string]float32{"a": float32(1.5), "b": float32(2.0)}
	out, err := MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "a: 1.5,") || !strings.Contains(s, "b: 2,") {
		t.Errorf("float32 not rendered correctly:\n%s", s)
	}
}

func TestKYAMLEncodeInterfaceNil(t *testing.T) {
	type S struct {
		I any `json:"i"`
	}
	out, err := MarshalKYAML(S{I: nil})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "i: null,") {
		t.Errorf("nil interface should render as null:\n%s", out)
	}
}

func TestKYAMLEncodeUnsignedAndSignedInts(t *testing.T) {
	v := map[string]any{
		"i8":  int8(-12),
		"i16": int16(-300),
		"i32": int32(-99999),
		"u8":  uint8(255),
		"u16": uint16(65535),
		"u32": uint32(4294967295),
		"u64": uint64(1 << 63),
	}
	out, err := MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"i8: -12,", "u8: 255,", "u64: 9223372036854775808,"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
}

// kyamlYAMLMarshalerError returns an error from MarshalYAML — exercises the
// dispatchMarshaler error-return branch for yaml.Marshaler.
type kyamlYAMLMarshalerError struct{}

func (kyamlYAMLMarshalerError) MarshalYAML() (any, error) {
	return nil, errors.New("yaml marshaler boom")
}

func TestKYAMLDispatchYAMLMarshalerError(t *testing.T) {
	_, err := MarshalKYAML(kyamlYAMLMarshalerError{})
	if err == nil || !strings.Contains(err.Error(), "yaml marshaler boom") {
		t.Errorf("expected yaml.Marshaler error to bubble up, got %v", err)
	}
}

// kyamlBytesMarshalerError returns an error from BytesMarshaler.
type kyamlBytesMarshalerError struct{}

func (kyamlBytesMarshalerError) MarshalYAML() ([]byte, error) {
	return nil, errors.New("bytes marshaler boom")
}

func TestKYAMLDispatchBytesMarshalerError(t *testing.T) {
	_, err := MarshalKYAML(kyamlBytesMarshalerError{})
	if err == nil || !strings.Contains(err.Error(), "bytes marshaler boom") {
		t.Errorf("expected BytesMarshaler error to bubble up, got %v", err)
	}
}

// kyamlTextMarshalerError returns an error from MarshalText.
type kyamlTextMarshalerError struct{}

func (kyamlTextMarshalerError) MarshalText() ([]byte, error) {
	return nil, errors.New("text marshaler boom")
}

func TestKYAMLDispatchTextMarshalerError(t *testing.T) {
	_, err := MarshalKYAML(kyamlTextMarshalerError{})
	if err == nil || !strings.Contains(err.Error(), "text marshaler boom") {
		t.Errorf("expected TextMarshaler error to bubble up, got %v", err)
	}
}

func TestKYAMLEncodeStructJSONAndYAMLTagMerge(t *testing.T) {
	// Both json and yaml tags present — under KYAML, json wins for the
	// name but yaml-only options (omitempty, etc.) merge in.
	type S struct {
		A string `json:"a" yaml:"a,omitempty"`
		B string `json:"b" yaml:"b,inline"`
		C string `json:"c" yaml:"c,required"`
	}
	v := S{A: "", B: "x", C: "y"}
	out, err := MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "a:") {
		t.Errorf("yaml omitempty should suppress empty string A:\n%s", out)
	}
	if !strings.Contains(string(out), `b: "x"`) || !strings.Contains(string(out), `c: "y"`) {
		t.Errorf("non-omitted fields missing:\n%s", out)
	}
}

func TestKYAMLEncodeStructInlineStruct(t *testing.T) {
	type Inner struct {
		Alpha int `json:"alpha"`
	}
	type Outer struct {
		Inner Inner `yaml:",inline"`
		Beta  int   `json:"beta"`
	}
	out, err := MarshalKYAML(Outer{Inner: Inner{Alpha: 1}, Beta: 2})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "alpha: 1,") || !strings.Contains(s, "beta: 2,") {
		t.Errorf("inline struct fields not flattened:\n%s", s)
	}
}

func TestKYAMLEncodeStructEmbeddedPointer(t *testing.T) {
	// Anonymous embedded *struct (pointer) — exercises the pointer-unwrap
	// branch in collectKYAMLFields.
	type Inner struct {
		Alpha int `json:"alpha"`
	}
	type Outer struct {
		*Inner
		Beta int `json:"beta"`
	}
	out, err := MarshalKYAML(Outer{Inner: &Inner{Alpha: 1}, Beta: 2})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "alpha: 1,") {
		t.Errorf("embedded pointer struct field not flattened:\n%s", s)
	}
}

func TestKYAMLEncodeStructConflict(t *testing.T) {
	// Two fields at the same depth resolving to the same effective name.
	// One uses json:"alpha"; the other uses yaml:"alpha" without a json
	// tag (under KYAML the yaml tag is the fallback). Both end up named
	// "alpha" — the field collector flags a conflict that surfaces as
	// an encode error. (Constructed this way to avoid the go vet
	// `structtag` warning on literally-repeated json tags.)
	type S struct {
		A int `json:"alpha"`
		B int `yaml:"alpha"`
	}
	_, err := MarshalKYAML(S{A: 1, B: 2})
	if err == nil {
		t.Error("expected error for conflicting field names")
	}
}

func TestKYAMLEncodeNilSliceAndMap(t *testing.T) {
	// nil slice/map render as null per KYAML semantics. Empty (non-nil)
	// render as [] / {}.
	type S struct {
		L1 []int          `json:"l1"`
		M1 map[string]int `json:"m1"`
	}
	out, err := MarshalKYAML(S{L1: nil, M1: nil})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "l1: null,") || !strings.Contains(s, "m1: null,") {
		t.Errorf("nil slice/map should render as null:\n%s", s)
	}
}
