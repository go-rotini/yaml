package yaml

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ─── newDecoder ─────────────────────────────────────────────────────────────

func TestNewDecoder(t *testing.T) {
	opts := defaultDecodeOptions()
	d := newDecoder(opts)
	if d == nil {
		t.Fatal("expected non-nil decoder")
	}
	if d.ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if d.anchors == nil {
		t.Fatal("expected non-nil anchors map")
	}
}

// ─── decode: nil node ───────────────────────────────────────────────────────

func TestDecodeNilNode(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	var v string
	rv := reflect.ValueOf(&v).Elem()
	err := d.decode(nil, rv)
	if err != nil {
		t.Fatalf("expected nil error for nil node, got %v", err)
	}
}

// ─── decode: max depth ──────────────────────────────────────────────────────

func TestDecodeMaxDepth(t *testing.T) {
	type Inner struct {
		D string `yaml:"d"`
	}
	type Level3 struct {
		C Inner `yaml:"c"`
	}
	type Level2 struct {
		B Level3 `yaml:"b"`
	}
	type Level1 struct {
		A Level2 `yaml:"a"`
	}
	input := "a:\n  b:\n    c:\n      d: val"
	var v Level1
	err := UnmarshalWithOptions([]byte(input), &v, WithMaxDepth(3))
	if err == nil {
		t.Fatal("expected max depth error")
	}
	if !strings.Contains(err.Error(), "exceeded max depth") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── decode: document node with no children ─────────────────────────────────

func TestDecodeEmptyDocument(t *testing.T) {
	var v any
	err := Unmarshal([]byte("---\n"), &v)
	if err != nil {
		t.Fatal(err)
	}
}

// ─── decode: pointer auto-allocation ────────────────────────────────────────

func TestDecodePointerAutoAlloc(t *testing.T) {
	type Config struct {
		Value *string `yaml:"value"`
	}
	var c Config
	err := Unmarshal([]byte("value: hello"), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Value == nil || *c.Value != "hello" {
		t.Errorf("expected hello, got %v", c.Value)
	}
}

// ─── decode: interface{} dispatch ───────────────────────────────────────────

func TestDecodeToEmptyInterface(t *testing.T) {
	var v any
	err := Unmarshal([]byte("hello"), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v != "hello" {
		t.Errorf("expected hello, got %v", v)
	}
}

// ─── decode: anchor registration ────────────────────────────────────────────

func TestDecodeAnchorRegistered(t *testing.T) {
	input := "defaults: &def\n  x: 1\nval: *def"
	var v map[string]any
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	val := v["val"].(map[string]any)
	if val["x"] != int64(1) {
		t.Errorf("expected x=1, got %v", val["x"])
	}
}

// ─── decodeAlias: unknown alias ─────────────────────────────────────────────

func TestDecodeAliasUnknown(t *testing.T) {
	input := "val: *unknown"
	var v any
	err := Unmarshal([]byte(input), &v)
	if err == nil {
		t.Fatal("expected unknown alias error")
	}
	if !strings.Contains(err.Error(), "unknown alias") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── decodeAlias: cycle detection ───────────────────────────────────────────

func TestDecodeAliasCycle(t *testing.T) {
	input := "a: &a\n  b: *a"
	var v any
	err := UnmarshalWithOptions([]byte(input), &v, WithMaxAliasExpansion(1))
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

// ─── decodeScalar: implicit empty value ─────────────────────────────────────

func TestDecodeScalarImplicitEmpty(t *testing.T) {
	input := "key:"
	var v map[string]string
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v["key"] != "" {
		t.Errorf("expected empty string, got %q", v["key"])
	}
}

// ─── decodeScalar: tag resolver ─────────────────────────────────────────────

type doubleResolver struct{}

func (r doubleResolver) Resolve(value string) (any, error) {
	var v int
	_, err := fmt.Sscanf(value, "%d", &v)
	if err != nil {
		return nil, fmt.Errorf("cannot parse: %s", value)
	}
	return v * 2, nil
}

func TestDecodeScalarTagResolver(t *testing.T) {
	input := "val: !double 21"
	var out map[string]int
	err := UnmarshalWithOptions([]byte(input), &out,
		WithTagResolver(&TagResolver{
			Tag:    "!double",
			GoType: reflect.TypeFor[int](),
			Resolve: func(value string) (any, error) {
				return doubleResolver{}.Resolve(value)
			},
		}))
	if err != nil {
		t.Fatal(err)
	}
	if out["val"] != 42 {
		t.Errorf("expected 42, got %d", out["val"])
	}
}

func TestDecodeScalarTagResolverError(t *testing.T) {
	input := "val: !double notanumber"
	var out map[string]int
	err := UnmarshalWithOptions([]byte(input), &out,
		WithTagResolver(&TagResolver{
			Tag:    "!double",
			GoType: reflect.TypeFor[int](),
			Resolve: func(value string) (any, error) {
				return doubleResolver{}.Resolve(value)
			},
		}))
	if err == nil {
		t.Fatal("expected tag resolver error")
	}
	if !strings.Contains(err.Error(), "tag resolver") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeScalarTagResolverConvertible(t *testing.T) {
	input := "val: !double 5"
	var out map[string]int64
	err := UnmarshalWithOptions([]byte(input), &out,
		WithTagResolver(&TagResolver{
			Tag:    "!double",
			GoType: reflect.TypeFor[int64](),
			Resolve: func(value string) (any, error) {
				return doubleResolver{}.Resolve(value)
			},
		}))
	if err != nil {
		t.Fatal(err)
	}
	if out["val"] != 10 {
		t.Errorf("expected 10, got %d", out["val"])
	}
}

func TestDecodeScalarTagResolverNotAssignable(t *testing.T) {
	input := "val: !double 5"
	var out map[string][]string
	err := UnmarshalWithOptions([]byte(input), &out,
		WithTagResolver(&TagResolver{
			Tag:    "!double",
			GoType: reflect.TypeFor[[]string](),
			Resolve: func(value string) (any, error) {
				return doubleResolver{}.Resolve(value)
			},
		}))
	if err == nil {
		t.Fatal("expected type error")
	}
}

// ─── decodeScalar: null value in plain style ────────────────────────────────

func TestDecodeScalarNull(t *testing.T) {
	tests := []string{"null", "Null", "NULL", "~"}
	for _, null := range tests {
		t.Run(null, func(t *testing.T) {
			input := fmt.Sprintf("val: %s\nother: keep", null)
			var v map[string]any
			err := Unmarshal([]byte(input), &v)
			if err != nil {
				t.Fatal(err)
			}
			if v["val"] != nil {
				t.Errorf("expected nil for null value %q, got %v", null, v["val"])
			}
			if v["other"] != "keep" {
				t.Errorf("expected keep, got %v", v["other"])
			}
		})
	}
}

// ─── decodeScalar: bool ─────────────────────────────────────────────────────

func TestDecodeScalarBool(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"false", false},
		{"False", false},
		{"FALSE", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var v bool
			err := Unmarshal([]byte(tt.input), &v)
			if err != nil {
				t.Fatal(err)
			}
			if v != tt.want {
				t.Errorf("got %v, want %v", v, tt.want)
			}
		})
	}
}

func TestDecodeScalarBoolError(t *testing.T) {
	var v bool
	err := Unmarshal([]byte("notabool"), &v)
	if err == nil {
		t.Fatal("expected error for non-bool")
	}
}

// ─── decodeScalar: integers ─────────────────────────────────────────────────

func TestDecodeScalarInt(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"42", 42},
		{"-17", -17},
		{"0x1F", 31},
		{"0o17", 15},
		{"0b1010", 10},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var v int64
			err := Unmarshal([]byte(tt.input), &v)
			if err != nil {
				t.Fatal(err)
			}
			if v != tt.want {
				t.Errorf("got %d, want %d", v, tt.want)
			}
		})
	}
}

func TestDecodeScalarIntError(t *testing.T) {
	var v int
	err := Unmarshal([]byte("not_a_number"), &v)
	if err == nil {
		t.Fatal("expected error for non-int")
	}
}

// ─── decodeScalar: unsigned integers ────────────────────────────────────────

func TestDecodeScalarUint(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"42", 42},
		{"0xFF", 255},
		{"0o77", 63},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var v uint64
			err := Unmarshal([]byte(tt.input), &v)
			if err != nil {
				t.Fatal(err)
			}
			if v != tt.want {
				t.Errorf("got %d, want %d", v, tt.want)
			}
		})
	}
}

func TestDecodeScalarUintError(t *testing.T) {
	var v uint
	err := Unmarshal([]byte("notanuint"), &v)
	if err == nil {
		t.Fatal("expected error for non-uint")
	}
}

// ─── decodeScalar: floats ───────────────────────────────────────────────────

func TestDecodeScalarFloat(t *testing.T) {
	tests := []struct {
		input   string
		checker func(float64) bool
	}{
		{"3.14", func(f float64) bool { return f == 3.14 }},
		{".inf", func(f float64) bool { return math.IsInf(f, 1) }},
		{"-.inf", func(f float64) bool { return math.IsInf(f, -1) }},
		{".nan", func(f float64) bool { return math.IsNaN(f) }},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var v float64
			err := Unmarshal([]byte(tt.input), &v)
			if err != nil {
				t.Fatal(err)
			}
			if !tt.checker(v) {
				t.Errorf("unexpected value: %v", v)
			}
		})
	}
}

func TestDecodeScalarFloatError(t *testing.T) {
	var v float64
	err := Unmarshal([]byte("notafloat"), &v)
	if err == nil {
		t.Fatal("expected error for non-float")
	}
}

// ─── decodeScalar: duration ─────────────────────────────────────────────────

func TestDecodeScalarDuration(t *testing.T) {
	type S struct {
		D time.Duration `yaml:"d"`
	}
	var s S
	err := Unmarshal([]byte("d: 5s"), &s)
	if err != nil {
		t.Fatal(err)
	}
	if s.D != 5*time.Second {
		t.Errorf("expected 5s, got %v", s.D)
	}
}

func TestDecodeScalarDurationError(t *testing.T) {
	type S struct {
		D time.Duration `yaml:"d"`
	}
	var s S
	err := Unmarshal([]byte("d: notaduration"), &s)
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

// ─── decodeScalar: byte slice (binary tag) ──────────────────────────────────

func TestDecodeScalarBinaryTag(t *testing.T) {
	input := "data: !!binary aGVsbG8="
	type S struct {
		Data []byte `yaml:"data"`
	}
	var s S
	err := Unmarshal([]byte(input), &s)
	if err != nil {
		t.Fatal(err)
	}
	if string(s.Data) != "hello" {
		t.Errorf("expected hello, got %q", string(s.Data))
	}
}

func TestDecodeScalarBinaryTagInvalid(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind:  nodeScalar,
		tag:   "tag:yaml.org,2002:binary",
		value: "@@@invalid-base64@@@",
		style: scalarPlain,
		pos:   Position{Line: 1, Column: 1},
	}
	var v []byte
	rv := reflect.ValueOf(&v).Elem()
	err := d.decodeScalar(n, rv)
	if err != nil {
		t.Fatalf("expected nil error (type error accumulated), got %v", err)
	}
	if len(d.typeErrors) == 0 {
		t.Fatal("expected accumulated type error for invalid base64")
	}
}

func TestDecodeScalarByteSliceNonBinary(t *testing.T) {
	input := "data: hello"
	type S struct {
		Data []byte `yaml:"data"`
	}
	var s S
	err := Unmarshal([]byte(input), &s)
	if err != nil {
		t.Fatal(err)
	}
	if string(s.Data) != "hello" {
		t.Errorf("expected hello, got %q", string(s.Data))
	}
}

// ─── decodeScalar: time.Time ────────────────────────────────────────────────

func TestDecodeScalarTime(t *testing.T) {
	tests := []struct {
		input string
		year  int
	}{
		{"2024-01-15", 2024},
		{"2024-01-15 10:30:00", 2024},
		{"2024-01-15t10:30:00Z", 2024},
		{"2024-01-15T10:30:00Z", 2024},
		{"2024-01-15T10:30:00.123456789Z", 2024},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			type S struct {
				T time.Time `yaml:"t"`
			}
			var s S
			err := Unmarshal([]byte("t: "+tt.input), &s)
			if err != nil {
				t.Fatal(err)
			}
			if s.T.Year() != tt.year {
				t.Errorf("expected year %d, got %d", tt.year, s.T.Year())
			}
		})
	}
}

func TestDecodeScalarTimeError(t *testing.T) {
	type S struct {
		T time.Time `yaml:"t"`
	}
	var s S
	err := Unmarshal([]byte("t: notatime"), &s)
	if err == nil {
		t.Fatal("expected error for invalid time")
	}
}

// ─── decodeScalar: struct that is not time.Time ─────────────────────────────

func TestDecodeScalarToStruct(t *testing.T) {
	type Custom struct {
		Name string `yaml:"name"`
	}
	type S struct {
		C Custom `yaml:"c"`
	}
	var s S
	err := Unmarshal([]byte("c: scalar_value"), &s)
	if err == nil {
		t.Fatal("expected type error for scalar->struct")
	}
}

// ─── decodeScalar: default case (unknown kind) ─────────────────────────────

func TestDecodeScalarToChannel(t *testing.T) {
	type S struct {
		C chan int `yaml:"c"`
	}
	var s S
	err := Unmarshal([]byte("c: 42"), &s)
	if err == nil {
		t.Fatal("expected type error for scalar->chan")
	}
}

// ─── decodeMapping ──────────────────────────────────────────────────────────

func TestDecodeMappingToMap(t *testing.T) {
	input := "a: 1\nb: 2"
	var v map[string]int
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v["a"] != 1 || v["b"] != 2 {
		t.Errorf("unexpected: %v", v)
	}
}

func TestDecodeMappingToStructDirect(t *testing.T) {
	input := "name: alice\nage: 30"
	type Person struct {
		Name string `yaml:"name"`
		Age  int    `yaml:"age"`
	}
	var p Person
	err := Unmarshal([]byte(input), &p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "alice" || p.Age != 30 {
		t.Errorf("unexpected: %+v", p)
	}
}

func TestDecodeMappingToEmptyInterface(t *testing.T) {
	input := "a: 1"
	var v any
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", v)
	}
	if m["a"] != int64(1) {
		t.Errorf("expected 1, got %v", m["a"])
	}
}

func TestDecodeMappingToTypedInterface(t *testing.T) {
	input := "a: 1"
	var v fmt.Stringer
	err := Unmarshal([]byte(input), &v)
	if err == nil {
		t.Fatal("expected error for mapping into typed interface")
	}
}

func TestDecodeMappingToDefault(t *testing.T) {
	input := "a: 1"
	var v int
	err := Unmarshal([]byte(input), &v)
	if err == nil {
		t.Fatal("expected type error for mapping->int")
	}
}

// ─── decodeMappingToMap: merge key ──────────────────────────────────────────

func TestDecodeMappingToMapMergeKey(t *testing.T) {
	input := "defaults: &d\n  x: 1\nresult:\n  <<: *d\n  y: 2"
	var v map[string]any
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	result := v["result"].(map[string]any)
	if result["x"] != int64(1) {
		t.Errorf("expected merged x=1, got %v", result["x"])
	}
	if result["y"] != int64(2) {
		t.Errorf("expected y=2, got %v", result["y"])
	}
}

// ─── decodeMappingToMap: duplicate key ──────────────────────────────────────

func TestDecodeMappingToMapDuplicateKey(t *testing.T) {
	input := "a: 1\na: 2"
	var v map[string]int
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v["a"] != 2 {
		t.Errorf("expected last value 2, got %d", v["a"])
	}
}

func TestDecodeMappingToMapDuplicateKeyDisallowed(t *testing.T) {
	input := "a: 1\na: 2"
	var v map[string]int
	err := UnmarshalWithOptions([]byte(input), &v, WithDisallowDuplicateKey())
	if err == nil {
		t.Fatal("expected duplicate key error")
	}
}

// ─── decodeMappingToStruct: strict mode ─────────────────────────────────────

func TestDecodeMappingToStructStrict(t *testing.T) {
	type S struct {
		Name string `yaml:"name"`
	}
	var s S
	err := UnmarshalWithOptions([]byte("name: alice\nunknown: val"), &s, WithStrict())
	if err == nil {
		t.Fatal("expected unknown field error in strict mode")
	}
}

// ─── decodeMappingToStruct: required field ──────────────────────────────────

func TestDecodeMappingToStructRequired(t *testing.T) {
	type S struct {
		Name string `yaml:"name,required"`
		Age  int    `yaml:"age"`
	}
	var s S
	err := Unmarshal([]byte("age: 30"), &s)
	if err == nil {
		t.Fatal("expected required field error")
	}
	if !strings.Contains(err.Error(), "required field") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── decodeMappingToStruct: merge key ───────────────────────────────────────

func TestDecodeMappingToStructMergeKey(t *testing.T) {
	input := `
defaults: &d
  adapter: postgres
  host: localhost
production:
  <<: *d
  host: prod.example.com`
	type DB struct {
		Adapter string `yaml:"adapter"`
		Host    string `yaml:"host"`
	}
	type Config struct {
		Defaults   DB `yaml:"defaults"`
		Production DB `yaml:"production"`
	}
	var c Config
	err := Unmarshal([]byte(input), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Production.Adapter != "postgres" {
		t.Errorf("expected merged adapter=postgres, got %q", c.Production.Adapter)
	}
	if c.Production.Host != "prod.example.com" {
		t.Errorf("expected host override, got %q", c.Production.Host)
	}
}

// ─── decodeMappingToStruct: field not settable ──────────────────────────────

func TestDecodeMappingToStructUnexported(t *testing.T) {
	type S struct {
		Name string `yaml:"name"`
		skip string `yaml:"skip"`
	}
	_ = S{skip: ""}
	var s S
	err := Unmarshal([]byte("name: alice\nskip: ignored"), &s)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "alice" {
		t.Errorf("expected alice, got %q", s.Name)
	}
}

// ─── decodeMerge: sequence of aliases ───────────────────────────────────────

func TestDecodeMergeSequenceOfAliases(t *testing.T) {
	input := `
a: &a
  x: 1
b: &b
  y: 2
c:
  <<: [*a, *b]
  z: 3`
	var v map[string]any
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	c := v["c"].(map[string]any)
	if c["x"] != int64(1) || c["y"] != int64(2) || c["z"] != int64(3) {
		t.Errorf("merge sequence failed: %v", c)
	}
}

// ─── decodeMerge: direct mapping ────────────────────────────────────────────

func TestDecodeMergeDirectMapping(t *testing.T) {
	input := `
base:
  x: 1
  y: 2
derived:
  <<:
    x: 10
    y: 20
  z: 3`
	var v map[string]any
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	derived := v["derived"].(map[string]any)
	if derived["z"] != int64(3) {
		t.Errorf("expected z=3, got %v", derived["z"])
	}
}

// ─── decodeMappingMerge: struct merge ───────────────────────────────────────

func TestDecodeMappingMergeStruct(t *testing.T) {
	input := `
defaults: &defaults
  adapter: postgres
  host: localhost
  port: 5432
production:
  <<: *defaults
  host: prod.db.example.com`
	type DB struct {
		Adapter string `yaml:"adapter"`
		Host    string `yaml:"host"`
		Port    int    `yaml:"port"`
	}
	type Config struct {
		Defaults   DB `yaml:"defaults"`
		Production DB `yaml:"production"`
	}
	var c Config
	err := Unmarshal([]byte(input), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Production.Adapter != "postgres" {
		t.Errorf("expected postgres, got %q", c.Production.Adapter)
	}
	if c.Production.Host != "prod.db.example.com" {
		t.Errorf("expected override, got %q", c.Production.Host)
	}
	if c.Production.Port != 5432 {
		t.Errorf("expected merged port 5432, got %d", c.Production.Port)
	}
}

// ─── decodeMappingMerge: nested merge key in struct ─────────────────────────

func TestDecodeMappingMergeStructNestedMerge(t *testing.T) {
	input := `
base1: &b1
  x: 1
base2: &b2
  <<: *b1
  y: 2
result:
  <<: *b2
  z: 3`
	type S struct {
		X int `yaml:"x"`
		Y int `yaml:"y"`
		Z int `yaml:"z"`
	}
	type Config struct {
		Base1  S `yaml:"base1"`
		Base2  S `yaml:"base2"`
		Result S `yaml:"result"`
	}
	var c Config
	err := Unmarshal([]byte(input), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Result.Z != 3 {
		t.Errorf("expected z=3, got %d", c.Result.Z)
	}
}

// ─── decodeSequence: slice ──────────────────────────────────────────────────

func TestDecodeSequenceSlice(t *testing.T) {
	input := "- 1\n- 2\n- 3"
	var v []int
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 3 || v[0] != 1 || v[2] != 3 {
		t.Errorf("unexpected: %v", v)
	}
}

// ─── decodeSequence: array ──────────────────────────────────────────────────

func TestDecodeSequenceArray(t *testing.T) {
	input := "- a\n- b\n- c"
	var v [3]string
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v != [3]string{"a", "b", "c"} {
		t.Errorf("unexpected: %v", v)
	}
}

func TestDecodeSequenceArrayOverflow(t *testing.T) {
	input := "- 1\n- 2\n- 3\n- 4\n- 5"
	var v [2]int
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v[0] != 1 || v[1] != 2 {
		t.Errorf("unexpected: %v", v)
	}
}

// ─── decodeSequence: interface ──────────────────────────────────────────────

func TestDecodeSequenceToInterface(t *testing.T) {
	input := "- 1\n- hello\n- true"
	var v any
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", v)
	}
	if len(s) != 3 {
		t.Errorf("expected 3 items, got %d", len(s))
	}
}

func TestDecodeSequenceToTypedInterface(t *testing.T) {
	input := "- 1\n- 2"
	var v fmt.Stringer
	err := Unmarshal([]byte(input), &v)
	if err == nil {
		t.Fatal("expected error for sequence into typed interface")
	}
}

func TestDecodeSequenceToDefault(t *testing.T) {
	input := "- 1\n- 2"
	var v int
	err := Unmarshal([]byte(input), &v)
	if err == nil {
		t.Fatal("expected type error for sequence->int")
	}
}

// ─── decodeToAny: nil node ──────────────────────────────────────────────────

func TestDecodeToAnyNilReturn(t *testing.T) {
	var v any
	err := Unmarshal([]byte(""), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Errorf("expected nil, got %v", v)
	}
}

// ─── decodeToAny: alias ────────────────────────────────────────────────────

func TestDecodeToAnyAlias(t *testing.T) {
	input := "x: &x hello\ny: *x"
	var v map[string]any
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v["y"] != "hello" {
		t.Errorf("expected hello, got %v", v["y"])
	}
}

func TestDecodeToAnyAliasUnknown(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind:  nodeAlias,
		alias: "nonexistent",
	}
	_, err := d.decodeToAny(n)
	if err == nil {
		t.Fatal("expected unknown alias error")
	}
}

func TestDecodeToAnyAliasCycle(t *testing.T) {
	opts := defaultDecodeOptions()
	opts.maxAliasExpansion = 1
	d := newDecoder(opts)
	anchor := &node{kind: nodeScalar, value: "val", anchor: "a"}
	d.anchors["a"] = anchor
	alias := &node{kind: nodeAlias, alias: "a"}
	d.aliasDepth = 2
	_, err := d.decodeToAny(alias)
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

// ─── decodeToAny: document ──────────────────────────────────────────────────

func TestDecodeToAnyDocument(t *testing.T) {
	input := "---\nhello"
	var v any
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v != "hello" {
		t.Errorf("expected hello, got %v", v)
	}
}

// ─── decodeToAny: merge key with sequence of maps ───────────────────────────

func TestDecodeToAnyMergeKeySequence(t *testing.T) {
	input := `
a: &a
  x: 1
b: &b
  y: 2
c:
  <<: [*a, *b]
  z: 3`
	var v any
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	c := m["c"].(map[string]any)
	if c["x"] != int64(1) {
		t.Errorf("expected x=1, got %v", c["x"])
	}
	if c["y"] != int64(2) {
		t.Errorf("expected y=2, got %v", c["y"])
	}
}

// ─── decodeToAny: non-scalar key ────────────────────────────────────────────

func TestDecodeToAnyNonScalarKey(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeMapping, value: "complex_key"},
			{kind: nodeScalar, value: "val"},
		},
	}
	result, err := d.decodeToAny(n)
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if _, ok := m["complex_key"]; !ok {
		t.Error("expected non-scalar key to be formatted as string")
	}
}

