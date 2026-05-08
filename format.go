package yaml

import (
	"bytes"
	"context"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// kyamlEmitter produces strict KYAML output per KEP-5295.
//
// The emitter is invoked from the existing encoder when opts.kyaml is set.
// It is not safe for concurrent use; callers create one per Marshal call.
type kyamlEmitter struct {
	opts *encoderOptions
	buf  []byte
	ctx  context.Context
	seen map[uintptr]bool
}

func newKYAMLEmitter(opts *encoderOptions) *kyamlEmitter {
	return &kyamlEmitter{
		opts: opts,
		ctx:  context.Background(),
		seen: make(map[uintptr]bool),
	}
}

// encode produces the full KYAML document, including the leading "---" header
// (R3.1) and a trailing newline.
func (e *kyamlEmitter) encode(v reflect.Value) ([]byte, error) {
	e.buf = append(e.buf, "---\n"...)
	if err := e.emit(v, 0); err != nil {
		return nil, err
	}
	if len(e.buf) == 0 || e.buf[len(e.buf)-1] != '\n' {
		e.buf = append(e.buf, '\n')
	}
	return e.buf, nil
}

// emit writes v at the given parent indent. The caller writes any leading
// indent before invoking emit; this function never writes a trailing newline.
func (e *kyamlEmitter) emit(v reflect.Value, indent int) error {
	if !v.IsValid() {
		e.buf = append(e.buf, "null"...)
		return nil
	}

	// Unwrap pointers and interfaces, with cycle guard for pointer types.
	// We collect every pointer we descended through and clean them all up
	// in a single deferred call at function return — avoiding a defer per
	// loop iteration.
	var tracked []uintptr
	defer func() {
		for _, p := range tracked {
			delete(e.seen, p)
		}
	}()
	for {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				e.buf = append(e.buf, "null"...)
				return nil
			}
			ptr := v.Pointer()
			if e.seen[ptr] {
				return fmt.Errorf("yaml: cyclic value of type %s: %w", v.Type(), ErrUnsupported)
			}
			e.seen[ptr] = true
			tracked = append(tracked, ptr)
			v = v.Elem()
			continue
		case reflect.Interface:
			if v.IsNil() {
				e.buf = append(e.buf, "null"...)
				return nil
			}
			v = v.Elem()
			continue
		}
		break
	}

	// Per R13.11: RawValue carries arbitrary YAML bytes that may contain
	// non-KYAML constructs (anchors, tags, block style). Re-parse through
	// the YAML decoder and re-emit as canonical KYAML rather than letting
	// the bytes flow through verbatim. Must run before dispatchMarshaler
	// since RawValue implements BytesMarshaler.
	if v.CanInterface() {
		if rv, ok := v.Interface().(RawValue); ok {
			return e.emitRawValue(rv, indent)
		}
	}

	// Per R13.2, json.Marshaler takes precedence under KYAML mode.
	if handled, err := e.dispatchMarshaler(v, indent); handled {
		return err
	}

	// Special types (matches KYAML's JSON-first semantics per R13).
	if v.CanInterface() {
		switch t := v.Interface().(type) {
		case time.Time:
			data, err := t.MarshalJSON()
			if err != nil {
				return fmt.Errorf("yaml: time.Time MarshalJSON: %w", err)
			}
			e.buf = append(e.buf, data...)
			return nil
		case time.Duration:
			if e.opts.durationAsString {
				// Per R13.7: emit as a quoted human-readable string.
				e.emitString(t.String(), indent)
				return nil
			}
			e.buf = strconv.AppendInt(e.buf, int64(t), 10)
			return nil
		case json.Number:
			s := t.String()
			if s == "" {
				e.buf = append(e.buf, '0')
				return nil
			}
			e.buf = append(e.buf, s...)
			return nil
		case json.RawMessage:
			var raw any
			if err := json.Unmarshal(t, &raw); err != nil {
				return fmt.Errorf("yaml: cannot decode json.RawMessage: %w", err)
			}
			return e.emit(reflect.ValueOf(raw), indent)
		case big.Int:
			e.buf = append(e.buf, t.String()...)
			return nil
		case big.Float:
			e.buf = append(e.buf, t.Text('g', -1)...)
			return nil
		}
	}
	if v.CanAddr() {
		switch p := v.Addr().Interface().(type) {
		case *big.Int:
			e.buf = append(e.buf, p.String()...)
			return nil
		case *big.Float:
			e.buf = append(e.buf, p.Text('g', -1)...)
			return nil
		}
	}

	switch v.Kind() {
	case reflect.String:
		e.emitString(v.String(), indent)
		return nil
	case reflect.Bool:
		if v.Bool() {
			e.buf = append(e.buf, "true"...)
		} else {
			e.buf = append(e.buf, "false"...)
		}
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		e.buf = strconv.AppendInt(e.buf, v.Int(), 10)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		e.buf = strconv.AppendUint(e.buf, v.Uint(), 10)
		return nil
	case reflect.Float32, reflect.Float64:
		return e.emitFloat(v.Float(), v.Type().Bits())
	case reflect.Slice:
		if v.IsNil() {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		// MapSlice is a slice of MapItem but renders as a mapping per its
		// ordered-map semantics (matching the rest of the package).
		if v.Type() == reflect.TypeFor[MapSlice]() {
			ms, _ := v.Interface().(MapSlice)
			return e.emitMapSlice(ms, indent)
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			e.emitString(base64.StdEncoding.EncodeToString(v.Bytes()), indent)
			return nil
		}
		return e.emitSequence(v, indent)
	case reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			buf := make([]byte, v.Len())
			for i := range v.Len() {
				buf[i] = byte(v.Index(i).Uint())
			}
			e.emitString(base64.StdEncoding.EncodeToString(buf), indent)
			return nil
		}
		return e.emitSequence(v, indent)
	case reflect.Map:
		if v.IsNil() {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		return e.emitMap(v, indent)
	case reflect.Struct:
		return e.emitStruct(v, indent)
	case reflect.Chan, reflect.Func, reflect.Complex64, reflect.Complex128:
		return fmt.Errorf("yaml: cannot KYAML-encode value of type %s: %w", v.Type(), ErrUnsupported)
	}

	return fmt.Errorf("yaml: cannot KYAML-encode value of type %s: %w", v.Type(), ErrUnsupported)
}

