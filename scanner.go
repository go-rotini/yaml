package yaml

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

type scanner struct {
	src            []byte
	pos            int
	line           int
	col            int
	tokens         []token
	indent         int
	indents        []int
	flow           int
	docStarted        bool
	scanErr           error
	lastContentLine   int
	docStartLine      int
	docStartMapIndent int
	flowBlockIndent   int
}

func newScanner(src []byte) *scanner {
	src = normalizeLineBreaks(src)
	return &scanner{
		src:               src,
		line:              1,
		col:               1,
		indent:            -1,
		indents:           nil,
		docStartMapIndent: -1,
		flowBlockIndent:   -1,
	}
}

func normalizeLineBreaks(src []byte) []byte {
	if !bytes.Contains(src, []byte{0xC2, 0x85}) &&
		!bytes.Contains(src, []byte{0xE2, 0x80, 0xA8}) &&
		!bytes.Contains(src, []byte{0xE2, 0x80, 0xA9}) {
		return src
	}
	var buf []byte
	for i := 0; i < len(src); {
		if i+1 < len(src) && src[i] == 0xC2 && src[i+1] == 0x85 {
			buf = append(buf, '\n')
			i += 2
			continue
		}
		if i+2 < len(src) && src[i] == 0xE2 && src[i+1] == 0x80 && (src[i+2] == 0xA8 || src[i+2] == 0xA9) {
			buf = append(buf, '\n')
			i += 3
			continue
		}
		buf = append(buf, src[i])
		i++
	}
	return buf
}

func (s *scanner) scan() ([]token, error) {
	s.emit(tokenStreamStart, "", s.position())
	if err := s.skipBOM(); err != nil {
		return nil, err
	}

	for !s.atEnd() {
		prevPos := s.pos
		if err := s.scanNext(); err != nil {
			return nil, err
		}
		if s.scanErr != nil {
			return nil, s.scanErr
		}
		if s.pos == prevPos && !s.atEnd() {
			return nil, &SyntaxError{
				Message: fmt.Sprintf("scanner stuck at byte %d (char %q)", s.pos, s.src[s.pos]),
				Pos:     s.position(),
			}
		}
	}

	s.unwindIndents(-1)
	s.emit(tokenStreamEnd, "", s.position())
	return s.tokens, nil
}

func (s *scanner) scanNext() error {
	s.skipSpaces()

	if s.atEnd() {
		return nil
	}

	ch := s.peek()

	if ch < 0x20 && ch != '\t' && ch != '\n' && ch != '\r' {
		return &SyntaxError{
			Message: "non-printable character in input",
			Pos:     s.position(),
		}
	}

	if ch == '#' {
		if s.pos == 0 || s.col == 1 || isBlank(s.src[s.pos-1]) {
			s.scanComment()
			return nil
		}
		return &SyntaxError{Message: "comment must be preceded by whitespace", Pos: s.position()}
	}

	if s.col == 1 && ch == '%' {
		if s.docStarted {
			return &SyntaxError{Message: "directive not allowed inside a document", Pos: s.position()}
		}
		return s.scanDirective()
	}

	if s.flow > 0 && s.col == 1 && ((s.startsWith("---") && s.isBlankOrEnd(3)) || (s.startsWith("...") && s.isBlankOrEnd(3))) {
		return &SyntaxError{Message: "invalid document marker inside flow collection", Pos: s.position()}
	}

	if s.flow == 0 && s.col == 1 && s.startsWith("---") && s.isBlankOrEnd(3) {
		s.unwindIndents(-1)
		s.emit(tokenDocumentStart, "", s.position())
		s.docStartLine = s.line
		s.advance(3)
		s.docStarted = true
		s.skipSpacesAndComments()
		return nil
	}

	if s.flow == 0 && s.col == 1 && s.startsWith("...") && s.isBlankOrEnd(3) {
		s.unwindIndents(-1)
		s.emit(tokenDocumentEnd, "", s.position())
		s.advance(3)
		s.docStarted = false
		s.skipSpaces()
		if !s.atEnd() && s.peek() != '\n' && s.peek() != '\r' && s.peek() != '#' {
			return &SyntaxError{Message: "unexpected content after document end marker", Pos: s.position()}
		}
		s.skipSpacesAndComments()
		return nil
	}

	if ch == '\n' || ch == '\r' {
		s.advanceLine()
		return nil
	}

	s.docStarted = true

	if s.flow > 0 {
		if s.flowBlockIndent >= 0 && s.col-1 <= s.flowBlockIndent {
			return &SyntaxError{Message: "flow content must be indented more than the enclosing block", Pos: s.position()}
		}
		return s.scanFlowToken()
	}

	return s.scanBlockToken()
}

