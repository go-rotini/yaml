package yaml

import (
	"context"
	"encoding"
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

type encoder struct {
	opts       *encoderOptions
	ctx        context.Context
	buf        []byte
	encodingKey bool
}

func newEncoder(opts *encoderOptions) *encoder {
	return &encoder{opts: opts, ctx: context.Background()}
}

func (e *encoder) encode(v reflect.Value) ([]byte, error) {
	e.buf = e.buf[:0]
	if err := e.marshalValue(v, 0, false); err != nil {
		return nil, err
	}
	if len(e.buf) > 0 && e.buf[len(e.buf)-1] != '\n' {
		e.buf = append(e.buf, '\n')
	}
	if e.opts.comments != nil {
		e.buf = applyComments(e.buf, e.opts.comments)
	}
	return e.buf, nil
}

func applyComments(buf []byte, comments map[string][]Comment) []byte {
	for path, cs := range comments {
		for _, c := range cs {
			switch c.Position {
			case HeadCommentPos:
				buf = insertHeadComment(buf, path, c.Text)
			case LineCommentPos:
				buf = insertLineComment(buf, path, c.Text)
			case FootCommentPos:
				buf = insertFootComment(buf, path, c.Text)
			}
		}
	}
	return buf
}

func insertHeadComment(buf []byte, path, text string) []byte {
	key := pathToKey(path)
	if key == "" {
		return buf
	}
	lines := strings.Split(string(buf), "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, key+":") {
			indent := strings.Repeat(" ", len(line)-len(trimmed))
			comment := indent + "# " + text
			after := make([]string, 0, len(lines)+1)
			after = append(after, lines[:i]...)
			after = append(after, comment)
			after = append(after, lines[i:]...)
			return []byte(strings.Join(after, "\n"))
		}
	}
	return buf
}

func insertLineComment(buf []byte, path, text string) []byte {
	key := pathToKey(path)
	if key == "" {
		return buf
	}
	lines := strings.Split(string(buf), "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, key+":") {
			lines[i] = line + " # " + text
			return []byte(strings.Join(lines, "\n"))
		}
	}
	return buf
}

func insertFootComment(buf []byte, path, text string) []byte {
	key := pathToKey(path)
	if key == "" {
		return buf
	}
	lines := strings.Split(string(buf), "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, key+":") {
			indent := strings.Repeat(" ", len(line)-len(trimmed))
			comment := indent + "# " + text
			after := make([]string, 0, len(lines)+1)
			after = append(after, lines[:i+1]...)
			after = append(after, comment)
			after = append(after, lines[i+1:]...)
			return []byte(strings.Join(after, "\n"))
		}
	}
	return buf
}

