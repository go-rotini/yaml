package yaml

import "testing"

func TestTokenString(t *testing.T) {
	tests := []struct {
		tok token
		exp string
	}{
		{token{kind: tokenStreamStart}, "STREAM-START"},
		{token{kind: tokenStreamEnd}, "STREAM-END"},
		{token{kind: tokenDocumentStart}, "DOCUMENT-START"},
		{token{kind: tokenDocumentEnd}, "DOCUMENT-END"},
		{token{kind: tokenBlockMappingStart}, "BLOCK-MAPPING-START"},
		{token{kind: tokenBlockSequenceStart}, "BLOCK-SEQUENCE-START"},
		{token{kind: tokenBlockEnd}, "BLOCK-END"},
		{token{kind: tokenFlowMappingStart}, "FLOW-MAPPING-START"},
		{token{kind: tokenFlowMappingEnd}, "FLOW-MAPPING-END"},
		{token{kind: tokenFlowSequenceStart}, "FLOW-SEQUENCE-START"},
		{token{kind: tokenFlowSequenceEnd}, "FLOW-SEQUENCE-END"},
		{token{kind: tokenKey}, "KEY"},
		{token{kind: tokenValue}, "VALUE"},
		{token{kind: tokenBlockEntry}, "BLOCK-ENTRY"},
		{token{kind: tokenFlowEntry}, "FLOW-ENTRY"},
		{token{kind: tokenAnchor, value: "name"}, "ANCHOR(name)"},
		{token{kind: tokenAlias, value: "ref"}, "ALIAS(ref)"},
		{token{kind: tokenTag, value: "!!str"}, "TAG(!!str)"},
		{token{kind: tokenScalar, value: "hello"}, "SCALAR(hello)"},
		{token{kind: tokenComment, value: "note"}, "COMMENT(note)"},
		{token{kind: tokenDirective, value: "%YAML"}, "DIRECTIVE(%YAML)"},
		{token{kind: tokenError}, "UNKNOWN"},
	}
	for _, tt := range tests {
		got := tt.tok.String()
		if got != tt.exp {
			t.Errorf("token %d: expected %q, got %q", tt.tok.kind, tt.exp, got)
		}
	}
}

func TestTokenKindConstants(t *testing.T) {
	kinds := []tokenKind{
		tokenError, tokenStreamStart, tokenStreamEnd, tokenDocumentStart,
		tokenDocumentEnd, tokenDirective, tokenBlockMappingStart,
		tokenBlockSequenceStart, tokenBlockEnd, tokenFlowMappingStart,
		tokenFlowMappingEnd, tokenFlowSequenceStart, tokenFlowSequenceEnd,
		tokenKey, tokenValue, tokenBlockEntry, tokenFlowEntry,
		tokenAnchor, tokenAlias, tokenTag, tokenScalar, tokenComment,
	}
	seen := make(map[tokenKind]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("duplicate token kind value: %d", k)
		}
		seen[k] = true
	}
}

func TestScalarStyleConstants(t *testing.T) {
	styles := []scalarStyle{
		scalarPlain, scalarSingleQuoted, scalarDoubleQuoted,
		scalarLiteral, scalarFolded,
	}
	seen := make(map[scalarStyle]bool)
	for _, s := range styles {
		if seen[s] {
			t.Errorf("duplicate scalar style value: %d", s)
		}
		seen[s] = true
	}
}

func TestTokenPositionField(t *testing.T) {
	tok := token{
		kind:  tokenScalar,
		value: "test",
		pos:   Position{Line: 5, Column: 3, Offset: 42},
		style: scalarDoubleQuoted,
	}
	if tok.pos.Line != 5 {
		t.Errorf("expected line 5, got %d", tok.pos.Line)
	}
	if tok.pos.Column != 3 {
		t.Errorf("expected column 3, got %d", tok.pos.Column)
	}
	if tok.pos.Offset != 42 {
		t.Errorf("expected offset 42, got %d", tok.pos.Offset)
	}
	if tok.style != scalarDoubleQuoted {
		t.Errorf("expected double-quoted style, got %d", tok.style)
	}
}

func TestTokenStringUnknownKind(t *testing.T) {
	tok := token{kind: tokenKind(999)}
	if got := tok.String(); got != "UNKNOWN" {
		t.Errorf("expected UNKNOWN for invalid kind, got %q", got)
	}
}
