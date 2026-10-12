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
	"strconv"
	"strings"
	"time"
)

type decoder struct {
	opts       *decoderOptions
	ctx        context.Context
	anchors    map[string]*node
	aliasDepth int
	aliasNodes int // nodes decoded inside alias expansions
	aliasLimit int // limit on aliasNodes; zero means no limit
	depth      int
	typeErrors []string
}

func newDecoder(opts *decoderOptions) *decoder {
	return &decoder{
		opts:       opts,
		ctx:        context.Background(),
		anchors:    make(map[string]*node),
		aliasLimit: aliasNodeLimit(opts, 0),
	}
}

// countAliasNode counts n toward the alias expansion limit when it is being
// decoded as part of an alias expansion.
func (d *decoder) countAliasNode(n *node) error {
	if d.aliasDepth == 0 {
		return nil
	}
	d.aliasNodes++
	if d.aliasLimit > 0 && d.aliasNodes > d.aliasLimit {
		return &AliasExpansionError{Limit: d.aliasLimit, Pos: n.pos}
	}
	return nil
}

func (d *decoder) decode(n *node, v reflect.Value) error {
	if n == nil {
		return nil
	}
	if err := d.countAliasNode(n); err != nil {
		return err
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
		if n.kind == nodeScalar && n.style == scalarPlain && isNullValue(n.value) {
			v.Set(reflect.Zero(v.Type()))
			return nil
		}
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
				if e, ok := reflect.TypeAssert[error](out[0]); ok {
					return e
				}
			}
			v.Set(ptr.Elem())
			return nil
		}
	}

	if v.CanAddr() {
		if u, ok := reflect.TypeAssert[UnmarshalerContext](v.Addr()); ok {
			return u.UnmarshalYAML(d.ctx, func(target any) error {
				rv := reflect.ValueOf(target)
				if rv.Kind() == reflect.Pointer {
					return d.decode(n, rv.Elem())
				}
				return d.decode(n, rv)
			})
		}
		if u, ok := reflect.TypeAssert[Unmarshaler](v.Addr()); ok {
			return u.UnmarshalYAML(func(target any) error {
				rv := reflect.ValueOf(target)
				if rv.Kind() == reflect.Pointer {
					return d.decode(n, rv.Elem())
				}
				return d.decode(n, rv)
			})
		}
		if u, ok := reflect.TypeAssert[BytesUnmarshaler](v.Addr()); ok {
			raw, err := marshalNode(n)
			if err != nil {
				return err
			}
			return u.UnmarshalYAML(raw)
		}
	}

	if v.CanAddr() && v.Type() != reflect.TypeFor[time.Time]() {
		if u, ok := reflect.TypeAssert[encoding.TextUnmarshaler](v.Addr()); ok {
			if n.kind == nodeScalar {
				return u.UnmarshalText([]byte(n.value))
			}
		}
	}

	if d.opts.useJSONUnmarshaler && v.CanAddr() {
		if u, ok := reflect.TypeAssert[jsonUnmarshaler](v.Addr()); ok {
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

	if n.kind == nodeMapping && v.Type() == reflect.TypeFor[MapSlice]() {
		return d.decodeIntoMapSlice(n, v)
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
			switch {
			case rv.Type().AssignableTo(v.Type()):
				v.Set(rv)
			case rv.Type().ConvertibleTo(v.Type()):
				v.Set(rv.Convert(v.Type()))
			default:
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
		switch v.Type() {
		case reflect.TypeFor[big.Int]():
			bi := new(big.Int)
			if _, ok := bi.SetString(val, 0); !ok {
				d.addTypeError(n, v.Type())
				return nil
			}
			v.Set(reflect.ValueOf(*bi))
			return nil
		case reflect.TypeFor[big.Float]():
			bf, _, err := big.ParseFloat(val, 10, 256, big.ToNearestEven)
			if err != nil {
				d.addTypeError(n, v.Type())
				return nil
			}
			v.Set(reflect.ValueOf(*bf))
			return nil
		case reflect.TypeFor[big.Rat]():
			br := new(big.Rat)
			if _, ok := br.SetString(val); !ok {
				d.addTypeError(n, v.Type())
				return nil
			}
			v.Set(reflect.ValueOf(*br))
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
		typeErrs := len(d.typeErrors)
		if err := d.decode(keyNode, key); err != nil {
			return err
		}
		if len(d.typeErrors) > typeErrs {
			// The key could not be decoded; leave the entry out rather than
			// store it under the zero key.
			continue
		}
		if !key.Comparable() {
			d.addKeyTypeError(keyNode, collectionKind(key.Interface()), v.Type())
			continue
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
	if len(sf.conflicts) > 0 {
		return &SyntaxError{
			Message: fmt.Sprintf("struct has conflicting yaml field names: %s", strings.Join(sf.conflicts, ", ")),
			Pos:     n.pos,
		}
	}

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
		if ok && sf.fields[idx].inline {
			ok = false
		}
		if !ok {
			if m, found := inlineMapField(v, sf); found {
				if err := d.decodeMapEntry(m, keyNode, valNode); err != nil {
					return err
				}
				continue
			}
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

	if d.opts.applyDefaults {
		if err := d.applyDefaults(v, sf, seen); err != nil {
			return err
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

	if d.opts.validator != nil && v.Kind() == reflect.Struct {
		iface := v.Addr().Interface()
		if err := d.opts.validator.Struct(iface); err != nil {
			return &ValidationError{Err: err, Pos: n.pos}
		}
	}

	return nil
}

// inlineMapField returns the settable inline map field of struct v, making
// the map if it is nil.
func inlineMapField(v reflect.Value, sf *structFields) (reflect.Value, bool) {
	for i := range sf.fields {
		fi := &sf.fields[i]
		if !fi.inline {
			continue
		}
		field := fieldByIndex(v, fi.index)
		if field.Kind() != reflect.Map || !field.CanSet() {
			continue
		}
		if field.IsNil() {
			field.Set(reflect.MakeMap(field.Type()))
		}
		return field, true
	}
	return reflect.Value{}, false
}

// decodeMapEntry decodes one mapping entry into map m.
func (d *decoder) decodeMapEntry(m reflect.Value, keyNode, valNode *node) error {
	key := reflect.New(m.Type().Key()).Elem()
	if err := d.decode(keyNode, key); err != nil {
		return err
	}
	if !key.Comparable() {
		d.addKeyTypeError(keyNode, collectionKind(key.Interface()), m.Type())
		return nil
	}
	if d.opts.disallowDuplicates && m.MapIndex(key).IsValid() {
		return &DuplicateKeyError{Key: keyNode.value, Pos: keyNode.pos}
	}
	val := reflect.New(m.Type().Elem()).Elem()
	if err := d.decode(valNode, val); err != nil {
		return err
	}
	m.SetMapIndex(key, val)
	return nil
}

func (d *decoder) applyDefaults(v reflect.Value, sf *structFields, seen map[string]bool) error {
	for _, fi := range sf.fields {
		if !fi.hasDefault {
			continue
		}

		if fi.required {
			return &DefaultError{
				Field:   fi.name,
				Message: "field has both required and default tags, which is contradictory",
			}
		}

		if seen[fi.name] {
			continue
		}

		field := fieldByIndex(v, fi.index)
		if !field.CanSet() {
			continue
		}

		if !field.IsZero() {
			continue
		}

		if err := setDefaultValue(field, fi.defaultValue); err != nil {
			return &DefaultError{
				Field:   fi.name,
				Message: err.Error(),
			}
		}
	}
	return nil
}

func setDefaultValue(v reflect.Value, raw string) error {
	if v.Kind() == reflect.Pointer {
		ptr := reflect.New(v.Type().Elem())
		if err := setDefaultValue(ptr.Elem(), raw); err != nil {
			return err
		}
		v.Set(ptr)
		return nil
	}

	switch v.Kind() {
	case reflect.String:
		v.SetString(raw)

	case reflect.Bool:
		b, err := parseBool(raw)
		if err != nil {
			return fmt.Errorf("%w %q", errInvalidBoolDefault, raw)
		}
		v.SetBool(b)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Type() == reflect.TypeFor[time.Duration]() {
			dur, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("%w %q", errInvalidDurationDefault, raw)
			}
			v.SetInt(int64(dur))
			return nil
		}
		i, err := parseInt(raw)
		if err != nil {
			return fmt.Errorf("%w %q", errInvalidIntDefault, raw)
		}
		v.SetInt(i)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := parseUint(raw)
		if err != nil {
			return fmt.Errorf("%w %q", errInvalidUintDefault, raw)
		}
		v.SetUint(u)

	case reflect.Float32, reflect.Float64:
		f, err := parseFloat(raw)
		if err != nil {
			return fmt.Errorf("%w %q", errInvalidFloatDefault, raw)
		}
		v.SetFloat(f)

	default:
		return fmt.Errorf("%w %s", errUnsupportedDefault, v.Type())
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
		d.aliasDepth++
		defer func() { d.aliasDepth-- }()
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
			if !key.Comparable() {
				d.addKeyTypeError(keyNode, collectionKind(key.Interface()), v.Type())
				continue
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
			if !ok || sf.fields[idx].inline {
				if m, found := inlineMapField(v, sf); found {
					if err := d.decodeMappingMerge(&node{kind: nodeMapping, children: []*node{keyNode, valNode}}, m); err != nil {
						return err
					}
				}
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
	if err := d.countAliasNode(n); err != nil {
		return nil, err
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

			// A mapping or sequence key cannot be a string key; report it
			// rather than letting such keys collide.
			var keyStr string
			switch keyNode.kind {
			case nodeScalar:
				keyStr = keyNode.value
			case nodeAlias:
				resolved, err := d.decodeToAny(keyNode)
				if err != nil {
					return nil, err
				}
				if isCollection(resolved) {
					d.addKeyTypeError(keyNode, collectionKind(resolved), reflect.TypeFor[map[string]any]())
					continue
				}
				keyStr = fmt.Sprintf("%v", resolved)
			default:
				d.addKeyTypeError(keyNode, keyNode.kind, reflect.TypeFor[map[string]any]())
				continue
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

// decodeIntoMapSlice decodes mapping n into the MapSlice v, decoding nested
// mappings as MapSlice too, as [WithOrderedMap] does.
func (d *decoder) decodeIntoMapSlice(n *node, v reflect.Value) error {
	prev := d.opts.useOrderedMap
	d.opts.useOrderedMap = true
	defer func() { d.opts.useOrderedMap = prev }()
	ms, err := d.decodeMappingToOrderedMap(n)
	if err != nil {
		return err
	}
	v.Set(reflect.ValueOf(ms))
	return nil
}

// decodeMappingToOrderedMap decodes n into a MapSlice in source order. A
// "<<" merge adds the merged entries that are not already present where the
// merge appears; an explicit key that follows replaces a merged entry's
// value in place.
func (d *decoder) decodeMappingToOrderedMap(n *node) (MapSlice, error) {
	ms := make(MapSlice, 0, len(n.children)/2)
	index := make(map[string]int) // string key -> position in ms
	merged := make(map[string]bool)
	for i := 0; i < len(n.children)-1; i += 2 {
		keyNode := n.children[i]
		valNode := n.children[i+1]

		if isMergeKey(keyNode) {
			src, err := d.decodeToAny(valNode)
			if err != nil {
				return nil, err
			}
			sources := []any{src}
			if seq, ok := src.([]any); ok {
				sources = seq
			}
			for _, s := range sources {
				m, ok := s.(MapSlice)
				if !ok {
					continue
				}
				for _, item := range m {
					k, isStr := item.Key.(string)
					if isStr {
						if _, exists := index[k]; exists {
							continue
						}
						index[k] = len(ms)
						merged[k] = true
					}
					ms = append(ms, item)
				}
			}
			continue
		}

		// MapItem.Key can hold any key, so a mapping or sequence key keeps
		// its decoded value.
		var key any
		switch keyNode.kind {
		case nodeScalar:
			key = keyNode.value
		default:
			resolved, err := d.decodeToAny(keyNode)
			if err != nil {
				return nil, err
			}
			key = resolved
			if keyNode.kind == nodeAlias && !isCollection(resolved) {
				key = fmt.Sprintf("%v", resolved)
			}
		}

		val, err := d.decodeToAny(valNode)
		if err != nil {
			return nil, err
		}
		if k, isStr := key.(string); isStr {
			if at, ok := index[k]; ok && merged[k] {
				ms[at].Value = val
				merged[k] = false
				continue
			}
			if _, ok := index[k]; !ok {
				index[k] = len(ms)
			}
		}
		ms = append(ms, MapItem{Key: key, Value: val})
	}
	return ms, nil
}

// isCollection reports whether v is a decoded mapping or sequence.
func isCollection(v any) bool {
	switch v.(type) {
	case map[string]any, []any, MapSlice:
		return true
	}
	return false
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

	if v, ok := resolveCoreTag(n.tag, val); ok {
		return v
	}

	if n.implicit && val == "" {
		return nil
	}

	if n.style != scalarPlain {
		return val
	}

	switch d.opts.schema {
	case FailsafeSchema:
		return val
	case JSONSchema:
		return scalarToAnyJSON(val)
	default:
		return scalarToAnyCore(val)
	}
}

// Long forms of the YAML 1.2 Core schema scalar tags.
const (
	tagNull  = "tag:yaml.org,2002:null"
	tagBool  = "tag:yaml.org,2002:bool"
	tagInt   = "tag:yaml.org,2002:int"
	tagFloat = "tag:yaml.org,2002:float"
)

// resolveCoreTag returns the value of a scalar explicitly tagged !!null,
// !!bool, !!int or !!float. It reports false for other tags and for a value
// the tag cannot represent, which then resolves as if untagged.
func resolveCoreTag(tag, val string) (any, bool) {
	switch tag {
	case tagNull:
		if isNullValue(val) {
			return nil, true
		}
	case tagBool:
		if b, err := parseBool(val); err == nil {
			return b, true
		}
	case tagInt:
		if i, err := parseInt(val); err == nil {
			return i, true
		}
	case tagFloat:
		if f, err := parseFloat(val); err == nil {
			return f, true
		}
	}
	return nil, false
}

func scalarToAnyCore(val string) any {
	if isNullValue(val) {
		return nil
	}

	if b, err := parseBool(val); err == nil {
		return b
	}

	if i, ok := parseCoreInt(val); ok {
		return i
	}
	if u, ok := parseCoreUint(val); ok {
		return u
	}

	if f, ok := parseCoreFloat(val); ok {
		return f
	}

	return val
}

func scalarToAnyJSON(val string) any {
	if val == nullLiteral {
		return nil
	}

	if val == trueLiteral {
		return true
	}
	if val == falseLiteral {
		return false
	}

	// JSON numbers are decimal and take no leading '+'.
	if val == "" || val[0] == '+' {
		return val
	}
	if i, err := strconv.ParseInt(val, 10, 64); err == nil {
		return i
	}
	if isDecimalFloat(val) {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}

	return val
}

func (d *decoder) addTypeError(n *node, t reflect.Type) {
	msg := fmt.Sprintf("line %d: cannot unmarshal %s into Go value of type %s", n.pos.Line, nodeKindName(n.kind), t)
	d.typeErrors = append(d.typeErrors, msg)
}

// addKeyTypeError records that the mapping or sequence key at n cannot be a
// key of a map of type mapType.
func (d *decoder) addKeyTypeError(n *node, kind nodeKind, mapType reflect.Type) {
	msg := fmt.Sprintf("line %d: cannot use %s as a key of %s", n.pos.Line, nodeKindName(kind), mapType)
	d.typeErrors = append(d.typeErrors, msg)
}

// collectionKind returns the node kind a decoded collection came from.
func collectionKind(v any) nodeKind {
	if _, ok := v.([]any); ok {
		return nodeSequence
	}
	return nodeMapping
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
	return n.kind == nodeMergeKey || n.tag == "tag:yaml.org,2002:merge" || n.kind == nodeScalar && n.value == "<<" && n.style == scalarPlain && n.tag == ""
}

// Canonical YAML 1.2 / KYAML representations of null and the booleans.
const (
	nullLiteral  = "null"
	trueLiteral  = "true"
	falseLiteral = "false"
)

func isNullValue(s string) bool {
	return s == "" || s == "~" || s == nullLiteral || s == "Null" || s == "NULL"
}

func parseBool(s string) (bool, error) {
	switch s {
	case trueLiteral, "True", "TRUE":
		return true, nil
	case falseLiteral, "False", "FALSE":
		return false, nil
	}
	return false, fmt.Errorf("not a bool %q: %w", s, errNotBool)
}

// intDigits splits an integer for an integer target into its digits and
// base: unsigned digits after a 0x, 0o or 0b prefix in either case, or an
// optionally signed decimal.
func intDigits(s string) (digits string, base int, err error) {
	for _, p := range []struct {
		prefix string
		base   int
	}{{"0x", 16}, {"0o", 8}, {"0b", 2}} {
		if len(s) >= 2 && strings.EqualFold(s[:2], p.prefix) {
			digits = s[2:]
			if digits == "" || digits[0] == '+' || digits[0] == '-' {
				return "", 0, fmt.Errorf("invalid integer %q: %w", s, strconv.ErrSyntax)
			}
			return digits, p.base, nil
		}
	}
	return s, 10, nil
}

// parseInt parses s for an integer target. See intDigits for the forms.
func parseInt(s string) (int64, error) {
	digits, base, err := intDigits(s)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(digits, base, 64)
}

// parseUint parses s for an unsigned integer target. It accepts the same
// forms as parseInt.
func parseUint(s string) (uint64, error) {
	digits, base, err := intDigits(s)
	if err != nil {
		return 0, err
	}
	if base == 10 {
		digits = strings.TrimPrefix(digits, "+")
	}
	return strconv.ParseUint(digits, base, 64)
}

// parseCoreInt parses s as a YAML 1.2 Core schema integer: an optionally
// signed decimal, or digits after a lowercase 0x or 0o prefix. Binary and
// uppercase prefixes are not part of the Core schema.
func parseCoreInt(s string) (int64, bool) {
	if len(s) >= 2 && s[0] == '0' && strings.ContainsRune("bBXO", rune(s[1])) {
		return 0, false
	}
	i, err := parseInt(s)
	return i, err == nil
}

// parseCoreUint parses a Core schema integer too large for int64.
func parseCoreUint(s string) (uint64, bool) {
	if len(s) >= 2 && s[0] == '0' && strings.ContainsRune("bBXO", rune(s[1])) {
		return 0, false
	}
	u, err := parseUint(s)
	return u, err == nil
}

// parseFloat parses s for a float target. Besides the YAML spellings it
// accepts anything [strconv.ParseFloat] does, such as "inf" or "NaN".
func parseFloat(s string) (float64, error) {
	if f, ok := yamlSpecialFloat(s); ok {
		return f, nil
	}
	return strconv.ParseFloat(s, 64)
}

// yamlSpecialFloat returns the value of a YAML 1.2 Core infinity or NaN
// spelling.
func yamlSpecialFloat(s string) (float64, bool) {
	switch s {
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF":
		return math.Inf(1), true
	case "-.inf", "-.Inf", "-.INF":
		return math.Inf(-1), true
	case ".nan", ".NaN", ".NAN":
		return math.NaN(), true
	}
	return 0, false
}

// parseCoreFloat parses s as a YAML 1.2 Core schema float: an optionally
// signed decimal with an optional fraction and exponent, or one of the
// infinity and NaN spellings. Plain "inf", "nan" or "infinity" are strings.
func parseCoreFloat(s string) (float64, bool) {
	if f, ok := yamlSpecialFloat(s); ok {
		return f, true
	}
	if !isDecimalFloat(s) {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

// isDecimalFloat reports whether s matches
// [-+]? ( \. [0-9]+ | [0-9]+ ( \. [0-9]* )? ) ( [eE] [-+]? [0-9]+ )?
func isDecimalFloat(s string) bool {
	digits := func(i int) int {
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return i
	}
	i := 0
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	start := i
	i = digits(i)
	intDigits := i > start
	if i < len(s) && s[i] == '.' {
		j := digits(i + 1)
		if !intDigits && j == i+1 {
			return false
		}
		i = j
	} else if !intDigits {
		return false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '-' || s[i] == '+') {
			i++
		}
		j := digits(i)
		if j == i {
			return false
		}
		i = j
	}
	return i == len(s)
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
	return time.Time{}, fmt.Errorf("cannot parse %q as time: %w", s, errNotTime)
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
// YAML bytes. data is the value re-encoded as block-style YAML, not the
// original source text (see [RawValue]).
type BytesUnmarshaler interface {
	UnmarshalYAML(data []byte) error
}

// Marshaler is implemented by types that can encode themselves into a YAML-
// compatible Go value. The returned value is then encoded normally.
type Marshaler interface {
	MarshalYAML() (any, error)
}

// BytesMarshaler is implemented by types that can encode themselves directly
// into YAML bytes. The bytes are parsed and written again at the position
// they appear, so multi-line output fits any indent or flow style. Bytes
// that are not valid YAML, or hold only a comment, are written as a string;
// more than one document is an error.
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
	UnmarshalJSON(data []byte) error
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
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("yaml: json encode: %w", err)
	}
	return b, nil
}