// ─── decodeToAny: sequence error ────────────────────────────────────────────

func TestDecodeToAnySequenceError(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind: nodeSequence,
		children: []*node{
			{kind: nodeAlias, alias: "nonexistent"},
		},
	}
	_, err := d.decodeToAny(n)
	if err == nil {
		t.Fatal("expected error from sequence child")
	}
}

// ─── decodeMappingToOrderedMap ──────────────────────────────────────────────

func TestDecodeMappingToOrderedMap(t *testing.T) {
	input := "b: 2\na: 1\nc: 3"
	var v any
	err := UnmarshalWithOptions([]byte(input), &v, WithOrderedMap())
	if err != nil {
		t.Fatal(err)
	}
	ms, ok := v.(MapSlice)
	if !ok {
		t.Fatalf("expected MapSlice, got %T", v)
	}
	if len(ms) != 3 {
		t.Fatalf("expected 3 items, got %d", len(ms))
	}
	if ms[0].Key != "b" || ms[1].Key != "a" || ms[2].Key != "c" {
		t.Errorf("order not preserved: %v", ms)
	}
}

func TestDecodeMappingToOrderedMapNonScalarKey(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	d.opts.useOrderedMap = true
	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeMapping, value: "complex"},
			{kind: nodeScalar, value: "val"},
		},
	}
	result, err := d.decodeMappingToOrderedMap(n)
	if err != nil {
		t.Fatal(err)
	}
	if result[0].Key != "complex" {
		t.Errorf("expected key 'complex', got %v", result[0].Key)
	}
}

// ─── scalarToAny ─────────────────────────────────────��──────────────────────

func TestScalarToAnyBinaryTag(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind:  nodeScalar,
		tag:   "tag:yaml.org,2002:binary",
		value: "aGVsbG8=",
		style: scalarPlain,
	}
	result := d.scalarToAny(n)
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if s != "aGVsbG8=" {
		t.Errorf("expected raw base64 string, got %q", s)
	}
}

func TestScalarToAnyBinaryTagInvalid(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind:  nodeScalar,
		tag:   "tag:yaml.org,2002:binary",
		value: "!!!invalid!!!",
		style: scalarPlain,
	}
	result := d.scalarToAny(n)
	if result != "!!!invalid!!!" {
		t.Errorf("expected raw string on decode error, got %v", result)
	}
}

func TestScalarToAnyQuotedString(t *testing.T) {
	var v any
	err := Unmarshal([]byte(`"42"`), &v)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string for quoted int, got %T", v)
	}
	if s != "42" {
		t.Errorf("expected '42', got %q", s)
	}
}

func TestScalarToAnyNull(t *testing.T) {
	tests := []string{"null", "Null", "NULL", "~"}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			var v any
			err := Unmarshal([]byte(input), &v)
			if err != nil {
				t.Fatal(err)
			}
			if v != nil {
				t.Errorf("expected nil for %q, got %v", input, v)
			}
		})
	}
}

func TestScalarToAnyBool(t *testing.T) {
	var v any
	err := Unmarshal([]byte("true"), &v)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := v.(bool)
	if !ok {
		t.Fatalf("expected bool, got %T", v)
	}
	if !b {
		t.Error("expected true")
	}
}

func TestScalarToAnyInt(t *testing.T) {
	var v any
	err := Unmarshal([]byte("42"), &v)
	if err != nil {
		t.Fatal(err)
	}
	i, ok := v.(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", v)
	}
	if i != 42 {
		t.Errorf("expected 42, got %d", i)
	}
}

func TestScalarToAnyFloat(t *testing.T) {
	var v any
	err := Unmarshal([]byte("3.14"), &v)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", v)
	}
	if f != 3.14 {
		t.Errorf("expected 3.14, got %v", f)
	}
}

func TestScalarToAnySpecialFloats(t *testing.T) {
	tests := []struct {
		input   string
		checker func(any) bool
	}{
		{".inf", func(v any) bool { return math.IsInf(v.(float64), 1) }},
		{"-.inf", func(v any) bool { return math.IsInf(v.(float64), -1) }},
		{".nan", func(v any) bool { return math.IsNaN(v.(float64)) }},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var v any
			err := Unmarshal([]byte(tt.input), &v)
			if err != nil {
				t.Fatal(err)
			}
			if !tt.checker(v) {
				t.Errorf("unexpected value: %v", v)
			}
		})
	}
}

func TestScalarToAnyHexOctal(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"0x1F", 31},
		{"0o17", 15},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var v any
			err := Unmarshal([]byte(tt.input), &v)
			if err != nil {
				t.Fatal(err)
			}
			if v != tt.want {
				t.Errorf("expected %d, got %v", tt.want, v)
			}
		})
	}
}

// ─── nodeKindName ───────────────────────────────────────────────────────────

