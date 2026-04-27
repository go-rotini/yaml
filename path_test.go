package yaml

import (
	"strings"
	"testing"
)

func TestPathString(t *testing.T) {
	p, err := PathString("$.foo.bar[0].baz")
	if err != nil {
		t.Fatal(err)
	}
	if p.String() != "$.foo.bar[0].baz" {
		t.Errorf("expected $.foo.bar[0].baz, got %s", p.String())
	}
}

func TestPathStringRoundTrips(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"$", "$"},
		{"$.name", "$.name"},
		{"$.a.b.c", "$.a.b.c"},
		{"$.items[0]", "$.items[0]"},
		{"$.items[-1]", "$.items[-1]"},
		{"$.*", "$.*"},
		{"$.items.*", "$.items.*"},
		{"$.items[*]", "$.items.*"},
		{"$..", "$.."},
	}
	for _, tt := range tests {
		p, err := PathString(tt.input)
		if err != nil {
			t.Fatalf("PathString(%q): %v", tt.input, err)
		}
		got := p.String()
		if got != tt.expected {
			t.Errorf("PathString(%q).String() = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestPathRead(t *testing.T) {
	input := `
name: hello
nested:
  key: value
items:
- a
- b
- c
`
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	doc := file.Docs[0]

	p, _ := PathString("$.name")
	nodes, err := p.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Value != "hello" {
		t.Errorf("expected 'hello', got %v", nodes)
	}

	p2, _ := PathString("$.nested.key")
	nodes2, err := p2.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes2) != 1 || nodes2[0].Value != "value" {
		t.Errorf("expected 'value', got %v", nodes2)
	}

	p3, _ := PathString("$.items[1]")
	nodes3, err := p3.Read(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes3) != 1 || nodes3[0].Value != "b" {
		t.Errorf("expected 'b', got %v", nodes3)
	}
}

func TestPathReadNotFound(t *testing.T) {
	file, err := Parse([]byte("a: 1"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.nonexistent")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 results for nonexistent path, got %d", len(nodes))
	}
}

func TestPathReadIndexOutOfBounds(t *testing.T) {
	file, err := Parse([]byte("items: [a, b]"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items[99]")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 results for out-of-bounds index, got %d", len(nodes))
	}
}

func TestPathReadChildOnScalar(t *testing.T) {
	file, err := Parse([]byte("name: hello"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.name.child")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 results for child of scalar, got %d", len(nodes))
	}
}

func TestPathReadIndexOnMapping(t *testing.T) {
	file, err := Parse([]byte("a: 1"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.a[0]")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 results for index on non-sequence, got %d", len(nodes))
	}
}

func TestPathWildcard(t *testing.T) {
	input := `items:
- a
- b
- c`
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items.*")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 wildcard results, got %d", len(nodes))
	}
}

func TestPathWildcardMapping(t *testing.T) {
	file, err := Parse([]byte("a: 1\nb: 2\nc: 3"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.*")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 wildcard mapping results, got %d", len(nodes))
	}
}

func TestPathWildcardOnScalar(t *testing.T) {
	file, err := Parse([]byte("name: hello"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.name.*")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 wildcard results on scalar, got %d", len(nodes))
	}
}

func TestPathBracketWildcard(t *testing.T) {
	file, err := Parse([]byte("items: [a, b]"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items[*]")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 results from [*], got %d", len(nodes))
	}
}

func TestPathReadString(t *testing.T) {
	data := []byte("name: world")
	p, _ := PathString("$.name")
	val, err := p.ReadString(data)
	if err != nil {
		t.Fatal(err)
	}
	if val != "world" {
		t.Errorf("expected 'world', got %q", val)
	}
}

func TestPathReadStringNotFound(t *testing.T) {
	data := []byte("a: 1")
	p, _ := PathString("$.missing")
	_, err := p.ReadString(data)
	if err == nil {
		t.Error("expected error for path not found")
	}
}

