package yaml

import (
	"bytes"
	"strings"
	"testing"
)

func TestScannerBasicMapping(t *testing.T) {
	tokens, err := newScanner([]byte("key: value")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasKey := false
	hasValue := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "key" {
			hasKey = true
		}
		if tok.kind == tokenScalar && tok.value == "value" {
			hasValue = true
		}
	}
	if !hasKey || !hasValue {
		t.Errorf("expected key and value tokens, got %v", tokens)
	}
}

func TestScannerBasicSequence(t *testing.T) {
	tokens, err := newScanner([]byte("- a\n- b\n- c")).scan()
	if err != nil {
		t.Fatal(err)
	}
	entries := 0
	for _, tok := range tokens {
		if tok.kind == tokenBlockEntry {
			entries++
		}
	}
	if entries != 3 {
		t.Errorf("expected 3 block entries, got %d", entries)
	}
}

func TestScannerFlowSequence(t *testing.T) {
	tokens, err := newScanner([]byte("[a, b, c]")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasStart := false
	hasEnd := false
	for _, tok := range tokens {
		if tok.kind == tokenFlowSequenceStart {
			hasStart = true
		}
		if tok.kind == tokenFlowSequenceEnd {
			hasEnd = true
		}
	}
	if !hasStart || !hasEnd {
		t.Error("expected flow sequence start and end tokens")
	}
}

func TestScannerFlowMapping(t *testing.T) {
	tokens, err := newScanner([]byte("{a: 1, b: 2}")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasStart := false
	hasEnd := false
	for _, tok := range tokens {
		if tok.kind == tokenFlowMappingStart {
			hasStart = true
		}
		if tok.kind == tokenFlowMappingEnd {
			hasEnd = true
		}
	}
	if !hasStart || !hasEnd {
		t.Error("expected flow mapping start and end tokens")
	}
}

func TestScannerStreamTokens(t *testing.T) {
	tokens, err := newScanner([]byte("a: 1")).scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) < 2 {
		t.Fatal("expected at least stream start and end")
	}
	if tokens[0].kind != tokenStreamStart {
		t.Errorf("first token should be STREAM-START, got %s", tokens[0].String())
	}
	if tokens[len(tokens)-1].kind != tokenStreamEnd {
		t.Errorf("last token should be STREAM-END, got %s", tokens[len(tokens)-1].String())
	}
}

func TestScannerDocumentMarkers(t *testing.T) {
	tokens, err := newScanner([]byte("---\na: 1\n...\n")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasDocStart := false
	hasDocEnd := false
	for _, tok := range tokens {
		if tok.kind == tokenDocumentStart {
			hasDocStart = true
		}
		if tok.kind == tokenDocumentEnd {
			hasDocEnd = true
		}
	}
	if !hasDocStart {
		t.Error("expected document start token")
	}
	if !hasDocEnd {
		t.Error("expected document end token")
	}
}

func TestScannerComment(t *testing.T) {
	tokens, err := newScanner([]byte("# comment\nkey: value")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasComment := false
	for _, tok := range tokens {
		if tok.kind == tokenComment {
			hasComment = true
		}
	}
	if !hasComment {
		t.Error("expected comment token")
	}
}

func TestScannerAnchorAlias(t *testing.T) {
	tokens, err := newScanner([]byte("a: &anc value\nb: *anc")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasAnchor := false
	hasAlias := false
	for _, tok := range tokens {
		if tok.kind == tokenAnchor {
			hasAnchor = true
		}
		if tok.kind == tokenAlias {
			hasAlias = true
		}
	}
	if !hasAnchor || !hasAlias {
		t.Error("expected anchor and alias tokens")
	}
}

func TestScannerTag(t *testing.T) {
	tokens, err := newScanner([]byte("a: !!str 123")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasTag := false
	for _, tok := range tokens {
		if tok.kind == tokenTag && strings.Contains(tok.value, "str") {
			hasTag = true
		}
	}
	if !hasTag {
		t.Error("expected tag token")
	}
}

func TestScannerDoubleQuoted(t *testing.T) {
	tokens, err := newScanner([]byte(`key: "hello world"`)).scan()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "hello world" && tok.style == scalarDoubleQuoted {
			found = true
		}
	}
	if !found {
		t.Error("expected double-quoted scalar")
	}
}

func TestScannerSingleQuoted(t *testing.T) {
	tokens, err := newScanner([]byte(`key: 'hello'`)).scan()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "hello" && tok.style == scalarSingleQuoted {
			found = true
		}
	}
	if !found {
		t.Error("expected single-quoted scalar")
	}
}

func TestScannerLiteralBlock(t *testing.T) {
	tokens, err := newScanner([]byte("text: |\n  hello\n  world\n")).scan()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.style == scalarLiteral {
			found = true
		}
	}
	if !found {
		t.Error("expected literal block scalar")
	}
}

func TestScannerFoldedBlock(t *testing.T) {
	tokens, err := newScanner([]byte("text: >\n  hello\n  world\n")).scan()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.style == scalarFolded {
			found = true
		}
	}
	if !found {
		t.Error("expected folded block scalar")
	}
}

func TestScannerNonPrintableCharacter(t *testing.T) {
	_, err := newScanner([]byte("key: val\x01ue")).scan()
	if err == nil {
		t.Fatal("expected error for non-printable character")
	}
	if !strings.Contains(err.Error(), "non-printable") {
		t.Errorf("expected non-printable error, got: %v", err)
	}
}

func TestScannerAllowTab(t *testing.T) {
	_, err := newScanner([]byte("key: hello\tworld")).scan()
	if err != nil {
		t.Fatalf("tab should be allowed: %v", err)
	}
}

func TestScannerEmpty(t *testing.T) {
	tokens, err := newScanner([]byte("")).scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens (stream start/end), got %d", len(tokens))
	}
}

