// Package yaml implements YAML 1.2.2 encoding and decoding.
//
// The API follows the conventions of [encoding/json]: use [Marshal] and
// [Unmarshal] for one-shot conversions, [Encoder] and [Decoder] for streaming,
// and struct field tags to control mapping between YAML keys and Go fields.
//
// For low-level AST access, [Parse] returns a [File] containing [Node] trees
// that can be inspected, mutated with [Path] queries, and re-serialized with
// [NodeToBytes].
//
// # Struct Tags
//
// Struct fields may be annotated with "yaml" tags:
//
//	type Config struct {
//	    Name    string `yaml:"name"`
//	    Count   int    `yaml:"count,omitempty"`
//	    Ignored string `yaml:"-"`
//	}
//
// The tag format is "keyname,opts" where opts is a comma-separated list of:
//   - omitempty: omit the field if it has its zero value
//   - inline: inline the struct's fields into the parent mapping
//   - flow: encode the field in flow style (e.g. [a, b] or {k: v})
//   - required: return an error during decoding if the key is absent
//
// A tag of "-" excludes the field from encoding and decoding.
//
// # Custom Marshalers
//
// Types can implement [Marshaler], [BytesMarshaler], [MarshalerContext],
// [Unmarshaler], [BytesUnmarshaler], or [UnmarshalerContext] for custom
// serialization logic.
//
// # Error Handling
//
// Decoding errors are returned as typed values that support [errors.Is]:
//
//	if errors.Is(err, yaml.ErrSyntax) { ... }
//	if errors.Is(err, yaml.ErrDuplicateKey) { ... }
//
// Use [FormatError] to produce a human-readable error with a source pointer.
package yaml
