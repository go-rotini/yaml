package yaml

import (
	"context"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type decoder struct {
	opts       *decoderOptions
	ctx        context.Context
	anchors    map[string]*node
	aliasDepth int
	depth      int
	typeErrors []string
}

func newDecoder(opts *decoderOptions) *decoder {
	return &decoder{
		opts:    opts,
		ctx:     context.Background(),
		anchors: make(map[string]*node),
	}
}

func (d *decoder) decode(n *node, v reflect.Value) error {
	if n == nil {
		return nil
	}

	d.depth++
	defer func() { d.depth-- }()
	if d.opts.maxDepth > 0 && d.depth > d.opts.maxDepth {
		return &SyntaxError{Message: fmt.Sprintf("exceeded max depth %d", d.opts.maxDepth), Pos: n.pos}
	}

	if n.anchor != "" {
		d.anchors[n.anchor] = n
	}

	if n.kind == nodeAlias {
		return d.decodeAlias(n, v)
	}

	if n.kind == nodeDocument {
		if len(n.children) == 0 {
			return nil
		}
		return d.decode(n.children[0], v)
	}

	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return d.decode(n, v.Elem())
	}

	if v.Kind() == reflect.Interface && v.NumMethod() == 0 {
		result, err := d.decodeToAny(n)
		if err != nil {
			return err
		}
		if result != nil {
			v.Set(reflect.ValueOf(result))
		}
		return nil
	}

	if d.opts.customUnmarshalers != nil {
		t := v.Type()
		if fn, ok := d.opts.customUnmarshalers[t]; ok {
			raw, err := marshalNode(n)
			if err != nil {
				return err
			}
			ptr := reflect.New(t)
			ptr.Elem().Set(v)
			out := reflect.ValueOf(fn).Call([]reflect.Value{ptr, reflect.ValueOf(raw)})
			if !out[0].IsNil() {
				return out[0].Interface().(error)
			}
			v.Set(ptr.Elem())
			return nil
		}
	}

	if v.CanAddr() {
		if u, ok := v.Addr().Interface().(UnmarshalerContext); ok {
			return u.UnmarshalYAML(d.ctx, func(target any) error {
				rv := reflect.ValueOf(target)
				if rv.Kind() == reflect.Pointer {
					return d.decode(n, rv.Elem())
				}
				return d.decode(n, rv)
			})
		}
		if u, ok := v.Addr().Interface().(Unmarshaler); ok {
			return u.UnmarshalYAML(func(target any) error {
				rv := reflect.ValueOf(target)
				if rv.Kind() == reflect.Pointer {
					return d.decode(n, rv.Elem())
				}
				return d.decode(n, rv)
			})
		}
		if u, ok := v.Addr().Interface().(BytesUnmarshaler); ok {
			raw, err := marshalNode(n)
			if err != nil {
				return err
			}
			return u.UnmarshalYAML(raw)
		}
	}

	if v.CanAddr() && v.Type() != reflect.TypeFor[time.Time]() {
		if u, ok := v.Addr().Interface().(encoding.TextUnmarshaler); ok {
			if n.kind == nodeScalar {
				return u.UnmarshalText([]byte(n.value))
			}
		}
	}

	if d.opts.useJSONUnmarshaler && v.CanAddr() {
		if u, ok := v.Addr().Interface().(jsonUnmarshaler); ok {
			raw, err := marshalNode(n)
			if err != nil {
				return err
			}
			jsonBytes, err := jsonEncodeYAML(raw)
			if err != nil {
				return err
			}
			return u.UnmarshalJSON(jsonBytes)
		}
	}

	switch n.kind {
	case nodeScalar:
		return d.decodeScalar(n, v)
	case nodeMapping:
		return d.decodeMapping(n, v)
	case nodeSequence:
		return d.decodeSequence(n, v)
	}

	return nil
}

func (d *decoder) decodeAlias(n *node, v reflect.Value) error {
	target, ok := d.anchors[n.alias]
	if !ok {
		return &SyntaxError{
			Message: fmt.Sprintf("unknown alias %q", n.alias),
			Pos:     n.pos,
		}
	}

	d.aliasDepth++
	if d.aliasDepth > d.opts.maxAliasExpansion {
		return &CycleError{Anchor: n.alias, Pos: n.pos}
	}
	defer func() { d.aliasDepth-- }()

	return d.decode(target, v)
}

