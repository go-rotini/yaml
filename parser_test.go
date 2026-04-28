package yaml

import (
	"strings"
	"testing"
)

func scanTokens(t *testing.T, input string) []token {
	t.Helper()
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	return tokens
}

func parseNodes(t *testing.T, input string) []*node {
	t.Helper()
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return docs
}

func TestParserEmpty(t *testing.T) {
	docs := parseNodes(t, "")
	if len(docs) != 0 {
		t.Errorf("expected 0 docs for empty input, got %d", len(docs))
	}
}

func TestParserSingleScalar(t *testing.T) {
	docs := parseNodes(t, "hello")
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	doc := docs[0]
	if doc.kind != nodeDocument {
		t.Errorf("expected document node, got %d", doc.kind)
	}
	if len(doc.children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(doc.children))
	}
	if doc.children[0].kind != nodeScalar || doc.children[0].value != "hello" {
		t.Errorf("expected scalar 'hello', got %+v", doc.children[0])
	}
}

func TestParserMapping(t *testing.T) {
	docs := parseNodes(t, "a: 1\nb: 2")
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	root := docs[0].children[0]
	if root.kind != nodeMapping {
		t.Errorf("expected mapping, got %d", root.kind)
	}
	if len(root.children) != 4 {
		t.Errorf("expected 4 children (2 key-value pairs), got %d", len(root.children))
	}
}

func TestParserSequence(t *testing.T) {
	docs := parseNodes(t, "- a\n- b\n- c")
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	root := docs[0].children[0]
	if root.kind != nodeSequence {
		t.Errorf("expected sequence, got %d", root.kind)
	}
	if len(root.children) != 3 {
		t.Errorf("expected 3 children, got %d", len(root.children))
	}
}

func TestParserNestedMapping(t *testing.T) {
	docs := parseNodes(t, "a:\n  b:\n    c: deep")
	root := docs[0].children[0]
	if root.kind != nodeMapping {
		t.Fatalf("expected mapping, got %d", root.kind)
	}
	// a -> mapping(b -> mapping(c -> deep))
	val := root.children[1] // value of 'a'
	if val.kind != nodeMapping {
		t.Errorf("expected nested mapping, got %d", val.kind)
	}
}

func TestParserFlowMapping(t *testing.T) {
	docs := parseNodes(t, "{a: 1, b: 2}")
	root := docs[0].children[0]
	if root.kind != nodeMapping {
		t.Errorf("expected mapping, got %d", root.kind)
	}
	if len(root.children) != 4 {
		t.Errorf("expected 4 children, got %d", len(root.children))
	}
}

func TestParserFlowSequence(t *testing.T) {
	docs := parseNodes(t, "[a, b, c]")
	root := docs[0].children[0]
	if root.kind != nodeSequence {
		t.Errorf("expected sequence, got %d", root.kind)
	}
	if len(root.children) != 3 {
		t.Errorf("expected 3 children, got %d", len(root.children))
	}
}

func TestParserMultiDocument(t *testing.T) {
	docs := parseNodes(t, "---\na: 1\n---\nb: 2")
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
}

func TestParserDocumentEnd(t *testing.T) {
	docs := parseNodes(t, "---\na: 1\n...")
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
}

func TestParserAnchorAlias(t *testing.T) {
	docs := parseNodes(t, "a: &anc value\nb: *anc")
	root := docs[0].children[0]
	// key 'a', value with anchor, key 'b', alias
	if len(root.children) < 4 {
		t.Fatalf("expected at least 4 children, got %d", len(root.children))
	}
	aValue := root.children[1]
	if aValue.anchor != "anc" {
		t.Errorf("expected anchor 'anc', got %q", aValue.anchor)
	}
	bValue := root.children[3]
	if bValue.kind != nodeAlias {
		t.Errorf("expected alias node, got %d", bValue.kind)
	}
}

func TestParserMergeKey(t *testing.T) {
	input := "defaults: &def\n  color: red\nitem:\n  <<: *def\n  size: 10"
	docs := parseNodes(t, input)
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
}

func TestParserTag(t *testing.T) {
	docs := parseNodes(t, "a: !!str 123")
	root := docs[0].children[0]
	val := root.children[1]
	if !strings.Contains(val.tag, "str") {
		t.Errorf("expected str tag, got %q", val.tag)
	}
}

