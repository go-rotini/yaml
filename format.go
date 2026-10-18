package yaml

import (
	"bytes"
	"context"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
// The emitter is invoked from the existing encoder through opts.kyaml, which
// only [WithKYAML] sets. It is not safe for concurrent use; callers create
// one per Marshal call.
type kyamlEmitter struct {
	opts *encoderOptions
	buf  []byte
	ctx  context.Context
	seen map[uintptr]bool
	keys keyTracker
	// cycles finds map and slice cycles and bounds nesting; pointers are
	// tracked in seen at every depth.
	cycles cycleGuard
}

func newKYAMLEmitter(opts *encoderOptions) *kyamlEmitter {
	return &kyamlEmitter{
		opts: opts,
		ctx:  context.Background(),
		seen: make(map[uintptr]bool),
	}
}

// encodeKYAML encodes v as KYAML in place of e's block or flow output. It
// is reached only through opts.kyaml, so a program that never uses
// [WithKYAML] does not link the emitter.
func encodeKYAML(e *encoder, v reflect.Value) ([]byte, error) {
	ke := newKYAMLEmitter(e.opts)
	ke.ctx = e.ctx
	ke.keys.on = e.opts.comments != nil
	out, err := ke.encode(v)
	if err != nil {
		return nil, err
	}
	if e.opts.comments != nil {
		out = e.opts.applyComments(out, e.opts.comments, ke.keys.positions)
	}
	return out, nil
}

// encode produces the full KYAML document, including the leading "---" header
// and a trailing newline.
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

	// One deferred cleanup for every pointer descended through, instead of
	// one defer per loop iteration.
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

	key, err := e.cycles.enter(v)
	if err != nil {
		return err
	}
	defer e.cycles.leave(key)

	// RawValue may carry non-KYAML constructs (anchors, tags, block
	// style). Re-parse and re-emit so they don't flow through verbatim.
	// Must run before dispatchMarshaler since RawValue implements BytesMarshaler.
	if v.CanInterface() {
		if rv, ok := reflect.TypeAssert[RawValue](v); ok {
			return e.emitRawValue(rv, indent)
		}
	}

	if handled, err := e.dispatchMarshaler(v, indent); handled {
		return err
	}

	// KYAML uses JSON-first semantics for these types.
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
			if !isJSONNumber(s) {
				// Text that is not a number is written as a string, never
				// spliced into the output.
				e.emitString(s, indent)
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
			e.buf = append(e.buf, trueLiteral...)
		} else {
			e.buf = append(e.buf, falseLiteral...)
		}
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		e.buf = strconv.AppendInt(e.buf, v.Int(), 10)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		e.buf = strconv.AppendUint(e.buf, v.Uint(), 10)
		return nil
	case reflect.Float32, reflect.Float64:
		return e.emitFloat(v.Float(), v.Type().Bits())
	case reflect.Slice:
		if v.IsNil() {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		if isMapSliceType(v.Type()) {
			ms, _ := reflect.TypeAssert[MapSlice](v.Convert(reflect.TypeFor[MapSlice]()))
			return e.emitMapSlice(ms, indent)
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			e.emitString(base64.StdEncoding.EncodeToString(v.Bytes()), indent)
			return nil
		}
		return e.emitSequence(v, indent)
	case reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			e.emitString(base64.StdEncoding.EncodeToString(byteArray(v)), indent)
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

// dispatchMarshaler invokes the highest-priority marshaler registered for v.
// Priority under KYAML mode: custom marshalers, json.Marshaler,
// MarshalerContext, Marshaler, BytesMarshaler, encoding.TextMarshaler.
// Returns handled=true if any marshaler ran.
func (e *kyamlEmitter) dispatchMarshaler(v reflect.Value, indent int) (handled bool, err error) {
	if e.opts.customMarshalers != nil && v.CanInterface() {
		if fn, ok := e.opts.customMarshalers[v.Type()]; ok {
			out := reflect.ValueOf(fn).Call([]reflect.Value{v})
			if !out[1].IsNil() {
				e2, _ := reflect.TypeAssert[error](out[1])
				return true, e2
			}
			data, _ := reflect.TypeAssert[[]byte](out[0])
			// The custom marshaler may have produced YAML or JSON; re-parse
			// and re-emit to keep the output strictly KYAML.
			return true, e.emitRawJSONOrText(data, indent)
		}
	}

	// A Number is written as a JSON number, or as a string when its text
	// is not a number, rather than failing in MarshalJSON.
	if v.Type() == reflect.TypeFor[Number]() {
		if s, ok := numberJSON(v.String()); ok {
			e.buf = append(e.buf, s...)
		} else {
			e.emitString(v.String(), indent)
		}
		return true, nil
	}

	if v.CanInterface() {
		if m, ok := reflect.TypeAssert[json.Marshaler](v); ok {
			data, mErr := m.MarshalJSON()
			if mErr != nil {
				return true, fmt.Errorf("yaml: %T.MarshalJSON: %w", v.Interface(), mErr)
			}
			return true, e.emitRawJSON(data, indent)
		}
	}
	if v.CanAddr() {
		if m, ok := reflect.TypeAssert[json.Marshaler](v.Addr()); ok {
			data, mErr := m.MarshalJSON()
			if mErr != nil {
				return true, fmt.Errorf("yaml: %T.MarshalJSON: %w", v.Addr().Interface(), mErr)
			}
			return true, e.emitRawJSON(data, indent)
		}
	}

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

// emitRawValue parses RawValue's stored bytes and re-emits as canonical KYAML.
// Pass-through would risk leaking anchors, tags, or block style
// into the output.
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

// emitRawJSONOrText parses marshaler output as JSON, then as YAML, and
// re-emits it as KYAML. Output that is neither is emitted as a quoted string.
func (e *kyamlEmitter) emitRawJSONOrText(data []byte, indent int) error {
	var v any
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&v); err == nil && json.Valid(data) {
		return e.emit(reflect.ValueOf(v), indent)
	}
	n, err := parseRawYAML(data)
	if err != nil {
		return err
	}
	if n.tag == tagStr && n.value == string(data) {
		e.emitString(string(data), indent)
		return nil
	}
	if err := UnmarshalWithOptions(data, &v, WithOrderedMap()); err != nil {
		return err
	}
	return e.emit(reflect.ValueOf(v), indent)
}

// emitFloat emits a float in KYAML form. NaN and infinities are rejected
// with ErrUnsupported.
func (e *kyamlEmitter) emitFloat(f float64, bits int) error {
	if math.IsNaN(f) {
		return fmt.Errorf("yaml: NaN is not representable in KYAML: %w", ErrUnsupported)
	}
	if math.IsInf(f, 0) {
		return fmt.Errorf("yaml: Inf is not representable in KYAML: %w", ErrUnsupported)
	}
	// Whole-valued floats with magnitude below 1e16 render as integers. A
	// whole float64 below 2^63 is an exact integer, so its decimal form
	// reads back as the same float64, beyond 2^53 too; the cutoff only
	// chooses where the exponent form takes over.
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

// emitSequence renders a sequence in flow form with bracket cuddling. The
// "paired brackets" rule applies only to sequences: for
// [{...}], the sequence's `[` cuddles to the element's `{`, and `]` cuddles
// to `}`. The cuddled element's logical indent matches the sequence's, so
// its closing bracket lines up with `]`.
//
// Cuddling is suppressed whenever any comment is registered via
// [WithComment]: the post-pass that injects comments matches by line, so
// keeping brackets on their own lines is required for reliable placement.
func (e *kyamlEmitter) emitSequence(v reflect.Value, indent int) error {
	n := v.Len()
	if n == 0 {
		e.buf = append(e.buf, "[]"...)
		return nil
	}
	inner := indent + e.opts.indent
	e.buf = append(e.buf, '[')

	// When any comment is registered, suppress cuddling globally.
	suppressCuddle := len(e.opts.comments) > 0

	lastCuddled := false
	for i := range n {
		elem := v.Index(i)
		elemForCuddle := unwrapForCuddle(elem)
		startsBracket := !suppressCuddle && emitsAsCompound(elemForCuddle)

		// A compound element cuddles onto `[` or onto the previous
		// element's close bracket; any other element starts its own line.
		cuddled := startsBracket && (i == 0 || lastVisibleAfterCommaIsCloseBracket(e.buf))
		switch {
		case cuddled && i > 0:
			e.buf = append(e.buf, ' ')
		case !cuddled:
			e.buf = append(e.buf, '\n')
			e.writeIndent(inner)
		}
		lastCuddled = cuddled

		// Cuddled compound elements use the sequence's own indent so their
		// close bracket aligns with `]`.
		elemIndent := inner
		if cuddled {
			elemIndent = indent
		}

		idx, parent := e.keys.enter(e.buf, itemSeg(i))
		valueStart := len(e.buf)
		if err := e.emit(elem, elemIndent); err != nil {
			return err
		}

		if i < n-1 {
			e.buf = append(e.buf, ',')
		}
		e.keys.leave(e.buf, idx, parent, !startsCollection(e.buf[valueStart:]))
	}

	// Cuddle the close bracket only if the last element ended with one,
	// and not when cuddling is suppressed for comments.
	if lastCuddled && lastIsCloseBracket(e.buf) {
		e.buf = append(e.buf, ']')
	} else {
		e.buf = append(e.buf, ',', '\n')
		e.writeIndent(indent)
		e.buf = append(e.buf, ']')
	}
	return nil
}

// mapEntry is a sortable key/value pair used by emitMap, emitMapSlice, and
// emitStruct.
type mapEntry struct {
	// rawKey is invalid for struct fields; populated otherwise for use by
	// cuddle and comment lookups.
	rawKey reflect.Value
	// keyStr is the rendered key text with any required quoting applied.
	keyStr string
	value  reflect.Value
	// emitNullDirect skips the normal value path and writes "null". Used
	// when the caller has already resolved the value to nil.
	emitNullDirect bool
}

// emitMap renders a Go map in KYAML flow form. Keys are sorted
// lexicographically.
func (e *kyamlEmitter) emitMap(v reflect.Value, indent int) error {
	if v.Len() == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}

	// Only string (or TextMarshaler) keys are allowed.
	keys := v.MapKeys()
	entries := make([]mapEntry, 0, len(keys))
	for _, k := range keys {
		if e.opts.omitEmpty && isEmpty(v.MapIndex(k)) {
			continue
		}
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

// emitMapSlice renders an ordered MapSlice in insertion order.
func (e *kyamlEmitter) emitMapSlice(ms MapSlice, indent int) error {
	if len(ms) == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}
	entries := make([]mapEntry, 0, len(ms))
	for _, item := range ms {
		if e.opts.omitEmpty && (item.Value == nil || isEmpty(reflect.ValueOf(item.Value))) {
			continue
		}
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

// emitStruct renders a struct in KYAML form. Field names come from the json
// tag first, then the yaml tag.
func (e *kyamlEmitter) emitStruct(v reflect.Value, indent int) error {
	sf := getKYAMLStructFields(v.Type())
	if len(sf.conflicts) > 0 {
		return fmt.Errorf("yaml: struct %s has conflicting field names: %s: %w",
			v.Type(), strings.Join(sf.conflicts, ", "), errConflictingFields)
	}

	entries := make([]mapEntry, 0, len(sf.fields))
	for i := range sf.fields {
		fi := &sf.fields[i]
		field := fieldForEncode(v, fi.index)
		if !field.IsValid() {
			continue
		}
		if omitField(e.opts, fi, field) {
			continue
		}
		if fi.inline && field.Kind() == reflect.Map {
			mapKeys := field.MapKeys()
			subEntries := make([]mapEntry, 0, len(mapKeys))
			for _, k := range mapKeys {
				if e.opts.omitEmpty && isEmpty(field.MapIndex(k)) {
					continue
				}
				ks, err := mapKeyToString(k)
				if err != nil {
					return err
				}
				if sf.namesField(ks) {
					continue
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
// structs. The "paired brackets" rule applies only to sequences, so a
// mapping's close `}` always sits on its own line at the mapping's indent,
// with a trailing comma after the final entry.
func (e *kyamlEmitter) emitMappingEntries(entries []mapEntry, indent int) error {
	n := len(entries)
	if n == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}
	inner := indent + e.opts.indent
	e.buf = append(e.buf, '{')

	for _, ent := range entries {
		// One field per line.
		e.buf = append(e.buf, '\n')
		e.writeIndent(inner)

		idx, parent := e.keys.enter(e.buf, ent.keyStr)
		e.buf = append(e.buf, e.formatKey(ent.keyStr)...)
		e.buf = append(e.buf, ':', ' ')

		valueStart := len(e.buf)
		switch {
		case ent.emitNullDirect, !ent.value.IsValid():
			e.buf = append(e.buf, "null"...)
		default:
			if err := e.emit(ent.value, inner); err != nil {
				return err
			}
		}

		// Trailing comma after every entry.
		e.buf = append(e.buf, ',')
		e.keys.leave(e.buf, idx, parent, !startsCollection(e.buf[valueStart:]))
	}

	e.buf = append(e.buf, '\n')
	e.writeIndent(indent)
	e.buf = append(e.buf, '}')
	return nil
}

// startsCollection reports whether emitted value text opens a flow mapping
// or sequence.
func startsCollection(value []byte) bool {
	return len(value) > 0 && (value[0] == '{' || value[0] == '[')
}

// formatKey returns the rendered key text, quoted when the key is
// type-ambiguous or not a simple identifier.
func (e *kyamlEmitter) formatKey(key string) string {
	if e.opts.kyamlAlwaysQuoteKeys {
		return quoteKYAMLString(key)
	}
	if needsKeyQuoting(key) {
		return quoteKYAMLString(key)
	}
	return key
}

// isJSONNumber reports whether s is a number in JSON syntax.
func isJSONNumber(s string) bool {
	if s == "" || (s[0] != '-' && (s[0] < '0' || s[0] > '9')) {
		return false
	}
	if last := s[len(s)-1]; last < '0' || last > '9' {
		return false
	}
	return json.Valid([]byte(s))
}

// emitString renders a string, always double-quoted. Long
// multi-line strings use the flow-folded form; everything else uses a
// single-line double-quoted scalar with embedded \n escapes.
//
// shouldUseFlowFold compares the fully-escaped length (post escapeKYAMLLine)
// rather than the raw input length. The escaped length is invariant under
// UTF-8 normalization, so the trigger does not flip across encode/decode
// cycles, even when invalid input bytes have been replaced by U+FFFD.
func (e *kyamlEmitter) emitString(s string, indent int) {
	if shouldUseFlowFold(s, e.opts.lineWidth) {
		e.buf = append(e.buf, kyamlFlowFold(s, indent+e.opts.indent)...)
		return
	}
	e.buf = append(e.buf, quoteKYAMLString(s)...)
}

// shouldUseFlowFold reports whether s should be emitted flow-folded
// rather than a single-line scalar. All three conditions must hold:
//  1. s contains at least two literal newlines, so the trigger can't flip
//     on round-trip.
//  2. The fully-escaped single-line form exceeds lineWidth.
//  3. No continuation line begins with whitespace. YAML strips leading
//     whitespace from the line after a `\<newline>` continuation, so a
//     string with such a line cannot be folded losslessly.
func shouldUseFlowFold(s string, lineWidth int) bool {
	if lineWidth <= 0 {
		return false
	}
	if strings.Count(s, "\n") < 2 {
		return false
	}
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			return false
		}
	}
	// +2 accounts for the surrounding double quotes added by quoteKYAMLString.
	return len(escapeKYAMLLine(s))+2 > lineWidth
}

// kyamlFlowFold renders s using the flow-folded form: a multi-line
// double-quoted string where each source line ends with `\n\` so the parser
// can fold away the trailing newline and leading whitespace, leaving only
// the literal `\n` escape's newline in the decoded value. contIndent is the
// column at which continuation lines start.
//
// Example output for "first\nsecond\nthird":
//
//	"first\n\
//	  second\n\
//	  third"
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

// quoteKYAMLString returns s wrapped in double quotes with KYAML escapes applied.
func quoteKYAMLString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	b.WriteString(escapeKYAMLLine(s))
	b.WriteByte('"')
	return b.String()
}

// escapeKYAMLLine applies the KYAML escape table to s and returns the body
// without surrounding quotes.
func escapeKYAMLLine(s string) string {
	if !needsKYAMLEscape(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 cannot round-trip through YAML's \xHH escape
			// (which encodes a code point, not a raw byte). Replacing with
			// the literal U+FFFD UTF-8 bytes matches encoding/json and
			// keeps Format idempotent: subsequent passes see valid UTF-8
			// and emit it unchanged.
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
			switch {
			case r < 0x20 || (r >= 0x7F && r <= 0x9F):
				fmt.Fprintf(&b, `\x%02x`, r)
			case r == 0xFEFF || r == 0xFFFE || r == 0xFFFF:
				fmt.Fprintf(&b, `\u%04X`, r)
			default:
				b.WriteRune(r)
			}
		}
		i += size
	}
	return b.String()
}

// needsKYAMLEscape reports whether escapeKYAMLLine changes s: whether s holds
// a byte other than printable ASCII, or a double quote or backslash.
func needsKYAMLEscape(s string) bool {
	for i := range len(s) {
		if c := s[i]; c < 0x20 || c >= 0x7F || c == '"' || c == '\\' {
			return true
		}
	}
	return false
}

// typeAmbiguousKeys lists scalars that KYAML requires be double-quoted
// because YAML 1.1 would otherwise parse them as a boolean or null.
var typeAmbiguousKeys = map[string]struct{}{
	"y": {}, "Y": {}, "yes": {}, "Yes": {}, "YES": {},
	"n": {}, "N": {}, "no": {}, "No": {}, "NO": {},
	trueLiteral: {}, "True": {}, "TRUE": {},
	falseLiteral: {}, "False": {}, "FALSE": {},
	"on": {}, "On": {}, "ON": {},
	"off": {}, "Off": {}, "OFF": {},
	"null": {}, "Null": {}, "NULL": {},
	"~": {},
}

// needsKeyQuoting reports whether key must be quoted in KYAML.
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
	return !validKYAMLKey(key)
}

// validKYAMLKey reports whether key may be emitted unquoted inside a KYAML
// flow mapping. The character class [A-Za-z_][A-Za-z0-9_./-]* is conservative
// by design: any flow-context indicator (brackets, braces, comma, colon,
// etc.) forces quoting to avoid parser ambiguity.
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

// mapKeyToString converts a reflect.Value map key to a string, or returns
// an error if the key is not a string or [encoding.TextMarshaler].
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
		if tm, ok := reflect.TypeAssert[encoding.TextMarshaler](k); ok {
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
	if writesAsText(v) {
		return false
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		// []byte and [N]byte render as a base64 string, not a sequence.
		return v.Type().Elem().Kind() != reflect.Uint8
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

// writesAsText reports whether dispatchMarshaler writes v through
// [encoding.TextMarshaler], whose output is a string.
func writesAsText(v reflect.Value) bool {
	if !v.CanInterface() {
		return false
	}
	if _, ok := reflect.TypeAssert[encoding.TextMarshaler](v); !ok {
		return false
	}
	switch v.Interface().(type) {
	case json.Marshaler, MarshalerContext, Marshaler, BytesMarshaler:
		return false
	}
	if v.CanAddr() {
		if _, ok := reflect.TypeAssert[json.Marshaler](v.Addr()); ok {
			return false
		}
	}
	return true
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

// kyamlStructFieldCache memoizes the per-type KYAML struct-field layout.
// Field names under KYAML come from the json tag first, then the yaml tag, and the exact Go field name (no lowercasing) when neither tag
// is present.
var kyamlStructFieldCache sync.Map

func getKYAMLStructFields(t reflect.Type) *structFields {
	if cached, ok := kyamlStructFieldCache.Load(t); ok {
		sf, _ := cached.(*structFields)
		return sf
	}
	// Match encoding/json: untagged fields keep their exact Go name rather
	// than the lowercased form yaml mode uses.
	sf := buildStructFields(t, kyamlFieldTag, func(name string) string { return name })
	kyamlStructFieldCache.Store(t, sf)
	return sf
}

// kyamlFieldTag returns the field options of f under KYAML: the json tag
// gives the name, and the yaml tag may still contribute omitempty, inline,
// required, default= and "-".
func kyamlFieldTag(f reflect.StructField) fieldInfo {
	jsonTag := f.Tag.Get("json")
	yamlTag := f.Tag.Get("yaml")

	var fi fieldInfo
	if jsonTag != "" {
		fi = parseTag(jsonTag)
	}
	if yamlTag != "" {
		y := parseTag(yamlTag)
		if jsonTag == "" {
			return y
		}
		fi.omitEmpty = fi.omitEmpty || y.omitEmpty
		fi.inline = fi.inline || y.inline
		fi.required = fi.required || y.required
		if y.hasDefault {
			fi.defaultValue = y.defaultValue
			fi.hasDefault = true
		}
		fi.skip = fi.skip || y.skip
	}
	return fi
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
// into an untyped value and then re-encoded as YAML. Numbers keep their
// JSON text.
func FromJSON(jsonData []byte) ([]byte, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(jsonData))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("yaml: json decode: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("yaml: json decode: %w", errJSONTrailingData)
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

// ValidKYAML reports whether data conforms to strict KYAML as defined by
// [KEP-5295]. It is equivalent to [ValidateKYAML](data) == nil.
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
func ValidKYAML(data []byte) bool {
	return ValidateKYAML(data) == nil
}

// ValidateKYAML parses data as YAML and reports any KYAML conformance
// violations. It returns nil if data is a valid KYAML document, a
// [*KYAMLError] carrying every violation (or only the first YAML directive
// found, if any), or the syntax error when data is not valid YAML.
// Validation is structural per the rules of [KEP-5295]; cosmetic deviations (indentation, bracket cuddling,
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
	case nullLiteral, trueLiteral, falseLiteral, "":
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
	_, err := strconv.ParseFloat(val, 64)
	if errors.Is(err, strconv.ErrRange) && isJSONNumber(val) {
		// A number beyond the float64 range is still a number.
		return
	}
	if err == nil {
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

// Format parses data as YAML, including non-KYAML constructs, and re-emits
// it as canonical KYAML. Anchors and aliases are expanded inline, merge
// keys are flattened, and explicit tags are stripped. Only the first
// document of a multi-document stream is formatted. Comments are kept on a
// best-effort basis; ones that cannot be placed reliably (for example on a
// key name that appears more than once) are dropped.
//
// Format is idempotent on its output: Format(Format(x)) equals Format(x)
// for any valid YAML x.
func Format(data []byte, opts ...EncodeOption) ([]byte, error) {
	// Parse to an AST first; the decoder discards comments.
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

	// Only docs[0] is encoded below (multi-doc streams should use
	// Encoder/Decoder), so extracting comments from later documents would
	// feed the encoder a map that does not match the data and break
	// idempotence.
	var firstDoc []*node
	if len(docs) > 0 {
		firstDoc = docs[:1]
	}
	comments := collectKYAMLComments(firstDoc)

	// Decode the parsed documents as UnmarshalWithOptions(data, &v,
	// WithOrderedMap()) would, without scanning and parsing data again.
	var v any
	o := defaultDecodeOptions()
	WithOrderedMap()(o)
	if err := decodeFirst(docs, nil, p.nodeCount, o, reflect.ValueOf(&v)); err != nil {
		return nil, err
	}

	encOpts := append([]EncodeOption{WithKYAML()}, opts...)
	if len(comments) > 0 {
		encOpts = append(encOpts, WithComment(comments))
	}
	return MarshalWithOptions(v, encOpts...)
}

// collectKYAMLComments walks the AST and extracts every node's head, line,
// and foot comments into a path-keyed map suitable for [WithComment].
// Paths use dotted notation for mapping keys ("metadata.name") and "[i]"
// for sequence indices.
//
// Comment placement is best-effort: the post-pass that consumes
// the map matches by last path segment. A pre-walk counts how often each
// key name appears in the document; duplicates are dropped to avoid
// mis-anchoring, which would break Format idempotence.
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

// countKYAMLKeys counts how often each scalar key value appears in the AST,
// so collectKYAMLComments can drop comments anchored on ambiguous keys.
func countKYAMLKeys(n *node, counts map[string]int) {
	c := keyCounter{counts: counts}
	c.count(n, 1)
}

// keyCounter counts key names for countKYAMLKeys. Format writes an anchored
// node again in place of each alias to it, so the keys below an alias count
// as repeated.
type keyCounter struct {
	counts   map[string]int
	expanded map[*node]bool
}

func (c *keyCounter) count(n *node, weight int) {
	if n == nil {
		return
	}
	switch n.kind {
	case nodeAlias:
		if n.ref == nil || n.ref.n == nil || c.expanded[n.ref.n] {
			return
		}
		if c.expanded == nil {
			c.expanded = make(map[*node]bool)
		}
		c.expanded[n.ref.n] = true
		c.count(n.ref.n, 2)
	case nodeMapping:
		for i := 0; i+1 < len(n.children); i += 2 {
			k := n.children[i]
			if k != nil && k.kind == nodeAlias && k.ref != nil && k.ref.n != nil {
				k = k.ref.n
			}
			if k != nil && k.kind == nodeScalar && k.value != "" {
				c.counts[k.value] += weight
			}
			c.count(n.children[i+1], weight)
		}
	case nodeSequence:
		for _, child := range n.children {
			c.count(child, weight)
		}
	}
}

func walkKYAMLComments(n *node, path string, out map[string][]Comment, keyCounts map[string]int) {
	if n == nil {
		return
	}

	addComments := func(p string) {
		// Drop comments on paths the post-pass can't anchor (sequence-index-only paths, keys with special chars).
		// Skipping here also avoids spurious cuddle suppression for a
		// comment that would never be emitted.
		if !pathIsAnchorable(p) {
			return
		}
		// Drop comments anchored on a key name that appears more than once
		// in the document. The post-pass matches by last segment, so a
		// duplicate would land at the first occurrence; Format pass 2
		// could then read it back at a different node and break idempotence.
		last := p
		if i := strings.LastIndex(p, "."); i >= 0 {
			last = p[i+1:]
		}
		if keyCounts[last] > 1 {
			return
		}
		// The scanner has already stripped the leading "#" and one optional
		// space, so write the stored text through verbatim, trimming only
		// trailing whitespace and skipping lines that become empty.
		if n.headComment != "" {
			for line := range strings.SplitSeq(n.headComment, "\n") {
				line = strings.TrimRight(line, " \t")
				if line == "" {
					continue
				}
				out[p] = append(out[p], Comment{Position: HeadCommentPos, Text: line})
			}
		}
		// Only scalar nodes contribute line and foot comments. On compound
		// values the post-pass would anchor at the parent's `key:` line in
		// pass 1, then the parser would re-read the same text as a head
		// comment on the first child in pass 2, breaking idempotence.
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
			// Skip keys whose value cannot serve as the last segment of a
			// dotted path: empty, or containing path-separator characters.
			// Skipping the entire subtree (no recursion into valNode) is
			// required because recursing with the parent's path would
			// mis-attribute inner comments; those comments are lost.
			if keyContainsPathSpecial(keyNode.value) {
				continue
			}
			childPath := keyNode.value
			if path != "" {
				childPath = path + "." + keyNode.value
			}
			// Forward all comments on the key node through walkKYAMLCommentsCollect,
			// which applies the same anchorability and uniqueness filters
			// before recording at the leaf.
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

// pathIsAnchorable reports whether path's last "."-separated segment is a
// simple identifier the comment post-pass can match against a "key:" line
// in the emitted output. Sequence-index segments ("[N]"), empty segments,
// and keys with special characters all fail this check.
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

// keyContainsPathSpecial reports whether s is unsuitable as the last
// segment of a comment-anchor path. The post-pass uses "."-splitting and
// literal-prefix matching, so any key that is not a simple identifier
// ([A-Za-z_][A-Za-z0-9_-]*) is rejected, and its comments are dropped.
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

// walkKYAMLCommentsCollect records the comments on a synthetic node at
// path, applying the same anchorability and uniqueness filters as the
// addComments helper inside walkKYAMLComments. It is used to merge a key
// node's comments onto its value node's path.
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

// Lint parses data as YAML and returns every KYAML deviation as a
// [LintIssue] with [SeverityError]. Unlike [ValidateKYAML], which stops at
// the first YAML directive, Lint always reports the full list. With
// [WithKYAMLLintCosmetic] in opts, Lint also adds a [SeverityWarning] issue
// when data differs from the output of [Format]. The error is non-nil only
// when data cannot be parsed as YAML.
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

// validateKYAMLBytes is the decoder's internal hook for WithStrictKYAML.
// It operates purely on the parsed AST; token-level checks such as
// directive rejection are the caller's responsibility. Returns a
// [*KYAMLError] if any violations are found.
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
