package yaml

import "reflect"

// EncodeOption configures the behavior of [Marshal], [MarshalWithOptions],
// and [Encoder].
type EncodeOption func(*encoderOptions)

// Comment attaches a comment to a YAML node identified by key path when
// encoding with [WithComment].
type Comment struct {
	Position CommentPosition
	Text     string
}

// CommentPosition specifies where a [Comment] appears relative to its node.
type CommentPosition int

const (
	HeadCommentPos CommentPosition = iota // before the node
	LineCommentPos                        // on the same line, after the value
	FootCommentPos                        // after the node
)

type encoderOptions struct {
	indent               int
	lineWidth            int
	indentSequence       bool
	flow                 bool
	flowExplicit         bool // true if WithFlow was called explicitly
	jsonCompat           bool
	useLiteral           bool
	useSingleQuote       bool
	quoteAll             bool
	omitEmpty            bool
	autoInt              bool
	durationAsString     bool
	comments             map[string][]Comment
	customMarshalers     map[reflect.Type]any
	kyaml                bool
	kyamlAlwaysQuoteKeys bool
}

func defaultEncodeOptions() *encoderOptions {
	return &encoderOptions{
		indent:         2,
		lineWidth:      80,
		indentSequence: true,
	}
}

// WithIndent sets the number of spaces per indentation level (default 2).
func WithIndent(n int) EncodeOption {
	return func(o *encoderOptions) { o.indent = n }
}

// WithIndentSequence controls whether a block sequence under a mapping key is
// indented one level from the key (true, the default) or starts at the
// key's column ("key:" then "- item").
func WithIndentSequence(b bool) EncodeOption {
	return func(o *encoderOptions) { o.indentSequence = b }
}

// WithFlow encodes all values in flow style (JSON-like inline notation)
// when set to true.
func WithFlow(b bool) EncodeOption {
	return func(o *encoderOptions) { o.flow = b; o.flowExplicit = true }
}

// WithJSON enables JSON-compatible output: all strings are double-quoted, keys
// are quoted, and null is used instead of YAML's tilde notation.
func WithJSON(b bool) EncodeOption {
	return func(o *encoderOptions) { o.jsonCompat = b }
}

// WithLiteralStyle encodes multi-line strings as YAML literal
// block scalars (|) instead of quoted scalars when set to true.
func WithLiteralStyle(b bool) EncodeOption {
	return func(o *encoderOptions) { o.useLiteral = b }
}

// WithSingleQuote prefers single-quoted scalars over double-quoted when
// the value contains no single quote or newline.
func WithSingleQuote(b bool) EncodeOption {
	return func(o *encoderOptions) { o.useSingleQuote = b }
}

// WithQuoteAllStrings forces all string scalar values to be quoted when set to true.
// Mapping keys remain unquoted unless they require quoting for syntactic reasons.
func WithQuoteAllStrings(b bool) EncodeOption {
	return func(o *encoderOptions) { o.quoteAll = b }
}

// WithOmitEmpty omits struct fields and map entries whose values are zero/empty,
// equivalent to adding ",omitempty" to every field tag.
func WithOmitEmpty(b bool) EncodeOption {
	return func(o *encoderOptions) { o.omitEmpty = b }
}

// WithComment attaches comments to nodes by dot-path key (e.g. "server.port").
// Each key maps to a slice of [Comment] values specifying position and text.
// A path names the entry from the root, with "." between keys and "[i]" for
// a sequence item (e.g. "servers[0].port"); a leading "$" or "$." is
// allowed. Comments are placed at the entry itself, never inside a scalar:
// a head comment on the line before it, a line comment at the end of its
// first line, and a foot comment after its value. A path that names no entry
// falls back to the first entry whose last segment matches, and is otherwise
// not placed.
func WithComment(comments map[string][]Comment) EncodeOption {
	return func(o *encoderOptions) { o.comments = comments }
}

