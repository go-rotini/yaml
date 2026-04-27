package yaml

// MapSlice is an ordered slice of key-value pairs. It is used as the
// decoded representation of YAML mappings when [UseOrderedMap] is enabled,
// preserving the original key order that a plain map[string]any would lose.
type MapSlice []MapItem

// MapItem is a single key-value pair within a [MapSlice].
type MapItem struct {
	Key   any
	Value any
}