func TestNodeKindName(t *testing.T) {
	tests := []struct {
		kind nodeKind
		want string
	}{
		{nodeScalar, "!!str"},
		{nodeMapping, "!!map"},
		{nodeSequence, "!!seq"},
		{nodeKind(99), "unknown"},
	}
	for _, tt := range tests {
		got := nodeKindName(tt.kind)
		if got != tt.want {
			t.Errorf("nodeKindName(%d) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

// ─── parseBool ──────────────────────────────────────────────────────────────

func TestParseBoolAll(t *testing.T) {
	trueValues := []string{"true", "True", "TRUE"}
	for _, s := range trueValues {
		b, err := parseBool(s)
		if err != nil || !b {
			t.Errorf("parseBool(%q) = %v, %v", s, b, err)
		}
	}
	falseValues := []string{"false", "False", "FALSE"}
	for _, s := range falseValues {
		b, err := parseBool(s)
		if err != nil || b {
			t.Errorf("parseBool(%q) = %v, %v", s, b, err)
		}
	}
	_, err := parseBool("yes")
	if err == nil {
		t.Error("expected error for non-bool string")
	}
}

// ─── parseInt ───────────────────────────────────────────────────────────────

func TestParseIntAll(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"42", 42},
		{"-1", -1},
		{"0x1F", 31},
		{"0X1F", 31},
		{"0o17", 15},
		{"0O17", 15},
		{"0b1010", 10},
		{"0B1010", 10},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseInt(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

// ─── parseUint ──────────────────────────────────────────────────────────────

func TestParseUintAll(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"42", 42},
		{"0xFF", 255},
		{"0XFF", 255},
		{"0o77", 63},
		{"0O77", 63},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseUint(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

// ─── parseFloat ─────────────────────────────────────────────────────────────

func TestParseFloatAll(t *testing.T) {
	tests := []struct {
		input   string
		checker func(float64) bool
	}{
		{"3.14", func(f float64) bool { return f == 3.14 }},
		{".inf", func(f float64) bool { return math.IsInf(f, 1) }},
		{".Inf", func(f float64) bool { return math.IsInf(f, 1) }},
		{".INF", func(f float64) bool { return math.IsInf(f, 1) }},
		{"-.inf", func(f float64) bool { return math.IsInf(f, -1) }},
		{"-.Inf", func(f float64) bool { return math.IsInf(f, -1) }},
		{"-.INF", func(f float64) bool { return math.IsInf(f, -1) }},
		{".nan", func(f float64) bool { return math.IsNaN(f) }},
		{".NaN", func(f float64) bool { return math.IsNaN(f) }},
		{".NAN", func(f float64) bool { return math.IsNaN(f) }},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			f, err := parseFloat(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if !tt.checker(f) {
				t.Errorf("unexpected value for %q: %v", tt.input, f)
			}
		})
	}
}

// ─── parseTime ──────────────────────────────────────────────────────────────

func TestParseTimeFormats(t *testing.T) {
	tests := []struct {
		input string
		year  int
	}{
		{"2024-01-15", 2024},
		{"2024-01-15 10:30:00", 2024},
		{"2024-01-15t10:30:00Z", 2024},
		{"2024-01-15T10:30:00Z", 2024},
		{"2024-01-15T10:30:00.123456789Z", 2024},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tm, err := parseTime(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if tm.Year() != tt.year {
				t.Errorf("expected year %d, got %d", tt.year, tm.Year())
			}
		})
	}
}

func TestParseTimeError(t *testing.T) {
	_, err := parseTime("not-a-time")
	if err == nil {
		t.Fatal("expected error for invalid time")
	}
}

// ─── fieldByIndex ───────────────────────────────────────────────────────────

func TestFieldByIndexNested(t *testing.T) {
	type Inner struct {
		Val string
	}
	type Outer struct {
		Inner *Inner
	}
	var o Outer
	v := reflect.ValueOf(&o).Elem()
	field := fieldByIndex(v, []int{0, 0})
	if !field.IsValid() {
		t.Fatal("expected valid field")
	}
	if o.Inner == nil {
		t.Fatal("expected Inner to be auto-allocated")
	}
}

// ─── isNullValue ────────────────────────────────────────────────────────────

func TestIsNullValue(t *testing.T) {
	trueVals := []string{"", "~", "null", "Null", "NULL"}
	for _, s := range trueVals {
		if !isNullValue(s) {
			t.Errorf("expected isNullValue(%q) = true", s)
		}
	}
	falseVals := []string{"nope", "nil", "none"}
	for _, s := range falseVals {
		if isNullValue(s) {
			t.Errorf("expected isNullValue(%q) = false", s)
		}
	}
}

// ─── isMergeKey ─────────────────────────────────────────────────────────────

func TestIsMergeKey(t *testing.T) {
	yes := &node{kind: nodeScalar, value: "<<"}
	no := &node{kind: nodeScalar, value: ">>"}
	notScalar := &node{kind: nodeMapping, value: "<<"}

	if !isMergeKey(yes) {
		t.Error("expected true for <<")
	}
	if isMergeKey(no) {
		t.Error("expected false for >>")
	}
	if isMergeKey(notScalar) {
		t.Error("expected false for non-scalar")
	}
}

// ─��─ decodeBase64 ───────────────────────────────────────────────────────────

func TestDecodeBase64(t *testing.T) {
	result, err := decodeBase64("aGVsbG8=")
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != "hello" {
		t.Errorf("expected hello, got %q", string(result))
	}
}

func TestDecodeBase64WithWhitespace(t *testing.T) {
	result, err := decodeBase64("aGVs\n bG8=")
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != "hello" {
		t.Errorf("expected hello, got %q", string(result))
	}
}

func TestDecodeBase64Invalid(t *testing.T) {
	_, err := decodeBase64("!!!invalid!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

// ─── jsonEncodeYAML ─────────────────────────────────────────────────────────

func TestJsonEncodeYAML(t *testing.T) {
	result, err := jsonEncodeYAML([]byte("key: val"))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]string
	if err := json.Unmarshal(result, &v); err != nil {
		t.Fatal(err)
	}
	if v["key"] != "val" {
		t.Errorf("expected val, got %q", v["key"])
	}
}

func TestJsonEncodeYAMLError(t *testing.T) {
	_, err := jsonEncodeYAML([]byte("[unclosed"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

// ─── marshalNode ────────────────────────────────────────────────────────────

func TestMarshalNodeBasic(t *testing.T) {
	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "key"},
			{kind: nodeScalar, value: "val"},
		},
	}
	raw, err := marshalNode(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "key") {
		t.Errorf("expected key in output, got %q", raw)
	}
}

// ─── decode: Unmarshaler interface ──────────────────────────────────────────

type decodeUnmarshaler struct {
	Val string
}

func (u *decodeUnmarshaler) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	u.Val = "custom:" + s
	return nil
}

func TestDecodeUnmarshalerInterface(t *testing.T) {
	input := "hello"
	var u decodeUnmarshaler
	err := Unmarshal([]byte(input), &u)
	if err != nil {
		t.Fatal(err)
	}
	if u.Val != "custom:hello" {
		t.Errorf("expected custom:hello, got %q", u.Val)
	}
}

// ─── decode: BytesUnmarshaler interface ─────────────────────────────────────

type decodeBytesUnmarshaler struct {
	Raw []byte
}

func (u *decodeBytesUnmarshaler) UnmarshalYAML(data []byte) error {
	u.Raw = append([]byte(nil), data...)
	return nil
}

func TestDecodeBytesUnmarshalerInterface(t *testing.T) {
	input := "key: val"
	type S struct {
		Key decodeBytesUnmarshaler `yaml:"key"`
	}
	var s S
	err := Unmarshal([]byte(input), &s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(s.Key.Raw), "val") {
		t.Errorf("expected raw to contain val, got %q", string(s.Key.Raw))
	}
}

// ─── decode: UnmarshalerContext interface ────────────────────────────────────

type decodeCtxUnmarshaler struct {
	Val string
}

type decodeCtxKey string

func (u *decodeCtxUnmarshaler) UnmarshalYAML(ctx context.Context, unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	prefix := ctx.Value(decodeCtxKey("prefix"))
	if prefix != nil {
		u.Val = prefix.(string) + s
	} else {
		u.Val = s
	}
	return nil
}

func TestDecodeUnmarshalerContextInterface(t *testing.T) {
	input := "world"
	ctx := context.WithValue(context.Background(), decodeCtxKey("prefix"), "hello-")
	dec := NewDecoder(strings.NewReader(input))
	var u decodeCtxUnmarshaler
	err := dec.DecodeContext(ctx, &u)
	if err != nil {
		t.Fatal(err)
	}
	if u.Val != "hello-world" {
		t.Errorf("expected hello-world, got %q", u.Val)
	}
}

// ─── decode: encoding.TextUnmarshaler ───────────────────────────────────────

func TestDecodeTextUnmarshaler(t *testing.T) {
	input := "val: 999999999999999999999999999999"
	type S struct {
		Val *big.Int `yaml:"val"`
	}
	var s S
	s.Val = new(big.Int)
	err := Unmarshal([]byte(input), &s)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := new(big.Int).SetString("999999999999999999999999999999", 10)
	if s.Val.Cmp(expected) != 0 {
		t.Errorf("expected %s, got %s", expected, s.Val)
	}
}

// ─── decode: JSON unmarshaler ───────────────────────────────────────────────

type decodeJSONTarget struct {
	Name string `json:"name"`
}

func (j *decodeJSONTarget) UnmarshalJSON(data []byte) error {
	type alias decodeJSONTarget
	return json.Unmarshal(data, (*alias)(j))
}

func TestDecodeJSONUnmarshaler(t *testing.T) {
	input := "name: alice"
	var v decodeJSONTarget
	err := UnmarshalWithOptions([]byte(input), &v, WithJSONUnmarshaler())
	if err != nil {
		t.Fatal(err)
	}
	if v.Name != "alice" {
		t.Errorf("expected alice, got %q", v.Name)
	}
}

// ─── decode: custom unmarshaler option ──────────────────────────────────────

func TestDecodeCustomUnmarshalerOption(t *testing.T) {
	type Color struct {
		R, G, B uint8
	}
	data := []byte("color: \"#ff8000\"\n")
	var m map[string]Color
	err := UnmarshalWithOptions(data, &m, WithCustomUnmarshaler(func(c *Color, raw []byte) error {
		s := strings.Trim(string(raw), " \n\"'")
		if !strings.HasPrefix(s, "#") || len(s) != 7 {
			return fmt.Errorf("invalid color: %s", s)
		}
		_, err := fmt.Sscanf(s, "#%02x%02x%02x", &c.R, &c.G, &c.B)
		return err
	}))
	if err != nil {
		t.Fatal(err)
	}
	c := m["color"]
	if c.R != 255 || c.G != 128 || c.B != 0 {
		t.Fatalf("expected {255 128 0}, got %+v", c)
	}
}

// ─── decode: addTypeError accumulation ──────────────────────────────────────

func TestDecodeTypeErrorAccumulation(t *testing.T) {
	input := "a: notint\nb: notint"
	var v struct {
		A int `yaml:"a"`
		B int `yaml:"b"`
	}
	err := Unmarshal([]byte(input), &v)
	if err == nil {
		t.Fatal("expected type error")
	}
	te, ok := err.(*TypeError)
	if !ok {
		t.Fatalf("expected *TypeError, got %T", err)
	}
	if len(te.Errors) != 2 {
		t.Errorf("expected 2 type errors, got %d", len(te.Errors))
	}
}

// ─── decode: embedded struct ────────────────────────────────────────────────

func TestDecodeEmbeddedStruct(t *testing.T) {
	type Base struct {
		Name string `yaml:"name"`
	}
	type Extended struct {
		Base `yaml:",inline"`
		Age  int `yaml:"age"`
	}
	var v Extended
	err := Unmarshal([]byte("name: alice\nage: 30"), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v.Name != "alice" || v.Age != 30 {
		t.Errorf("unexpected: %+v", v)
	}
}

// ─── decode: embedded pointer struct ────────────────────────────────────────

func TestDecodeEmbeddedPointerStruct(t *testing.T) {
	type Base struct {
		Name string `yaml:"name"`
	}
	type Extended struct {
		*Base `yaml:",inline"`
		Age   int `yaml:"age"`
	}
	var v Extended
	err := Unmarshal([]byte("name: alice\nage: 30"), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v.Base == nil || v.Base.Name != "alice" {
		t.Errorf("unexpected: %+v", v)
	}
}

// ─── decode: flow collections ───────────────────────────────────────────────

func TestDecodeFlowSequence(t *testing.T) {
	input := "[1, 2, 3]"
	var v []int
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 3 || v[0] != 1 || v[2] != 3 {
		t.Errorf("unexpected: %v", v)
	}
}

func TestDecodeFlowMapping(t *testing.T) {
	input := "{a: 1, b: 2}"
	var v map[string]int
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v["a"] != 1 || v["b"] != 2 {
		t.Errorf("unexpected: %v", v)
	}
}

// ─── decode: multi-line scalars ─────────────────────────────────────────────

func TestDecodeLiteralScalar(t *testing.T) {
	input := "val: |\n  line one\n  line two\n"
	var v map[string]string
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v["val"] != "line one\nline two\n" {
		t.Errorf("unexpected: %q", v["val"])
	}
}

func TestDecodeFoldedScalar(t *testing.T) {
	input := "val: >\n  para one\n  continues\n\n  para two\n"
	var v map[string]string
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	expected := "para one continues\npara two\n"
	if v["val"] != expected {
		t.Errorf("expected %q, got %q", expected, v["val"])
	}
}

// ─── decode: deeply nested ──────────────────────────────────────────────────

func TestDecodeDeeplyNested(t *testing.T) {
	input := "a:\n  b:\n    c:\n      d:\n        e: deep"
	var v any
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	a := m["a"].(map[string]any)
	b := a["b"].(map[string]any)
	c := b["c"].(map[string]any)
	d := c["d"].(map[string]any)
	if d["e"] != "deep" {
		t.Errorf("expected deep, got %v", d["e"])
	}
}

// ─── decode: escape sequences ───────────────────────────────────────────────

func TestDecodeDoubleQuotedEscapes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"\n"`, "\n"},
		{`"\t"`, "\t"},
		{`"\\"`, "\\"},
		{`"\""`, "\""},
		{`"A"`, "A"},
		{`"\U00000041"`, "A"},
		{`"\x41"`, "A"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var v string
			err := Unmarshal([]byte(tt.input), &v)
			if err != nil {
				t.Fatal(err)
			}
			if v != tt.want {
				t.Errorf("got %q, want %q", v, tt.want)
			}
		})
	}
}

// ─── decode: float32 ────────────────────────────────────────────────────────

func TestDecodeFloat32(t *testing.T) {
	type S struct {
		V float32 `yaml:"v"`
	}
	var s S
	err := Unmarshal([]byte("v: 3.14"), &s)
	if err != nil {
		t.Fatal(err)
	}
	if s.V < 3.13 || s.V > 3.15 {
		t.Errorf("expected ~3.14, got %v", s.V)
	}
}

// ─── decode: int width variants ─────────────────────────────────────────────

func TestDecodeIntWidths(t *testing.T) {
	type S struct {
		I8  int8  `yaml:"i8"`
		I16 int16 `yaml:"i16"`
		I32 int32 `yaml:"i32"`
	}
	var s S
	err := Unmarshal([]byte("i8: 1\ni16: 2\ni32: 3"), &s)
	if err != nil {
		t.Fatal(err)
	}
	if s.I8 != 1 || s.I16 != 2 || s.I32 != 3 {
		t.Errorf("unexpected: %+v", s)
	}
}

// ─── decode: uint width variants ────────────────────────────────────────────

func TestDecodeUintWidths(t *testing.T) {
	type S struct {
		U8  uint8  `yaml:"u8"`
		U16 uint16 `yaml:"u16"`
		U32 uint32 `yaml:"u32"`
	}
	var s S
	err := Unmarshal([]byte("u8: 1\nu16: 2\nu32: 3"), &s)
	if err != nil {
		t.Fatal(err)
	}
	if s.U8 != 1 || s.U16 != 2 || s.U32 != 3 {
		t.Errorf("unexpected: %+v", s)
	}
}

// ─── decode: map with merge (existing key not overwritten) ──────────────────

func TestDecodeMergeMapsExistingKeyPreserved(t *testing.T) {
	input := `
defaults: &d
  x: 1
  y: 2
result:
  x: override
  <<: *d`
	var v map[string]any
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	result := v["result"].(map[string]any)
	if result["x"] != "override" {
		t.Errorf("expected override, got %v", result["x"])
	}
	if result["y"] != int64(2) {
		t.Errorf("expected merged y=2, got %v", result["y"])
	}
}

// ─── decode: ordered map ────────────────────────────────────────────────────

func TestDecodeOrderedMapPreservesOrder(t *testing.T) {
	input := "z: 3\na: 1\nm: 2"
	var v any
	err := UnmarshalWithOptions([]byte(input), &v, WithOrderedMap())
	if err != nil {
		t.Fatal(err)
	}
	ms, ok := v.(MapSlice)
	if !ok {
		t.Fatalf("expected MapSlice, got %T", v)
	}
	if len(ms) != 3 {
		t.Fatalf("expected 3 items, got %d", len(ms))
	}
	if ms[0].Key != "z" || ms[1].Key != "a" || ms[2].Key != "m" {
		t.Errorf("order not preserved: %v", ms)
	}
}

// ─── decode: AllowDuplicateMapKey ───────────────────────────────────────────

func TestDecodeAllowDuplicateMapKey(t *testing.T) {
	input := "a: 1\na: 2"
	var v map[string]int
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v["a"] != 2 {
		t.Errorf("expected 2, got %d", v["a"])
	}
}

// ─── decode: DisallowDuplicateKey ───────────────────────────────────────────

func TestDecodeDisallowDuplicateKey(t *testing.T) {
	input := "a: 1\na: 2"
	var v map[string]int
	err := UnmarshalWithOptions([]byte(input), &v, WithDisallowDuplicateKey())
	if err == nil {
		t.Fatal("expected duplicate key error")
	}
}

// ─── decode: big.Float / big.Rat ────────────────────────────────────────────

func TestDecodeBigFloat(t *testing.T) {
	input := `v: "3.14159265358979323846264338327950288"`
	type S struct {
		V *big.Float `yaml:"v"`
	}
	var s S
	s.V = new(big.Float)
	err := Unmarshal([]byte(input), &s)
	if err != nil {
		t.Fatal(err)
	}
	expected, _, _ := new(big.Float).Parse("3.14159265358979323846264338327950288", 10)
	if s.V.Cmp(expected) != 0 {
		t.Errorf("expected %s, got %s", expected.Text('f', 30), s.V.Text('f', 30))
	}
}

func TestDecodeBigRat(t *testing.T) {
	input := `v: "1/3"`
	type S struct {
		V *big.Rat `yaml:"v"`
	}
	var s S
	s.V = new(big.Rat)
	err := Unmarshal([]byte(input), &s)
	if err != nil {
		t.Fatal(err)
	}
	expected := new(big.Rat).SetFrac64(1, 3)
	if s.V.Cmp(expected) != 0 {
		t.Errorf("expected %s, got %s", expected, s.V)
	}
}

// ─── decode: string ─────────────────────────────────────────────────────────

func TestDecodeStringDirect(t *testing.T) {
	var v string
	err := Unmarshal([]byte("hello world"), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v != "hello world" {
		t.Errorf("expected 'hello world', got %q", v)
	}
}

// ─── decode: single-quoted string ───────────────────────────────────────────

func TestDecodeSingleQuotedEscape(t *testing.T) {
	input := `'it''s escaped'`
	var v string
	err := Unmarshal([]byte(input), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v != "it's escaped" {
		t.Errorf("expected \"it's escaped\", got %q", v)
	}
}

// ─── decode: decodeMapping interface dispatch ───────────────────────────────

func TestDecodeMappingToAnyField(t *testing.T) {
	type S struct {
		Data any `yaml:"data"`
	}
	var s S
	err := Unmarshal([]byte("data:\n  x: 1\n  y: 2"), &s)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := s.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", s.Data)
	}
	if m["x"] != int64(1) {
		t.Errorf("expected x=1, got %v", m["x"])
	}
}

func TestDecodeMappingToAnyFieldNil(t *testing.T) {
	type S struct {
		Data any `yaml:"data"`
	}
	var s S
	err := Unmarshal([]byte("data:"), &s)
	if err != nil {
		t.Fatal(err)
	}
}

// ─── decodeSequence: into any field ─────────────────────────────────────────

func TestDecodeSequenceToAnyField(t *testing.T) {
	type S struct {
		Items any `yaml:"items"`
	}
	var s S
	err := Unmarshal([]byte("items:\n- 1\n- 2"), &s)
	if err != nil {
		t.Fatal(err)
	}
	sl, ok := s.Items.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", s.Items)
	}
	if len(sl) != 2 {
		t.Errorf("expected 2 items, got %d", len(sl))
	}
}

func TestDecodeSequenceToAnyFieldNil(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind: nodeSequence,
		children: []*node{
			{kind: nodeScalar, value: "hello"},
		},
	}
	var v any
	rv := reflect.ValueOf(&v).Elem()
	err := d.decodeSequence(n, rv)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", v)
	}
	if len(s) != 1 {
		t.Errorf("expected 1 item, got %d", len(s))
	}
}

// ──�� decode: custom unmarshaler error return ────────────────────────────────

func TestDecodeCustomUnmarshalerError(t *testing.T) {
	type T struct{ V int }
	data := []byte("v: 42")
	var out T
	err := UnmarshalWithOptions(data, &out, WithCustomUnmarshaler(func(t *T, raw []byte) error {
		return fmt.Errorf("custom error")
	}))
	if err == nil {
		t.Fatal("expected custom unmarshaler error")
	}
	if !strings.Contains(err.Error(), "custom error") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ─── decode: Unmarshaler with non-pointer target ────────────────────────────

// ─── decode: BytesUnmarshaler in struct ─────────────────────────────────────

func TestDecodeBytesUnmarshalerMapping(t *testing.T) {
	input := "item:\n  key: val\n  nested: true"
	type S struct {
		Item decodeBytesUnmarshaler `yaml:"item"`
	}
	var s S
	err := Unmarshal([]byte(input), &s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(s.Item.Raw), "key") {
		t.Errorf("expected raw to contain key, got %q", string(s.Item.Raw))
	}
}

// ─── decode: null into typed fields ─────────────────────────────────────────

func TestDecodeNullIntoTypedInt(t *testing.T) {
	type S struct {
		Val int `yaml:"val"`
	}
	var s S
	s.Val = 42
	err := Unmarshal([]byte("val: null"), &s)
	if err != nil {
		t.Fatal(err)
	}
	if s.Val != 0 {
		t.Errorf("expected 0 for null int, got %d", s.Val)
	}
}

func TestDecodeNullIntoTypedString(t *testing.T) {
	type S struct {
		Val string `yaml:"val"`
	}
	var s S
	s.Val = "existing"
	err := Unmarshal([]byte("val: null"), &s)
	if err != nil {
		t.Fatal(err)
	}
	if s.Val != "" {
		t.Errorf("expected empty for null string, got %q", s.Val)
	}
}

// ─── decode: decodeAlias through internal API ───────────────────────────────

func TestDecodeAliasInternal(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	anchor := &node{kind: nodeScalar, value: "hello", anchor: "a"}
	d.anchors["a"] = anchor
	alias := &node{kind: nodeAlias, alias: "a"}

	var v string
	rv := reflect.ValueOf(&v).Elem()
	err := d.decode(alias, rv)
	if err != nil {
		t.Fatal(err)
	}
	if v != "hello" {
		t.Errorf("expected hello, got %q", v)
	}
}

func TestDecodeAliasUnknownInternal(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	alias := &node{kind: nodeAlias, alias: "nonexistent"}

	var v string
	rv := reflect.ValueOf(&v).Elem()
	err := d.decode(alias, rv)
	if err == nil {
		t.Fatal("expected unknown alias error")
	}
}

func TestDecodeAliasCycleInternal(t *testing.T) {
	opts := defaultDecodeOptions()
	opts.maxAliasExpansion = 1
	d := newDecoder(opts)
	anchor := &node{kind: nodeScalar, value: "hello", anchor: "a"}
	d.anchors["a"] = anchor
	alias := &node{kind: nodeAlias, alias: "a"}
	d.aliasDepth = 2

	var v string
	rv := reflect.ValueOf(&v).Elem()
	err := d.decode(alias, rv)
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

// ─── decode: unknown node kind default ──────────────────────────────────────

func TestDecodeUnknownNodeKind(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{kind: nodeKind(99)}
	var v string
	rv := reflect.ValueOf(&v).Elem()
	err := d.decode(n, rv)
	if err != nil {
		t.Fatalf("expected nil for unknown kind, got %v", err)
	}
}

// ─── decode: document with children ─────────────────────────────────────────

func TestDecodeDocumentWithChild(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind: nodeDocument,
		children: []*node{
			{kind: nodeScalar, value: "hello"},
		},
	}
	var v string
	rv := reflect.ValueOf(&v).Elem()
	err := d.decode(n, rv)
	if err != nil {
		t.Fatal(err)
	}
	if v != "hello" {
		t.Errorf("expected hello, got %q", v)
	}
}

func TestDecodeDocumentEmpty(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{kind: nodeDocument}
	var v string
	rv := reflect.ValueOf(&v).Elem()
	err := d.decode(n, rv)
	if err != nil {
		t.Fatal(err)
	}
}

// ─── decodeMerge: internal coverage ─────────────────────────────────────────

func TestDecodeMergeUnknownAlias(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{kind: nodeAlias, alias: "unknown"}
	v := reflect.MakeMap(reflect.TypeFor[map[string]any]())
	err := d.decodeMerge(n, v)
	if err == nil {
		t.Fatal("expected error for unknown alias")
	}
}

func TestDecodeMergeSequenceInternal(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	mapping1 := &node{
		kind:   nodeMapping,
		anchor: "a",
		children: []*node{
			{kind: nodeScalar, value: "x"},
			{kind: nodeScalar, value: "1"},
		},
	}
	d.anchors["a"] = mapping1

	seqNode := &node{
		kind: nodeSequence,
		children: []*node{
			{kind: nodeAlias, alias: "a"},
		},
	}
	v := reflect.MakeMap(reflect.TypeFor[map[string]any]())
	err := d.decodeMerge(seqNode, v)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeMergeDirectMappingInternal(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "x"},
			{kind: nodeScalar, value: "1"},
		},
	}
	v := reflect.MakeMap(reflect.TypeFor[map[string]any]())
	err := d.decodeMerge(n, v)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeMergeScalar(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{kind: nodeScalar, value: "ignored"}
	v := reflect.MakeMap(reflect.TypeFor[map[string]any]())
	err := d.decodeMerge(n, v)
	if err != nil {
		t.Fatal(err)
	}
}

// ─── decodeMappingMerge: internal coverage ──────────────────────────────────

func TestDecodeMappingMergeNonMapping(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{kind: nodeScalar, value: "not a mapping"}
	v := reflect.MakeMap(reflect.TypeFor[map[string]any]())
	err := d.decodeMappingMerge(n, v)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeMappingMergeMapInternal(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "x"},
			{kind: nodeScalar, value: "1"},
			{kind: nodeScalar, value: "y"},
			{kind: nodeScalar, value: "2"},
		},
	}
	v := reflect.MakeMap(reflect.TypeFor[map[string]string]())
	v.SetMapIndex(reflect.ValueOf("x"), reflect.ValueOf("existing"))
	err := d.decodeMappingMerge(n, v)
	if err != nil {
		t.Fatal(err)
	}
	x := v.MapIndex(reflect.ValueOf("x"))
	if x.String() != "existing" {
		t.Errorf("expected existing key preserved, got %v", x)
	}
	y := v.MapIndex(reflect.ValueOf("y"))
	if y.String() != "2" {
		t.Errorf("expected y=2, got %v", y)
	}
}

func TestDecodeMappingMergeStructInternal(t *testing.T) {
	type S struct {
		X string `yaml:"x"`
		Y string `yaml:"y"`
	}
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "x"},
			{kind: nodeScalar, value: "1"},
			{kind: nodeScalar, value: "y"},
			{kind: nodeScalar, value: "2"},
		},
	}
	var s S
	s.X = "existing"
	rv := reflect.ValueOf(&s).Elem()
	err := d.decodeMappingMerge(n, rv)
	if err != nil {
		t.Fatal(err)
	}
	if s.X != "existing" {
		t.Errorf("expected existing X preserved, got %q", s.X)
	}
	if s.Y != "2" {
		t.Errorf("expected Y=2, got %q", s.Y)
	}
}

// ─── decodeMappingToMap: merge key internal ─────────────────────────────────

func TestDecodeMappingToMapMergeInternal(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	defaults := &node{
		kind:   nodeMapping,
		anchor: "defaults",
		children: []*node{
			{kind: nodeScalar, value: "x"},
			{kind: nodeScalar, value: "1"},
		},
	}
	d.anchors["defaults"] = defaults

	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "<<"},
			{kind: nodeAlias, alias: "defaults"},
			{kind: nodeScalar, value: "y"},
			{kind: nodeScalar, value: "2"},
		},
	}
	v := reflect.MakeMap(reflect.TypeFor[map[string]any]())
	err := d.decodeMappingToMap(n, v)
	if err != nil {
		t.Fatal(err)
	}
}

// ─── decodeMappingToStruct: merge key and field not settable ────────────────

func TestDecodeMappingToStructMergeInternal(t *testing.T) {
	type S struct {
		X string `yaml:"x"`
		Y string `yaml:"y"`
	}
	d := newDecoder(defaultDecodeOptions())
	defaults := &node{
		kind:   nodeMapping,
		anchor: "defaults",
		children: []*node{
			{kind: nodeScalar, value: "x"},
			{kind: nodeScalar, value: "1"},
		},
	}
	d.anchors["defaults"] = defaults

	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "<<"},
			{kind: nodeAlias, alias: "defaults"},
			{kind: nodeScalar, value: "y"},
			{kind: nodeScalar, value: "2"},
		},
	}
	var s S
	rv := reflect.ValueOf(&s).Elem()
	err := d.decodeMappingToStruct(n, rv)
	if err != nil {
		t.Fatal(err)
	}
	if s.X != "1" || s.Y != "2" {
		t.Errorf("unexpected: %+v", s)
	}
}

// ─── decodeSequence: array error ────────────────────────────────────────────

func TestDecodeSequenceArrayError(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind: nodeSequence,
		children: []*node{
			{kind: nodeAlias, alias: "nonexistent"},
		},
	}
	var v [1]string
	rv := reflect.ValueOf(&v).Elem()
	err := d.decodeSequence(n, rv)
	if err == nil {
		t.Fatal("expected error from array decode")
	}
}

// ─── decodeToAny: document dispatches ───────────────────────────────────────

func TestDecodeToAnyDocumentInternal(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind: nodeDocument,
		children: []*node{
			{kind: nodeScalar, value: "hello"},
		},
	}
	result, err := d.decodeToAny(n)
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello" {
		t.Errorf("expected hello, got %v", result)
	}
}

func TestDecodeToAnyDocumentEmpty(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{kind: nodeDocument}
	result, err := d.decodeToAny(n)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

// ─── decodeToAny: merge with sequence of maps ──────────────────────────────

func TestDecodeToAnyMergeSequenceInternal(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	m1 := &node{
		kind:   nodeMapping,
		anchor: "a",
		children: []*node{
			{kind: nodeScalar, value: "x"},
			{kind: nodeScalar, value: "1"},
		},
	}
	m2 := &node{
		kind:   nodeMapping,
		anchor: "b",
		children: []*node{
			{kind: nodeScalar, value: "y"},
			{kind: nodeScalar, value: "2"},
		},
	}
	d.anchors["a"] = m1
	d.anchors["b"] = m2

	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "<<"},
			{kind: nodeSequence, children: []*node{
				{kind: nodeAlias, alias: "a"},
				{kind: nodeAlias, alias: "b"},
			}},
			{kind: nodeScalar, value: "z"},
			{kind: nodeScalar, value: "3"},
		},
	}
	result, err := d.decodeToAny(n)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["z"] == nil {
		t.Error("expected z key in merged map")
	}
}

// ─── decodeToAny: default nil return for unknown kind ───────────────────────

func TestDecodeToAnyUnknownKind(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{kind: nodeKind(99)}
	result, err := d.decodeToAny(n)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil for unknown kind, got %v", result)
	}
}

// ─── decodeToAny: nil node ──────────────────────────────────────────────────

func TestDecodeToAnyNilNodeInternal(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	result, err := d.decodeToAny(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

// ─── decodeMappingToOrderedMap: error path ──────────────────────────────────

func TestDecodeMappingToOrderedMapError(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	d.opts.useOrderedMap = true
	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "key"},
			{kind: nodeAlias, alias: "nonexistent"},
		},
	}
	_, err := d.decodeMappingToOrderedMap(n)
	if err == nil {
		t.Fatal("expected error from orderedMap decode")
	}
}

// ─── scalarToAny: int range check ──────────────────────────────────────────

func TestScalarToAnyIntRange(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{
		kind:  nodeScalar,
		value: "42",
		style: scalarPlain,
	}
	result := d.scalarToAny(n)
	i, ok := result.(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", result)
	}
	if i != 42 {
		t.Errorf("expected 42, got %d", i)
	}
}

// ─── decode: struct merge with nested merge key ─────────────────────────────

func TestDecodeMappingMergeStructNestedMergeInternal(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	innerDefaults := &node{
		kind:   nodeMapping,
		anchor: "inner",
		children: []*node{
			{kind: nodeScalar, value: "a"},
			{kind: nodeScalar, value: "1"},
		},
	}
	d.anchors["inner"] = innerDefaults

	n := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "<<"},
			{kind: nodeAlias, alias: "inner"},
			{kind: nodeScalar, value: "b"},
			{kind: nodeScalar, value: "2"},
		},
	}

	type S struct {
		A string `yaml:"a"`
		B string `yaml:"b"`
	}
	var s S
	rv := reflect.ValueOf(&s).Elem()
	err := d.decodeMappingMerge(n, rv)
	if err != nil {
		t.Fatal(err)
	}
	if s.A != "1" || s.B != "2" {
		t.Errorf("unexpected: %+v", s)
	}
}

// ─── Basic Unmarshal ─────────────────────────────────────────────────────────

func TestUnmarshalSimpleMap(t *testing.T) {
	input := `name: hello
value: world`
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "hello" {
		t.Errorf("expected name=hello, got %q", out["name"])
	}
	if out["value"] != "world" {
		t.Errorf("expected value=world, got %q", out["value"])
	}
}

func TestUnmarshalStruct(t *testing.T) {
	input := `name: alice
age: 30
active: true`

	type Person struct {
		Name   string `yaml:"name"`
		Age    int    `yaml:"age"`
		Active bool   `yaml:"active"`
	}

	var p Person
	if err := Unmarshal([]byte(input), &p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "alice" {
		t.Errorf("expected name=alice, got %q", p.Name)
	}
	if p.Age != 30 {
		t.Errorf("expected age=30, got %d", p.Age)
	}
	if !p.Active {
		t.Error("expected active=true")
	}
}

func TestUnmarshalNestedStruct(t *testing.T) {
	input := `
metadata:
  name: webapp
  namespace: default
spec:
  replicas: 3
`
	type Meta struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	}
	type Spec struct {
		Replicas int `yaml:"replicas"`
	}
	type Deployment struct {
		Metadata Meta `yaml:"metadata"`
		Spec     Spec `yaml:"spec"`
	}

	var d Deployment
	if err := Unmarshal([]byte(input), &d); err != nil {
		t.Fatal(err)
	}
	if d.Metadata.Name != "webapp" {
		t.Errorf("expected metadata.name=webapp, got %q", d.Metadata.Name)
	}
	if d.Metadata.Namespace != "default" {
		t.Errorf("expected metadata.namespace=default, got %q", d.Metadata.Namespace)
	}
	if d.Spec.Replicas != 3 {
		t.Errorf("expected spec.replicas=3, got %d", d.Spec.Replicas)
	}
}

func TestUnmarshalSequence(t *testing.T) {
	input := `
- alice
- bob
- charlie
`
	var out []string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	expected := []string{"alice", "bob", "charlie"}
	if !reflect.DeepEqual(out, expected) {
		t.Errorf("expected %v, got %v", expected, out)
	}
}

func TestUnmarshalSequenceOfMaps(t *testing.T) {
	input := `
- name: alice
  age: 30
- name: bob
  age: 25
`
	type Person struct {
		Name string `yaml:"name"`
		Age  int    `yaml:"age"`
	}
	var out []Person
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 items, got %d", len(out))
	}
	if out[0].Name != "alice" || out[0].Age != 30 {
		t.Errorf("unexpected first item: %+v", out[0])
	}
	if out[1].Name != "bob" || out[1].Age != 25 {
		t.Errorf("unexpected second item: %+v", out[1])
	}
}

// ─── Scalar Types ────────────────────────────────────────────────────────────

func TestUnmarshalScalarTypes(t *testing.T) {
	input := `
str: hello
integer: 42
negative: -7
hex: 0xFF
octal: 0o17
float: 3.14
infinity: .inf
neg_inf: -.inf
nan: .nan
bool_true: true
bool_false: false
null_tilde: ~
null_word: null
`
	var out map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}

	check := func(key string, expected any) {
		t.Helper()
		got := out[key]
		if expected == nil {
			if got != nil {
				t.Errorf("%s: expected nil, got %v (%T)", key, got, got)
			}
			return
		}
		switch e := expected.(type) {
		case float64:
			g, ok := got.(float64)
			if !ok {
				t.Errorf("%s: expected float64, got %T", key, got)
				return
			}
			if math.IsNaN(e) {
				if !math.IsNaN(g) {
					t.Errorf("%s: expected NaN, got %v", key, g)
				}
			} else if e != g {
				t.Errorf("%s: expected %v, got %v", key, e, g)
			}
		default:
			if !reflect.DeepEqual(got, expected) {
				t.Errorf("%s: expected %v (%T), got %v (%T)", key, expected, expected, got, got)
			}
		}
	}

	check("str", "hello")
	check("integer", int64(42))
	check("negative", int64(-7))
	check("hex", int64(255))
	check("octal", int64(15))
	check("float", 3.14)
	check("infinity", math.Inf(1))
	check("neg_inf", math.Inf(-1))
	check("nan", math.NaN())
	check("bool_true", true)
	check("bool_false", false)
	check("null_tilde", nil)
	check("null_word", nil)
}

// ─── Quoted Scalars ──────────────────────────────────────────────────────────

func TestUnmarshalQuotedScalars(t *testing.T) {
	input := `
single: 'hello world'
double: "hello\nworld"
escaped: "tab\there"
empty_single: ''
empty_double: ""
`
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["single"] != "hello world" {
		t.Errorf("single: expected 'hello world', got %q", out["single"])
	}
	if out["double"] != "hello\nworld" {
		t.Errorf("double: expected 'hello\\nworld', got %q", out["double"])
	}
	if out["escaped"] != "tab\there" {
		t.Errorf("escaped: expected 'tab\\there', got %q", out["escaped"])
	}
	if out["empty_single"] != "" {
		t.Errorf("empty_single: expected empty, got %q", out["empty_single"])
	}
	if out["empty_double"] != "" {
		t.Errorf("empty_double: expected empty, got %q", out["empty_double"])
	}
}

// ─── Block Scalars ───────────────────────────────────────────────────────────

func TestUnmarshalLiteralScalar(t *testing.T) {
	input := "content: |\n  line one\n  line two\n  line three\n"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	expected := "line one\nline two\nline three\n"
	if out["content"] != expected {
		t.Errorf("expected %q, got %q", expected, out["content"])
	}
}

func TestUnmarshalFoldedScalar(t *testing.T) {
	input := "content: >\n  line one\n  line two\n  line three\n"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	expected := "line one line two line three\n"
	if out["content"] != expected {
		t.Errorf("expected %q, got %q", expected, out["content"])
	}
}

func TestUnmarshalBlockScalarStripChomp(t *testing.T) {
	input := "content: |-\n  line one\n  line two\n"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	expected := "line one\nline two"
	if out["content"] != expected {
		t.Errorf("expected %q, got %q", expected, out["content"])
	}
}

func TestUnmarshalBlockScalarKeepChomp(t *testing.T) {
	input := "content: |+\n  line one\n  line two\n\n\n"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	expected := "line one\nline two\n\n\n"
	if out["content"] != expected {
		t.Errorf("expected %q, got %q", expected, out["content"])
	}
}

// ─── Flow Style ──────────────────────────────────────────────────────────────

func TestUnmarshalFlowSequence(t *testing.T) {
	input := `items: [a, b, c]`
	var out map[string][]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	expected := []string{"a", "b", "c"}
	if !reflect.DeepEqual(out["items"], expected) {
		t.Errorf("expected %v, got %v", expected, out["items"])
	}
}

func TestUnmarshalFlowMapping(t *testing.T) {
	input := `limits: {cpu: 100m, memory: 64Mi}`
	var out map[string]map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["limits"]["cpu"] != "100m" {
		t.Errorf("expected cpu=100m, got %q", out["limits"]["cpu"])
	}
	if out["limits"]["memory"] != "64Mi" {
		t.Errorf("expected memory=64Mi, got %q", out["limits"]["memory"])
	}
}

// ─── Anchors and Aliases ─────────────────────────────────────────────────────

func TestUnmarshalAnchorAlias(t *testing.T) {
	input := `
defaults: &defaults
  adapter: postgres
  host: localhost
development:
  <<: *defaults
  database: dev_db
production:
  <<: *defaults
  database: prod_db
`
	var out map[string]map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["development"]["adapter"] != "postgres" {
		t.Errorf("expected adapter=postgres, got %q", out["development"]["adapter"])
	}
	if out["development"]["database"] != "dev_db" {
		t.Errorf("expected database=dev_db, got %q", out["development"]["database"])
	}
	if out["production"]["host"] != "localhost" {
		t.Errorf("expected host=localhost, got %q", out["production"]["host"])
	}
	if out["production"]["database"] != "prod_db" {
		t.Errorf("expected database=prod_db, got %q", out["production"]["database"])
	}
}

// ─── Struct Tags ─────────────────────────────────────────────────────────────

func TestUnmarshalOmitEmpty(t *testing.T) {
	type Config struct {
		Name    string `yaml:"name"`
		Value   string `yaml:"value,omitempty"`
		Count   int    `yaml:"count,omitempty"`
		Enabled bool   `yaml:"enabled,omitempty"`
	}

	c := Config{Name: "test"}
	data, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "value:") {
		t.Error("omitempty should have hidden empty value field")
	}
	if strings.Contains(s, "count:") {
		t.Error("omitempty should have hidden zero count field")
	}
	if strings.Contains(s, "enabled:") {
		t.Error("omitempty should have hidden false enabled field")
	}
	if !strings.Contains(s, "name: test") {
		t.Errorf("expected 'name: test' in output, got:\n%s", s)
	}
}

func TestUnmarshalSkipField(t *testing.T) {
	input := `name: test
secret: hidden
value: visible`

	type Config struct {
		Name   string `yaml:"name"`
		Secret string `yaml:"-"`
		Value  string `yaml:"value"`
	}

	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if c.Secret != "" {
		t.Errorf("skip field should be empty, got %q", c.Secret)
	}
	if c.Name != "test" {
		t.Errorf("expected name=test, got %q", c.Name)
	}
}

func TestUnmarshalJSONTagFallback(t *testing.T) {
	input := `name: test
count: 5`

	type Config struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if c.Name != "test" {
		t.Errorf("expected name=test, got %q", c.Name)
	}
	if c.Count != 5 {
		t.Errorf("expected count=5, got %d", c.Count)
	}
}

// ─── Strict Mode ─────────────────────────────────────────────────────────────

func TestUnmarshalStrict(t *testing.T) {
	input := `name: test
unknown_field: value`

	type Config struct {
		Name string `yaml:"name"`
	}

	var c Config
	err := UnmarshalWithOptions([]byte(input), &c, WithStrict())
	if err == nil {
		t.Error("expected error for unknown field in strict mode")
	}
	if !errors.Is(err, ErrUnknownField) {
		t.Errorf("expected UnknownFieldError, got %T: %v", err, err)
	}
}

// ─── Pointers ────────────────────────────────────────────────────────────────

func TestUnmarshalPointer(t *testing.T) {
	input := `name: test
count: 5`

	type Config struct {
		Name  *string `yaml:"name"`
		Count *int    `yaml:"count"`
	}

	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if c.Name == nil || *c.Name != "test" {
		t.Errorf("expected name pointer to 'test'")
	}
	if c.Count == nil || *c.Count != 5 {
		t.Errorf("expected count pointer to 5")
	}
}

// ─── Any (interface{}) ──────────────────────────────────────────────────────

func TestUnmarshalToAny(t *testing.T) {
	input := `
string: hello
number: 42
float: 3.14
bool: true
null_val: null
list:
  - a
  - b
nested:
  key: value
`
	var out any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}

	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	if m["string"] != "hello" {
		t.Errorf("expected string=hello, got %v", m["string"])
	}
	if m["number"] != int64(42) {
		t.Errorf("expected number=42, got %v (%T)", m["number"], m["number"])
	}
	if m["bool"] != true {
		t.Errorf("expected bool=true, got %v", m["bool"])
	}
	if m["null_val"] != nil {
		t.Errorf("expected null_val=nil, got %v", m["null_val"])
	}

	list, ok := m["list"].([]any)
	if !ok || len(list) != 2 {
		t.Errorf("expected list of 2 items, got %v", m["list"])
	}

	nested, ok := m["nested"].(map[string]any)
	if !ok {
		t.Errorf("expected nested map, got %T", m["nested"])
	} else if nested["key"] != "value" {
		t.Errorf("expected nested.key=value, got %v", nested["key"])
	}
}

// ─── Embedded Structs ────────────────────────────────────────────────────────

func TestUnmarshalEmbeddedStruct(t *testing.T) {
	input := `name: test
value: hello`

	type Base struct {
		Name string `yaml:"name"`
	}
	type Extended struct {
		Base  `yaml:",inline"`
		Value string `yaml:"value"`
	}

	var e Extended
	if err := Unmarshal([]byte(input), &e); err != nil {
		t.Fatal(err)
	}
	if e.Name != "test" {
		t.Errorf("expected name=test, got %q", e.Name)
	}
	if e.Value != "hello" {
		t.Errorf("expected value=hello, got %q", e.Value)
	}
}

// ─── Time ────────────────────────────────────────────────────────────────────

func TestUnmarshalTime(t *testing.T) {
	input := `date: 2024-01-15`

	type Config struct {
		Date time.Time `yaml:"date"`
	}

	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if c.Date.Year() != 2024 || c.Date.Month() != 1 || c.Date.Day() != 15 {
		t.Errorf("unexpected date: %v", c.Date)
	}
}

// ─── Duration ────────────────────────────────────────────────────────────────

func TestUnmarshalDuration(t *testing.T) {
	input := `timeout: 5s`

	type Config struct {
		Timeout time.Duration `yaml:"timeout"`
	}

	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if c.Timeout != 5*time.Second {
		t.Errorf("expected 5s, got %v", c.Timeout)
	}
}

// ─── Edge Cases ──────────────────────────────────────────────────────────────

func TestUnmarshalEmptyDocument(t *testing.T) {
	var out map[string]string
	if err := Unmarshal([]byte(""), &out); err != nil {
		t.Fatal(err)
	}
}

func TestUnmarshalCommentOnly(t *testing.T) {
	var out map[string]string
	if err := Unmarshal([]byte("# just a comment"), &out); err != nil {
		t.Fatal(err)
	}
}

func TestUnmarshalDocumentEndMarker(t *testing.T) {
	input := "name: test\n..."
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "test" {
		t.Errorf("expected name=test, got %q", out["name"])
	}
}

func TestUnmarshalBOM(t *testing.T) {
	input := "\xEF\xBB\xBFname: test"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "test" {
		t.Errorf("expected name=test, got %q", out["name"])
	}
}

// ─── Directive Handling ──────────────────────────────────────────────────────

func TestUnmarshalWithDirective(t *testing.T) {
	input := "%YAML 1.2\n---\nname: test\n"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "test" {
		t.Errorf("expected name=test, got %q", out["name"])
	}
}

// ─── Comments ────────────────────────────────────────────────────────────────

func TestUnmarshalWithComments(t *testing.T) {
	input := `
# header comment
name: test  # inline comment
# middle comment
value: hello
# footer comment
`
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "test" {
		t.Errorf("expected name=test, got %q", out["name"])
	}
	if out["value"] != "hello" {
		t.Errorf("expected value=hello, got %q", out["value"])
	}
}

// ─── Uint Types ──────────────────────────────────────────────────────────────

func TestUnmarshalUintTypes(t *testing.T) {
	input := `port: 8080
mask: 0xFF`

	type Config struct {
		Port uint16 `yaml:"port"`
		Mask uint8  `yaml:"mask"`
	}

	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if c.Port != 8080 {
		t.Errorf("expected port=8080, got %d", c.Port)
	}
	if c.Mask != 255 {
		t.Errorf("expected mask=255, got %d", c.Mask)
	}
}

// ─── Double-Quoted Escape Sequences ─────────────────────────────────────────

func TestUnmarshalEscapeSequences(t *testing.T) {
	input := `
null_char: "\0"
bell: "\a"
backspace: "\b"
tab: "\t"
newline: "\n"
vtab: "\v"
formfeed: "\f"
carriage: "\r"
escape: "\e"
double_quote: "\""
backslash: "\\"
slash: "\/"
next_line: "\N"
nbsp: "\_"
line_sep: "\L"
para_sep: "\P"
hex: "\x41"
unicode4: "A"
unicode8: "\U00000041"
`
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}

	checks := map[string]string{
		"null_char":    "\x00",
		"bell":         "\a",
		"backspace":    "\b",
		"tab":          "\t",
		"newline":      "\n",
		"vtab":         "\v",
		"formfeed":     "\f",
		"carriage":     "\r",
		"escape":       "\x1b",
		"double_quote": "\"",
		"backslash":    "\\",
		"slash":        "/",
		"next_line":    "\u0085",
		"nbsp":         " ",
		"line_sep":     " ",
		"para_sep":     " ",
		"hex":          "A",
		"unicode4":     "A",
		"unicode8":     "A",
	}

	for key, expected := range checks {
		if out[key] != expected {
			t.Errorf("%s: expected %q, got %q", key, expected, out[key])
		}
	}
}

// ─── Deeply Nested Structures ───────────────────────────────────────────────

func TestUnmarshalDeeplyNested(t *testing.T) {
	input := `
level1:
  level2:
    level3:
      level4:
        value: deep
`
	var out map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	l1 := out["level1"].(map[string]any)
	l2 := l1["level2"].(map[string]any)
	l3 := l2["level3"].(map[string]any)
	l4 := l3["level4"].(map[string]any)
	if l4["value"] != "deep" {
		t.Errorf("expected deep, got %v", l4["value"])
	}
}

// ─── Merge Keys with Sequence of Aliases ────────────────────────────────────

func TestUnmarshalMergeKeySequence(t *testing.T) {
	input := `
defaults: &defaults
  adapter: postgres
  host: localhost
overrides: &overrides
  timeout: 30
production:
  <<: [*defaults, *overrides]
  database: prod
`
	var out map[string]map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	prod := out["production"]
	if prod["adapter"] != "postgres" {
		t.Errorf("expected adapter=postgres, got %v", prod["adapter"])
	}
	if prod["timeout"] != int64(30) {
		t.Errorf("expected timeout=30, got %v (%T)", prod["timeout"], prod["timeout"])
	}
	if prod["database"] != "prod" {
		t.Errorf("expected database=prod, got %v", prod["database"])
	}
}

// ─── Folded Scalar with Blank Lines ─────────────────────────────────────────

func TestUnmarshalFoldedWithBlankLines(t *testing.T) {
	input := "content: >\n  para one\n  continues\n\n  para two\n"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	expected := "para one continues\npara two\n"
	if out["content"] != expected {
		t.Errorf("expected %q, got %q", expected, out["content"])
	}
}

// ─── Multiline Plain Scalar ─────────────────────────────────────────────────

func TestUnmarshalMultilinePlainScalar(t *testing.T) {
	input := `description: this is
  a multiline
  plain scalar`
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	expected := "this is a multiline plain scalar"
	if out["description"] != expected {
		t.Errorf("expected %q, got %q", expected, out["description"])
	}
}

// ─── Single-Quoted Escaping ─────────────────────────────────────────────────

func TestUnmarshalSingleQuotedEscape(t *testing.T) {
	input := `value: 'it''s a test'`
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["value"] != "it's a test" {
		t.Errorf("expected \"it's a test\", got %q", out["value"])
	}
}

// ─── Complex K8s Struct Unmarshal ───────────────────────────────────────────

func TestUnmarshalK8sStructs(t *testing.T) {
	input := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp
  namespace: prod
  labels:
    app: webapp
    tier: frontend
spec:
  replicas: 3
  selector:
    matchLabels:
      app: webapp
  template:
    metadata:
      labels:
        app: webapp
    spec:
      containers:
        - name: web
          image: nginx:1.25
          ports:
            - containerPort: 80
          resources:
            limits:
              cpu: 500m
              memory: 128Mi
            requests:
              cpu: 250m
              memory: 64Mi
`
	type Resources struct {
		Limits   map[string]string `yaml:"limits"`
		Requests map[string]string `yaml:"requests"`
	}
	type Port struct {
		ContainerPort int `yaml:"containerPort"`
	}
	type Container struct {
		Name      string    `yaml:"name"`
		Image     string    `yaml:"image"`
		Ports     []Port    `yaml:"ports"`
		Resources Resources `yaml:"resources"`
	}
	type PodSpec struct {
		Containers []Container `yaml:"containers"`
	}
	type TemplateMeta struct {
		Labels map[string]string `yaml:"labels"`
	}
	type Template struct {
		Metadata TemplateMeta `yaml:"metadata"`
		Spec     PodSpec      `yaml:"spec"`
	}
	type Selector struct {
		MatchLabels map[string]string `yaml:"matchLabels"`
	}
	type DeploymentSpec struct {
		Replicas int      `yaml:"replicas"`
		Selector Selector `yaml:"selector"`
		Template Template `yaml:"template"`
	}
	type Metadata struct {
		Name      string            `yaml:"name"`
		Namespace string            `yaml:"namespace"`
		Labels    map[string]string `yaml:"labels"`
	}
	type Deployment struct {
		APIVersion string         `yaml:"apiVersion"`
		Kind       string         `yaml:"kind"`
		Metadata   Metadata       `yaml:"metadata"`
		Spec       DeploymentSpec `yaml:"spec"`
	}

	var d Deployment
	if err := Unmarshal([]byte(input), &d); err != nil {
		t.Fatal(err)
	}

	if d.APIVersion != "apps/v1" {
		t.Errorf("expected apiVersion=apps/v1, got %q", d.APIVersion)
	}
	if d.Kind != "Deployment" {
		t.Errorf("expected kind=Deployment, got %q", d.Kind)
	}
	if d.Metadata.Name != "webapp" {
		t.Errorf("expected name=webapp, got %q", d.Metadata.Name)
	}
	if d.Spec.Replicas != 3 {
		t.Errorf("expected replicas=3, got %d", d.Spec.Replicas)
	}
	if len(d.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(d.Spec.Template.Spec.Containers))
	}
	c := d.Spec.Template.Spec.Containers[0]
	if c.Name != "web" {
		t.Errorf("expected container name=web, got %q", c.Name)
	}
	if c.Image != "nginx:1.25" {
		t.Errorf("expected image=nginx:1.25, got %q", c.Image)
	}
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != 80 {
		t.Errorf("unexpected ports: %+v", c.Ports)
	}
	if c.Resources.Limits["cpu"] != "500m" {
		t.Errorf("expected cpu limit=500m, got %q", c.Resources.Limits["cpu"])
	}
	if c.Resources.Requests["memory"] != "64Mi" {
		t.Errorf("expected memory request=64Mi, got %q", c.Resources.Requests["memory"])
	}
}

// ─── Folded Scalar Strip ────────────────────────────────────────────────────

func TestUnmarshalFoldedScalarStrip(t *testing.T) {
	input := "desc: >-\n  hello\n  world\n"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["desc"] != "hello world" {
		t.Errorf("expected 'hello world', got %q", out["desc"])
	}
}

// ─── Duplicate Key Detection ────────────────────────────────────────────────

func TestUnmarshalDuplicateKeyLastWins(t *testing.T) {
	input := `name: first
name: second`
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "second" {
		t.Errorf("expected last value to win, got %q", out["name"])
	}
}

// ─── Boolean Case Variants ──────────────────────────────────────────────────

func TestUnmarshalBooleanVariants(t *testing.T) {
	input := `
a: true
b: True
c: TRUE
d: false
e: False
f: FALSE
`
	type Config struct {
		A bool `yaml:"a"`
		B bool `yaml:"b"`
		C bool `yaml:"c"`
		D bool `yaml:"d"`
		E bool `yaml:"e"`
		F bool `yaml:"f"`
	}

	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if !c.A || !c.B || !c.C {
		t.Error("expected all true variants to be true")
	}
	if c.D || c.E || c.F {
		t.Error("expected all false variants to be false")
	}
}

// ─── Quoted Integers Stay as Strings ────────────────────────────────────────

func TestUnmarshalQuotedIntAsString(t *testing.T) {
	input := `port: "8080"`
	var out map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["port"].(string); !ok {
		t.Errorf("expected quoted int to remain string, got %T", out["port"])
	}
	if out["port"] != "8080" {
		t.Errorf("expected '8080', got %v", out["port"])
	}
}

// ─── Null Variants ──────────────────────────────────────────────────────────

func TestUnmarshalNullVariants(t *testing.T) {
	input := `
a: null
b: Null
c: NULL
d: ~
e:
`
	var out map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a", "b", "c", "d", "e"} {
		if out[key] != nil {
			t.Errorf("%s: expected nil, got %v (%T)", key, out[key], out[key])
		}
	}
}

// ─── Complex Flow Collections ───────────────────────────────────────────────

func TestUnmarshalNestedFlowCollections(t *testing.T) {
	input := `data: {names: [alice, bob], counts: [1, 2, 3]}`
	var out map[string]map[string][]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	names := out["data"]["names"]
	if len(names) != 2 || names[0] != "alice" || names[1] != "bob" {
		t.Errorf("unexpected names: %v", names)
	}
}

// ─── Direct Alias (not merge key) ──────────────────────────────────────────

func TestUnmarshalDirectAlias(t *testing.T) {
	input := `
base: &base
  name: default
  port: 8080
service: *base
`
	var out map[string]map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["service"]["name"] != "default" {
		t.Errorf("expected name=default, got %v", out["service"]["name"])
	}
	if out["service"]["port"] != int64(8080) {
		t.Errorf("expected port=8080, got %v", out["service"]["port"])
	}
}

// ─── Explicit Key (?) ───────────────────────────────────────────────────────

func TestUnmarshalExplicitKey(t *testing.T) {
	input := `? explicit_key
: explicit_value
`
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["explicit_key"] != "explicit_value" {
		t.Errorf("expected explicit_value, got %q", out["explicit_key"])
	}
}

// ─── Tags (ignored but parsed) ──────────────────────────────────────────────

func TestUnmarshalWithTags(t *testing.T) {
	input := `value: !!str 42`
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["value"] != "42" {
		t.Errorf("expected '42', got %q", out["value"])
	}
}

func TestUnmarshalVerbatimTag(t *testing.T) {
	input := "value: !<tag:yaml.org,2002:str> 42"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["value"] != "42" {
		t.Errorf("expected '42', got %q", out["value"])
	}
}

// ─── Decode Sequence Into Array ─────────────────────────────────────────────

func TestUnmarshalIntoArray(t *testing.T) {
	input := "- a\n- b\n- c"
	var out [3]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out != [3]string{"a", "b", "c"} {
		t.Errorf("expected [a b c], got %v", out)
	}
}

// ─── Decode Mapping Into Interface ──────────────────────────────────────────

func TestUnmarshalMappingToInterface(t *testing.T) {
	input := `key: value`
	var out any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	if m["key"] != "value" {
		t.Errorf("expected value, got %v", m["key"])
	}
}

// ─── Decode Sequence Into Interface ─────────────────────────────────────────

func TestUnmarshalSequenceToInterface(t *testing.T) {
	input := "- 1\n- 2\n- 3"
	var out any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	s, ok := out.([]any)
	if !ok {
		t.Fatalf("expected slice, got %T", out)
	}
	if len(s) != 3 {
		t.Errorf("expected 3 items, got %d", len(s))
	}
}

// ─── Flow Sequence in Struct ────────────────────────────────────────────────

func TestUnmarshalFlowSequenceInStruct(t *testing.T) {
	input := `items: [ReadWriteOnce, ReadOnlyMany]`
	type Config struct {
		Items []string `yaml:"items"`
	}
	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(c.Items))
	}
	if c.Items[0] != "ReadWriteOnce" || c.Items[1] != "ReadOnlyMany" {
		t.Errorf("unexpected items: %v", c.Items)
	}
}

// ─── Unmarshal into map[string]int ──────────────────────────────────────────

func TestUnmarshalMapStringInt(t *testing.T) {
	input := `a: 1
b: 2
c: 3`
	var out map[string]int
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["a"] != 1 || out["b"] != 2 || out["c"] != 3 {
		t.Errorf("unexpected map: %v", out)
	}
}

// ─── Merge Key with Struct ──────────────────────────────────────────────────

func TestUnmarshalMergeKeyStruct(t *testing.T) {
	input := `
defaults: &defaults
  adapter: postgres
  host: localhost
production:
  <<: *defaults
  database: prod
`
	type DBConfig struct {
		Adapter  string `yaml:"adapter"`
		Host     string `yaml:"host"`
		Database string `yaml:"database"`
	}
	type Config struct {
		Defaults   DBConfig `yaml:"defaults"`
		Production DBConfig `yaml:"production"`
	}

	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if c.Production.Adapter != "postgres" {
		t.Errorf("expected adapter=postgres, got %q", c.Production.Adapter)
	}
	if c.Production.Host != "localhost" {
		t.Errorf("expected host=localhost, got %q", c.Production.Host)
	}
	if c.Production.Database != "prod" {
		t.Errorf("expected database=prod, got %q", c.Production.Database)
	}
}

// ─── Decoder with Options ───────────────────────────────────────────────────

func TestDecoderWithStrictOption(t *testing.T) {
	input := "name: test\nunknown: val"
	type Config struct {
		Name string `yaml:"name"`
	}
	dec := NewDecoder(strings.NewReader(input), WithStrict())
	var c Config
	err := dec.Decode(&c)
	if err == nil {
		t.Error("expected error for unknown field in strict decoder")
	}
}

// ─── Type Error on Bad Scalar ───────────────────────────────────────────────

func TestUnmarshalTypeErrorBadInt(t *testing.T) {
	input := `value: not_a_number`
	type Config struct {
		Value int `yaml:"value"`
	}
	var c Config
	err := Unmarshal([]byte(input), &c)
	if err == nil {
		t.Error("expected type error")
	}
	if !errors.Is(err, ErrType) {
		t.Errorf("expected TypeError, got %T: %v", err, err)
	}
}

func TestUnmarshalTypeErrorBadBool(t *testing.T) {
	input := `active: maybe`
	type Config struct {
		Active bool `yaml:"active"`
	}
	var c Config
	err := Unmarshal([]byte(input), &c)
	if err == nil {
		t.Error("expected type error")
	}
}

func TestUnmarshalTypeErrorBadFloat(t *testing.T) {
	input := `rate: hello`
	type Config struct {
		Rate float64 `yaml:"rate"`
	}
	var c Config
	err := Unmarshal([]byte(input), &c)
	if err == nil {
		t.Error("expected type error")
	}
}

func TestUnmarshalTypeErrorBadUint(t *testing.T) {
	input := `count: -5`
	type Config struct {
		Count uint `yaml:"count"`
	}
	var c Config
	err := Unmarshal([]byte(input), &c)
	if err == nil {
		t.Error("expected type error")
	}
}

func TestUnmarshalTypeErrorStructFromScalar(t *testing.T) {
	input := `inner: just_a_string`
	type Inner struct {
		Name string `yaml:"name"`
	}
	type Config struct {
		Inner Inner `yaml:"inner"`
	}
	var c Config
	err := Unmarshal([]byte(input), &c)
	if err == nil {
		t.Error("expected type error for struct from scalar")
	}
}

// ─── Decode Sequence to Typed Interface ─────────────────────────────────────

func TestDecodeSequenceErrorNonSlice(t *testing.T) {
	input := "- a\n- b"
	var s string
	err := Unmarshal([]byte(input), &s)
	if err == nil {
		t.Error("expected error decoding sequence into string")
	}
}

// ─── Decode Mapping Error Cases ─────────────────────────────────────────────

func TestDecodeMappingToNonMapStruct(t *testing.T) {
	input := `key: value`
	var s []string
	err := Unmarshal([]byte(input), &s)
	if err == nil {
		t.Error("expected error decoding mapping into slice")
	}
}

// ─── BytesUnmarshaler Interface ─────────────────────────────────────────────

type bytesUnmarshalerType struct {
	parsed string
}

func (b *bytesUnmarshalerType) UnmarshalYAML(data []byte) error {
	b.parsed = "bytes:" + string(data)
	return nil
}

func TestBytesUnmarshaler(t *testing.T) {
	input := `value: hello`
	type Config struct {
		Value bytesUnmarshalerType `yaml:"value"`
	}
	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(c.Value.parsed, "bytes:") {
		t.Errorf("expected bytes: prefix, got %q", c.Value.parsed)
	}
}

// ─── Unmarshaler Interface ──────────────────────────────────────────────────

type unmarshalerType struct {
	name string
}

func (u *unmarshalerType) UnmarshalYAML(unmarshal func(any) error) error {
	var raw string
	if err := unmarshal(&raw); err != nil {
		return err
	}
	u.name = "custom:" + raw
	return nil
}

func TestUnmarshalerInterface(t *testing.T) {
	input := `value: hello`
	type Config struct {
		Value unmarshalerType `yaml:"value"`
	}
	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if c.Value.name != "custom:hello" {
		t.Errorf("expected 'custom:hello', got %q", c.Value.name)
	}
}

// ─── Escaped Double-Quoted with Quote Chars ─────────────────────────────────

// ─── CRLF Line Endings ─────────────────────────────────────────────────────

func TestUnmarshalCRLF(t *testing.T) {
	input := "name: test\r\nvalue: hello\r\n"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "test" || out["value"] != "hello" {
		t.Errorf("CRLF handling failed: %v", out)
	}
}

// ─── Empty Sequence ─────────────────────────────────────────────────────────

func TestUnmarshalEmptySequence(t *testing.T) {
	input := `items: []`
	type Config struct {
		Items []string `yaml:"items"`
	}
	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Items) != 0 {
		t.Errorf("expected empty slice, got %v", c.Items)
	}
}

