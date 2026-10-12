package yaml

// MapSlice is an ordered slice of key-value pairs. It is used as the
// decoded representation of YAML mappings when [WithOrderedMap] is enabled,
// preserving the original key order that a plain map[string]any would lose.
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
