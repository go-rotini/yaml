package yaml

import (
	"bytes"
	"cmp"
	"context"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type encoder struct {
	opts        *encoderOptions
	ctx         context.Context
	buf         []byte
	encodingKey bool
	// flowDepth counts the flow collections being written, where block
	// scalars are not allowed.
	flowDepth int
	keys      keyTracker
	cycles    cycleGuard
}

// cycleCheckDepth is the nesting depth after which the encoders look for
// cyclic values. Shallower values cannot hold a cycle that matters, so the
// common case pays nothing.
const cycleCheckDepth = 1000

// cycleGuard detects cyclic values while encoding: values nested more than
// [maxNestingDepth] deep, and, past cycleCheckDepth, a pointer, map, slice
// or struct already being written further up.
type cycleGuard struct {
	depth int
	seen  map[cycleKey]struct{}
}

// cycleKey identifies a value being written: where its data lives, its
// length (for slices) and its type.
type cycleKey struct {
	p uintptr
	n int
	t reflect.Type
}

// enter counts one level of nesting for v. The caller must call leave with
// the result when it is done with v.
func (g *cycleGuard) enter(v reflect.Value) (cycleKey, error) {
	g.depth++
	if g.depth > maxNestingDepth {
		return cycleKey{}, fmt.Errorf("yaml: value of type %s is nested more than %d deep, or is cyclic: %w", v.Type(), maxNestingDepth, ErrUnsupported)
	}
	if g.depth <= cycleCheckDepth {
		return cycleKey{}, nil
	}
	var key cycleKey
	switch v.Kind() {
	case reflect.Pointer, reflect.Map:
		if v.IsNil() {
			return cycleKey{}, nil
		}
		key = cycleKey{p: v.Pointer(), t: v.Type()}
	case reflect.Slice:
		if v.IsNil() || v.Len() == 0 {
			return cycleKey{}, nil
		}
		key = cycleKey{p: v.Pointer(), n: v.Len(), t: v.Type()}
	case reflect.Struct:
		if !v.CanAddr() {
			return cycleKey{}, nil
		}
		key = cycleKey{p: v.UnsafeAddr(), t: v.Type()}
	default:
		return cycleKey{}, nil
	}
	if _, ok := g.seen[key]; ok {
		return cycleKey{}, fmt.Errorf("yaml: cyclic value of type %s: %w", v.Type(), ErrUnsupported)
	}
	if g.seen == nil {
		g.seen = make(map[cycleKey]struct{})
	}
	g.seen[key] = struct{}{}
	return key, nil
}

// leave ends the level started by enter.
func (g *cycleGuard) leave(key cycleKey) {
	g.depth--
	if key.t != nil {
		delete(g.seen, key)
	}
}

func newEncoder(opts *encoderOptions) *encoder {
	return &encoder{opts: opts, ctx: context.Background()}
}

func (e *encoder) encode(v reflect.Value) ([]byte, error) {
	if e.opts.kyaml {
		ke := newKYAMLEmitter(e.opts)
		ke.ctx = e.ctx
		ke.keys.on = e.opts.comments != nil
		out, err := ke.encode(v)
		if err != nil {
			return nil, err
		}
		if e.opts.comments != nil {
			out = applyComments(out, e.opts.comments, ke.keys.positions)
		}
		return out, nil
	}
	if err := checkBlockIndent(e.opts); err != nil {
		return nil, err
	}
	// Most documents fit the initial buffer, which saves growing it from
	// nothing one doubling at a time.
	if e.buf == nil {
		e.buf = make([]byte, 0, 256)
	}
	e.buf = e.buf[:0]
	e.keys = keyTracker{on: e.opts.comments != nil}
	if err := e.marshalValue(v, 0, false); err != nil {
		return nil, err
	}
	if len(e.buf) == 0 {
		e.buf = nil // as if nothing was ever appended
	}
	if len(e.buf) > 0 && e.buf[len(e.buf)-1] != '\n' {
		e.buf = append(e.buf, '\n')
	}
	if e.opts.comments != nil {
		e.buf = applyComments(e.buf, e.opts.comments, e.keys.positions)
	}
	return e.buf, nil
}

// checkBlockIndent reports an error when block-style output is selected
// with an indent below 1, which would write nested collections at their
// parent's column.
func checkBlockIndent(o *encoderOptions) error {
	if o.indent < 1 && !o.flow && !o.kyaml {
		return fmt.Errorf("yaml: WithIndent(%d) must be at least 1 for block-style output: %w", o.indent, ErrOptionConflict)
	}
	return nil
}

// keyPos records where the entry at path was written, for placing
// comments: line is the line its key or sequence item starts on, end the
// line its value ends on. lineCommentAtEnd places a line comment on the
// end line, after a scalar that may span lines.
type keyPos struct {
	path             string
	line, end        int
	lineCommentAtEnd bool
}

// keyTracker records the position of every mapping entry and sequence item
// while encoding, so that [WithComment] comments are placed by structure
// rather than by matching text.
type keyTracker struct {
	on        bool
	path      string
	positions []keyPos
	counted   int
	lines     int
}

// line returns the 0-based line that the next byte appended to buf is on.
func (k *keyTracker) line(buf []byte) int {
	k.lines += bytes.Count(buf[k.counted:], []byte{'\n'})
	k.counted = len(buf)
	return k.lines
}

// enter records the start of the entry seg (a key, or "[i]" for a sequence
// item) and makes it the current path. leave takes what it returns.
func (k *keyTracker) enter(buf []byte, seg string) (idx int, parent string) {
	if !k.on {
		return -1, ""
	}
	parent = k.path
	switch {
	case strings.HasPrefix(seg, "["):
		k.path = parent + seg
	case parent == "":
		k.path = seg
	default:
		k.path = parent + "." + seg
	}
	k.positions = append(k.positions, keyPos{path: k.path, line: k.line(buf)})
	return len(k.positions) - 1, parent
}

// leave records the end of the entry started by enter.
func (k *keyTracker) leave(buf []byte, idx int, parent string, lineCommentAtEnd bool) {
	if idx < 0 {
		return
	}
	k.positions[idx].end = k.line(buf)
	k.positions[idx].lineCommentAtEnd = lineCommentAtEnd
	k.path = parent
}

// findKeyPos returns the position recorded for path, which may start with
// "$" or "$.". A path that matches no entry falls back to the first entry
// whose last segment equals the path's last segment.
func findKeyPos(positions []keyPos, path string) (keyPos, bool) {
	path = strings.TrimPrefix(strings.TrimPrefix(path, "$"), ".")
	if path == "" {
		return keyPos{}, false
	}
	for _, kp := range positions {
		if kp.path == path {
			return kp, true
		}
	}
	last := path[strings.LastIndex(path, ".")+1:]
	for _, kp := range positions {
		if kp.path[strings.LastIndex(kp.path, ".")+1:] == last {
			return kp, true
		}
	}
	return keyPos{}, false
}

// applyComments inserts comments at the lines recorded in positions: a head
// comment on its own line before the entry, a line comment at the end of
// the entry's first line (or its last, for a multi-line scalar), and a foot
// comment on its own line after the entry's value.
func applyComments(buf []byte, comments map[string][]Comment, positions []keyPos) []byte {
	type extra struct{ head, tail, foot []string }
	lines := strings.Split(string(buf), "\n")
	added := make(map[int]*extra)
	at := func(i int) *extra {
		if added[i] == nil {
			added[i] = &extra{}
		}
		return added[i]
	}
	indentOf := func(i int) string {
		return lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " "))]
	}
	for _, path := range slices.Sorted(maps.Keys(comments)) {
		kp, ok := findKeyPos(positions, path)
		if !ok || kp.line >= len(lines) || kp.end >= len(lines) {
			continue
		}
		for _, c := range comments[path] {
			// Every line of the text is a comment line of its own, so a
			// line break cannot end the comment early.
			text := commentLines(c.Text)
			switch c.Position {
			case HeadCommentPos:
				for _, t := range text {
					at(kp.line).head = append(at(kp.line).head, indentOf(kp.line)+"# "+t)
				}
			case LineCommentPos:
				l := kp.line
				if kp.lineCommentAtEnd {
					l = kp.end
				}
				at(l).tail = append(at(l).tail, " # "+text[0])
				for _, t := range text[1:] {
					at(l).foot = append(at(l).foot, indentOf(kp.line)+"# "+t)
				}
			case FootCommentPos:
				for _, t := range text {
					at(kp.end).foot = append(at(kp.end).foot, indentOf(kp.line)+"# "+t)
				}
			}
		}
	}
	if len(added) == 0 {
		return buf
	}
	out := make([]string, 0, len(lines)+len(added))
	for i, line := range lines {
		x := added[i]
		if x == nil {
			out = append(out, line)
			continue
		}
		out = append(out, x.head...)
		out = append(out, line+strings.Join(x.tail, ""))
		out = append(out, x.foot...)
	}
	return []byte(strings.Join(out, "\n"))
}