func (s *scanner) scanBlockToken() error {
	ch := s.peek()
	if s.col-1 > s.indent && ch != '{' && ch != '[' && ch != '}' && ch != ']' {
		lineStart := s.pos - (s.col - 1)
		if lineStart < 0 {
			lineStart = 0
		}
		for i := lineStart; i < s.pos; i++ {
			if s.src[i] == '\t' {
				return &SyntaxError{Message: "tab character used for indentation", Pos: s.position()}
			}
			if s.src[i] != ' ' {
				break
			}
		}
	}
	s.unwindIndents(s.col - 1)

	if ch == '-' && s.isBlankOrEnd(1) {
		return s.scanBlockEntry()
	}

	if ch == '?' && s.isBlankOrEnd(1) {
		return s.scanExplicitKey()
	}

	if ch == ':' && s.isBlankOrEnd(1) {
		return s.scanBlockValue()
	}

	if ch == '{' {
		return s.scanFlowStart(tokenFlowMappingStart)
	}
	if ch == '[' {
		return s.scanFlowStart(tokenFlowSequenceStart)
	}
	if ch == '}' || ch == ']' {
		return &SyntaxError{Message: fmt.Sprintf("unexpected '%c' in block context", ch), Pos: s.position()}
	}

	if (ch == '&' || ch == '*' || ch == '!') && s.col-1 <= s.indent {
		for i := len(s.tokens) - 1; i >= 0; i-- {
			tk := s.tokens[i]
			if tk.kind == tokenComment || tk.kind == tokenAnchor || tk.kind == tokenTag || tk.kind == tokenAlias {
				continue
			}
			if tk.kind == tokenValue && tk.pos.Line < s.line {
				return &SyntaxError{Message: "node property not indented enough", Pos: s.position()}
			}
			break
		}
	}
	if ch == '&' {
		return s.scanAnchor()
	}
	if ch == '*' {
		return s.scanAlias()
	}
	if ch == '!' {
		return s.scanTag()
	}
	if ch == '|' || ch == '>' {
		return s.scanBlockScalar()
	}
	if ch == '\'' {
		return s.scanSingleQuotedScalar()
	}
	if ch == '"' {
		return s.scanDoubleQuotedScalar()
	}

	return s.scanPlainScalar()
}

func (s *scanner) scanFlowToken() error {
	ch := s.peek()

	switch ch {
	case '{':
		return s.scanFlowStart(tokenFlowMappingStart)
	case '}':
		return s.scanFlowEnd(tokenFlowMappingEnd)
	case '[':
		return s.scanFlowStart(tokenFlowSequenceStart)
	case ']':
		return s.scanFlowEnd(tokenFlowSequenceEnd)
	case ',':
		s.emit(tokenFlowEntry, "", s.position())
		s.advance(1)
		s.skipSpacesAndComments()
		return nil
	case '?':
		if s.isBlankOrEnd(1) {
			s.emit(tokenKey, "", s.position())
			s.advance(1)
			s.skipSpacesAndComments()
			return nil
		}
		return s.scanPlainScalar()
	case ':':
		if s.isBlankOrEnd(1) {
			if s.flow > 0 {
				prevIsScalar := false
				for pi := len(s.tokens) - 1; pi >= 0; pi-- {
					pk := s.tokens[pi].kind
					if pk == tokenComment {
						continue
					}
					if pk == tokenScalar || pk == tokenFlowSequenceEnd || pk == tokenFlowMappingEnd || pk == tokenAlias || pk == tokenKey || pk == tokenTag || pk == tokenAnchor {
						prevIsScalar = true
					}
					break
				}
				if !prevIsScalar {
					s.emit(tokenKey, "", s.position())
				}
			}
			s.emit(tokenValue, "", s.position())
			s.advance(1)
			s.skipSpaces()
			return nil
		}
		if s.flow > 0 {
			prevIsScalar := false
			for pi := len(s.tokens) - 1; pi >= 0; pi-- {
				pk := s.tokens[pi].kind
				if pk == tokenComment {
					continue
				}
				if pk == tokenScalar || pk == tokenFlowSequenceEnd || pk == tokenFlowMappingEnd {
					prevIsScalar = true
				}
				break
			}
			next := byte(0)
			if s.pos+1 < len(s.src) {
				next = s.src[s.pos+1]
			}
			if prevIsScalar || next == ',' || next == '}' || next == ']' || next == '{' || next == '[' ||
				next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == 0 {
				s.emit(tokenValue, "", s.position())
				s.advance(1)
				s.skipSpaces()
				return nil
			}
		}
		return s.scanPlainScalar()
	case '&':
		return s.scanAnchor()
	case '*':
		return s.scanAlias()
	case '!':
		return s.scanTag()
	case '\'':
		return s.scanSingleQuotedScalar()
	case '"':
		return s.scanDoubleQuotedScalar()
	case '#':
		s.scanComment()
		return nil
	case '\n', '\r':
		s.advanceLine()
		return nil
	}

	return s.scanPlainScalar()
}

func (s *scanner) scanBlockEntry() error {
	if s.lastContentLine == s.line {
		return &SyntaxError{Message: "block sequence entry not allowed here", Pos: s.position()}
	}
	col := s.col - 1
	if s.col-1 > s.indent {
		s.pushIndent(col, tokenBlockSequenceStart)
	}
	s.emit(tokenBlockEntry, "", s.position())
	s.advance(1)
	s.skipSpaces()
	return nil
}

func (s *scanner) scanExplicitKey() error {
	col := s.col - 1
	if s.col-1 > s.indent {
		s.pushIndent(col, tokenBlockMappingStart)
	}
	s.emit(tokenKey, "", s.position())
	s.advance(1)
	s.skipSpaces()
	return nil
}

