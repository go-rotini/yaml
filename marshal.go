package yaml

import (
	"context"
	"io"
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
	enc := newEncoder(o)
	return enc.encode(reflect.ValueOf(v))
}

// Encoder writes YAML values to an output stream. When multiple values are
// encoded, each is separated by a "---" document marker.
//
// An Encoder is not safe for concurrent use. Callers that need to encode
// from multiple goroutines must provide their own synchronisation.
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

// Close flushes the encoder. It is a no-op but is provided so [Encoder]
// can satisfy io.Closer.
func (enc *Encoder) Close() error {
	return nil
}