func (d *decoder) decodeScalar(n *node, v reflect.Value) error {
	if n.implicit && n.value == "" {
		return nil
	}

	if n.tag != "" && d.opts.tagResolvers != nil {
		if resolver, ok := d.opts.tagResolvers[n.tag]; ok {
			result, err := resolver.Resolve(n.value)
			if err != nil {
				return &SyntaxError{Message: fmt.Sprintf("tag resolver %q: %v", n.tag, err), Pos: n.pos}
			}
			rv := reflect.ValueOf(result)
			if rv.Type().AssignableTo(v.Type()) {
				v.Set(rv)
			} else if rv.Type().ConvertibleTo(v.Type()) {
				v.Set(rv.Convert(v.Type()))
			} else {
				d.addTypeError(n, v.Type())
			}
			return nil
		}
	}

	val := n.value

	if n.style == scalarPlain && isNullValue(val) {
		v.Set(reflect.Zero(v.Type()))
		return nil
	}

	switch v.Kind() {
	case reflect.String:
		v.SetString(val)
	case reflect.Bool:
		b, err := parseBool(val)
		if err != nil {
			d.addTypeError(n, v.Type())
			return nil
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Type() == reflect.TypeFor[time.Duration]() {
			dur, err := time.ParseDuration(val)
			if err != nil {
				d.addTypeError(n, v.Type())
				return nil
			}
			v.SetInt(int64(dur))
			return nil
		}
		i, err := parseInt(val)
		if err != nil {
			d.addTypeError(n, v.Type())
			return nil
		}
		v.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := parseUint(val)
		if err != nil {
			d.addTypeError(n, v.Type())
			return nil
		}
		v.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := parseFloat(val)
		if err != nil {
			d.addTypeError(n, v.Type())
			return nil
		}
		v.SetFloat(f)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			if n.tag == "tag:yaml.org,2002:binary" {
				decoded, err := decodeBase64(val)
				if err != nil {
					d.addTypeError(n, v.Type())
					return nil
				}
				v.SetBytes(decoded)
			} else {
				v.SetBytes([]byte(val))
			}
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			t, err := parseTime(val)
			if err != nil {
				d.addTypeError(n, v.Type())
				return nil
			}
			v.Set(reflect.ValueOf(t))
			return nil
		}
		d.addTypeError(n, v.Type())
	default:
		d.addTypeError(n, v.Type())
	}
	return nil
}

func (d *decoder) decodeMapping(n *node, v reflect.Value) error {
	switch v.Kind() {
	case reflect.Map:
		return d.decodeMappingToMap(n, v)
	case reflect.Struct:
		return d.decodeMappingToStruct(n, v)
	case reflect.Interface:
		if v.NumMethod() == 0 {
			result, err := d.decodeToAny(n)
			if err != nil {
				return err
			}
			if result != nil {
				v.Set(reflect.ValueOf(result))
			}
			return nil
		}
		return &TypeError{Errors: []string{fmt.Sprintf("cannot decode mapping into %s", v.Type())}}
	default:
		d.addTypeError(n, v.Type())
		return nil
	}
}

func (d *decoder) decodeMappingToMap(n *node, v reflect.Value) error {
	if v.IsNil() {
		v.Set(reflect.MakeMap(v.Type()))
	}
	keyType := v.Type().Key()
	valType := v.Type().Elem()

	for i := 0; i < len(n.children)-1; i += 2 {
		keyNode := n.children[i]
		valNode := n.children[i+1]

		if isMergeKey(keyNode) {
			if err := d.decodeMerge(valNode, v); err != nil {
				return err
			}
			continue
		}

		key := reflect.New(keyType).Elem()
		if err := d.decode(keyNode, key); err != nil {
			return err
		}

		if d.opts.disallowDuplicates {
			existing := v.MapIndex(key)
			if existing.IsValid() {
				return &DuplicateKeyError{Key: keyNode.value, Pos: keyNode.pos}
			}
		}

		val := reflect.New(valType).Elem()
		if err := d.decode(valNode, val); err != nil {
			return err
		}

		v.SetMapIndex(key, val)
	}
	return nil
}