// WithAutoInt writes string values that are decimal integers (e.g. "42")
// unquoted, so they decode back as integers rather than strings. It has no
// effect under [WithJSON], [WithQuoteAllStrings] or [WithKYAML]. The
// original text is not kept: "+42" and "042" both decode back as 42.
func WithAutoInt(b bool) EncodeOption {
	return func(o *encoderOptions) { o.autoInt = b }
}

// WithLineWidth sets the line width (default 80) above which [WithKYAML]
// output may write a string with two or more newlines in flow-folded form, one
// source line per output line. Zero or less disables folding. Other output
// is not wrapped.
func WithLineWidth(n int) EncodeOption {
	return func(o *encoderOptions) { o.lineWidth = n }
}

// WithKYAML enables KYAML output mode, a strict subset of YAML defined by
// Kubernetes [KEP-5295]. KYAML output:
//
//   - always begins with a "---" document header
//   - uses flow style ({} for mappings, [] for sequences) exclusively
//   - double-quotes every string value with full escape handling
//   - quotes keys only when they are type-ambiguous (the "Norway problem")
//   - emits trailing commas (suppressed where brackets are cuddled)
//   - sorts native map keys lexicographically
//   - prefers the json struct tag and json.Marshaler over their yaml equivalents
//   - never emits anchors, aliases, tags, merge keys, or block-style scalars
//
// KYAML is a subset of YAML 1.2.2, so output is always valid YAML.
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
func WithKYAML() EncodeOption {
	return func(o *encoderOptions) {
		o.kyaml = true
		o.flow = true
	}
}

// WithKYAMLAlwaysQuoteKeys forces double-quoting of every map and struct key
// under KYAML mode. Default is to quote only type-ambiguous keys.
//
// Has no effect unless [WithKYAML] is also set.
func WithKYAMLAlwaysQuoteKeys() EncodeOption {
	return func(o *encoderOptions) { o.kyamlAlwaysQuoteKeys = true }
}

// WithDurationAsString controls how [time.Duration] values are encoded under
// [WithKYAML]. When false (the default), durations encode as int64
// nanoseconds, matching [encoding/json]. When true, they encode as their
// quoted String() form (e.g. "1h30m").
//
// Outside KYAML mode durations always encode in their String() form, and
// this option has no effect.
func WithDurationAsString(b bool) EncodeOption {
	return func(o *encoderOptions) { o.durationAsString = b }
}

// DecodeOption configures the behavior of [Unmarshal], [UnmarshalWithOptions],
// and [Decoder].
type DecodeOption func(*decoderOptions)

// StructValidator validates a struct after all fields have been decoded.
// Implement this interface to integrate with validation libraries.
type StructValidator interface {
	Struct(v any) error
}

// TagResolver maps a custom YAML tag to a Go type and provides a function
// that converts the scalar string value into the target type. Register
// resolvers with [WithTagResolver].
type TagResolver struct {
	Tag     string
	GoType  reflect.Type
	Resolve func(value string) (any, error)
}

// Schema selects the YAML tag resolution schema used when decoding plain
// scalars into interface{}/any values.
type Schema int

const (
	// CoreSchema resolves plain scalars using the YAML 1.2 Core schema:
	// null (~, null, Null, NULL, empty), bool (true, True, TRUE and the same
	// for false), int (decimal, hex 0x, octal 0o), float (decimal with an
	// optional exponent, and .inf, -.inf, .nan in lower, title or upper case).
	// Plain inf, nan and infinity are strings. Scalars tagged !!null, !!bool,
	// !!int or !!float resolve to that type under every schema. Timestamps
	// are not resolved, so a date decodes into any as a string (decode into
	// time.Time for a time), and a !!binary scalar decodes into any as its
	// base64 text (decode into []byte for the bytes).
	CoreSchema Schema = iota

	// JSONSchema resolves plain scalars like JSON: null (lowercase only),
	// true/false (lowercase only), and JSON-format numbers.
	JSONSchema

	// FailsafeSchema treats all plain scalars as strings with no type
	// coercion. Only explicitly tagged values are resolved.
	FailsafeSchema
)