// commentLines splits comment text at its line breaks (LF, CR, CRLF, NEL,
// LS and PS). A comment cannot escape anything, so each other character
// that is not printable, and each byte that is not valid UTF-8, becomes
// U+FFFD.
func commentLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch r {
		case '\r', '\n', 0x85, 0x2028, 0x2029:
			lines = append(lines, printableComment(text[start:i]))
			if r == '\r' && i+1 < len(text) && text[i+1] == '\n' {
				size++
			}
			start = i + size
		}
		i += size
	}
	return append(lines, printableComment(text[start:]))
}

// printableComment returns s with each character [hasUnsafeRune] rejects,
// other than a tab, replaced by U+FFFD.
func printableComment(s string) string {
	if !hasUnsafeRune(s, true) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r != '\t' && unsafeRune(r) {
			r = utf8.RuneError
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (e *encoder) marshalValue(v reflect.Value, indent int, inline bool) error {
	if !v.IsValid() {
		e.buf = append(e.buf, "null"...)
		return nil
	}

	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		key, err := e.cycles.enter(v)
		if err != nil {
			return err
		}
		err = e.marshalValue(v.Elem(), indent, inline)
		e.cycles.leave(key)
		return err
	}

	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		return e.marshalValue(v.Elem(), indent, inline)
	}

	key, err := e.cycles.enter(v)
	if err != nil {
		return err
	}
	err = e.marshalNonPointer(v, indent, inline)
	e.cycles.leave(key)
	return err
}