func (d *decoder) decodeMappingToStruct(n *node, v reflect.Value) error {
	sf := getStructFields(v.Type())

	for i := 0; i < len(n.children)-1; i += 2 {
		keyNode := n.children[i]
		valNode := n.children[i+1]

		if isMergeKey(keyNode) {
			if err := d.decodeMerge(valNode, v); err != nil {
				return err
			}
			continue
		}

		keyName := keyNode.value

		idx, ok := sf.byName[keyName]
		if !ok {
			if d.opts.strict {
				return &UnknownFieldError{Field: keyName, Pos: keyNode.pos}
			}
			continue
		}

		fi := sf.fields[idx]
		field := fieldByIndex(v, fi.index)
		if !field.CanSet() {
			continue
		}

		if err := d.decode(valNode, field); err != nil {
			return err
		}
	}

	seen := make(map[string]bool)
	for i := 0; i < len(n.children)-1; i += 2 {
		if n.children[i].kind == nodeScalar {
			seen[n.children[i].value] = true
		}
	}
	for _, fi := range sf.fields {
		if fi.required && !seen[fi.name] {
			return &SyntaxError{
				Message: fmt.Sprintf("required field %q is missing", fi.name),
				Pos:     n.pos,
			}
		}
	}

	return nil
}

func (d *decoder) decodeMerge(n *node, v reflect.Value) error {
	if n.kind == nodeAlias {
		target, ok := d.anchors[n.alias]
		if !ok {
			return &SyntaxError{
				Message: fmt.Sprintf("unknown alias %q", n.alias),
				Pos:     n.pos,
			}
		}
		return d.decodeMappingMerge(target, v)
	}

	if n.kind == nodeSequence {
		for _, child := range n.children {
			if err := d.decodeMerge(child, v); err != nil {
				return err
			}
		}
		return nil
	}

	if n.kind == nodeMapping {
		return d.decodeMappingMerge(n, v)
	}

	return nil
}

func (d *decoder) decodeMappingMerge(n *node, v reflect.Value) error {
	if n.kind != nodeMapping {
		return nil
	}

	switch v.Kind() {
	case reflect.Map:
		for i := 0; i < len(n.children)-1; i += 2 {
			keyNode := n.children[i]
			valNode := n.children[i+1]

			key := reflect.New(v.Type().Key()).Elem()
			if err := d.decode(keyNode, key); err != nil {
				return err
			}
			existing := v.MapIndex(key)
			if existing.IsValid() {
				continue
			}
			val := reflect.New(v.Type().Elem()).Elem()
			if err := d.decode(valNode, val); err != nil {
				return err
			}
			v.SetMapIndex(key, val)
		}

	case reflect.Struct:
		sf := getStructFields(v.Type())
		for i := 0; i < len(n.children)-1; i += 2 {
			keyNode := n.children[i]
			valNode := n.children[i+1]

			if isMergeKey(keyNode) {
				if err := d.decodeMerge(valNode, v); err != nil {
					return err
				}
				continue
			}

			idx, ok := sf.byName[keyNode.value]
			if !ok {
				continue
			}
			fi := sf.fields[idx]
			field := fieldByIndex(v, fi.index)
			if !field.CanSet() || !field.IsZero() {
				continue
			}
			if err := d.decode(valNode, field); err != nil {
				return err
			}
		}
	}

	return nil
}

