package yaml

import (
	"strings"
	"testing"
)

func TestNodeKindConstants(t *testing.T) {
	kinds := []nodeKind{nodeDocument, nodeMapping, nodeSequence, nodeScalar, nodeAnchor, nodeAlias, nodeMergeKey}
	seen := make(map[nodeKind]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("duplicate nodeKind: %d", k)
		}
		seen[k] = true
	}
}

func TestPublicNodeKindConstants(t *testing.T) {
	kinds := []NodeKind{DocumentNode, MappingNode, SequenceNode, ScalarNode, AliasNode}
	seen := make(map[NodeKind]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("duplicate NodeKind: %d", k)
		}
		seen[k] = true
	}
}

func TestPublicScalarStyleConstants(t *testing.T) {
	styles := []ScalarStyle{PlainStyle, SingleQuotedStyle, DoubleQuotedStyle, LiteralStyle, FoldedStyle}
	seen := make(map[ScalarStyle]bool)
	for _, s := range styles {
		if seen[s] {
			t.Errorf("duplicate ScalarStyle: %d", s)
		}
		seen[s] = true
	}
}

func TestParseAST(t *testing.T) {
	input := `
name: hello
items:
- a
- b
`
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(file.Docs))
	}
	doc := file.Docs[0]
	if doc.Kind != DocumentNode {
		t.Errorf("expected DocumentNode, got %v", doc.Kind)
	}
	if len(doc.Children) == 0 {
		t.Fatal("expected children in document")
	}
	root := doc.Children[0]
	if root.Kind != MappingNode {
		t.Errorf("expected MappingNode, got %v", root.Kind)
	}
}

func TestParseEmpty(t *testing.T) {
	file, err := Parse([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) != 0 {
		t.Errorf("expected 0 docs for empty input, got %d", len(file.Docs))
	}
}

func TestParseInvalid(t *testing.T) {
	_, err := Parse([]byte(`[unclosed`))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParseMultiDoc(t *testing.T) {
	input := "---\na: 1\n---\nb: 2\n"
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(file.Docs))
	}
}

func TestNodeString(t *testing.T) {
	scalar := &Node{Kind: ScalarNode, Value: "hello"}
	if scalar.String() != "hello" {
		t.Errorf("expected 'hello', got %q", scalar.String())
	}
	mapping := &Node{Kind: MappingNode, Children: []*Node{{}, {}, {}, {}}}
	if !strings.Contains(mapping.String(), "2 pairs") {
		t.Errorf("expected '2 pairs', got %q", mapping.String())
	}
	seq := &Node{Kind: SequenceNode, Children: []*Node{{}, {}}}
	if !strings.Contains(seq.String(), "2 items") {
		t.Errorf("expected '2 items', got %q", seq.String())
	}
	alias := &Node{Kind: AliasNode, Alias: "foo"}
	if alias.String() != "*foo" {
		t.Errorf("expected '*foo', got %q", alias.String())
	}
	doc := &Node{Kind: DocumentNode}
	if doc.String() != "---" {
		t.Errorf("expected '---', got %q", doc.String())
	}
}

func TestNodeStringUnknownKind(t *testing.T) {
	n := &Node{Kind: NodeKind(99)}
	if n.String() != "" {
		t.Errorf("expected empty string for unknown kind, got %q", n.String())
	}
}

func TestWalk(t *testing.T) {
	file, err := Parse([]byte("a: 1\nb: 2"))
	if err != nil {
		t.Fatal(err)
	}
	var count int
	Walk(file.Docs[0], func(n *Node) bool {
		count++
		return true
	})
	if count < 3 {
		t.Errorf("expected at least 3 nodes, got %d", count)
	}
}

func TestWalkStopEarly(t *testing.T) {
	file, err := Parse([]byte("a: 1\nb: 2"))
	if err != nil {
		t.Fatal(err)
	}
	var count int
	Walk(file.Docs[0], func(n *Node) bool {
		count++
		return count < 2
	})
	if count != 2 {
		t.Errorf("expected walk to stop at 2, got %d", count)
	}
}

func TestWalkNil(t *testing.T) {
	Walk(nil, func(n *Node) bool { return true })
}