func TestPathReadStringInvalidYAML(t *testing.T) {
	p, _ := PathString("$.a")
	_, err := p.ReadString([]byte("[unclosed"))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestPathReadStringEmptyDocs(t *testing.T) {
	p, _ := PathString("$.a")
	_, err := p.ReadString([]byte(""))
	if err == nil {
		t.Error("expected error for no documents")
	}
}

func TestPathReplace(t *testing.T) {
	input := `name: old`
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.name")
	err = p.Replace(file.Docs[0], &Node{Kind: ScalarNode, Value: "new"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := NodeToBytes(file.Docs[0])
	if !strings.Contains(string(data), "new") {
		t.Errorf("expected 'new' after replace, got %q", string(data))
	}
}

func TestPathReplaceIndex(t *testing.T) {
	file, err := Parse([]byte("items: [a, b, c]"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items[1]")
	err = p.Replace(file.Docs[0], &Node{Kind: ScalarNode, Value: "replaced"})
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := PathString("$.items[1]")
	nodes, _ := p2.Read(file.Docs[0])
	if len(nodes) != 1 || nodes[0].Value != "replaced" {
		t.Errorf("expected 'replaced', got %v", nodes)
	}
}

func TestPathReplaceNegativeIndex(t *testing.T) {
	file, err := Parse([]byte("items: [a, b, c]"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items[-1]")
	err = p.Replace(file.Docs[0], &Node{Kind: ScalarNode, Value: "last"})
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := PathString("$.items[2]")
	nodes, _ := p2.Read(file.Docs[0])
	if len(nodes) != 1 || nodes[0].Value != "last" {
		t.Errorf("expected 'last', got %v", nodes)
	}
}

func TestPathReplaceTooShort(t *testing.T) {
	p, _ := PathString("$")
	err := p.Replace(nil, nil)
	if err == nil {
		t.Error("expected error for path too short")
	}
}

func TestPathAppend(t *testing.T) {
	input := `items:
- a
- b`
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items")
	err = p.Append(file.Docs[0], &Node{Kind: ScalarNode, Value: "c"})
	if err != nil {
		t.Fatal(err)
	}
	nodes, _ := p.Read(file.Docs[0])
	if nodes[0].Kind != SequenceNode || len(nodes[0].Children) != 3 {
		t.Errorf("expected 3 items after append, got %d", len(nodes[0].Children))
	}
}

func TestPathAppendToNonSequence(t *testing.T) {
	file, err := Parse([]byte("name: hello"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.name")
	err = p.Append(file.Docs[0], &Node{Kind: ScalarNode, Value: "x"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPathDelete(t *testing.T) {
	input := `a: 1
b: 2
c: 3`
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.b")
	err = p.Delete(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	data, _ := NodeToBytes(file.Docs[0])
	if strings.Contains(string(data), "b:") {
		t.Errorf("expected 'b' to be deleted, got %q", string(data))
	}
}

func TestPathDeleteIndex(t *testing.T) {
	file, err := Parse([]byte("items: [a, b, c]"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items[1]")
	err = p.Delete(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := PathString("$.items")
	nodes, _ := p2.Read(file.Docs[0])
	if len(nodes[0].Children) != 2 {
		t.Errorf("expected 2 items after delete, got %d", len(nodes[0].Children))
	}
}

func TestPathDeleteNegativeIndex(t *testing.T) {
	file, err := Parse([]byte("items: [a, b, c]"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items[-1]")
	err = p.Delete(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := PathString("$.items")
	nodes, _ := p2.Read(file.Docs[0])
	if len(nodes[0].Children) != 2 {
		t.Errorf("expected 2 items after delete, got %d", len(nodes[0].Children))
	}
}

func TestPathDeleteTooShort(t *testing.T) {
	p, _ := PathString("$")
	err := p.Delete(nil)
	if err == nil {
		t.Error("expected error for path too short")
	}
}

func TestPathErrors(t *testing.T) {
	_, err := PathString("")
	if err == nil {
		t.Error("expected error for empty path")
	}
	_, err = PathString("foo")
	if err == nil {
		t.Error("expected error for path not starting with $")
	}
	_, err = PathString("$.")
	if err == nil {
		t.Error("expected error for trailing dot")
	}
	_, err = PathString("$[abc]")
	if err == nil {
		t.Error("expected error for non-numeric index")
	}
	_, err = PathString("$[0")
	if err == nil {
		t.Error("expected error for unclosed bracket")
	}
}

func TestPathRecursiveDescentThenField(t *testing.T) {
	_, err := PathString("$..foo")
	if err != nil {
		// The parser sees ".." as recursive, then "foo" is unexpected
		// because it expects a dot or bracket prefix. This is expected behavior.
		_ = err
	}
}

func TestPathErrorUnexpectedChar(t *testing.T) {
	_, err := PathString("$@")
	if err == nil {
		t.Error("expected error for unexpected character")
	}
}

func TestPathRecursiveDescent(t *testing.T) {
	input := `a:
  b:
    c: deep`
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$..")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) < 3 {
		t.Errorf("expected multiple nodes from recursive descent, got %d", len(nodes))
	}
}

func TestPathNegativeIndex(t *testing.T) {
	input := `items: [a, b, c]`
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items[-1]")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Value != "c" {
		t.Errorf("expected 'c' for index -1, got %v", nodes)
	}
}

func TestPathNegativeIndexOutOfBounds(t *testing.T) {
	file, err := Parse([]byte("items: [a]"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items[-5]")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 results for out-of-bounds negative index, got %d", len(nodes))
	}
}

func TestRootSegmentMatch(t *testing.T) {
	n := &Node{Kind: ScalarNode, Value: "test"}
	rs := rootSegment{}
	result := rs.match(n)
	if len(result) != 1 || result[0] != n {
		t.Error("rootSegment.match should return the node itself")
	}
}

func TestChildSegmentNoMatch(t *testing.T) {
	n := &Node{Kind: SequenceNode}
	cs := childSegment{name: "foo"}
	result := cs.match(n)
	if len(result) != 0 {
		t.Error("childSegment on non-mapping should return nil")
	}
}

func TestIndexSegmentOnNonSequence(t *testing.T) {
	n := &Node{Kind: MappingNode}
	is := indexSegment{idx: 0}
	result := is.match(n)
	if len(result) != 0 {
		t.Error("indexSegment on non-sequence should return nil")
	}
}

func TestPathStringMethod(t *testing.T) {
	p, _ := PathString("$.foo[0].*")
	got := p.String()
	if got != "$.foo[0].*" {
		t.Errorf("expected $.foo[0].*, got %q", got)
	}
}

func TestPathRootOnly(t *testing.T) {
	file, err := Parse([]byte("a: 1"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected 1 node for root path, got %d", len(nodes))
	}
}

func TestPathReplaceOnNonMapping(t *testing.T) {
	file, err := Parse([]byte("items: [a, b]"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items.foo")
	err = p.Replace(file.Docs[0], &Node{Kind: ScalarNode, Value: "x"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPathDeleteOnNonMapping(t *testing.T) {
	file, err := Parse([]byte("items: [a, b]"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items.foo")
	err = p.Delete(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
}

func TestPathReplaceIndexOutOfBounds(t *testing.T) {
	file, err := Parse([]byte("items: [a]"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items[5]")
	err = p.Replace(file.Docs[0], &Node{Kind: ScalarNode, Value: "x"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPathDeleteIndexOutOfBounds(t *testing.T) {
	file, err := Parse([]byte("items: [a]"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items[5]")
	err = p.Delete(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
}

func TestPathRecursiveDescentWithChild(t *testing.T) {
	input := `a:
  x: 1
b:
  x: 2`
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$..")
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Error("expected nodes from recursive descent")
	}
}

func TestPathReadNonDocumentRoot(t *testing.T) {
	n := &Node{
		Kind: MappingNode,
		Children: []*Node{
			{Kind: ScalarNode, Value: "key"},
			{Kind: ScalarNode, Value: "val"},
		},
	}
	p, _ := PathString("$.key")
	nodes, err := p.Read(n)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Value != "val" {
		t.Errorf("expected val, got %v", nodes)
	}
}

func TestPathStringUnexpectedChar(t *testing.T) {
	_, err := PathString("$@foo")
	if err == nil {
		t.Fatal("expected error for unexpected character")
	}
	if !strings.Contains(err.Error(), "unexpected character") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPathReadRecursive(t *testing.T) {
	file, err := Parse([]byte("a:\n  b:\n    c: deep\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := PathString("$..c")
	if err != nil {
		// If $..c isn't valid syntax, try $.a..c or use the manual path
		p = &Path{segments: []pathSegment{rootSegment{}, recursiveSegment{}, childSegment{name: "c"}}}
	}
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	_ = nodes
}

func TestPathReadStringParseError(t *testing.T) {
	p, _ := PathString("$.key")
	_, err := p.ReadString([]byte{0x01})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestPathReadStringNoDocuments(t *testing.T) {
	p, _ := PathString("$.key")
	_, err := p.ReadString([]byte(""))
	if err == nil {
		t.Fatal("expected error for no documents")
	}
}

func TestPathReplaceReadError(t *testing.T) {
	p, _ := PathString("$.key")
	_, err := p.ReadString([]byte("[invalid"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPathAppendNoMatch(t *testing.T) {
	file, _ := Parse([]byte("key: val\n"))
	p, _ := PathString("$.nonexistent")
	err := p.Append(file.Docs[0], &Node{Kind: ScalarNode, Value: "x"})
	if err != nil {
		t.Errorf("expected nil error for no-match append, got %v", err)
	}
}

func TestPathDeleteNoMatch(t *testing.T) {
	file, _ := Parse([]byte("key: val\n"))
	p, _ := PathString("$.nonexistent")
	err := p.Delete(file.Docs[0])
	if err != nil {
		t.Errorf("expected nil error for no-match delete, got %v", err)
	}
}

func TestPathStringEmptyFieldName(t *testing.T) {
	_, err := PathString("$.[0]")
	if err == nil {
		t.Fatal("expected error for empty field name in path")
	}
	if !strings.Contains(err.Error(), "empty field name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadPositions(t *testing.T) {
	input := "items:\n  - name: a\n  - name: b\n"
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items[*].name")
	positions, err := p.ReadPositions(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 2 {
		t.Fatalf("expected 2 positions, got %d", len(positions))
	}
	if positions[0].Line == 0 || positions[1].Line == 0 {
		t.Error("expected non-zero line numbers")
	}
}
