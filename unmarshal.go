package yaml

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

// Unmarshal parses YAML data and stores the first document into the value
// pointed to by v. If v is nil or not a pointer, Unmarshal returns an error.
//
// A number that does not fit the target type is reported in a [TypeError]
// rather than wrapped or rounded; into any, numbers decode as int64, uint64
// or float64, and one beyond those ranges as a [Number] (see
// [CoreSchema]). An alias must follow its anchor in the same document, or
// name an anchor of a reference file. Mapping keys decoded into any keep
// their source text, so "1" and "0x1" are different keys; an alias key
// takes the text of the scalar it refers to.
func Unmarshal(data []byte, v any) error {
	return UnmarshalWithOptions(data, v)
}

// UnmarshalTo parses YAML data into a value of type T and returns it.
// This is a generic alternative to [Unmarshal] that allocates the target
// value internally, removing the need for the caller to declare a variable
// and pass its address.
func UnmarshalTo[T any](data []byte, opts ...DecodeOption) (T, error) {
	var v T
	if err := UnmarshalWithOptions(data, &v, opts...); err != nil {
		var zero T
		return zero, err
	}
	return v, nil
}

// UnmarshalKYAML is shorthand for [UnmarshalWithOptions](data, v, [WithStrictKYAML]()).
// It parses data as strict KYAML and decodes the first document into v;
// non-KYAML constructs (anchors, aliases, tags, merge keys, block-style
// scalars/mappings/sequences, plain string values, single-quoted scalars,
// non-string keys, hex/octal/binary numeric literals, YAML 1.1 boolean
// aliases, .nan/.inf, the ? complex-key indicator, and YAML directives)
// produce a [*KYAMLError].
func UnmarshalKYAML(data []byte, v any) error {
	return UnmarshalWithOptions(data, v, WithStrictKYAML())
}

// UnmarshalKYAMLWithOptions is shorthand for
// [UnmarshalWithOptions](data, v, append(opts, [WithStrictKYAML]())...).
func UnmarshalKYAMLWithOptions(data []byte, v any, opts ...DecodeOption) error {
	return UnmarshalWithOptions(data, v, append(opts, WithStrictKYAML())...)
}

// UnmarshalKYAMLTo is the generic form of [UnmarshalKYAML], returning a value
// of type T. Useful when the caller does not want to declare and pass a
// pointer to a target variable.
func UnmarshalKYAMLTo[T any](data []byte, opts ...DecodeOption) (T, error) {
	var v T
	if err := UnmarshalKYAMLWithOptions(data, &v, opts...); err != nil {
		var zero T
		return zero, err
	}
	return v, nil
}

// DecodeKYAMLFile reads path, validates as strict KYAML, and decodes the first
// document into v.
func DecodeKYAMLFile(path string, v any, opts ...DecodeOption) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("yaml: read file %q: %w", path, err)
	}
	return UnmarshalKYAMLWithOptions(data, v, opts...)
}

// UnmarshalWithOptions parses YAML data into v, applying the given
// [DecodeOption] values to control strictness, limits, and custom resolvers.
func UnmarshalWithOptions(data []byte, v any, opts ...DecodeOption) error {
	o := defaultDecodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("yaml: unmarshal requires a non-nil pointer, got %T: %w", v, ErrNilPointer)
	}

	docs, refAnchors, nodes, err := parseStream(data, o)
	if err != nil {
		return err
	}
	return decodeFirst(docs, refAnchors, nodes, o, rv)
}

// decodeFirst decodes the first of the parsed docs into the value non-nil
// pointer rv points to. refAnchors and nodes are as parseStream returns
// them.
func decodeFirst(docs []*node, refAnchors map[string]*node, nodes int, o *decoderOptions, rv reflect.Value) error {
	d := newDecoder(o)
	d.refAnchors = refAnchors
	d.aliasLimit = aliasNodeLimit(o, nodes)

	if len(docs) == 0 {
		// An empty stream holds no value, but a struct target still gets
		// its defaults and required checks.
		return d.fillAbsent(derefTarget(rv.Elem()), Position{})
	}

	if err := d.decode(docs[0], rv.Elem()); err != nil {
		return err
	}

	if len(d.typeErrors) > 0 {
		return &TypeError{Errors: d.typeErrors}
	}

	return nil
}