// dispatchMarshaler checks for the KYAML-mode marshaler priority chain
// (json.Marshaler first per R13.2, then yaml marshalers, then TextMarshaler,
// then custom marshalers). Returns handled=true if a marshaler was invoked.
func (e *kyamlEmitter) dispatchMarshaler(v reflect.Value, indent int) (handled bool, err error) {
	// Custom marshalers (highest priority — explicit user override).
	if e.opts.customMarshalers != nil && v.CanInterface() {
		if fn, ok := e.opts.customMarshalers[v.Type()]; ok {
			out := reflect.ValueOf(fn).Call([]reflect.Value{v})
			if !out[1].IsNil() {
				e2, _ := out[1].Interface().(error)
				return true, e2
			}
			data, _ := out[0].Interface().([]byte)
			// Re-route through KYAML: the custom marshaler may have produced YAML/JSON;
			// re-parse to any and re-emit.
			return true, e.emitRawJSONOrText(data, indent)
		}
	}

	// json.Marshaler — primary under KYAML (R13.2).
	if v.CanInterface() {
		if m, ok := v.Interface().(json.Marshaler); ok {
			data, mErr := m.MarshalJSON()
			if mErr != nil {
				return true, fmt.Errorf("yaml: %T.MarshalJSON: %w", v.Interface(), mErr)
			}
			return true, e.emitRawJSON(data, indent)
		}
	}
	if v.CanAddr() {
		if m, ok := v.Addr().Interface().(json.Marshaler); ok {
			data, mErr := m.MarshalJSON()
			if mErr != nil {
				return true, fmt.Errorf("yaml: %T.MarshalJSON: %w", v.Addr().Interface(), mErr)
			}
			return true, e.emitRawJSON(data, indent)
		}
	}

	// yaml marshalers — secondary under KYAML.
	if v.CanInterface() {
		iface := v.Interface()
		if m, ok := iface.(MarshalerContext); ok {
			result, mErr := m.MarshalYAML(e.ctx)
			if mErr != nil {
				return true, mErr
			}
			return true, e.emit(reflect.ValueOf(result), indent)
		}
		if m, ok := iface.(Marshaler); ok {
			result, mErr := m.MarshalYAML()
			if mErr != nil {
				return true, mErr
			}
			return true, e.emit(reflect.ValueOf(result), indent)
		}
		if m, ok := iface.(BytesMarshaler); ok {
			data, mErr := m.MarshalYAML()
			if mErr != nil {
				return true, mErr
			}
			return true, e.emitRawJSONOrText(data, indent)
		}
		if m, ok := iface.(encoding.TextMarshaler); ok {
			data, mErr := m.MarshalText()
			if mErr != nil {
				return true, mErr
			}
			e.emitString(string(data), indent)
			return true, nil
		}
	}
	return false, nil
}

// emitRawValue parses RawValue's stored YAML bytes and re-emits as KYAML.
// Per R13.11, raw bytes are not passed through verbatim under KYAML mode —
// they could otherwise leak non-KYAML constructs (anchors, tags, block
// style) into strict-KYAML output.
func (e *kyamlEmitter) emitRawValue(rv RawValue, indent int) error {
	if len(rv) == 0 {
		e.buf = append(e.buf, "null"...)
		return nil
	}
	var raw any
	if err := UnmarshalWithOptions([]byte(rv), &raw, WithOrderedMap()); err != nil {
		return fmt.Errorf("yaml: cannot decode RawValue: %w", err)
	}
	return e.emit(reflect.ValueOf(raw), indent)
}

// emitRawJSON parses raw JSON bytes and re-emits as KYAML.
func (e *kyamlEmitter) emitRawJSON(data []byte, indent int) error {
	var v any
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("yaml: cannot decode JSON output for KYAML: %w", err)
	}
	return e.emit(reflect.ValueOf(v), indent)
}

// emitRawJSONOrText tries to parse as JSON; if that fails, emits as a quoted
// string. Used for marshaler outputs that may be either YAML/JSON or plain text.
func (e *kyamlEmitter) emitRawJSONOrText(data []byte, indent int) error {
	var v any
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&v); err == nil {
		return e.emit(reflect.ValueOf(v), indent)
	}
	e.emitString(string(data), indent)
	return nil
}

// emitFloat emits a float per R6.2. NaN and ±Inf are rejected with ErrUnsupported.
func (e *kyamlEmitter) emitFloat(f float64, bits int) error {
	if math.IsNaN(f) {
		return fmt.Errorf("yaml: NaN is not representable in KYAML: %w", ErrUnsupported)
	}
	if math.IsInf(f, 0) {
		return fmt.Errorf("yaml: Inf is not representable in KYAML: %w", ErrUnsupported)
	}
	// Whole-valued floats render as integers when WithAutoInt or KYAML-default
	// JSON-style (per R6.2c).
	if f == math.Trunc(f) && !math.IsInf(f, 0) && math.Abs(f) < 1e16 {
		e.buf = strconv.AppendInt(e.buf, int64(f), 10)
		return nil
	}
	if bits == 32 {
		e.buf = strconv.AppendFloat(e.buf, f, 'g', -1, 32)
	} else {
		e.buf = strconv.AppendFloat(e.buf, f, 'g', -1, 64)
	}
	return nil
}

// emitSequence renders a sequence in flow form with bracket cuddling per
// R7 + R8. Cuddling only applies to sequences (R8.2 "paired brackets" — a
// sequence's brackets pair with the brackets of a compound element).
//
// For [{...}], the open `[` cuddles to the inner `{` and the close `]`
// cuddles to the inner `}`. The cuddled compound element's logical indent is
// the sequence's own indent (so its close bracket lines up with what would
// otherwise be the sequence's close position).
//
// Per R8.5, cuddling is suppressed when comments are present (set via
// [WithComment]) — comment placement uses a post-pass string replacement
// that can't reliably target cuddled boundaries, so the safest behavior is
// to keep brackets on their own lines whenever any comment is registered.
func (e *kyamlEmitter) emitSequence(v reflect.Value, indent int) error {
	n := v.Len()
	if n == 0 {
		e.buf = append(e.buf, "[]"...)
		return nil
	}
	inner := indent + e.opts.indent
	e.buf = append(e.buf, '[')

	// R8.5: when any comment is registered, suppress cuddling globally.
	suppressCuddle := len(e.opts.comments) > 0

	for i := range n {
		elem := v.Index(i)
		elemForCuddle := unwrapForCuddle(elem)
		startsBracket := !suppressCuddle && emitsAsCompound(elemForCuddle)

		// Pre-element placement.
		if i == 0 {
			if !startsBracket {
				e.buf = append(e.buf, '\n')
				e.writeIndent(inner)
			}
			// else: cuddled open — emit element directly after `[`.
		} else {
			// Previous iteration appended ','.
			if startsBracket && lastVisibleAfterCommaIsCloseBracket(e.buf) {
				e.buf = append(e.buf, ' ')
			} else {
				e.buf = append(e.buf, '\n')
				e.writeIndent(inner)
			}
		}

		// Compute the indent passed to the element. For cuddled compound
		// elements, the element's "logical indent" equals the sequence's
		// own indent (so its close bracket lines up with `]`'s position).
		// For non-cuddled elements (whether scalar or compound), the
		// element sits at `inner`.
		elemIndent := inner
		if startsBracket {
			elemIndent = indent
		}

		if err := e.emit(elem, elemIndent); err != nil {
			return err
		}

		if i < n-1 {
			e.buf = append(e.buf, ',')
		}
	}

	// Close: cuddle if last element ended with a closing bracket
	// (and we're cuddling — i.e., the last element is compound). Suppress
	// close cuddling per R8.5 when comments are present.
	if !suppressCuddle && lastIsCloseBracket(e.buf) {
		e.buf = append(e.buf, ']')
	} else {
		e.buf = append(e.buf, ',', '\n')
		e.writeIndent(indent)
		e.buf = append(e.buf, ']')
	}
	return nil
}