func (s *scanner) scanBlockValue() error {
	col := s.col - 1
	hasKey := false
	depth := 0
	for i := len(s.tokens) - 1; i >= 0; i-- {
		tk := s.tokens[i].kind
		if tk == tokenBlockEnd || tk == tokenFlowMappingEnd || tk == tokenFlowSequenceEnd {
			depth++
		} else if tk == tokenBlockMappingStart || tk == tokenBlockSequenceStart ||
			tk == tokenFlowMappingStart || tk == tokenFlowSequenceStart {
			if depth > 0 {
				depth--
			} else {
				break
			}
		} else if depth == 0 {
			if tk == tokenKey {
				hasKey = true
				break
			}
			if tk == tokenValue || tk == tokenDocumentStart || tk == tokenDocumentEnd || tk == tokenStreamStart {
				break
			}
		}
	}
	if !hasKey {
		keyCol := col
		colonLine := s.line
		insertIdx := len(s.tokens)
		for insertIdx > 0 {
			prev := s.tokens[insertIdx-1]
			if prev.pos.Line == colonLine && (prev.kind == tokenAlias || prev.kind == tokenTag || prev.kind == tokenAnchor) {
				keyCol = prev.pos.Column - 1
				insertIdx--
				continue
			}
			if prev.kind == tokenFlowSequenceEnd || prev.kind == tokenFlowMappingEnd {
				flowDepth := 1
				startKind := tokenFlowSequenceStart
				if prev.kind == tokenFlowMappingEnd {
					startKind = tokenFlowMappingStart
				}
				insertIdx--
				for insertIdx > 0 && flowDepth > 0 {
					tk := s.tokens[insertIdx-1].kind
					if tk == prev.kind {
						flowDepth++
					} else if tk == startKind {
						flowDepth--
					}
					insertIdx--
				}
				if insertIdx >= 0 && insertIdx < len(s.tokens) {
					keyCol = s.tokens[insertIdx].pos.Column - 1
				}
				continue
			}
			break
		}
		if s.flow == 0 && keyCol > s.indent {
			s.indents = append(s.indents, s.indent)
			s.indent = keyCol
			mapToken := token{kind: tokenBlockMappingStart, pos: s.position()}
			keyToken := token{kind: tokenKey, pos: s.position()}
			s.tokens = append(s.tokens[:insertIdx], append([]token{mapToken, keyToken}, s.tokens[insertIdx:]...)...)
		} else {
			keyToken := token{kind: tokenKey, pos: s.position()}
			s.tokens = append(s.tokens[:insertIdx], append([]token{keyToken}, s.tokens[insertIdx:]...)...)
		}
	}
	s.emit(tokenValue, "", s.position())
	s.advance(1)
	s.skipSpaces()
	return nil
}

func (s *scanner) scanFlowStart(kind tokenKind) error {
	if s.flow == 0 {
		s.flowBlockIndent = s.indent
	}
	s.flow++
	s.emit(kind, "", s.position())
	s.advance(1)
	s.skipSpacesAndComments()
	return nil
}

func (s *scanner) scanFlowEnd(kind tokenKind) error {
	s.flow--
	if s.flow < 0 {
		s.flow = 0
	}
	pos := s.position()
	s.emit(kind, "", pos)
	s.advance(1)
	s.skipSpaces()

	if !s.atEnd() && s.peek() == ':' {
		next := byte(0)
		if s.pos+1 < len(s.src) {
			next = s.src[s.pos+1]
		}
		isValue := next == 0 || isBlank(next) || next == '\n' || next == '\r'
		if !isValue && s.flow > 0 {
			isValue = true
		}
		if isValue {
			col := pos.Column - 1
			insertIdx := len(s.tokens) - 1
			flowDepth := 1
			startKind := tokenFlowSequenceStart
			if kind == tokenFlowMappingEnd {
				startKind = tokenFlowMappingStart
			}
			for insertIdx > 0 && flowDepth > 0 {
				tk := s.tokens[insertIdx-1].kind
				if tk == kind {
					flowDepth++
				} else if tk == startKind {
					flowDepth--
				}
				insertIdx--
			}
			mapInsertIdx := insertIdx
			for mapInsertIdx > 0 {
				pk := s.tokens[mapInsertIdx-1].kind
				if pk == tokenAnchor || pk == tokenTag {
					mapInsertIdx--
				} else {
					break
				}
			}
			keyInsertIdx := insertIdx
			if insertIdx > 0 && insertIdx < len(s.tokens) {
				flowLine := s.tokens[insertIdx].pos.Line
				keyInsertIdx = mapInsertIdx
				for keyInsertIdx < insertIdx {
					if s.tokens[keyInsertIdx].pos.Line == flowLine {
						break
					}
					keyInsertIdx++
				}
			}
			if mapInsertIdx >= 0 && mapInsertIdx < len(s.tokens) {
				col = s.tokens[mapInsertIdx].pos.Column - 1
			}
			if s.flow == 0 && insertIdx >= 0 && insertIdx < len(s.tokens) &&
				s.tokens[insertIdx].pos.Line < pos.Line {
				return &SyntaxError{Message: "multiline flow key is not allowed", Pos: pos}
			}
			if s.flow == 0 && col > s.indent {
				s.indents = append(s.indents, s.indent)
				s.indent = col
				mapToken := token{kind: tokenBlockMappingStart, pos: pos}
				keyToken := token{kind: tokenKey, pos: pos}
				if mapInsertIdx == keyInsertIdx {
					s.tokens = append(s.tokens[:mapInsertIdx], append([]token{mapToken, keyToken}, s.tokens[mapInsertIdx:]...)...)
				} else {
					s.tokens = append(s.tokens[:keyInsertIdx], append([]token{keyToken}, s.tokens[keyInsertIdx:]...)...)
					s.tokens = append(s.tokens[:mapInsertIdx], append([]token{mapToken}, s.tokens[mapInsertIdx:]...)...)
				}
			} else if s.flow == 0 {
				keyToken := token{kind: tokenKey, pos: pos}
				s.tokens = append(s.tokens[:keyInsertIdx], append([]token{keyToken}, s.tokens[keyInsertIdx:]...)...)
			} else {
				keyToken := token{kind: tokenKey, pos: pos}
				s.tokens = append(s.tokens[:keyInsertIdx], append([]token{keyToken}, s.tokens[keyInsertIdx:]...)...)
			}
		}
	}

	return nil
}

