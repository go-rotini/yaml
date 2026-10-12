package yaml

import (
	"bytes"
	"context"
	"encoding"
	"encoding/base64"
	"fmt"
	"maps"
	"math"
	"math/big"
	"reflect"
	"slices"
	"sort"
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
	e.buf = e.buf[:0]
	e.keys = keyTracker{on: e.opts.comments != nil}
	if err := e.marshalValue(v, 0, false); err != nil {
		return nil, err
	}
	if len(e.buf) > 0 && e.buf[len(e.buf)-1] != '\n' {
		e.buf = append(e.buf, '\n')
	}
	if e.opts.comments != nil {
		e.buf = applyComments(e.buf, e.opts.comments, e.keys.positions)
	}
	return e.buf, nil
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
			switch c.Position {
			case HeadCommentPos:
				at(kp.line).head = append(at(kp.line).head, indentOf(kp.line)+"# "+c.Text)
			case LineCommentPos:
				l := kp.line
				if kp.lineCommentAtEnd {
					l = kp.end
				}
				at(l).tail = append(at(l).tail, " # "+c.Text)
			case FootCommentPos:
				at(kp.end).foot = append(at(kp.end).foot, indentOf(kp.line)+"# "+c.Text)
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
		return e.marshalValue(v.Elem(), indent, inline)
	}

	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		return e.marshalValue(v.Elem(), indent, inline)
	}

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

	if v.CanInterface() {
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
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
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
		default:
			e.buf = strconv.AppendFloat(e.buf, f, 'g', -1, 64)
		}
	case reflect.Slice:
		if v.IsNil() {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			e.buf = append(e.buf, "!!binary "...)
			e.buf = append(e.buf, base64.StdEncoding.EncodeToString(v.Bytes())...)
			return nil
		}
		return e.marshalSlice(v, indent, inline)
	case reflect.Array:
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
		e.buf = append(e.buf, fmt.Sprintf("%v", v.Interface())...)
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
func (e *encoder) mapKeys(v reflect.Value) []reflect.Value {
	keys := v.MapKeys()
	if e.opts.omitEmpty {
		keys = slices.DeleteFunc(keys, func(k reflect.Value) bool { return isEmpty(v.MapIndex(k)) })
	}
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
	})
	return keys
}

// inlineMapKeys returns the keys of inline map field m to encode. A key
// that names a field of the struct is left out: the field takes precedence,
// as it does when decoding.
func (e *encoder) inlineMapKeys(m reflect.Value, sf *structFields) []reflect.Value {
	return slices.DeleteFunc(e.mapKeys(m), func(k reflect.Value) bool {
		return sf.namesField(mapKeyText(k))
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
		idx, parent := e.keys.enter(e.buf, mapKeyText(key))
		if err := e.writeKey(key, indent); err != nil {
			return err
		}
		if err := e.writeBlockValue(v.MapIndex(key), indent, false); err != nil {
			return err
		}
		e.keys.leave(e.buf, idx, parent, false)
	}

	return nil
}

func (e *encoder) marshalFlowMapping(v reflect.Value, keys []reflect.Value, indent int) error {
	e.flowDepth++
	defer func() { e.flowDepth-- }()
	e.buf = append(e.buf, '{')

	for i, key := range keys {
		if i > 0 {
			e.buf = append(e.buf, ", "...)
		}
		idx, parent := e.keys.enter(e.buf, mapKeyText(key))
		if err := e.writeKey(key, indent); err != nil {
			return err
		}
		e.buf = append(e.buf, ": "...)
		if err := e.marshalValue(v.MapIndex(key), indent, true); err != nil {
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
	switch m := v.Interface().(type) {
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
		switch m := v.Interface().(type) {
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
		return true
	case reflect.Slice:
		return v.Type().Elem().Kind() != reflect.Uint8
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
		return isSequenceValue(v)
	case reflect.Map:
		return !e.opts.omitEmpty || len(e.mapKeys(v)) > 0
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
				idx, parent := e.keys.enter(e.buf, mapKeyText(key))
				if err := e.writeKey(key, indent); err != nil {
					return err
				}
				if err := e.writeBlockValue(field.MapIndex(key), indent, false); err != nil {
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
				idx, parent := e.keys.enter(e.buf, mapKeyText(key))
				if err := e.writeKey(key, indent); err != nil {
					return err
				}
				e.buf = append(e.buf, ": "...)
				if err := e.marshalValue(field.MapIndex(key), indent, true); err != nil {
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
		switch {
		case r == '\t':
			if !allowTab {
				return true
			}
		case r < 0x20, r >= 0x7f && r <= 0x9f,
			r == 0x2028, r == 0x2029, r == 0xfeff, r == 0xfffe, r == 0xffff, r == utf8.RuneError:
			return true
		}
	}
	return false
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
	switch scalarToAnyCore(s).(type) {
	case nil:
		return tagNull
	case bool:
		return tagBool
	case int64:
		return tagInt
	case float64:
		return tagFloat
	}
	return tagStr
}

// formatTag returns the shortest written form of a resolved tag: !!name for
// the YAML tag namespace, the local tag itself, or the verbatim !<tag>.
func formatTag(tag string) string {
	if rest, ok := strings.CutPrefix(tag, "tag:yaml.org,2002:"); ok && isTagShorthand(rest) {
		return "!!" + rest
	}
	if rest, ok := strings.CutPrefix(tag, "!"); ok && isTagShorthand(rest) {
		return tag
	}
	return "!<" + tag + ">"
}

// isTagShorthand reports whether s can follow a tag handle as written:
// non-empty, with no '!', '%', flow indicator, space or control character.
func isTagShorthand(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("!%,[]{}", r) {
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
		e.writeQuotedScalar("")
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
	_, isStr := scalarToAnyCore(s).(string)
	return !isStr
}

func needsQuoting(s string) bool {
	// "<<" as a plain key is a merge key.
	if s == "" || s == "<<" || readsAsNonString(s) {
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
		return v.Len() > 0
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
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
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