func TestScannerMultiDoc(t *testing.T) {
	tokens, err := newScanner([]byte("---\na: 1\n---\nb: 2\n")).scan()
	if err != nil {
		t.Fatal(err)
	}
	docStarts := 0
	for _, tok := range tokens {
		if tok.kind == tokenDocumentStart {
			docStarts++
		}
	}
	if docStarts != 2 {
		t.Errorf("expected 2 document starts, got %d", docStarts)
	}
}

func TestScannerNestedMapping(t *testing.T) {
	input := "a:\n  b:\n    c: deep"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	mappingStarts := 0
	for _, tok := range tokens {
		if tok.kind == tokenBlockMappingStart {
			mappingStarts++
		}
	}
	if mappingStarts < 3 {
		t.Errorf("expected at least 3 mapping starts, got %d", mappingStarts)
	}
}

func TestScannerPositionTracking(t *testing.T) {
	tokens, err := newScanner([]byte("a: 1\nb: 2")).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "b" {
			if tok.pos.Line != 2 {
				t.Errorf("expected 'b' on line 2, got line %d", tok.pos.Line)
			}
			break
		}
	}
}

func TestScannerBOM(t *testing.T) {
	tokens, err := newScanner([]byte("\xEF\xBB\xBFkey: value")).scan()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "key" {
			found = true
		}
	}
	if !found {
		t.Error("expected scanner to handle UTF-8 BOM")
	}
}

func TestScannerDirective(t *testing.T) {
	tokens, err := newScanner([]byte("%YAML 1.2\n---\na: 1")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasDirective := false
	for _, tok := range tokens {
		if tok.kind == tokenDirective {
			hasDirective = true
		}
	}
	if !hasDirective {
		t.Error("expected directive token")
	}
}

func TestScannerDoubleQuotedEscapes(t *testing.T) {
	tokens, err := newScanner([]byte(`key: "hello\nworld"`)).scan()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && strings.Contains(tok.value, "\n") {
			found = true
		}
	}
	if !found {
		t.Error("expected escaped newline in double-quoted scalar")
	}
}

func TestScannerSingleQuotedEscapedQuote(t *testing.T) {
	tokens, err := newScanner([]byte(`key: 'it''s'`)).scan()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "it's" {
			found = true
		}
	}
	if !found {
		t.Error("expected escaped single quote in value")
	}
}