// derefTarget follows non-nil pointers from v to the value they point to.
func derefTarget(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	return v
}

// parseStream applies the size limit, scans and parses data, validates it
// when strict KYAML is on, and loads reference anchors. It returns the
// documents, the anchors loaded from reference files, and the number of
// nodes parsed (input plus reference files).
func parseStream(data []byte, o *decoderOptions) ([]*node, map[string]*node, int, error) {
	if o.maxDocumentSize > 0 && len(data) > o.maxDocumentSize {
		return nil, nil, 0, fmt.Errorf("yaml: document size %d exceeds maximum %d: %w", len(data), o.maxDocumentSize, ErrDocumentSize)
	}

	data, err := detectAndConvertEncoding(data)
	if err != nil {
		return nil, nil, 0, err
	}

	tokens, err := newScanner(data).scan()
	if err != nil {
		return nil, nil, 0, err
	}

	if o.strictKYAML {
		for _, tok := range tokens {
			if tok.kind == tokenDirective {
				return nil, nil, 0, &KYAMLError{Errors: []KYAMLViolation{{
					Rule:    "R12.9",
					Message: fmt.Sprintf("YAML directive %q not allowed in KYAML", tok.value),
					Pos:     tok.pos,
					Token:   tok.value,
				}}}
			}
		}
	}

	p := newParser(tokens)
	p.maxNodes = o.maxNodes
	docs, err := p.parse()
	if err != nil {
		return nil, nil, 0, err
	}

	if o.strictKYAML {
		if err := validateKYAMLBytes(docs); err != nil {
			return nil, nil, 0, err
		}
	}

	refAnchors, refNodes, err := loadReferences(p.anchors, o)
	if err != nil {
		return nil, nil, 0, err
	}

	return docs, refAnchors, p.nodeCount + refNodes, nil
}

// aliasNodeLimit returns the alias expansion node limit for an input of
// parsed nodes, or zero for no limit.
func aliasNodeLimit(o *decoderOptions, parsed int) int {
	switch {
	case o.maxAliasNodes > 0:
		return o.maxAliasNodes
	case o.maxAliasNodes < 0:
		return 0
	}
	return max(10_000, 100*parsed)
}

// Decoder reads and decodes YAML documents from an input stream. Use
// successive calls to [Decoder.Decode] to iterate over a multi-document
// stream; it returns [io.EOF] when no more documents remain.
//
// The entire input is consumed from the reader on the first call to Decode;
// with [WithMaxDocumentSize], reading stops as soon as the input exceeds
// the limit.
// For streams where documents arrive incrementally (e.g. from a network
// connection), callers should frame each document and decode individually
// with [Unmarshal].
//
// A Decoder is not safe for concurrent use. Callers that need to decode
// from multiple goroutines must provide their own synchronization.
type Decoder struct {
	r     io.Reader
	opts  *decoderOptions
	docs  []*node
	pos   int
	init  bool
	anch  map[string]*node
	nodes int
	err   error // the read or parse error, returned by every later Decode
}

// NewDecoder returns a new [Decoder] that reads from r.
func NewDecoder(r io.Reader, opts ...DecodeOption) *Decoder {
	o := defaultDecodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	return &Decoder{r: r, opts: o}
}

// Decode reads the next YAML document from the stream and stores it in the
// value pointed to by v. It returns [io.EOF] when the stream is exhausted.
// A stream that cannot be read or parsed yields no documents: Decode returns
// that error, and so does every later call.
func (dec *Decoder) Decode(v any) error {
	return dec.DecodeContext(context.Background(), v)
}