func (s *scanner) scanAnchor() error {
	pos := s.position()
	s.advance(1)
	start := s.pos
	for !s.atEnd() {
		ch := s.peek()
		if isBlank(ch) || ch == '\n' || ch == '\r' || ch == ',' || ch == '}' || ch == ']' || ch == '{' || ch == '[' {
			break
		}
		s.advance(1)
	}
	s.emit(tokenAnchor, string(s.src[start:s.pos]), pos)
	s.skipSpaces()
	return nil
}

func (s *scanner) scanAlias() error {
	pos := s.position()
	s.advance(1)
	start := s.pos
	for !s.atEnd() {
		ch := s.peek()
		if isBlank(ch) || ch == ',' || ch == '}' || ch == ']' || ch == '{' || ch == '[' || ch == '\n' || ch == '\r' {
			break
		}
		s.advance(1)
	}
	s.emit(tokenAlias, string(s.src[start:s.pos]), pos)
	s.skipSpaces()
	return nil
}

func (s *scanner) scanTag() error {
	pos := s.position()
	s.advance(1)
	start := s.pos - 1

	if !s.atEnd() && s.peek() == '<' {
		for !s.atEnd() && s.peek() != '>' {
			s.advance(1)
		}
		if !s.atEnd() {
			s.advance(1)
		}
	} else if !s.atEnd() && s.peek() == '!' {
		s.advance(1)
		for !s.atEnd() && !isBlank(s.peek()) && s.peek() != '\n' && s.peek() != '\r' {
			ch := s.peek()
			if ch == ',' || ch == '}' || ch == ']' || ch == '{' || ch == '[' {
				if s.flow > 0 {
					break
				}
				return &SyntaxError{Message: fmt.Sprintf("invalid character '%c' in tag", ch), Pos: s.position()}
			}
			s.advance(1)
		}
	} else {
		for !s.atEnd() && !isBlank(s.peek()) && s.peek() != '\n' && s.peek() != '\r' {
			ch := s.peek()
			if ch == ',' || ch == '}' || ch == ']' || ch == '{' || ch == '[' {
				if s.flow > 0 {
					break
				}
				return &SyntaxError{Message: fmt.Sprintf("invalid character '%c' in tag", ch), Pos: s.position()}
			}
			s.advance(1)
		}
	}

	s.emit(tokenTag, string(s.src[start:s.pos]), pos)
	s.skipSpaces()
	return nil
}

func (s *scanner) scanBlockScalar() error {
	pos := s.position()
	ch := s.peek()
	style := scalarLiteral
	if ch == '>' {
		style = scalarFolded
	}
	s.advance(1)

	chomp := 0
	explicitIndent := 0

	for !s.atEnd() && s.peek() != '\n' && s.peek() != '\r' {
		c := s.peek()
		if c == '-' {
			chomp = -1
			s.advance(1)
		} else if c == '+' {
			chomp = 1
			s.advance(1)
		} else if c >= '1' && c <= '9' {
			explicitIndent = int(c - '0')
			s.advance(1)
		} else if c == '#' {
			if s.pos > 0 && isBlank(s.src[s.pos-1]) {
				s.scanComment()
				break
			}
			return &SyntaxError{Message: "comment must be preceded by whitespace", Pos: s.position()}
		} else if c == ' ' || c == '\t' {
			s.advance(1)
		} else {
			return &SyntaxError{Message: "invalid content after block scalar indicator", Pos: s.position()}
		}
	}

	if !s.atEnd() && (s.peek() == '\n' || s.peek() == '\r') {
		s.advanceLine()
	}

	blockIndent := 0
	if explicitIndent > 0 {
		blockIndent = s.indent + explicitIndent
	} else {
		maxEmptyIndent := 0
		lineSpaces := 0
		for i := s.pos; i < len(s.src); i++ {
			if s.src[i] == ' ' {
				lineSpaces++
				continue
			}
			if s.src[i] == '\n' || s.src[i] == '\r' {
				if lineSpaces > maxEmptyIndent {
					maxEmptyIndent = lineSpaces
				}
				lineSpaces = 0
				continue
			}
			col := 0
			for j := i - 1; j >= 0 && s.src[j] != '\n' && s.src[j] != '\r'; j-- {
				col++
			}
			blockIndent = col
			break
		}
		if blockIndent > 0 && maxEmptyIndent > blockIndent {
			return &SyntaxError{Message: "leading empty lines have more indentation than content", Pos: pos}
		}
	}

	if blockIndent <= s.indent {
		blockIndent = s.indent + 1
	}

	var lines []string
	var trailingNewlines int

	for !s.atEnd() {
		lineStart := s.pos
		lineCol := 0
		for !s.atEnd() && s.peek() == ' ' {
			s.advance(1)
			lineCol++
		}

		if s.atEnd() || s.peek() == '\n' || s.peek() == '\r' {
			if lineCol > blockIndent {
				for i := 0; i < trailingNewlines; i++ {
					lines = append(lines, "")
				}
				trailingNewlines = 0
				lines = append(lines, strings.Repeat(" ", lineCol-blockIndent))
			} else {
				trailingNewlines++
			}
			if !s.atEnd() {
				s.advanceLine()
			}
			continue
		}

		if lineCol < blockIndent {
			s.pos = lineStart
			s.col = s.col - lineCol
			break
		}

		if lineCol == 0 && s.pos+3 <= len(s.src) {
			marker := string(s.src[s.pos : s.pos+3])
			if (marker == "---" || marker == "...") && (s.pos+3 >= len(s.src) || isBlank(s.src[s.pos+3]) || s.src[s.pos+3] == '\n' || s.src[s.pos+3] == '\r') {
				s.pos = lineStart
				s.col = s.col - lineCol
				break
			}
		}

		for i := 0; i < trailingNewlines; i++ {
			lines = append(lines, "")
		}
		trailingNewlines = 0

		contentStart := s.pos
		for !s.atEnd() && s.peek() != '\n' && s.peek() != '\r' {
			s.advance(1)
		}
		line := string(s.src[contentStart:s.pos])
		if lineCol > blockIndent {
			line = strings.Repeat(" ", lineCol-blockIndent) + line
		}
		lines = append(lines, line)

		if !s.atEnd() {
			s.advanceLine()
		}
	}

	var result string
	if len(lines) == 0 {
		switch chomp {
		case 1:
			if trailingNewlines > 0 {
				result = strings.Repeat("\n", trailingNewlines)
			}
		default:
			result = ""
		}
	} else {
		if style == scalarLiteral {
			result = strings.Join(lines, "\n")
		} else {
			var buf strings.Builder
			emptyCount := 0
			lastContentLine := ""
			wroteContent := false
			for _, line := range lines {
				if line == "" {
					emptyCount++
					continue
				}
				if !wroteContent {
					for range emptyCount {
						buf.WriteByte('\n')
					}
					buf.WriteString(line)
					emptyCount = 0
					wroteContent = true
					lastContentLine = line
					continue
				}
				if emptyCount > 0 {
					n := emptyCount
					moreIndented := (len(line) > 0 && (line[0] == ' ' || line[0] == '\t')) ||
						(len(lastContentLine) > 0 && (lastContentLine[0] == ' ' || lastContentLine[0] == '\t'))
					if moreIndented {
						n++
					}
					for range n {
						buf.WriteByte('\n')
					}
					buf.WriteString(line)
					emptyCount = 0
				} else {
					moreIndentPrev := len(lastContentLine) > 0 && (lastContentLine[0] == ' ' || lastContentLine[0] == '\t')
					moreIndentCurr := len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
					if moreIndentPrev || moreIndentCurr {
						buf.WriteByte('\n')
					} else {
						buf.WriteByte(' ')
					}
					buf.WriteString(line)
				}
				lastContentLine = line
			}
			for range emptyCount {
				buf.WriteByte('\n')
			}
			result = buf.String()
		}

		switch chomp {
		case -1:
		case 0:
			result += "\n"
		case 1:
			result += "\n"
			result += strings.Repeat("\n", trailingNewlines)
		}
	}

	s.emit(tokenScalar, result, pos)
	s.tokens[len(s.tokens)-1].style = style
	return nil
}

