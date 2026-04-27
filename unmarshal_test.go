package yaml

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnmarshalBasic(t *testing.T) {
	var m map[string]string
	err := Unmarshal([]byte("key: value"), &m)
	if err != nil {
		t.Fatal(err)
	}
	if m["key"] != "value" {
		t.Errorf("expected value, got %q", m["key"])
	}
}

func TestUnmarshalRequiresPointer(t *testing.T) {
	var s string
	err := Unmarshal([]byte("hello"), s)
	if err == nil {
		t.Error("expected error for non-pointer")
	}
}

func TestUnmarshalNilPointer(t *testing.T) {
	err := Unmarshal([]byte("hello"), nil)
	if err == nil {
		t.Error("expected error for nil pointer")
	}
}

func TestUnmarshalEmpty(t *testing.T) {
	var v any
	err := Unmarshal([]byte(""), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Errorf("expected nil for empty input, got %v", v)
	}
}

func TestUnmarshalWithOptionsStrict(t *testing.T) {
	type S struct {
		Name string `yaml:"name"`
	}
	var s S
	err := UnmarshalWithOptions([]byte("name: test\nextra: field"), &s, WithStrict())
	if err == nil {
		t.Error("expected error in strict mode for unknown field")
	}
}

func TestUnmarshalMaxDocumentSize(t *testing.T) {
	data := []byte("key: " + strings.Repeat("x", 100))
	var v any
	err := UnmarshalWithOptions(data, &v, WithMaxDocumentSize(10))
	if err == nil {
		t.Error("expected error for exceeding max document size")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected size error, got: %v", err)
	}
}

