package yaml

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// deepInputs returns inputs nested deeper than maxNestingDepth in flow and
// block style.
func deepInputs() map[string][]byte {
	n := maxNestingDepth + 10
	return map[string][]byte{
		"flow sequence": []byte(strings.Repeat("[", n) + strings.Repeat("]", n)),
		"flow mapping":  []byte(strings.Repeat("{a: ", n) + strings.Repeat("}", n)),
		"block":         []byte(strings.Repeat("- ", n) + "x\n"),
	}
}

func TestDeepNestingRejectedEverywhere(t *testing.T) {
	type S struct{ A any }
	for name, in := range deepInputs() {
		t.Run(name, func(t *testing.T) {
			var s S
			if err := Unmarshal(in, &s); !errors.Is(err, ErrSyntax) {
				t.Errorf("Unmarshal struct: %v", err)
			}
			var v any
			if err := UnmarshalWithOptions(in, &v, WithMaxDepth(0)); !errors.Is(err, ErrSyntax) {
				t.Errorf("Unmarshal any without a depth limit: %v", err)
			}
			if err := NewDecoder(bytes.NewReader(in)).Decode(&v); !errors.Is(err, ErrSyntax) {
				t.Errorf("Decoder: %v", err)
			}
			if _, err := Parse(in); !errors.Is(err, ErrSyntax) {
				t.Errorf("Parse: %v", err)
			}
			if Valid(in) {
				t.Error("Valid: true")
			}
			if err := ValidateKYAML(in); !errors.Is(err, ErrSyntax) {
				t.Errorf("ValidateKYAML: %v", err)
			}
			if _, err := Lint(in); !errors.Is(err, ErrSyntax) {
				t.Errorf("Lint: %v", err)
			}
			if _, err := Format(in); !errors.Is(err, ErrSyntax) {
				t.Errorf("Format: %v", err)
			}
		})
	}
}

func TestMaxDepthAppliesToAny(t *testing.T) {
	in := []byte(strings.Repeat("[", 500) + strings.Repeat("]", 500))
	var v any
	if err := Unmarshal(in, &v); err == nil || !strings.Contains(err.Error(), "exceeded max depth 100") {
		t.Errorf("into any: got %v, want max depth error", err)
	}
	var s []any
	if err := Unmarshal(in, &s); err == nil {
		t.Error("into []any: expected max depth error")
	}
	var m MapSlice
	if err := Unmarshal([]byte(strings.Repeat("{a: ", 200)+strings.Repeat("}", 200)), &m); err == nil {
		t.Error("into MapSlice: expected max depth error")
	}
	if err := UnmarshalWithOptions(in, &v, WithMaxDepth(0)); err != nil {
		t.Errorf("no limit: %v", err)
	}
	// The limit counts levels the same way for any as for typed targets.
	in = []byte(strings.Repeat("[", 99) + strings.Repeat("]", 99))
	if err := Unmarshal(in, &v); err != nil {
		t.Errorf("99 levels: %v", err)
	}
}

func TestDecodeNumberOverflow(t *testing.T) {
	type S struct {
		A int8
		B uint8
		C float32
		D int32
		E int64
		F float64
	}
	cases := map[string]string{
		"int8":    "a: 300",
		"uint8":   "b: 256",
		"float32": "c: 1e300",
		"int32":   "d: 4294967297",
		"int64":   "e: 99999999999999999999",
		"float64": "f: 1e400",
	}
	for name, in := range cases {
		var s S
		err := Unmarshal([]byte(in), &s)
		if !errors.Is(err, ErrType) || !strings.Contains(err.Error(), "overflows") {
			t.Errorf("%s: got %+v, %v; want overflow TypeError", name, s, err)
		}
	}
	var s S
	if err := Unmarshal([]byte("a: -128\nb: 255\nc: .inf\nd: -2147483648"), &s); err != nil {
		t.Errorf("in range: %v", err)
	}

	type D struct {
		A int8  `yaml:"a,default=300"`
		B uint8 `yaml:"b,default=256"`
	}
	var d D
	if err := UnmarshalWithOptions([]byte("{}"), &d, WithDefaults()); !errors.Is(err, ErrDefault) {
		t.Errorf("default overflow: got %+v, %v", d, err)
	}
}

func TestMarshalMapSliceIsMapping(t *testing.T) {
	ms := MapSlice{{Key: "b", Value: 1}, {Key: "a", Value: MapSlice{{Key: "x", Value: "z"}}}}
	cases := []struct {
		v    any
		opts []EncodeOption
		want string
	}{
		{ms, nil, "b: 1\na:\n  x: z\n"},
		{ms, []EncodeOption{WithFlow(true)}, "{b: 1, a: {x: z}}\n"},
		{[]MapItem(ms), nil, "b: 1\na:\n  x: z\n"},
		{map[string]any{"m": ms}, nil, "m:\n  b: 1\n  a:\n    x: z\n"},
		{struct{ M MapSlice }{ms}, nil, "m:\n  b: 1\n  a:\n    x: z\n"},
		{[]any{ms}, nil, "- b: 1\n  a:\n    x: z\n"},
		{MapSlice{}, nil, "{}\n"},
		{MapSlice{{Key: "a", Value: ""}, {Key: "b", Value: 2}}, []EncodeOption{WithOmitEmpty(true)}, "b: 2\n"},
		{map[string]any{"m": MapSlice{{Key: "a", Value: nil}}}, []EncodeOption{WithOmitEmpty(true)}, "m: {}\n"},
		{struct{ M MapSlice }{MapSlice{{Key: "s", Value: []int{1}}}}, []EncodeOption{WithIndentSequence(false)}, "m:\n  s:\n  - 1\n"},
	}
	for i, c := range cases {
		out, err := MarshalWithOptions(c.v, c.opts...)
		if err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		if string(out) != c.want {
			t.Errorf("%d: got %q, want %q", i, out, c.want)
		}
	}

	src := []byte("z: 1\na:\n  y: [1, 2]\n  b: {c: d}\n")
	var v any
	if err := UnmarshalWithOptions(src, &v, WithOrderedMap()); err != nil {
		t.Fatal(err)
	}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var back any
	if err := UnmarshalWithOptions(out, &back, WithOrderedMap()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v, back) {
		t.Errorf("ordered round trip:\n%#v\n%#v\n%s", v, back, out)
	}
}

type selfMarshaler struct{}

func (selfMarshaler) MarshalYAML() (any, error) { return selfMarshaler{}, nil }

func TestMarshalCyclicValues(t *testing.T) {
	type node struct {
		Name string
		Next *node
	}
	ptr := &node{Name: "a"}
	ptr.Next = ptr
	m := map[string]any{}
	m["self"] = m
	s := []any{nil}
	s[0] = s
	cases := map[string]any{
		"pointer":        ptr,
		"map":            m,
		"slice":          s,
		"marshaler":      []any{selfMarshaler{}},
		"marshaler root": selfMarshaler{},
	}
	for name, v := range cases {
		for _, opts := range [][]EncodeOption{nil, {WithFlow(true)}, {WithKYAML()}} {
			if _, err := MarshalWithOptions(v, opts...); !errors.Is(err, ErrUnsupported) {
				t.Errorf("%s %d options: got %v, want ErrUnsupported", name, len(opts), err)
			}
		}
	}

	// A value shared without a cycle is fine.
	shared := &node{Name: "s"}
	if _, err := Marshal([]*node{shared, shared}); err != nil {
		t.Errorf("shared value: %v", err)
	}
}

