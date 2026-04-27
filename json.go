package yaml

import (
	"encoding/json"
	"fmt"
)

// ToJSON converts YAML bytes to JSON bytes. The YAML input is decoded
// into an untyped value and then re-encoded as JSON.
func ToJSON(yamlData []byte) ([]byte, error) {
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

// FromJSON converts JSON bytes to YAML bytes. The JSON input is decoded
// into an untyped value and then re-encoded as YAML.
func FromJSON(jsonData []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(jsonData, &v); err != nil {
		return nil, fmt.Errorf("yaml: json decode: %w", err)
	}
	return Marshal(v)
}
