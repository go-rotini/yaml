package yaml

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestYAMLToJSON(t *testing.T) {
	yamlData := []byte("name: test\ncount: 42")
	jsonData, err := YAMLToJSON(yamlData)
	if err != nil {
		t.Fatal(err)
	}
	s := string(jsonData)
	if !strings.Contains(s, `"name":"test"`) {
		t.Errorf("expected name in JSON, got: %s", s)
	}
	if !strings.Contains(s, `"count":42`) {
		t.Errorf("expected count in JSON, got: %s", s)
	}
}

func TestJSONToYAML(t *testing.T) {
	jsonData := []byte(`{"name":"test","count":42}`)
	yamlData, err := JSONToYAML(jsonData)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	if err := Unmarshal(yamlData, &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "test" {
		t.Errorf("expected name=test, got %v", out["name"])
	}
}

func TestYAMLToJSONError(t *testing.T) {
	_, err := YAMLToJSON([]byte(`[invalid`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestJSONToYAMLError(t *testing.T) {
	_, err := JSONToYAML([]byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestYAMLToJSONTypes(t *testing.T) {
	input := `
string: hello
integer: 42
float: 3.14
bool: true
null_val: null
list:
  - one
  - two
nested:
  key: value
`
	jsonData, err := YAMLToJSON([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(jsonData, &m); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if m["string"] != "hello" {
		t.Errorf("string: got %v", m["string"])
	}
	if m["integer"].(float64) != 42 {
		t.Errorf("integer: got %v", m["integer"])
	}
	if m["bool"] != true {
		t.Errorf("bool: got %v", m["bool"])
	}
	if m["null_val"] != nil {
		t.Errorf("null_val: got %v", m["null_val"])
	}
}

func TestJSONToYAMLTypes(t *testing.T) {
	input := `{"s":"hello","n":42,"f":3.14,"b":true,"nil":null,"arr":[1,2],"obj":{"k":"v"}}`
	yamlData, err := JSONToYAML([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := Unmarshal(yamlData, &m); err != nil {
		t.Fatal(err)
	}
	if m["s"] != "hello" {
		t.Errorf("s: got %v", m["s"])
	}
	if m["b"] != true {
		t.Errorf("b: got %v", m["b"])
	}
}

func TestYAMLToJSONEmpty(t *testing.T) {
	jsonData, err := YAMLToJSON([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if string(jsonData) != "null" {
		t.Errorf("expected null for empty YAML, got %s", jsonData)
	}
}

func TestJSONToYAMLEmpty(t *testing.T) {
	yamlData, err := JSONToYAML([]byte("null"))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := Unmarshal(yamlData, &v); err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Errorf("expected nil for null JSON, got %v", v)
	}
}

func TestYAMLToJSONRoundTrip(t *testing.T) {
	original := `name: roundtrip
items:
  - a
  - b
count: 99
`
	jsonData, err := YAMLToJSON([]byte(original))
	if err != nil {
		t.Fatal(err)
	}
	yamlData, err := JSONToYAML(jsonData)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := Unmarshal(yamlData, &m); err != nil {
		t.Fatal(err)
	}
	if m["name"] != "roundtrip" {
		t.Errorf("name: got %v", m["name"])
	}
}