func (s *scanner) scanSingleQuotedScalar() error {
	pos := s.position()
	s.advance(1)
	var buf []byte

	for {
		if s.atEnd() {
			return &SyntaxError{Message: "unterminated single-quoted string", Pos: pos}
		}
		ch := s.peek()
		if ch == '\'' {
			s.advance(1)
			if !s.atEnd() && s.peek() == '\'' {
				buf = append(buf, '\'')
				s.advance(1)
				continue
			}
			break
		}
		if ch == '\n' || ch == '\r' {
			for len(buf) > 0 && (buf[len(buf)-1] == ' ' || buf[len(buf)-1] == '\t') {
				buf = buf[:len(buf)-1]
			}
			s.advanceLine()
			if err := s.checkDocMarkerInQuoted("single-quoted"); err != nil {
				return err
			}
			for !s.atEnd() && (s.peek() == ' ' || s.peek() == '\t') {
				s.advance(1)
			}
			emptyLines := 0
			for !s.atEnd() && (s.peek() == '\n' || s.peek() == '\r') {
				emptyLines++
				s.advanceLine()
				if err := s.checkDocMarkerInQuoted("single-quoted"); err != nil {
					return err
				}
				for !s.atEnd() && (s.peek() == ' ' || s.peek() == '\t') {
					s.advance(1)
				}
			}
			if emptyLines > 0 {
				for range emptyLines {
					buf = append(buf, '\n')
				}
			} else {
				buf = append(buf, ' ')
			}
			continue
		}
		buf = append(buf, ch)
		s.advance(1)
	}

	s.emitScalar(string(buf), pos)
	s.tokens[len(s.tokens)-1].style = scalarSingleQuoted
	s.skipSpaces()
	if err := s.checkTrailingAfterQuoted(); err != nil {
		return err
	}
	return nil
}