func TestReferenceDirSymlinkToPrefixSibling(t *testing.T) {
	root := t.TempDir()
	refs := filepath.Join(root, "refs")
	evil := filepath.Join(root, "refs-evil")
	for _, d := range []string{refs, evil} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(evil, "x.yaml"), []byte("s: &s from refs-evil\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "refs-evil", "x.yaml"), filepath.Join(refs, "link.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, recursive := range []bool{false, true} {
		var v any
		err := UnmarshalWithOptions([]byte("a: *s\n"), &v, WithReferenceDirs(refs), WithRecursiveDir(recursive))
		if !errors.Is(err, ErrPathEscape) {
			t.Errorf("recursive=%v: got %v, %v; want ErrPathEscape", recursive, v, err)
		}
	}

	// A file inside the directory whose name starts with ".." is not an escape.
	if err := os.Remove(filepath.Join(refs, "link.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refs, "..x.yaml"), []byte("s: &s inside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var v map[string]string
	if err := UnmarshalWithOptions([]byte("a: *s\n"), &v, WithReferenceDirs(refs)); err != nil || v["a"] != "inside" {
		t.Errorf("..x.yaml: got %v, %v", v, err)
	}
}

// countingReader yields an endless stream of "a" and counts the bytes read.
type countingReader struct{ n int }

func (r *countingReader) Read(p []byte) (int, error) {
	if r.n > 1<<20 {
		return 0, io.EOF
	}
	for i := range p {
		p[i] = 'a'
	}
	r.n += len(p)
	return len(p), nil
}

func TestDecoderMaxDocumentSizeStopsReading(t *testing.T) {
	r := &countingReader{}
	var v any
	err := NewDecoder(r, WithMaxDocumentSize(1024)).Decode(&v)
	if !errors.Is(err, ErrDocumentSize) {
		t.Fatalf("got %v, want ErrDocumentSize", err)
	}
	if r.n > 1024+512 {
		t.Errorf("read %d bytes for a 1024 byte limit", r.n)
	}
}

type badTargetUnmarshaler struct{ target func() any }

func (u *badTargetUnmarshaler) UnmarshalYAML(unmarshal func(any) error) error {
	return unmarshal(u.target())
}

func TestUnmarshalerCallbackTargets(t *testing.T) {
	var x int
	var nilPtr *int
	for name, target := range map[string]func() any{
		"non-pointer": func() any { return x },
		"nil":         func() any { return nil },
		"nil pointer": func() any { return nilPtr },
		"nil map":     func() any { return map[string]int(nil) },
	} {
		u := &badTargetUnmarshaler{target: target}
		if err := Unmarshal([]byte("5"), u); !errors.Is(err, ErrNilPointer) {
			t.Errorf("%s: got %v, want ErrNilPointer", name, err)
		}
	}
	m := map[string]int{}
	u := &badTargetUnmarshaler{target: func() any { return m }}
	if err := Unmarshal([]byte("a: 1"), u); err != nil || m["a"] != 1 {
		t.Errorf("map target: %v, %v", m, err)
	}
}

type unexportedInner struct{ X int }

func TestUnexportedEmbeddedPointerDoesNotPanic(t *testing.T) {
	type S struct {
		*unexportedInner
		Y int
	}
	var s S
	err := Unmarshal([]byte("x: 1\ny: 2"), &s)
	if !errors.Is(err, ErrType) || s.Y != 2 {
		t.Errorf("got %+v, %v; want a TypeError and y decoded", s, err)
	}
	if err := UnmarshalWithOptions([]byte("base: &b {x: 1}\n<<: *b\ny: 2"), &s); !errors.Is(err, ErrType) {
		t.Errorf("merge: got %v", err)
	}
}

func TestMergeKeySemantics(t *testing.T) {
	t.Run("nested merge into typed map", func(t *testing.T) {
		var m map[string]int
		in := "base: &b {a: 1}\nmid: &m {<<: *b, b: 2}\nout:\n  <<: *m\n  c: 3\n"
		var doc struct {
			Out map[string]int `yaml:"out"`
		}
		if err := Unmarshal([]byte(in), &doc); err != nil {
			t.Fatal(err)
		}
		m = doc.Out
		if !reflect.DeepEqual(m, map[string]int{"a": 1, "b": 2, "c": 3}) {
			t.Errorf("got %v", m)
		}
	})
	t.Run("explicit zero wins over merge", func(t *testing.T) {
		type S struct{ Port int }
		var s S
		if err := Unmarshal([]byte("port: 0\n<<: {port: 5}"), &s); err != nil || s.Port != 0 {
			t.Errorf("got %+v, %v", s, err)
		}
	})
	t.Run("merge replaces a prefilled value", func(t *testing.T) {
		type S struct{ Port int }
		s := S{Port: 9000}
		if err := Unmarshal([]byte("<<: {port: 8080}"), &s); err != nil || s.Port != 8080 {
			t.Errorf("got %+v, %v", s, err)
		}
	})
	t.Run("earlier merge source wins", func(t *testing.T) {
		type S struct{ A, B int }
		var s S
		if err := Unmarshal([]byte("<<: [{a: 1}, {a: 2, b: 3}]"), &s); err != nil || s != (S{1, 3}) {
			t.Errorf("got %+v, %v", s, err)
		}
	})
	t.Run("required field supplied by merge", func(t *testing.T) {
		type S struct {
			Name string `yaml:"name,required"`
		}
		var s S
		if err := Unmarshal([]byte("base: &b {name: x}\n<<: *b"), &s); err != nil || s.Name != "x" {
			t.Errorf("got %+v, %v", s, err)
		}
	})
	t.Run("merging a scalar is an error", func(t *testing.T) {
		for _, in := range []string{"<<: 1\na: 2", "<<: [1, 2]\na: 2", "x: &x 1\n<<: *x\na: 2"} {
			var s struct{ A int }
			var m map[string]int
			var v any
			var o any
			for target, err := range map[string]error{
				"struct":  Unmarshal([]byte(in), &s),
				"map":     Unmarshal([]byte(in), &m),
				"any":     Unmarshal([]byte(in), &v),
				"ordered": UnmarshalWithOptions([]byte(in), &o, WithOrderedMap()),
			} {
				if !errors.Is(err, ErrType) {
					t.Errorf("%q into %s: got %v, want TypeError", in, target, err)
				}
			}
		}
	})
	t.Run("self-merging anchor stops", func(t *testing.T) {
		var s struct{ A int }
		if err := Unmarshal([]byte("&a {a: 1, <<: *a}"), &s); err == nil {
			t.Error("expected an error")
		}
	})
}

func TestDisallowDuplicateKeyEveryTarget(t *testing.T) {
	in := []byte("a: 1\na: 2\n")
	var s struct{ A int }
	var m map[string]int
	var v any
	var o any
	var ms MapSlice
	for name, err := range map[string]error{
		"struct":   UnmarshalWithOptions(in, &s, WithDisallowDuplicateKey()),
		"map":      UnmarshalWithOptions(in, &m, WithDisallowDuplicateKey()),
		"any":      UnmarshalWithOptions(in, &v, WithDisallowDuplicateKey()),
		"ordered":  UnmarshalWithOptions(in, &o, WithDisallowDuplicateKey(), WithOrderedMap()),
		"MapSlice": UnmarshalWithOptions(in, &ms, WithDisallowDuplicateKey()),
	} {
		if !errors.Is(err, ErrDuplicateKey) {
			t.Errorf("%s: got %v, want DuplicateKeyError", name, err)
		}
	}
	if err := Unmarshal(in, &s); err != nil || s.A != 2 {
		t.Errorf("without the option: %+v, %v", s, err)
	}
}

func TestRequiredAndDefaultsWhenMappingAbsent(t *testing.T) {
	type C struct {
		Host string `yaml:"host,default=localhost"`
		Port int    `yaml:"port,default=80"`
	}
	for _, in := range []string{"", "# only a comment\n", "---\n", "~\n"} {
		var c C
		if err := UnmarshalWithOptions([]byte(in), &c, WithDefaults()); err != nil || c != (C{"localhost", 80}) {
			t.Errorf("%q: got %+v, %v", in, c, err)
		}
		pc := &C{}
		if err := UnmarshalWithOptions([]byte(in), &pc, WithDefaults()); err != nil {
			t.Errorf("%q into **C: %v", in, err)
		}
	}

	type R struct {
		Name string `yaml:"name,required"`
	}
	for _, in := range []string{"", "~"} {
		var r R
		if err := Unmarshal([]byte(in), &r); !errors.Is(err, ErrRequiredField) || !errors.Is(err, ErrSyntax) {
			t.Errorf("%q: got %v, want RequiredFieldError", in, err)
		}
	}

	type Outer struct {
		Other int `yaml:"other"`
		Inner C   `yaml:"inner"`
		Need  R   `yaml:"need"`
		Opt   *R  `yaml:"opt"`
	}
	var o Outer
	err := UnmarshalWithOptions([]byte("other: 1\nneed: {name: n}"), &o, WithDefaults())
	if err != nil || o.Inner != (C{"localhost", 80}) || o.Opt != nil {
		t.Errorf("absent section: got %+v, %v", o, err)
	}
	if err := Unmarshal([]byte("other: 1"), &o); !errors.Is(err, ErrRequiredField) {
		t.Errorf("absent section with a required field: got %v", err)
	}
	if err := UnmarshalWithOptions([]byte("need: {name: n}\ninner: ~"), &o, WithDefaults()); err != nil || o.Inner.Port != 80 {
		t.Errorf("null section: got %+v, %v", o, err)
	}
}

func TestScalarIntoSliceIsTypeError(t *testing.T) {
	var s struct{ Tags []string }
	if err := Unmarshal([]byte("tags: web"), &s); !errors.Is(err, ErrType) {
		t.Errorf("got %+v, %v", s, err)
	}
	var b struct{ Data []byte }
	if err := Unmarshal([]byte("data: abc"), &b); err != nil || string(b.Data) != "abc" {
		t.Errorf("[]byte: got %q, %v", b.Data, err)
	}
}

func TestNodeToBytesAliasKey(t *testing.T) {
	for _, in := range []string{
		"a: &a k\n*a : v\n",
		"- &b x\n- {*b : y}\n",
		"&a a: &b b\n*b : *a\n",
		"x: &a: k\n*a: : v\n",
	} {
		f, err := Parse([]byte(in))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		var want any
		if err := Unmarshal([]byte(in), &want); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		out, err := NodeToBytes(f.Docs[0])
		if err != nil {
			t.Fatal(err)
		}
		var got any
		if err := Unmarshal(out, &got); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: wrote %q, read %v, %v; want %v", in, out, got, err, want)
		}
	}
}

func TestIndentBelowOne(t *testing.T) {
	v := map[string]any{"a": map[string]int{"b": 1}}
	for _, n := range []int{0, -1} {
		if _, err := MarshalWithOptions(v, WithIndent(n)); !errors.Is(err, ErrOptionConflict) {
			t.Errorf("WithIndent(%d): got %v", n, err)
		}
		if err := NewEncoder(io.Discard, WithIndent(n)).Encode(v); !errors.Is(err, ErrOptionConflict) {
			t.Errorf("Encoder WithIndent(%d): got %v", n, err)
		}
		f, _ := Parse([]byte("a:\n  b: 1\n"))
		if _, err := NodeToBytesWithOptions(f.Docs[0], WithIndent(n)); !errors.Is(err, ErrOptionConflict) {
			t.Errorf("NodeToBytesWithOptions WithIndent(%d): got %v", n, err)
		}
		if _, err := MarshalWithOptions(v, WithIndent(n), WithFlow(true)); err != nil {
			t.Errorf("flow WithIndent(%d): %v", n, err)
		}
	}
}

type dominantA struct {
	X int `yaml:"x"`
}

type dominantB struct {
	X int `yaml:"x"`
}

type dominantUntagged struct{ X int }

func TestDominantEmbeddedField(t *testing.T) {
	type Outer struct {
		dominantA
		dominantB
		X int `yaml:"x"`
	}
	var o Outer
	if err := Unmarshal([]byte("x: 5"), &o); err != nil || o.X != 5 || o.dominantA.X != 0 {
		t.Errorf("shallow field: %+v, %v", o, err)
	}
	if out, err := Marshal(Outer{X: 7}); err != nil || string(out) != "x: 7\n" {
		t.Errorf("Marshal: %q, %v", out, err)
	}
	if out, err := MarshalKYAML(Outer{X: 7}); err != nil || !strings.Contains(string(out), "x: 7") {
		t.Errorf("MarshalKYAML: %q, %v", out, err)
	}

	type Tagged struct {
		dominantA
		dominantUntagged
	}
	var tg Tagged
	if err := Unmarshal([]byte("x: 3"), &tg); err != nil || tg.dominantA.X != 3 {
		t.Errorf("tagged wins at equal depth: %+v, %v", tg, err)
	}

	type Tie struct {
		dominantA
		dominantB
	}
	var tie Tie
	if err := Unmarshal([]byte("x: 1"), &tie); err == nil {
		t.Error("true tie: expected a conflict error")
	}
}

type positionedErr struct{}

func (*positionedErr) UnmarshalYAML(func(any) error) error { return errors.New("bad value") }

func TestUnmarshalerErrorPositionAndNull(t *testing.T) {
	var s struct {
		A int
		B positionedErr
	}
	err := Unmarshal([]byte("a: 1\nb: x\n"), &s)
	var ue *UnmarshalerError
	if !errors.As(err, &ue) || ue.Pos.Line != 2 || ue.Err.Error() != "bad value" {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(FormatError([]byte("a: 1\nb: x\n"), err), "  b: x\n     ^") {
		t.Errorf("FormatError:\n%s", FormatError([]byte("a: 1\nb: x\n"), err))
	}

	var addr struct {
		A netip.Addr
		B netip.Addr
	}
	if err := Unmarshal([]byte("a: null\nb: ~"), &addr); err != nil || addr.A.IsValid() {
		t.Errorf("null into TextUnmarshaler: %+v, %v", addr, err)
	}
	err = Unmarshal([]byte("a: 1.2.3"), &addr)
	if !errors.As(err, &ue) || ue.Pos.Line != 1 {
		t.Errorf("TextUnmarshaler error: %v", err)
	}
}

func TestAliasesFollowAnchorsInDocument(t *testing.T) {
	var v any
	for _, in := range []string{"a: *x\nb: &x 1\n", "a: &x 1\n---\nb: *x\n"} {
		dec := NewDecoder(strings.NewReader(in))
		var err error
		for err == nil {
			err = dec.Decode(&v)
		}
		if !errors.Is(err, ErrSyntax) || !strings.Contains(err.Error(), "unknown alias") {
			t.Errorf("%q: got %v, want unknown alias", in, err)
		}
	}
	var m map[string]int
	if err := Unmarshal([]byte("a: &x 1\nb: *x\nc: &x 2\nd: *x\n"), &m); err != nil || m["b"] != 1 || m["d"] != 2 {
		t.Errorf("redefined anchor: %v, %v", m, err)
	}
	if err := UnmarshalWithOptions([]byte("&a [*a]"), &v, WithMaxDepth(0)); !errors.Is(err, ErrCycle) {
		t.Errorf("self reference: got %v, want CycleError", err)
	}
	// An anchor in a part of the document no field reads still counts.
	var s struct{ B int }
	if err := Unmarshal([]byte("defs: &x 5\nb: *x\n"), &s); err != nil || s.B != 5 {
		t.Errorf("skipped anchor: %+v, %v", s, err)
	}
}

func TestAliasKeyUsesAnchorText(t *testing.T) {
	var v map[string]any
	if err := Unmarshal([]byte("a: &k 1.0\n*k : b\n"), &v); err != nil || v["1.0"] != "b" {
		t.Errorf("got %v, %v", v, err)
	}
	var a any
	if err := Unmarshal([]byte("x: &n ~\n*n : b\n"), &a); err != nil || a.(map[string]any)["~"] != "b" {
		t.Errorf("null key: got %v, %v", a, err)
	}
	var o any
	if err := UnmarshalWithOptions([]byte("a: &k 1.0\n*k : b\n"), &o, WithOrderedMap()); err != nil || o.(MapSlice)[1].Key != "1.0" {
		t.Errorf("ordered: got %v, %v", o, err)
	}
}

func TestCommentWithLineBreaks(t *testing.T) {
	for _, text := range []string{"hello\nb: 99", "hello\rb: 99", "hello\r\nb: 99", "hello\u2028b: 99", "hello\u0085b: 99"} {
		for _, pos := range []CommentPosition{HeadCommentPos, LineCommentPos, FootCommentPos} {
			out, err := MarshalWithOptions(map[string]int{"a": 1}, WithComment(map[string][]Comment{"a": {{Position: pos, Text: text}}}))
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]int
			if err := Unmarshal(out, &m); err != nil || len(m) != 1 {
				t.Errorf("%q at %d: wrote %q, read %v, %v", text, pos, out, m, err)
			}
		}
	}
}

func TestFloatEncoding(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{float32(0.1), "0.1\n"},
		{450.0, "450.0\n"},
		{float32(2), "2.0\n"},
		{0.0, "0.0\n"},
		{math.Copysign(0, -1), "-0.0\n"},
		{1e21, "1e+21\n"},
		{2.5, "2.5\n"},
	}
	for _, c := range cases {
		out, err := Marshal(c.v)
		if err != nil || string(out) != c.want {
			t.Errorf("%v (%T): got %q, %v; want %q", c.v, c.v, out, err, c.want)
		}
	}
	var v any
	if err := Unmarshal([]byte("price: 450.00"), &v); err != nil {
		t.Fatal(err)
	}
	out, _ := Marshal(v)
	var back any
	if err := Unmarshal(out, &back); err != nil || !reflect.DeepEqual(v, back) {
		t.Errorf("round trip: %#v -> %q -> %#v", v, out, back)
	}
	if out, _ := FromJSON([]byte(`{"port": 8080, "ratio": 0.5}`)); string(out) != "port: 8080\nratio: 0.5\n" {
		t.Errorf("FromJSON: %q", out)
	}
	if _, err := FromJSON([]byte(`{} x`)); err == nil {
		t.Error("FromJSON trailing data: expected an error")
	}
}

func TestMapKeyOrder(t *testing.T) {
	out, _ := Marshal(map[int]string{1: "a", 10: "b", 2: "c", 9: "d", -3: "e"})
	if string(out) != "-3: e\n1: a\n2: c\n9: d\n10: b\n" {
		t.Errorf("int keys: %q", out)
	}
	m := map[any]int{1: 1, "1": 2, 2.5: 3, "b": 4, uint(3): 5, true: 6}
	first, _ := Marshal(m)
	for range 20 {
		if again, _ := Marshal(m); !bytes.Equal(again, first) {
			t.Fatalf("mixed keys are not deterministic:\n%s\n%s", first, again)
		}
	}
	if string(first) != "1: 1\n2.5: 3\n3: 5\n\"1\": 2\nb: 4\ntrue: 6\n" {
		t.Errorf("mixed keys: %q", first)
	}
}

func TestScannerRejectsNonPrintable(t *testing.T) {
	for _, in := range []string{"\xc8", "a: \x01", "a: &\x01b x", "'a\x7fb'", "\"a\x1bb\"", "# c\x00\na: 1", "a: \xef\xbf\xbe"} {
		if _, err := Parse([]byte(in)); !errors.Is(err, ErrSyntax) {
			t.Errorf("%q: got %v, want SyntaxError", in, err)
		}
	}
	for _, in := range []string{"a: \"\\x01\\0\"", "a: \t b", "a: é ☃ 😀", "\ufeffa: 1", "a: \u0085"} {
		if _, err := Parse([]byte(in)); err != nil {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestErrorDetails(t *testing.T) {
	if FormatError(nil, nil) != "" {
		t.Error("FormatError(nil) should be empty")
	}
	var s struct {
		A int
		B int
		C int
	}
	err := Unmarshal([]byte("a: true\nb: 3.5\nc: x"), &s)
	msg := fmt.Sprint(err)
	for _, want := range []string{"line 1, column 4: cannot unmarshal !!bool", "line 2, column 4: cannot unmarshal !!float", "line 3, column 4: cannot unmarshal !!str"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in %s", want, msg)
		}
	}

	src := []byte("é: 1\né: 2\n")
	err = UnmarshalWithOptions(src, &map[string]int{}, WithDisallowDuplicateKey())
	if got := FormatError(src, err); !strings.HasSuffix(got, "  é: 2\n  ^\n") {
		t.Errorf("duplicate key caret:\n%s", got)
	}
	src = []byte("ä: 1\nzz: 2\n")
	var strict struct{ A int }
	err = UnmarshalWithOptions([]byte("é: [x, y]\n"), &strict, WithStrict())
	if !strings.Contains(FormatError([]byte("é: [x, y]\n"), err), "^") {
		t.Errorf("unknown field caret missing: %s", FormatError(src, err))
	}
	src = []byte("{é: 1, é: 2}")
	err = UnmarshalWithOptions(src, &map[string]int{}, WithDisallowDuplicateKey())
	if got := FormatError(src, err); !strings.HasSuffix(got, "\n  "+strings.Repeat(" ", 7)+"^\n") {
		t.Errorf("multibyte caret:\n%s", got)
	}

	_, err = Parse([]byte("a:\n  - 1\n  b: 2"))
	var se *SyntaxError
	if !errors.As(err, &se) || se.Pos.Line != 3 || strings.Contains(se.Message, "stuck") {
		t.Errorf("mis-indented key: %v", err)
	}
}

func TestArrayDecodeZeroesRest(t *testing.T) {
	a := [3]int{9, 9, 9}
	if err := Unmarshal([]byte("[1]"), &a); err != nil || a != [3]int{1, 0, 0} {
		t.Errorf("got %v, %v", a, err)
	}
	if err := Unmarshal([]byte("[1, 2, 3, 4]"), &a); err != nil || a != [3]int{1, 2, 3} {
		t.Errorf("longer: got %v, %v", a, err)
	}
}

func TestNodeToBytesTagEdgeCases(t *testing.T) {
	for _, in := range []string{"!!%0a%09 0", "!00", "a: !t\nb: 1", "- !t\n- x", "!<tag:example.com,2000:a%20b> x", "{!t : 1}"} {
		f, err := Parse([]byte(in))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		out, err := NodeToBytes(f.Docs[0])
		if err != nil {
			t.Fatal(err)
		}
		f2, err := Parse(out)
		if err != nil {
			t.Errorf("%q: wrote %q, which does not parse: %v", in, out, err)
			continue
		}
		var want, got any
		errWant := Unmarshal([]byte(in), &want)
		errGot := Unmarshal(out, &got)
		if (errWant == nil) != (errGot == nil) || !reflect.DeepEqual(want, got) {
			t.Errorf("%q: wrote %q: %v (%v) vs %v (%v)", in, out, want, errWant, got, errGot)
		}
		if f.Docs[0].Children[0].Tag != f2.Docs[0].Children[0].Tag {
			t.Errorf("%q: tag %q became %q", in, f.Docs[0].Children[0].Tag, f2.Docs[0].Children[0].Tag)
		}
	}
}

type unexportedInt int

func TestUnexportedEmbeddedNonStructSkipped(t *testing.T) {
	type S struct {
		unexportedInt
		A int
	}
	for _, opts := range [][]EncodeOption{nil, {WithKYAML()}} {
		out, err := MarshalWithOptions(S{5, 1}, opts...)
		if err != nil || strings.Contains(string(out), "unexportedint") || strings.Contains(string(out), "5") {
			t.Errorf("got %q, %v", out, err)
		}
	}
}

func TestByteArrayRoundTrip(t *testing.T) {
	type S struct {
		A [3]byte
		B [4]byte
	}
	in := S{[3]byte{'h', 'i', '!'}, [4]byte{1, 2}}
	out, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "a: !!binary aGkh") {
		t.Errorf("block output: %q", out)
	}
	var back S
	if err := Unmarshal(out, &back); err != nil || back != in {
		t.Errorf("round trip: %q -> %v, %v", out, back, err)
	}
	if err := Unmarshal([]byte("a: !!binary aGkhIQ=="), &back); !errors.Is(err, ErrType) {
		t.Errorf("too long: %v", err)
	}
}

func TestPathQuotedKeysAndEdits(t *testing.T) {
	src := []byte("metadata:\n  labels:\n    app.kubernetes.io/name: web\n    'a[0]': x\n")
	for expr, want := range map[string]string{
		"$.metadata.labels['app.kubernetes.io/name']": "web",
		`$.metadata.labels["app.kubernetes.io/name"]`: "web",
		"$.metadata.labels['a[0]']":                   "x",
	} {
		p, err := PathString(expr)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		got, err := p.ReadString(src)
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v", expr, got, err)
		}
		back, err := PathString(p.String())
		if err != nil || back.String() != p.String() {
			t.Errorf("%s: String %q does not compile back: %v", expr, p.String(), err)
		}
	}
	for _, bad := range []string{"$['a", "$['a'x", `$["a]`} {
		if _, err := PathString(bad); !errors.Is(err, ErrPathSyntax) {
			t.Errorf("%q: got %v", bad, err)
		}
	}

	f, _ := Parse([]byte("a: 1\nb: 2\na: 3\n"))
	p, _ := PathString("$.a")
	if err := p.Delete(f.Docs[0]); err != nil {
		t.Fatal(err)
	}
	if n := len(f.Docs[0].Children[0].Children); n != 2 {
		t.Errorf("Delete left %d children, want 2 (both a entries removed)", n)
	}

	root, _ := PathString("$")
	if err := root.Replace(f.Docs[0], &Node{}); !errors.Is(err, ErrPathSyntax) {
		t.Errorf("Replace too short: %v", err)
	}
	if err := root.Delete(f.Docs[0]); !errors.Is(err, ErrPathSyntax) {
		t.Errorf("Delete too short: %v", err)
	}
}

func TestEmptyAnchorOrAliasName(t *testing.T) {
	for _, in := range []string{"0: &,", "a: & value", "- &\n- x", "[&, 1]", "a: *", "a: *,", "{a: * }"} {
		if _, err := Parse([]byte(in)); !errors.Is(err, ErrSyntax) {
			t.Errorf("%q: got %v, want SyntaxError", in, err)
		}
		var v any
		if err := Unmarshal([]byte(in), &v); !errors.Is(err, ErrSyntax) {
			t.Errorf("%q: Unmarshal got %v", in, err)
		}
	}
}

func TestQuotedMergeKeyIsAKey(t *testing.T) {
	for _, in := range []string{"\"<<\": {a: 1}\nb: 2\n", "'<<': {a: 1}\nb: 2\n", "{\"<<\": {a: 1}, b: 2}"} {
		var v any
		if err := Unmarshal([]byte(in), &v); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		m := v.(map[string]any)
		if _, ok := m["<<"]; !ok || m["a"] != nil {
			t.Errorf("%q into any: %v", in, m)
		}
		var o any
		if err := UnmarshalWithOptions([]byte(in), &o, WithOrderedMap()); err != nil || o.(MapSlice)[0].Key != "<<" || len(o.(MapSlice)) != 2 {
			t.Errorf("%q ordered: %v, %v", in, o, err)
		}
		var ms MapSlice
		if err := Unmarshal([]byte(in), &ms); err != nil || ms[0].Key != "<<" {
			t.Errorf("%q MapSlice: %v, %v", in, ms, err)
		}
		var tm map[string]any
		if err := Unmarshal([]byte(in), &tm); err != nil || tm["<<"] == nil || tm["a"] != nil {
			t.Errorf("%q typed map: %v, %v", in, tm, err)
		}
		var s struct {
			Merge map[string]int `yaml:"<<"`
			A     int
			B     int
		}
		if err := Unmarshal([]byte(in), &s); err != nil || s.Merge["a"] != 1 || s.A != 0 || s.B != 2 {
			t.Errorf("%q struct: %+v, %v", in, s, err)
		}
		f, err := Parse([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if k := f.Docs[0].Children[0].Children[0]; k.MergeKey || k.Value != "<<" {
			t.Errorf("%q Node: %+v", in, k)
		}
		out, err := NodeToBytes(f.Docs[0])
		if err != nil {
			t.Fatal(err)
		}
		var back any
		if err := Unmarshal(out, &back); err != nil || !reflect.DeepEqual(back, v) {
			t.Errorf("%q NodeToBytes: wrote %q, read %v, %v", in, out, back, err)
		}
		mout, err := Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := Unmarshal(mout, &back); err != nil || !reflect.DeepEqual(back, v) {
			t.Errorf("%q Marshal: wrote %q, read %v, %v", in, mout, back, err)
		}
	}
	// A plain "<<" is still a merge key.
	var v map[string]any
	if err := Unmarshal([]byte("<<: {a: 1}\nb: 2\n"), &v); err != nil || v["a"] != uint64(1) && v["a"] != int64(1) {
		t.Errorf("plain merge: %v, %v", v, err)
	}
}

// CycleOuterViaPointer reaches itself through an embedded pointer in its
// Inner field, so its promoted fields name its own struct types.
type CycleOuterViaPointer struct {
	X     int `yaml:"x"`
	Inner CycleInnerViaPointer
}

// CycleInnerViaPointer embeds a pointer back to CycleOuterViaPointer.
type CycleInnerViaPointer struct {
	*CycleOuterViaPointer
	Y int `yaml:"y,default=3"`
}

func TestStructReachingItselfThroughEmbeddedPointer(t *testing.T) {
	var a CycleOuterViaPointer
	if err := Unmarshal([]byte("x: 1"), &a); err != nil || a.X != 1 || a.Inner.Y != 0 {
		t.Fatalf("got %+v, %v", a, err)
	}
	var d CycleOuterViaPointer
	if err := UnmarshalWithOptions([]byte("x: 1"), &d, WithDefaults()); err != nil || d.Inner.Y != 3 {
		t.Fatalf("defaults: got %+v, %v", d, err)
	}
}

func TestMergeKeepsExplicitKeyWithOtherSpelling(t *testing.T) {
	var ints map[int]string
	for _, in := range []string{"01: explicit\n<<: {1: merged}", "<<: {1: merged}\n0x1: explicit"} {
		ints = nil
		if err := Unmarshal([]byte(in), &ints); err != nil || ints[1] != "explicit" || len(ints) != 1 {
			t.Errorf("%q: got %v, %v", in, ints, err)
		}
	}
	var bools map[bool]string
	if err := Unmarshal([]byte("true: explicit\n<<: {True: merged}"), &bools); err != nil || bools[true] != "explicit" {
		t.Errorf("map[bool]: got %v, %v", bools, err)
	}
	var s struct {
		Rest map[int]string `yaml:",inline"`
	}
	if err := Unmarshal([]byte("01: explicit\n<<: {1: merged, 2: two}"), &s); err != nil || s.Rest[1] != "explicit" || s.Rest[2] != "two" {
		t.Errorf("inline map: got %v, %v", s.Rest, err)
	}
	// A key the target already held before decoding is still merged over.
	pre := map[string]int{"a": 0}
	if err := Unmarshal([]byte("<<: {a: 1}"), &pre); err != nil || pre["a"] != 1 {
		t.Errorf("pre-filled merge: got %v, %v", pre, err)
	}
}

func TestDisallowDuplicateKeyIgnoresPrefilledEntries(t *testing.T) {
	m := map[string]int{"a": 0}
	if err := UnmarshalWithOptions([]byte("a: 1"), &m, WithDisallowDuplicateKey()); err != nil || m["a"] != 1 {
		t.Errorf("pre-filled map: got %v, %v", m, err)
	}
	var s struct {
		A    int            `yaml:"a"`
		Rest map[string]int `yaml:",inline"`
	}
	s.Rest = map[string]int{"b": 0}
	if err := UnmarshalWithOptions([]byte("b: 1"), &s, WithDisallowDuplicateKey()); err != nil || s.Rest["b"] != 1 {
		t.Errorf("pre-filled inline map: got %v, %v", s.Rest, err)
	}
	dec := NewDecoder(strings.NewReader("a: 1\n---\na: 2\n"), WithDisallowDuplicateKey())
	reused := map[string]int{}
	for range 2 {
		if err := dec.Decode(&reused); err != nil {
			t.Fatalf("reused map: %v", err)
		}
	}
	if reused["a"] != 2 {
		t.Errorf("reused map = %v", reused)
	}
	// Two spellings of one key in the same mapping are still duplicates.
	var ints map[int]int
	var dup *DuplicateKeyError
	err := UnmarshalWithOptions([]byte("1: 1\n01: 2"), &ints, WithDisallowDuplicateKey())
	if !errors.As(err, &dup) || dup.Key != "01" {
		t.Errorf("map[int] spellings: got %v", err)
	}
	var inl struct {
		Rest map[int]int `yaml:",inline"`
	}
	if err := UnmarshalWithOptions([]byte("1: 1\n01: 2"), &inl, WithDisallowDuplicateKey()); !errors.As(err, &dup) {
		t.Errorf("inline map[int] spellings: got %v", err)
	}
	// A merge never counts as a duplicate.
	if err := UnmarshalWithOptions([]byte("a: 1\n<<: {a: 2}"), &m, WithDisallowDuplicateKey()); err != nil || m["a"] != 1 {
		t.Errorf("merge: got %v, %v", m, err)
	}
}

func TestAliasKeysInStructsAndDuplicateChecks(t *testing.T) {
	type S struct {
		Base string `yaml:"base"`
		Name string `yaml:"name"`
	}
	var s S
	if err := UnmarshalWithOptions([]byte("base: &k name\n*k : bob"), &s, WithStrict()); err != nil || s.Name != "bob" {
		t.Errorf("struct: got %+v, %v", s, err)
	}
	in := []byte("&k a: 1\n*k : 2")
	targets := map[string]func() any{
		"any":      func() any { return new(any) },
		"struct":   func() any { return new(struct{ A int }) },
		"map":      func() any { return new(map[string]int) },
		"mapslice": func() any { return new(MapSlice) },
		"inline": func() any {
			return new(struct {
				R map[string]int `yaml:",inline"`
			})
		},
	}
	for name, mk := range targets {
		var dup *DuplicateKeyError
		err := UnmarshalWithOptions(in, mk(), WithDisallowDuplicateKey())
		if !errors.As(err, &dup) || dup.Key != "a" {
			t.Errorf("%s: got %v", name, err)
		}
	}
	var ordered any
	if err := UnmarshalWithOptions(in, &ordered, WithDisallowDuplicateKey(), WithOrderedMap()); !errors.Is(err, ErrDuplicateKey) {
		t.Errorf("ordered: got %v", err)
	}
	// A merge does not replace a field set through an alias key.
	var m S
	if err := Unmarshal([]byte("base: &k name\n*k : explicit\n<<: {name: merged}"), &m); err != nil || m.Name != "explicit" {
		t.Errorf("merge: got %+v, %v", m, err)
	}
}

// rawCapture records the bytes a [BytesUnmarshaler] receives.
type rawCapture struct{ raw string }

func (r *rawCapture) UnmarshalYAML(data []byte) error {
	r.raw = string(data)
	return nil
}

// jsonCapture records the value a JSON unmarshaler receives.
type jsonCapture struct{ v any }

func (j *jsonCapture) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &j.v)
}

func TestRawUnmarshalersGetAliasesExpanded(t *testing.T) {
	in := []byte("base: &b {x: 1}\nval:\n  inner: *b\n")
	var b struct {
		Val rawCapture `yaml:"val"`
	}
	if err := Unmarshal(in, &b); err != nil {
		t.Fatalf("BytesUnmarshaler: %v", err)
	}
	var back map[string]map[string]int
	if err := Unmarshal([]byte(b.Val.raw), &back); err != nil || back["inner"]["x"] != 1 {
		t.Errorf("BytesUnmarshaler got %q: %v, %v", b.Val.raw, back, err)
	}
	var j struct {
		Val jsonCapture `yaml:"val"`
	}
	if err := UnmarshalWithOptions(in, &j, WithJSONUnmarshaler()); err != nil {
		t.Fatalf("JSON unmarshaler: %v", err)
	}
	if want := map[string]any{"inner": map[string]any{"x": float64(1)}}; !reflect.DeepEqual(j.Val.v, want) {
		t.Errorf("JSON unmarshaler got %v", j.Val.v)
	}

	var d struct {
		D time.Duration `yaml:"d"`
		V time.Duration `yaml:"val"`
	}
	parse := WithCustomUnmarshaler(func(p *time.Duration, data []byte) error {
		v, err := time.ParseDuration(strings.TrimSpace(string(data)))
		*p = v
		return err
	})
	if err := UnmarshalWithOptions([]byte("d: &d 5s\nval: *d\n"), &d, parse); err != nil || d.D != 5*time.Second || d.V != 5*time.Second {
		t.Errorf("custom unmarshaler: got %+v, %v", d, err)
	}

	// Expansion is bounded like decoding.
	bomb := []byte("a: &a [1, 2, 3, 4]\nb: &b [*a, *a, *a, *a]\nc: &c [*b, *b, *b, *b]\nval: [*c, *c, *c, *c]\n")
	var lim struct {
		Val rawCapture `yaml:"val"`
	}
	if err := UnmarshalWithOptions(bomb, &lim, WithMaxAliasNodes(20)); !errors.Is(err, ErrAliasExpansion) {
		t.Errorf("alias limit: got %v", err)
	}
}

// strictBytes decodes its bytes strictly, so an unknown field fails with a
// position inside those bytes.
type strictBytes struct{ A int }

func (s *strictBytes) UnmarshalYAML(data []byte) error {
	var v struct {
		A int `yaml:"a"`
	}
	err := UnmarshalWithOptions(data, &v, WithStrict())
	s.A = v.A
	return err
}

func TestBytesUnmarshalerErrorUsesSourcePosition(t *testing.T) {
	in := []byte("x: 1\ny: 2\nz: 3\nval:\n  a: 1\n  bogus: 2\n")
	var v struct {
		Val strictBytes `yaml:"val"`
	}
	err := Unmarshal(in, &v)
	var um *UnmarshalerError
	var unk *UnknownFieldError
	if !errors.As(err, &um) || um.Pos.Line != 5 || !errors.As(err, &unk) {
		t.Fatalf("got %v", err)
	}
	if out := FormatError(in, err); !strings.Contains(out, "a: 1\n") || strings.Contains(out, "y: 2") {
		t.Errorf("FormatError points at the wrong line:\n%s", out)
	}
}

func TestKYAMLJSONNumberIsNeverSpliced(t *testing.T) {
	for _, s := range []string{"1, evil: 2", "1\nevil: 2", "7}\nx: {", "0x10", "+1", "01", "1.", "-"} {
		out, err := MarshalKYAML(map[string]json.Number{"a": json.Number(s)})
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		var back map[string]any
		if err := Unmarshal(out, &back); err != nil || len(back) != 1 || back["a"] != s {
			t.Errorf("%q: wrote %q, read %v, %v", s, out, back, err)
		}
	}
	for _, s := range []string{"12", "-1.5e3", "0", "0.25"} {
		out, err := MarshalKYAML(map[string]json.Number{"a": json.Number(s)})
		if err != nil || !strings.Contains(string(out), "a: "+s+",") {
			t.Errorf("%q: wrote %q, %v", s, out, err)
		}
	}
}

func TestMarshalQuotesYAML11Spellings(t *testing.T) {
	quoted := []string{
		"yes", "Yes", "NO", "y", "N", "on", "Off", "=",
		"0b101", "0X1F", "0O17", "0o17", "012", "1_000", "+1_000", "1_000.5",
		"inf", "-Infinity", "nan", "NaN", "1e400", "0x10000000000000000", "18446744073709551616",
		"2024-01-01", "2024-1-2", "2024-01-01t10:00:00",
	}
	for _, s := range quoted {
		out, err := Marshal(map[string]string{"v": s})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(out, []byte(`v: "`)) {
			t.Errorf("%q written unquoted: %q", s, out)
		}
		var back map[string]string
		if err := Unmarshal(out, &back); err != nil || back["v"] != s {
			t.Errorf("%q: read %v, %v", s, back, err)
		}
	}
	for _, s := range []string{"hello", "1.2.3", "2024x", "2024-01", "infra", "none", "0x-1", "Yesterday", "v1.0"} {
		out, err := Marshal(map[string]string{"v": s})
		if err != nil || string(out) != "v: "+s+"\n" {
			t.Errorf("%q: got %q, %v", s, out, err)
		}
	}
}

func TestTaggedScalarsMustFitTheirTag(t *testing.T) {
	for _, in := range []string{"x: !!int abc", "x: !!int 1.5", "x: !!float abc", "x: !!bool yes", "x: !!null x"} {
		for _, s := range []Schema{CoreSchema, JSONSchema, FailsafeSchema} {
			var v map[string]any
			if err := UnmarshalWithOptions([]byte(in), &v, WithSchema(s)); !errors.Is(err, ErrType) {
				t.Errorf("%q schema %d into any: got %v, %v", in, s, v, err)
			}
		}
		var str struct{ X string }
		if err := Unmarshal([]byte(in), &str); !errors.Is(err, ErrType) {
			t.Errorf("%q into a string field: got %+v, %v", in, str, err)
		}
	}
	for _, s := range []Schema{CoreSchema, JSONSchema, FailsafeSchema} {
		var v map[string]any
		in := "u: !!int 18446744073709551615\nbig: !!int 18446744073709551616\nf: !!float 1\ns: !!int 5\n"
		if err := UnmarshalWithOptions([]byte(in), &v, WithSchema(s)); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"u": uint64(math.MaxUint64), "big": "18446744073709551616", "f": 1.0, "s": int64(5)}
		if !reflect.DeepEqual(v, want) {
			t.Errorf("schema %d: got %#v", s, v)
		}
	}
	var str struct{ X string }
	if err := Unmarshal([]byte("x: !!int 5"), &str); err != nil || str.X != "5" {
		t.Errorf("fitting tag into a string field: got %+v, %v", str, err)
	}
}