type decoderOptions struct {
	strict             bool
	strictKYAML        bool
	kyamlLintCosmetic  bool
	disallowDuplicates bool
	useOrderedMap      bool
	useJSONUnmarshaler bool
	applyDefaults      bool
	maxDepth           int
	maxAliasExpansion  int
	maxDocumentSize    int
	maxNodes           int
	maxAliasNodes      int
	recursiveDir       bool
	schema             Schema
	validator          StructValidator
	referenceFiles     []string
	referenceDirs      []string
	customUnmarshalers map[reflect.Type]any
	tagResolvers       map[string]*TagResolver
}

func defaultDecodeOptions() *decoderOptions {
	return &decoderOptions{
		maxDepth:          100,
		maxAliasExpansion: 1000,
	}
}

// WithSchema selects the YAML tag resolution schema. The default is
// [CoreSchema]. Use [JSONSchema] for JSON-compatible resolution or
// [FailsafeSchema] to treat all plain scalars as strings.
func WithSchema(s Schema) DecodeOption {
	return func(o *decoderOptions) { o.schema = s }
}

// WithDefaults enables applying default values from struct tags when a YAML
// key is absent from the input and the field still holds its zero value.
// Default values are specified with the "default=<value>" tag option (e.g.
// `yaml:"port,default=8080"`). Only scalar types are supported: string, bool, int/uint variants, float
// variants, and time.Duration. Without this option, default tags are ignored.
func WithDefaults() DecodeOption {
	return func(o *decoderOptions) { o.applyDefaults = true }
}

// WithStrict causes decoding to return an [UnknownFieldError] if a YAML key does
// not correspond to any field in the target struct.
func WithStrict() DecodeOption {
	return func(o *decoderOptions) { o.strict = true }
}

// WithDisallowDuplicateKey causes decoding to return a [DuplicateKeyError] if a
// mapping contains the same key more than once.
func WithDisallowDuplicateKey() DecodeOption {
	return func(o *decoderOptions) { o.disallowDuplicates = true }
}

// WithOrderedMap causes decoding into any (interface{}) to produce [MapSlice]
// values for mappings instead of map[string]any, preserving key order. A
// mapping or sequence used as a key is kept as the [MapItem] key, while
// map[string]any and map[any]any cannot hold one and report a [TypeError].
func WithOrderedMap() DecodeOption {
	return func(o *decoderOptions) { o.useOrderedMap = true }
}

// WithJSONUnmarshaler causes the decoder to try a type's UnmarshalJSON method
// if no YAML-specific unmarshaler is found.
func WithJSONUnmarshaler() DecodeOption {
	return func(o *decoderOptions) { o.useJSONUnmarshaler = true }
}

// WithMaxDepth limits the nesting depth of the decoded value (default 100).
// Deeper documents are rejected with a [SyntaxError]. Zero or less means no
// limit.
func WithMaxDepth(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxDepth = n }
}

// WithMaxAliasExpansion limits how deeply alias expansions may nest during
// decoding (default 1000): an alias inside an anchored value that is itself
// being expanded counts one level deeper. Exceeding the limit, which a
// cyclic alias always does, fails with a [CycleError]. The limit applies to
// nesting depth; [WithMaxAliasNodes] limits the total number of expanded
// nodes.
func WithMaxAliasExpansion(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxAliasExpansion = n }
}

// WithMaxAliasNodes limits the total number of nodes that alias expansion may
// produce while decoding one document; exceeding it fails with an
// [AliasExpansionError]. This stops "billion laughs" documents, where a few
// nested aliases expand to an enormous value. The default, also selected by
// zero, scales with the input: 100 times the number of nodes parsed
// (including reference files), and at least 10,000. A negative n means no
// limit.
func WithMaxAliasNodes(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxAliasNodes = n }
}

// WithValidator registers a [StructValidator] that is called after each struct
// is fully decoded.
func WithValidator(v StructValidator) DecodeOption {
	return func(o *decoderOptions) { o.validator = v }
}