func (s *scanner) scanDoubleQuotedScalar() error {
	pos := s.position()
	s.advance(1)
	var buf []byte

	for {
		if s.atEnd() {
			return &SyntaxError{Message: "unterminated double-quoted string", Pos: pos}
		}
		ch := s.peek()
		if ch == '"' {
			s.advance(1)
			break
		}
		if ch == '\\' {
			s.advance(1)
			if s.atEnd() {
				return &SyntaxError{Message: "unexpected end of input in escape sequence", Pos: s.position()}
			}
			esc := s.peek()
			s.advance(1)
			switch esc {
			case '0':
				buf = append(buf, 0)
			case 'a':
				buf = append(buf, '\a')
			case 'b':
				buf = append(buf, '\b')
			case 't', '\t':
				buf = append(buf, '\t')
			case 'n':
				buf = append(buf, '\n')
			case 'v':
				buf = append(buf, '\v')
			case 'f':
				buf = append(buf, '\f')
			case 'r':
				buf = append(buf, '\r')
			case 'e':
				buf = append(buf, 0x1b)
			case ' ':
				buf = append(buf, ' ')
			case '"':
				buf = append(buf, '"')
			case '/':
				buf = append(buf, '/')
			case '\\':
				buf = append(buf, '\\')
			case 'N':
				buf = append(buf, 0xc2, 0x85)
			case '_':
				buf = append(buf, 0xc2, 0xa0)
			case 'L':
				buf = append(buf, 0xe2, 0x80, 0xa8)
			case 'P':
				buf = append(buf, 0xe2, 0x80, 0xa9)
			case 'x':
				r, err := s.scanHexEscape(2)
				if err != nil {
					return err
				}
				buf = utf8.AppendRune(buf, r)
			case 'u':
				r, err := s.scanHexEscape(4)
				if err != nil {
					return err
				}
				buf = utf8.AppendRune(buf, r)
			case 'U':
				r, err := s.scanHexEscape(8)
				if err != nil {
					return err
				}
				buf = utf8.AppendRune(buf, r)
			case '\n':
				for !s.atEnd() && s.peek() == ' ' {
					s.advance(1)
				}
			case '\r':
				if !s.atEnd() && s.peek() == '\n' {
					s.advance(1)
				}
				for !s.atEnd() && s.peek() == ' ' {
					s.advance(1)
				}
			default:
				return &SyntaxError{Message: fmt.Sprintf("invalid escape character '%c'", esc), Pos: s.position()}
			}
			continue
		}
		if ch == '\n' || ch == '\r' {
			for len(buf) > 0 && (buf[len(buf)-1] == ' ' || buf[len(buf)-1] == '\t') {
				buf = buf[:len(buf)-1]
			}
			s.advanceLine()
			if err := s.checkDocMarkerInQuoted("double-quoted"); err != nil {
				return err
			}
			for !s.atEnd() && (s.peek() == ' ' || s.peek() == '\t') {
				s.advance(1)
			}
			emptyLines := 0
			for !s.atEnd() && (s.peek() == '\n' || s.peek() == '\r') {
				emptyLines++
				s.advanceLine()
				if err := s.checkDocMarkerInQuoted("double-quoted"); err != nil {
					return err
				}
				for !s.atEnd() && (s.peek() == ' ' || s.peek() == '\t') {
					s.advance(1)
				}
			}
			if s.flow == 0 && !s.atEnd() && s.col-1 <= s.indent {
				return &SyntaxError{Message: "quoted scalar continuation not indented enough", Pos: s.position()}
			}
			if emptyLines > 0 {
				for range emptyLines {
					buf = append(buf, '\n')
				}
			} else {
				buf = append(buf, ' ')
			}
			continue
		}
		buf = append(buf, ch)
		s.advance(1)
	}

	s.emitScalar(string(buf), pos)
	s.tokens[len(s.tokens)-1].style = scalarDoubleQuoted
	s.skipSpaces()
	if err := s.checkTrailingAfterQuoted(); err != nil {
		return err
	}
	return nil
}

func (s *scanner) checkTrailingAfterQuoted() error {
	if s.atEnd() {
		return nil
	}
	ch := s.peek()
	if ch == '\n' || ch == '\r' || ch == ':' {
		return nil
	}
	if ch == '#' && s.pos > 0 && isBlank(s.src[s.pos-1]) {
		return nil
	}
	if s.flow > 0 && (ch == ',' || ch == '}' || ch == ']' || ch == '{' || ch == '[') {
		return nil
	}
	return &SyntaxError{Message: "trailing content after quoted scalar", Pos: s.position()}
}

func (s *scanner) scanHexEscape(n int) (rune, error) {
	var val rune
	for range n {
		if s.atEnd() {
			return 0, &SyntaxError{Message: "unexpected end of hex escape", Pos: s.position()}
		}
		ch := s.peek()
		s.advance(1)
		val <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			val |= rune(ch - '0')
		case ch >= 'a' && ch <= 'f':
			val |= rune(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			val |= rune(ch-'A') + 10
		default:
			return 0, &SyntaxError{Message: "invalid hex digit in escape", Pos: s.position()}
		}
	}
	return val, nil
}