// mapEntry holds a sortable, pre-rendered key plus the value for emitMap and
// emitStruct.
type mapEntry struct {
	rawKey reflect.Value // for cuddle/comment lookups; may be invalid for struct fields
	keyStr string        // pre-rendered key text (with quoting decision applied)
	value  reflect.Value
	// nilStringFlag: true if value was already determined to be null by the caller
	// and value should not be emitted normally.
	emitNullDirect bool
}

// emitMap renders a Go map in KYAML flow form. Native maps are sorted
// lexicographically (R4.5).
func (e *kyamlEmitter) emitMap(v reflect.Value, indent int) error {
	if v.Len() == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}

	keys := v.MapKeys()
	// Convert keys to string and sort. R4.4: only string keys allowed under KYAML.
	entries := make([]mapEntry, 0, len(keys))
	for _, k := range keys {
		ks, err := mapKeyToString(k)
		if err != nil {
			return err
		}
		entries = append(entries, mapEntry{
			rawKey: k,
			keyStr: ks,
			value:  v.MapIndex(k),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].keyStr < entries[j].keyStr })

	return e.emitMappingEntries(entries, indent)
}

// emitMapSlice renders an ordered MapSlice. Order is preserved (insertion order).
func (e *kyamlEmitter) emitMapSlice(ms MapSlice, indent int) error {
	if len(ms) == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}
	entries := make([]mapEntry, 0, len(ms))
	for _, item := range ms {
		ks, err := mapKeyAnyToString(item.Key)
		if err != nil {
			return err
		}
		entries = append(entries, mapEntry{
			rawKey: reflect.ValueOf(item.Key),
			keyStr: ks,
			value:  reflect.ValueOf(item.Value),
		})
	}
	return e.emitMappingEntries(entries, indent)
}

// emitStruct renders a struct in KYAML form using KYAML field resolution
// (json tag primary per R13.4).
func (e *kyamlEmitter) emitStruct(v reflect.Value, indent int) error {
	sf := getKYAMLStructFields(v.Type())
	if len(sf.conflicts) > 0 {
		return fmt.Errorf("yaml: struct %s has conflicting field names: %s: %w",
			v.Type(), strings.Join(sf.conflicts, ", "), errConflictingFields)
	}

	// Collect non-omitted fields.
	entries := make([]mapEntry, 0, len(sf.fields))
	for _, fi := range sf.fields {
		field := fieldByIndex(v, fi.index)
		if !field.IsValid() {
			continue
		}
		if fi.omitEmpty && isEmpty(field) {
			continue
		}
		// Inline-map handling: emit each key from the map as a top-level entry.
		if fi.inline && field.Kind() == reflect.Map {
			mapKeys := field.MapKeys()
			subEntries := make([]mapEntry, 0, len(mapKeys))
			for _, k := range mapKeys {
				ks, err := mapKeyToString(k)
				if err != nil {
					return err
				}
				subEntries = append(subEntries, mapEntry{
					rawKey: k,
					keyStr: ks,
					value:  field.MapIndex(k),
				})
			}
			sort.Slice(subEntries, func(i, j int) bool { return subEntries[i].keyStr < subEntries[j].keyStr })
			entries = append(entries, subEntries...)
			continue
		}
		entries = append(entries, mapEntry{
			keyStr: fi.name,
			value:  field,
		})
	}

	if len(entries) == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}

	return e.emitMappingEntries(entries, indent)
}

// emitMappingEntries is the shared rendering routine for maps, MapSlices, and
// structs. Mappings never cuddle their close bracket — KEP R8.2's "paired
// brackets" rule applies only to sequences with compound elements
// (`[{...}]` / `[[...]]`). A mapping's close `}` always sits on its own
// line at the mapping's indent, with a trailing comma after the final entry
// per R8.1.
func (e *kyamlEmitter) emitMappingEntries(entries []mapEntry, indent int) error {
	n := len(entries)
	if n == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}
	inner := indent + e.opts.indent
	e.buf = append(e.buf, '{')

	for _, ent := range entries {
		// Each field on a line of its own (R4.1).
		e.buf = append(e.buf, '\n')
		e.writeIndent(inner)

		// Key
		e.buf = append(e.buf, e.formatKey(ent.keyStr)...)
		e.buf = append(e.buf, ':', ' ')

		// Value
		switch {
		case ent.emitNullDirect, !ent.value.IsValid():
			e.buf = append(e.buf, "null"...)
		default:
			if err := e.emit(ent.value, inner); err != nil {
				return err
			}
		}

		// Trailing comma after every entry (R8.1, no cuddling for mappings).
		e.buf = append(e.buf, ',')
	}

	// Close: always on its own line at the mapping's indent.
	e.buf = append(e.buf, '\n')
	e.writeIndent(indent)
	e.buf = append(e.buf, '}')
	return nil
}

// formatKey returns the rendered key text with R5 quoting decisions applied.
func (e *kyamlEmitter) formatKey(key string) string {
	if e.opts.kyamlAlwaysQuoteKeys {
		return quoteKYAMLString(key)
	}
	if needsKeyQuoting(key) {
		return quoteKYAMLString(key)
	}
	return key
}

// emitString renders a string value per R6.4 (always double-quoted). For
// multi-line strings whose fully-escaped single-line form exceeds the
// configured line width, the value is rendered using KYAML's flow-folded
// multi-line form (R10) — readable across the source line for log/config
// content. Otherwise a single-line double-quoted scalar with embedded \n
// escapes is used.
//
// The flow-fold trigger uses the FULLY-ESCAPED length (post escapeKYAMLLine)
// which is invariant under UTF-8 normalization: invalid UTF-8 bytes are
// replaced by literal U+FFFD UTF-8 (3 bytes) on first encode, and the
// resulting valid U+FFFD round-trips to itself on subsequent passes.
// Idempotence verified by FuzzKYAMLRoundTrip.
func (e *kyamlEmitter) emitString(s string, indent int) {
	if shouldUseFlowFold(s, e.opts.lineWidth) {
		e.buf = append(e.buf, kyamlFlowFold(s, indent+e.opts.indent)...)
		return
	}
	e.buf = append(e.buf, quoteKYAMLString(s)...)
}

