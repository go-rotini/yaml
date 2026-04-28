package yaml

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// Unmarshal parses YAML data and stores the first document into the value
// pointed to by v. If v is nil or not a pointer, Unmarshal returns an error.
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

	if o.maxDocumentSize > 0 && len(data) > o.maxDocumentSize {
		return fmt.Errorf("yaml: document size %d exceeds maximum %d: %w", len(data), o.maxDocumentSize, ErrDocumentSize)
	}

	data, err := detectAndConvertEncoding(data)
	if err != nil {
		return err
	}

	tokens, err := newScanner(data).scan()
	if err != nil {
		return err
	}

	p := newParser(tokens)
	p.maxNodes = o.maxNodes
	docs, err := p.parse()
	if err != nil {
		return err
	}

	if len(docs) == 0 {
		return nil
	}

	d := newDecoder(o)
	d.anchors = p.anchors

	if err := loadReferences(d, o); err != nil {
		return err
	}

	if err := d.decode(docs[0], rv.Elem()); err != nil {
		return err
	}

	if len(d.typeErrors) > 0 {
		return &TypeError{Errors: d.typeErrors}
	}

	return nil
}

// Decoder reads and decodes YAML documents from an input stream. Use
// successive calls to [Decoder.Decode] to iterate over a multi-document
// stream; it returns [io.EOF] when no more documents remain.
//
// The entire input is consumed from the reader on the first call to Decode.
// For streams where documents arrive incrementally (e.g. from a network
// connection), callers should frame each document and decode individually
// with [Unmarshal].
//
// A Decoder is not safe for concurrent use. Callers that need to decode
// from multiple goroutines must provide their own synchronization.
type Decoder struct {
	r    io.Reader
	opts *decoderOptions
	docs []*node
	pos  int
	init bool
	anch map[string]*node
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
func (dec *Decoder) Decode(v any) error {
	return dec.DecodeContext(context.Background(), v)
}

// DecodeContext reads the next YAML document, passing ctx to types that
// implement [UnmarshalerContext].
func (dec *Decoder) DecodeContext(ctx context.Context, v any) error {
	if !dec.init {
		data, err := io.ReadAll(dec.r)
		if err != nil {
			return fmt.Errorf("yaml: read input: %w", err)
		}

		if dec.opts.maxDocumentSize > 0 && len(data) > dec.opts.maxDocumentSize {
			return fmt.Errorf("yaml: document size %d exceeds maximum %d: %w", len(data), dec.opts.maxDocumentSize, ErrDocumentSize)
		}

		data, err = detectAndConvertEncoding(data)
		if err != nil {
			return err
		}

		tokens, err := newScanner(data).scan()
		if err != nil {
			return err
		}

		p := newParser(tokens)
		p.maxNodes = dec.opts.maxNodes
		dec.docs, err = p.parse()
		if err != nil {
			return err
		}
		dec.anch = p.anchors
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
	d.anchors = dec.anch

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

func loadReferences(d *decoder, o *decoderOptions) error {
	files := o.referenceFiles

	for _, dir := range o.referenceDirs {
		clean := filepath.Clean(dir)
		realClean, err := filepath.EvalSymlinks(clean)
		if err != nil {
			return fmt.Errorf("yaml: cannot resolve reference dir %q: %w", dir, err)
		}
		if o.recursiveDir {
			err = filepath.WalkDir(realClean, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				name := d.Name()
				if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
					resolved, err := filepath.EvalSymlinks(path)
					if err != nil {
						return fmt.Errorf("yaml: cannot resolve symlink %q: %w", path, err)
					}
					if !strings.HasPrefix(resolved, realClean) {
						return fmt.Errorf("yaml: reference file %q escapes directory %q: %w", resolved, realClean, ErrPathEscape)
					}
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				return fmt.Errorf("yaml: walk reference dir: %w", err)
			}
		} else {
			entries, err := os.ReadDir(realClean)
			if err != nil {
				return fmt.Errorf("yaml: cannot read reference dir %q: %w", dir, err)
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
					full := filepath.Join(realClean, name)
					resolved, err := filepath.EvalSymlinks(full)
					if err != nil {
						return fmt.Errorf("yaml: cannot resolve symlink %q: %w", full, err)
					}
					if !strings.HasPrefix(resolved, realClean) {
						return fmt.Errorf("yaml: reference file %q escapes directory %q: %w", resolved, realClean, ErrPathEscape)
					}
					files = append(files, full)
				}
			}
		}
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("yaml: cannot read reference file %q: %w", file, err)
		}

		data, err = detectAndConvertEncoding(data)
		if err != nil {
			return err
		}

		tokens, err := newScanner(data).scan()
		if err != nil {
			return fmt.Errorf("yaml: error scanning reference file %q: %w", file, err)
		}

		p := newParser(tokens)
		_, err = p.parse()
		if err != nil {
			return fmt.Errorf("yaml: error parsing reference file %q: %w", file, err)
		}

		for name, node := range p.anchors {
			if _, exists := d.anchors[name]; exists {
				return &DuplicateKeyError{
					Key: name,
					Pos: node.pos,
				}
			}
			d.anchors[name] = node
		}
	}

	return nil
}