func (s *scanner) scanPlainScalar() error {
	pos := s.position()
	startIndent := s.col

	if s.flow > 0 {
		ch := s.peek()
		if ch == '-' || ch == '?' || ch == ':' {
			next := byte(0)
			if s.pos+1 < len(s.src) {
				next = s.src[s.pos+1]
			}
			if next == 0 || isBlank(next) || next == '\n' || next == '\r' ||
				next == ',' || next == ']' || next == '}' || next == '[' || next == '{' {
				return &SyntaxError{Message: fmt.Sprintf("invalid plain scalar character '%c' in flow context", ch), Pos: pos}
			}
		}
	}

	var parts []string
	hadNewline := false
	lastTextLine := pos.Line

	for {
		start := s.pos
		for !s.atEnd() {
			ch := s.peek()
			if ch == '\n' || ch == '\r' {
				break
			}
			if ch < 0x20 && ch != '\t' {
				return &SyntaxError{
					Message: "non-printable character in input",
					Pos:     s.position(),
				}
			}
			if ch == '#' && s.pos > 0 && isBlank(s.src[s.pos-1]) {
				break
			}
			if s.flow > 0 && (ch == ',' || ch == '}' || ch == ']' || ch == '{' || ch == '[') {
				break
			}
			if ch == ':' && s.isBlankOrEnd(1) {
				break
			}
			if ch == ':' && s.flow > 0 {
				next := byte(0)
				if s.pos+1 < len(s.src) {
					next = s.src[s.pos+1]
				}
				if next == ',' || next == '}' || next == ']' || next == '{' || next == '[' {
					break
				}
			}
			s.advance(1)
		}

		text := strings.TrimRight(string(s.src[start:s.pos]), " \t")
		if text == "" && hadNewline {
			break
		}

		if hadNewline && len(parts) > 0 {
			lastPart := parts[len(parts)-1]
			if lastPart == "\n" {
				parts = append(parts, text)
			} else {
				parts = append(parts, " "+text)
			}
		} else {
			parts = append(parts, text)
		}
		lastTextLine = s.line

		if s.atEnd() || s.peek() == '#' {
			break
		}
		if s.flow > 0 {
			ch := s.peek()
			if ch == ',' || ch == '}' || ch == ']' || ch == '{' || ch == '[' {
				break
			}
		}

		if s.peek() != '\n' && s.peek() != '\r' {
			break
		}

		s.advanceLine()
		emptyLines := 0
		for !s.atEnd() {
			if s.peek() == '\n' || s.peek() == '\r' {
				emptyLines++
				s.advanceLine()
				continue
			}
			savedPos := s.pos
			savedLine := s.line
			savedCol := s.col
			for !s.atEnd() && (s.peek() == ' ' || s.peek() == '\t') {
				s.advance(1)
			}
			if !s.atEnd() && (s.peek() == '\n' || s.peek() == '\r') {
				emptyLines++
				s.advanceLine()
				continue
			}
			s.pos = savedPos
			s.line = savedLine
			s.col = savedCol
			break
		}
		s.skipSpaces()

		if s.atEnd() {
			break
		}

		if s.col <= startIndent && s.col <= s.indent+1 {
			break
		}

		if s.col == 1 && s.startsWith("---") && s.isBlankOrEnd(3) {
			break
		}
		if s.col == 1 && s.startsWith("...") && s.isBlankOrEnd(3) {
			break
		}

		ch := s.peek()
		if ch == '-' && s.isBlankOrEnd(1) {
			if s.indent < 0 || s.col-1 <= s.indent {
				break
			}
		}
		if ch == ':' && s.isBlankOrEnd(1) {
			break
		}
		if ch == '#' {
			break
		}
		if ch == '?' && s.isBlankOrEnd(1) {
			break
		}

		if emptyLines > 0 {
			for range emptyLines {
				parts = append(parts, "\n")
			}
		}
		hadNewline = true
	}

	value := strings.Join(parts, "")

	if s.line != pos.Line {
		if s.flow == 0 && s.line == lastTextLine {
			tmpPos := s.pos
			for tmpPos < len(s.src) && s.src[tmpPos] == ' ' {
				tmpPos++
			}
			if tmpPos < len(s.src) && s.src[tmpPos] == ':' {
				next := byte(0)
				if tmpPos+1 < len(s.src) {
					next = s.src[tmpPos+1]
				}
				if next == 0 || isBlank(next) || next == '\n' || next == '\r' {
					return &SyntaxError{Message: "multiline scalar used as implicit key", Pos: pos}
				}
			}
		}
		s.emit(tokenScalar, value, pos)
	} else {
		s.emitScalar(value, pos)
	}
	return nil
}

func (s *scanner) scanComment() {
	pos := s.position()
	s.advance(1)
	if !s.atEnd() && s.peek() == ' ' {
		s.advance(1)
	}
	start := s.pos
	for !s.atEnd() && s.peek() != '\n' && s.peek() != '\r' {
		s.advance(1)
	}
	s.emit(tokenComment, string(s.src[start:s.pos]), pos)
}

func (s *scanner) scanDirective() error {
	pos := s.position()
	start := s.pos
	for !s.atEnd() && s.peek() != '\n' && s.peek() != '\r' {
		s.advance(1)
	}
	s.emit(tokenDirective, string(s.src[start:s.pos]), pos)
	if !s.atEnd() {
		s.advanceLine()
	}
	return nil
}

func (s *scanner) pushIndent(col int, kind tokenKind) {
	if col > s.indent {
		s.indents = append(s.indents, s.indent)
		s.indent = col
		s.emit(kind, "", s.position())
	}
}

func (s *scanner) unwindIndents(col int) {
	for s.indent > col && len(s.indents) > 0 {
		s.indent = s.indents[len(s.indents)-1]
		s.indents = s.indents[:len(s.indents)-1]
		s.emit(tokenBlockEnd, "", s.position())
	}
}