// DecodeContext reads the next YAML document, passing ctx to types that
// implement [UnmarshalerContext].
func (dec *Decoder) DecodeContext(ctx context.Context, v any) error {
	if dec.err != nil {
		return dec.err
	}
	if !dec.init {
		r := dec.r
		if limit := dec.opts.maxDocumentSize; limit > 0 && int64(limit) < math.MaxInt64 {
			r = io.LimitReader(r, int64(limit)+1)
		}
		data, err := io.ReadAll(r)
		if err != nil {
			dec.err = fmt.Errorf("yaml: read input: %w", err)
			return dec.err
		}

		dec.docs, dec.anch, dec.nodes, err = parseStream(data, dec.opts)
		if err != nil {
			dec.err = err
			return err
		}
		dec.init = true
	}

	if dec.pos >= len(dec.docs) {
		return io.EOF
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("yaml: decode requires a non-nil pointer, got %T: %w", v, ErrNilPointer)
	}

	d := newDecoder(dec.opts)
	d.ctx = ctx
	d.refAnchors = dec.anch
	d.aliasLimit = aliasNodeLimit(dec.opts, dec.nodes)

	doc := dec.docs[dec.pos]
	dec.pos++

	if err := d.decode(doc, rv.Elem()); err != nil {
		return err
	}

	if len(d.typeErrors) > 0 {
		return &TypeError{Errors: d.typeErrors}
	}

	return nil
}

// within reports whether path is dir or lies inside it. Both must be clean
// and absolute or relative to the same directory.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// referenceDirFiles returns the .yaml and .yml files in dir, and in its
// subdirectories when recursive is set. A file whose symlink resolves
// outside dir fails with [ErrPathEscape].
func referenceDirFiles(dir string, recursive bool) ([]string, error) {
	realDir, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return nil, fmt.Errorf("yaml: cannot resolve reference dir %q: %w", dir, err)
	}
	var files []string
	add := func(path string) error {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("yaml: cannot resolve symlink %q: %w", path, err)
		}
		if !within(realDir, resolved) {
			return fmt.Errorf("yaml: reference file %q escapes directory %q: %w", resolved, realDir, ErrPathEscape)
		}
		// Read the file checked, not a path a symlink swap could redirect.
		files = append(files, resolved)
		return nil
	}
	isYAML := func(name string) bool {
		return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
	}

	if recursive {
		err = filepath.WalkDir(realDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !isYAML(d.Name()) {
				return nil
			}
			return add(path)
		})
		if err != nil {
			return nil, fmt.Errorf("yaml: walk reference dir: %w", err)
		}
		return files, nil
	}

	entries, err := os.ReadDir(realDir)
	if err != nil {
		return nil, fmt.Errorf("yaml: cannot read reference dir %q: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !isYAML(e.Name()) {
			continue
		}
		if err := add(filepath.Join(realDir, e.Name())); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// loadReferences loads the anchors of the reference files and directories
// in o. It returns them with the number of nodes parsed. An anchor also in
// docAnchors, or in another reference file, fails with a
// [DuplicateKeyError].
func loadReferences(docAnchors map[string]*node, o *decoderOptions) (map[string]*node, int, error) {
	files := slices.Clone(o.referenceFiles)
	nodes := 0
	if len(files) == 0 && len(o.referenceDirs) == 0 {
		return nil, 0, nil
	}
	anchors := make(map[string]*node)

	for _, dir := range o.referenceDirs {
		dirFiles, err := referenceDirFiles(dir, o.recursiveDir)
		if err != nil {
			return nil, 0, err
		}
		files = append(files, dirFiles...)
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, 0, fmt.Errorf("yaml: cannot read reference file %q: %w", file, err)
		}

		data, err = detectAndConvertEncoding(data)
		if err != nil {
			return nil, 0, err
		}

		tokens, err := newScanner(data).scan()
		if err != nil {
			return nil, 0, fmt.Errorf("yaml: error scanning reference file %q: %w", file, err)
		}

		p := newParser(tokens)
		_, err = p.parse()
		if err != nil {
			return nil, 0, fmt.Errorf("yaml: error parsing reference file %q: %w", file, err)
		}
		nodes += p.nodeCount

		for name, node := range p.anchors {
			_, inDoc := docAnchors[name]
			if _, exists := anchors[name]; exists || inDoc {
				return nil, 0, &DuplicateKeyError{
					Key: name,
					Pos: node.pos,
				}
			}
			anchors[name] = node
		}
	}

	return anchors, nodes, nil
}
