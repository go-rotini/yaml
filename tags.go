package yaml

import (
	"reflect"
	"strings"
	"sync"
)

type fieldInfo struct {
	name      string
	index     []int
	omitEmpty bool
	flow      bool
	inline    bool
	required  bool
	anchor    string
	alias     string
	skip      bool
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
			if v, ok := strings.CutPrefix(opt, "anchor="); ok {
				fi.anchor = v
			} else if v, ok := strings.CutPrefix(opt, "alias="); ok {
				fi.alias = v
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
		return cached.(*structFields)
	}
	sf := &structFields{
		byName: make(map[string]int),
	}
	collectFields(t, nil, sf)
	structFieldCache.Store(t, sf)
	return sf
}

func collectFields(t reflect.Type, index []int, sf *structFields) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() && !f.Anonymous {
			continue
		}

		tag := f.Tag.Get("yaml")
		if tag == "" {
			tag = f.Tag.Get("json")
		}

		fi := parseTag(tag)
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
				collectFields(ft, fi.index, sf)
				continue
			}
		}

		if fi.inline {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectFields(ft, fi.index, sf)
				continue
			}
		}
		if fi.inline && f.Type.Kind() == reflect.Map {
			sf.fields = append(sf.fields, fi)
			sf.byName[fi.name] = len(sf.fields) - 1
			continue
		}

		if fi.name == "" {
			fi.name = strings.ToLower(f.Name)
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