func TestScannerMergeKey(t *testing.T) {
	tokens, err := newScanner([]byte("<<: *ref")).scan()
	if err != nil {
		t.Fatal(err)
	}
	foundKey := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "<<" {
			foundKey = true
		}
	}
	if !foundKey {
		t.Error("expected merge key '<<'")
	}
}

func TestNormalizeLineBreaksNEL(t *testing.T) {
	input := []byte("a\xc2\x85b")
	out := normalizeLineBreaks(input)
	if string(out) != "a\nb" {
		t.Errorf("expected NEL normalized to LF, got %q", out)
	}
}

func TestNormalizeLineBreaksLS(t *testing.T) {
	input := []byte("a\xe2\x80\xa8b")
	out := normalizeLineBreaks(input)
	if string(out) != "a\nb" {
		t.Errorf("expected LS normalized to LF, got %q", out)
	}
}

func TestNormalizeLineBreaksPS(t *testing.T) {
	input := []byte("a\xe2\x80\xa9b")
	out := normalizeLineBreaks(input)
	if string(out) != "a\nb" {
		t.Errorf("expected PS normalized to LF, got %q", out)
	}
}

func TestNormalizeLineBreaksNoChange(t *testing.T) {
	input := []byte("hello\nworld")
	out := normalizeLineBreaks(input)
	if &out[0] != &input[0] {
		t.Error("expected same slice when no normalization needed")
	}
}

func TestNormalizeLineBreaksEmpty(t *testing.T) {
	out := normalizeLineBreaks(nil)
	if len(out) != 0 {
		t.Errorf("expected empty, got %q", out)
	}
}

func TestValid(t *testing.T) {
	if !Valid([]byte("key: value")) {
		t.Error("expected valid YAML")
	}
	if !Valid([]byte("- a\n- b")) {
		t.Error("expected valid YAML list")
	}
}

func TestValidInvalidYAML(t *testing.T) {
	if Valid([]byte(`[invalid`)) {
		t.Error("expected invalid")
	}
	if Valid([]byte(`"`)) {
		t.Error("expected invalid for unterminated quote")
	}
}

func TestScannerUnterminatedString(t *testing.T) {
	_, err := newScanner([]byte(`key: "unterminated`)).scan()
	if err == nil {
		t.Error("expected error for unterminated double quote")
	}
}

func TestScannerUnterminatedSingleQuote(t *testing.T) {
	_, err := newScanner([]byte(`key: 'unterminated`)).scan()
	if err == nil {
		t.Error("expected error for unterminated single quote")
	}
}

func TestScannerFlowNested(t *testing.T) {
	tokens, err := newScanner([]byte(`{a: [1, 2], b: {c: 3}}`)).scan()
	if err != nil {
		t.Fatal(err)
	}
	flowStarts := 0
	for _, tok := range tokens {
		if tok.kind == tokenFlowMappingStart || tok.kind == tokenFlowSequenceStart {
			flowStarts++
		}
	}
	if flowStarts < 3 {
		t.Errorf("expected at least 3 flow starts, got %d", flowStarts)
	}
}

func TestScannerBlockScalarStrip(t *testing.T) {
	tokens, err := newScanner([]byte("text: |-\n  hello\n")).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.style == scalarLiteral {
			if strings.HasSuffix(tok.value, "\n") {
				t.Errorf("strip chomping should remove trailing newline, got %q", tok.value)
			}
			return
		}
	}
	t.Error("expected literal scalar")
}

func TestScannerBlockScalarKeep(t *testing.T) {
	tokens, err := newScanner([]byte("text: |+\n  hello\n\n")).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.style == scalarLiteral {
			if !strings.HasSuffix(tok.value, "\n") {
				t.Errorf("keep chomping should preserve trailing newline, got %q", tok.value)
			}
			return
		}
	}
	t.Error("expected literal scalar")
}

