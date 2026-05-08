// Package yaml implements YAML 1.2.2 encoding and decoding, plus KYAML — a
// strict YAML subset defined by Kubernetes [KEP-5295].
//
// The API follows the conventions of [encoding/json]: use [Marshal] and
// [Unmarshal] for one-shot conversions, [Encoder] and [Decoder] for streaming,
// and struct field tags to control mapping between YAML keys and Go fields.
//
// For low-level AST access, [Parse] returns a [File] containing [Node] trees
// that can be inspected, mutated with [Path] queries, and re-serialized with
// [NodeToBytes].
//
// # KYAML mode
//
// KYAML is a strict YAML subset — every KYAML document is valid YAML, but
// only a small fraction of YAML is valid KYAML. KYAML output uses flow style
// exclusively, double-quotes every string value, quotes type-ambiguous keys
// (the "Norway problem"), and emits a leading "---" document header. Use
// [MarshalKYAML] and [UnmarshalKYAML] for the convenience API, or compose
// [WithKYAML] (encoder) and [WithStrictKYAML] (decoder) with the existing
// option machinery.
//
// [Format] re-emits arbitrary YAML as canonical KYAML; anchors and aliases
// are reified, merge keys are resolved, and explicit tags are stripped.
// [IsKYAML] and [ValidateKYAML] classify input as conformant or not. [Lint]
// reports every conformance deviation, with optional cosmetic checks via
// [WithKYAMLLintCosmetic].
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
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
//   - default=<value>: set field to <value> during decoding if the key is absent
//     (requires [WithDefaults]; scalar types only)
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
