package yaml

import (
	"context"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type decoder struct {
	opts       *decoderOptions
	ctx        context.Context
	refAnchors map[string]*node // anchors loaded from reference files
	aliasDepth int
	aliasNodes int // nodes decoded inside alias expansions
	aliasLimit int // limit on aliasNodes; zero means no limit
	depth      int
	typeErrors []string
	// mergeSource is set while decoding a merge value into any: the
	// mappings it names keep the first of a repeated key, as merging into a
	// typed target does.
	mergeSource bool
	// merged holds the merge-source mappings already decoded for the mapping
	// whose merges are being decoded, so that a source reached again through
	// another merge is not decoded again.
	merged map[*node]any
}

// mergeScope starts a fresh set of decoded merge sources for a mapping that
// is not itself a merge source. The returned function restores the previous
// set.
func (d *decoder) mergeScope() func() {
	prev := d.merged
	d.merged = nil
	return func() { d.merged = prev }
}

// decodeMergeSourceToAny decodes mapping n as a merge source with decode,
// reusing the result when n was already decoded for the same mapping.
func (d *decoder) decodeMergeSourceToAny(n *node, decode func() (any, error)) (any, error) {
	if v, ok := d.merged[n]; ok {
		return v, nil
	}
	v, err := decode()
	if err != nil {
		return nil, err
	}
	if d.merged == nil {
		d.merged = make(map[*node]any)
	}
	d.merged[n] = v
	return v, nil
}

func newDecoder(opts *decoderOptions) *decoder {
	return &decoder{
		opts:       opts,
		ctx:        context.Background(),
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

// aliasTarget returns the node alias n refers to: the anchor defined before
// it in its document, or else an anchor from a reference file.
func (d *decoder) aliasTarget(n *node) (*node, error) {
	if n.ref != nil && n.ref.n != nil {
		return n.ref.n, nil
	}
	if target, ok := d.refAnchors[n.alias]; ok {
		return target, nil
	}
	return nil, &SyntaxError{
		Message: fmt.Sprintf("unknown alias %q", n.alias),
		Pos:     n.pos,
	}
}

// enter counts one level of nesting at n against [WithMaxDepth]. The caller
// must call d.depth-- when it is done with n.
func (d *decoder) enter(n *node) error {
	d.depth++
	if d.opts.maxDepth > 0 && d.depth > d.opts.maxDepth {
		return &SyntaxError{Message: fmt.Sprintf("exceeded max depth %d", d.opts.maxDepth), Pos: n.pos}
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

	err := d.enter(n)
	defer func() { d.depth-- }()
	if err != nil {
		return err
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
		if n.kind == nodeScalar && n.style == scalarPlain && isNullValue(n.value) && !isNonNullTag(n.tag) {
			v.Set(reflect.Zero(v.Type()))
			return nil
		}
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return d.decode(n, v.Elem())
	}

	if v.Kind() == reflect.Interface && v.NumMethod() == 0 {
		return d.decodeIntoAny(n, v)
	}

	if d.opts.customUnmarshalers != nil {
		t := v.Type()
		if fn, ok := d.opts.customUnmarshalers[t]; ok {
			raw, err := d.rawBytes(n)
			if err != nil {
				return err
			}
			ptr := reflect.New(t)
			ptr.Elem().Set(v)
			out := reflect.ValueOf(fn).Call([]reflect.Value{ptr, reflect.ValueOf(raw)})
			if !out[0].IsNil() {
				if e, ok := reflect.TypeAssert[error](out[0]); ok {
					return bytesUnmarshalerError(e, n)
				}
			}
			v.Set(ptr.Elem())
			return nil
		}
	}

	if v.CanAddr() {
		if u, ok := reflect.TypeAssert[UnmarshalerContext](v.Addr()); ok {
			return unmarshalerError(u.UnmarshalYAML(d.ctx, d.unmarshalFunc(n)), n)
		}
		if u, ok := reflect.TypeAssert[Unmarshaler](v.Addr()); ok {
			return unmarshalerError(u.UnmarshalYAML(d.unmarshalFunc(n)), n)
		}
		if u, ok := reflect.TypeAssert[BytesUnmarshaler](v.Addr()); ok {
			raw, err := d.rawBytes(n)
			if err != nil {
				return err
			}
			return bytesUnmarshalerError(u.UnmarshalYAML(raw), n)
		}
	}

	if v.CanAddr() && v.Type() != reflect.TypeFor[time.Time]() {
		if u, ok := reflect.TypeAssert[encoding.TextUnmarshaler](v.Addr()); ok {
			if n.kind == nodeScalar {
				// Null leaves the zero value, as for any other scalar target.
				if n.style == scalarPlain && isNullValue(n.value) && n.tag == "" {
					v.Set(reflect.Zero(v.Type()))
					return nil
				}
				return unmarshalerError(u.UnmarshalText([]byte(n.value)), n)
			}
		}
	}

	if d.opts.useJSONUnmarshaler && v.CanAddr() {
		if u, ok := reflect.TypeAssert[jsonUnmarshaler](v.Addr()); ok {
			raw, err := d.rawBytes(n)
			if err != nil {
				return err
			}
			jsonBytes, err := jsonEncodeYAML(raw)
			if err != nil {
				return err
			}
			return bytesUnmarshalerError(u.UnmarshalJSON(jsonBytes), n)
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

// unmarshalFunc returns the function passed to an [Unmarshaler] decoding n.
// Its target must be a non-nil pointer, or a non-nil map.
func (d *decoder) unmarshalFunc(n *node) func(any) error {
	return func(target any) error {
		rv := reflect.ValueOf(target)
		switch {
		case rv.Kind() == reflect.Pointer && !rv.IsNil():
			return d.decode(n, rv.Elem())
		case rv.Kind() == reflect.Map && !rv.IsNil():
			return d.decode(n, rv)
		}
		return fmt.Errorf("yaml: unmarshal requires a non-nil pointer, got %T: %w", target, ErrNilPointer)
	}
}

func (d *decoder) decodeAlias(n *node, v reflect.Value) error {
	target, err := d.aliasTarget(n)
	if err != nil {
		return err
	}

	d.aliasDepth++
	if d.aliasDepth > d.opts.maxAliasExpansion {
		return &CycleError{Anchor: n.alias, Pos: n.pos}
	}
	defer func() { d.aliasDepth-- }()

	return d.decode(target, v)
}

func (d *decoder) decodeScalar(n *node, v reflect.Value) error {
	// An empty value is absent unless a tag says it is not null.
	if n.implicit && n.value == "" && !isNonNullTag(n.tag) {
		return d.fillAbsent(v, n.pos)
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

	if isCoreTag(n.tag) {
		if _, ok := resolveCoreTag(n.tag, val); !ok {
			d.addTagError(n)
			return nil
		}
	}

	if n.style == scalarPlain && isNullValue(val) && n.tag != tagStr {
		v.Set(reflect.Zero(v.Type()))
		return d.fillAbsent(v, n.pos)
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
			d.addRangeOrTypeError(n, v.Type(), err)
			return nil
		}
		if v.OverflowInt(i) {
			d.addOverflowError(n, v.Type())
			return nil
		}
		v.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := parseUint(val)
		if err != nil {
			d.addRangeOrTypeError(n, v.Type(), err)
			return nil
		}
		if v.OverflowUint(u) {
			d.addOverflowError(n, v.Type())
			return nil
		}
		v.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := parseFloat(val)
		if err != nil {
			d.addRangeOrTypeError(n, v.Type(), err)
			return nil
		}
		if v.OverflowFloat(f) {
			d.addOverflowError(n, v.Type())
			return nil
		}
		v.SetFloat(f)
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.Uint8 {
			d.addTypeError(n, v.Type())
			return nil
		}
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
	case reflect.Array:
		if v.Type().Elem().Kind() != reflect.Uint8 || n.tag != "tag:yaml.org,2002:binary" {
			d.addTypeError(n, v.Type())
			return nil
		}
		decoded, err := decodeBase64(val)
		if err != nil || len(decoded) > v.Len() {
			d.addTypeError(n, v.Type())
			return nil
		}
		reflect.Copy(v, reflect.ValueOf(decoded))
		for i := len(decoded); i < v.Len(); i++ {
			v.Index(i).SetUint(0)
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
			return d.decodeIntoAny(n, v)
		}
		return &TypeError{Errors: []string{fmt.Sprintf("cannot decode mapping into %s", v.Type())}}
	default:
		d.addTypeError(n, v.Type())
		return nil
	}
}

// checkDuplicateKeys returns a [DuplicateKeyError] for the first scalar key
// that mapping n holds twice, when [WithDisallowDuplicateKey] is set.
func (d *decoder) checkDuplicateKeys(n *node) error {
	if !d.opts.disallowDuplicates || len(n.children) < 4 {
		return nil
	}
	seen := make(map[string]bool, len(n.children)/2)
	for i := 0; i < len(n.children)-1; i += 2 {
		k := n.children[i]
		if isMergeKey(k) {
			continue
		}
		text, ok := d.keyText(k)
		if !ok {
			continue
		}
		if seen[text] {
			return &DuplicateKeyError{Key: text, Pos: k.pos}
		}
		seen[text] = true
	}
	return nil
}

// keyText returns the text of scalar key k, following an alias to its
// anchor. It reports false for a mapping or sequence key and for an alias
// with no anchor.
func (d *decoder) keyText(k *node) (string, bool) {
	if k.kind == nodeAlias {
		target, err := d.aliasTarget(k)
		if err != nil {
			return "", false
		}
		k = target
	}
	if k.kind != nodeScalar {
		return "", false
	}
	return k.value, true
}

// hasMergeKey reports whether mapping n holds a merge key.
func hasMergeKey(n *node) bool {
	for i := 0; i < len(n.children)-1; i += 2 {
		if isMergeKey(n.children[i]) {
			return true
		}
	}
	return false
}

// mergeSet records the entries a mapping has set, so that its merges never
// replace one: field and key names for a struct target, decoded keys for a
// map target. done holds the source mappings already merged in full; merging
// one again cannot set anything new, so it is skipped.
type mergeSet struct {
	names map[string]bool
	keys  map[any]bool
	done  map[*node]bool
}

// explicitNames returns the scalar keys written in mapping n itself, or nil
// when n has no merge key.
func (d *decoder) explicitNames(n *node) *mergeSet {
	if !hasMergeKey(n) {
		return nil
	}
	names := make(map[string]bool, len(n.children)/2)
	for i := 0; i < len(n.children)-1; i += 2 {
		k := n.children[i]
		if isMergeKey(k) {
			continue
		}
		if text, ok := d.keyText(k); ok {
			names[text] = true
		}
	}
	return &mergeSet{names: names}
}

// decodeMappingToMap decodes mapping n into map v. The entries written in n
// are decoded first and its merges after them, so that a merge never
// replaces a key n sets, however that key is spelled. Duplicate keys are
// those n itself writes twice; entries already in v do not count.
func (d *decoder) decodeMappingToMap(n *node, v reflect.Value) error {
	if v.IsNil() {
		v.Set(reflect.MakeMap(v.Type()))
	}
	merges := hasMergeKey(n)
	var written map[any]bool
	if merges || d.opts.disallowDuplicates {
		written = make(map[any]bool, len(n.children)/2)
	}

	sc := d.entryScratchFor(v.Type())
	for i := 0; i < len(n.children)-1; i += 2 {
		keyNode := n.children[i]
		if isMergeKey(keyNode) {
			continue
		}
		if _, err := d.decodeMapEntry(v, keyNode, n.children[i+1], written, false, &sc); err != nil {
			return err
		}
	}
	if !merges {
		return nil
	}
	filled := &mergeSet{keys: written}
	for i := 0; i < len(n.children)-1; i += 2 {
		if isMergeKey(n.children[i]) {
			if err := d.decodeMerge(n.children[i+1], v, filled); err != nil {
				return err
			}
		}
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
	if err := d.checkDuplicateKeys(n); err != nil {
		return err
	}
	filled := d.explicitNames(n)
	var inlineWritten map[any]bool
	if filled != nil || d.opts.disallowDuplicates {
		inlineWritten = make(map[any]bool)
	}

	// The entries written in n are decoded before its merges, so that a
	// merge never replaces one.
	for i := 0; i < len(n.children)-1; i += 2 {
		keyNode := n.children[i]
		valNode := n.children[i+1]

		if isMergeKey(keyNode) {
			continue
		}

		if !d.scalarKeyFitsTag(keyNode) {
			continue
		}
		keyName, _ := d.keyText(keyNode)

		idx, ok := sf.byName[keyName]
		if ok && sf.fields[idx].inline {
			ok = false
		}
		if !ok {
			if m, found := inlineMapField(v, sf); found {
				if _, err := d.decodeMapEntry(m, keyNode, valNode, inlineWritten, false, nil); err != nil {
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
		if !field.IsValid() {
			d.addEmbeddedPointerError(keyNode, v.Type(), fi.name)
			continue
		}
		if !field.CanSet() {
			continue
		}

		if err := d.decode(valNode, field); err != nil {
			return err
		}
	}

	if filled != nil {
		filled.keys = inlineWritten
		for i := 0; i < len(n.children)-1; i += 2 {
			if isMergeKey(n.children[i]) {
				if err := d.decodeMerge(n.children[i+1], v, filled); err != nil {
					return err
				}
			}
		}
	}

	// filled holds the keys written in n and those set by its merges.
	var seen map[string]bool
	if filled != nil {
		seen = filled.names
	} else {
		seen = make(map[string]bool, len(n.children)/2)
		for i := 0; i < len(n.children)-1; i += 2 {
			if text, ok := d.keyText(n.children[i]); ok {
				seen[text] = true
			}
		}
	}

	return d.finishStruct(v, sf, seen, n.pos, true)
}

// finishStruct applies defaults and required checks to struct v once the
// keys in seen have been decoded into it, and does the same for each struct
// field that was absent. validate runs the validator on v.
func (d *decoder) finishStruct(v reflect.Value, sf *structFields, seen map[string]bool, pos Position, validate bool) error {
	if d.opts.applyDefaults {
		if err := d.applyDefaults(v, sf, seen); err != nil {
			return err
		}
	}

	for _, fi := range sf.fields {
		if fi.required && !seen[fi.name] {
			return &RequiredFieldError{Field: fi.name, Pos: pos}
		}
	}

	for i := range sf.fields {
		fi := &sf.fields[i]
		if seen[fi.name] || fi.inline {
			continue
		}
		field := fieldForEncode(v, fi.index)
		if field.Kind() != reflect.Struct || !field.CanSet() || !hasFillRules(field.Type(), d.opts.applyDefaults) {
			continue
		}
		if err := d.finishStruct(field, getStructFields(field.Type()), nil, pos, false); err != nil {
			return err
		}
	}

	if validate && d.opts.validator != nil && v.CanAddr() {
		iface := v.Addr().Interface()
		if err := d.opts.validator.Struct(iface); err != nil {
			return &ValidationError{Err: err, Pos: pos}
		}
	}

	return nil
}

// fillAbsent applies defaults and required checks to struct v, whose
// mapping is absent from the input or null.
func (d *decoder) fillAbsent(v reflect.Value, pos Position) error {
	if v.Kind() != reflect.Struct || !v.CanSet() || !hasFillRules(v.Type(), d.opts.applyDefaults) {
		return nil
	}
	sf := getStructFields(v.Type())
	if len(sf.conflicts) > 0 {
		return nil
	}
	return d.finishStruct(v, sf, nil, pos, false)
}

// fillRules records whether a struct type, or a struct field of it at any
// depth (not behind a pointer), has a required field or a default.
type fillRules struct{ required, defaults bool }

var fillRulesCache sync.Map

// hasFillRules reports whether decoding struct type t must check required
// fields, or apply defaults when defaults is true, even when its mapping is
// absent.
func hasFillRules(t reflect.Type, defaults bool) bool {
	r := structFillRules(t)
	return r.required || defaults && r.defaults
}

func structFillRules(t reflect.Type) fillRules {
	if cached, ok := fillRulesCache.Load(t); ok {
		r, _ := cached.(fillRules)
		return r
	}
	var r fillRules
	collectFillRules(t, &r, map[reflect.Type]bool{})
	fillRulesCache.Store(t, r)
	return r
}

// collectFillRules merges into r the rules of t and of every struct type
// reachable from it through non-inline struct fields. Promoted fields of an
// embedded pointer can lead back to t, so each type is visited once.
func collectFillRules(t reflect.Type, r *fillRules, visited map[reflect.Type]bool) {
	if visited[t] {
		return
	}
	visited[t] = true
	sf := getStructFields(t)
	for _, fi := range sf.fields {
		r.required = r.required || fi.required
		r.defaults = r.defaults || fi.hasDefault
		ft := t.FieldByIndex(fi.index).Type
		if ft.Kind() == reflect.Struct && !fi.inline {
			collectFillRules(ft, r, visited)
		}
	}
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

// decodeMapEntry decodes one mapping entry into map m and reports whether it
// set it. written, when not nil, holds the decoded keys the mapping has set
// and gains this one. A merged entry (merge true) whose key is in written is
// skipped; a written entry whose key is in written is a [DuplicateKeyError]
// under [WithDisallowDuplicateKey]. sc, when not nil, supplies the key and
// value to decode into.
func (d *decoder) decodeMapEntry(m reflect.Value, keyNode, valNode *node, written map[any]bool, merge bool, sc *entryScratch) (bool, error) {
	key := sc.keyFor(m.Type().Key())
	typeErrs := len(d.typeErrors)
	if err := d.decode(keyNode, key); err != nil {
		return false, err
	}
	if len(d.typeErrors) > typeErrs {
		// The key could not be decoded; leave the entry out rather than
		// store it under the zero key.
		return false, nil
	}
	if !key.Comparable() {
		d.addKeyTypeError(keyNode, collectionKind(key.Interface()), m.Type())
		return false, nil
	}
	if written != nil {
		k := key.Interface()
		if written[k] {
			if merge {
				return false, nil
			}
			if d.opts.disallowDuplicates {
				text, _ := d.keyText(keyNode)
				return false, &DuplicateKeyError{Key: text, Pos: keyNode.pos}
			}
		}
		written[k] = true
	}
	val := sc.valFor(m.Type().Elem())
	if err := d.decode(valNode, val); err != nil {
		return false, err
	}
	m.SetMapIndex(key, val)
	return true, nil
}

// entryScratch holds a key and a value that the entries of one map decode
// into in turn, which is safe because SetMapIndex copies them. Each is
// reused only when its type is reusable; otherwise every entry gets a fresh
// one.
type entryScratch struct {
	reuseKey, reuseVal bool
	key, val           reflect.Value
}

// entryScratchFor returns the scratch for the entries of map type t.
func (d *decoder) entryScratchFor(t reflect.Type) entryScratch {
	return entryScratch{
		reuseKey: d.reusableEntryType(t.Key()),
		reuseVal: d.reusableEntryType(t.Elem()),
	}
}

// keyFor returns the zero key of type t to decode an entry's key into.
func (sc *entryScratch) keyFor(t reflect.Type) reflect.Value {
	if sc == nil || !sc.reuseKey {
		return reflect.New(t).Elem()
	}
	return reuseZero(&sc.key, t)
}

// valFor returns the zero value of type t to decode an entry's value into.
func (sc *entryScratch) valFor(t reflect.Type) reflect.Value {
	if sc == nil || !sc.reuseVal {
		return reflect.New(t).Elem()
	}
	return reuseZero(&sc.val, t)
}

// reuseZero zeroes and returns *slot, making it on first use.
func reuseZero(slot *reflect.Value, t reflect.Type) reflect.Value {
	if slot.IsValid() {
		slot.SetZero()
	} else {
		*slot = reflect.New(t).Elem()
	}
	return *slot
}

// reusableEntryType reports whether a map key or element of type t can be
// decoded into a reused value: a boolean, number or string type with no
// methods and no custom unmarshaler, so no code but the decoder's sees its
// address.
func (d *decoder) reusableEntryType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
	default:
		return false
	}
	if t.NumMethod() > 0 || reflect.PointerTo(t).NumMethod() > 0 {
		return false
	}
	_, custom := d.opts.customUnmarshalers[t]
	return !custom
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
		if err != nil || v.OverflowInt(i) {
			return fmt.Errorf("%w %q", errInvalidIntDefault, raw)
		}
		v.SetInt(i)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := parseUint(raw)
		if err != nil || v.OverflowUint(u) {
			return fmt.Errorf("%w %q", errInvalidUintDefault, raw)
		}
		v.SetUint(u)

	case reflect.Float32, reflect.Float64:
		f, err := parseFloat(raw)
		if err != nil || v.OverflowFloat(f) {
			return fmt.Errorf("%w %q", errInvalidFloatDefault, raw)
		}
		v.SetFloat(f)

	default:
		return fmt.Errorf("%w %s", errUnsupportedDefault, v.Type())
	}

	return nil
}

// decodeMerge merges the mapping, or sequence of mappings, n into v. Keys in
// filled are already set and are left alone; the keys it sets are added.
func (d *decoder) decodeMerge(n *node, v reflect.Value, filled *mergeSet) error {
	switch n.kind {
	case nodeAlias:
		target, err := d.aliasTarget(n)
		if err != nil {
			return err
		}
		d.aliasDepth++
		defer func() { d.aliasDepth-- }()
		if d.aliasDepth > d.opts.maxAliasExpansion {
			return &CycleError{Anchor: n.alias, Pos: n.pos}
		}
		if target.kind != nodeMapping && target.kind != nodeSequence {
			d.addMergeError(n)
			return nil
		}
		return d.decodeMerge(target, v, filled)
	case nodeSequence:
		for _, child := range n.children {
			if child.kind != nodeMapping && child.kind != nodeAlias {
				d.addMergeError(child)
				continue
			}
			if child.kind == nodeAlias {
				if target, err := d.aliasTarget(child); err == nil && target.kind != nodeMapping {
					d.addMergeError(child)
					continue
				}
			}
			if err := d.decodeMerge(child, v, filled); err != nil {
				return err
			}
		}
		return nil
	case nodeMapping:
		if filled.done[n] {
			return nil
		}
		if err := d.decodeMappingMerge(n, v, filled); err != nil {
			return err
		}
		if filled.done == nil {
			filled.done = make(map[*node]bool)
		}
		filled.done[n] = true
		return nil
	}
	d.addMergeError(n)
	return nil
}

// decodeMappingMerge merges the entries of mapping n into v: first the keys
// written in n, then those of n's own merges, so that n's keys win.
func (d *decoder) decodeMappingMerge(n *node, v reflect.Value, filled *mergeSet) error {
	var sf *structFields
	switch v.Kind() {
	case reflect.Map:
	case reflect.Struct:
		sf = getStructFields(v.Type())
	default:
		return nil
	}
	for _, merges := range []bool{false, true} {
		for i := 0; i < len(n.children)-1; i += 2 {
			keyNode := n.children[i]
			valNode := n.children[i+1]
			if isMergeKey(keyNode) != merges {
				continue
			}
			if merges {
				if err := d.decodeMerge(valNode, v, filled); err != nil {
					return err
				}
				continue
			}
			if err := d.mergeEntry(v, sf, keyNode, valNode, filled); err != nil {
				return err
			}
		}
	}
	return nil
}

// mergeEntry decodes one merged entry into map or struct v unless filled
// shows it is already set, and records it in filled when it sets it.
func (d *decoder) mergeEntry(v reflect.Value, sf *structFields, keyNode, valNode *node, filled *mergeSet) error {
	if sf == nil {
		_, err := d.decodeMapEntry(v, keyNode, valNode, filled.keys, true, nil)
		return err
	}
	// A struct target reads the key's text without decoding it, so count it
	// here, as decoding it into a map would.
	if err := d.countAliasNode(keyNode); err != nil {
		return err
	}
	if !d.scalarKeyFitsTag(keyNode) {
		return nil
	}
	name, scalar := d.keyText(keyNode)
	if scalar && filled.names[name] {
		return nil
	}
	set, err := d.mergeField(v, sf, name, keyNode, valNode, filled)
	if set && scalar {
		filled.names[name] = true
	}
	return err
}

// mergeField decodes one merged entry named name into struct v, reporting
// whether it set a value.
func (d *decoder) mergeField(v reflect.Value, sf *structFields, name string, keyNode, valNode *node, filled *mergeSet) (bool, error) {
	idx, ok := sf.byName[name]
	if !ok || sf.fields[idx].inline {
		if m, found := inlineMapField(v, sf); found {
			return d.decodeMapEntry(m, keyNode, valNode, filled.keys, true, nil)
		}
		return false, nil
	}
	field := fieldByIndex(v, sf.fields[idx].index)
	if !field.IsValid() {
		d.addEmbeddedPointerError(keyNode, v.Type(), sf.fields[idx].name)
		return false, nil
	}
	if !field.CanSet() {
		return false, nil
	}
	return true, d.decode(valNode, field)
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
		// As in encoding/json, items beyond the array's length are dropped
		// and elements beyond the sequence's are zeroed.
		for i := range v.Len() {
			if i >= len(n.children) {
				v.Index(i).Set(reflect.Zero(v.Type().Elem()))
				continue
			}
			if err := d.decode(n.children[i], v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Interface:
		if v.NumMethod() == 0 {
			return d.decodeIntoAny(n, v)
		}
		return &TypeError{Errors: []string{fmt.Sprintf("cannot decode sequence into %s", v.Type())}}
	default:
		d.addTypeError(n, v.Type())
	}
	return nil
}

// decodeIntoAny decodes n, whose nesting level decode has already counted,
// into the empty interface v.
func (d *decoder) decodeIntoAny(n *node, v reflect.Value) error {
	d.depth--
	result, err := d.decodeToAny(n)
	d.depth++
	if err != nil {
		return err
	}
	if result != nil {
		v.Set(reflect.ValueOf(result))
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

	err := d.enter(n)
	defer func() { d.depth-- }()
	if err != nil {
		return nil, err
	}

	if n.kind == nodeAlias {
		target, err := d.aliasTarget(n)
		if err != nil {
			return nil, err
		}
		d.aliasDepth++
		if d.aliasDepth > d.opts.maxAliasExpansion {
			return nil, &CycleError{Anchor: n.alias, Pos: n.pos}
		}
		defer func() { d.aliasDepth-- }()
		return d.decodeToAny(target)
	}

	mergeSource := d.mergeSource
	d.mergeSource = false

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
		if !mergeSource {
			defer d.mergeScope()()
			return d.decodeMappingToAny(n, false)
		}
		return d.decodeMergeSourceToAny(n, func() (any, error) { return d.decodeMappingToAny(n, true) })

	case nodeSequence:
		s := make([]any, 0, len(n.children))
		for _, child := range n.children {
			d.mergeSource = mergeSource
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

// decodeMappingToAny decodes mapping n into a map[string]any, or a MapSlice
// under [WithOrderedMap]. A merge source keeps the first of a repeated key.
func (d *decoder) decodeMappingToAny(n *node, mergeSource bool) (any, error) {
	if err := d.checkDuplicateKeys(n); err != nil {
		return nil, err
	}
	if d.opts.useOrderedMap {
		return d.decodeMappingToOrderedMap(n)
	}
	m := make(map[string]any, len(n.children)/2)
	var written map[string]bool
	if mergeSource {
		written = make(map[string]bool, len(n.children)/2)
	}
	for i := 0; i < len(n.children)-1; i += 2 {
		keyNode := n.children[i]
		valNode := n.children[i+1]

		if isMergeKey(keyNode) {
			d.mergeSource = true
			merged, err := d.decodeToAny(valNode)
			d.mergeSource = false
			if err != nil {
				return nil, err
			}
			sources, ok := mergeSources[map[string]any](merged)
			if !ok {
				d.addMergeError(valNode)
			}
			for _, mm := range sources {
				for k, v := range mm {
					if _, exists := m[k]; !exists {
						m[k] = v
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
			if !d.keyFitsTag(keyNode) {
				continue
			}
			keyStr = keyNode.value
		case nodeAlias:
			target, err := d.aliasTarget(keyNode)
			if err != nil {
				return nil, err
			}
			if target.kind != nodeScalar {
				d.addKeyTypeError(keyNode, target.kind, reflect.TypeFor[map[string]any]())
				continue
			}
			if !d.keyFitsTag(target) {
				continue
			}
			keyStr = target.value
		default:
			d.addKeyTypeError(keyNode, keyNode.kind, reflect.TypeFor[map[string]any]())
			continue
		}

		if written != nil {
			if written[keyStr] {
				continue
			}
			written[keyStr] = true
		}
		val, err := d.decodeToAny(valNode)
		if err != nil {
			return nil, err
		}
		m[keyStr] = val
	}
	return m, nil
}

// mergeSources returns the mappings of type M that a decoded merge value
// holds: the value itself, or the items of a sequence of them. It reports
// false when the value is anything else.
func mergeSources[M map[string]any | MapSlice](v any) ([]M, bool) {
	if m, ok := v.(M); ok {
		return []M{m}, true
	}
	seq, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]M, 0, len(seq))
	for _, item := range seq {
		m, ok := item.(M)
		if !ok {
			return out, false
		}
		out = append(out, m)
	}
	return out, true
}

// decodeIntoMapSlice decodes mapping n into the MapSlice v, decoding nested
// mappings as MapSlice too, as [WithOrderedMap] does.
func (d *decoder) decodeIntoMapSlice(n *node, v reflect.Value) error {
	prev := d.opts.useOrderedMap
	d.opts.useOrderedMap = true
	defer func() { d.opts.useOrderedMap = prev }()
	defer d.mergeScope()()
	if err := d.checkDuplicateKeys(n); err != nil {
		return err
	}
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
			d.mergeSource = true
			src, err := d.decodeToAny(valNode)
			d.mergeSource = false
			if err != nil {
				return nil, err
			}
			sources, ok := mergeSources[MapSlice](src)
			if !ok {
				d.addMergeError(valNode)
			}
			for _, m := range sources {
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
		// its decoded value; an alias to a scalar key uses that key's text.
		var key any
		switch keyNode.kind {
		case nodeScalar:
			if !d.keyFitsTag(keyNode) {
				continue
			}
			key = keyNode.value
		case nodeAlias:
			target, err := d.aliasTarget(keyNode)
			if err != nil {
				return nil, err
			}
			if target.kind == nodeScalar {
				if !d.keyFitsTag(target) {
					continue
				}
				key = target.value
				break
			}
			resolved, err := d.decodeToAny(keyNode)
			if err != nil {
				return nil, err
			}
			key = resolved
		default:
			resolved, err := d.decodeToAny(keyNode)
			if err != nil {
				return nil, err
			}
			key = resolved
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

	if isCoreTag(n.tag) {
		if v, ok := resolveCoreTag(n.tag, val); ok {
			return v
		}
		d.addTagError(n)
		return nil
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

// isCoreTag reports whether tag is !!null, !!bool, !!int or !!float.
func isCoreTag(tag string) bool {
	return tag == tagNull || tag == tagBool || tag == tagInt || tag == tagFloat
}

// resolveCoreTag returns the value of a scalar explicitly tagged !!null,
// !!bool, !!int or !!float: an !!int is an int64, or a uint64 above the
// int64 range, and a number beyond those ranges or the float64 range is a
// [Number]. It reports false for other tags and for a value that does not
// fit its tag.
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
		i, err := parseInt(val)
		if err == nil {
			return i, true
		}
		if u, uerr := parseUint(val); uerr == nil {
			return u, true
		}
		if errors.Is(err, strconv.ErrRange) {
			if n, ok := bigIntNumber(val); ok {
				return n, true
			}
		}
	case tagFloat:
		f, err := parseFloat(val)
		if err == nil {
			return f, true
		}
		if errors.Is(err, strconv.ErrRange) && isDecimalFloat(val) {
			return Number(val), true
		}
	}
	return nil, false
}

// bigIntNumber returns the integer s, which parseInt rejects as out of
// range, as a [Number]: its own text when that is a Core schema integer,
// otherwise in decimal. It reports false when s is not an integer at all,
// as parseInt can report the range before it reaches a bad character.
func bigIntNumber(s string) (Number, bool) {
	if coreIntSyntax(s) {
		return Number(s), true
	}
	digits, base, err := intDigits(s)
	if err != nil {
		return "", false
	}
	z, ok := new(big.Int).SetString(digits, base)
	if !ok {
		return "", false
	}
	return Number(z.String()), true
}

// isNonNullTag reports whether tag is !!bool, !!int, !!float or !!str, under
// which a null spelling is not null.
func isNonNullTag(tag string) bool {
	return tag == tagBool || tag == tagInt || tag == tagFloat || tag == tagStr
}

// scalarKeyFitsTag reports whether key k, or the scalar an alias key names,
// fits its core tag, recording a type error when it does not. A mapping or
// sequence key, or an alias with no anchor, is left to the caller.
func (d *decoder) scalarKeyFitsTag(k *node) bool {
	if k.kind == nodeAlias {
		target, err := d.aliasTarget(k)
		if err != nil {
			return true
		}
		k = target
	}
	return k.kind != nodeScalar || d.keyFitsTag(k)
}

// keyFitsTag reports whether scalar key k fits its !!null, !!bool, !!int or
// !!float tag, if it has one, recording a type error when it does not. A key
// decoded into any keeps its text, but is checked as its value would be.
func (d *decoder) keyFitsTag(k *node) bool {
	if !isCoreTag(k.tag) {
		return true
	}
	if _, ok := resolveCoreTag(k.tag, k.value); !ok {
		d.addTagError(k)
		return false
	}
	return true
}

// addTagError records that scalar n carries a !!null, !!bool, !!int or
// !!float tag that its value does not fit.
func (d *decoder) addTagError(n *node) {
	msg := fmt.Sprintf("line %d, column %d: cannot unmarshal %q as %s", n.pos.Line, n.pos.Column, n.value, d.nodeTagName(n))
	d.typeErrors = append(d.typeErrors, msg)
}

func scalarToAnyCore(val string) any {
	c := resolveCore(val)
	switch c.kind {
	case coreNull:
		return nil
	case coreBool:
		return c.b
	case coreInt:
		return c.i
	case coreUint:
		return c.u
	case coreFloat:
		return c.f
	case coreNumber:
		return Number(val)
	}
	return val
}

// coreKind is the type a plain scalar resolves to under the Core schema.
type coreKind uint8

const (
	coreStr coreKind = iota
	coreNull
	coreBool
	coreInt  // fits int64
	coreUint // fits uint64 but not int64
	coreFloat
	coreNumber // an int or float beyond those ranges
)

// coreValue is a plain scalar resolved under the Core schema: its kind and,
// for a bool or number, its value.
type coreValue struct {
	kind coreKind
	b    bool
	i    int64
	u    uint64
	f    float64
}

// resolveCore resolves plain scalar s under the Core schema. It allocates
// nothing for a string that cannot be a number.
func resolveCore(s string) coreValue {
	if isNullValue(s) {
		return coreValue{kind: coreNull}
	}
	if b, ok := parseCoreBool(s); ok {
		return coreValue{kind: coreBool, b: b}
	}
	// Every Core int and float starts with a digit, a sign or a dot.
	if c := s[0]; c != '.' && c != '+' && c != '-' && (c < '0' || c > '9') {
		return coreValue{}
	}
	if coreIntSyntax(s) {
		if i, ok := parseCoreInt(s); ok {
			return coreValue{kind: coreInt, i: i}
		}
		if u, ok := parseCoreUint(s); ok {
			return coreValue{kind: coreUint, u: u}
		}
		return coreValue{kind: coreNumber}
	}
	if f, ok := parseCoreFloat(s); ok {
		return coreValue{kind: coreFloat, f: f}
	}
	if isDecimalFloat(s) {
		return coreValue{kind: coreNumber}
	}
	return coreValue{}
}

// coreIntSyntax reports whether s has the form parseCoreInt or
// parseCoreUint accept, apart from range: an optionally signed decimal, or
// digits after a lowercase 0x or 0o prefix.
func coreIntSyntax(s string) bool {
	digits, isDigit := s, isDecDigit
	switch {
	case strings.HasPrefix(s, "0x"):
		digits, isDigit = s[2:], isHexDigit
	case strings.HasPrefix(s, "0o"):
		digits, isDigit = s[2:], isOctDigit
	case s[0] == '+' || s[0] == '-':
		digits = s[1:]
	}
	if digits == "" {
		return false
	}
	for i := range len(digits) {
		if !isDigit(digits[i]) {
			return false
		}
	}
	return true
}

func isDecDigit(c byte) bool { return c >= '0' && c <= '9' }
func isOctDigit(c byte) bool { return c >= '0' && c <= '7' }
func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// parseCoreBool returns the value of a Core schema boolean spelling.
func parseCoreBool(s string) (value, ok bool) {
	switch s {
	case trueLiteral, "True", "TRUE":
		return true, true
	case falseLiteral, "False", "FALSE":
		return false, true
	}
	return false, false
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
	i, err := strconv.ParseInt(val, 10, 64)
	if err == nil {
		return i
	}
	if errors.Is(err, strconv.ErrRange) {
		// As under the Core schema: uint64 above the int64 range, and a
		// Number beyond it.
		if u, err := strconv.ParseUint(val, 10, 64); err == nil {
			return u
		}
		return Number(val)
	}
	if isDecimalFloat(val) {
		f, err := strconv.ParseFloat(val, 64)
		if err == nil {
			return f
		}
		if errors.Is(err, strconv.ErrRange) {
			return Number(val)
		}
	}

	return val
}

func (d *decoder) addTypeError(n *node, t reflect.Type) {
	msg := fmt.Sprintf("line %d, column %d: cannot unmarshal %s into Go value of type %s", n.pos.Line, n.pos.Column, d.nodeTagName(n), t)
	d.typeErrors = append(d.typeErrors, msg)
}

// addOverflowError records that scalar n is out of range for type t.
func (d *decoder) addOverflowError(n *node, t reflect.Type) {
	msg := fmt.Sprintf("line %d, column %d: value %q overflows Go value of type %s", n.pos.Line, n.pos.Column, n.value, t)
	d.typeErrors = append(d.typeErrors, msg)
}

// addRangeOrTypeError records why scalar n could not be parsed for type t:
// out of range when err is [strconv.ErrRange], otherwise the wrong type.
func (d *decoder) addRangeOrTypeError(n *node, t reflect.Type, err error) {
	if errors.Is(err, strconv.ErrRange) {
		d.addOverflowError(n, t)
		return
	}
	d.addTypeError(n, t)
}

// addMergeError records that the merge value n is not a mapping or a
// sequence of mappings.
func (d *decoder) addMergeError(n *node) {
	msg := fmt.Sprintf("line %d, column %d: merge value must be a mapping or a sequence of mappings", n.pos.Line, n.pos.Column)
	d.typeErrors = append(d.typeErrors, msg)
}

// addEmbeddedPointerError records that field name of struct type t is
// behind a nil pointer to an unexported embedded struct, which cannot be
// allocated.
func (d *decoder) addEmbeddedPointerError(n *node, t reflect.Type, name string) {
	msg := fmt.Sprintf("line %d, column %d: cannot set field %q of %s: it is behind a nil pointer to an unexported embedded struct", n.pos.Line, n.pos.Column, name, t)
	d.typeErrors = append(d.typeErrors, msg)
}

// nodeTagName returns the tag n resolves to, in short form, for messages.
func (d *decoder) nodeTagName(n *node) string {
	if n.tag != "" {
		if rest, ok := strings.CutPrefix(n.tag, "tag:yaml.org,2002:"); ok {
			return "!!" + rest
		}
		return n.tag
	}
	if n.kind != nodeScalar {
		return nodeKindName(n.kind)
	}
	if n.style != scalarPlain || d.opts.schema == FailsafeSchema {
		return "!!str"
	}
	var v any
	if d.opts.schema == JSONSchema {
		v = scalarToAnyJSON(n.value)
	} else {
		v = scalarToAnyCore(n.value)
	}
	switch v.(type) {
	case nil:
		return "!!null"
	case bool:
		return "!!bool"
	case int64, uint64:
		return "!!int"
	case float64:
		return "!!float"
	case Number:
		if coreIntSyntax(n.value) {
			return "!!int"
		}
		return "!!float"
	}
	return "!!str"
}

// unmarshalerError wraps err, returned by a custom unmarshaler decoding n,
// in an [*UnmarshalerError] that gives n's position. It returns nil for a
// nil err, and err itself when it already carries a position.
func unmarshalerError(err error, n *node) error {
	if err == nil {
		return nil
	}
	if _, ok := errorPosition(err); ok {
		return err
	}
	return &UnmarshalerError{Err: err, Pos: n.pos}
}

// bytesUnmarshalerError is unmarshalerError for an unmarshaler given n
// re-encoded as bytes. Positions in its error refer to those bytes, not to
// the source, so err is always wrapped with n's position.
func bytesUnmarshalerError(err error, n *node) error {
	if err == nil {
		return nil
	}
	return &UnmarshalerError{Err: err, Pos: n.pos}
}

// addKeyTypeError records that the mapping or sequence key at n cannot be a
// key of a map of type mapType.
func (d *decoder) addKeyTypeError(n *node, kind nodeKind, mapType reflect.Type) {
	msg := fmt.Sprintf("line %d, column %d: cannot use %s as a key of %s", n.pos.Line, n.pos.Column, nodeKindName(kind), mapType)
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
	if b, ok := parseCoreBool(s); ok {
		return b, nil
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

// fieldByIndex returns the field of struct v at index, allocating embedded
// struct pointers on the way. It returns the zero Value when such a pointer
// is nil and cannot be set, as for an unexported embedded struct.
func fieldByIndex(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				if !v.CanSet() {
					return reflect.Value{}
				}
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

// rawBytes returns n re-encoded for an unmarshaler that takes bytes. Aliases
// are replaced by their anchors' content and anchors are left out, so the
// bytes parse on their own and hold only the value.
func (d *decoder) rawBytes(n *node) ([]byte, error) {
	if !hasAnchorOrAlias(n) {
		return marshalNode(n)
	}
	expanded, err := d.expandAliases(n)
	if err != nil {
		return nil, err
	}
	return marshalNode(expanded)
}

func hasAnchorOrAlias(n *node) bool {
	return n.anchor != "" || n.kind == nodeAlias || slices.ContainsFunc(n.children, hasAnchorOrAlias)
}

// expandAliases returns a copy of n with every alias replaced by a copy of
// its anchor's node and no anchors. Expansion counts against the same limits
// as decoding.
func (d *decoder) expandAliases(n *node) (*node, error) {
	if n.kind == nodeAlias {
		target, err := d.aliasTarget(n)
		if err != nil {
			return nil, err
		}
		d.aliasDepth++
		defer func() { d.aliasDepth-- }()
		if d.aliasDepth > d.opts.maxAliasExpansion {
			return nil, &CycleError{Anchor: n.alias, Pos: n.pos}
		}
		return d.expandAliases(target)
	}
	if err := d.countAliasNode(n); err != nil {
		return nil, err
	}
	c := *n
	c.anchor = ""
	if len(n.children) > 0 {
		c.children = make([]*node, len(n.children))
		for i, child := range n.children {
			e, err := d.expandAliases(child)
			if err != nil {
				return nil, err
			}
			if n.kind == nodeMapping && i%2 == 0 && child.kind == nodeAlias && isMergeKey(e) {
				// An alias key is the anchor's text, never a merge key, so
				// the copy must not read back as one.
				e = &node{kind: nodeScalar, value: "<<", style: scalarDoubleQuoted, pos: e.pos}
			}
			c.children[i] = e
		}
	}
	return &c, nil
}

// Unmarshaler is implemented by types that can decode themselves from YAML.
// The unmarshal function decodes the underlying YAML value into any Go
// type; its argument must be a non-nil pointer (or a non-nil map), or it
// returns an error wrapping [ErrNilPointer]. An error UnmarshalYAML returns
// is wrapped in an [*UnmarshalerError] giving the value's position.
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