// shouldUseFlowFold reports whether s should be emitted using KYAML's
// flow-folded multi-line form (R10) rather than a single-line double-quoted
// scalar with embedded \n escapes.
//
// Triggers (all must hold):
//  1. The string contains at least 2 literal newlines (so the threshold
//     can't flip on round-trip).
//  2. Its fully-escaped single-line form (post escapeKYAMLLine) exceeds
//     lineWidth. Using the escaped length rather than the raw input length
//     keeps the trigger stable across encode → decode → encode cycles
//     (UTF-8 normalization preserves escape lengths).
//  3. No continuation line (any line after the first) begins with
//     whitespace. YAML's `\<newline>` continuation rule strips leading
//     whitespace on the next source line — but if the first byte of the
//     line is part of the actual string data, we'd lose it on round-trip.
//     Strings violating this constraint render as single-line.
func shouldUseFlowFold(s string, lineWidth int) bool {
	if lineWidth <= 0 {
		return false
	}
	if strings.Count(s, "\n") < 2 {
		return false
	}
	// Continuation-line leading-whitespace check.
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue // empty continuation line is safe
		}
		if line[0] == ' ' || line[0] == '\t' {
			return false
		}
	}
	escaped := escapeKYAMLLine(s)
	// +2 for the surrounding double quotes that quoteKYAMLString would add.
	return len(escaped)+2 > lineWidth
}

// kyamlFlowFold renders s using the KEP-5295 R10 flow-folded form: a
// multi-line double-quoted string with `\n` escape sequences for actual
// newlines and trailing-backslash continuations to wrap each source line.
// contIndent is the column at which continuation lines start.
//
// Example output for "first\nsecond\nthird":
//
//	"first\n\
//	  second\n\
//	  third"
//
// The trailing `\<newline><indent>` between lines is YAML's flow-folding
// indicator: the parser folds the newline-and-leading-whitespace away,
// leaving only the literal `\n` escape's newline in the decoded value.
func kyamlFlowFold(s string, contIndent int) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	b.WriteByte('"')
	indentStr := strings.Repeat(" ", contIndent)
	for i, line := range lines {
		b.WriteString(escapeKYAMLLine(line))
		if i < len(lines)-1 {
			b.WriteString(`\n`)
			b.WriteByte('\\')
			b.WriteByte('\n')
			b.WriteString(indentStr)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func (e *kyamlEmitter) writeIndent(n int) {
	for range n {
		e.buf = append(e.buf, ' ')
	}
}

// quoteKYAMLString produces a double-quoted KYAML string per R6.4 + R6.5.
func quoteKYAMLString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	b.WriteString(escapeKYAMLLine(s))
	b.WriteByte('"')
	return b.String()
}

// escapeKYAMLLine applies the R6.5 escape table to s. The result does not
// include the surrounding quotes.
func escapeKYAMLLine(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 byte: replace with the literal UTF-8 bytes for
			// U+FFFD (matches encoding/json's behavior). YAML's \xHH escape
			// represents a Unicode code point, not a raw byte, so invalid
			// bytes cannot round-trip exactly. Emitting U+FFFD as its
			// literal UTF-8 (3 bytes) keeps Format idempotent on canonical
			// KYAML — subsequent passes see valid UTF-8 and emit it
			// literally.
			b.WriteString("�")
			i++
			continue
		}
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case 0:
			b.WriteString(`\0`)
		case 0x07:
			b.WriteString(`\a`)
		case 0x0B:
			b.WriteString(`\v`)
		case 0x1B:
			b.WriteString(`\e`)
		case 0x85:
			b.WriteString(`\N`)
		case 0x2028:
			b.WriteString(`\L`)
		case 0x2029:
			b.WriteString(`\P`)
		default:
			if r < 0x20 || (r >= 0x7F && r <= 0x9F) {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
		i += size
	}
	return b.String()
}

// typeAmbiguousKeys is the set of keys that MUST be double-quoted under KYAML
// even though they would otherwise be valid plain scalars (R5.2). Includes
// every YAML 1.1 boolean alias and null literal.
var typeAmbiguousKeys = map[string]struct{}{
	"y": {}, "Y": {}, "yes": {}, "Yes": {}, "YES": {},
	"n": {}, "N": {}, "no": {}, "No": {}, "NO": {},
	"true": {}, "True": {}, "TRUE": {},
	"false": {}, "False": {}, "FALSE": {},
	"on": {}, "On": {}, "ON": {},
	"off": {}, "Off": {}, "OFF": {},
	"null": {}, "Null": {}, "NULL": {},
	"~": {},
}

// needsKeyQuoting reports whether key must be quoted under KYAML R5.
func needsKeyQuoting(key string) bool {
	if key == "" {
		return true
	}
	if _, ambiguous := typeAmbiguousKeys[key]; ambiguous {
		return true
	}
	// Numeric-looking keys must be quoted.
	if _, err := strconv.ParseInt(key, 10, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseFloat(key, 64); err == nil {
		return true
	}
	// Validate against the KYAML key character class. Unquoted keys must
	// match [A-Za-z_][A-Za-z0-9_./-]* — no flow-context indicators
	// (brackets, braces, comma, colon, etc.) which would corrupt parsing.
	return !validKYAMLKey(key)
}

// validKYAMLKey reports whether key is a "safe" identifier that can be
// emitted unquoted inside a KYAML flow mapping. The character class is
// deliberately conservative — anything outside [A-Za-z0-9_./-] (with a
// letter or underscore as the first byte) gets quoted.
func validKYAMLKey(key string) bool {
	if key == "" {
		return false
	}
	first := key[0]
	if first != '_' && (first < 'A' || first > 'Z') && (first < 'a' || first > 'z') {
		return false
	}
	for i := 1; i < len(key); i++ {
		if !isKYAMLKeyChar(key[i]) {
			return false
		}
	}
	return true
}

func isKYAMLKeyChar(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_' || c == '.' || c == '/' || c == '-':
		return true
	}
	return false
}

// mapKeyToString converts a reflect.Value map key to a string. Returns an
// error if the key is not a string-typed value (R4.4).
func mapKeyToString(k reflect.Value) (string, error) {
	for k.Kind() == reflect.Pointer || k.Kind() == reflect.Interface {
		if k.IsNil() {
			return "", fmt.Errorf("yaml: nil map key is not allowed in KYAML: %w", ErrUnsupported)
		}
		k = k.Elem()
	}
	if k.Kind() == reflect.String {
		return k.String(), nil
	}
	if k.CanInterface() {
		if tm, ok := k.Interface().(encoding.TextMarshaler); ok {
			data, err := tm.MarshalText()
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
	}
	return "", fmt.Errorf("yaml: KYAML mapping key must be a string or TextMarshaler, got %s: %w", k.Type(), ErrUnsupported)
}

func mapKeyAnyToString(k any) (string, error) {
	if k == nil {
		return "", fmt.Errorf("yaml: nil map key is not allowed in KYAML: %w", ErrUnsupported)
	}
	switch t := k.(type) {
	case string:
		return t, nil
	case encoding.TextMarshaler:
		data, err := t.MarshalText()
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return mapKeyToString(reflect.ValueOf(k))
}

// unwrapForCuddle dereferences pointers/interfaces to find the value that
// will determine bracket cuddling.
func unwrapForCuddle(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return v
		}
		v = v.Elem()
	}
	return v
}