func pathToKey(path string) string {
	path = strings.TrimPrefix(path, "$.")
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
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
				err, _ := out[1].Interface().(error)
				return err
			}
			e.buf = append(e.buf, out[0].Bytes()...)
			return nil
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
			e.buf = append(e.buf, raw...)
			return nil
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
			e.buf = append(e.buf, raw...)
			return nil
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
			e.buf = append(e.buf, "true"...)
		} else {
			e.buf = append(e.buf, "false"...)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Type() == reflect.TypeFor[time.Duration]() {
			d, _ := v.Interface().(time.Duration)
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
			bi, _ := v.Interface().(big.Int)
			e.buf = append(e.buf, bi.String()...)
			return nil
		case reflect.TypeFor[big.Float]():
			bf, _ := v.Interface().(big.Float)
			e.buf = append(e.buf, bf.Text('g', -1)...)
			return nil
		case reflect.TypeFor[big.Rat]():
			br, _ := v.Interface().(big.Rat)
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

	seqIndent := indent
	if e.opts.indentSequence {
		seqIndent = indent + e.opts.indent
	}

	for i := range v.Len() {
		if i > 0 {
			e.buf = append(e.buf, '\n')
			e.writeIndent(seqIndent)
		}
		e.buf = append(e.buf, "- "...)

		elem := v.Index(i)
		if err := e.marshalValue(elem, seqIndent+e.opts.indent, false); err != nil {
			return err
		}
	}
	return nil
}

func (e *encoder) marshalFlowSequence(v reflect.Value, indent int) error {
	e.buf = append(e.buf, '[')
	for i := range v.Len() {
		if i > 0 {
			e.buf = append(e.buf, ", "...)
		}
		if err := e.marshalValue(v.Index(i), indent, true); err != nil {
			return err
		}
	}
	e.buf = append(e.buf, ']')
	return nil
}

func (e *encoder) marshalMap(v reflect.Value, indent int, inline bool) error {
	if v.Len() == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}

	if e.opts.flow || inline {
		return e.marshalFlowMapping(v, indent)
	}

	keys := v.MapKeys()
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
	})

	for i, key := range keys {
		val := v.MapIndex(key)

		if i > 0 {
			e.buf = append(e.buf, '\n')
			e.writeIndent(indent)
		}

		e.encodingKey = true
		if err := e.marshalValue(key, indent, true); err != nil {
			return err
		}
		e.encodingKey = false
		e.buf = append(e.buf, ':')

		if isCompound(val) {
			e.buf = append(e.buf, '\n')
			e.writeIndent(indent + e.opts.indent)
			if err := e.marshalValue(val, indent+e.opts.indent, false); err != nil {
				return err
			}
		} else {
			e.buf = append(e.buf, ' ')
			if err := e.marshalValue(val, indent+e.opts.indent, false); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *encoder) marshalFlowMapping(v reflect.Value, indent int) error {
	e.buf = append(e.buf, '{')

	keys := v.MapKeys()
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
	})

	for i, key := range keys {
		if i > 0 {
			e.buf = append(e.buf, ", "...)
		}
		e.encodingKey = true
		if err := e.marshalValue(key, indent, true); err != nil {
			return err
		}
		e.encodingKey = false
		e.buf = append(e.buf, ": "...)
		if err := e.marshalValue(v.MapIndex(key), indent, true); err != nil {
			return err
		}
	}
	e.buf = append(e.buf, '}')
	return nil
}