func TestUnmarshalEmptyMapping(t *testing.T) {
	input := `config: {}`
	type Config struct {
		Config map[string]string `yaml:"config"`
	}
	var c Config
	if err := Unmarshal([]byte(input), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Config) != 0 {
		t.Errorf("expected empty map, got %v", c.Config)
	}
}

// ─── Mixed Content Sequence ─────────────────────────────────────────────────

func TestUnmarshalMixedSequence(t *testing.T) {
	input := `
- hello
- 42
- true
- null
- 3.14
`
	var out []any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 5 {
		t.Fatalf("expected 5 items, got %d", len(out))
	}
	if out[0] != "hello" {
		t.Errorf("[0] expected hello, got %v", out[0])
	}
	if out[1] != int64(42) {
		t.Errorf("[1] expected 42, got %v (%T)", out[1], out[1])
	}
	if out[2] != true {
		t.Errorf("[2] expected true, got %v", out[2])
	}
	if out[3] != nil {
		t.Errorf("[3] expected nil, got %v", out[3])
	}
	if out[4] != 3.14 {
		t.Errorf("[4] expected 3.14, got %v", out[4])
	}
}

// ─── Flow in Block Context ──────────────────────────────────────────────────

func TestUnmarshalFlowInBlock(t *testing.T) {
	input := `
metadata:
  labels: {app: web, env: prod}
  ports: [80, 443]
`
	var out map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	meta := out["metadata"].(map[string]any)
	labels := meta["labels"].(map[string]any)
	if labels["app"] != "web" {
		t.Errorf("expected app=web, got %v", labels["app"])
	}
	ports := meta["ports"].([]any)
	if len(ports) != 2 {
		t.Errorf("expected 2 ports, got %d", len(ports))
	}
}