// emitsAsCompound reports whether emit(v) will produce output that begins
// with `{` or `[`. Used for cuddling decisions.
func emitsAsCompound(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		// []byte → string, not compound.
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return false
		}
		return true
	case reflect.Map:
		return true
	case reflect.Struct:
		switch v.Type() {
		case reflect.TypeFor[time.Time](),
			reflect.TypeFor[big.Int](),
			reflect.TypeFor[big.Float](),
			reflect.TypeFor[big.Rat](),
			reflect.TypeFor[json.Number](),
			reflect.TypeFor[json.RawMessage]():
			return false
		}
		return true
	}
	return false
}

// lastIsCloseBracket reports whether buf's last non-whitespace byte is `}` or `]`.
func lastIsCloseBracket(buf []byte) bool {
	for _, c := range slices.Backward(buf) {
		if c == ' ' || c == '\n' || c == '\t' {
			continue
		}
		return c == '}' || c == ']'
	}
	return false
}

// lastVisibleAfterCommaIsCloseBracket returns true when, after virtually
// skipping a trailing comma separator, the most recent non-whitespace byte
// is `}` or `]`. Used by the KYAML sequence emitter to decide whether to
// cuddle a compound element following a previously-emitted compound.
func lastVisibleAfterCommaIsCloseBracket(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}
	end := len(buf) - 1
	if buf[end] == ',' {
		end--
	}
	for i := end; i >= 0; i-- {
		c := buf[i]
		if c == ' ' || c == '\n' || c == '\t' {
			continue
		}
		return c == '}' || c == ']'
	}
	return false
}

// getKYAMLStructFields returns a struct-field cache specialized for KYAML mode:
// the json tag is primary (per R13.4), the yaml tag is secondary, and
// lowercased fallback names are NOT used (matches encoding/json behavior:
// the exact Go field name is used when no tag is present).
var kyamlStructFieldCache sync.Map

func getKYAMLStructFields(t reflect.Type) *structFields {
	if cached, ok := kyamlStructFieldCache.Load(t); ok {
		sf, _ := cached.(*structFields)
		return sf
	}
	sf := &structFields{
		byName: make(map[string]int),
	}
	collectKYAMLFields(t, nil, sf)
	kyamlStructFieldCache.Store(t, sf)
	return sf
}

func collectKYAMLFields(t reflect.Type, index []int, sf *structFields) {
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() && !f.Anonymous {
			continue
		}

		// json tag wins under KYAML; yaml tag provides additional options
		// (omitzero, inline, required, default=).
		jsonTag := f.Tag.Get("json")
		yamlTag := f.Tag.Get("yaml")

		var fi fieldInfo
		// Parse both, then merge with json winning for the name.
		if jsonTag != "" {
			fi = parseTag(jsonTag)
		}
		if yamlTag != "" {
			y := parseTag(yamlTag)
			if jsonTag == "" {
				fi = y
			} else {
				// Merge yaml-tag-only options.
				if y.omitEmpty {
					fi.omitEmpty = true
				}
				if y.inline {
					fi.inline = true
				}
				if y.required {
					fi.required = true
				}
				if y.hasDefault {
					fi.defaultValue = y.defaultValue
					fi.hasDefault = true
				}
				if y.skip {
					fi.skip = true
				}
			}
		}

		if fi.skip {
			continue
		}

		fi.index = make([]int, len(index)+1)
		copy(fi.index, index)
		fi.index[len(index)] = i

		if f.Anonymous && fi.name == "" && !fi.inline {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectKYAMLFields(ft, fi.index, sf)
				continue
			}
		}

		if fi.inline {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectKYAMLFields(ft, fi.index, sf)
				continue
			}
			if f.Type.Kind() == reflect.Map {
				sf.fields = append(sf.fields, fi)
				sf.byName[fi.name] = len(sf.fields) - 1
				continue
			}
		}

		if fi.name == "" {
			// KYAML uses the exact Go field name when no tag is present
			// (matches encoding/json, NOT the default yaml-mode lowercasing).
			fi.name = f.Name
		}

		if idx, exists := sf.byName[fi.name]; exists {
			existing := sf.fields[idx]
			if len(fi.index) == len(existing.index) {
				sf.conflicts = append(sf.conflicts, fi.name)
			} else if len(fi.index) < len(existing.index) {
				sf.fields[idx] = fi
			}
			continue
		}

		sf.fields = append(sf.fields, fi)
		sf.byName[fi.name] = len(sf.fields) - 1
	}
}

// ToJSON converts YAML bytes to JSON bytes. The YAML input is decoded
// into an untyped value and then re-encoded as JSON.
func ToJSON(yamlData []byte) ([]byte, error) {
	var v any
	if err := Unmarshal(yamlData, &v); err != nil {
		return nil, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("yaml: json encode: %w", err)
	}
	return b, nil
}

// FromJSON converts JSON bytes to YAML bytes. The JSON input is decoded
// into an untyped value and then re-encoded as YAML.
func FromJSON(jsonData []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(jsonData, &v); err != nil {
		return nil, fmt.Errorf("yaml: json decode: %w", err)
	}
	return Marshal(v)
}

// Valid reports whether data is valid YAML.
func Valid(data []byte) bool {
	data, err := detectAndConvertEncoding(data)
	if err != nil {
		return false
	}
	tokens, err := newScanner(data).scan()
	if err != nil {
		return false
	}
	p := newParser(tokens)
	_, err = p.parse()
	return err == nil
}

// ValidKYAML reports whether data is a valid KYAML document — strict KYAML, per
// the rules of [KEP-5295]. ValidKYAML is equivalent to [ValidateKYAML](data) == nil.
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
func ValidKYAML(data []byte) bool {
	return ValidateKYAML(data) == nil
}