func TestScannerExplicitKey(t *testing.T) {
	tokens, err := newScanner([]byte("? key\n: value")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasKey := false
	for _, tok := range tokens {
		if tok.kind == tokenKey {
			hasKey = true
		}
	}
	if !hasKey {
		t.Error("expected explicit key token")
	}
}

func TestScanNonPrintableCharacter(t *testing.T) {
	_, err := newScanner([]byte{0x01}).scan()
	if err == nil {
		t.Fatal("expected error for non-printable character")
	}
}

func TestScanFlowAnchor(t *testing.T) {
	tokens, err := newScanner([]byte("{&anc key: val}")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasAnchor := false
	for _, tok := range tokens {
		if tok.kind == tokenAnchor {
			hasAnchor = true
		}
	}
	if !hasAnchor {
		t.Error("expected anchor token in flow context")
	}
}

func TestScanFlowAlias(t *testing.T) {
	tokens, err := newScanner([]byte("[&a val, *a]")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasAlias := false
	for _, tok := range tokens {
		if tok.kind == tokenAlias {
			hasAlias = true
		}
	}
	if !hasAlias {
		t.Error("expected alias token in flow context")
	}
}

func TestScanFlowTag(t *testing.T) {
	tokens, err := newScanner([]byte("[!mytag val]")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasTag := false
	for _, tok := range tokens {
		if tok.kind == tokenTag {
			hasTag = true
		}
	}
	if !hasTag {
		t.Error("expected tag token in flow context")
	}
}

func TestScanFlowComment(t *testing.T) {
	tokens, err := newScanner([]byte("[val] # comment")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasComment := false
	for _, tok := range tokens {
		if tok.kind == tokenComment {
			hasComment = true
		}
	}
	if !hasComment {
		t.Error("expected comment token after flow sequence")
	}
}

func TestScanFlowNewline(t *testing.T) {
	tokens, err := newScanner([]byte("[\n  a,\n  b\n]")).scan()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, tok := range tokens {
		if tok.kind == tokenScalar {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 scalars, got %d", count)
	}
}

func TestScanFlowPlainScalar(t *testing.T) {
	tokens, err := newScanner([]byte("{a: b}")).scan()
	if err != nil {
		t.Fatal(err)
	}
	var vals []string
	for _, tok := range tokens {
		if tok.kind == tokenScalar {
			vals = append(vals, tok.value)
		}
	}
	if len(vals) != 2 || vals[0] != "a" || vals[1] != "b" {
		t.Errorf("expected [a, b], got %v", vals)
	}
}

func TestScanBlockScalarCommentAfterIndicator(t *testing.T) {
	input := "text: | # this is a comment\n  hello\n"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && strings.Contains(tok.value, "hello") {
			return
		}
	}
	t.Error("expected scalar with hello")
}

func TestScanBlockScalarExplicitIndent(t *testing.T) {
	input := "text: |2\n  hello\n"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && strings.Contains(tok.value, "hello") {
			return
		}
	}
	t.Error("expected scalar with hello")
}

func TestScanBlockScalarExtraIndent(t *testing.T) {
	input := "text: |\n  line1\n    extra\n"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && strings.Contains(tok.value, "  extra") {
			return
		}
	}
	t.Error("expected scalar with extra-indented content")
}

func TestScanBlockScalarKeepChomp(t *testing.T) {
	input := "text: |+\n  hello\n\n\n"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && strings.HasSuffix(tok.value, "\n\n") {
			return
		}
	}
	t.Error("expected scalar with trailing newlines preserved")
}

func TestScanBlockScalarKeepChompEmptyContent(t *testing.T) {
	input := "text: |+\n\n\n"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && len(tok.value) > 0 {
			return
		}
	}
	t.Error("expected non-empty scalar from keep-chomp with trailing newlines")
}

func TestScanBlockScalarFolded(t *testing.T) {
	input := "text: >\n  line1\n  line2\n"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && strings.Contains(tok.value, "line1 line2") {
			return
		}
	}
	t.Error("expected folded scalar joining lines")
}

func TestScanBlockScalarFoldedEmptyLines(t *testing.T) {
	input := "text: >\n  para1\n\n  para2\n"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && strings.Contains(tok.value, "para1\npara2") {
			return
		}
	}
	t.Error("expected folded scalar with paragraph break")
}