// marshalNonPointer writes v, which is not a pointer or interface.
func (e *encoder) marshalNonPointer(v reflect.Value, indent int, inline bool) error {
	if v.CanInterface() && e.opts.customMarshalers != nil {
		t := v.Type()
		if fn, ok := e.opts.customMarshalers[t]; ok {
			out := reflect.ValueOf(fn).Call([]reflect.Value{v})
			if !out[1].IsNil() {
				err, _ := reflect.TypeAssert[error](out[1])
				return err
			}
			return e.writeRawYAML(out[0].Bytes(), indent, inline)
		}
	}

	if v.CanInterface() && v.Type().NumMethod() > 0 {
		iface := v.Interface()
		if m, ok := iface.(MarshalerContext); ok {
			result, err := m.MarshalYAML(e.ctx)
			if err != nil {
				return err
			}
			return e.marshalValue(reflect.ValueOf(result), indent, inline)
		}
		if m, ok := iface.(Marshaler); ok {
			result, err := m.MarshalYAML()
			if err != nil {
				return err
			}
			return e.marshalValue(reflect.ValueOf(result), indent, inline)
		}
		if m, ok := iface.(BytesMarshaler); ok {
			raw, err := m.MarshalYAML()
			if err != nil {
				return err
			}
			return e.writeRawYAML(raw, indent, inline)
		}
		if m, ok := iface.(encoding.TextMarshaler); ok {
			raw, err := m.MarshalText()
			if err != nil {
				return err
			}
			e.writeScalar(string(raw), indent)
			return nil
		}
	}

	if v.CanAddr() && v.Addr().CanInterface() {
		iface := v.Addr().Interface()
		if m, ok := iface.(Marshaler); ok {
			result, err := m.MarshalYAML()
			if err != nil {
				return err
			}
			return e.marshalValue(reflect.ValueOf(result), indent, inline)
		}
		if m, ok := iface.(BytesMarshaler); ok {
			raw, err := m.MarshalYAML()
			if err != nil {
				return err
			}
			return e.writeRawYAML(raw, indent, inline)
		}
		if m, ok := iface.(encoding.TextMarshaler); ok {
			raw, err := m.MarshalText()
			if err != nil {
				return err
			}
			e.writeScalar(string(raw), indent)
			return nil
		}
	}

	if v.Type() == reflect.TypeFor[Number]() && isNumberText(v.String()) {
		if e.opts.jsonCompat {
			s, _ := numberJSON(v.String())
			e.buf = append(e.buf, s...)
		} else {
			e.buf = append(e.buf, v.String()...)
		}
		return nil
	}
	if v.Type() == reflect.TypeFor[json.Number]() && isDecimalFloat(v.String()) {
		// A JSON number is written as the number it holds.
		e.buf = append(e.buf, v.String()...)
		return nil
	}

	switch v.Kind() {
	case reflect.String:
		e.writeScalar(v.String(), indent)
	case reflect.Bool:
		if v.Bool() {
			e.buf = append(e.buf, trueLiteral...)
		} else {
			e.buf = append(e.buf, falseLiteral...)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Type() == reflect.TypeFor[time.Duration]() {
			d, _ := reflect.TypeAssert[time.Duration](v)
			e.buf = append(e.buf, d.String()...)
		} else {
			e.buf = strconv.AppendInt(e.buf, v.Int(), 10)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		e.buf = strconv.AppendUint(e.buf, v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		switch {
		case math.IsInf(f, 1):
			e.buf = append(e.buf, ".inf"...)
		case math.IsInf(f, -1):
			e.buf = append(e.buf, "-.inf"...)
		case math.IsNaN(f):
			e.buf = append(e.buf, ".nan"...)
		case e.opts.autoInt && f == math.Trunc(f) && math.Abs(f) < 1e16:
			e.buf = strconv.AppendInt(e.buf, int64(f), 10)
		default:
			start := len(e.buf)
			e.buf = strconv.AppendFloat(e.buf, f, 'g', -1, v.Type().Bits())
			// A whole number keeps a fraction so that it reads back as a
			// float, not an integer.
			if !bytes.ContainsAny(e.buf[start:], ".e") {
				e.buf = append(e.buf, ".0"...)
			}
		}
	case reflect.Slice:
		if v.IsNil() {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		if isMapSliceType(v.Type()) {
			return e.marshalMapSlice(v, indent, inline)
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			e.buf = append(e.buf, "!!binary "...)
			e.buf = append(e.buf, base64.StdEncoding.EncodeToString(v.Bytes())...)
			return nil
		}
		return e.marshalSlice(v, indent, inline)
	case reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			e.buf = append(e.buf, "!!binary "...)
			e.buf = append(e.buf, base64.StdEncoding.EncodeToString(byteArray(v))...)
			return nil
		}
		return e.marshalSlice(v, indent, inline)
	case reflect.Map:
		if v.IsNil() {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		return e.marshalMap(v, indent, inline)
	case reflect.Struct:
		switch v.Type() {
		case reflect.TypeFor[big.Int]():
			bi, _ := reflect.TypeAssert[big.Int](v)
			e.buf = append(e.buf, bi.String()...)
			return nil
		case reflect.TypeFor[big.Float]():
			bf, _ := reflect.TypeAssert[big.Float](v)
			e.buf = append(e.buf, bf.Text('g', -1)...)
			return nil
		case reflect.TypeFor[big.Rat]():
			br, _ := reflect.TypeAssert[big.Rat](v)
			e.buf = append(e.buf, br.RatString()...)
			return nil
		}
		return e.marshalStruct(v, indent, inline)
	default:
		return fmt.Errorf("yaml: cannot encode value of type %s: %w", v.Type(), ErrUnsupported)
	}

	return nil
}

// byteArray returns the bytes of [N]byte value v.
func byteArray(v reflect.Value) []byte {
	b := make([]byte, v.Len())
	reflect.Copy(reflect.ValueOf(b), v)
	return b
}

// isMapSliceType reports whether t is [MapSlice] or another slice of
// [MapItem], which is written as a mapping.
func isMapSliceType(t reflect.Type) bool {
	return t.Kind() == reflect.Slice && t.Elem() == reflect.TypeFor[MapItem]()
}

// mapSliceItems returns the indexes of the items of MapSlice v to write,
// leaving out items with empty values when [WithOmitEmpty] is set.
func (e *encoder) mapSliceItems(v reflect.Value) []int {
	items := make([]int, 0, v.Len())
	for i := range v.Len() {
		if e.opts.omitEmpty && mapItemEmpty(v.Index(i).Field(1)) {
			continue
		}
		items = append(items, i)
	}
	return items
}

// hasNonEmptyValue reports whether map v has an entry whose value is not
// empty.
func hasNonEmptyValue(v reflect.Value) bool {
	iter := v.MapRange()
	for iter.Next() {
		if !isEmpty(iter.Value()) {
			return true
		}
	}
	return false
}

// mapItemEmpty reports whether the MapItem value val, an interface, is nil
// or holds an empty value.
func mapItemEmpty(val reflect.Value) bool {
	return val.IsNil() || isEmpty(val.Elem())
}

// marshalMapSlice writes MapSlice v as a mapping, in item order.
func (e *encoder) marshalMapSlice(v reflect.Value, indent int, inline bool) error {
	items := e.mapSliceItems(v)
	if len(items) == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}
	flow := e.opts.flow || inline
	if flow {
		e.flowDepth++
		defer func() { e.flowDepth-- }()
		e.buf = append(e.buf, '{')
	}
	for i, at := range items {
		item := v.Index(at)
		key, val := item.Field(0), item.Field(1)
		switch {
		case flow && i > 0:
			e.buf = append(e.buf, ", "...)
		case i > 0:
			e.buf = append(e.buf, '\n')
			e.writeIndent(indent)
		}
		var seg string
		if e.keys.on {
			seg = fmt.Sprint(key.Interface())
		}
		idx, parent := e.keys.enter(e.buf, seg)
		if err := e.writeKey(key, indent); err != nil {
			return err
		}
		if flow {
			e.buf = append(e.buf, ": "...)
			if err := e.marshalValue(val, indent, true); err != nil {
				return err
			}
		} else if err := e.writeBlockValue(val, indent, false); err != nil {
			return err
		}
		e.keys.leave(e.buf, idx, parent, false)
	}
	if flow {
		e.buf = append(e.buf, '}')
	}
	return nil
}

func (e *encoder) marshalSlice(v reflect.Value, indent int, inline bool) error {
	if v.Len() == 0 {
		e.buf = append(e.buf, "[]"...)
		return nil
	}

	if e.opts.flow || inline {
		return e.marshalFlowSequence(v, indent)
	}

	for i := range v.Len() {
		if i > 0 {
			e.buf = append(e.buf, '\n')
			e.writeIndent(indent)
		}
		idx, parent := e.keys.enter(e.buf, itemSeg(i))
		e.buf = append(e.buf, "- "...)

		// A collection item starts on the "- " line, so its later lines
		// align with the column after "- ", whatever the indent setting.
		if err := e.marshalValue(v.Index(i), indent+len("- "), false); err != nil {
			return err
		}
		e.keys.leave(e.buf, idx, parent, false)
	}
	return nil
}

// itemSeg returns the comment path segment of sequence item i.
func itemSeg(i int) string {
	return "[" + strconv.Itoa(i) + "]"
}

func (e *encoder) marshalFlowSequence(v reflect.Value, indent int) error {
	e.flowDepth++
	defer func() { e.flowDepth-- }()
	e.buf = append(e.buf, '[')
	for i := range v.Len() {
		if i > 0 {
			e.buf = append(e.buf, ", "...)
		}
		idx, parent := e.keys.enter(e.buf, itemSeg(i))
		if err := e.marshalValue(v.Index(i), indent, true); err != nil {
			return err
		}
		e.keys.leave(e.buf, idx, parent, false)
	}
	e.buf = append(e.buf, ']')
	return nil
}

// mapKeys returns the keys of map v to encode, sorted, leaving out entries
// with empty values when [WithOmitEmpty] is set.
//
// Numeric keys sort by value, before all other keys, which sort by their
// text; keys with the same text sort by type name.
func (e *encoder) mapKeys(v reflect.Value) []mapKey {
	keys := make([]mapKey, 0, v.Len())
	iter := v.MapRange()
	for iter.Next() {
		if e.opts.omitEmpty && isEmpty(iter.Value()) {
			continue
		}
		k := iter.Key()
		keys = append(keys, mapKey{v: k, text: mapKeyText(k)})
	}
	slices.SortStableFunc(keys, compareMapKeys)
	return keys
}

// mapKey is a map key with its text.
type mapKey struct {
	v    reflect.Value
	text string
}

func compareMapKeys(a, b mapKey) int {
	av, bv := unwrapInterface(a.v), unwrapInterface(b.v)
	an, aNum := numericKey(av)
	bn, bNum := numericKey(bv)
	switch {
	case aNum && bNum:
		if c := an.compare(bn); c != 0 {
			return c
		}
	case aNum:
		return -1
	case bNum:
		return 1
	}
	if c := strings.Compare(a.text, b.text); c != 0 {
		return c
	}
	return strings.Compare(av.Type().String(), bv.Type().String())
}

// unwrapInterface returns the value held by interface v, or v itself.
func unwrapInterface(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Interface && !v.IsNil() {
		return v.Elem()
	}
	return v
}

// keyNumber is the value of a numeric map key.
type keyNumber struct {
	kind reflect.Kind // reflect.Int, reflect.Uint or reflect.Float64
	i    int64
	u    uint64
	f    float64
}

// numericKey returns the number v holds, if it is an integer or float.
func numericKey(v reflect.Value) (keyNumber, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return keyNumber{kind: reflect.Int, i: v.Int(), f: float64(v.Int())}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return keyNumber{kind: reflect.Uint, u: v.Uint(), f: float64(v.Uint())}, true
	case reflect.Float32, reflect.Float64:
		return keyNumber{kind: reflect.Float64, f: v.Float()}, true
	}
	return keyNumber{}, false
}

// compare orders n and m by value: exactly when both are signed or both
// unsigned integers, otherwise as float64.
func (n keyNumber) compare(m keyNumber) int {
	switch {
	case n.kind == reflect.Int && m.kind == reflect.Int:
		return cmp.Compare(n.i, m.i)
	case n.kind == reflect.Uint && m.kind == reflect.Uint:
		return cmp.Compare(n.u, m.u)
	}
	return cmp.Compare(n.f, m.f)
}

// inlineMapKeys returns the keys of inline map field m to encode. A key
// that names a field of the struct is left out: the field takes precedence,
// as it does when decoding.
func (e *encoder) inlineMapKeys(m reflect.Value, sf *structFields) []mapKey {
	return slices.DeleteFunc(e.mapKeys(m), func(k mapKey) bool {
		return sf.namesField(k.text)
	})
}

// namesField reports whether name is the key of a non-inline field.
func (sf *structFields) namesField(name string) bool {
	idx, ok := sf.byName[name]
	return ok && !sf.fields[idx].inline
}

// mapKeyText returns the text of map key k.
func mapKeyText(k reflect.Value) string {
	if k.Kind() == reflect.String {
		return k.String()
	}
	return fmt.Sprint(k.Interface())
}

// omitField reports whether a struct field is left out: it sits behind a
// nil embedded pointer, or it is empty and tagged omitempty or
// [WithOmitEmpty] is set.
func omitField(opts *encoderOptions, fi *fieldInfo, field reflect.Value) bool {
	return !field.IsValid() || (fi.omitEmpty || opts.omitEmpty) && isEmpty(field)
}

// fieldForEncode returns the field of struct v at index, or the zero Value
// when an embedded struct pointer on the way is nil. It never allocates.
func fieldForEncode(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

func (e *encoder) marshalMap(v reflect.Value, indent int, inline bool) error {
	keys := e.mapKeys(v)
	if len(keys) == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}

	if e.opts.flow || inline {
		return e.marshalFlowMapping(v, keys, indent)
	}

	for i, key := range keys {
		if i > 0 {
			e.buf = append(e.buf, '\n')
			e.writeIndent(indent)
		}
		idx, parent := e.keys.enter(e.buf, key.text)
		if err := e.writeKey(key.v, indent); err != nil {
			return err
		}
		if err := e.writeBlockValue(v.MapIndex(key.v), indent, false); err != nil {
			return err
		}
		e.keys.leave(e.buf, idx, parent, false)
	}

	return nil
}

func (e *encoder) marshalFlowMapping(v reflect.Value, keys []mapKey, indent int) error {
	e.flowDepth++
	defer func() { e.flowDepth-- }()
	e.buf = append(e.buf, '{')

	for i, key := range keys {
		if i > 0 {
			e.buf = append(e.buf, ", "...)
		}
		idx, parent := e.keys.enter(e.buf, key.text)
		if err := e.writeKey(key.v, indent); err != nil {
			return err
		}
		e.buf = append(e.buf, ": "...)
		if err := e.marshalValue(v.MapIndex(key.v), indent, true); err != nil {
			return err
		}
		e.keys.leave(e.buf, idx, parent, false)
	}
	e.buf = append(e.buf, '}')
	return nil
}

// writeKey writes a mapping key, quoted when it would otherwise read back
// as a different value.
func (e *encoder) writeKey(key reflect.Value, indent int) error {
	e.encodingKey = true
	defer func() { e.encodingKey = false }()
	return e.marshalValue(key, indent, true)
}

// writeFieldName writes a struct field name as a mapping key.
func (e *encoder) writeFieldName(name string) {
	e.encodingKey = true
	e.writeScalar(name, 0)
	e.encodingKey = false
}

// writeBlockValue writes the ':' after a block mapping key and then val:
// on the following lines when it is a non-empty collection, otherwise after
// a space. flow writes val in flow style.
//
// Without [WithIndentSequence] a block sequence starts at the key's column.
func (e *encoder) writeBlockValue(val reflect.Value, indent int, flow bool) error {
	e.buf = append(e.buf, ':')
	val, err := e.resolveMarshaler(val)
	if err != nil {
		return err
	}
	if raw, ok, err := e.rawMarshal(val); ok {
		if err != nil {
			return err
		}
		n, err := parseRawYAML(raw)
		if err != nil {
			return err
		}
		if flow {
			e.buf = append(e.buf, ' ')
			e.emitFlowNode(n)
		} else {
			e.emitBlockChild(n, indent, true)
		}
		return nil
	}
	if !flow && e.isBlockCompound(val) {
		childIndent := indent + e.opts.indent
		if !e.opts.indentSequence && isSequenceValue(val) {
			childIndent = indent
		}
		e.buf = append(e.buf, '\n')
		e.writeIndent(childIndent)
		return e.marshalValue(val, childIndent, false)
	}
	e.buf = append(e.buf, ' ')
	return e.marshalValue(val, indent+e.opts.indent, flow)
}

// rawMarshal calls the marshaler that writes v as YAML bytes, a custom
// marshaler or [BytesMarshaler], when marshalValue would use one for v.
// MarshalYAML methods returning values must already be resolved.
func (e *encoder) rawMarshal(v reflect.Value) (raw []byte, ok bool, err error) {
	if !v.IsValid() || !v.CanInterface() {
		return nil, false, nil
	}
	if fn, found := e.opts.customMarshalers[v.Type()]; found {
		out := reflect.ValueOf(fn).Call([]reflect.Value{v})
		if !out[1].IsNil() {
			err, _ = reflect.TypeAssert[error](out[1])
			return nil, true, err
		}
		return out[0].Bytes(), true, nil
	}
	switch m := methodValue(v).(type) {
	case BytesMarshaler:
		raw, err = m.MarshalYAML()
		return raw, true, err
	case encoding.TextMarshaler:
		return nil, false, nil
	}
	if v.CanAddr() {
		if m, found := reflect.TypeAssert[BytesMarshaler](v.Addr()); found {
			raw, err = m.MarshalYAML()
			return raw, true, err
		}
	}
	return nil, false, nil
}

// parseRawYAML parses the YAML bytes returned by a custom marshaler or
// [BytesMarshaler] into the node they hold; blank bytes hold null. Bytes
// that are not valid YAML, or hold no value (only a comment, such as
// "#ff8000"), are taken as a string. More than one document is an error.
func parseRawYAML(raw []byte) (*node, error) {
	if strings.TrimSpace(string(raw)) == "" {
		return &node{kind: nodeScalar}, nil
	}
	text := &node{kind: nodeScalar, value: string(raw), tag: tagStr}
	docs, ok := parseDocs(raw)
	switch {
	case !ok, len(docs) == 0:
		return text, nil
	case len(docs) > 1:
		return nil, fmt.Errorf("yaml: marshaler returned %d YAML documents: %w", len(docs), ErrUnsupported)
	}
	n := docs[0]
	if n.kind == nodeDocument {
		if len(n.children) == 0 {
			return text, nil
		}
		n = n.children[0]
	}
	return n, nil
}

// parseDocs parses raw into its documents, reporting false if raw is not
// valid YAML.
func parseDocs(raw []byte) ([]*node, bool) {
	tokens, err := newScanner(raw).scan()
	if err != nil {
		return nil, false
	}
	docs, err := newParser(tokens).parse()
	return docs, err == nil
}

// writeRawYAML writes the YAML bytes returned by a custom marshaler or
// [BytesMarshaler] at the current position, re-emitting them so that
// multi-line output is indented to fit, and in flow style where flow is
// required.
func (e *encoder) writeRawYAML(raw []byte, indent int, inline bool) error {
	n, err := parseRawYAML(raw)
	if err != nil {
		return err
	}
	if e.opts.flow || inline {
		e.emitFlowNode(n)
	} else {
		e.emitNode(n, indent)
	}
	return nil
}

// methodValue returns v as an interface value for asserting the marshaler
// interfaces, or nil when v's type has no methods and so implements none,
// which spares boxing a copy of v.
func methodValue(v reflect.Value) any {
	if v.Type().NumMethod() == 0 {
		return nil
	}
	return v.Interface()
}

// maxMarshalerChain bounds how many MarshalYAML results resolveMarshaler
// follows when each returns another marshaler.
const maxMarshalerChain = 64

// resolveMarshaler calls the MarshalYAML method marshalValue would call for
// v, and returns the value it produced, so that the layout can be chosen
// from what is actually written. It returns v itself when no MarshalYAML
// method applies.
func (e *encoder) resolveMarshaler(v reflect.Value) (reflect.Value, error) {
	for range maxMarshalerChain {
		for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && !v.IsNil() {
			v = v.Elem()
		}
		if !v.IsValid() || !v.CanInterface() || v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
			return v, nil
		}
		if _, ok := e.opts.customMarshalers[v.Type()]; ok {
			return v, nil
		}
		var (
			result any
			err    error
		)
		switch m := methodValue(v).(type) {
		case MarshalerContext:
			result, err = m.MarshalYAML(e.ctx)
		case Marshaler:
			result, err = m.MarshalYAML()
		case BytesMarshaler, encoding.TextMarshaler:
			return v, nil
		default:
			if !v.CanAddr() {
				return v, nil
			}
			pm, ok := reflect.TypeAssert[Marshaler](v.Addr())
			if !ok {
				return v, nil
			}
			result, err = pm.MarshalYAML()
		}
		if err != nil {
			return v, err
		}
		v = reflect.ValueOf(result)
	}
	return v, nil
}

// isSequenceValue reports whether v is written as a sequence ([]byte is
// written as a !!binary scalar).
func isSequenceValue(v reflect.Value) bool {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && !v.IsNil() {
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Array:
		return v.Type().Elem().Kind() != reflect.Uint8
	case reflect.Slice:
		return v.Type().Elem().Kind() != reflect.Uint8 && !isMapSliceType(v.Type())
	}
	return false
}

// isBlockCompound reports whether v is written as a block collection on
// lines of its own: a map or struct with at least one entry left after
// omitted entries, or a non-empty sequence.
func (e *encoder) isBlockCompound(v reflect.Value) bool {
	if !isCompound(v) {
		return false
	}
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if byMarshaler, asText := e.marshaledBy(v); byMarshaler {
		return !asText
	}
	switch v.Kind() {
	case reflect.Slice:
		if isMapSliceType(v.Type()) {
			if !e.opts.omitEmpty {
				return true
			}
			for i := range v.Len() {
				if !mapItemEmpty(v.Index(i).Field(1)) {
					return true
				}
			}
			return false
		}
		return isSequenceValue(v)
	case reflect.Map:
		return !e.opts.omitEmpty || hasNonEmptyValue(v)
	case reflect.Struct:
		return e.hasStructEntries(v)
	}
	return true
}

// marshaledBy reports whether marshalValue writes v through a marshaler
// instead of by kind, and whether that marshaler is
// [encoding.TextMarshaler], whose output is always a scalar. It follows
// the order marshalValue checks them in.
func (e *encoder) marshaledBy(v reflect.Value) (byMarshaler, asText bool) {
	if !v.CanInterface() {
		return false, false
	}
	if _, ok := e.opts.customMarshalers[v.Type()]; ok {
		return true, false
	}
	switch v.Interface().(type) {
	case MarshalerContext, Marshaler, BytesMarshaler:
		return true, false
	case encoding.TextMarshaler:
		return true, true
	}
	if v.CanAddr() {
		switch v.Addr().Interface().(type) {
		case Marshaler, BytesMarshaler:
			return true, false
		case encoding.TextMarshaler:
			return true, true
		}
	}
	return false, false
}

// hasStructEntries reports whether struct v writes at least one mapping
// entry. A struct with conflicting field names reports true so that encoding
// it returns the conflict error.
func (e *encoder) hasStructEntries(v reflect.Value) bool {
	sf := getStructFields(v.Type())
	if len(sf.conflicts) > 0 {
		return true
	}
	for i := range sf.fields {
		fi := &sf.fields[i]
		field := fieldForEncode(v, fi.index)
		if omitField(e.opts, fi, field) {
			continue
		}
		if fi.inline && field.Kind() == reflect.Map && len(e.inlineMapKeys(field, sf)) == 0 {
			continue
		}
		return true
	}
	return false
}

func (e *encoder) marshalStruct(v reflect.Value, indent int, inline bool) error {
	sf := getStructFields(v.Type())
	if len(sf.conflicts) > 0 {
		return fmt.Errorf("yaml: struct %s has conflicting field names: %s: %w", v.Type(), strings.Join(sf.conflicts, ", "), errConflictingFields)
	}

	if !e.hasStructEntries(v) {
		e.buf = append(e.buf, "{}"...)
		return nil
	}

	if e.opts.flow || inline {
		return e.marshalFlowStruct(v, sf, indent)
	}

	first := true
	next := func() {
		if !first {
			e.buf = append(e.buf, '\n')
			e.writeIndent(indent)
		}
		first = false
	}
	for i := range sf.fields {
		fi := &sf.fields[i]
		field := fieldForEncode(v, fi.index)

		if omitField(e.opts, fi, field) {
			continue
		}

		if fi.inline && field.Kind() == reflect.Map {
			for _, key := range e.inlineMapKeys(field, sf) {
				next()
				idx, parent := e.keys.enter(e.buf, key.text)
				if err := e.writeKey(key.v, indent); err != nil {
					return err
				}
				if err := e.writeBlockValue(field.MapIndex(key.v), indent, false); err != nil {
					return err
				}
				e.keys.leave(e.buf, idx, parent, false)
			}
			continue
		}

		next()
		idx, parent := e.keys.enter(e.buf, fi.name)
		e.writeFieldName(fi.name)
		if err := e.writeBlockValue(field, indent, fi.flow); err != nil {
			return err
		}
		e.keys.leave(e.buf, idx, parent, false)
	}

	return nil
}

// marshalFlowStruct writes v as a flow mapping. The entries of an inline map
// field are merged into it, as in block style.
func (e *encoder) marshalFlowStruct(v reflect.Value, sf *structFields, indent int) error {
	e.flowDepth++
	defer func() { e.flowDepth-- }()
	e.buf = append(e.buf, '{')
	first := true
	next := func() {
		if !first {
			e.buf = append(e.buf, ", "...)
		}
		first = false
	}
	for i := range sf.fields {
		fi := &sf.fields[i]
		field := fieldForEncode(v, fi.index)
		if omitField(e.opts, fi, field) {
			continue
		}
		if fi.inline && field.Kind() == reflect.Map {
			for _, key := range e.inlineMapKeys(field, sf) {
				next()
				idx, parent := e.keys.enter(e.buf, key.text)
				if err := e.writeKey(key.v, indent); err != nil {
					return err
				}
				e.buf = append(e.buf, ": "...)
				if err := e.marshalValue(field.MapIndex(key.v), indent, true); err != nil {
					return err
				}
				e.keys.leave(e.buf, idx, parent, false)
			}
			continue
		}
		next()
		idx, parent := e.keys.enter(e.buf, fi.name)
		e.writeFieldName(fi.name)
		e.buf = append(e.buf, ": "...)
		if err := e.marshalValue(field, indent, true); err != nil {
			return err
		}
		e.keys.leave(e.buf, idx, parent, false)
	}
	e.buf = append(e.buf, '}')
	return nil
}

func (e *encoder) writeScalar(s string, indent int) {
	if e.opts.jsonCompat {
		e.writeQuotedScalar(s)
		return
	}

	if e.opts.quoteAll && !e.encodingKey {
		e.writeQuotedScalar(s)
		return
	}

	if e.opts.useLiteral && strings.Contains(s, "\n") && literalSafe(s) && !e.encodingKey && !e.opts.flow && e.flowDepth == 0 {
		e.writeLiteralScalar(s, indent)
		return
	}

	if e.opts.autoInt {
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			e.buf = append(e.buf, s...)
			return
		}
	}

	if needsQuoting(s) {
		e.writeQuotedScalar(s)
		return
	}

	e.buf = append(e.buf, s...)
}

