package yaml

import (
	"errors"
	"math"
	"strings"
	"testing"
)

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

// TestKYAMLEncodeStringEscapes verifies R6.5 escape handling.
func TestKYAMLEncodeStringEscapes(t *testing.T) {
	cases := map[string]string{
		"newline":  "a\nb",
		"tab":      "a\tb",
		"quote":    `a"b`,
		"slash":    `a\b`,
		"control":  string([]byte{0x01}),
		"empty":    "",
		"unicode":  "日本語",
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
