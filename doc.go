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
// [ValidKYAML] and [ValidateKYAML] classify input as conformant or not. [Lint]
// reports every conformance deviation, with optional cosmetic checks via
// [WithKYAMLLintCosmetic].
//
// # KYAML rule reference
//
// Validation errors from [ValidateKYAML] and [UnmarshalKYAML] carry rule IDs
// from the KYAML specification:
//
//   - R3.1   Document must begin with the "---" header
//   - R4.1   Mappings must be in flow style ({...})
//   - R4.4   Mapping keys must be string scalars
//   - R5     Type-ambiguous keys must be double-quoted
//   - R6.1   Booleans render as lowercase true/false
//   - R6.3   Null renders as lowercase "null"
//   - R6.4   String values must be double-quoted
//   - R7.1   Sequences must be in flow style ([...])
//   - R12.1  Anchors and aliases not allowed (& *)
//   - R12.2  Explicit tags not allowed (!! !)
//   - R12.3  Merge keys not allowed (<<)
//   - R12.4  Block-style scalars not allowed (| >)
//   - R12.5  Block-style mappings not allowed
//   - R12.6  Block-style sequences not allowed
//   - R12.7  Plain (unquoted) string values not allowed
//   - R12.8  Single-quoted scalars not allowed
//   - R12.9  YAML directives not allowed (%YAML %TAG)
//   - R12.11 Hex/octal/binary integer literals not allowed
//   - R12.12 YAML 1.1 boolean aliases must be quoted (yes no on off)
//   - R12.13 NaN and infinity literals not allowed (.nan .inf)
//
// See [KEP-5295] for the full specification.
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
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
package yaml