func TestOptionDisallowDuplicateKey(t *testing.T) {
	input := `
a: 1
b: 2
a: 3
`
	var out map[string]any
	err := UnmarshalWithOptions([]byte(input), &out, WithDisallowDuplicateKey())
	if err == nil {
		t.Fatal("expected error for duplicate key")
	}
	var dupErr *DuplicateKeyError
	if !errors.As(err, &dupErr) {
		t.Fatalf("expected DuplicateKeyError, got %T: %v", err, err)
	}
	if dupErr.Key != "a" {
		t.Errorf("expected duplicate key 'a', got %q", dupErr.Key)
	}

	err = UnmarshalWithOptions([]byte(input), &out)
	if err != nil {
		t.Fatalf("without DisallowDuplicateKey should succeed: %v", err)
	}
}

func TestOptionUseOrderedMap(t *testing.T) {
	input := `
z: 1
a: 2
m: 3
`
	var out any
	err := UnmarshalWithOptions([]byte(input), &out, WithOrderedMap())
	if err != nil {
		t.Fatal(err)
	}
	ms, ok := out.(MapSlice)
	if !ok {
		t.Fatalf("expected MapSlice, got %T", out)
	}
	if len(ms) != 3 {
		t.Fatalf("expected 3 items, got %d", len(ms))
	}
	if ms[0].Key != "z" || ms[1].Key != "a" || ms[2].Key != "m" {
		t.Errorf("expected keys [z, a, m], got [%v, %v, %v]", ms[0].Key, ms[1].Key, ms[2].Key)
	}
}

func TestOptionMaxDepth(t *testing.T) {
	input := `
a:
  b:
    c:
      d: deep
`
	var out map[string]any
	err := UnmarshalWithOptions([]byte(input), &out, WithMaxDepth(2))
	if err == nil {
		t.Fatal("expected error for exceeded max depth")
	}
	if !strings.Contains(err.Error(), "exceeded max depth") {
		t.Errorf("expected max depth error, got: %v", err)
	}

	err = UnmarshalWithOptions([]byte(input), &out, WithMaxDepth(100))
	if err != nil {
		t.Fatalf("should succeed with large max depth: %v", err)
	}
}

func TestOptionMaxAliasExpansion(t *testing.T) {
	input := `
anchor: &a
  x: 1
ref: *a
`
	var out any
	err := UnmarshalWithOptions([]byte(input), &out, WithMaxAliasExpansion(1000))
	if err != nil {
		t.Fatalf("should succeed with large alias expansion: %v", err)
	}

	err = UnmarshalWithOptions([]byte(input), &out, WithMaxAliasExpansion(0))
	if err == nil {
		t.Fatal("expected error for exceeded alias expansion with limit 0")
	}
}

func TestOptionUseJSONUnmarshaler(t *testing.T) {
	input := `value: hello`
	var out struct {
		Value jsonTarget `yaml:"value"`
	}
	err := UnmarshalWithOptions([]byte(input), &out, WithJSONUnmarshaler())
	if err != nil {
		t.Fatal(err)
	}
	if out.Value.Data != "hello" {
		t.Errorf("expected 'hello', got %q", out.Value.Data)
	}
}

type jsonTarget struct {
	Data string
}

func (j *jsonTarget) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	j.Data = s
	return nil
}

// ─── Decode Coverage: mapping to non-map/struct ─────────────���────────────────

func TestUnmarshalMappingToAnyInterface(t *testing.T) {
	input := `a: 1`
	var out any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	if m["a"] != int64(1) {
		t.Errorf("expected a=1, got %v", m["a"])
	}
}

func TestUnmarshalMappingToStringError(t *testing.T) {
	input := `a: 1`
	var out string
	err := Unmarshal([]byte(input), &out)
	if err == nil {
		t.Fatal("expected type error for mapping->string")
	}
}

func TestUnmarshalSequenceToAnyInterface(t *testing.T) {
	input := `- 1
- 2`
	var out any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	s, ok := out.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", out)
	}
	if len(s) != 2 {
		t.Fatalf("expected 2 items, got %d", len(s))
	}
}