func (e *encoder) marshalStruct(v reflect.Value, indent int, inline bool) error {
	sf := getStructFields(v.Type())
	if len(sf.conflicts) > 0 {
		return fmt.Errorf("yaml: struct %s has conflicting field names: %s: %w", v.Type(), strings.Join(sf.conflicts, ", "), errConflictingFields)
	}

	if e.opts.flow || inline {
		return e.marshalFlowStruct(v, sf, indent)
	}

	first := true
	for _, fi := range sf.fields {
		field := fieldByIndex(v, fi.index)

		if fi.omitEmpty && isEmpty(field) {
			continue
		}

		if fi.inline && field.Kind() == reflect.Map {
			keys := field.MapKeys()
			sort.Slice(keys, func(i, j int) bool {
				return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
			})
			for _, key := range keys {
				val := field.MapIndex(key)
				if !first {
					e.buf = append(e.buf, '\n')
					e.writeIndent(indent)
				}
				first = false
				e.buf = append(e.buf, fmt.Sprint(key.Interface())...)
				e.buf = append(e.buf, ':')
				if isCompound(val) {
					e.buf = append(e.buf, '\n')
					e.writeIndent(indent + e.opts.indent)
					if err := e.marshalValue(val, indent+e.opts.indent, false); err != nil {
						return err
					}
				} else {
					e.buf = append(e.buf, ' ')
					if err := e.marshalValue(val, indent+e.opts.indent, false); err != nil {
						return err
					}
				}
			}
			continue
		}

		if !first {
			e.buf = append(e.buf, '\n')
			e.writeIndent(indent)
		}
		first = false

		e.buf = append(e.buf, fi.name...)
		e.buf = append(e.buf, ':')

		if fi.flow {
			e.buf = append(e.buf, ' ')
			if err := e.marshalValue(field, indent+e.opts.indent, true); err != nil {
				return err
			}
			continue
		}

		if isCompound(field) {
			e.buf = append(e.buf, '\n')
			e.writeIndent(indent + e.opts.indent)
			if err := e.marshalValue(field, indent+e.opts.indent, false); err != nil {
				return err
			}
		} else {
			e.buf = append(e.buf, ' ')
			if err := e.marshalValue(field, indent+e.opts.indent, false); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *encoder) marshalFlowStruct(v reflect.Value, sf *structFields, indent int) error {
	e.buf = append(e.buf, '{')
	first := true
	for _, fi := range sf.fields {
		field := fieldByIndex(v, fi.index)
		if fi.omitEmpty && isEmpty(field) {
			continue
		}
		if !first {
			e.buf = append(e.buf, ", "...)
		}
		first = false
		e.buf = append(e.buf, fi.name...)
		e.buf = append(e.buf, ": "...)
		if err := e.marshalValue(field, indent, true); err != nil {
			return err
		}
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

	if e.opts.useLiteral && strings.Contains(s, "\n") {
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

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			break
		}
		e.writeIndent(indent + e.opts.indent)
		e.buf = append(e.buf, line...)
		if i < len(lines)-1 {
			e.buf = append(e.buf, '\n')
		}
	}
}

func (e *encoder) writeQuotedScalar(s string) {
	if e.opts.useSingleQuote && !strings.ContainsAny(s, "'\n") {
		e.buf = append(e.buf, '\'')
		e.buf = append(e.buf, s...)
		e.buf = append(e.buf, '\'')
		return
	}

	e.buf = append(e.buf, '"')
	for _, r := range s {
		switch r {
		case '"':
			e.buf = append(e.buf, '\\', '"')
		case '\\':
			e.buf = append(e.buf, '\\', '\\')
		case '\n':
			e.buf = append(e.buf, '\\', 'n')
		case '\r':
			e.buf = append(e.buf, '\\', 'r')
		case '\t':
			e.buf = append(e.buf, '\\', 't')
		case '\a':
			e.buf = append(e.buf, '\\', 'a')
		case '\b':
			e.buf = append(e.buf, '\\', 'b')
		case '\v':
			e.buf = append(e.buf, '\\', 'v')
		case '\f':
			e.buf = append(e.buf, '\\', 'f')
		case 0x1b:
			e.buf = append(e.buf, '\\', 'e')
		default:
			e.buf = append(e.buf, string(r)...)
		}
	}
	e.buf = append(e.buf, '"')
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

func (e *encoder) emitNode(n *node, indent int) {
	switch n.kind {
	case nodeScalar:
		e.writeScalar(n.value, indent)
	case nodeMapping:
		for i := 0; i < len(n.children)-1; i += 2 {
			if i > 0 {
				e.buf = append(e.buf, '\n')
				e.writeIndent(indent)
			}
			e.emitNode(n.children[i], indent)
			e.buf = append(e.buf, ": "...)
			child := n.children[i+1]
			if child.kind == nodeMapping || child.kind == nodeSequence {
				e.buf = append(e.buf, '\n')
				e.writeIndent(indent + e.opts.indent)
				e.emitNode(child, indent+e.opts.indent)
			} else {
				e.emitNode(child, indent+e.opts.indent)
			}
		}
	case nodeSequence:
		for i, child := range n.children {
			if i > 0 {
				e.buf = append(e.buf, '\n')
				e.writeIndent(indent)
			}
			e.buf = append(e.buf, "- "...)
			e.emitNode(child, indent+e.opts.indent)
		}
	case nodeDocument:
		for _, child := range n.children {
			e.emitNode(child, indent)
		}
	}
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}

	if isNullValue(s) || s == "true" || s == "false" || s == "True" || s == "False" || s == "TRUE" || s == "FALSE" {
		return true
	}

	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}

	if strings.ContainsAny(s, ":\n\r\t\"'\\#{}[]|>&*!%@`,?") {
		return true
	}

	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
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
			t, _ := v.Interface().(time.Time)
			return t.IsZero()
		case reflect.TypeFor[big.Int]():
			bi, _ := v.Interface().(big.Int)
			return bi.Sign() == 0
		case reflect.TypeFor[big.Float]():
			bf, _ := v.Interface().(big.Float)
			return bf.Sign() == 0
		case reflect.TypeFor[big.Rat]():
			br, _ := v.Interface().(big.Rat)
			return br.Sign() == 0
		}
		return false
	}
	return false
}

