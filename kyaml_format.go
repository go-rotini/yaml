package yaml

import (
	"context"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
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
			defer delete(e.seen, ptr)
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
				return err
			}
			e.buf = append(e.buf, data...)
			return nil
		case time.Duration:
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
			var any any
			if err := json.Unmarshal(t, &any); err != nil {
				return fmt.Errorf("yaml: cannot decode json.RawMessage: %w", err)
			}
			return e.emit(reflect.ValueOf(any), indent)
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
				return true, mErr
			}
			return true, e.emitRawJSON(data, indent)
		}
	}
	if v.CanAddr() {
		if m, ok := v.Addr().Interface().(json.Marshaler); ok {
			data, mErr := m.MarshalJSON()
			if mErr != nil {
				return true, mErr
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
func (e *kyamlEmitter) emitSequence(v reflect.Value, indent int) error {
	n := v.Len()
	if n == 0 {
		e.buf = append(e.buf, "[]"...)
		return nil
	}
	inner := indent + e.opts.indent
	e.buf = append(e.buf, '[')

	for i := range n {
		elem := v.Index(i)
		elemForCuddle := unwrapForCuddle(elem)
		startsBracket := emitsAsCompound(elemForCuddle)

		// Pre-element placement.
		if i == 0 {
			if !startsBracket {
				e.buf = append(e.buf, '\n')
				e.writeIndent(inner)
			}
			// else: cuddled open — emit element directly after `[`.
		} else {
			// Previous iteration appended ','.
			if startsBracket && lastVisibleIsCloseBracket(e.buf, ',') {
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
	// (and we're cuddling — i.e., the last element is compound).
	if lastIsCloseBracket(e.buf) {
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
		if ent.emitNullDirect {
			e.buf = append(e.buf, "null"...)
		} else if !ent.value.IsValid() {
			e.buf = append(e.buf, "null"...)
		} else {
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

// emitString renders a string value per R6.4 (always double-quoted).
func (e *kyamlEmitter) emitString(s string, indent int) {
	if shouldUseFlowFold(s, e.opts.lineWidth) {
		e.buf = append(e.buf, kyamlFlowFold(s, indent+e.opts.indent)...)
		return
	}
	e.buf = append(e.buf, quoteKYAMLString(s)...)
}

func (e *kyamlEmitter) writeIndent(n int) {
	for range n {
		e.buf = append(e.buf, ' ')
	}
}

// shouldUseFlowFold decides whether a string is a candidate for KYAML's
// flow-folded multi-line form (R10.4): contains a literal newline AND the
// fully-escaped single-line form would exceed lineWidth.
func shouldUseFlowFold(s string, lineWidth int) bool {
	if !strings.Contains(s, "\n") {
		return false
	}
	if lineWidth <= 0 {
		return false
	}
	// Approximate the single-line escaped length: each newline becomes "\n" (2 chars),
	// quotes wrap (2 more), other escapes are roughly the same length.
	approx := len(s) + strings.Count(s, "\n") + 2
	return approx > lineWidth
}

// kyamlFlowFold renders s using the flow-folding form per R10. The output is
// a multi-line double-quoted string with embedded \n escapes for actual
// newlines and trailing-backslash continuations to wrap long lines.
func kyamlFlowFold(s string, contIndent int) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	b.WriteByte('"')
	indentStr := strings.Repeat(" ", contIndent)
	for i, line := range lines {
		// Escape this line's content.
		escaped := escapeKYAMLLine(line)
		b.WriteString(escaped)
		// If not the final line, emit \n followed by a flow-fold continuation.
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
			// Invalid UTF-8: emit replacement character (matches encoding/json).
			b.WriteString(`\xEF\xBF\xBD`)
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
	// Validate against the KYAML key character class. We accept
	// [A-Za-z_][A-Za-z0-9_./-]* with an optional [A-Za-z0-9_./-]+ suffix in
	// brackets (label-key prefix syntax e.g. "kubernetes.io/role").
	if !validKYAMLKey(key) {
		return true
	}
	return false
}

func validKYAMLKey(key string) bool {
	if key == "" {
		return false
	}
	first := key[0]
	if !(first == '_' || (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')) {
		return false
	}
	inBracket := false
	for i := 1; i < len(key); i++ {
		c := key[i]
		if c == '[' {
			if inBracket {
				return false
			}
			inBracket = true
			continue
		}
		if c == ']' {
			if !inBracket {
				return false
			}
			if i != len(key)-1 {
				return false
			}
			inBracket = false
			continue
		}
		if !isKYAMLKeyChar(c) {
			return false
		}
	}
	if inBracket {
		return false
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
	for i := len(buf) - 1; i >= 0; i-- {
		c := buf[i]
		if c == ' ' || c == '\n' || c == '\t' {
			continue
		}
		return c == '}' || c == ']'
	}
	return false
}

// lastVisibleIsCloseBracket returns true if, after virtually skipping a
// trailing skipChar, the most recent visible byte is `}` or `]`.
func lastVisibleIsCloseBracket(buf []byte, skipChar byte) bool {
	if len(buf) == 0 {
		return false
	}
	end := len(buf) - 1
	if buf[end] == skipChar {
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