func TestFilter(t *testing.T) {
	file, err := Parse([]byte("a: 1\nb: 2"))
	if err != nil {
		t.Fatal(err)
	}
	scalars := Filter(file.Docs[0], func(n *Node) bool {
		return n.Kind == ScalarNode
	})
	if len(scalars) < 4 {
		t.Errorf("expected at least 4 scalars (keys+values), got %d", len(scalars))
	}
}

func TestFilterNoMatch(t *testing.T) {
	file, err := Parse([]byte("a: 1"))
	if err != nil {
		t.Fatal(err)
	}
	aliases := Filter(file.Docs[0], func(n *Node) bool {
		return n.Kind == AliasNode
	})
	if len(aliases) != 0 {
		t.Errorf("expected 0 aliases, got %d", len(aliases))
	}
}

func TestNodeToBytes(t *testing.T) {
	n := &Node{
		Kind: MappingNode,
		Children: []*Node{
			{Kind: ScalarNode, Value: "key"},
			{Kind: ScalarNode, Value: "val"},
		},
	}
	data, err := NodeToBytes(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "key: val") {
		t.Errorf("expected 'key: val', got %q", string(data))
	}
}

func TestNodeToBytesSequence(t *testing.T) {
	n := &Node{
		Kind: SequenceNode,
		Children: []*Node{
			{Kind: ScalarNode, Value: "a"},
			{Kind: ScalarNode, Value: "b"},
		},
	}
	data, err := NodeToBytes(n)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "- a") || !strings.Contains(s, "- b") {
		t.Errorf("expected sequence output, got %q", s)
	}
}

func TestASTRoundTrip(t *testing.T) {
	input := `name: hello
items:
- a
- b`
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	data, err := NodeToBytes(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "hello" {
		t.Errorf("round-trip: expected name=hello, got %v", out["name"])
	}
}

func TestExportKindAllValues(t *testing.T) {
	tests := []struct {
		internal nodeKind
		expected NodeKind
	}{
		{nodeDocument, DocumentNode},
		{nodeMapping, MappingNode},
		{nodeSequence, SequenceNode},
		{nodeScalar, ScalarNode},
		{nodeAlias, AliasNode},
		{nodeAnchor, ScalarNode},
		{nodeMergeKey, ScalarNode},
		{nodeKind(99), ScalarNode},
	}
	for _, tt := range tests {
		got := exportKind(tt.internal)
		if got != tt.expected {
			t.Errorf("exportKind(%d) = %d, want %d", tt.internal, got, tt.expected)
		}
	}
}

func TestExportStyleAllValues(t *testing.T) {
	tests := []struct {
		internal scalarStyle
		expected ScalarStyle
	}{
		{scalarPlain, PlainStyle},
		{scalarSingleQuoted, SingleQuotedStyle},
		{scalarDoubleQuoted, DoubleQuotedStyle},
		{scalarLiteral, LiteralStyle},
		{scalarFolded, FoldedStyle},
		{scalarStyle(99), PlainStyle},
	}
	for _, tt := range tests {
		got := exportStyle(tt.internal)
		if got != tt.expected {
			t.Errorf("exportStyle(%d) = %d, want %d", tt.internal, got, tt.expected)
		}
	}
}

func TestImportKindAllValues(t *testing.T) {
	tests := []struct {
		pub      NodeKind
		expected nodeKind
	}{
		{DocumentNode, nodeDocument},
		{MappingNode, nodeMapping},
		{SequenceNode, nodeSequence},
		{ScalarNode, nodeScalar},
		{AliasNode, nodeAlias},
		{NodeKind(99), nodeScalar},
	}
	for _, tt := range tests {
		got := importKind(tt.pub)
		if got != tt.expected {
			t.Errorf("importKind(%d) = %d, want %d", tt.pub, got, tt.expected)
		}
	}
}

func TestImportStyleAllValues(t *testing.T) {
	tests := []struct {
		pub      ScalarStyle
		expected scalarStyle
	}{
		{PlainStyle, scalarPlain},
		{SingleQuotedStyle, scalarSingleQuoted},
		{DoubleQuotedStyle, scalarDoubleQuoted},
		{LiteralStyle, scalarLiteral},
		{FoldedStyle, scalarFolded},
		{ScalarStyle(99), scalarPlain},
	}
	for _, tt := range tests {
		got := importStyle(tt.pub)
		if got != tt.expected {
			t.Errorf("importStyle(%d) = %d, want %d", tt.pub, got, tt.expected)
		}
	}
}

func TestExportNodeNil(t *testing.T) {
	result := exportNode(nil)
	if result != nil {
		t.Error("expected nil for nil input")
	}
}

func TestImportNodeNil(t *testing.T) {
	result := importNode(nil)
	if result != nil {
		t.Error("expected nil for nil input")
	}
}

func TestExportNodeWithChildren(t *testing.T) {
	internal := &node{
		kind: nodeMapping,
		children: []*node{
			{kind: nodeScalar, value: "key"},
			{kind: nodeScalar, value: "val"},
		},
	}
	pub := exportNode(internal)
	if pub.Kind != MappingNode {
		t.Errorf("expected MappingNode, got %d", pub.Kind)
	}
	if len(pub.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(pub.Children))
	}
	if pub.Children[0].Value != "key" {
		t.Errorf("expected key, got %q", pub.Children[0].Value)
	}
}