func (d *decoder) decodeSequence(n *node, v reflect.Value) error {
	switch v.Kind() {
	case reflect.Slice:
		slice := reflect.MakeSlice(v.Type(), len(n.children), len(n.children))
		for i, child := range n.children {
			if err := d.decode(child, slice.Index(i)); err != nil {
				return err
			}
		}
		v.Set(slice)
	case reflect.Array:
		for i, child := range n.children {
			if i >= v.Len() {
				break
			}
			if err := d.decode(child, v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Interface:
		if v.NumMethod() == 0 {
			result, err := d.decodeToAny(n)
			if err != nil {
				return err
			}
			if result != nil {
				v.Set(reflect.ValueOf(result))
			}
			return nil
		}
		return &TypeError{Errors: []string{fmt.Sprintf("cannot decode sequence into %s", v.Type())}}
	default:
		d.addTypeError(n, v.Type())
	}
	return nil
}

func (d *decoder) decodeToAny(n *node) (any, error) {
	if n == nil {
		return nil, nil
	}

	if n.anchor != "" {
		d.anchors[n.anchor] = n
	}

	if n.kind == nodeAlias {
		target, ok := d.anchors[n.alias]
		if !ok {
			return nil, &SyntaxError{
				Message: fmt.Sprintf("unknown alias %q", n.alias),
				Pos:     n.pos,
			}
		}
		d.aliasDepth++
		if d.aliasDepth > d.opts.maxAliasExpansion {
			return nil, &CycleError{Anchor: n.alias, Pos: n.pos}
		}
		defer func() { d.aliasDepth-- }()
		return d.decodeToAny(target)
	}

	if n.kind == nodeDocument {
		if len(n.children) == 0 {
			return nil, nil
		}
		return d.decodeToAny(n.children[0])
	}

	switch n.kind {
	case nodeScalar:
		return d.scalarToAny(n), nil

	case nodeMapping:
		if d.opts.useOrderedMap {
			return d.decodeMappingToOrderedMap(n)
		}
		m := make(map[string]any, len(n.children)/2)
		for i := 0; i < len(n.children)-1; i += 2 {
			keyNode := n.children[i]
			valNode := n.children[i+1]

			if isMergeKey(keyNode) {
				merged, err := d.decodeToAny(valNode)
				if err != nil {
					return nil, err
				}
				if mm, ok := merged.(map[string]any); ok {
					for k, v := range mm {
						if _, exists := m[k]; !exists {
							m[k] = v
						}
					}
				}
				if ms, ok := merged.([]any); ok {
					for _, item := range ms {
						if mm, ok := item.(map[string]any); ok {
							for k, v := range mm {
								if _, exists := m[k]; !exists {
									m[k] = v
								}
							}
						}
					}
				}
				continue
			}

			var keyStr string
			switch keyNode.kind {
			case nodeScalar:
				keyStr = keyNode.value
			case nodeAlias:
				resolved, err := d.decodeToAny(keyNode)
				if err != nil {
					return nil, err
				}
				keyStr = fmt.Sprintf("%v", resolved)
			default:
				keyStr = fmt.Sprintf("%v", keyNode.value)
			}

			val, err := d.decodeToAny(valNode)
			if err != nil {
				return nil, err
			}
			m[keyStr] = val
		}
		return m, nil

	case nodeSequence:
		s := make([]any, 0, len(n.children))
		for _, child := range n.children {
			val, err := d.decodeToAny(child)
			if err != nil {
				return nil, err
			}
			s = append(s, val)
		}
		return s, nil
	}

	return nil, nil
}

func (d *decoder) decodeMappingToOrderedMap(n *node) (MapSlice, error) {
	ms := make(MapSlice, 0, len(n.children)/2)
	for i := 0; i < len(n.children)-1; i += 2 {
		keyNode := n.children[i]
		valNode := n.children[i+1]

		var keyStr string
		switch keyNode.kind {
		case nodeScalar:
			keyStr = keyNode.value
		case nodeAlias:
			resolved, err := d.decodeToAny(keyNode)
			if err != nil {
				return nil, err
			}
			keyStr = fmt.Sprintf("%v", resolved)
		default:
			keyStr = fmt.Sprintf("%v", keyNode.value)
		}

		val, err := d.decodeToAny(valNode)
		if err != nil {
			return nil, err
		}
		ms = append(ms, MapItem{Key: keyStr, Value: val})
	}
	return ms, nil
}

func (d *decoder) scalarToAny(n *node) any {
	val := n.value

	if n.tag == "tag:yaml.org,2002:str" {
		return val
	}

	if n.tag == "tag:yaml.org,2002:binary" {
		return val
	}

	if n.tag == "!" {
		return val
	}

	if n.implicit && val == "" {
		return nil
	}

	if n.style != scalarPlain {
		return val
	}

	if isNullValue(val) {
		return nil
	}

	if b, err := parseBool(val); err == nil {
		return b
	}

	if i, err := parseInt(val); err == nil {
		if i >= math.MinInt && i <= math.MaxInt {
			return int64(i)
		}
		return i
	}

	if f, err := parseFloat(val); err == nil {
		return f
	}

	return val
}

func (d *decoder) addTypeError(n *node, t reflect.Type) {
	msg := fmt.Sprintf("line %d: cannot unmarshal %s into Go value of type %s", n.pos.Line, nodeKindName(n.kind), t)
	d.typeErrors = append(d.typeErrors, msg)
}

func nodeKindName(k nodeKind) string {
	switch k {
	case nodeScalar:
		return "!!str"
	case nodeMapping:
		return "!!map"
	case nodeSequence:
		return "!!seq"
	default:
		return "unknown"
	}
}

func isMergeKey(n *node) bool {
	return n.kind == nodeScalar && n.value == "<<"
}

func isNullValue(s string) bool {
	return s == "" || s == "~" || s == "null" || s == "Null" || s == "NULL"
}

func parseBool(s string) (bool, error) {
	switch s {
	case "true", "True", "TRUE":
		return true, nil
	case "false", "False", "FALSE":
		return false, nil
	}
	return false, fmt.Errorf("not a bool: %q", s)
}

func parseInt(s string) (int64, error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		return strconv.ParseInt(s[2:], 16, 64)
	}
	if strings.HasPrefix(s, "0o") || strings.HasPrefix(s, "0O") {
		return strconv.ParseInt(s[2:], 8, 64)
	}
	if strings.HasPrefix(s, "0b") || strings.HasPrefix(s, "0B") {
		return strconv.ParseInt(s[2:], 2, 64)
	}
	return strconv.ParseInt(s, 10, 64)
}

func parseUint(s string) (uint64, error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		return strconv.ParseUint(s[2:], 16, 64)
	}
	if strings.HasPrefix(s, "0o") || strings.HasPrefix(s, "0O") {
		return strconv.ParseUint(s[2:], 8, 64)
	}
	return strconv.ParseUint(s, 10, 64)
}