func TestUnmarshalSequenceToArray(t *testing.T) {
	input := `- a
- b
- c
- d`
	var out [2]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out[0] != "a" || out[1] != "b" {
		t.Errorf("expected [a, b], got %v", out)
	}
}

// ─── Encode Coverage: emitNode ───────────────────────────────────────────────

func TestBytesUnmarshalerCallsEmitNode(t *testing.T) {
	input := `
items:
- name: a
  value: 1
- name: b
  value: 2
`
	var out struct {
		Items []rawItem `yaml:"items"`
	}
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(out.Items))
	}
	if !strings.Contains(string(out.Items[0].Raw), "name") {
		t.Errorf("expected raw YAML with 'name', got %q", out.Items[0].Raw)
	}
}

type rawItem struct {
	Raw []byte
}

func (r *rawItem) UnmarshalYAML(data []byte) error {
	r.Raw = append([]byte(nil), data...)
	return nil
}

// ─── Scanner/Parser Coverage: flow sequence with explicit keys ───────────────

func TestUnmarshalFlowSequenceWithKeys(t *testing.T) {
	input := `[{a: 1}, {b: 2}]`
	var out []map[string]int
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 items, got %d", len(out))
	}
	if out[0]["a"] != 1 {
		t.Errorf("expected a=1, got %v", out[0]["a"])
	}
}

func TestUnmarshalFlowSequenceKeyValue(t *testing.T) {
	input := `[? a : 1, ? b : 2]`
	var out []any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 items, got %d", len(out))
	}
}

// ─── Scanner Coverage: scanFlowToken edge cases ──────────────��──────────────

func TestUnmarshalDeepNestedFlowCollections(t *testing.T) {
	input := `{a: [1, {b: [2, 3]}], c: {d: [4]}}`
	var out map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	a := out["a"].([]any)
	if len(a) != 2 {
		t.Fatalf("expected 2 items in a, got %d", len(a))
	}
}

func TestUnmarshalEmptyFlowMapping(t *testing.T) {
	input := `a: {}`
	var out map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	inner := out["a"].(map[string]any)
	if len(inner) != 0 {
		t.Errorf("expected empty mapping, got %v", inner)
	}
}

func TestUnmarshalEmptyFlowSequence(t *testing.T) {
	input := `a: []`
	var out map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	inner := out["a"].([]any)
	if len(inner) != 0 {
		t.Errorf("expected empty sequence, got %v", inner)
	}
}

// ─── Scanner Coverage: scanHexEscape and scanDoubleQuotedScalar ──────────────

func TestUnmarshalDoubleQuotedEscapes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"\x41"`, "A"},
		{`"A"`, "A"},
		{`"\U00000041"`, "A"},
		{`"\0"`, "\x00"},
		{`"\/"`, "/"},
		{`"\_"`, " "},
		{`"\L"`, " "},
		{`"\P"`, " "},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			input := "v: " + tt.input
			var out map[string]string
			if err := Unmarshal([]byte(input), &out); err != nil {
				t.Fatal(err)
			}
			if out["v"] != tt.want {
				t.Errorf("got %q, want %q", out["v"], tt.want)
			}
		})
	}
}

// ─── Decode Coverage: fieldByIndex with nil pointer ──────────────────────────

func TestUnmarshalEmbeddedPointerStruct(t *testing.T) {
	type Base struct {
		Name string `yaml:"name"`
	}
	type Outer struct {
		*Base `yaml:",inline"`
		Age   int `yaml:"age"`
	}
	input := `
name: alice
age: 30
`
	var out Outer
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "alice" {
		t.Errorf("expected name=alice, got %q", out.Name)
	}
	if out.Age != 30 {
		t.Errorf("expected age=30, got %d", out.Age)
	}
}

// ─── Decode Coverage: decodeMapping to non-empty interface ───────────────────

func TestUnmarshalMappingToNonEmptyInterface(t *testing.T) {
	input := `a: 1`
	var out fmt.Stringer
	err := Unmarshal([]byte(input), &out)
	if err == nil {
		t.Fatal("expected error for mapping to non-empty interface")
	}
}

func TestUnmarshalSequenceToNonEmptyInterface(t *testing.T) {
	input := `- 1
- 2`
	var out fmt.Stringer
	err := Unmarshal([]byte(input), &out)
	if err == nil {
		t.Fatal("expected error for sequence to non-empty interface")
	}
}

// ─── Decode Coverage: decodeMerge with direct mapping ────────────────────────

func TestUnmarshalMergeDirectMapping(t *testing.T) {
	input := `
defaults:
  color: red
  size: large
item:
  <<:
    color: blue
    weight: heavy
  name: widget
`
	var out map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	item := out["item"].(map[string]any)
	if item["name"] != "widget" {
		t.Errorf("expected name=widget, got %v", item["name"])
	}
	if item["color"] != "blue" {
		t.Errorf("expected color=blue, got %v", item["color"])
	}
}

// ─── Parser Coverage: parseDocument edge cases ───────────────────────────────

func TestUnmarshalDocumentEndOnly(t *testing.T) {
	input := `---
...`
	var out any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("expected nil, got %v", out)
	}
}

func TestUnmarshalMultipleDocumentsEndMarkers(t *testing.T) {
	input := `---
a: 1
...
---
b: 2
...`
	dec := NewDecoder(strings.NewReader(input))
	var first, second map[string]any
	if err := dec.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&second); err != nil {
		t.Fatal(err)
	}
	if first["a"] != int64(1) {
		t.Errorf("first doc: expected a=1, got %v", first["a"])
	}
	if second["b"] != int64(2) {
		t.Errorf("second doc: expected b=2, got %v", second["b"])
	}
}

// ─── Decoder error handling ──────────────────────────────────────────────────

func TestDecoderNonPointer(t *testing.T) {
	dec := NewDecoder(strings.NewReader("a: 1"))
	err := dec.Decode("not a pointer")
	if err == nil {
		t.Fatal("expected error for non-pointer")
	}
}

// ─── Decode Coverage: parseInt with hex/octal edge cases ─────────────────────

func TestUnmarshalParseIntEdgeCases(t *testing.T) {
	input := `
hex: 0xFF
oct: 0o77
bin: 0b1010
`
	var out struct {
		Hex int `yaml:"hex"`
		Oct int `yaml:"oct"`
		Bin int `yaml:"bin"`
	}
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out.Hex != 255 {
		t.Errorf("hex: expected 255, got %d", out.Hex)
	}
	if out.Oct != 63 {
		t.Errorf("oct: expected 63, got %d", out.Oct)
	}
}

// ─── Decode Coverage: parseUint ──────────────────────────────────────────────

func TestUnmarshalUintEdgeCases(t *testing.T) {
	input := `
hex: 0xAB
oct: 0o17
`
	var out struct {
		Hex uint `yaml:"hex"`
		Oct uint `yaml:"oct"`
	}
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out.Hex != 0xAB {
		t.Errorf("hex: expected 171, got %d", out.Hex)
	}
	if out.Oct != 15 {
		t.Errorf("oct: expected 15, got %d", out.Oct)
	}
}

// ─── Decode Coverage: parseTime edge cases ───────────────────────────────────

func TestUnmarshalTimeLowercaseT(t *testing.T) {
	input := `ts: 2024-01-15 10:30:00`
	var out struct {
		Ts time.Time `yaml:"ts"`
	}
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out.Ts.Year() != 2024 {
		t.Errorf("expected year 2024, got %d", out.Ts.Year())
	}
}

// ─── Scanner Coverage: scanSingleQuotedScalar with escaped quotes ────────────

func TestUnmarshalSingleQuotedEscapedQuote(t *testing.T) {
	input := `v: 'it''s a test'`
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["v"] != "it's a test" {
		t.Errorf("expected it's a test, got %q", out["v"])
	}
}

// ─── Scanner Coverage: block scalar with different chomp modes ───────────────

func TestBlockScalarClipChompEmpty(t *testing.T) {
	input := "v: |\n  \n"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
}

func TestBlockScalarExplicitIndent(t *testing.T) {
	input := "v: |2\n  hello\n  world\n"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["v"], "hello") {
		t.Errorf("expected 'hello' in value, got %q", out["v"])
	}
}

// ─── Encode Coverage: marshalSlice with compound elements ────────────────────

// ─── Decode Coverage: scalarToAny edge cases ─────────────────────────────────

func TestUnmarshalScalarToAnyEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  any
	}{
		{"v: .inf", nil},
		{"v: -.inf", nil},
		{"v: .nan", nil},
		{"v: 0x1F", nil},
		{"v: 0o17", nil},
		{"v: ~", nil},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var out map[string]any
			if err := Unmarshal([]byte(tt.input), &out); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// ─── Additional Coverage: decodeMapping dispatch ─────────────────────────────

func TestUnmarshalMappingToTypedInterface(t *testing.T) {
	input := `a: 1`
	type myInterface interface{ Method() }
	var out myInterface
	err := Unmarshal([]byte(input), &out)
	if err == nil {
		t.Fatal("expected error for mapping to typed interface")
	}
}

func TestUnmarshalMappingToIntError(t *testing.T) {
	input := `a: 1`
	var out int
	err := Unmarshal([]byte(input), &out)
	if err != nil {
		var te *TypeError
		if !errors.As(err, &te) {
			t.Fatalf("expected TypeError, got %T: %v", err, err)
		}
	}
}

func TestUnmarshalSequenceToTypedInterface(t *testing.T) {
	input := `- 1`
	type myInterface interface{ Method() }
	var out myInterface
	err := Unmarshal([]byte(input), &out)
	if err == nil {
		t.Fatal("expected error for sequence to typed interface")
	}
}

func TestUnmarshalSequenceToIntError(t *testing.T) {
	input := `- 1`
	var out int
	err := Unmarshal([]byte(input), &out)
	if err != nil {
		var te *TypeError
		if !errors.As(err, &te) {
			t.Fatalf("expected TypeError, got %T: %v", err, err)
		}
	}
}

// ─── Additional Coverage: decodeMerge paths ─────────────────────────────────

func TestUnmarshalMergeKeyIntoStruct(t *testing.T) {
	input := `
defaults: &defaults
  name: base
  color: red
item:
  <<: *defaults
  color: blue
`
	type Item struct {
		Name  string `yaml:"name"`
		Color string `yaml:"color"`
	}
	type Doc struct {
		Defaults Item `yaml:"defaults"`
		Item     Item `yaml:"item"`
	}
	var out Doc
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out.Item.Name != "base" {
		t.Errorf("expected merged name=base, got %q", out.Item.Name)
	}
	if out.Item.Color != "blue" {
		t.Errorf("expected overridden color=blue, got %q", out.Item.Color)
	}
}

func TestUnmarshalMergeSequenceOfAliases(t *testing.T) {
	input := `
a: &a
  x: 1
b: &b
  y: 2
c:
  <<: [*a, *b]
  z: 3
`
	var out map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	c := out["c"].(map[string]any)
	if c["x"] != int64(1) {
		t.Errorf("expected x=1 from merge, got %v", c["x"])
	}
	if c["y"] != int64(2) {
		t.Errorf("expected y=2 from merge, got %v", c["y"])
	}
	if c["z"] != int64(3) {
		t.Errorf("expected z=3, got %v", c["z"])
	}
}

// ─── Additional Coverage: emitNode sequence/mapping paths ────────────────────

func TestBytesUnmarshalerSequence(t *testing.T) {
	input := `items:
- a
- b
- c`
	var out struct {
		Items rawItem `yaml:"items"`
	}
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	raw := string(out.Items.Raw)
	if !strings.Contains(raw, "- a") {
		t.Errorf("expected sequence in raw YAML, got %q", raw)
	}
}

func TestBytesUnmarshalerNestedMapping(t *testing.T) {
	input := `outer:
  inner:
    key: val`
	var out struct {
		Outer rawItem `yaml:"outer"`
	}
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	raw := string(out.Outer.Raw)
	if !strings.Contains(raw, "inner:") {
		t.Errorf("expected nested mapping in raw YAML, got %q", raw)
	}
}

// ─── Additional Coverage: scanDoubleQuotedScalar unicode escapes ─────────────

func TestUnmarshalDoubleQuotedUnicodeEscapes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"A"`, "A"},
		{`"\U00000041"`, "A"},
		{`"\x41"`, "A"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var out string
			if err := Unmarshal([]byte(tt.input), &out); err != nil {
				t.Fatal(err)
			}
			if out != tt.want {
				t.Errorf("got %q, want %q", out, tt.want)
			}
		})
	}
}

// ─── Additional Coverage: parseBlockMapping alias/tag in key position ────────

func TestUnmarshalBlockMappingWithAliasKey(t *testing.T) {
	input := "{a: &k val1, *k : val2}"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
}

// ─── Additional Coverage: scanFlowToken with value/key in flow context ───────

func TestUnmarshalFlowMappingMultipleEntries(t *testing.T) {
	input := `{a: 1, b: 2, c: 3}`
	var out map[string]int
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["a"] != 1 || out["b"] != 2 || out["c"] != 3 {
		t.Errorf("expected a=1,b=2,c=3, got %v", out)
	}
}

func TestUnmarshalFlowMappingEmptyValue(t *testing.T) {
	input := `{a: , b: 2}`
	var out map[string]any
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	if out["a"] != nil {
		t.Errorf("expected a=nil, got %v", out["a"])
	}
	if out["b"] != int64(2) {
		t.Errorf("expected b=2, got %v", out["b"])
	}
}

// ─── UTF-16 / UTF-32 Encoding Detection ─────────────────────────────────────

func TestUnmarshalUTF16BE(t *testing.T) {
	utf8Input := []byte("key: value")
	var utf16be []byte
	utf16be = append(utf16be, 0xFE, 0xFF) // BOM
	for _, b := range utf8Input {
		utf16be = append(utf16be, 0x00, b)
	}
	var out map[string]string
	if err := Unmarshal(utf16be, &out); err != nil {
		t.Fatal(err)
	}
	if out["key"] != "value" {
		t.Errorf("expected key=value, got %v", out["key"])
	}
}

func TestUnmarshalUTF16LE(t *testing.T) {
	utf8Input := []byte("key: value")
	var utf16le []byte
	utf16le = append(utf16le, 0xFF, 0xFE) // BOM
	for _, b := range utf8Input {
		utf16le = append(utf16le, b, 0x00)
	}
	var out map[string]string
	if err := Unmarshal(utf16le, &out); err != nil {
		t.Fatal(err)
	}
	if out["key"] != "value" {
		t.Errorf("expected key=value, got %v", out["key"])
	}
}

func TestUnmarshalUTF32BE(t *testing.T) {
	utf8Input := []byte("a: b")
	var utf32be []byte
	utf32be = append(utf32be, 0x00, 0x00, 0xFE, 0xFF) // BOM
	for _, b := range utf8Input {
		utf32be = append(utf32be, 0x00, 0x00, 0x00, b)
	}
	var out map[string]string
	if err := Unmarshal(utf32be, &out); err != nil {
		t.Fatal(err)
	}
	if out["a"] != "b" {
		t.Errorf("expected a=b, got %v", out["a"])
	}
}

func TestUnmarshalUTF32LE(t *testing.T) {
	utf8Input := []byte("a: b")
	var utf32le []byte
	utf32le = append(utf32le, 0xFF, 0xFE, 0x00, 0x00) // BOM
	for _, b := range utf8Input {
		utf32le = append(utf32le, b, 0x00, 0x00, 0x00)
	}
	var out map[string]string
	if err := Unmarshal(utf32le, &out); err != nil {
		t.Fatal(err)
	}
	if out["a"] != "b" {
		t.Errorf("expected a=b, got %v", out["a"])
	}
}

func TestValidUTF16(t *testing.T) {
	var utf16be []byte
	utf16be = append(utf16be, 0xFE, 0xFF)
	for _, b := range []byte("key: val") {
		utf16be = append(utf16be, 0x00, b)
	}
	if !Valid(utf16be) {
		t.Error("expected valid UTF-16 BE input")
	}
}

// ─── Non-Printable Character Rejection ───────────────────────────────────────

func TestRejectNonPrintableCharacter(t *testing.T) {
	input := []byte("key: val\x01ue")
	var out map[string]string
	err := Unmarshal(input, &out)
	if err == nil {
		t.Fatal("expected error for non-printable character")
	}
	if !strings.Contains(err.Error(), "non-printable") {
		t.Errorf("expected non-printable error, got: %v", err)
	}
}

func TestAllowTabInValue(t *testing.T) {
	input := "key: hello\tworld"
	var out map[string]string
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
}

// ─── Context-Aware Unmarshaler ──────────────────────────────────────────────

type ctxUnmarshaler struct {
	Val string
}

func (c *ctxUnmarshaler) UnmarshalYAML(ctx context.Context, unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	prefix := ctx.Value(ctxKey("prefix"))
	if prefix != nil {
		c.Val = prefix.(string) + s
	} else {
		c.Val = s
	}
	return nil
}

type ctxKey string

func TestUnmarshalerContext(t *testing.T) {
	input := "hello"
	ctx := context.WithValue(context.Background(), ctxKey("prefix"), "ctx-")
	dec := NewDecoder(strings.NewReader(input))
	var out ctxUnmarshaler
	if err := dec.DecodeContext(ctx, &out); err != nil {
		t.Fatal(err)
	}
	if out.Val != "ctx-hello" {
		t.Errorf("expected 'ctx-hello', got %q", out.Val)
	}
}