func TestParserDirective(t *testing.T) {
	docs := parseNodes(t, "%YAML 1.2\n---\na: 1")
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
}

func TestParserTAGDirective(t *testing.T) {
	docs := parseNodes(t, "%TAG !custom! tag:example.com,2024:\n---\na: !custom!val test")
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	root := docs[0].children[0]
	val := root.children[1]
	if !strings.Contains(val.tag, "example.com") {
		t.Errorf("expected resolved tag, got %q", val.tag)
	}
}

func TestParserComments(t *testing.T) {
	docs := parseNodes(t, "# head comment\na: 1 # line comment\n# foot comment")
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
}

func TestParserScalarStyles(t *testing.T) {
	input := "plain: hello\nsingle: 'world'\ndouble: \"test\"\nliteral: |\n  block\nfolded: >\n  fold\n"
	docs := parseNodes(t, input)
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
}

func TestParserExplicitKey(t *testing.T) {
	docs := parseNodes(t, "? key\n: value")
	root := docs[0].children[0]
	if root.kind != nodeMapping {
		t.Errorf("expected mapping, got %d", root.kind)
	}
}

func TestParserNestedFlowInBlock(t *testing.T) {
	docs := parseNodes(t, "a: {b: [1, 2]}")
	root := docs[0].children[0]
	if root.kind != nodeMapping {
		t.Fatalf("expected mapping, got %d", root.kind)
	}
	val := root.children[1] // value of 'a'
	if val.kind != nodeMapping {
		t.Errorf("expected nested flow mapping, got %d", val.kind)
	}
}

func TestParserMaxNodes(t *testing.T) {
	tokens := scanTokens(t, "a:\n  b:\n    c:\n      d: deep")
	p := newParser(tokens)
	p.maxNodes = 3
	_, err := p.parse()
	if err == nil {
		t.Error("expected error for exceeded max nodes")
	}
	if !strings.Contains(err.Error(), "node") {
		t.Errorf("expected node limit error, got: %v", err)
	}
}

func TestParserMaxNodesZeroUnlimited(t *testing.T) {
	tokens := scanTokens(t, "a:\n  b:\n    c: deep")
	p := newParser(tokens)
	p.maxNodes = 0
	_, err := p.parse()
	if err != nil {
		t.Fatalf("maxNodes=0 should be unlimited: %v", err)
	}
}

func TestParserImplicitDocument(t *testing.T) {
	docs := parseNodes(t, "a: 1")
	if len(docs) != 1 {
		t.Fatalf("expected 1 implicit doc, got %d", len(docs))
	}
	if docs[0].kind != nodeDocument {
		t.Errorf("expected document node, got %d", docs[0].kind)
	}
}

func TestParserExplicitDocument(t *testing.T) {
	docs := parseNodes(t, "---\na: 1")
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].kind != nodeDocument {
		t.Errorf("expected document node, got %d", docs[0].kind)
	}
}

func TestParserEmptyFlowMapping(t *testing.T) {
	docs := parseNodes(t, "a: {}")
	root := docs[0].children[0]
	val := root.children[1]
	if val.kind != nodeMapping {
		t.Errorf("expected mapping, got %d", val.kind)
	}
	if len(val.children) != 0 {
		t.Errorf("expected 0 children for empty mapping, got %d", len(val.children))
	}
}

func TestParserEmptyFlowSequence(t *testing.T) {
	docs := parseNodes(t, "a: []")
	root := docs[0].children[0]
	val := root.children[1]
	if val.kind != nodeSequence {
		t.Errorf("expected sequence, got %d", val.kind)
	}
	if len(val.children) != 0 {
		t.Errorf("expected 0 children for empty sequence, got %d", len(val.children))
	}
}

func TestNewParserFields(t *testing.T) {
	tokens := []token{{kind: tokenStreamStart}, {kind: tokenStreamEnd}}
	p := newParser(tokens)
	if p.anchors == nil {
		t.Error("expected non-nil anchors map")
	}
	if p.tagHandles == nil {
		t.Error("expected non-nil tagHandles map")
	}
	if p.tagHandles["!!"] != "tag:yaml.org,2002:" {
		t.Errorf("expected default !! handle, got %q", p.tagHandles["!!"])
	}
}