func TestUnmarshalMaxDocumentSizeWithinLimit(t *testing.T) {
	data := []byte("key: val")
	var v any
	err := UnmarshalWithOptions(data, &v, WithMaxDocumentSize(1000))
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnmarshalMaxNodes(t *testing.T) {
	data := []byte("a: 1\nb: 2\nc: 3\nd: 4\ne: 5\n")
	var v any
	err := UnmarshalWithOptions(data, &v, WithMaxNodes(2))
	if err == nil {
		t.Fatal("expected error for too many nodes")
	}
	if !strings.Contains(err.Error(), "max node count") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnmarshalInvalidYAML(t *testing.T) {
	var v any
	err := Unmarshal([]byte("[unclosed"), &v)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestDecoderMultiDocument(t *testing.T) {
	input := "---\nname: first\n---\nname: second\n---\nname: third\n"
	dec := NewDecoder(strings.NewReader(input))

	type Doc struct {
		Name string `yaml:"name"`
	}

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

	if len(docs) != 3 {
		t.Fatalf("expected 3 docs, got %d", len(docs))
	}
	if docs[0].Name != "first" {
		t.Errorf("doc0: expected first, got %q", docs[0].Name)
	}
}

func TestDecoderRequiresPointer(t *testing.T) {
	dec := NewDecoder(strings.NewReader("a: 1"))
	var s string
	err := dec.Decode(s)
	if err == nil {
		t.Error("expected error for non-pointer")
	}
}

func TestDecoderEOFOnEmpty(t *testing.T) {
	dec := NewDecoder(strings.NewReader(""))
	var v any
	err := dec.Decode(&v)
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestDecoderDecodeContext(t *testing.T) {
	dec := NewDecoder(strings.NewReader("a: 1"))
	ctx := context.Background()
	var v map[string]int
	err := dec.DecodeContext(ctx, &v)
	if err != nil {
		t.Fatal(err)
	}
	if v["a"] != 1 {
		t.Errorf("expected a=1, got %v", v["a"])
	}
}

func TestDecoderWithOptions(t *testing.T) {
	dec := NewDecoder(strings.NewReader("a: 1"), WithMaxDepth(50))
	var v any
	err := dec.Decode(&v)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReferenceFiles(t *testing.T) {
	dir := t.TempDir()
	refFile := filepath.Join(dir, "refs.yaml")
	os.WriteFile(refFile, []byte("defaults: &defaults\n  color: red\n  size: large\n"), 0644)

	input := `item:
  <<: *defaults
  name: widget`
	var out map[string]any
	err := UnmarshalWithOptions([]byte(input), &out, WithReferenceFiles(refFile))
	if err != nil {
		t.Fatal(err)
	}
	item := out["item"].(map[string]any)
	if item["color"] != "red" {
		t.Errorf("expected color=red from reference, got %v", item["color"])
	}
}

func TestReferenceDirs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ref1.yaml"), []byte("base: &base\n  x: 1\n"), 0644)

	input := `val: *base`
	var out map[string]any
	err := UnmarshalWithOptions([]byte(input), &out, WithReferenceDirs(dir))
	if err != nil {
		t.Fatal(err)
	}
}

func TestReferenceDuplicateAnchor(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("x: &dup 1\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.yaml"), []byte("y: &dup 2\n"), 0644)

	input := `val: *dup`
	var out map[string]any
	err := UnmarshalWithOptions([]byte(input), &out, WithReferenceDirs(dir))
	if err == nil {
		t.Fatal("expected duplicate anchor error")
	}
}

func TestRecursiveDirOption(t *testing.T) {
	dir := t.TempDir()
	sub := dir + "/sub"
	os.MkdirAll(sub, 0o755)
	os.WriteFile(sub+"/ref.yaml", []byte("anchor_val: &myref hello\n"), 0o644)

	data := []byte("val: *myref\n")
	var v map[string]string
	err := UnmarshalWithOptions(data, &v,
		WithReferenceDirs(dir),
		WithRecursiveDir(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	if v["val"] != "hello" {
		t.Fatalf("expected 'hello', got %q", v["val"])
	}
}

func TestReferenceFileNotFound(t *testing.T) {
	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceFiles("/nonexistent/file.yaml"))
	if err == nil {
		t.Error("expected error for nonexistent reference file")
	}
}

func TestReferenceDirNotFound(t *testing.T) {
	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceDirs("/nonexistent/dir"))
	if err == nil {
		t.Error("expected error for nonexistent reference dir")
	}
}

func TestDecoderMaxDocumentSize(t *testing.T) {
	data := "key: " + strings.Repeat("x", 100)
	dec := NewDecoder(strings.NewReader(data), WithMaxDocumentSize(10))
	var v any
	err := dec.Decode(&v)
	if err == nil {
		t.Error("expected error for exceeding max document size")
	}
}

func TestAllowDuplicateMapKey(t *testing.T) {
	data := []byte("a: 1\na: 2\n")
	var v map[string]int
	err := UnmarshalWithOptions(data, &v, WithAllowDuplicateMapKey())
	if err != nil {
		t.Fatal(err)
	}
	if v["a"] != 2 {
		t.Fatalf("expected last value to win, got %v", v["a"])
	}
}

func TestUnmarshalBadEncodingConversion(t *testing.T) {
	// Non-printable character triggers scan error after encoding detection
	data := []byte{0x01}
	var v any
	err := Unmarshal(data, &v)
	if err == nil {
		t.Error("expected error for non-printable character")
	}
}

func TestDecoderReadError(t *testing.T) {
	r := &errReader{err: io.ErrUnexpectedEOF}
	dec := NewDecoder(r)
	var v any
	err := dec.Decode(&v)
	if err != io.ErrUnexpectedEOF {
		t.Errorf("expected ErrUnexpectedEOF, got %v", err)
	}
}

type errReader struct {
	err error
}

func (r *errReader) Read(p []byte) (int, error) {
	return 0, r.err
}

func TestDecoderScanError(t *testing.T) {
	dec := NewDecoder(strings.NewReader(string([]byte{0x01})))
	var v any
	err := dec.Decode(&v)
	if err == nil {
		t.Error("expected scan error for non-printable")
	}
}

func TestDecoderParseError(t *testing.T) {
	dec := NewDecoder(strings.NewReader("{unclosed"))
	var v any
	err := dec.Decode(&v)
	if err == nil {
		t.Error("expected parse error for unclosed flow")
	}
}

type testStructValidator struct{}

func (v *testStructValidator) Struct(s any) error {
	return fmt.Errorf("validation failed")
}

func TestUnmarshalValidator(t *testing.T) {
	type S struct {
		Name string `yaml:"name"`
	}
	var s S
	err := UnmarshalWithOptions([]byte("name: test"), &s, WithValidator(&testStructValidator{}))
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecoderValidator(t *testing.T) {
	type S struct {
		Name string `yaml:"name"`
	}
	dec := NewDecoder(strings.NewReader("name: test"), WithValidator(&testStructValidator{}))
	var s S
	err := dec.Decode(&s)
	if err == nil {
		t.Fatal("expected validation error from decoder")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadReferencesSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "refs")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "good.yaml"), []byte("x: &a 1\n"), 0o644)

	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "evil.yaml"), []byte("y: &b 2\n"), 0o644)
	os.Symlink(filepath.Join(outside, "evil.yaml"), filepath.Join(sub, "link.yaml"))

	var v any
	err := UnmarshalWithOptions([]byte("val: *a"), &v, WithReferenceDirs(sub))
	if err == nil {
		t.Fatal("expected symlink escape error")
	}
}

func TestLoadReferencesRecursiveSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "refs")
	os.MkdirAll(filepath.Join(sub, "inner"), 0o755)
	os.WriteFile(filepath.Join(sub, "inner", "good.yaml"), []byte("x: &a 1\n"), 0o644)

	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "evil.yaml"), []byte("y: &b 2\n"), 0o644)
	os.Symlink(filepath.Join(outside, "evil.yaml"), filepath.Join(sub, "inner", "link.yaml"))

	var v any
	err := UnmarshalWithOptions([]byte("val: *a"), &v, WithReferenceDirs(sub), WithRecursiveDir(true))
	if err == nil {
		t.Fatal("expected symlink escape error for recursive dir")
	}
}

func TestLoadReferencesWalkDirError(t *testing.T) {
	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceDirs("/nonexistent/dir"), WithRecursiveDir(true))
	if err == nil {
		t.Error("expected error for nonexistent recursive dir")
	}
}

func TestLoadReferencesBadScanInRefFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte{0x01}, 0o644)

	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceFiles(filepath.Join(dir, "bad.yaml")))
	if err == nil {
		t.Fatal("expected scan error from reference file")
	}
}

func TestLoadReferencesBadParseInRefFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("{unclosed"), 0o644)

	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceFiles(filepath.Join(dir, "bad.yaml")))
	if err == nil {
		t.Fatal("expected parse error from reference file")
	}
}

func TestDecoderMaxDocumentSizeExceeded(t *testing.T) {
	data := "key: " + strings.Repeat("x", 200)
	dec := NewDecoder(strings.NewReader(data), WithMaxDocumentSize(50))
	var v any
	err := dec.Decode(&v)
	if err == nil {
		t.Error("expected error exceeding max document size via decoder")
	}
}

func TestDecoderValidatorNonStruct(t *testing.T) {
	dec := NewDecoder(strings.NewReader("hello"), WithValidator(&testStructValidator{}))
	var s string
	err := dec.Decode(&s)
	if err != nil {
		t.Errorf("validator should not fire for non-struct, got %v", err)
	}
}

func TestDecoderTypeErrors(t *testing.T) {
	dec := NewDecoder(strings.NewReader("- 1\n- 2\n"))
	var s string
	err := dec.Decode(&s)
	if err == nil {
		t.Fatal("expected type error for sequence into string")
	}
	var te *TypeError
	if !errors.As(err, &te) {
		t.Errorf("expected TypeError, got %T: %v", err, err)
	}
}

func TestLoadReferencesNonRecursiveDirWithSubdir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(dir, "ref.yaml"), []byte("x: &a 1\n"), 0o644)
	os.WriteFile(filepath.Join(sub, "ref2.yaml"), []byte("y: &b 2\n"), 0o644)

	var v any
	err := UnmarshalWithOptions([]byte("val: *a"), &v, WithReferenceDirs(dir))
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadReferencesNonRecursiveSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "evil.yaml"), []byte("y: &b 2\n"), 0o644)
	os.Symlink(filepath.Join(outside, "evil.yaml"), filepath.Join(dir, "link.yaml"))

	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceDirs(dir))
	if err == nil {
		t.Fatal("expected symlink escape error in non-recursive mode")
	}
}

func TestLoadReferencesRecursiveWalkError(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "ref.yaml"), []byte("x: &a 1\n"), 0o644)
	os.Chmod(sub, 0o000)
	defer os.Chmod(sub, 0o755)

	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceDirs(dir), WithRecursiveDir(true))
	if err == nil {
		t.Log("walk error test: OS may not enforce permissions for root")
	}
}

func TestLoadReferencesRefFileScanError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte{0x01}, 0o644)

	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceDirs(dir))
	if err == nil {
		t.Fatal("expected scan error from reference file in dir")
	}
}

func TestLoadReferencesRefFileParseError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("{unclosed"), 0o644)

	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceDirs(dir))
	if err == nil {
		t.Fatal("expected parse error from reference file in dir")
	}
}

func TestLoadReferencesNonRecursiveReadDirError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(f, []byte("x"), 0o644)

	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceDirs(f))
	if err == nil {
		t.Fatal("expected error from ReadDir on a file path")
	}
}

func TestLoadReferencesNonRecursiveBrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	os.Symlink("/nonexistent/target.yaml", filepath.Join(dir, "broken.yaml"))

	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceDirs(dir))
	if err == nil {
		t.Fatal("expected error from broken symlink in reference dir")
	}
}

func TestLoadReferencesRecursiveBrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	os.Symlink("/nonexistent/target.yaml", filepath.Join(dir, "broken.yaml"))

	var v any
	err := UnmarshalWithOptions([]byte("a: 1"), &v, WithReferenceDirs(dir), WithRecursiveDir(true))
	if err == nil {
		t.Fatal("expected error from broken symlink in recursive reference dir")
	}
}