func TestNumbersBeyondGoRangesKeepTheirText(t *testing.T) {
	in := "a: 123456789012345678901234567890\nb: -9223372036854775809\nc: 18446744073709551615\nd: 9223372036854775808\ne: 1e400\nf: 1.5\n"
	want := map[string]any{
		"a": "123456789012345678901234567890",
		"b": "-9223372036854775809",
		"c": uint64(math.MaxUint64),
		"d": uint64(9223372036854775808),
		"e": "1e400",
		"f": 1.5,
	}
	for _, s := range []Schema{CoreSchema, JSONSchema} {
		var v map[string]any
		if err := UnmarshalWithOptions([]byte(in), &v, WithSchema(s)); err != nil || !reflect.DeepEqual(v, want) {
			t.Errorf("schema %d: got %#v, %v", s, v, err)
		}
	}
	// The text is written back quoted, so it stays exact.
	var v map[string]any
	if err := Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := Unmarshal(out, &back); err != nil || !reflect.DeepEqual(back, want) {
		t.Errorf("round trip: wrote %q, read %#v, %v", out, back, err)
	}
	// Typed targets still report the overflow.
	var n struct{ A int64 }
	if err := Unmarshal([]byte(in), &n); !errors.Is(err, ErrType) || !strings.Contains(err.Error(), "overflows") {
		t.Errorf("int64 field: got %v", err)
	}
	// A plain big integer stays plain through Parse and NodeToBytes.
	f, err := Parse([]byte("a: 123456789012345678901234567890\n"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := NodeToBytes(f.Docs[0]); err != nil || string(out) != "a: 123456789012345678901234567890" {
		t.Errorf("NodeToBytes: got %q, %v", out, err)
	}
}

func TestNodeToBytesDropsRedundantUintTag(t *testing.T) {
	for in, want := range map[string]string{
		"x: !!int 18446744073709551615":  "x: 18446744073709551615",
		"x: !!int 184467440737095516150": "x: 184467440737095516150",
		"x: !!float 450":                 "x: !!float 450",
	} {
		f, err := Parse([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if out, err := NodeToBytes(f.Docs[0]); err != nil || string(out) != want {
			t.Errorf("%q: got %q, %v", in, out, err)
		}
	}
}

func TestOmitEmptyZeroUintptr(t *testing.T) {
	v := struct {
		P uintptr `yaml:"p,omitempty"`
		Q uintptr `yaml:"q,omitempty"`
	}{Q: 7}
	for _, m := range []func(any) ([]byte, error){Marshal, MarshalKYAML} {
		out, err := m(v)
		if err != nil || bytes.Contains(out, []byte("p:")) || !bytes.Contains(out, []byte("q: 7")) {
			t.Errorf("got %q, %v", out, err)
		}
	}
}

func TestPathStringQuotedKeysRoundTrip(t *testing.T) {
	for _, key := range []string{`it's`, `a\b`, `\'`, `x.y`, `a]b`, `\\`} {
		p, err := PathString(`$['` + pathKeyEscaper.Replace(key) + `']`)
		if err != nil {
			t.Fatalf("%q: %v", key, err)
		}
		s := p.String()
		back, err := PathString(s)
		if err != nil {
			t.Errorf("%q: String %q does not parse: %v", key, s, err)
			continue
		}
		if back.String() != s {
			t.Errorf("%q: %q reads back as %q", key, s, back.String())
		}
		data := []byte(`{"` + strings.ReplaceAll(key, `\`, `\\`) + `": found}`)
		if got, err := back.ReadString(data); err != nil || got != "found" {
			t.Errorf("%q: %q read %q, %v", key, s, got, err)
		}
	}
	p, err := PathString(`$['it\'s']`)
	if err != nil || p.String() != `$['it\'s']` {
		t.Errorf("String of $['it\\'s']: %v, %v", p, err)
	}
}

func TestTagsRoundTripThroughNodeToBytes(t *testing.T) {
	tags := []string{"!<", "!a>b", "!a\"b", "!a\\b", "!a^b", "!a`b", "!a|b", "!x!y", "!café",
		"tag:yaml.org,2002:st\x80", "tag:yaml.org,2002:a<b", "tag:example.com,2000:a", "!plain", "tag:yaml.org,2002:str"}
	for _, tag := range tags {
		n := &Node{Kind: ScalarNode, Tag: tag, Value: "v"}
		out, err := NodeToBytes(n)
		if err != nil {
			t.Fatalf("%q: %v", tag, err)
		}
		if !utf8.Valid(out) {
			t.Errorf("%q: wrote invalid UTF-8 %q", tag, out)
			continue
		}
		f, err := Parse(out)
		if err != nil {
			t.Errorf("%q: wrote %q, which does not parse: %v", tag, out, err)
			continue
		}
		got := f.Docs[0].Children[0]
		if got.Value != "v" || got.Tag != tag && !(tag == "tag:yaml.org,2002:str" && got.Tag == "") {
			t.Errorf("%q: wrote %q, read tag %q value %q", tag, out, got.Tag, got.Value)
		}
	}
}

func TestScannerRejectsInvalidVerbatimTags(t *testing.T) {
	for _, in := range []string{"!<!<>0", "!<a>b", "!<a b> x", "!<a", "!<> x", "!<café> x"} {
		if _, err := Parse([]byte(in)); !errors.Is(err, ErrSyntax) {
			t.Errorf("%q: got %v, want a SyntaxError", in, err)
		}
	}
	for in, want := range map[string]string{
		"!<tag:example.com,2000:a> x": "tag:example.com,2000:a",
		"[!<!a>, b]":                  "!a",
		"{!<!a> : b}":                 "!a",
		"!a%21b x":                    "!a!b",
	} {
		f, err := Parse([]byte(in))
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		n := f.Docs[0].Children[0]
		for n.Tag == "" && len(n.Children) > 0 {
			n = n.Children[0]
		}
		if n.Tag != want {
			t.Errorf("%q: tag %q, want %q", in, n.Tag, want)
		}
	}
}

func TestCommentTextIsMadePrintable(t *testing.T) {
	for _, text := range []string{"a\x01b", "bad \xff utf8", "del\x7f", "bom\ufeff", "tab\tok"} {
		for _, kyaml := range []bool{false, true} {
			opts := []EncodeOption{WithComment(map[string][]Comment{"a": {
				{Position: HeadCommentPos, Text: text},
				{Position: LineCommentPos, Text: text},
				{Position: FootCommentPos, Text: text},
			}})}
			if kyaml {
				opts = append(opts, WithKYAML())
			}
			out, err := MarshalWithOptions(map[string]int{"a": 1}, opts...)
			if err != nil {
				t.Fatal(err)
			}
			var back map[string]int
			if err := Unmarshal(out, &back); err != nil || back["a"] != 1 {
				t.Errorf("%q kyaml=%v: wrote %q, read %v, %v", text, kyaml, out, back, err)
			}
		}
	}
	out, err := MarshalWithOptions(map[string]int{"a": 1}, WithComment(map[string][]Comment{"a": {{Position: HeadCommentPos, Text: "tab\tok"}}}))
	if err != nil || !bytes.Contains(out, []byte("# tab\tok")) {
		t.Errorf("tab: got %q, %v", out, err)
	}
}

func TestNodeToBytesRejectsUnwritableAnchorNames(t *testing.T) {
	for _, name := range []string{"a b", "a\x00", "a,b", "a]", "\xff", "a\tb", "a\nb", "a\x7f", "a\ufffe"} {
		anchored := &Node{Kind: MappingNode, Children: []*Node{
			{Kind: ScalarNode, Value: "k"},
			{Kind: ScalarNode, Value: "1", Anchor: name},
		}}
		if _, err := NodeToBytes(anchored); err == nil {
			t.Errorf("anchor %q: no error", name)
		}
		if err := anchored.Validate(); err == nil {
			t.Errorf("anchor %q: Validate passed", name)
		}
		alias := &Node{Kind: SequenceNode, Children: []*Node{{Kind: AliasNode, Alias: name}}}
		if _, err := NodeToBytes(alias); err == nil {
			t.Errorf("alias %q: no error", name)
		}
	}
	ok := &Node{Kind: SequenceNode, Children: []*Node{
		{Kind: ScalarNode, Value: "1", Anchor: "a:b/é"},
		{Kind: AliasNode, Alias: "a:b/é"},
	}}
	out, err := NodeToBytes(ok)
	if err != nil {
		t.Fatal(err)
	}
	var back []int
	if err := Unmarshal(out, &back); err != nil || !reflect.DeepEqual(back, []int{1, 1}) {
		t.Errorf("wrote %q, read %v, %v", out, back, err)
	}
}

func TestCyclicNodeTree(t *testing.T) {
	m := &Node{Kind: MappingNode}
	m.Children = []*Node{{Kind: ScalarNode, Value: "self"}, m}
	if err := m.Validate(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Validate: got %v", err)
	}
	if _, err := NodeToBytes(m); !errors.Is(err, ErrUnsupported) {
		t.Errorf("NodeToBytes: got %v", err)
	}
	if got := Filter(m, func(*Node) bool { return true }); len(got) != 2 {
		t.Errorf("Filter visited %d nodes", len(got))
	}
	p, err := PathString("$..self")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.Read(m); err != nil || len(got) != 1 {
		t.Errorf("Path: got %v, %v", got, err)
	}

	// A node shared by two parents is not a cycle.
	shared := &Node{Kind: ScalarNode, Value: "x"}
	seq := &Node{Kind: SequenceNode, Children: []*Node{shared, shared}}
	if err := seq.Validate(); err != nil {
		t.Errorf("shared node: %v", err)
	}
	if out, err := NodeToBytes(seq); err != nil || string(out) != "- x\n- x" {
		t.Errorf("shared node: got %q, %v", out, err)
	}

	deep := &Node{Kind: ScalarNode, Value: "x"}
	for range maxNestingDepth + 1 {
		deep = &Node{Kind: SequenceNode, Children: []*Node{deep}}
	}
	if _, err := NodeToBytes(deep); !errors.Is(err, ErrUnsupported) {
		t.Errorf("deep: got %v", err)
	}
}

func TestRequiredFieldErrorWithoutPosition(t *testing.T) {
	var r struct {
		Name string `yaml:"name,required"`
	}
	for _, in := range []string{"", "# comment only\n"} {
		err := Unmarshal([]byte(in), &r)
		if !errors.Is(err, ErrRequiredField) || err.Error() != `yaml: required field "name" is missing` {
			t.Errorf("%q: got %v", in, err)
		}
	}
	if err := Unmarshal([]byte("other: 1"), &r); err == nil || err.Error() != `yaml: line 1, column 1: required field "name" is missing` {
		t.Errorf("positioned: got %v", err)
	}
}

func TestReferenceDirFilesAreTheCheckedTargets(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("a: &x 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.yaml")); err != nil {
		t.Skip(err)
	}
	files, err := referenceDirFiles(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != realTarget {
		t.Errorf("files = %v, want [%s]", files, realTarget)
	}
}
