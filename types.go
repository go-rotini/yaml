package yaml

// MapSlice is an ordered slice of key-value pairs. It is used as the
// decoded representation of YAML mappings when [WithOrderedMap] is enabled,
// preserving the original key order that a plain map[string]any would lose.
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
// During decoding, the original source bytes spanning the value (including
// any surrounding quotes for scalars) are stored verbatim. During encoding
// in default mode the bytes are emitted as-is. Under KYAML mode the raw
// bytes are re-parsed and re-emitted as canonical KYAML, since pass-through
// could otherwise leak non-KYAML constructs (anchors, tags, etc.) into
// strict-KYAML output (R13.11).
type RawValue []byte

// MarshalYAML returns the raw bytes verbatim. Implements [BytesMarshaler].
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
