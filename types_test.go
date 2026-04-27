package yaml

import "testing"

func TestMapSliceType(t *testing.T) {
	ms := MapSlice{
		{Key: "name", Value: "test"},
		{Key: "count", Value: 42},
	}
	if len(ms) != 2 {
		t.Errorf("expected 2 items, got %d", len(ms))
	}
	if ms[0].Key != "name" {
		t.Errorf("expected key=name, got %v", ms[0].Key)
	}
	if ms[1].Value != 42 {
		t.Errorf("expected value=42, got %v", ms[1].Value)
	}
}

func TestMapSliceEmpty(t *testing.T) {
	ms := MapSlice{}
	if len(ms) != 0 {
		t.Errorf("empty MapSlice should have length 0, got %d", len(ms))
	}
}

func TestMapItemZeroValue(t *testing.T) {
	var item MapItem
	if item.Key != nil {
		t.Errorf("zero MapItem.Key should be nil, got %v", item.Key)
	}
	if item.Value != nil {
		t.Errorf("zero MapItem.Value should be nil, got %v", item.Value)
	}
}

func TestMapSliceAppend(t *testing.T) {
	ms := MapSlice{{Key: "a", Value: 1}}
	ms = append(ms, MapItem{Key: "b", Value: 2})
	if len(ms) != 2 {
		t.Fatalf("expected 2 items after append, got %d", len(ms))
	}
	if ms[1].Key != "b" || ms[1].Value != 2 {
		t.Errorf("appended item wrong: %v", ms[1])
	}
}

func TestMapSliceHeterogeneousValues(t *testing.T) {
	ms := MapSlice{
		{Key: "string", Value: "hello"},
		{Key: "int", Value: 42},
		{Key: "bool", Value: true},
		{Key: "nil", Value: nil},
		{Key: "nested", Value: MapSlice{{Key: "inner", Value: 1}}},
	}
	if len(ms) != 5 {
		t.Errorf("expected 5 items, got %d", len(ms))
	}
	inner, ok := ms[4].Value.(MapSlice)
	if !ok {
		t.Fatalf("expected nested MapSlice, got %T", ms[4].Value)
	}
	if inner[0].Key != "inner" {
		t.Errorf("expected inner key, got %v", inner[0].Key)
	}
}