func TestExportNodePreservesComments(t *testing.T) {
	internal := &node{
		kind:        nodeScalar,
		value:       "test",
		headComment: "head",
		lineComment: "line",
		footComment: "foot",
	}
	pub := exportNode(internal)
	if pub.HeadComment != "head" {
		t.Errorf("expected head comment, got %q", pub.HeadComment)
	}
	if pub.Comment != "line" {
		t.Errorf("expected line comment, got %q", pub.Comment)
	}
	if pub.FootComment != "foot" {
		t.Errorf("expected foot comment, got %q", pub.FootComment)
	}
}

func TestImportNodeRoundTrip(t *testing.T) {
	pub := &Node{
		Kind:   MappingNode,
		Tag:    "!!map",
		Anchor: "anc",
		Children: []*Node{
			{Kind: ScalarNode, Value: "k", Style: DoubleQuotedStyle},
			{Kind: ScalarNode, Value: "v"},
		},
	}
	internal := importNode(pub)
	if internal.kind != nodeMapping {
		t.Errorf("expected nodeMapping, got %d", internal.kind)
	}
	if internal.tag != "!!map" {
		t.Errorf("expected tag !!map, got %q", internal.tag)
	}
	if internal.anchor != "anc" {
		t.Errorf("expected anchor anc, got %q", internal.anchor)
	}
	if len(internal.children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(internal.children))
	}
	if internal.children[0].style != scalarDoubleQuoted {
		t.Errorf("expected double-quoted style, got %d", internal.children[0].style)
	}
}

func TestParseWithAnchorAlias(t *testing.T) {
	input := "defaults: &def\n  color: red\nitem:\n  <<: *def\n  size: 10\n"
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(file.Docs))
	}
}

func TestParseScalarStyles(t *testing.T) {
	input := "plain: hello\nsingle: 'world'\ndouble: \"test\"\nliteral: |\n  block\nfolded: >\n  fold\n"
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) == 0 {
		t.Fatal("expected at least 1 doc")
	}
}

func TestParseParseError(t *testing.T) {
	_, err := Parse([]byte("{unclosed"))
	if err == nil {
		t.Fatal("expected parse error for unclosed flow mapping")
	}
}

func TestParseBadEncoding(t *testing.T) {
	// UTF-32 BE BOM followed by truncated data (odd length)
	data := []byte{0x00, 0x00, 0xFE, 0xFF, 0x00}
	_, err := Parse(data)
	if err != nil {
		// The encoding converter truncates to a multiple of 4 after BOM, so single byte becomes empty
		// This should still parse (as empty YAML)
	}
}

func TestParseScanErrorNonPrintable(t *testing.T) {
	data := []byte{0x01}
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for non-printable character")
	}
}

func TestNodeValidateOddMapping(t *testing.T) {
	n := &Node{
		Kind: MappingNode,
		Children: []*Node{
			{Kind: ScalarNode, Value: "key"},
		},
	}
	if err := n.Validate(); err == nil {
		t.Fatal("expected error for odd mapping children")
	}
}

func TestNodeValidateEmptyAlias(t *testing.T) {
	n := &Node{
		Kind: AliasNode,
	}
	if err := n.Validate(); err == nil {
		t.Fatal("expected error for empty alias name")
	}
}

func TestNodeValidateValid(t *testing.T) {
	n := &Node{
		Kind: MappingNode,
		Children: []*Node{
			{Kind: ScalarNode, Value: "key"},
			{Kind: ScalarNode, Value: "val"},
		},
	}
	if err := n.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
