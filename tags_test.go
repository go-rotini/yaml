package yaml

import (
	"reflect"
	"sync"
	"testing"
)

func TestParseTagEmpty(t *testing.T) {
	fi := parseTag("")
	if fi.name != "" || fi.omitEmpty || fi.flow || fi.inline || fi.required || fi.skip {
		t.Errorf("empty tag should produce zero fieldInfo, got %+v", fi)
	}
}

func TestParseTagNameOnly(t *testing.T) {
	fi := parseTag("myfield")
	if fi.name != "myfield" {
		t.Errorf("expected name=myfield, got %q", fi.name)
	}
	if fi.omitEmpty || fi.flow || fi.inline || fi.required || fi.skip {
		t.Error("no options should be set")
	}
}

func TestParseTagDash(t *testing.T) {
	fi := parseTag("-")
	if !fi.skip {
		t.Error("expected skip=true for dash tag")
	}
}

func TestParseTagOmitempty(t *testing.T) {
	fi := parseTag("name,omitempty")
	if fi.name != "name" {
		t.Errorf("expected name=name, got %q", fi.name)
	}
	if !fi.omitEmpty {
		t.Error("expected omitEmpty=true")
	}
}

func TestParseTagFlow(t *testing.T) {
	fi := parseTag("items,flow")
	if !fi.flow {
		t.Error("expected flow=true")
	}
}

func TestParseTagInline(t *testing.T) {
	fi := parseTag(",inline")
	if !fi.inline {
		t.Error("expected inline=true")
	}
	if fi.name != "" {
		t.Errorf("expected empty name for inline, got %q", fi.name)
	}
}

func TestParseTagRequired(t *testing.T) {
	fi := parseTag("field,required")
	if !fi.required {
		t.Error("expected required=true")
	}
}

func TestParseTagAnchor(t *testing.T) {
	fi := parseTag("name,anchor=foo")
	if fi.anchor != "foo" {
		t.Errorf("expected anchor=foo, got %q", fi.anchor)
	}
}

func TestParseTagAlias(t *testing.T) {
	fi := parseTag("ref,alias=bar")
	if fi.alias != "bar" {
		t.Errorf("expected alias=bar, got %q", fi.alias)
	}
}

func TestParseTagMultipleOptions(t *testing.T) {
	fi := parseTag("field,omitempty,flow,required")
	if fi.name != "field" {
		t.Errorf("expected name=field, got %q", fi.name)
	}
	if !fi.omitEmpty {
		t.Error("expected omitEmpty=true")
	}
	if !fi.flow {
		t.Error("expected flow=true")
	}
	if !fi.required {
		t.Error("expected required=true")
	}
}

func TestParseTagUnknownOption(t *testing.T) {
	fi := parseTag("name,unknown")
	if fi.name != "name" {
		t.Errorf("expected name=name, got %q", fi.name)
	}
	if fi.omitEmpty || fi.flow || fi.inline || fi.required {
		t.Error("unknown option should not set any flags")
	}
}

func TestParseTagDashIgnoresOptions(t *testing.T) {
	fi := parseTag("-,omitempty")
	if !fi.skip {
		t.Error("expected skip=true")
	}
	if fi.omitEmpty {
		t.Error("dash should short-circuit, omitempty should not be parsed")
	}
}

func TestGetStructFieldsSimple(t *testing.T) {
	type S struct {
		Name string `yaml:"name"`
		Age  int    `yaml:"age,omitempty"`
	}
	sf := getStructFields(reflect.TypeOf(S{}))
	if len(sf.fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(sf.fields))
	}
	if sf.fields[0].name != "name" {
		t.Errorf("expected field 0 name=name, got %q", sf.fields[0].name)
	}
	if sf.fields[1].name != "age" {
		t.Errorf("expected field 1 name=age, got %q", sf.fields[1].name)
	}
	if !sf.fields[1].omitEmpty {
		t.Error("expected omitEmpty on age field")
	}
}

func TestGetStructFieldsSkip(t *testing.T) {
	type S struct {
		Name    string `yaml:"name"`
		Ignored string `yaml:"-"`
	}
	sf := getStructFields(reflect.TypeOf(S{}))
	if len(sf.fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(sf.fields))
	}
	if sf.fields[0].name != "name" {
		t.Errorf("expected name, got %q", sf.fields[0].name)
	}
}

func TestGetStructFieldsUnexported(t *testing.T) {
	type S struct {
		Name     string `yaml:"name"`
		internal string
	}
	sf := getStructFields(reflect.TypeOf(S{}))
	if len(sf.fields) != 1 {
		t.Fatalf("expected 1 field (unexported skipped), got %d", len(sf.fields))
	}
	_ = S{}.internal
}

func TestGetStructFieldsNoTag(t *testing.T) {
	type S struct {
		Name string
		Age  int
	}
	sf := getStructFields(reflect.TypeOf(S{}))
	if len(sf.fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(sf.fields))
	}
	if sf.fields[0].name != "name" {
		t.Errorf("expected lowercase name, got %q", sf.fields[0].name)
	}
	if sf.fields[1].name != "age" {
		t.Errorf("expected lowercase age, got %q", sf.fields[1].name)
	}
}

