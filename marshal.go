package yaml

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
)

// Marshal serializes v into YAML bytes using default encoding options.
func Marshal(v any) ([]byte, error) {
	return MarshalWithOptions(v)
}

// MarshalWithOptions serializes v into YAML bytes, applying the given
// [EncodeOption] values to control formatting and style.
func MarshalWithOptions(v any, opts ...EncodeOption) ([]byte, error) {
	o := defaultEncodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	if o.kyaml && o.flowExplicit && !o.flow {
		return nil, fmt.Errorf("yaml: WithFlow(false) cannot be combined with WithKYAML(): %w", ErrOptionConflict)
	}
	enc := newEncoder(o)
	return enc.encode(reflect.ValueOf(v))
}

// MarshalKYAML is shorthand for [MarshalWithOptions](v, [WithKYAML]()).
//
// The output is a strict KYAML document per [KEP-5295]: a "---" header
// followed by flow-style mappings/sequences, double-quoted string values,
// trailing commas, and lexicographic key ordering for native maps.
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
func MarshalKYAML(v any) ([]byte, error) {
	return MarshalWithOptions(v, WithKYAML())
}

// MarshalKYAMLWithOptions is shorthand for [MarshalWithOptions](v, append(opts, [WithKYAML]())...).
// Allows additional encoder options to be composed atop KYAML mode (for
// example, [WithIndent](4) to change the indent step from 2 to 4).
func MarshalKYAMLWithOptions(v any, opts ...EncodeOption) ([]byte, error) {
	return MarshalWithOptions(v, append(opts, WithKYAML())...)
}

// EncodeKYAMLFile encodes v as KYAML and atomically writes the result to path.
// The destination file is created with mode 0644 if it does not exist.
func EncodeKYAMLFile(path string, v any, opts ...EncodeOption) error {
	data, err := MarshalKYAMLWithOptions(v, opts...)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Encoder writes YAML values to an output stream. When multiple values are
// encoded, each is separated by a "---" document marker.
//
// An Encoder is not safe for concurrent use. Callers that need to encode
// from multiple goroutines must provide their own synchronization.
type Encoder struct {
	w    io.Writer
	opts *encoderOptions
	n    int
}

// NewEncoder returns a new [Encoder] that writes to w.
func NewEncoder(w io.Writer, opts ...EncodeOption) *Encoder {
	o := defaultEncodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	return &Encoder{w: w, opts: o}
}

// Encode writes the YAML encoding of v to the stream.
func (enc *Encoder) Encode(v any) error {
	return enc.EncodeContext(context.Background(), v)
}

// EncodeContext writes the YAML encoding of v to the stream. The context is
// passed to types implementing [MarshalerContext].
func (enc *Encoder) EncodeContext(ctx context.Context, v any) error {
	e := newEncoder(enc.opts)
	e.ctx = ctx
	data, err := e.encode(reflect.ValueOf(v))
	if err != nil {
		return err
	}
	if enc.n > 0 {
		if _, err := enc.w.Write([]byte("---\n")); err != nil {
			return err
		}
	}
	_, err = enc.w.Write(data)
	enc.n++
	return err
}

// Close is a no-op. It is provided for symmetry with [NewEncoder] so
// callers can use a defer pattern without harm.
func (enc *Encoder) Close() error {
	return nil
}
