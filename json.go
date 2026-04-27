package yaml

import "encoding/json"

// YAMLToJSON converts YAML bytes to JSON bytes. The YAML input is decoded
// into an untyped value and then re-encoded as JSON.
func YAMLToJSON(yamlData []byte) ([]byte, error) {
	var v any
	if err := Unmarshal(yamlData, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// JSONToYAML converts JSON bytes to YAML bytes. The JSON input is decoded
// into an untyped value and then re-encoded as YAML.
func JSONToYAML(jsonData []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(jsonData, &v); err != nil {
		return nil, err
	}
	return Marshal(v)
}