// ValidateKYAML parses data as YAML and reports any KYAML conformance
// violations. Returns nil if data is a valid KYAML document, or a
// [*KYAMLError] carrying every violation. Validation is structural per the
// rules of [KEP-5295]; cosmetic deviations (indentation, bracket cuddling,
// trailing commas, key ordering) are not checked here. Use [Lint] for
// cosmetic validation.
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
func ValidateKYAML(data []byte) error {
	data, err := detectAndConvertEncoding(data)
	if err != nil {
		return err
	}
	tokens, err := newScanner(data).scan()
	if err != nil {
		return err
	}
	for _, tok := range tokens {
		if tok.kind == tokenDirective {
			return &KYAMLError{Errors: []KYAMLViolation{{
				Rule:    "R12.9",
				Message: fmt.Sprintf("YAML directive %q not allowed in KYAML", tok.value),
				Pos:     tok.pos,
				Token:   tok.value,
			}}}
		}
	}
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return &KYAMLError{Errors: []KYAMLViolation{{
			Rule:    "R3.1",
			Message: "KYAML document must contain at least one document with the \"---\" header",
		}}}
	}
	var violations []KYAMLViolation
	for _, doc := range docs {
		validateKYAMLNode(doc, &violations)
	}
	if len(violations) > 0 {
		return &KYAMLError{Errors: violations}
	}
	return nil
}

func validateKYAMLNode(n *node, out *[]KYAMLViolation) {
	if n == nil {
		return
	}
	if n.kind == nodeDocument {
		if !n.docStartExplicit {
			*out = append(*out, KYAMLViolation{
				Rule:    "R3.1",
				Message: "KYAML document must begin with the \"---\" header",
				Pos:     n.pos,
			})
		}
		for _, child := range n.children {
			validateKYAMLNode(child, out)
		}
		return
	}
	if n.anchor != "" {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.1",
			Message: fmt.Sprintf("anchor %q not allowed in KYAML", "&"+n.anchor),
			Pos:     n.pos,
			Token:   "&" + n.anchor,
		})
	}
	if n.kind == nodeAlias {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.1",
			Message: fmt.Sprintf("alias %q not allowed in KYAML", "*"+n.alias),
			Pos:     n.pos,
			Token:   "*" + n.alias,
		})
		return
	}
	if n.kind == nodeMergeKey {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.3",
			Message: "merge key (<<) not allowed in KYAML",
			Pos:     n.pos,
			Token:   "<<",
		})
		return
	}
	if n.tag != "" {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.2",
			Message: fmt.Sprintf("explicit tag %q not allowed in KYAML", n.tag),
			Pos:     n.pos,
			Token:   n.tag,
		})
	}
	switch n.kind {
	case nodeMapping:
		if !n.flow {
			*out = append(*out, KYAMLViolation{
				Rule:    "R12.5",
				Message: "block-style mapping not allowed in KYAML; use flow style {}",
				Pos:     n.pos,
			})
		}
		for i := 0; i+1 < len(n.children); i += 2 {
			validateKYAMLKey(n.children[i], out)
			validateKYAMLNode(n.children[i+1], out)
		}
	case nodeSequence:
		if !n.flow {
			*out = append(*out, KYAMLViolation{
				Rule:    "R12.6",
				Message: "block-style sequence not allowed in KYAML; use flow style []",
				Pos:     n.pos,
			})
		}
		for _, c := range n.children {
			validateKYAMLNode(c, out)
		}
	case nodeScalar:
		validateKYAMLScalar(n, false, out)
	}
}

func validateKYAMLKey(n *node, out *[]KYAMLViolation) {
	if n == nil {
		return
	}
	if n.kind == nodeScalar && n.value == "<<" && n.style == scalarPlain {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.3",
			Message: "merge key (<<) not allowed in KYAML",
			Pos:     n.pos,
			Token:   "<<",
		})
		return
	}
	if n.kind != nodeScalar {
		*out = append(*out, KYAMLViolation{
			Rule:    "R4.4",
			Message: fmt.Sprintf("KYAML mapping key must be a string scalar, got %s", nodeKindName(n.kind)),
			Pos:     n.pos,
		})
		return
	}
	if n.anchor != "" {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.1",
			Message: fmt.Sprintf("anchor %q on key not allowed in KYAML", "&"+n.anchor),
			Pos:     n.pos,
			Token:   "&" + n.anchor,
		})
	}
	if n.tag != "" {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.2",
			Message: fmt.Sprintf("explicit tag %q on key not allowed in KYAML", n.tag),
			Pos:     n.pos,
			Token:   n.tag,
		})
	}
	validateKYAMLScalar(n, true, out)
}

func validateKYAMLScalar(n *node, asKey bool, out *[]KYAMLViolation) {
	switch n.style {
	case scalarSingleQuoted:
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.8",
			Message: "single-quoted scalar not allowed in KYAML; use double quotes",
			Pos:     n.pos,
			Token:   n.value,
		})
		return
	case scalarLiteral, scalarFolded:
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.4",
			Message: "block-style scalar (| or >) not allowed in KYAML; use double-quoted form",
			Pos:     n.pos,
			Token:   n.value,
		})
		return
	case scalarDoubleQuoted:
		return
	}
	val := n.value
	switch val {
	case "null", "true", "false", "":
		return
	case "Null", "NULL", "~":
		*out = append(*out, KYAMLViolation{
			Rule:    "R6.3",
			Message: fmt.Sprintf("YAML null variant %q not allowed in KYAML; use lowercase \"null\" or quote the value", val),
			Pos:     n.pos,
			Token:   val,
		})
		return
	case "True", "TRUE", "False", "FALSE":
		*out = append(*out, KYAMLViolation{
			Rule:    "R6.1",
			Message: fmt.Sprintf("non-canonical boolean %q not allowed in KYAML; use lowercase \"true\"/\"false\" or quote the value", val),
			Pos:     n.pos,
			Token:   val,
		})
		return
	}
	if _, ambiguous := typeAmbiguousKeys[val]; ambiguous {
		rule := "R12.12"
		if asKey {
			rule = "R5.2"
		}
		*out = append(*out, KYAMLViolation{
			Rule:    rule,
			Message: fmt.Sprintf("type-ambiguous word %q must be double-quoted in KYAML", val),
			Pos:     n.pos,
			Token:   val,
		})
		return
	}
	if isHexOctalBinaryInt(val) {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.11",
			Message: fmt.Sprintf("non-decimal integer literal %q not allowed in KYAML; use decimal", val),
			Pos:     n.pos,
			Token:   val,
		})
		return
	}
	switch val {
	case ".nan", ".NaN", ".NAN":
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.13",
			Message: "NaN literal not allowed in KYAML",
			Pos:     n.pos,
			Token:   val,
		})
		return
	case ".inf", ".Inf", ".INF", "-.inf", "-.Inf", "-.INF", "+.inf", "+.Inf", "+.INF":
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.13",
			Message: "infinity literal not allowed in KYAML",
			Pos:     n.pos,
			Token:   val,
		})
		return
	}
	if _, err := strconv.ParseInt(val, 10, 64); err == nil {
		return
	}
	if _, err := strconv.ParseFloat(val, 64); err == nil {
		lower := strings.ToLower(val)
		if strings.Contains(lower, "inf") || strings.Contains(lower, "nan") {
			*out = append(*out, KYAMLViolation{
				Rule:    "R12.13",
				Message: fmt.Sprintf("non-finite float %q not allowed in KYAML", val),
				Pos:     n.pos,
				Token:   val,
			})
			return
		}
		if strings.HasPrefix(lower, "0x") {
			*out = append(*out, KYAMLViolation{
				Rule:    "R12.11",
				Message: fmt.Sprintf("hex float literal %q not allowed in KYAML", val),
				Pos:     n.pos,
				Token:   val,
			})
			return
		}
		return
	}
	if !asKey {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.7",
			Message: fmt.Sprintf("plain (unquoted) string scalar %q not allowed as a value in KYAML; use double quotes", val),
			Pos:     n.pos,
			Token:   val,
		})
		return
	}
	if needsKeyQuoting(val) {
		*out = append(*out, KYAMLViolation{
			Rule:    "R5",
			Message: fmt.Sprintf("key %q must be double-quoted in KYAML", val),
			Pos:     n.pos,
			Token:   val,
		})
	}
}

