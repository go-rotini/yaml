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
		indentSequence: false,
	}
}

// WithIndent sets the number of spaces per indentation level (default 2).
func WithIndent(n int) EncodeOption {
	return func(o *encoderOptions) { o.indent = n }
}

// WithIndentSequence controls whether sequence items are indented relative to
// their parent key. When false (the default), the "- " prefix aligns with
// the parent's indentation level.
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
// the value contains no characters requiring escape sequences.
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
func WithComment(comments map[string][]Comment) EncodeOption {
	return func(o *encoderOptions) { o.comments = comments }
}

// WithAutoInt encodes float64 values that have no fractional part as integers
// (e.g. 42 instead of 42.0).
func WithAutoInt(b bool) EncodeOption {
	return func(o *encoderOptions) { o.autoInt = b }
}

// WithLineWidth sets the preferred line width for scalar wrapping (default 80).
// The encoder may exceed this width when a single word is longer than the limit.
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

// WithDurationAsString controls how [time.Duration] values are encoded.
// When false (the default), durations encode as int64 nanoseconds, matching
// [encoding/json]. When true, durations encode using their human-readable
// String() form (e.g. "1h30m") wrapped as a quoted string under KYAML mode.
//
// The string form is more readable for config files; the int64 form is
// stable for machine-to-machine interchange and matches stdlib behavior.
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
	// null (~, null, Null, NULL, empty), bool (true/false in any case),
	// int (decimal, hex 0x, octal 0o), float (including .inf, .nan).
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
// key is absent from the input. Default values are specified with the
// "default=<value>" tag option (e.g. `yaml:"port,default=8080"`).
// Only scalar types are supported: string, bool, int/uint variants, float
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
// values for mappings instead of map[string]any, preserving key order.
func WithOrderedMap() DecodeOption {
	return func(o *decoderOptions) { o.useOrderedMap = true }
}

// WithJSONUnmarshaler causes the decoder to try a type's UnmarshalJSON method
// if no YAML-specific unmarshaler is found.
func WithJSONUnmarshaler() DecodeOption {
	return func(o *decoderOptions) { o.useJSONUnmarshaler = true }
}

// WithMaxDepth limits the nesting depth of the decoded value (default 100).
// Deeply nested documents are rejected with a [SyntaxError].
func WithMaxDepth(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxDepth = n }
}

// WithMaxAliasExpansion limits the total number of alias expansions during
// decoding (default 1000). This prevents denial-of-service via
// exponentially expanding aliases (the "billion laughs" attack).
func WithMaxAliasExpansion(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxAliasExpansion = n }
}

// WithValidator registers a [StructValidator] that is called after each struct
// is fully decoded.
func WithValidator(v StructValidator) DecodeOption {
	return func(o *decoderOptions) { o.validator = v }
}

// WithReferenceFiles loads the given YAML files and makes their anchors
// available for alias resolution in the primary document.
func WithReferenceFiles(files ...string) DecodeOption {
	return func(o *decoderOptions) { o.referenceFiles = append(o.referenceFiles, files...) }
}

// WithReferenceDirs loads all .yaml and .yml files in the given directories
// and makes their anchors available for alias resolution. By default only
// the top level of each directory is scanned; use [WithRecursiveDir] to walk
// subdirectories.
func WithReferenceDirs(dirs ...string) DecodeOption {
	return func(o *decoderOptions) { o.referenceDirs = append(o.referenceDirs, dirs...) }
}

// WithRecursiveDir controls whether [WithReferenceDirs] walks subdirectories
// recursively. Symlinks that escape the directory root are rejected.
func WithRecursiveDir(b bool) DecodeOption {
	return func(o *decoderOptions) { o.recursiveDir = b }
}

// WithMaxDocumentSize rejects input that exceeds n bytes before parsing begins.
func WithMaxDocumentSize(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxDocumentSize = n }
}

// WithMaxNodes limits the total number of AST nodes the parser may create.
// Zero means no limit.
func WithMaxNodes(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxNodes = n }
}

// WithCustomMarshaler registers a function that encodes values of type T to YAML
// bytes, overriding the default encoding for that type.
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
// positions. Use [errors.Is](err, [ErrKYAML]) to test generically.
//
// See [KEP-5295] for the full KYAML format definition.
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
func WithStrictKYAML() DecodeOption {
	return func(o *decoderOptions) { o.strictKYAML = true }
}

// WithKYAMLLintCosmetic additionally enforces the cosmetic rules of KYAML:
// indentation, bracket cuddling, trailing commas, key ordering, and key
// quoting. Used primarily by linters that want to assert byte-equivalence
// with kubectl's KYAML output.
//
// Has no effect unless [WithStrictKYAML] is also set.
func WithKYAMLLintCosmetic() DecodeOption {
	return func(o *decoderOptions) { o.kyamlLintCosmetic = true }
}