func (e *encoder) writeLiteralScalar(s string, indent int) {
	if strings.HasSuffix(s, "\n") {
		e.buf = append(e.buf, '|')
	} else {
		e.buf = append(e.buf, '|', '-')
	}
	e.buf = append(e.buf, '\n')

	// The line break after the last line belongs to whatever follows.
	for i, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		if i > 0 {
			e.buf = append(e.buf, '\n')
		}
		e.writeIndent(indent + e.opts.indent)
		e.buf = append(e.buf, line...)
	}
}

func (e *encoder) writeQuotedScalar(s string) {
	if e.opts.useSingleQuote && !strings.Contains(s, "'") && !hasUnsafeRune(s, true) {
		e.buf = append(e.buf, '\'')
		e.buf = append(e.buf, s...)
		e.buf = append(e.buf, '\'')
		return
	}

	e.buf = append(e.buf, '"')
	e.buf = append(e.buf, escapeKYAMLLine(s)...)
	e.buf = append(e.buf, '"')
}

// hasUnsafeRune reports whether s holds a character that must be escaped
// in a double-quoted scalar to read back unchanged: a control character, a
// character the decoder reads as a line break (U+0085, U+2028, U+2029), a
// byte order mark or noncharacter, or invalid UTF-8. allowTab exempts tabs.
func hasUnsafeRune(s string, allowTab bool) bool {
	for _, r := range s {
		if r == '\t' {
			if !allowTab {
				return true
			}
			continue
		}
		if unsafeRune(r) {
			return true
		}
	}
	return false
}