func TestScanSingleQuotedNewline(t *testing.T) {
	input := "'line1\n  line2'"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && strings.Contains(tok.value, "line1 line2") {
			return
		}
	}
	t.Error("expected single-quoted scalar with newline folded to space")
}

func TestScanDoubleQuotedEscapes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"\0"`, "\x00"},
		{`"\a"`, "\a"},
		{`"\b"`, "\b"},
		{`"\t"`, "\t"},
		{`"\n"`, "\n"},
		{`"\v"`, "\v"},
		{`"\f"`, "\f"},
		{`"\r"`, "\r"},
		{`"\e"`, "\x1b"},
		{`"\ "`, " "},
		{`"\""`, `"`},
		{`"\/"`, "/"},
		{`"\\"`, `\`},
		{`"\N"`, "\xc2\x85"},
		{`"\_"`, "\xc2\xa0"},
		{`"\L"`, "\xe2\x80\xa8"},
		{`"\P"`, "\xe2\x80\xa9"},
		{`"\x41"`, "A"},
		{"\"\\u0041\"", "A"},
		{`"A"`, "A"},
		{`"\U00000041"`, "A"},
	}
	for _, tt := range tests {
		tokens, err := newScanner([]byte(tt.input)).scan()
		if err != nil {
			t.Errorf("input %s: %v", tt.input, err)
			continue
		}
		found := false
		for _, tok := range tokens {
			if tok.kind == tokenScalar && tok.value == tt.want {
				found = true
			}
		}
		if !found {
			t.Errorf("input %s: expected %q in tokens", tt.input, tt.want)
		}
	}
}

func TestScanDoubleQuotedNewlineContinuation(t *testing.T) {
	input := "\"line1\n  line2\""
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && strings.Contains(tok.value, "line1 line2") {
			return
		}
	}
	t.Error("expected double-quoted scalar with newline folded to space")
}

func TestScanDoubleQuotedEscapedNewline(t *testing.T) {
	input := "\"line1\\\n  line2\""
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "line1line2" {
			return
		}
	}
	t.Error("expected escaped newline to join without space")
}

func TestScanDoubleQuotedEscapedCR(t *testing.T) {
	input := "\"line1\\\r\nline2\""
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "line1line2" {
			return
		}
	}
	t.Error("expected escaped CR+LF to join without space")
}

func TestScanDoubleQuotedUnknownEscape(t *testing.T) {
	input := "\"\\z\""
	_, err := newScanner([]byte(input)).scan()
	if err == nil {
		t.Error("expected error for invalid escape sequence \\z")
	}
}

func TestScanHexEscapeError(t *testing.T) {
	_, err := newScanner([]byte("\"\\xZZ\"")).scan()
	if err == nil {
		t.Fatal("expected error for invalid hex escape")
	}
}

func TestScanHexEscapeUnexpectedEnd(t *testing.T) {
	_, err := newScanner([]byte("\"\\x4")).scan()
	if err == nil {
		t.Fatal("expected error for truncated hex escape")
	}
}

func TestScanPlainScalarDocumentStartBreak(t *testing.T) {
	input := "val\n---\nnew: doc"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasDocStart := false
	for _, tok := range tokens {
		if tok.kind == tokenDocumentStart {
			hasDocStart = true
		}
	}
	if !hasDocStart {
		t.Error("expected document start to break plain scalar")
	}
}

func TestScanPlainScalarDocumentEndBreak(t *testing.T) {
	input := "val\n...\n"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasDocEnd := false
	for _, tok := range tokens {
		if tok.kind == tokenDocumentEnd {
			hasDocEnd = true
		}
	}
	if !hasDocEnd {
		t.Error("expected document end to break plain scalar")
	}
}

func TestScanPlainScalarBlockEntryBreak(t *testing.T) {
	input := "val\n- item"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasEntry := false
	for _, tok := range tokens {
		if tok.kind == tokenBlockEntry {
			hasEntry = true
		}
	}
	if !hasEntry {
		t.Error("expected block entry to break plain scalar")
	}
}

func TestScanPlainScalarColonBreak(t *testing.T) {
	input := "multiline\nkey: val"
	_, err := newScanner([]byte(input)).scan()
	if err == nil {
		t.Error("expected error for multiline implicit key")
	}
}

func TestScanPlainScalarCommentBreak(t *testing.T) {
	input := "multi\n#comment\nkey: val"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasComment := false
	for _, tok := range tokens {
		if tok.kind == tokenComment {
			hasComment = true
		}
	}
	if !hasComment {
		t.Error("expected comment to break multiline plain scalar")
	}
}

func TestScanPlainScalarExplicitKeyBreak(t *testing.T) {
	input := "multi\n? key"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasKey := false
	for _, tok := range tokens {
		if tok.kind == tokenKey {
			hasKey = true
		}
	}
	if !hasKey {
		t.Error("expected explicit key to break multiline plain scalar")
	}
}

func TestScanFlowEndNegativeDepth(t *testing.T) {
	_, err := newScanner([]byte("]")).scan()
	if err == nil {
		t.Error("expected error for stray ] in block context")
	}
}

func TestScanEmitScalarMappingDetection(t *testing.T) {
	input := "a : 1\nb : 2\n"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	keyCount := 0
	for _, tok := range tokens {
		if tok.kind == tokenKey {
			keyCount++
		}
	}
	if keyCount < 2 {
		t.Errorf("expected 2 key tokens, got %d", keyCount)
	}
}

func TestScanAdvanceNewline(t *testing.T) {
	s := newScanner([]byte("a\nb"))
	s.advance(1)
	if s.col != 2 {
		t.Errorf("after 'a', col should be 2, got %d", s.col)
	}
	s.advance(1)
	if s.line != 2 || s.col != 1 {
		t.Errorf("after newline, expected line=2 col=1, got line=%d col=%d", s.line, s.col)
	}
}

func TestScanPeekAtEnd(t *testing.T) {
	s := newScanner([]byte(""))
	if s.peek() != 0 {
		t.Errorf("peek at end should return 0, got %d", s.peek())
	}
}

func TestValidBadEncoding(t *testing.T) {
	// Non-printable - should fail validation
	if Valid([]byte{0x01}) {
		t.Error("expected Valid to return false for non-printable")
	}
}

func TestScanFlowColonNotBlank(t *testing.T) {
	// In flow context, colon followed by non-blank should scan as plain scalar
	input := "[a:b]"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "a:b" {
			return
		}
	}
	t.Error("expected 'a:b' as plain scalar in flow")
}

func TestScanDoubleQuotedTabEscape(t *testing.T) {
	input := "\"\\	\""
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "\t" {
			return
		}
	}
	t.Error("expected tab from \\<tab> escape")
}

func TestScanFlowSingleQuoted(t *testing.T) {
	tokens, err := newScanner([]byte("[  'hello' ]")).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "hello" && tok.style == scalarSingleQuoted {
			return
		}
	}
	t.Error("expected single-quoted scalar in flow sequence")
}

func TestScanFlowDoubleQuoted(t *testing.T) {
	tokens, err := newScanner([]byte(`[ "hello" ]`)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "hello" && tok.style == scalarDoubleQuoted {
			return
		}
	}
	t.Error("expected double-quoted scalar in flow sequence")
}

func TestScanDoubleQuotedUnicodeEscapes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"A"`, "A"},
		{`"é"`, "é"},
		{`"\U0001F600"`, "\U0001F600"},
	}
	for _, tt := range tests {
		tokens, err := newScanner([]byte(tt.input)).scan()
		if err != nil {
			t.Errorf("input %s: %v", tt.input, err)
			continue
		}
		found := false
		for _, tok := range tokens {
			if tok.kind == tokenScalar && tok.value == tt.want {
				found = true
			}
		}
		if !found {
			t.Errorf("input %s: expected %q in tokens", tt.input, tt.want)
		}
	}
}

