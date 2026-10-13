package yaml

import (
	"reflect"
	"strings"
	"sync"
)

type fieldInfo struct {
	name         string
	index        []int
	omitEmpty    bool
	flow         bool
	inline       bool
	required     bool
	skip         bool
	tagged       bool // the name comes from a struct tag
	inlineMap    bool // an inline map field, holding the keys of no other field
	defaultValue string
	hasDefault   bool
}

func parseTag(tag string) fieldInfo {
	var fi fieldInfo
	if tag == "" {
		return fi
	}

	parts := strings.Split(tag, ",")
	fi.name = parts[0]
	if fi.name == "-" {
		fi.skip = true
		return fi
	}

	for _, opt := range parts[1:] {
		switch opt {
		case "omitempty":
			fi.omitEmpty = true
		case "flow":
			fi.flow = true
		case "inline":
			fi.inline = true
		case "required":
			fi.required = true
		default:
			if v, ok := strings.CutPrefix(opt, "default="); ok {
				fi.defaultValue = v
				fi.hasDefault = true
			}
		}
	}
	return fi
}

type structFields struct {
	fields    []fieldInfo
	byName    map[string]int
	conflicts []string
}

var structFieldCache sync.Map

func getStructFields(t reflect.Type) *structFields {
	if cached, ok := structFieldCache.Load(t); ok {
		sf, _ := cached.(*structFields)
		return sf
	}
	sf := buildStructFields(t, yamlFieldTag, strings.ToLower)
	structFieldCache.Store(t, sf)
	return sf
}

// yamlFieldTag returns the field options from f's yaml tag, or its json
// tag when it has no yaml tag.
func yamlFieldTag(f reflect.StructField) fieldInfo {
	tag := f.Tag.Get("yaml")
	if tag == "" {
		tag = f.Tag.Get("json")
	}
	return parseTag(tag)
}

// buildStructFields lists the fields of struct type t to encode and decode,
// following encoding/json's rules: the fields of embedded structs are
// promoted, and of several fields with one name the shallowest wins, then
// the one named by a tag. Fields still tied are left out and their name
// recorded as a conflict. tagOf reads a field's tag options, and
// defaultName gives the name of a field that has none.
func buildStructFields(t reflect.Type, tagOf func(reflect.StructField) fieldInfo, defaultName func(string) string) *structFields {
	var cands []fieldInfo
	collectFields(t, nil, map[reflect.Type]bool{t: true}, tagOf, defaultName, &cands)

	byName := make(map[string][]int)
	for i, fi := range cands {
		if !isInlineMap(fi) {
			byName[fi.name] = append(byName[fi.name], i)
		}
	}
	winner := make(map[string]int, len(byName))
	sf := &structFields{byName: make(map[string]int)}
	for i, fi := range cands {
		if isInlineMap(fi) {
			continue
		}
		group := byName[fi.name]
		if group[0] != i {
			continue
		}
		if w, ok := dominantField(cands, group); ok {
			winner[fi.name] = w
		} else {
			sf.conflicts = append(sf.conflicts, fi.name)
		}
	}
	for i, fi := range cands {
		switch {
		case isInlineMap(fi):
			sf.fields = append(sf.fields, fi)
			if _, named := winner[fi.name]; !named {
				sf.byName[fi.name] = len(sf.fields) - 1
			}
		case byName[fi.name][0] == i:
			w, ok := winner[fi.name]
			if !ok {
				continue
			}
			sf.fields = append(sf.fields, cands[w])
			sf.byName[fi.name] = len(sf.fields) - 1
		}
	}
	return sf
}

// isInlineMap reports whether fi is an inline map field.
func isInlineMap(fi fieldInfo) bool {
	return fi.inlineMap
}

// dominantField returns the field of group, indexes into cands of fields
// with one name, that wins: the only shallowest one, or the only tagged one
// among the shallowest.
func dominantField(cands []fieldInfo, group []int) (int, bool) {
	depth := len(cands[group[0]].index)
	for _, i := range group[1:] {
		depth = min(depth, len(cands[i].index))
	}
	best, tagged, count := -1, 0, 0
	for _, i := range group {
		if len(cands[i].index) != depth {
			continue
		}
		count++
		if cands[i].tagged {
			tagged++
			best = i
		}
		if best < 0 {
			best = i
		}
	}
	switch {
	case count == 1:
		return best, true
	case tagged == 1:
		return best, true
	}
	return 0, false
}

// collectFields appends the fields of struct type t, reached through
// index, to cands. onPath holds the struct types embedded on the way, so a
// type embedding itself is not walked again.
func collectFields(t reflect.Type, index []int, onPath map[reflect.Type]bool, tagOf func(reflect.StructField) fieldInfo, defaultName func(string) string, cands *[]fieldInfo) {
	for i := range t.NumField() {
		f := t.Field(i)
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if !f.IsExported() && (!f.Anonymous || ft.Kind() != reflect.Struct) {
			// An unexported embedded field is used only to promote the
			// fields of a struct.
			continue
		}

		fi := tagOf(f)
		if fi.skip {
			continue
		}
		fi.tagged = fi.name != ""

		fi.index = make([]int, len(index)+1)
		copy(fi.index, index)
		fi.index[len(index)] = i

		if f.Anonymous && fi.name == "" || fi.inline {
			if ft.Kind() == reflect.Struct {
				if !onPath[ft] {
					onPath[ft] = true
					collectFields(ft, fi.index, onPath, tagOf, defaultName, cands)
					delete(onPath, ft)
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if fi.inline && f.Type.Kind() == reflect.Map {
			fi.inlineMap = true
			*cands = append(*cands, fi)
			continue
		}

		if fi.name == "" {
			fi.name = defaultName(f.Name)
		}
		*cands = append(*cands, fi)
	}
}