func TestGetStructFieldsJSONFallback(t *testing.T) {
	type S struct {
		Name string `json:"json_name"`
	}
	sf := getStructFields(reflect.TypeOf(S{}))
	if len(sf.fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(sf.fields))
	}
	if sf.fields[0].name != "json_name" {
		t.Errorf("expected json_name from json tag fallback, got %q", sf.fields[0].name)
	}
}

func TestGetStructFieldsEmbeddedStruct(t *testing.T) {
	type Base struct {
		ID int `yaml:"id"`
	}
	type Outer struct {
		Base
		Name string `yaml:"name"`
	}
	sf := getStructFields(reflect.TypeOf(Outer{}))
	names := make(map[string]bool)
	for _, f := range sf.fields {
		names[f.name] = true
	}
	if !names["id"] {
		t.Error("expected embedded id field")
	}
	if !names["name"] {
		t.Error("expected name field")
	}
}

func TestGetStructFieldsEmbeddedPointer(t *testing.T) {
	type Base struct {
		ID int `yaml:"id"`
	}
	type Outer struct {
		*Base
		Name string `yaml:"name"`
	}
	sf := getStructFields(reflect.TypeOf(Outer{}))
	names := make(map[string]bool)
	for _, f := range sf.fields {
		names[f.name] = true
	}
	if !names["id"] {
		t.Error("expected embedded pointer id field")
	}
}

func TestGetStructFieldsInlineStruct(t *testing.T) {
	type Inner struct {
		X int `yaml:"x"`
	}
	type Outer struct {
		Inner Inner  `yaml:",inline"`
		Name  string `yaml:"name"`
	}
	sf := getStructFields(reflect.TypeOf(Outer{}))
	names := make(map[string]bool)
	for _, f := range sf.fields {
		names[f.name] = true
	}
	if !names["x"] {
		t.Error("expected inlined x field")
	}
}

func TestGetStructFieldsInlineMap(t *testing.T) {
	type S struct {
		Name  string            `yaml:"name"`
		Extra map[string]string `yaml:",inline"`
	}
	sf := getStructFields(reflect.TypeOf(S{}))
	if len(sf.fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(sf.fields))
	}
}

func TestGetStructFieldsInlinePointerStruct(t *testing.T) {
	type Inner struct {
		X int `yaml:"x"`
	}
	type Outer struct {
		Inner *Inner `yaml:",inline"`
	}
	sf := getStructFields(reflect.TypeOf(Outer{}))
	names := make(map[string]bool)
	for _, f := range sf.fields {
		names[f.name] = true
	}
	if !names["x"] {
		t.Error("expected inlined pointer struct x field")
	}
}

func TestGetStructFieldsConflictingNames(t *testing.T) {
	type Base struct {
		Name string `yaml:"name"`
	}
	type Outer struct {
		Base `yaml:",inline"`
		Name string `yaml:"name"`
	}
	sf := getStructFields(reflect.TypeOf(Outer{}))
	count := 0
	for _, f := range sf.fields {
		if f.name == "name" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 name field after conflict resolution, got %d", count)
	}
}

func TestGetStructFieldsCaching(t *testing.T) {
	type S struct {
		Name string `yaml:"name"`
	}
	sf1 := getStructFields(reflect.TypeOf(S{}))
	sf2 := getStructFields(reflect.TypeOf(S{}))
	if sf1 != sf2 {
		t.Error("expected cached result to be same pointer")
	}
}

func TestGetStructFieldsConcurrent(t *testing.T) {
	structFieldCache = sync.Map{}
	type S struct {
		A string `yaml:"a"`
		B int    `yaml:"b"`
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sf := getStructFields(reflect.TypeOf(S{}))
			if len(sf.fields) != 2 {
				t.Errorf("expected 2 fields, got %d", len(sf.fields))
			}
		}()
	}
	wg.Wait()
}

func TestCollectFieldsByNameIndex(t *testing.T) {
	type S struct {
		First  string `yaml:"first"`
		Second int    `yaml:"second"`
	}
	sf := getStructFields(reflect.TypeOf(S{}))
	idx, ok := sf.byName["first"]
	if !ok {
		t.Fatal("expected first in byName")
	}
	if sf.fields[idx].name != "first" {
		t.Errorf("expected first, got %q", sf.fields[idx].name)
	}
}

func TestTagParsingAnchorAlias(t *testing.T) {
	fi := parseTag("name,anchor=foo")
	if fi.anchor != "foo" {
		t.Errorf("expected anchor=foo, got %q", fi.anchor)
	}
	fi2 := parseTag("ref,alias=bar")
	if fi2.alias != "bar" {
		t.Errorf("expected alias=bar, got %q", fi2.alias)
	}
}