// unsafeRune reports whether r, a tab apart, cannot be written as is: a
// control character, a line or paragraph separator, a byte order mark or
// noncharacter, or [utf8.RuneError] (which also stands for invalid UTF-8).
func unsafeRune(r rune) bool {
	return r < 0x20 || r >= 0x7f && r <= 0x9f ||
		r == 0x2028 || r == 0x2029 || r == 0xfeff || r == 0xfffe || r == 0xffff || r == utf8.RuneError
}

// literalSafe reports whether multi-line s reads back unchanged from a
// literal block scalar: it has no character that needs escaping besides
// newlines and tabs, its first content line does not start with a space
// (which would be taken as indentation), and it does not end in more than
// one newline.
func literalSafe(s string) bool {
	if hasUnsafeRune(strings.ReplaceAll(s, "\n", ""), true) || strings.HasSuffix(s, "\n\n") {
		return false
	}
	content := strings.TrimLeft(s, "\n")
	return content != "" && content[0] != ' '
}

func (e *encoder) writeIndent(n int) {
	for range n {
		e.buf = append(e.buf, ' ')
	}
}

func (e *encoder) encodeNode(n *node) ([]byte, error) {
	e.buf = e.buf[:0]
	e.emitNode(n, 0)
	return append([]byte(nil), e.buf...), nil
}