func TestResolveTagShorthand(t *testing.T) {
	tokens := []token{{kind: tokenStreamStart}, {kind: tokenStreamEnd}}
	p := newParser(tokens)
	resolved, err := p.resolveTag("!!str")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "tag:yaml.org,2002:str" {
		t.Errorf("expected tag:yaml.org,2002:str, got %q", resolved)
	}
}

func TestResolveTagVerbatim(t *testing.T) {
	tokens := []token{{kind: tokenStreamStart}, {kind: tokenStreamEnd}}
	p := newParser(tokens)
	resolved, err := p.resolveTag("!<tag:example.com,2024:custom>")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "tag:example.com,2024:custom" {
		t.Errorf("expected verbatim tag, got %q", resolved)
	}
}

func TestResolveTagLocal(t *testing.T) {
	tokens := []token{{kind: tokenStreamStart}, {kind: tokenStreamEnd}}
	p := newParser(tokens)
	resolved, err := p.resolveTag("!local")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "!local" {
		t.Errorf("expected !local, got %q", resolved)
	}
}

func TestResolveTagEmpty(t *testing.T) {
	p := newParser(nil)
	resolved, err := p.resolveTag("")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "" {
		t.Error("expected empty tag to resolve to empty")
	}
}

func TestResolveTagCustomHandle(t *testing.T) {
	p := newParser(nil)
	p.tagHandles["!e!"] = "tag:example.com,"
	got, err := p.resolveTag("!e!name")
	if err != nil {
		t.Fatal(err)
	}
	if got != "tag:example.com,name" {
		t.Errorf("expected custom tag handle resolution, got %q", got)
	}
}

func TestResolveTagCustomPrimaryHandle(t *testing.T) {
	p := newParser(nil)
	p.tagHandles["!"] = "tag:example.com,"
	got, err := p.resolveTag("!name")
	if err != nil {
		t.Fatal(err)
	}
	if got != "tag:example.com,name" {
		t.Errorf("expected custom ! handle, got %q", got)
	}
}

func TestParseDocumentAfterDirective(t *testing.T) {
	input := "%YAML 1.2\n---\nkey: val"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatal("expected at least 1 document")
	}
}

func TestParseStreamEndAfterDirective(t *testing.T) {
	input := "%YAML 1.2\n"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Error("expected error for directive without document start")
	}
}

func TestParseNodeValueToken(t *testing.T) {
	input := "? key\n: val\n"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatal("expected document")
	}
}

func TestParseBlockSequenceEmptyEntries(t *testing.T) {
	input := "-\n-\n- val\n"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 || len(docs[0].children) == 0 {
		t.Fatal("expected document with sequence")
	}
	seq := docs[0].children[0]
	if len(seq.children) != 3 {
		t.Errorf("expected 3 items, got %d", len(seq.children))
	}
}

func TestParseFlowMappingUnterminated(t *testing.T) {
	input := "{key: val"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error for unterminated flow mapping")
	}
}

func TestParseFlowSequenceUnterminated(t *testing.T) {
	input := "[a, b"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error for unterminated flow sequence")
	}
}

func TestParseFlowSequenceWithKeyValue(t *testing.T) {
	input := "[? a : b, c]"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 || len(docs[0].children) == 0 {
		t.Fatal("expected document with sequence")
	}
}

func TestParseFlowSequenceKeyOnly(t *testing.T) {
	input := "[? a, b]"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 || len(docs[0].children) == 0 {
		t.Fatal("expected document with sequence")
	}
}

func TestParseFlowMappingError(t *testing.T) {
	input := "{ key: [unclosed }"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error for malformed flow mapping value")
	}
}

func TestParseFlowSequenceError(t *testing.T) {
	input := "[ {unclosed ]"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error for malformed flow sequence item")
	}
}

func TestCollectComments(t *testing.T) {
	p := newParser([]token{
		{kind: tokenComment, value: "comment1"},
		{kind: tokenComment, value: "comment2"},
		{kind: tokenStreamEnd},
	})
	result := p.collectComments()
	if result != "comment1\ncomment2" {
		t.Errorf("expected collected comments, got %q", result)
	}
}