func isHexOctalBinaryInt(s string) bool {
	if len(s) < 3 {
		return false
	}
	low := strings.ToLower(s)
	return strings.HasPrefix(low, "0x") || strings.HasPrefix(low, "0o") || strings.HasPrefix(low, "0b")
}

// Format parses data as YAML (any subset, including non-KYAML constructs)
// and re-emits it as canonical KYAML. Anchors and aliases are reified
// (expanded inline by the decoder); merge keys are resolved into flat key
// lists; explicit tags are stripped. Comments are preserved best-effort:
// the AST is pre-walked to extract head/inline/foot comments by path
// (R11.4) and re-inserted into the KYAML output via the existing comment
// post-pass.
//
// Format is idempotent on its output: Format(Format(x)) produces the same
// bytes as Format(x) for any valid YAML x.
func Format(data []byte, opts ...EncodeOption) ([]byte, error) {
	// Parse to AST first to extract comments before they're lost in decode.
	scanData, err := detectAndConvertEncoding(data)
	if err != nil {
		return nil, err
	}
	tokens, err := newScanner(scanData).scan()
	if err != nil {
		return nil, err
	}
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		return nil, err
	}

	comments := collectKYAMLComments(docs)

	// Decode to a generic any value (anchors and merge keys resolved here).
	var v any
	if err := UnmarshalWithOptions(data, &v, WithOrderedMap()); err != nil {
		return nil, err
	}

	encOpts := append([]EncodeOption{WithKYAML()}, opts...)
	if len(comments) > 0 {
		encOpts = append(encOpts, WithComment(comments))
	}
	return MarshalWithOptions(v, encOpts...)
}

// collectKYAMLComments walks the parsed AST and extracts every node's
// head/inline/foot comments into a path-keyed map suitable for
// [WithComment]. Paths use dotted notation for mapping keys
// ("metadata.name") and `[i]` for sequence indices. Per R11.5, this is
// best-effort — the post-pass inserter that consumes the map matches by
// last-path-segment.
//
// To prevent ambiguous-anchor failures (which break idempotence), we
// pre-walk the AST counting how often each key name occurs anywhere in
// the document. Names appearing more than once are skipped during
// extraction since the post-pass would match the wrong `key:` line.
func collectKYAMLComments(docs []*node) map[string][]Comment {
	keyCounts := make(map[string]int)
	for _, doc := range docs {
		if doc != nil {
			for _, child := range doc.children {
				countKYAMLKeys(child, keyCounts)
			}
		}
	}
	out := make(map[string][]Comment)
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		for _, child := range doc.children {
			walkKYAMLComments(child, "", out, keyCounts)
		}
	}
	return out
}

// countKYAMLKeys recursively counts how often each scalar key value
// occurs across the AST. Used to skip extraction for ambiguous keys.
func countKYAMLKeys(n *node, counts map[string]int) {
	if n == nil {
		return
	}
	switch n.kind {
	case nodeMapping:
		for i := 0; i+1 < len(n.children); i += 2 {
			k := n.children[i]
			if k != nil && k.kind == nodeScalar && k.value != "" {
				counts[k.value]++
			}
			if i+1 < len(n.children) {
				countKYAMLKeys(n.children[i+1], counts)
			}
		}
	case nodeSequence:
		for _, c := range n.children {
			countKYAMLKeys(c, counts)
		}
	}
}

func walkKYAMLComments(n *node, path string, out map[string][]Comment, keyCounts map[string]int) {
	if n == nil {
		return
	}

	addComments := func(p string) {
		if !pathIsAnchorable(p) {
			// The post-pass can't place comments on paths whose final
			// segment isn't a simple identifier (e.g., sequence-index-only
			// paths like "[0]"). Skip extraction to avoid triggering
			// spurious cuddle suppression for comments that won't appear.
			// R11.5 explicitly allows comment loss in these edge cases.
			return
		}
		// Skip if the path's last segment is an ambiguous key name
		// (appears more than once anywhere in the document). The post-pass
		// matches by last-segment, so duplicates would land at the first
		// match — Format pass 2 might re-read from a different node and
		// break idempotence.
		last := p
		if i := strings.LastIndex(p, "."); i >= 0 {
			last = p[i+1:]
		}
		if keyCounts[last] > 1 {
			return
		}
		// The scanner already strips the leading "#" and one optional space
		// from each comment, so the stored text is the raw content. Pass it
		// through verbatim — no further prefix stripping. Trim only trailing
		// whitespace and skip lines that are empty after that trim.
		if n.headComment != "" {
			for line := range strings.SplitSeq(n.headComment, "\n") {
				line = strings.TrimRight(line, " \t")
				if line == "" {
					continue
				}
				out[p] = append(out[p], Comment{Position: HeadCommentPos, Text: line})
			}
		}
		// Line and foot comments are only extracted for scalar nodes. On
		// compound values (mappings, sequences), the post-pass anchors at
		// the parent's `key:` line — but pass 1 places the comment there
		// while pass 2 reads it back as a head comment on the compound's
		// first child, breaking idempotence. Per R11.5, dropping line/foot
		// comments on compound values is permitted and avoids the
		// asymmetry.
		if n.kind == nodeScalar {
			if n.lineComment != "" {
				line := strings.TrimRight(n.lineComment, " \t")
				if line != "" {
					out[p] = append(out[p], Comment{Position: LineCommentPos, Text: line})
				}
			}
			if n.footComment != "" {
				for line := range strings.SplitSeq(n.footComment, "\n") {
					line = strings.TrimRight(line, " \t")
					if line == "" {
						continue
					}
					out[p] = append(out[p], Comment{Position: FootCommentPos, Text: line})
				}
			}
		}
	}

	addComments(path)

	switch n.kind {
	case nodeMapping:
		for i := 0; i+1 < len(n.children); i += 2 {
			keyNode := n.children[i]
			valNode := n.children[i+1]
			if keyNode == nil || keyNode.kind != nodeScalar {
				continue
			}
			// Skip keys that can't be reliably anchored by the path-based
			// comment lookup. The post-pass splits paths on `.` and uses
			// the last segment to match `key:` lines; keys containing
			// path-separator characters (or that are empty) would collide
			// with the path scheme and silently mis-place — or trigger
			// cuddle suppression for a comment that never gets emitted.
			//
			// We skip the ENTIRE subtree (no recursion into valNode):
			// recursing with the parent's path would mis-attribute inner
			// comments to the parent, breaking idempotence. R11.5
			// explicitly allows comment loss in these edge cases.
			if keyContainsPathSpecial(keyNode.value) {
				continue
			}
			childPath := keyNode.value
			if path != "" {
				childPath = path + "." + keyNode.value
			}
			// Comments on the key node attach at the entry's path. The
			// addComments closure (and walkKYAMLCommentsCollect, which
			// shares the same ambiguity rules) filters by anchorability
			// and key-name uniqueness, so we can pass through here
			// unconditionally — ambiguous-path comments are dropped at
			// the leaf write, not here.
			if keyNode.headComment != "" || keyNode.lineComment != "" || keyNode.footComment != "" {
				tmp := &node{
					headComment: keyNode.headComment,
					lineComment: keyNode.lineComment,
					footComment: keyNode.footComment,
				}
				walkKYAMLCommentsCollect(tmp, childPath, out, keyCounts)
			}
			walkKYAMLComments(valNode, childPath, out, keyCounts)
		}
	case nodeSequence:
		for i, child := range n.children {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			walkKYAMLComments(child, childPath, out, keyCounts)
		}
	}
}