// isBlockCollection reports whether n is a non-empty mapping or sequence,
// which emitNode writes in block style on lines of its own.
func isBlockCollection(n *node) bool {
	return (n.kind == nodeMapping || n.kind == nodeSequence) && len(n.children) > 0
}

func (e *encoder) emitNode(n *node, indent int) {
	if props := nodeProps(n); props != "" {
		e.buf = append(e.buf, props...)
		if isBlockCollection(n) {
			e.buf = append(e.buf, '\n')
			e.writeIndent(indent)
		} else {
			e.buf = append(e.buf, ' ')
		}
	}
	e.emitNodeBody(n, indent)
}

// emitBlockChild writes n after a "?" or ":" indicator: a block collection
// starts on the next line, indented, and anything else follows a space. A
// mapping value that is a block sequence starts at the key's column unless
// [WithIndentSequence] is set.
func (e *encoder) emitBlockChild(n *node, indent int, isValue bool) {
	if !isBlockCollection(n) {
		e.buf = append(e.buf, ' ')
		e.emitNode(n, indent+e.opts.indent)
		return
	}
	if props := nodeProps(n); props != "" {
		e.buf = append(e.buf, ' ')
		e.buf = append(e.buf, props...)
	}
	childIndent := indent + e.opts.indent
	if isValue && n.kind == nodeSequence && !e.opts.indentSequence {
		childIndent = indent
	}
	e.buf = append(e.buf, '\n')
	e.writeIndent(childIndent)
	e.emitNodeBody(n, childIndent)
}

// emitFlowNode writes n in flow style.
func (e *encoder) emitFlowNode(n *node) {
	if props := nodeProps(n); props != "" {
		e.buf = append(e.buf, props...)
		e.buf = append(e.buf, ' ')
	}
	switch n.kind {
	case nodeScalar:
		e.flowDepth++
		e.writeNodeScalar(n, 0)
		e.flowDepth--
	case nodeMergeKey:
		e.buf = append(e.buf, "<<"...)
	case nodeAlias:
		e.buf = append(e.buf, '*')
		e.buf = append(e.buf, n.alias...)
	case nodeMapping:
		e.buf = append(e.buf, '{')
		for i := 0; i < len(n.children)-1; i += 2 {
			if i > 0 {
				e.buf = append(e.buf, ", "...)
			}
			if k := n.children[i]; k.kind == nodeMapping || k.kind == nodeSequence {
				e.buf = append(e.buf, "? "...)
			}
			e.emitFlowNode(n.children[i])
			if n.children[i].kind == nodeAlias {
				e.buf = append(e.buf, ' ')
			}
			e.buf = append(e.buf, ": "...)
			e.emitFlowNode(n.children[i+1])
		}
		e.buf = append(e.buf, '}')
	case nodeSequence:
		e.buf = append(e.buf, '[')
		for i, child := range n.children {
			if i > 0 {
				e.buf = append(e.buf, ", "...)
			}
			e.emitFlowNode(child)
		}
		e.buf = append(e.buf, ']')
	case nodeDocument:
		if len(n.children) > 0 {
			e.emitFlowNode(n.children[0])
		}
	}
}

// nodeProps returns the anchor and tag written before n, or "".
func nodeProps(n *node) string {
	if n.kind == nodeAlias || n.kind == nodeMergeKey || n.kind == nodeDocument {
		return ""
	}
	tag := nodeTag(n)
	switch {
	case n.anchor == "":
		return tag
	case tag == "":
		return "&" + n.anchor
	default:
		return "&" + n.anchor + " " + tag
	}
}

