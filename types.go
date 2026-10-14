package yaml

import (
	"fmt"
	"math/big"
	"strings"
)

// MapSlice is an ordered slice of key-value pairs. It is used as the
// decoded representation of YAML mappings when [WithOrderedMap] is enabled,
// preserving the original key order that a plain map[string]any would lose.
// It encodes as a mapping with its items in order, as does any other slice
// of [MapItem].
// A MapSlice target (or *MapSlice, field or element) decodes the same way
// without the option, its nested mappings becoming MapSlice too. Merge keys
// (<<) add the merged entries where the merge appears; an explicit key that
// follows replaces a merged value in place.
type MapSlice []MapItem

// MapItem is a single key-value pair within a [MapSlice].
type MapItem struct {
	Key   any
	Value any
}

// RawValue is raw YAML that has not been decoded. Use it to delay decoding
// or to pass a YAML value through without interpreting it. Analogous to
// [encoding/json.RawMessage].
//
// During decoding, the value's node is re-encoded as block-style YAML and
// those bytes are stored, as with any [BytesUnmarshaler]; the source text is
// not kept verbatim, so tags, anchors, aliases, comments and unneeded quotes
// are lost. During encoding in default mode the bytes are emitted as-is.
// Under KYAML mode they are re-parsed and re-emitted as canonical KYAML, so
// non-KYAML constructs cannot leak into the output.
type RawValue []byte

// MarshalYAML returns the raw bytes verbatim, or "null" when r is empty.
// Implements [BytesMarshaler].
func (r RawValue) MarshalYAML() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}

// UnmarshalYAML stores the raw bytes for later decoding.
// Implements [BytesUnmarshaler].
func (r *RawValue) UnmarshalYAML(data []byte) error {
	*r = append((*r)[:0], data...)
	return nil
}

// Number is a number too large or too small for int64, uint64 or float64.
// Decoding into any produces a Number for an integer above the uint64 range
// or below the int64 range, or a float beyond the float64 range, rather
// than rounding it, so that no digit is lost. Its text is the number as
// written, such as "18446744073709551616", "0x1ffffffffffffffff" or
// "1e400"; an !!int written in a form outside the Core schema, such as
// 0X1F…, is given in decimal.
//
// A Number is encoded as the number it holds: [Marshal] writes its text
// unquoted, and JSON and KYAML output write it as a JSON number, in
// decimal for an integer. A Number whose text is not a YAML number is
// encoded as a string. Analogous to [encoding/json.Number].
type Number string

// String returns the number's text.
func (n Number) String() string { return string(n) }

// MarshalJSON writes the number as a JSON number literal: an integer in
// decimal, and a float with its sign, digits and exponent in JSON form. It
// fails when the text is not a YAML number. Implements [encoding/json.Marshaler].
func (n Number) MarshalJSON() ([]byte, error) {
	s, ok := numberJSON(string(n))
	if !ok {
		return nil, fmt.Errorf("%w %q", errInvalidNumber, string(n))
	}
	return []byte(s), nil
}

// isNumberText reports whether s is a YAML Core schema integer or decimal
// float, so that it reads back as a number when written plain.
func isNumberText(s string) bool {
	return s != "" && (coreIntSyntax(s) || isDecimalFloat(s))
}

// numberJSON returns the JSON literal for the number s, a YAML Core schema
// integer or decimal float, reporting false when s is neither. An integer
// is written in decimal; a float keeps its digits and exponent, without a
// leading '+', leading zeros or an empty fraction.
func numberJSON(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	if coreIntSyntax(s) {
		digits, base := s, 10
		switch {
		case strings.HasPrefix(s, "0x"):
			digits, base = s[2:], 16
		case strings.HasPrefix(s, "0o"):
			digits, base = s[2:], 8
		}
		z, ok := new(big.Int).SetString(digits, base)
		if !ok {
			return "", false
		}
		return z.String(), true
	}
	if !isDecimalFloat(s) {
		return "", false
	}
	var b strings.Builder
	b.Grow(len(s) + 2)
	if s[0] == '-' {
		b.WriteByte('-')
	}
	if s[0] == '-' || s[0] == '+' {
		s = s[1:]
	}
	mant, exp := s, ""
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mant, exp = s[:i], s[i:]
	}
	whole, frac, _ := strings.Cut(mant, ".")
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	b.WriteString(whole)
	if frac != "" {
		b.WriteByte('.')
		b.WriteString(frac)
	}
	b.WriteString(exp)
	return b.String(), true
}