func TestScanDoubleQuotedUnicodeEscapeError(t *testing.T) {
	_, err := newScanner([]byte(`"\uZZZZ"`)).scan()
	if err == nil {
		t.Fatal("expected error for invalid \\u hex escape")
	}
}

func TestScanDoubleQuotedBigUnicodeEscapeError(t *testing.T) {
	_, err := newScanner([]byte(`"\UZZZZZZZZ"`)).scan()
	if err == nil {
		t.Fatal("expected error for invalid \\U hex escape")
	}
}

func TestScanHexDigitsAF(t *testing.T) {
	tokens, err := newScanner([]byte(`"\xaB"`)).scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.kind == tokenScalar {
			// 0xaB = 171, encoded as UTF-8 rune
			expected := string(rune(0xAB))
			if tok.value == expected {
				return
			}
		}
	}
	t.Error("expected hex escape with a-f/A-F digits")
}

func TestScanBlockScalarNonSpecialAfterIndicator(t *testing.T) {
	input := "|x\n  hello\n"
	_, err := newScanner([]byte(input)).scan()
	_ = err
}

func TestScanDoubleQuotedEndOfEscape(t *testing.T) {
	_, err := newScanner([]byte(`"\`)).scan()
	if err == nil {
		t.Fatal("expected error for end of input in escape")
	}
}

func TestScanPlainScalarColonContinuation(t *testing.T) {
	input := "multi\n  line: val"
	_, err := newScanner([]byte(input)).scan()
	if err == nil {
		t.Error("expected error for multiline implicit key")
	}
}

func TestScanEmitScalarInFlow(t *testing.T) {
	tokens, err := newScanner([]byte("{a : 1}")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasScalar := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "a" {
			hasScalar = true
		}
	}
	if !hasScalar {
		t.Error("expected scalar 'a' in flow mapping")
	}
}

func TestSkipSpacesAndCommentsWithComment(t *testing.T) {
	input := "---\n# comment\nkey: val"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasComment := false
	for _, tok := range tokens {
		if tok.kind == tokenComment {
			hasComment = true
		}
	}
	if !hasComment {
		t.Error("expected comment token")
	}
}

func TestValidBadScan(t *testing.T) {
	if Valid([]byte(`"unterminated`)) {
		t.Error("expected Valid to return false for unterminated string")
	}
}

func TestScanDoubleQuotedSpecialEscapes(t *testing.T) {
	tests := []struct {
		input string
		want  []byte
	}{
		{`"\N"`, []byte{0xc2, 0x85}},
		{`"\_"`, []byte{0xc2, 0xa0}},
		{`"\L"`, []byte{0xe2, 0x80, 0xa8}},
		{`"\P"`, []byte{0xe2, 0x80, 0xa9}},
	}
	for _, tt := range tests {
		tokens, err := newScanner([]byte(tt.input)).scan()
		if err != nil {
			t.Errorf("input %s: %v", tt.input, err)
			continue
		}
		found := false
		for _, tok := range tokens {
			if tok.kind == tokenScalar && tok.value == string(tt.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("input %s: expected %q in tokens", tt.input, tt.want)
		}
	}
}

func TestScanDoubleQuotedCREscape(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte('"')
	buf.WriteString("hello")
	buf.WriteByte('\\')
	buf.WriteByte('\r')
	buf.WriteByte('\n')
	buf.WriteString("  world")
	buf.WriteByte('"')

	tokens, err := newScanner(buf.Bytes()).scan()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "helloworld" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected escaped CR continuation to produce 'helloworld'")
	}
}

func TestScanPlainScalarContinuationColonBreak(t *testing.T) {
	input := "a:\n  multi\n    : next\n"
	tokens, err := newScanner([]byte(input)).scan()
	if err != nil {
		t.Fatal(err)
	}
	foundMulti := false
	for _, tok := range tokens {
		if tok.kind == tokenScalar && tok.value == "multi" {
			foundMulti = true
		}
	}
	if !foundMulti {
		t.Fatal("expected plain scalar 'multi' to break at continuation colon")
	}
}

func TestScanNextAtEndAfterSpaces(t *testing.T) {
	tokens, err := newScanner([]byte("   ")).scan()
	if err != nil {
		t.Fatal(err)
	}
	hasEnd := false
	for _, tok := range tokens {
		if tok.kind == tokenStreamEnd {
			hasEnd = true
		}
	}
	if !hasEnd {
		t.Fatal("expected stream end token for whitespace-only input")
	}
}