// Long forms of the tags implied by a node's kind.
const (
	tagStr = "tag:yaml.org,2002:str"
	tagMap = "tag:yaml.org,2002:map"
	tagSeq = "tag:yaml.org,2002:seq"
)

// nodeTag returns the tag written before n, or "" when the written form
// already implies it: a mapping's !!map, a sequence's !!seq, a string's
// !!str, or a plain scalar that resolves to its !!null, !!bool, !!int or
// !!float tag on its own.
func nodeTag(n *node) string {
	tag := n.tag
	if rest, ok := strings.CutPrefix(tag, "!!"); ok {
		tag = "tag:yaml.org,2002:" + rest
	}
	switch tag {
	case "", "!", tagStr:
		return ""
	case tagMap:
		if n.kind == nodeMapping {
			return ""
		}
	case tagSeq:
		if n.kind == nodeSequence {
			return ""
		}
	case tagNull, tagBool, tagInt, tagFloat:
		if n.kind == nodeScalar && n.style == scalarPlain && coreTag(n.value) == tag {
			return ""
		}
	}
	return formatTag(tag)
}

// coreTag returns the tag a plain scalar resolves to under the Core schema.
func coreTag(s string) string {
	switch resolveCore(s).kind {
	case coreNull:
		return tagNull
	case coreBool:
		return tagBool
	case coreInt, coreUint:
		return tagInt
	case coreFloat:
		return tagFloat
	case coreNumber:
		if coreIntSyntax(s) {
			return tagInt
		}
		return tagFloat
	}
	return tagStr
}

// formatTag returns the shortest written form of a resolved tag: !!name for
// the YAML tag namespace, the local tag itself, or the verbatim !<tag>.
//
// A YAML namespace or local tag whose name holds characters a shorthand
// cannot is written as !! or ! with those characters percent-encoded, which
// reads back as the same tag. Any other tag is written verbatim with its
// characters outside the URI set percent-encoded, so that it is valid YAML.
func formatTag(tag string) string {
	if rest, ok := strings.CutPrefix(tag, "tag:yaml.org,2002:"); ok && rest != "" {
		if isTagShorthand(rest) {
			return "!!" + rest
		}
		return "!!" + escapeTagURI(rest, true)
	}
	if rest, ok := strings.CutPrefix(tag, "!"); ok && rest != "" {
		if isTagShorthand(rest) {
			return tag
		}
		return "!" + escapeTagURI(rest, true)
	}
	return "!<" + escapeTagURI(tag, false) + ">"
}

// escapeTagURI percent-encodes the bytes of s that are not URI characters
// (ns-uri-char). For a shorthand, which the parser percent-decodes, it also
// encodes '%' itself, '!' and the flow indicators; a verbatim tag is read
// as written, so its '%' is kept.
func escapeTagURI(s string, shorthand bool) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		if shorthand && isTagChar(c) || !shorthand && isURIChar(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0xf])
	}
	return b.String()
}

// isTagShorthand reports whether s can follow a tag handle as written:
// non-empty, and made of ns-tag-char bytes only.
func isTagShorthand(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if !isTagChar(s[i]) {
			return false
		}
	}
	return true
}

func (e *encoder) emitNodeBody(n *node, indent int) {
	switch n.kind {
	case nodeScalar:
		e.writeNodeScalar(n, indent)
	case nodeMergeKey:
		e.buf = append(e.buf, "<<"...)
	case nodeAlias:
		e.buf = append(e.buf, '*')
		e.buf = append(e.buf, n.alias...)
	case nodeMapping:
		if len(n.children) == 0 {
			e.buf = append(e.buf, "{}"...)
			return
		}
		for i := 0; i < len(n.children)-1; i += 2 {
			if i > 0 {
				e.buf = append(e.buf, '\n')
				e.writeIndent(indent)
			}
			key := n.children[i]
			if key.kind == nodeMapping || key.kind == nodeSequence {
				// A collection key needs the explicit "? key" form.
				e.buf = append(e.buf, '?')
				e.emitBlockChild(key, indent, false)
				e.buf = append(e.buf, '\n')
				e.writeIndent(indent)
			} else {
				e.emitNode(key, indent)
				if key.kind == nodeAlias {
					// An alias name may end in ':', so the indicator is set apart.
					e.buf = append(e.buf, ' ')
				}
			}
			e.buf = append(e.buf, ':')
			e.emitBlockChild(n.children[i+1], indent, true)
		}
	case nodeSequence:
		if len(n.children) == 0 {
			e.buf = append(e.buf, "[]"...)
			return
		}
		for i, child := range n.children {
			if i > 0 {
				e.buf = append(e.buf, '\n')
				e.writeIndent(indent)
			}
			e.buf = append(e.buf, "- "...)
			e.emitNode(child, indent+len("- "))
		}
	case nodeDocument:
		for _, child := range n.children {
			e.emitNode(child, indent)
		}
	}
}

// writeNodeScalar writes a scalar node so that it reads back with the same
// value and type. A quoted scalar stays quoted, a scalar tagged as a string
// is quoted when it would otherwise read as another type, and a plain
// scalar that reads as null, a bool or a number, or that carries a written
// tag, is written unquoted when it can be.
func (e *encoder) writeNodeScalar(n *node, indent int) {
	tagged := nodeTag(n) != ""
	switch {
	case n.style == scalarSingleQuoted || n.style == scalarDoubleQuoted:
		e.writeQuotedScalar(n.value)
	case n.style != scalarPlain || isStrTag(n.tag):
		e.writeScalar(n.value, indent)
	case n.value == "" && tagged:
		// The bare tag reads back as the same tagged empty scalar.
	case n.value == "":
		e.buf = append(e.buf, nullLiteral...)
	case readsAsNonString(n.value), n.value == "<<":
		e.buf = append(e.buf, n.value...)
	default:
		e.writeScalar(n.value, indent)
	}
}

// isStrTag reports whether tag marks a scalar as a string: the !!str tag in
// short or long form, or the non-specific tag "!".
func isStrTag(tag string) bool {
	return tag == tagStr || tag == "!!str" || tag == "!"
}

// readsAsNonString reports whether the plain scalar s resolves to null, a
// bool or a number under the Core schema.
func readsAsNonString(s string) bool {
	return resolveCore(s).kind != coreStr
}

// yaml11Words lists the plain scalars a YAML 1.1 reader resolves to a bool,
// along with "=", its value key.
var yaml11Words = map[string]struct{}{
	"y": {}, "Y": {}, "yes": {}, "Yes": {}, "YES": {},
	"n": {}, "N": {}, "no": {}, "No": {}, "NO": {},
	"on": {}, "On": {}, "ON": {},
	"off": {}, "Off": {}, "OFF": {},
	"=": {},
}

// readsAsYAML11NonString reports whether a YAML 1.1 reader, or one that
// parses numbers with Go literal syntax, would resolve the plain scalar s to
// something other than a string: a 1.1 bool word, a 1.1 integer or float
// (digits with underscores anywhere), a number with a 0b, 0o or 0x prefix in
// either case, underscores, a leading zero or an inf or nan spelling, a
// number out of range, or a timestamp.
func readsAsYAML11NonString(s string) bool {
	if _, ok := yaml11Words[s]; ok {
		return true
	}
	c := s[0]
	if (c == '+' || c == '-') && len(s) > 1 {
		c = s[1]
	}
	switch {
	case c >= '0' && c <= '9', c == '.':
	case c == 'i' || c == 'I' || c == 'n' || c == 'N':
		return isGoSpecialFloat(s)
	default:
		return false
	}
	if isYAML11Number(s) {
		return true
	}
	// strconv allocates an error for text it rejects, so it is called only
	// for text shaped like a Go number literal, or, for ParseInt, text whose
	// leading digits are enough to overflow, which it reports as out of
	// range before reaching what follows.
	shape := goNumberShape(s)
	if shape || longDigitPrefix(s) {
		if _, err := strconv.ParseInt(s, 0, 64); err == nil || errors.Is(err, strconv.ErrRange) {
			return true
		}
	}
	if shape {
		if _, err := strconv.ParseFloat(s, 64); err == nil || errors.Is(err, strconv.ErrRange) {
			return true
		}
	}
	return isYAML11Date(s)
}

