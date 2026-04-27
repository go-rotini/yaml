package yaml

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestMarshalBasic(t *testing.T) {
	data, err := Marshal(map[string]string{"key": "value"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "key:") {
		t.Errorf("expected key in output, got %q", data)
	}
}

func TestMarshalNil(t *testing.T) {
	data, err := Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "null") {
		t.Errorf("expected null, got %q", data)
	}
}

func TestMarshalWithOptionsIndent(t *testing.T) {
	type S struct {
		A struct {
			B string `yaml:"b"`
		} `yaml:"a"`
	}
	v := S{}
	v.A.B = "val"
	data, err := MarshalWithOptions(v, WithIndent(4))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "    b:") {
		t.Errorf("expected 4-space indent, got %q", data)
	}
}

func TestMarshalWithOptionsFlow(t *testing.T) {
	data, err := MarshalWithOptions([]int{1, 2, 3}, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[") {
		t.Errorf("expected flow sequence, got %q", data)
	}
}

func TestNewEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if enc == nil {
		t.Fatal("expected non-nil encoder")
	}
}

func TestNewEncoderWithOptions(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf, WithIndent(4), WithFlow(true))
	if enc == nil {
		t.Fatal("expected non-nil encoder")
	}
}

func TestEncoderEncode(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	err := enc.Encode(map[string]int{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "x:") {
		t.Errorf("expected x in output, got %q", buf.String())
	}
}

func TestEncoderMultiDoc(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	type Doc struct {
		Name string `yaml:"name"`
	}

	_ = enc.Encode(Doc{Name: "first"})
	_ = enc.Encode(Doc{Name: "second"})
	_ = enc.Close()

	output := buf.String()
	if !strings.Contains(output, "---") {
		t.Error("multi-doc output should contain ---")
	}

	dec := NewDecoder(strings.NewReader(output))
	var docs []Doc
	for {
		var d Doc
		err := dec.Decode(&d)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, d)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
}

func TestEncoderClose(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Close(); err != nil {
		t.Errorf("Close should return nil, got %v", err)
	}
}

func TestEncoderEncodeContext(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	ctx := context.Background()
	err := enc.EncodeContext(ctx, map[string]int{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "x:") {
		t.Errorf("expected x in output, got %q", buf.String())
	}
}

func TestEncoderWriteError(t *testing.T) {
	enc := NewEncoder(&failWriter{}, WithIndent(4))
	err := enc.Encode(map[string]string{"a": "b"})
	if err != nil {
		t.Fatal(err)
	}
	err = enc.Encode(map[string]string{"c": "d"})
	if err == nil {
		t.Fatal("expected write error")
	}
}

type failWriter struct {
	n int
}

func (f *failWriter) Write(p []byte) (int, error) {
	f.n++
	if f.n > 1 {
		return 0, fmt.Errorf("write failed")
	}
	return len(p), nil
}

func TestEncoderMultiDocSeparator(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	_ = enc.Encode("first")
	_ = enc.Encode("second")
	output := buf.String()
	if !strings.Contains(output, "---\n") {
		t.Error("expected --- separator between documents")
	}
}

func TestEncoderWriteErrorOnSeparator(t *testing.T) {
	w := &failWriter{}
	enc := NewEncoder(w)
	_ = enc.Encode("first")
	w.n = 999
	err := enc.Encode("second")
	if err == nil {
		t.Error("expected error writing separator")
	}
}

type errorMarshaler struct{}

func (e errorMarshaler) MarshalYAML() (any, error) {
	return nil, fmt.Errorf("marshal error")
}

func TestEncodeContextMarshalError(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	err := enc.EncodeContext(context.Background(), errorMarshaler{})
	if err == nil {
		t.Fatal("expected error from marshaler")
	}
	if !strings.Contains(err.Error(), "marshal error") {
		t.Errorf("unexpected error: %v", err)
	}
}