func TestCollectCommentsEmpty(t *testing.T) {
	p := newParser([]token{
		{kind: tokenStreamEnd},
	})
	result := p.collectComments()
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestPeekPastEnd(t *testing.T) {
	p := newParser([]token{})
	tok := p.peek()
	if tok.kind != tokenStreamEnd {
		t.Errorf("expected tokenStreamEnd, got %v", tok.kind)
	}
}

func TestParseMappingEntryNilValue(t *testing.T) {
	input := "key:\n"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 || len(docs[0].children) == 0 {
		t.Fatal("expected document with mapping")
	}
	mapping := docs[0].children[0]
	if len(mapping.children) != 2 {
		t.Fatalf("expected 2 children (key+val), got %d", len(mapping.children))
	}
	if mapping.children[1].value != "" {
		t.Errorf("expected empty value, got %q", mapping.children[1].value)
	}
}

func TestParseBlockMappingWithTag(t *testing.T) {
	input := "!custom key: val\n"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatal("expected document")
	}
}

func TestParseBlockMappingBreakOnUnexpected(t *testing.T) {
	input := "key: val\n---\nnew: doc\n"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) < 2 {
		t.Errorf("expected 2 documents, got %d", len(docs))
	}
}

func TestParseFlowMappingValueEmptyBeforeEnd(t *testing.T) {
	input := "{key:}"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 || len(docs[0].children) == 0 {
		t.Fatal("expected document with mapping")
	}
}

func TestParseFlowMappingValueEmptyBeforeEntry(t *testing.T) {
	input := "{a:, b: c}"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 || len(docs[0].children) == 0 {
		t.Fatal("expected document with mapping")
	}
}

func TestParseFlowSequenceEntryError(t *testing.T) {
	// Flow sequence with key-value where value parsing fails
	input := "[? a : {unclosed]"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error for malformed flow sequence entry value")
	}
}

func TestParseBlockSequenceErrorInItem(t *testing.T) {
	input := "- {unclosed"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error for malformed sequence item")
	}
}

func TestParseResolveTagCustomHandleFallthrough(t *testing.T) {
	p := newParser(nil)
	p.tagHandles["!custom!"] = "tag:custom.org,"
	got, err := p.resolveTag("!custom!type")
	if err != nil {
		t.Fatal(err)
	}
	if got != "tag:custom.org,type" {
		t.Errorf("expected custom handle resolution, got %q", got)
	}
	got2, err := p.resolveTag("!other")
	if err != nil {
		t.Fatal(err)
	}
	if got2 != "!other" {
		t.Errorf("expected unchanged for non-matching handle, got %q", got2)
	}
}

func TestParseNodeBlockEntry(t *testing.T) {
	input := "- a\n- b\n"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 || docs[0].children[0].kind != nodeSequence {
		t.Fatal("expected sequence from block entry")
	}
}

func TestParseNodeKey(t *testing.T) {
	input := "? a\n: b\n"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 || docs[0].children[0].kind != nodeMapping {
		t.Fatal("expected mapping from key token")
	}
}

func TestParseNodeImplicitScalar(t *testing.T) {
	input := "---\n---\n"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) < 2 {
		t.Errorf("expected at least 2 empty documents, got %d", len(docs))
	}
}

func TestParseMappingEntryNilValueAfterKey(t *testing.T) {
	input := "? a\n"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatal("expected document")
	}
}

func TestParseBlockMappingAliasKey(t *testing.T) {
	input := "{a: &k val1, *k : val2}\n"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatal("expected document")
	}
}

func TestParseFlowSequenceKeyValueError(t *testing.T) {
	input := "[? a : {unclosed]"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFlowSequenceItemError(t *testing.T) {
	input := "[ {unclosed ]"
	tokens := scanTokens(t, input)
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error for bad flow sequence item")
	}
}

func TestParseNodeBlockEntryError(t *testing.T) {
	tokens := []token{
		{kind: tokenStreamStart},
		{kind: tokenBlockEntry},
		{kind: tokenFlowMappingStart},
		{kind: tokenStreamEnd},
	}
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error from block entry with unterminated flow mapping")
	}
}

func TestParseNodeTokenKeyError(t *testing.T) {
	tokens := []token{
		{kind: tokenStreamStart},
		{kind: tokenKey},
		{kind: tokenFlowMappingStart},
		{kind: tokenStreamEnd},
	}
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error from tokenKey with unterminated flow mapping")
	}
}