func (s *scanner) emitScalar(value string, pos Position) {
	col := pos.Column - 1
	multiline := s.line > pos.Line

	needMapping := false
	savedPos := s.pos
	savedLine := s.line
	savedCol := s.col

	tmpPos := s.pos
	for tmpPos < len(s.src) && s.src[tmpPos] == ' ' {
		tmpPos++
	}
	if tmpPos < len(s.src) && s.src[tmpPos] == ':' {
		next := byte(0)
		if tmpPos+1 < len(s.src) {
			next = s.src[tmpPos+1]
		}
		isValueIndicator := false
		if next == 0 || isBlank(next) || next == '\n' || next == '\r' {
			isValueIndicator = true
		} else if s.flow > 0 && (next == ',' || next == '}' || next == ']' || next == '{' || next == '[') {
			isValueIndicator = true
		} else if s.flow > 0 && tmpPos == s.pos {
			isValueIndicator = true
		}
		if isValueIndicator && multiline && s.flow == 0 {
			s.scanErr = &SyntaxError{Message: "multiline scalar used as implicit key", Pos: pos}
		}
		if isValueIndicator && !multiline {
			needMapping = true
		}
	}

	s.pos = savedPos
	s.line = savedLine
	s.col = savedCol

	if needMapping {
		if s.flow == 0 {
			hasValueOnLine := false
			hasContentAfterValue := false
			for i := len(s.tokens) - 1; i >= 0; i-- {
				tk := s.tokens[i]
				if tk.pos.Line < pos.Line {
					break
				}
				if hasValueOnLine && (tk.kind == tokenScalar || tk.kind == tokenAlias ||
					tk.kind == tokenFlowSequenceEnd || tk.kind == tokenFlowMappingEnd) {
					hasContentAfterValue = true
					break
				}
				if tk.kind == tokenValue && tk.pos.Line == pos.Line {
					hasValueOnLine = true
				}
			}
			if hasValueOnLine && hasContentAfterValue {
				s.scanErr = &SyntaxError{Message: "invalid nested implicit key on same line", Pos: pos}
			}
		}
		insertIdx := len(s.tokens)
		indentCol := col
		hasPropOnLine := false
		for insertIdx > 0 {
			prev := s.tokens[insertIdx-1]
			if (prev.kind == tokenTag || prev.kind == tokenAnchor) && prev.pos.Line == pos.Line {
				indentCol = prev.pos.Column - 1
				hasPropOnLine = true
				insertIdx--
			} else {
				break
			}
		}
		if s.flow == 0 && hasPropOnLine && pos.Line == s.docStartLine {
			s.scanErr = &SyntaxError{Message: "block collection on document start line must not have properties", Pos: pos}
		}
		if s.flow == 0 && indentCol > s.indent {
			s.indents = append(s.indents, s.indent)
			s.indent = indentCol
			if pos.Line == s.docStartLine {
				s.docStartMapIndent = indentCol
			}
			mapToken := token{kind: tokenBlockMappingStart, pos: pos}
			keyToken := token{kind: tokenKey, pos: pos}
			s.tokens = append(s.tokens[:insertIdx], append([]token{mapToken, keyToken}, s.tokens[insertIdx:]...)...)
		} else {
			if s.flow == 0 && s.line > s.docStartLine && s.docStartMapIndent >= 0 && indentCol == s.docStartMapIndent {
				s.scanErr = &SyntaxError{Message: "block mapping on document start line cannot continue to next line", Pos: pos}
			}
			keyToken := token{kind: tokenKey, pos: pos}
			s.tokens = append(s.tokens[:insertIdx], append([]token{keyToken}, s.tokens[insertIdx:]...)...)
		}
	} else if s.flow == 0 {
		if col > s.indent {
			isSeqCtx := false
			for i := len(s.tokens) - 1; i >= 0; i-- {
				tk := s.tokens[i].kind
				if tk == tokenBlockEntry {
					isSeqCtx = true
					break
				}
				if tk == tokenValue || tk == tokenKey || tk == tokenBlockMappingStart || tk == tokenBlockSequenceStart || tk == tokenBlockEnd {
					break
				}
			}
			if !isSeqCtx {
			}
		}
	}

	s.emit(tokenScalar, value, pos)
}

func (s *scanner) emit(kind tokenKind, value string, pos Position) {
	s.tokens = append(s.tokens, token{kind: kind, value: value, pos: pos})
	if kind == tokenAnchor || kind == tokenAlias || kind == tokenTag || kind == tokenScalar {
		s.lastContentLine = pos.Line
	}
}

func (s *scanner) position() Position {
	return Position{Line: s.line, Column: s.col, Offset: s.pos}
}

func (s *scanner) peek() byte {
	if s.pos >= len(s.src) {
		return 0
	}
	return s.src[s.pos]
}

func (s *scanner) advance(n int) {
	for i := 0; i < n && s.pos < len(s.src); i++ {
		if s.src[s.pos] == '\n' {
			s.line++
			s.col = 1
		} else {
			s.col++
		}
		s.pos++
	}
}

func (s *scanner) advanceLine() {
	if s.pos < len(s.src) {
		if s.src[s.pos] == '\r' {
			s.pos++
			if s.pos < len(s.src) && s.src[s.pos] == '\n' {
				s.pos++
			}
		} else if s.src[s.pos] == '\n' {
			s.pos++
		}
		s.line++
		s.col = 1
	}
}

func (s *scanner) atEnd() bool {
	return s.pos >= len(s.src)
}

func (s *scanner) checkDocMarkerInQuoted(kind string) error {
	if s.col == 1 && (s.startsWith("---") || s.startsWith("...")) && s.isBlankOrEnd(3) {
		return &SyntaxError{Message: fmt.Sprintf("document marker inside %s scalar", kind), Pos: s.position()}
	}
	return nil
}

func (s *scanner) startsWith(prefix string) bool {
	if s.pos+len(prefix) > len(s.src) {
		return false
	}
	return string(s.src[s.pos:s.pos+len(prefix)]) == prefix
}

func (s *scanner) isBlankOrEnd(offset int) bool {
	p := s.pos + offset
	if p >= len(s.src) {
		return true
	}
	ch := s.src[p]
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func (s *scanner) skipSpaces() {
	for !s.atEnd() && (s.peek() == ' ' || s.peek() == '\t') {
		s.advance(1)
	}
}

func (s *scanner) skipSpacesAndComments() {
	for {
		s.skipSpaces()
		if s.atEnd() {
			break
		}
		ch := s.peek()
		if ch == '#' {
			if s.pos == 0 || s.col == 1 || isBlank(s.src[s.pos-1]) {
				s.scanComment()
				continue
			}
			break
		}
		if ch == '\n' || ch == '\r' {
			s.advanceLine()
			continue
		}
		break
	}
}

func (s *scanner) skipBOM() error {
	if len(s.src) >= 3 && s.src[0] == 0xEF && s.src[1] == 0xBB && s.src[2] == 0xBF {
		s.pos += 3
	}
	return nil
}

func isBlank(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

// Valid reports whether data is valid YAML.
func Valid(data []byte) bool {
	data, err := detectAndConvertEncoding(data)
	if err != nil {
		return false
	}
	tokens, err := newScanner(data).scan()
	if err != nil {
		return false
	}
	p := newParser(tokens)
	_, err = p.parse()
	return err == nil
}