// WithReferenceFiles loads the given YAML files and makes their anchors
// available for alias resolution in the primary document. An anchor name
// defined in more than one place fails with a [DuplicateKeyError]. With a
// [Decoder], the anchors are available to every document in the stream.
func WithReferenceFiles(files ...string) DecodeOption {
	return func(o *decoderOptions) { o.referenceFiles = append(o.referenceFiles, files...) }
}

// WithReferenceDirs loads all .yaml and .yml files in the given directories
// and makes their anchors available for alias resolution, as
// [WithReferenceFiles] does. By default only the top level of each directory
// is scanned; use [WithRecursiveDir] to walk subdirectories. A file whose
// symlink resolves outside the directory fails with [ErrPathEscape].
func WithReferenceDirs(dirs ...string) DecodeOption {
	return func(o *decoderOptions) { o.referenceDirs = append(o.referenceDirs, dirs...) }
}

// WithRecursiveDir controls whether [WithReferenceDirs] walks subdirectories
// recursively.
func WithRecursiveDir(b bool) DecodeOption {
	return func(o *decoderOptions) { o.recursiveDir = b }
}

// WithMaxDocumentSize rejects input that exceeds n bytes before parsing
// begins, with an error wrapping [ErrDocumentSize]. Zero means no limit.
func WithMaxDocumentSize(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxDocumentSize = n }
}

// WithMaxNodes limits the total number of AST nodes the parser may create;
// exceeding it fails with a [SyntaxError]. Zero, the default, means no limit.
func WithMaxNodes(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxNodes = n }
}

// WithCustomMarshaler registers a function that encodes values of type T to YAML
// bytes, overriding the default encoding for that type. The bytes are
// handled as for [BytesMarshaler].
func WithCustomMarshaler[T any](fn func(T) ([]byte, error)) EncodeOption {
	return func(o *encoderOptions) {
		if o.customMarshalers == nil {
			o.customMarshalers = make(map[reflect.Type]any)
		}
		o.customMarshalers[reflect.TypeFor[T]()] = fn
	}
}

// WithCustomUnmarshaler registers a function that decodes YAML bytes into a
// value of type T, overriding the default decoding for that type.
func WithCustomUnmarshaler[T any](fn func(*T, []byte) error) DecodeOption {
	return func(o *decoderOptions) {
		if o.customUnmarshalers == nil {
			o.customUnmarshalers = make(map[reflect.Type]any)
		}
		o.customUnmarshalers[reflect.TypeFor[T]()] = fn
	}
}

// WithTagResolver registers a [TagResolver] that handles a custom YAML tag
// (e.g. "!mytype") during decoding.
func WithTagResolver(resolver *TagResolver) DecodeOption {
	return func(o *decoderOptions) {
		if o.tagResolvers == nil {
			o.tagResolvers = make(map[string]*TagResolver)
		}
		o.tagResolvers[resolver.Tag] = resolver
	}
}

// WithStrictKYAML rejects YAML constructs that fall outside the KYAML subset:
// anchors, aliases, tags, merge keys, block-style scalars/mappings/sequences,
// plain string scalars (except keys), single-quoted scalars, non-string keys,
// hex/octal/binary numeric literals, YAML 1.1 boolean aliases, .nan/.inf
// floats, the ? complex-key indicator, and YAML directives. Documents must
// begin with the "---" header.
//
// Errors are returned as [*KYAMLError] carrying every violation with source
// positions. Use [errors.Is](err, [ErrKYAML]) to test generically. A
// [Decoder] checks the whole stream on its first Decode call.
//
// See [KEP-5295] for the full KYAML format definition.
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
func WithStrictKYAML() DecodeOption {
	return func(o *decoderOptions) { o.strictKYAML = true }
}

// WithKYAMLLintCosmetic makes [Lint] also check the cosmetic rules of KYAML
// (indentation, bracket cuddling, trailing commas, key ordering and key
// quoting) by reporting a [SeverityWarning] issue when the input differs
// from the output of [Format].
//
// It affects only [Lint]; decoding ignores it.
func WithKYAMLLintCosmetic() DecodeOption {
	return func(o *decoderOptions) { o.kyamlLintCosmetic = true }
}
