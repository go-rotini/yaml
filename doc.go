// Package yaml implements YAML 1.2.2 encoding and decoding, plus support for
// KYAML, the strict YAML subset defined by Kubernetes [KEP-5295].
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
// KYAML is a strict subset of YAML: every KYAML document is valid YAML, but
// only a small fraction of YAML is valid KYAML. KYAML output uses flow style
// exclusively, double-quotes every string value, quotes type-ambiguous keys
// (the "Norway problem"), and emits a leading "---" document header. Use
// [MarshalKYAML] and [UnmarshalKYAML] for the convenience API, or compose
// [WithKYAML] (encoder) and [WithStrictKYAML] (decoder) with the existing
// option machinery.
//
// [Format] re-emits arbitrary YAML as canonical KYAML; anchors and aliases
// are reified, merge keys are resolved, and explicit tags are stripped.
// [ValidKYAML] and [ValidateKYAML] classify input as conformant or not, and
// [Lint] reports every deviation as a list of issues.
//
// # KYAML rules
//
// [ValidateKYAML], [UnmarshalKYAML] and [Lint] report these violations, each
// as a [KYAMLViolation] or [LintIssue] whose Rule field identifies the rule:
//
//   - the document does not begin with the "---" header
//   - a mapping or sequence is in block style
//   - a mapping key is not a string scalar
//   - a type-ambiguous key is not double-quoted
//   - a boolean or null is not lowercase true/false/null
//   - a string value is plain (unquoted) or single-quoted
//   - anchors or aliases (& *)
//   - explicit tags (!! !)
//   - merge keys (<<)
//   - block scalars (| >)
//   - YAML directives (%YAML %TAG)
//   - hex, octal or binary integer literals
//   - unquoted YAML 1.1 boolean words (yes no on off)
//   - NaN or infinity literals (.nan .inf)
//
// With [WithKYAMLLintCosmetic], [Lint] also warns when the input differs from
// the output of [Format].
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
//   - inline: merge the struct's fields, or the map's entries, into the parent
//     mapping; a map key that names a struct field is left out when encoding,
//     and keys that name no field are decoded into the map
//   - flow: encode the field in flow style (e.g. [a, b] or {k: v})
//   - required: return an error during decoding if the key is absent
//   - default=<value>: set field to <value> during decoding if the key is absent
//     (requires [WithDefaults]; scalar types only)
//
// A tag of "-" excludes the field from encoding and decoding. A struct with
// no fields left to write is encoded as an empty mapping ({}).
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