// longDigitPrefix reports whether s, after an optional sign, starts with at
// least 17 characters that can make up a Go integer literal: fewer cannot
// overflow 64 bits in any base.
func longDigitPrefix(s string) bool {
	if s[0] == '+' || s[0] == '-' {
		s = s[1:]
	}
	if len(s) < 17 {
		return false
	}
	for i := range 17 {
		c := s[i]
		if !isHexDigit(c) && c != '_' && !strings.ContainsRune("xXoObB", rune(c)) {
			return false
		}
	}
	return true
}

// isGoSpecialFloat reports whether [strconv.ParseFloat] reads s as an
// infinity or NaN: an optionally signed "inf" or "infinity", or "nan", in
// any case.
func isGoSpecialFloat(s string) bool {
	t := s
	signed := t[0] == '+' || t[0] == '-'
	if signed {
		t = t[1:]
	}
	return asciiEqualFold(t, "inf") || asciiEqualFold(t, "infinity") || !signed && asciiEqualFold(t, "nan")
}

// asciiEqualFold reports whether s equals the lowercase ASCII word lower,
// ignoring ASCII case.
func asciiEqualFold(s, lower string) bool {
	if len(s) != len(lower) {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lower[i] {
			return false
		}
	}
	return true
}

// goNumberShape reports whether s could be a Go integer or float literal:
// it holds only digits, letters that can appear in one, underscores and at
// most one dot, with a sign only at the start or after an exponent letter.
// Every literal [strconv.ParseInt] or [strconv.ParseFloat] accepts has this
// shape, apart from the inf and nan spellings.
func goNumberShape(s string) bool {
	dots := 0
	for i := range len(s) {
		switch c := s[i]; {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F', c == '_':
		case c == 'x' || c == 'X' || c == 'o' || c == 'O' || c == 'p' || c == 'P':
		case c == '.':
			dots++
			if dots > 1 {
				return false
			}
		case c == '+' || c == '-':
			if i > 0 && !strings.ContainsRune("eEpP", rune(s[i-1])) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// isYAML11Number reports whether s is a YAML 1.1 integer or float, whose
// underscores may fall anywhere after the first digit:
// [-+]?[0-9][0-9_]*, [-+]?0x[0-9a-fA-F_]+, [-+]?0b[01_]+, or
// [-+]?[0-9][0-9_]*\.[0-9_]*([eE][-+][0-9]+)? and [-+]?\.[0-9][0-9_]*.
func isYAML11Number(s string) bool {
	if s[0] == '+' || s[0] == '-' {
		s = s[1:]
	}
	run := func(s string, ok func(byte) bool) int {
		i := 0
		for i < len(s) && ok(s[i]) {
			i++
		}
		return i
	}
	decimal := func(c byte) bool { return c >= '0' && c <= '9' || c == '_' }
	switch {
	case len(s) > 2 && s[0] == '0' && s[1] == 'x':
		return run(s[2:], func(c byte) bool { return isHexDigit(c) || c == '_' }) == len(s)-2
	case len(s) > 2 && s[0] == '0' && s[1] == 'b':
		return run(s[2:], func(c byte) bool { return c == '0' || c == '1' || c == '_' }) == len(s)-2
	}
	var whole int
	if s != "" && s[0] >= '0' && s[0] <= '9' {
		whole = run(s, decimal)
	}
	if whole == len(s) {
		return whole > 0
	}
	if s[whole] != '.' {
		return false
	}
	rest := s[whole+1:]
	frac := run(rest, decimal)
	if whole == 0 && (frac == 0 || rest[0] == '_') {
		return false
	}
	rest = rest[frac:]
	if rest == "" {
		return true
	}
	if len(rest) < 3 || rest[0] != 'e' && rest[0] != 'E' || rest[1] != '+' && rest[1] != '-' {
		return false
	}
	return run(rest[2:], isDecDigit) == len(rest)-2
}

// isYAML11Date reports whether s starts with a YAML 1.1 timestamp's date,
// yyyy-m-d with one or two digit month and day, followed by nothing or by a
// time separator.
func isYAML11Date(s string) bool {
	digits := func(i, minN, maxN int) int {
		n := 0
		for i+n < len(s) && n < maxN && s[i+n] >= '0' && s[i+n] <= '9' {
			n++
		}
		if n < minN {
			return -1
		}
		return i + n
	}
	i := digits(0, 4, 4)
	for range 2 {
		if i < 0 || i >= len(s) || s[i] != '-' {
			return false
		}
		i = digits(i+1, 1, 2)
	}
	if i < 0 {
		return false
	}
	return i == len(s) || s[i] == 'T' || s[i] == 't' || s[i] == ' ' || s[i] == '\t'
}

func needsQuoting(s string) bool {
	// "<<" as a plain key is a merge key.
	if s == "" || s == "<<" || readsAsNonString(s) {
		return true
	}

	// Quoting what a YAML 1.1 reader would type keeps the text a string for
	// those readers too.
	if readsAsYAML11NonString(s) {
		return true
	}

	if strings.ContainsAny(s, ":\n\r\t\"'\\#{}[]|>&*!%@`,?") {
		return true
	}

	if hasUnsafeRune(s, false) {
		return true
	}

	if s[0] == '-' || s[0] == '.' || s[0] == ' ' {
		return true
	}

	if s[len(s)-1] == ' ' {
		return true
	}

	return false
}

func isCompound(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}
	k := v.Kind()
	if k == reflect.Pointer || k == reflect.Interface {
		if v.IsNil() {
			return false
		}
		return isCompound(v.Elem())
	}
	switch k {
	case reflect.Map:
		return !v.IsNil() && v.Len() > 0
	case reflect.Slice:
		return !v.IsNil() && v.Len() > 0
	case reflect.Struct:
		switch v.Type() {
		case reflect.TypeFor[time.Time](),
			reflect.TypeFor[big.Int](),
			reflect.TypeFor[big.Float](),
			reflect.TypeFor[big.Rat]():
			return false
		}
		return true
	case reflect.Array:
		return v.Len() > 0 && v.Type().Elem().Kind() != reflect.Uint8
	}
	return false
}

func isEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Slice, reflect.Map:
		return v.IsNil() || v.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	case reflect.Struct:
		switch v.Type() {
		case reflect.TypeFor[time.Time]():
			t, _ := reflect.TypeAssert[time.Time](v)
			return t.IsZero()
		case reflect.TypeFor[big.Int]():
			bi, _ := reflect.TypeAssert[big.Int](v)
			return bi.Sign() == 0
		case reflect.TypeFor[big.Float]():
			bf, _ := reflect.TypeAssert[big.Float](v)
			return bf.Sign() == 0
		case reflect.TypeFor[big.Rat]():
			br, _ := reflect.TypeAssert[big.Rat](v)
			return br.Sign() == 0
		}
		return false
	}
	return false
}