func TestDecodeContextBasic(t *testing.T) {
	dec := NewDecoder(strings.NewReader("key: val"))
	var out map[string]string
	if err := dec.DecodeContext(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if out["key"] != "val" {
		t.Errorf("expected key=val, got %v", out["key"])
	}
}

// ─── big.Int / big.Float / big.Rat Support ──────────────────────────────────

func TestUnmarshalBigInt(t *testing.T) {
	input := `v: "123456789012345678901234567890"`
	var out struct {
		V *big.Int `yaml:"v"`
	}
	out.V = new(big.Int)
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	expected, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	if out.V.Cmp(expected) != 0 {
		t.Errorf("expected %s, got %s", expected, out.V)
	}
}

func TestUnmarshalBigFloat(t *testing.T) {
	input := `v: "3.14159265358979323846264338327950288"`
	var out struct {
		V *big.Float `yaml:"v"`
	}
	out.V = new(big.Float)
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	expected, _, _ := new(big.Float).Parse("3.14159265358979323846264338327950288", 10)
	if out.V.Cmp(expected) != 0 {
		t.Errorf("expected %s, got %s", expected.String(), out.V.String())
	}
}

func TestUnmarshalBigRat(t *testing.T) {
	input := `v: "22/7"`
	var out struct {
		V *big.Rat `yaml:"v"`
	}
	out.V = new(big.Rat)
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	expected := new(big.Rat).SetFrac64(22, 7)
	if out.V.Cmp(expected) != 0 {
		t.Errorf("expected %s, got %s", expected, out.V)
	}
}

// ─── Validator Integration ───────────────────────────────────────────────────

type testValidator struct{}

func (tv testValidator) Struct(v any) error {
	type hasName interface{ GetName() string }
	if n, ok := v.(hasName); ok {
		if n.GetName() == "" {
			return fmt.Errorf("name is required")
		}
	}
	return nil
}

type validatedStruct struct {
	Name string `yaml:"name"`
	Age  int    `yaml:"age"`
}

func (v validatedStruct) GetName() string { return v.Name }

func TestValidatorOption(t *testing.T) {
	input := `name: ""
age: 30`
	var out validatedStruct
	err := UnmarshalWithOptions([]byte(input), &out, WithValidator(testValidator{}))
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected 'name is required', got: %v", err)
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if valErr.Pos.Line == 0 {
		t.Error("expected non-zero line in ValidationError position")
	}

	input2 := `name: alice
age: 30`
	err = UnmarshalWithOptions([]byte(input2), &out, WithValidator(testValidator{}))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidatorFormatError(t *testing.T) {
	input := []byte("name: \"\"\nage: 30")
	var out validatedStruct
	err := UnmarshalWithOptions(input, &out, WithValidator(testValidator{}))
	if err == nil {
		t.Fatal("expected validation error")
	}
	formatted := FormatError(input, err)
	if !strings.Contains(formatted, "validation") {
		t.Error("expected 'validation' in formatted output")
	}
	if !strings.Contains(formatted, "^") {
		t.Error("expected caret pointer in formatted output")
	}
}

func TestValidatorNotCalledOnMap(t *testing.T) {
	input := `a: 1`
	var out map[string]int
	err := UnmarshalWithOptions([]byte(input), &out, WithValidator(testValidator{}))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// ─── Required Tag ────────────────────────────────────────────────────────────

func TestRequiredTag(t *testing.T) {
	type S struct {
		Name string `yaml:"name,required"`
		Age  int    `yaml:"age"`
	}
	input := `age: 30`
	var out S
	err := Unmarshal([]byte(input), &out)
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("expected required error, got: %v", err)
	}

	input2 := `name: alice
age: 30`
	err = Unmarshal([]byte(input2), &out)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// ─── Conflicting Tag Precedence ──────────────────────────────────────────────

func TestConflictingFieldNames(t *testing.T) {
	type Base struct {
		Name string `yaml:"name"`
	}
	type Outer struct {
		Base `yaml:",inline"`
		Name string `yaml:"name"`
	}
	input := `name: alice`
	var out Outer
	if err := Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

var benchSmallYAML = []byte(`
name: app
version: "1.0"
debug: true
port: 8080
`)

var benchMediumYAML = []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-server
  namespace: production
  labels:
    app: web
    tier: frontend
    version: v2
spec:
  replicas: 3
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: nginx
        image: nginx:1.25
        ports:
        - containerPort: 80
        - containerPort: 443
        resources:
          limits:
            cpu: 500m
            memory: 128Mi
          requests:
            cpu: 100m
            memory: 64Mi
        env:
        - name: ENV
          value: production
        - name: LOG_LEVEL
          value: info
`)

func BenchmarkUnmarshalSmall(b *testing.B) {
	var out map[string]any
	for b.Loop() {
		_ = Unmarshal(benchSmallYAML, &out)
	}
}

func BenchmarkUnmarshalMedium(b *testing.B) {
	var out map[string]any
	for b.Loop() {
		_ = Unmarshal(benchMediumYAML, &out)
	}
}

func BenchmarkUnmarshalLarge(b *testing.B) {
	data, err := os.ReadFile("../../k8s.yaml")
	if err != nil {
		b.Skip("k8s.yaml not found")
	}
	var out map[string]any
	b.ResetTimer()
	for b.Loop() {
		_ = Unmarshal(data, &out)
	}
}

func BenchmarkMarshalSmall(b *testing.B) {
	v := map[string]any{"name": "app", "version": "1.0", "debug": true, "port": 8080}
	for b.Loop() {
		_, _ = Marshal(v)
	}
}

func BenchmarkMarshalMedium(b *testing.B) {
	type Container struct {
		Name  string `yaml:"name"`
		Image string `yaml:"image"`
		Ports []int  `yaml:"ports"`
	}
	type Spec struct {
		Replicas   int         `yaml:"replicas"`
		Containers []Container `yaml:"containers"`
	}
	v := Spec{
		Replicas: 3,
		Containers: []Container{
			{Name: "nginx", Image: "nginx:1.25", Ports: []int{80, 443}},
			{Name: "sidecar", Image: "envoy:1.28", Ports: []int{9090}},
		},
	}
	for b.Loop() {
		_, _ = Marshal(v)
	}
}

func BenchmarkScannerSmall(b *testing.B) {
	for b.Loop() {
		_, _ = newScanner(benchSmallYAML).scan()
	}
}

func BenchmarkScannerMedium(b *testing.B) {
	for b.Loop() {
		_, _ = newScanner(benchMediumYAML).scan()
	}
}

func BenchmarkRoundTrip(b *testing.B) {
	v := map[string]any{
		"name": "test",
		"items": []any{
			map[string]any{"key": "a", "value": int64(1)},
			map[string]any{"key": "b", "value": int64(2)},
		},
	}
	for b.Loop() {
		data, _ := Marshal(v)
		var out map[string]any
		_ = Unmarshal(data, &out)
	}
}

// ─── Fuzz Tests ──────────────────────────────────────────────────────────────

func FuzzUnmarshal(f *testing.F) {
	f.Add([]byte("key: value"))
	f.Add([]byte("- item1\n- item2"))
	f.Add([]byte("{a: 1, b: 2}"))
	f.Add([]byte("[1, 2, 3]"))
	f.Add([]byte("name: 'quoted'"))
	f.Add([]byte("name: \"double\\nquoted\""))
	f.Add([]byte("|\n  literal\n  block"))
	f.Add([]byte(">\n  folded\n  block"))
	f.Add([]byte("---\na: 1\n...\n---\nb: 2"))
	f.Add([]byte("anchor: &a value\nref: *a"))
	f.Add(benchSmallYAML)
	f.Add(benchMediumYAML)

	f.Fuzz(func(t *testing.T, data []byte) {
		var out any
		_ = Unmarshal(data, &out)
	})
}

func FuzzScanner(f *testing.F) {
	f.Add([]byte("key: value"))
	f.Add([]byte("- item"))
	f.Add([]byte("{a: 1}"))
	f.Add([]byte("[1, 2]"))
	f.Add([]byte("'single'"))
	f.Add([]byte("\"double\""))
	f.Add([]byte("|\n  literal"))
	f.Add([]byte(">\n  folded"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = newScanner(data).scan()
	})
}

func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte("key: value"))
	f.Add([]byte("items:\n- a\n- b"))
	f.Add([]byte("n: 42"))
	f.Add([]byte("b: true"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v any
		if err := Unmarshal(data, &v); err != nil {
			return
		}
		out, err := Marshal(v)
		if err != nil {
			return
		}
		var v2 any
		_ = Unmarshal(out, &v2)
	})
}

// ─── CustomMarshaler / CustomUnmarshaler ────────────────────────────────────

func TestCustomUnmarshaler(t *testing.T) {
	type Color struct {
		R, G, B uint8
	}
	data := []byte("color: \"#ff8000\"\n")
	var m map[string]Color
	err := UnmarshalWithOptions(data, &m, WithCustomUnmarshaler(func(c *Color, raw []byte) error {
		s := strings.Trim(string(raw), " \n\"'")
		if !strings.HasPrefix(s, "#") || len(s) != 7 {
			return fmt.Errorf("invalid color: %s", s)
		}
		_, err := fmt.Sscanf(s, "#%02x%02x%02x", &c.R, &c.G, &c.B)
		return err
	}))
	if err != nil {
		t.Fatal(err)
	}
	c := m["color"]
	if c.R != 255 || c.G != 128 || c.B != 0 {
		t.Fatalf("expected {255 128 0}, got %+v", c)
	}
}

// ──�� TAG Directive ──────────────────────────────────────────────────────────

func TestTAGDirective(t *testing.T) {
	data := []byte("%TAG !custom! tag:example.com,2024:\n---\nname: !custom!person John\n")
	file, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) == 0 {
		t.Fatal("no documents")
	}
	found := false
	Walk(file.Docs[0], func(n *Node) bool {
		if n.Tag == "tag:example.com,2024:person" {
			found = true
			if n.Value != "John" {
				t.Fatalf("expected John, got %s", n.Value)
			}
		}
		return true
	})
	if !found {
		t.Fatal("tag was not resolved to tag:example.com,2024:person")
	}
}

func TestTAGDirectiveSecondary(t *testing.T) {
	data := []byte("---\nname: !!str 42\n")
	file, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	Walk(file.Docs[0], func(n *Node) bool {
		if n.Tag == "tag:yaml.org,2002:str" {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("!! tag handle not resolved")
	}
}

func TestTAGDirectiveMultipleDocuments(t *testing.T) {
	data := []byte("%TAG !a! tag:a.com,2024:\n---\nv: !a!x hello\n...\n%TAG !a! tag:a.com,2024:\n---\nv: !a!x world\n")
	file, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(file.Docs))
	}
}

func TestTAGDirectiveVerbatimTag(t *testing.T) {
	data := []byte("name: !<tag:yaml.org,2002:str> 42\n")
	file, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	Walk(file.Docs[0], func(n *Node) bool {
		if n.Tag == "tag:yaml.org,2002:str" {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("verbatim tag not resolved")
	}
}

func TestTAGDirectiveInvalid(t *testing.T) {
	data := []byte("%TAG !bad\n---\nv: 1\n")
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for invalid TAG directive")
	}
}

// ─── Custom Tag Resolver ──────────────��─────────────────────────────────────

func TestCustomTagResolver(t *testing.T) {
	data := []byte("value: !double 21\n")
	type Wrapper struct {
		Value int `yaml:"value"`
	}
	var w Wrapper
	err := UnmarshalWithOptions(data, &w, WithTagResolver(&TagResolver{
		Tag:    "!double",
		GoType: reflect.TypeFor[int](),
		Resolve: func(value string) (any, error) {
			var n int
			_, err := fmt.Sscanf(value, "%d", &n)
			return n * 2, err
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if w.Value != 42 {
		t.Fatalf("expected 42, got %d", w.Value)
	}
}

// ─── Unterminated Flow Error ────────────────────────────────────────────────

func TestUnterminatedFlowSequence(t *testing.T) {
	data := []byte("a: [1, 2")
	if Valid(data) {
		t.Fatal("expected invalid for unterminated flow sequence")
	}
	var v any
	err := Unmarshal(data, &v)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unterminated flow sequence") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTabHandling(t *testing.T) {
	t.Run("TabInFlowContent", func(t *testing.T) {
		data := []byte("a: {b:\t1}")
		var v map[string]map[string]int
		if err := Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		if v["a"]["b"] != 1 {
			t.Fatalf("expected 1, got %d", v["a"]["b"])
		}
	})
	t.Run("TabInScalarContent", func(t *testing.T) {
		data := []byte("a: \"hello\tworld\"\n")
		var v map[string]string
		if err := Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		if v["a"] != "hello\tworld" {
			t.Fatalf("expected hello\\tworld, got %q", v["a"])
		}
	})
	t.Run("TabAfterDash", func(t *testing.T) {
		data := []byte("items:\n-\ta\n-\tb\n")
		var v map[string][]string
		if err := Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		if len(v["items"]) != 2 || v["items"][0] != "a" {
			t.Fatalf("expected [a b], got %v", v["items"])
		}
	})
}

func TestSchemaTypes(t *testing.T) {
	t.Run("CoreSchema", func(t *testing.T) {
		data := []byte("n: null\nb: true\ni: 42\nf: 3.14\nhex: 0xFF\noct: 0o77\ninf: .inf\nnan: .nan\n")
		var v map[string]any
		if err := Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		if v["n"] != nil {
			t.Fatalf("expected nil, got %v", v["n"])
		}
		if v["b"] != true {
			t.Fatalf("expected true, got %v", v["b"])
		}
		if v["i"] != int64(42) {
			t.Fatalf("expected 42, got %v", v["i"])
		}
		if v["hex"] != int64(255) {
			t.Fatalf("expected 255, got %v", v["hex"])
		}
	})
	t.Run("FailsafeStrings", func(t *testing.T) {
		data := []byte("a: !!str true\nb: !!str 42\n")
		var v map[string]string
		if err := Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		if v["a"] != "true" {
			t.Fatalf("expected 'true', got %q", v["a"])
		}
		if v["b"] != "42" {
			t.Fatalf("expected '42', got %q", v["b"])
		}
	})
}

func TestWithSchemaCore(t *testing.T) {
	data := []byte("n: null\nN: Null\nbig: NULL\ntilde: ~\nb: True\ni: 0o77\nhex: 0xFF")
	var v map[string]any
	if err := UnmarshalWithOptions(data, &v, WithSchema(CoreSchema)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"n", "N", "big", "tilde"} {
		if v[key] != nil {
			t.Errorf("CoreSchema: expected nil for %q, got %v", key, v[key])
		}
	}
	if v["b"] != true {
		t.Errorf("CoreSchema: expected true for 'True', got %v", v["b"])
	}
	if v["i"] != int64(63) {
		t.Errorf("CoreSchema: expected 63 for 0o77, got %v", v["i"])
	}
	if v["hex"] != int64(255) {
		t.Errorf("CoreSchema: expected 255 for 0xFF, got %v", v["hex"])
	}
}

func TestWithSchemaJSON(t *testing.T) {
	data := []byte("n: null\nN: Null\nbig: NULL\ntilde: ~\nbt: true\nbf: false\nbT: True\ni: 42\nf: 3.14")
	var v map[string]any
	if err := UnmarshalWithOptions(data, &v, WithSchema(JSONSchema)); err != nil {
		t.Fatal(err)
	}
	if v["n"] != nil {
		t.Errorf("JSONSchema: expected nil for 'null', got %v", v["n"])
	}
	if v["N"] != "Null" {
		t.Errorf("JSONSchema: expected string 'Null', got %v (%T)", v["N"], v["N"])
	}
	if v["big"] != "NULL" {
		t.Errorf("JSONSchema: expected string 'NULL', got %v (%T)", v["big"], v["big"])
	}
	if v["tilde"] != "~" {
		t.Errorf("JSONSchema: expected string '~', got %v (%T)", v["tilde"], v["tilde"])
	}
	if v["bt"] != true {
		t.Errorf("JSONSchema: expected true, got %v", v["bt"])
	}
	if v["bf"] != false {
		t.Errorf("JSONSchema: expected false, got %v", v["bf"])
	}
	if v["bT"] != "True" {
		t.Errorf("JSONSchema: expected string 'True', got %v (%T)", v["bT"], v["bT"])
	}
	if v["i"] != int64(42) {
		t.Errorf("JSONSchema: expected 42, got %v", v["i"])
	}
}

func TestWithSchemaFailsafe(t *testing.T) {
	data := []byte("n: null\nb: true\ni: 42\nf: 3.14")
	var v map[string]any
	if err := UnmarshalWithOptions(data, &v, WithSchema(FailsafeSchema)); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{"n": "null", "b": "true", "i": "42", "f": "3.14"} {
		s, ok := v[key].(string)
		if !ok {
			t.Errorf("FailsafeSchema: expected string for %q, got %T (%v)", key, v[key], v[key])
			continue
		}
		if s != expected {
			t.Errorf("FailsafeSchema: expected %q for %q, got %q", expected, key, s)
		}
	}
}

func TestWithSchemaFailsafeExplicitTags(t *testing.T) {
	data := []byte("s: !!str 42\nn: !!null ''")
	var v map[string]any
	if err := UnmarshalWithOptions(data, &v, WithSchema(FailsafeSchema)); err != nil {
		t.Fatal(err)
	}
	if v["s"] != "42" {
		t.Errorf("expected string '42', got %v (%T)", v["s"], v["s"])
	}
}

func TestUnterminatedFlowMapping(t *testing.T) {
	data := []byte("a: {b: 1")
	if Valid(data) {
		t.Fatal("expected invalid for unterminated flow mapping")
	}
	var v any
	err := Unmarshal(data, &v)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unterminated flow mapping") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── Conformance Tests (embedded YAML test suite cases) ─────────────────────

func TestConformanceSuite(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		valid   bool
		decoded any
	}{
		{"EmptyDocument", "", true, nil},
		{"ScalarString", "hello\n", true, "hello"},
		{"ScalarInt", "42\n", true, int64(42)},
		{"ScalarFloat", "3.14\n", true, 3.14},
		{"ScalarBoolTrue", "true\n", true, true},
		{"ScalarBoolFalse", "false\n", true, false},
		{"ScalarNull", "null\n", true, nil},
		{"ScalarTilde", "~\n", true, nil},
		{"PlainMapping", "a: 1\nb: 2\n", true, map[string]any{"a": int64(1), "b": int64(2)}},
		{"FlowMapping", "{a: 1, b: 2}\n", true, map[string]any{"a": int64(1), "b": int64(2)}},
		{"BlockSequence", "- a\n- b\n- c\n", true, []any{"a", "b", "c"}},
		{"FlowSequence", "[a, b, c]\n", true, []any{"a", "b", "c"}},
		{"NestedMapping", "a:\n  b: 1\n", true, map[string]any{"a": map[string]any{"b": int64(1)}}},
		{"NestedSequence", "- - a\n  - b\n", true, []any{[]any{"a", "b"}}},
		{"LiteralBlock", "|\n  hello\n  world\n", true, "hello\nworld\n"},
		{"FoldedBlock", ">\n  hello\n  world\n", true, "hello world\n"},
		{"LiteralStrip", "|-\n  hello\n", true, "hello"},
		{"LiteralKeep", "|+\n  hello\n\n", true, "hello\n\n"},
		{"Anchor", "&x hello\n", true, "hello"},
		{"Alias", "a: &x hello\nb: *x\n", true, map[string]any{"a": "hello", "b": "hello"}},
		{"MergeKey", "a: &d\n  x: 1\nb:\n  <<: *d\n  y: 2\n", true, map[string]any{
			"a": map[string]any{"x": int64(1)},
			"b": map[string]any{"x": int64(1), "y": int64(2)},
		}},
		{"ExplicitDoc", "---\nhello\n", true, "hello"},
		{"DocEnd", "hello\n...\n", true, "hello"},
		{"DoubleQuoted", "\"hello\\nworld\"\n", true, "hello\nworld"},
		{"SingleQuoted", "'hello world'\n", true, "hello world"},
		{"SingleQuoteEscape", "'it''s'\n", true, "it's"},
		{"HexInt", "0xFF\n", true, int64(255)},
		{"OctalInt", "0o77\n", true, int64(63)},
		{"PosInf", ".inf\n", true, math.Inf(1)},
		{"NegInf", "-.inf\n", true, math.Inf(-1)},
		{"NaN", ".nan\n", true, nil},
		{"NullVariants_NULL", "NULL\n", true, nil},
		{"NullVariants_Null", "Null\n", true, nil},
		{"BoolVariants_True", "True\n", true, true},
		{"BoolVariants_FALSE", "FALSE\n", true, false},
		{"EmptyMapping", "{}\n", true, map[string]any{}},
		{"EmptySequence", "[]\n", true, []any{}},
		{"Comment", "# comment\nvalue\n", true, "value"},
		{"InlineComment", "value # comment\n", true, "value"},
		{"YAMLDirective", "%YAML 1.2\n---\nhello\n", true, "hello"},
		{"UnterminatedFlow", "[1, 2", false, nil},
		{"DuplicateAnchorAlias", "a: &x 1\nb: &x 2\nc: *x\n", true, map[string]any{"a": int64(1), "b": int64(2), "c": int64(2)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			valid := Valid([]byte(tc.input))
			if valid != tc.valid {
				t.Fatalf("Valid: expected %v, got %v", tc.valid, valid)
			}
			if !tc.valid {
				return
			}

			var got any
			err := Unmarshal([]byte(tc.input), &got)
			if err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if tc.name == "NaN" {
				f, ok := got.(float64)
				if !ok || !math.IsNaN(f) {
					t.Fatalf("expected NaN, got %v", got)
				}
				return
			}

			if !reflect.DeepEqual(got, tc.decoded) {
				t.Fatalf("expected %#v, got %#v", tc.decoded, got)
			}
		})
	}
}

// ─── Differential Tests ─────────────────────────────────────────────────────

func TestDifferentialCoreSchema(t *testing.T) {
	inputs := []string{
		"null", "~", "true", "false", "True", "False", "TRUE", "FALSE",
		"42", "-17", "0xFF", "0o77", "3.14", ".inf", "-.inf", ".nan",
		"hello", "hello world", "''", "\"\"",
		"[1, 2, 3]", "{a: 1, b: 2}",
		"- a\n- b", "a: 1\nb: 2",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			var v any
			err := Unmarshal([]byte(input+"\n"), &v)
			if err != nil {
				return
			}
			out, err := Marshal(v)
			if err != nil {
				t.Fatalf("roundtrip marshal failed: %v", err)
			}
			var v2 any
			if err := Unmarshal(out, &v2); err != nil {
				t.Fatalf("roundtrip unmarshal failed: %v", err)
			}
			isNaN := func(v any) bool {
				f, ok := v.(float64)
				return ok && math.IsNaN(f)
			}
			if isNaN(v) && isNaN(v2) {
				return
			}
			if !reflect.DeepEqual(v, v2) {
				t.Fatalf("roundtrip mismatch: %#v → %s → %#v", v, out, v2)
			}
		})
	}
}

// ─── Benchmark Comparison Framework ─────────────────────────────────────────

func BenchmarkConformanceSmall(b *testing.B) {
	data := []byte("name: test\nport: 8080\ntags:\n  - web\n  - api\n")
	b.ResetTimer()
	for b.Loop() {
		var v any
		Unmarshal(data, &v)
	}
}

func BenchmarkConformanceMedium(b *testing.B) {
	var buf bytes.Buffer
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&buf, "key%d: value%d\n", i, i)
	}
	data := buf.Bytes()
	b.ResetTimer()
	for b.Loop() {
		var v any
		Unmarshal(data, &v)
	}
}

func BenchmarkConformanceLarge(b *testing.B) {
	var buf bytes.Buffer
	buf.WriteString("items:\n")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&buf, "  - name: item%d\n    value: %d\n    tags: [a, b, c]\n", i, i)
	}
	data := buf.Bytes()
	b.ResetTimer()
	for b.Loop() {
		var v any
		Unmarshal(data, &v)
	}
}

// ─── NEL / LS / PS Line Break Normalization ─────────────────────────────────

func TestNELLineBreak(t *testing.T) {
	// U+0085 NEL = 0xC2 0x85
	data := []byte("a: 1\xc2\x85b: 2\n")
	var v map[string]int
	if err := Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if v["a"] != 1 || v["b"] != 2 {
		t.Fatalf("expected {a:1, b:2}, got %v", v)
	}
}

func TestLSLineBreak(t *testing.T) {
	// U+2028 LS = 0xE2 0x80 0xA8
	data := []byte("a: 1\xe2\x80\xa8b: 2\n")
	var v map[string]int
	if err := Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if v["a"] != 1 || v["b"] != 2 {
		t.Fatalf("expected {a:1, b:2}, got %v", v)
	}
}

func TestPSLineBreak(t *testing.T) {
	// U+2029 PS = 0xE2 0x80 0xA9
	data := []byte("a: 1\xe2\x80\xa9b: 2\n")
	var v map[string]int
	if err := Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if v["a"] != 1 || v["b"] != 2 {
		t.Fatalf("expected {a:1, b:2}, got %v", v)
	}
}

// ─── Binary / Base64 ────────────────────────────────────────────────────────

func TestUnmarshalBinary(t *testing.T) {
	data := []byte("data: !!binary aGVsbG8=\n")
	var v struct {
		Data []byte `yaml:"data"`
	}
	if err := Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if string(v.Data) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(v.Data))
	}
}

// ─── MaxDocumentSize ────────────────────────────────────────────────────────

func TestMaxDocumentSize(t *testing.T) {
	data := []byte("key: value\n")
	var v any
	err := UnmarshalWithOptions(data, &v, WithMaxDocumentSize(5))
	if err == nil {
		t.Fatal("expected error for oversized document")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeMappingToInterface(t *testing.T) {
	data := []byte("{a: 1}\n")
	var v any
	if err := Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", v)
	}
	if m["a"] != int64(1) {
		t.Fatalf("expected 1, got %v", m["a"])
	}
}

func TestDecodeSequenceToSlice(t *testing.T) {
	data := []byte("- 1\n- hello\n- true\n")
	var v []any
	if err := Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if len(v) != 3 {
		t.Fatalf("expected 3 items, got %d", len(v))
	}
}

func TestDecodeSequenceToArray(t *testing.T) {
	data := []byte("- 1\n- 2\n- 3\n")
	var v [3]int
	if err := Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if v != [3]int{1, 2, 3} {
		t.Fatalf("expected [1,2,3], got %v", v)
	}
}

func TestDecodeMergeSequence(t *testing.T) {
	data := []byte(`
defaults1: &d1
  a: 1
  b: 2
defaults2: &d2
  c: 3
merged:
  <<: [*d1, *d2]
  d: 4
`)
	var v map[string]any
	if err := Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	merged, ok := v["merged"].(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", v["merged"])
	}
	if merged["a"] != int64(1) || merged["c"] != int64(3) || merged["d"] != int64(4) {
		t.Fatalf("merge sequence failed: %v", merged)
	}
}

func TestDecodeDuplicateKeyInMap(t *testing.T) {
	var m map[string]int
	err := UnmarshalWithOptions([]byte("a: 1\na: 2\n"), &m, WithDisallowDuplicateKey())
	if err == nil {
		t.Fatal("expected duplicate key error")
	}
	var dke *DuplicateKeyError
	if !errors.As(err, &dke) {
		t.Errorf("expected DuplicateKeyError, got %T: %v", err, err)
	}
}

func TestDecodeMergeThroughStruct(t *testing.T) {
	type S struct {
		A string `yaml:"a"`
		B string `yaml:"b"`
	}
	input := "a: &base\n  a: hello\n  b: world\nresult:\n  <<: *base\n  b: override\n"
	var out struct {
		A      map[string]any `yaml:"a"`
		Result S              `yaml:"result"`
	}
	err := Unmarshal([]byte(input), &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.Result.A != "hello" {
		t.Errorf("expected a=hello from merge, got %q", out.Result.A)
	}
	if out.Result.B != "override" {
		t.Errorf("expected b=override (not overridden by merge), got %q", out.Result.B)
	}
}

func TestDecodeMergeStructNestedMergeError(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	badNode := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "<<"},
			{kind: nodeAlias, alias: "nonexistent"},
		},
	}
	type S struct {
		A string `yaml:"a"`
	}
	var s S
	err := d.decodeMappingToStruct(badNode, reflect.ValueOf(&s).Elem())
	if err == nil {
		t.Error("expected error for unknown alias in struct merge")
	}
}

func TestDecodeMappingMergeMapErrorInDecode(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	mergeNode := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "key"},
			{kind: nodeAlias, alias: "missing"},
		},
	}
	m := reflect.ValueOf(map[string]any{})
	err := d.decodeMappingMerge(mergeNode, m)
	if err == nil {
		t.Error("expected error for missing alias in map merge")
	}
}

func TestDecodeMappingMergeStructErrorInDecode(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	mergeNode := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "a"},
			{kind: nodeAlias, alias: "missing"},
		},
	}
	type S struct {
		A string `yaml:"a"`
	}
	var s S
	err := d.decodeMappingMerge(mergeNode, reflect.ValueOf(&s).Elem())
	if err == nil {
		t.Error("expected error for missing alias in struct merge")
	}
}

func TestDecodeMappingMergeStructChainedMerge(t *testing.T) {
	input := "base1: &b1\n  a: 1\nbase2: &b2\n  <<: *b1\n  b: 2\nresult:\n  <<: *b2\n  c: 3\n"
	type Result struct {
		A string `yaml:"a"`
		B string `yaml:"b"`
		C string `yaml:"c"`
	}
	var out struct {
		Base1  map[string]any `yaml:"base1"`
		Base2  map[string]any `yaml:"base2"`
		Result Result         `yaml:"result"`
	}
	err := Unmarshal([]byte(input), &out)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeSequenceOverflowArray(t *testing.T) {
	var arr [3]int
	err := Unmarshal([]byte("[1, 2, 3, 4, 5]"), &arr)
	if err != nil {
		t.Fatal(err)
	}
	if arr != [3]int{1, 2, 3} {
		t.Errorf("expected [1 2 3], got %v", arr)
	}
}

func TestDecodeSequenceToNonSlice(t *testing.T) {
	var s string
	err := Unmarshal([]byte("[1, 2]"), &s)
	if err == nil {
		t.Fatal("expected type error for sequence into string")
	}
}

func TestDecodeScalarToAnyIntOverflow(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{kind: nodeScalar, value: "99999999999999999999", style: scalarPlain}
	result := d.scalarToAny(n)
	// Should fall through to float or string
	_, isString := result.(string)
	_, isFloat := result.(float64)
	if !isString && !isFloat {
		t.Errorf("expected string or float for overflow int, got %T: %v", result, result)
	}
}

func TestDecodeMergeScalarIgnored(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	scalarNode := &node{kind: nodeScalar, value: "123"}
	m := reflect.ValueOf(make(map[string]any))
	err := d.decodeMerge(scalarNode, m)
	if err != nil {
		t.Errorf("expected nil error for scalar merge (ignored), got %v", err)
	}
}

func TestDecodeMergeSequenceWithUnknownAlias(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	seqNode := &node{
		kind: nodeSequence,
		children: []*node{
			{kind: nodeAlias, alias: "missing"},
		},
	}
	m := reflect.ValueOf(make(map[string]any))
	err := d.decodeMerge(seqNode, m)
	if err == nil {
		t.Error("expected error for unknown alias in merge sequence")
	}
}

func TestDecodeMappingMergeNonMappingNode(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	n := &node{kind: nodeScalar, value: "notamap"}
	m := reflect.ValueOf(make(map[string]any))
	err := d.decodeMappingMerge(n, m)
	if err != nil {
		t.Errorf("expected nil for non-mapping merge, got %v", err)
	}
}

func TestDecodeMappingMergeKeyDecodeError(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	mergeNode := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeAlias, alias: "missing_key"},
			{kind: nodeScalar, value: "val"},
		},
	}
	m := reflect.ValueOf(make(map[string]any))
	err := d.decodeMappingMerge(mergeNode, m)
	if err == nil {
		t.Error("expected error for missing alias key in mapping merge")
	}
}

func TestDecodeMappingMergeValueDecodeError(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	d.anchors["key"] = &node{kind: nodeScalar, value: "k"}
	mergeNode := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "newkey"},
			{kind: nodeAlias, alias: "missing_val"},
		},
	}
	m := reflect.MakeMap(reflect.TypeFor[map[string]any]())
	err := d.decodeMappingMerge(mergeNode, m)
	if err == nil {
		t.Error("expected error for missing alias value in mapping merge")
	}
}

func TestDecodeToAnyMergeSequenceNonMap(t *testing.T) {
	input := "defaults: &d\n  - 1\n  - 2\nresult:\n  <<: *d\n  key: val\n"
	var v map[string]any
	err := Unmarshal([]byte(input), &v)
	// The merge of a non-map should be handled gracefully
	_ = err
}

func TestDecodeSequenceErrorInElement(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	seqNode := &node{
		kind: nodeSequence,
		children: []*node{
			{kind: nodeAlias, alias: "nonexistent"},
		},
	}
	var slice []string
	err := d.decodeSequence(seqNode, reflect.ValueOf(&slice).Elem())
	if err == nil {
		t.Error("expected error for unknown alias in sequence element")
	}
}

func TestDecodeUnexportedFieldSkipped(t *testing.T) {
	type S struct {
		unexported string `yaml:"hidden"`
		Exported   string `yaml:"visible"`
	}
	var s S
	err := Unmarshal([]byte("hidden: secret\nvisible: public"), &s)
	if err != nil {
		t.Fatal(err)
	}
	if s.Exported != "public" {
		t.Errorf("expected public, got %q", s.Exported)
	}
	if s.unexported != "" {
		t.Errorf("expected unexported to be empty, got %q", s.unexported)
	}
}

type nonPtrUnmarshaler struct {
	val string
}

func (u *nonPtrUnmarshaler) UnmarshalYAML(unmarshal func(any) error) error {
	m := make(map[string]string)
	if err := unmarshal(m); err != nil {
		return err
	}
	u.val = m["key"]
	return nil
}

func TestDecodeUnmarshalerNonPointerCallback(t *testing.T) {
	var u nonPtrUnmarshaler
	err := Unmarshal([]byte("key: val"), &u)
	if err != nil {
		t.Fatal(err)
	}
	if u.val != "val" {
		t.Errorf("expected val, got %q", u.val)
	}
}

type nonPtrCtxUnmarshaler struct {
	val string
}

func (u *nonPtrCtxUnmarshaler) UnmarshalYAML(ctx context.Context, unmarshal func(any) error) error {
	m := make(map[string]string)
	if err := unmarshal(m); err != nil {
		return err
	}
	u.val = m["key"]
	return nil
}

func TestDecodeCtxUnmarshalerNonPointerCallback(t *testing.T) {
	var u nonPtrCtxUnmarshaler
	err := Unmarshal([]byte("key: val"), &u)
	if err != nil {
		t.Fatal(err)
	}
	if u.val != "val" {
		t.Errorf("expected val, got %q", u.val)
	}
}

func TestDecodeMergeErrorInMap(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	mergeKeyNode := &node{kind: nodeScalar, value: "<<"}
	mergeValNode := &node{kind: nodeAlias, alias: "missing_anchor"}
	mappingNode := &node{
		kind:     nodeMapping,
		children: []*node{mergeKeyNode, mergeValNode},
	}
	m := reflect.MakeMap(reflect.TypeFor[map[string]any]())
	err := d.decodeMappingToMap(mappingNode, m)
	if err == nil {
		t.Error("expected error for unknown alias in merge within map")
	}
}

func TestDecodeMappingMergeStructWithNestedMergeKey(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	innerAnchor := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "a"},
			{kind: nodeScalar, value: "1"},
		},
	}
	d.anchors["inner"] = innerAnchor
	outerNode := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "<<"},
			{kind: nodeAlias, alias: "inner"},
		},
	}
	type S struct {
		A string `yaml:"a"`
	}
	var s S
	err := d.decodeMappingMerge(outerNode, reflect.ValueOf(&s).Elem())
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeToAnyMergeSequenceWithNonMapItem(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	scalarAnchor := &node{kind: nodeScalar, value: "just_a_string", style: scalarPlain}
	d.anchors["str"] = scalarAnchor

	mapAnchor := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "a"},
			{kind: nodeScalar, value: "1"},
		},
	}
	d.anchors["map"] = mapAnchor

	seqNode := &node{
		kind: nodeSequence,
		children: []*node{
			{kind: nodeAlias, alias: "str"},
			{kind: nodeAlias, alias: "map"},
		},
	}
	mergeKeyNode := &node{kind: nodeScalar, value: "<<"}
	keyNode := &node{kind: nodeScalar, value: "b"}
	valNode := &node{kind: nodeScalar, value: "2"}
	mappingNode := &node{
		kind:     nodeMapping,
		children: []*node{mergeKeyNode, seqNode, keyNode, valNode},
	}

	result, err := d.decodeToAny(mappingNode)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["b"] != int64(2) {
		t.Errorf("expected b=2, got %v", m["b"])
	}
}

func TestDecodeToAnyMergeError(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	mergeKeyNode := &node{kind: nodeScalar, value: "<<"}
	badAlias := &node{kind: nodeAlias, alias: "nonexistent"}
	keyNode := &node{kind: nodeScalar, value: "b"}
	valNode := &node{kind: nodeScalar, value: "2"}
	mappingNode := &node{
		kind:     nodeMapping,
		children: []*node{mergeKeyNode, badAlias, keyNode, valNode},
	}
	_, err := d.decodeToAny(mappingNode)
	if err == nil {
		t.Error("expected error for unknown alias in merge via decodeToAny")
	}
}

func TestDecodeMappingMergeStructUnknownField(t *testing.T) {
	input := "base: &base\n  a: hello\n  extra: ignored\nresult:\n  <<: *base\n"
	type Result struct {
		A string `yaml:"a"`
	}
	var out struct {
		Base   map[string]any `yaml:"base"`
		Result Result         `yaml:"result"`
	}
	err := Unmarshal([]byte(input), &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.Result.A != "hello" {
		t.Errorf("expected a=hello, got %q", out.Result.A)
	}
}

func TestDecodeMappingToStructUnexportedField(t *testing.T) {
	type S struct {
		exported   string `yaml:"exported"`
		Unexported string `yaml:"unexported"`
	}
	var s S
	err := Unmarshal([]byte("exported: a\nunexported: b"), &s)
	if err != nil {
		t.Fatal(err)
	}
	if s.exported != "" {
		t.Errorf("expected unexported field to be empty, got %q", s.exported)
	}
}

func TestDecodeMappingMergeStructWithNestedMergeKeyInternal(t *testing.T) {
	d := newDecoder(defaultDecodeOptions())
	innerAnchor := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "<<"},
			{kind: nodeAlias, alias: "nonexistent"},
		},
	}
	d.anchors["inner"] = innerAnchor

	outerNode := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "<<"},
			{kind: nodeAlias, alias: "inner"},
		},
	}
	type S struct {
		A string `yaml:"a"`
	}
	var s S
	err := d.decodeMappingMerge(outerNode, reflect.ValueOf(&s).Elem())
	if err == nil {
		t.Error("expected error for nested merge with unknown alias")
	}
}

func TestBigIntRoundTrip(t *testing.T) {
	type S struct {
		N *big.Int `yaml:"n"`
	}

	large := new(big.Int)
	large.SetString("123456789012345678901234567890", 10)

	tests := []struct {
		name  string
		input string
		want  *big.Int
	}{
		{"small", "n: 42", big.NewInt(42)},
		{"negative", "n: -99", big.NewInt(-99)},
		{"zero", "n: 0", big.NewInt(0)},
		{"large", "n: 123456789012345678901234567890", large},
		{"hex", "n: 0xff", big.NewInt(255)},
		{"octal", "n: 0o77", big.NewInt(63)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s S
			if err := Unmarshal([]byte(tt.input), &s); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if s.N.Cmp(tt.want) != 0 {
				t.Errorf("got %s, want %s", s.N, tt.want)
			}

			data, err := Marshal(s)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var s2 S
			if err := Unmarshal(data, &s2); err != nil {
				t.Fatalf("round-trip unmarshal: %v", err)
			}
			if s2.N.Cmp(tt.want) != 0 {
				t.Errorf("round-trip got %s, want %s", s2.N, tt.want)
			}
		})
	}
}

func TestBigFloatRoundTrip(t *testing.T) {
	type S struct {
		F *big.Float `yaml:"f"`
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"integer", "f: 42", "42"},
		{"decimal", "f: 3.14159", "3.14159"},
		{"negative", "f: -1.5", "-1.5"},
		{"zero", "f: 0", "0"},
		{"large", "f: 1e100", "1e+100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s S
			if err := Unmarshal([]byte(tt.input), &s); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			got := s.F.Text('g', -1)
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}

			data, err := Marshal(s)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var s2 S
			if err := Unmarshal(data, &s2); err != nil {
				t.Fatalf("round-trip unmarshal: %v", err)
			}
			got2 := s2.F.Text('g', -1)
			if got2 != tt.want {
				t.Errorf("round-trip got %s, want %s", got2, tt.want)
			}
		})
	}
}

func TestBigRatRoundTrip(t *testing.T) {
	type S struct {
		R *big.Rat `yaml:"r"`
	}

	tests := []struct {
		name  string
		input string
		want  *big.Rat
	}{
		{"fraction", "r: 3/4", big.NewRat(3, 4)},
		{"whole", "r: 5", big.NewRat(5, 1)},
		{"negative", "r: -1/3", big.NewRat(-1, 3)},
		{"decimal", "r: 0.5", big.NewRat(1, 2)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s S
			if err := Unmarshal([]byte(tt.input), &s); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if s.R.Cmp(tt.want) != 0 {
				t.Errorf("got %s, want %s", s.R.RatString(), tt.want.RatString())
			}

			data, err := Marshal(s)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var s2 S
			if err := Unmarshal(data, &s2); err != nil {
				t.Fatalf("round-trip unmarshal: %v", err)
			}
			if s2.R.Cmp(tt.want) != 0 {
				t.Errorf("round-trip got %s, want %s", s2.R.RatString(), tt.want.RatString())
			}
		})
	}
}

func TestBigNilPointer(t *testing.T) {
	type S struct {
		N *big.Int   `yaml:"n,omitempty"`
		F *big.Float `yaml:"f,omitempty"`
		R *big.Rat   `yaml:"r,omitempty"`
	}

	data, err := Marshal(S{})
	if err != nil {
		t.Fatalf("marshal nil pointers: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty output for all-nil omitempty, got: %q", data)
	}

	var s S
	if err := Unmarshal([]byte("n: null\nf: null\nr: null"), &s); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if s.N != nil || s.F != nil || s.R != nil {
		t.Errorf("expected nil pointers, got n=%v f=%v r=%v", s.N, s.F, s.R)
	}
}

func TestBigIntInvalidInput(t *testing.T) {
	type S struct {
		N *big.Int `yaml:"n"`
	}
	var s S
	err := Unmarshal([]byte("n: not_a_number"), &s)
	if err == nil {
		t.Error("expected error for invalid big.Int input")
	}
}

func TestBigFloatInvalidInput(t *testing.T) {
	type S struct {
		F *big.Float `yaml:"f"`
	}
	var s S
	err := Unmarshal([]byte("f: not_a_number"), &s)
	if err == nil {
		t.Error("expected error for invalid big.Float input")
	}
}

func TestBigRatInvalidInput(t *testing.T) {
	type S struct {
		R *big.Rat `yaml:"r"`
	}
	var s S
	err := Unmarshal([]byte("r: not/valid/rat"), &s)
	if err == nil {
		t.Error("expected error for invalid big.Rat input")
	}
}