func TestParseBlockMappingScalarEntryError(t *testing.T) {
	tokens := []token{
		{kind: tokenStreamStart},
		{kind: tokenBlockMappingStart},
		{kind: tokenScalar, value: "key"},
		{kind: tokenValue},
		{kind: tokenFlowMappingStart},
		{kind: tokenStreamEnd},
	}
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error from scalar mapping entry with unterminated flow value")
	}
}

func TestParseBlockMappingBreakOnUnexpectedToken(t *testing.T) {
	tokens := []token{
		{kind: tokenStreamStart},
		{kind: tokenDocumentStart},
		{kind: tokenBlockMappingStart},
		{kind: tokenBlockSequenceStart},
		{kind: tokenDocumentEnd},
		{kind: tokenStreamEnd},
	}
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error from unexpected token after root node")
	}
}

func TestParseFlowSequenceKeyError(t *testing.T) {
	tokens := []token{
		{kind: tokenStreamStart},
		{kind: tokenFlowSequenceStart},
		{kind: tokenKey},
		{kind: tokenFlowMappingStart},
		{kind: tokenStreamEnd},
	}
	p := newParser(tokens)
	_, err := p.parse()
	if err == nil {
		t.Fatal("expected error from flow sequence key with unterminated flow mapping")
	}
}

func TestParseFlowSequenceKeyOnlyTokens(t *testing.T) {
	tokens := []token{
		{kind: tokenStreamStart},
		{kind: tokenFlowSequenceStart},
		{kind: tokenKey},
		{kind: tokenScalar, value: "a"},
		{kind: tokenFlowEntry},
		{kind: tokenScalar, value: "b"},
		{kind: tokenFlowSequenceEnd},
		{kind: tokenStreamEnd},
	}
	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("expected at least one document")
	}
}

func TestUnknownDirectiveWarning(t *testing.T) {
	input := "%CUSTOM foo bar\n---\nkey: val"
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(file.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(file.Warnings))
	}
	if !strings.Contains(file.Warnings[0], "%CUSTOM") {
		t.Errorf("expected warning about %%CUSTOM, got: %s", file.Warnings[0])
	}
}

func TestParseImplicitDocumentStart(t *testing.T) {
	input := "key: val\n"
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(file.Docs))
	}
}

func TestParseMultiDocRequiresMarker(t *testing.T) {
	input := "a: 1\n---\nb: 2\n"
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(file.Docs))
	}
}

func TestParseAllTokenTypesInDocument(t *testing.T) {
	input := "anchor: &a val\nalias: *a\ntag: !custom tagged\nmap:\n  key: value\nseq:\n  - item\n"
	file, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(file.Docs))
	}
}

func TestDecodeTagURIPercentEncoding(t *testing.T) {
	result := decodeTagURI("tag%3Aexample")
	if result != "tag:example" {
		t.Errorf("expected 'tag:example', got %q", result)
	}
}

func TestDecodeTagURINoEncoding(t *testing.T) {
	result := decodeTagURI("plain")
	if result != "plain" {
		t.Errorf("expected 'plain', got %q", result)
	}
}

func TestDecodeTagURIMultiplePercent(t *testing.T) {
	result := decodeTagURI("%41%42%43")
	if result != "ABC" {
		t.Errorf("expected 'ABC', got %q", result)
	}
}

func TestDecodeTagURIInvalidPercent(t *testing.T) {
	result := decodeTagURI("%ZZ")
	if result != "%ZZ" {
		t.Errorf("invalid hex should pass through, got %q", result)
	}
}

func TestDecodeTagURITruncatedPercent(t *testing.T) {
	result := decodeTagURI("end%4")
	if !strings.Contains(result, "%4") {
		t.Errorf("truncated %% should pass through, got %q", result)
	}
}

func TestIsValidYAMLVersion(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"1.2", true},
		{"1.1", true},
		{"0.9", true},
		{"1.", false},
		{".1", false},
		{"", false},
		{"12", false},
		{"a.b", false},
	}
	for _, tt := range tests {
		got := isValidYAMLVersion(tt.input)
		if got != tt.want {
			t.Errorf("isValidYAMLVersion(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
