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
	indent           int
	lineWidth        int
	indentSequence   bool
	flow             bool
	jsonCompat       bool
	useLiteral       bool
	useSingleQuote   bool
	omitEmpty        bool
	autoInt          bool
	comments         map[string][]Comment
	customMarshalers map[reflect.Type]any
}

func defaultEncodeOptions() *encoderOptions {
	return &encoderOptions{
		indent:         2,
		lineWidth:      80,
		indentSequence: false,
	}
}

// Indent sets the number of spaces per indentation level (default 2).
func Indent(n int) EncodeOption {
	return func(o *encoderOptions) { o.indent = n }
}

// IndentSequence controls whether sequence items are indented relative to
// their parent key. When false (the default), the "- " prefix aligns with
// the parent's indentation level.
func IndentSequence(b bool) EncodeOption {
	return func(o *encoderOptions) { o.indentSequence = b }
}

// Flow encodes all values in flow style (JSON-like inline notation)
// when set to true.
func Flow(b bool) EncodeOption {
	return func(o *encoderOptions) { o.flow = b }
}

// JSON enables JSON-compatible output: all strings are double-quoted, keys
// are quoted, and null is used instead of YAML's tilde notation.
func JSON(b bool) EncodeOption {
	return func(o *encoderOptions) { o.jsonCompat = b }
}

// UseLiteralStyleIfMultiline encodes multi-line strings as YAML literal
// block scalars (|) instead of quoted scalars when set to true.
func UseLiteralStyleIfMultiline(b bool) EncodeOption {
	return func(o *encoderOptions) { o.useLiteral = b }
}

// UseSingleQuote prefers single-quoted scalars over double-quoted when
// the value contains no characters requiring escape sequences.
func UseSingleQuote(b bool) EncodeOption {
	return func(o *encoderOptions) { o.useSingleQuote = b }
}

// OmitEmpty omits struct fields and map entries whose values are zero/empty,
// equivalent to adding ",omitempty" to every field tag.
func OmitEmpty(b bool) EncodeOption {
	return func(o *encoderOptions) { o.omitEmpty = b }
}

// WithComment attaches comments to nodes by dot-path key (e.g. "server.port").
// Each key maps to a slice of [Comment] values specifying position and text.
func WithComment(comments map[string][]Comment) EncodeOption {
	return func(o *encoderOptions) { o.comments = comments }
}

// AutoInt encodes float64 values that have no fractional part as integers
// (e.g. 42 instead of 42.0).
func AutoInt(b bool) EncodeOption {
	return func(o *encoderOptions) { o.autoInt = b }
}

// LineWidth sets the preferred line width for scalar wrapping (default 80).
// The encoder may exceed this width when a single word is longer than the limit.
func LineWidth(n int) EncodeOption {
	return func(o *encoderOptions) { o.lineWidth = n }
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
	disallowDuplicates bool
	allowDuplicates    bool
	useOrderedMap      bool
	useJSONUnmarshaler bool
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

// Strict causes decoding to return an [UnknownFieldError] if a YAML key does
// not correspond to any field in the target struct.
func Strict() DecodeOption {
	return func(o *decoderOptions) { o.strict = true }
}

// DisallowDuplicateKey causes decoding to return a [DuplicateKeyError] if a
// mapping contains the same key more than once.
func DisallowDuplicateKey() DecodeOption {
	return func(o *decoderOptions) { o.disallowDuplicates = true }
}

// UseOrderedMap causes decoding into any (interface{}) to produce [MapSlice]
// values for mappings instead of map[string]any, preserving key order.
func UseOrderedMap() DecodeOption {
	return func(o *decoderOptions) { o.useOrderedMap = true }
}

// UseJSONUnmarshaler causes the decoder to try a type's UnmarshalJSON method
// if no YAML-specific unmarshaler is found.
func UseJSONUnmarshaler() DecodeOption {
	return func(o *decoderOptions) { o.useJSONUnmarshaler = true }
}

// MaxDepth limits the nesting depth of the decoded value (default 100).
// Deeply nested documents are rejected with a [SyntaxError].
func MaxDepth(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxDepth = n }
}

// MaxAliasExpansion limits the total number of alias expansions during
// decoding (default 1000). This prevents denial-of-service via
// exponentially expanding aliases (the "billion laughs" attack).
func MaxAliasExpansion(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxAliasExpansion = n }
}

// Validator registers a [StructValidator] that is called after each struct
// is fully decoded.
func Validator(v StructValidator) DecodeOption {
	return func(o *decoderOptions) { o.validator = v }
}

// ReferenceFiles loads the given YAML files and makes their anchors
// available for alias resolution in the primary document.
func ReferenceFiles(files ...string) DecodeOption {
	return func(o *decoderOptions) { o.referenceFiles = append(o.referenceFiles, files...) }
}

// ReferenceDirs loads all .yaml and .yml files in the given directories
// and makes their anchors available for alias resolution. By default only
// the top level of each directory is scanned; use [RecursiveDir] to walk
// subdirectories.
func ReferenceDirs(dirs ...string) DecodeOption {
	return func(o *decoderOptions) { o.referenceDirs = append(o.referenceDirs, dirs...) }
}

// RecursiveDir controls whether [ReferenceDirs] walks subdirectories
// recursively. Symlinks that escape the directory root are rejected.
func RecursiveDir(b bool) DecodeOption {
	return func(o *decoderOptions) { o.recursiveDir = b }
}

// AllowDuplicateMapKey silently accepts duplicate mapping keys, with the
// last value winning. This is the default YAML 1.2.2 behavior; use
// [DisallowDuplicateKey] for stricter handling.
func AllowDuplicateMapKey() DecodeOption {
	return func(o *decoderOptions) { o.allowDuplicates = true }
}

// MaxDocumentSize rejects input that exceeds n bytes before parsing begins.
func MaxDocumentSize(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxDocumentSize = n }
}

// MaxNodes limits the total number of AST nodes the parser may create.
// Zero means no limit.
func MaxNodes(n int) DecodeOption {
	return func(o *decoderOptions) { o.maxNodes = n }
}

// CustomMarshaler registers a function that encodes values of type T to YAML
// bytes, overriding the default encoding for that type.
func CustomMarshaler[T any](fn func(T) ([]byte, error)) EncodeOption {
	return func(o *encoderOptions) {
		if o.customMarshalers == nil {
			o.customMarshalers = make(map[reflect.Type]any)
		}
		o.customMarshalers[reflect.TypeFor[T]()] = fn
	}
}

// CustomUnmarshaler registers a function that decodes YAML bytes into a
// value of type T, overriding the default decoding for that type.
func CustomUnmarshaler[T any](fn func(*T, []byte) error) DecodeOption {
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