func parseFloat(s string) (float64, error) {
	switch s {
	case ".inf", ".Inf", ".INF":
		return math.Inf(1), nil
	case "-.inf", "-.Inf", "-.INF":
		return math.Inf(-1), nil
	case ".nan", ".NaN", ".NAN":
		return math.NaN(), nil
	}
	return strconv.ParseFloat(s, 64)
}

func parseTime(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02",
		"2006-01-02 15:04:05",
		"2006-01-02t15:04:05Z07:00",
		time.RFC3339,
		time.RFC3339Nano,
	}
	for _, f := range formats {
		t, err := time.Parse(f, s)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q as time", s)
}

func fieldByIndex(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

func marshalNode(n *node) ([]byte, error) {
	enc := newEncoder(defaultEncodeOptions())
	return enc.encodeNode(n)
}

// Unmarshaler is implemented by types that can decode themselves from YAML.
// The unmarshal function may be called with a pointer to decode the
// underlying YAML value into any Go type.
type Unmarshaler interface {
	UnmarshalYAML(unmarshal func(any) error) error
}

// BytesUnmarshaler is implemented by types that can decode themselves from
// raw YAML bytes.
type BytesUnmarshaler interface {
	UnmarshalYAML([]byte) error
}

// Marshaler is implemented by types that can encode themselves into a YAML-
// compatible Go value. The returned value is then encoded normally.
type Marshaler interface {
	MarshalYAML() (any, error)
}

// BytesMarshaler is implemented by types that can encode themselves directly
// into YAML bytes.
type BytesMarshaler interface {
	MarshalYAML() ([]byte, error)
}

// MarshalerContext is like [Marshaler] but accepts a context, which is set
// via [Encoder.EncodeContext].
type MarshalerContext interface {
	MarshalYAML(ctx context.Context) (any, error)
}

// UnmarshalerContext is like [Unmarshaler] but accepts a context, which is
// set via [Decoder.DecodeContext].
type UnmarshalerContext interface {
	UnmarshalYAML(ctx context.Context, unmarshal func(any) error) error
}

type jsonUnmarshaler interface {
	UnmarshalJSON([]byte) error
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, s)
	return base64.StdEncoding.DecodeString(s)
}

func jsonEncodeYAML(yamlData []byte) ([]byte, error) {
	var v any
	if err := Unmarshal(yamlData, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