// pathIsAnchorable reports whether path's last `.`-separated segment is a
// simple identifier — the only form that the comment post-pass can
// reliably match against `key:` lines in the emitted output. Sequence-
// index segments (`[N]`), empty segments, and keys with special chars
// fail this check.
func pathIsAnchorable(path string) bool {
	if path == "" {
		return false
	}
	last := path
	if i := strings.LastIndex(path, "."); i >= 0 {
		last = path[i+1:]
	}
	return !keyContainsPathSpecial(last)
}

// keyContainsPathSpecial reports whether s is unsuitable for use as the
// last segment of a comment-anchor path. The post-pass that places
// comments uses path splitting on `.` and a literal-prefix match on the
// emitted output, so any key that isn't a simple identifier
// ([A-Za-z_][A-Za-z0-9_-]*) cannot be reliably anchored — the comment
// would only trigger spurious cuddle suppression.
//
// Per R11.5, comment loss in these edge cases is permitted.
func keyContainsPathSpecial(s string) bool {
	if s == "" {
		return true
	}
	first := s[0]
	if first != '_' && (first < 'A' || first > 'Z') && (first < 'a' || first > 'z') {
		return true
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c != '_' && c != '-' &&
			(c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return true
		}
	}
	return false
}

// walkKYAMLCommentsCollect records comments from a synthetic node onto the
// given path. Used to merge a key node's comments into the same path as
// the value node. Same anchorability/uniqueness filtering as
// walkKYAMLComments's addComments helper.
func walkKYAMLCommentsCollect(n *node, path string, out map[string][]Comment, keyCounts map[string]int) {
	if !pathIsAnchorable(path) {
		return
	}
	last := path
	if i := strings.LastIndex(path, "."); i >= 0 {
		last = path[i+1:]
	}
	if keyCounts[last] > 1 {
		return
	}
	if n.headComment != "" {
		for line := range strings.SplitSeq(n.headComment, "\n") {
			line = strings.TrimRight(line, " \t")
			if line == "" {
				continue
			}
			out[path] = append(out[path], Comment{Position: HeadCommentPos, Text: line})
		}
	}
	if n.lineComment != "" {
		line := strings.TrimRight(n.lineComment, " \t")
		if line != "" {
			out[path] = append(out[path], Comment{Position: LineCommentPos, Text: line})
		}
	}
	if n.footComment != "" {
		for line := range strings.SplitSeq(n.footComment, "\n") {
			line = strings.TrimRight(line, " \t")
			if line == "" {
				continue
			}
			out[path] = append(out[path], Comment{Position: FootCommentPos, Text: line})
		}
	}
}

// Lint parses data as YAML and returns a slice of LintIssue values describing
// every KYAML deviation. Unlike [ValidateKYAML], Lint always returns the full
// list of issues. With [WithKYAMLLintCosmetic] in opts, Lint additionally
// reports cosmetic deviations.
func Lint(data []byte, opts ...DecodeOption) ([]LintIssue, error) {
	o := defaultDecodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	data, err := detectAndConvertEncoding(data)
	if err != nil {
		return nil, err
	}
	tokens, err := newScanner(data).scan()
	if err != nil {
		return nil, err
	}
	var issues []LintIssue
	for _, tok := range tokens {
		if tok.kind == tokenDirective {
			issues = append(issues, LintIssue{
				Rule:     "R12.9",
				Message:  fmt.Sprintf("YAML directive %q not allowed in KYAML", tok.value),
				Pos:      tok.pos,
				Severity: SeverityError,
			})
		}
	}
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		issues = append(issues, LintIssue{
			Rule:     "R3.1",
			Message:  "KYAML document must contain at least one document with the \"---\" header",
			Severity: SeverityError,
		})
		return issues, nil
	}
	var violations []KYAMLViolation
	for _, doc := range docs {
		validateKYAMLNode(doc, &violations)
	}
	for _, v := range violations {
		issues = append(issues, LintIssue{
			Rule:     v.Rule,
			Message:  v.Message,
			Pos:      v.Pos,
			Severity: SeverityError,
		})
	}
	if o.kyamlLintCosmetic {
		formatted, fErr := Format(data)
		if fErr == nil && !bytes.Equal(formatted, data) {
			issues = append(issues, LintIssue{
				Rule:     "R8/R9",
				Message:  "input does not match canonical KYAML formatting (run Format to apply)",
				Severity: SeverityWarning,
			})
		}
	}
	return issues, nil
}

// validateKYAMLBytes is the internal hook called by the decoder when
// WithStrictKYAML is set. The caller is responsible for token-level checks
// (directive rejection) before parsing — this function operates purely on
// the parsed AST. Returns a *KYAMLError if any violations are found.
func validateKYAMLBytes(docs []*node) error {
	if len(docs) == 0 {
		return &KYAMLError{Errors: []KYAMLViolation{{
			Rule:    "R3.1",
			Message: "KYAML document must contain at least one document with the \"---\" header",
		}}}
	}
	var violations []KYAMLViolation
	for _, doc := range docs {
		validateKYAMLNode(doc, &violations)
	}
	if len(violations) > 0 {
		return &KYAMLError{Errors: violations}
	}
	return nil
}
