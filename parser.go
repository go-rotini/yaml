package yaml

import (
	"fmt"
	"strings"
)

type parser struct {
	tokens            []token
	pos               int
	anchors           map[string]*node
	tagHandles        map[string]string
	maxNodes          int
	nodeCount         int
	seenYAMLDirective bool
	warnings          []string
}

func newParser(tokens []token) *parser {
	return &parser{
		tokens:  tokens,
		anchors: make(map[string]*node),
		tagHandles: map[string]string{
			"!":  "!",
			"!!": "tag:yaml.org,2002:",
		},
	}
}

func (p *parser) parse() ([]*node, error) {
	var docs []*node
	prevDocEnded := false

	if p.peek().kind == tokenStreamStart {
		p.advance()
	}

	for p.peek().kind != tokenStreamEnd {
		prevPos := p.pos
		p.skipComments()
		if p.peek().kind == tokenStreamEnd {
			break
		}

		if len(docs) > 0 && !prevDocEnded {
			tk := p.peek().kind
			if tk == tokenScalar || tk == tokenBlockMappingStart || tk == tokenBlockSequenceStart ||
				tk == tokenFlowMappingStart || tk == tokenFlowSequenceStart ||
				tk == tokenAnchor || tk == tokenTag || tk == tokenAlias || tk == tokenKey || tk == tokenValue {
				return nil, &SyntaxError{
					Message: "expected document start marker (---) before new content",
					Pos:     p.peek().pos,
				}
			}
		}

		p.tagHandles = map[string]string{
			"!":  "!",
			"!!": "tag:yaml.org,2002:",
		}
		p.seenYAMLDirective = false
		hasDirectives := false
		for p.peek().kind == tokenDirective {
			hasDirectives = true
			if err := p.parseDirective(); err != nil {
				return nil, err
			}
			p.skipComments()
		}

		if hasDirectives && p.peek().kind != tokenDocumentStart {
			return nil, &SyntaxError{
				Message: "expected document start (---) after directive",
				Pos:     p.peek().pos,
			}
		}

		doc, err := p.parseDocument()
		if err != nil {
			return nil, err
		}
		if doc != nil {
			prevDocEnded = doc.docEndExplicit || doc.docStartExplicit
			docs = append(docs, doc)
		}
		if p.pos == prevPos {
			return nil, &SyntaxError{
				Message: fmt.Sprintf("parser stuck at token %s", p.peek()),
				Pos:     p.peek().pos,
			}
		}
	}

	return docs, nil
}

func (p *parser) parseDirective() error {
	t := p.peek()
	p.advance()

	text := t.value
	if idx := strings.Index(text, " #"); idx >= 0 {
		text = strings.TrimRight(text[:idx], " \t")
	}
	if strings.HasPrefix(text, "%TAG") {
		parts := strings.Fields(text)
		if len(parts) != 3 {
			return &SyntaxError{
				Message: fmt.Sprintf("invalid %%TAG directive: %s", text),
				Pos:     t.pos,
			}
		}
		handle := parts[1]
		prefix := parts[2]
		p.tagHandles[handle] = prefix
	} else if strings.HasPrefix(text, "%YAML") {
		parts := strings.Fields(text)
		if len(parts) != 2 {
			return &SyntaxError{
				Message: fmt.Sprintf("invalid %%YAML directive: %s", text),
				Pos:     t.pos,
			}
		}
		if p.seenYAMLDirective {
			return &SyntaxError{
				Message: "duplicate %YAML directive",
				Pos:     t.pos,
			}
		}
		p.seenYAMLDirective = true
	} else {
		name := strings.Fields(text)[0]
		p.warnings = append(p.warnings, fmt.Sprintf("line %d: unknown directive %q", t.pos.Line, name))
	}
	return nil
}

func (p *parser) resolveTag(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "!<") && strings.HasSuffix(raw, ">") {
		return raw[2 : len(raw)-1], nil
	}
	if strings.HasPrefix(raw, "!!") {
		if prefix, ok := p.tagHandles["!!"]; ok {
			return prefix + decodeTagURI(raw[2:]), nil
		}
		return "tag:yaml.org,2002:" + decodeTagURI(raw[2:]), nil
	}
	for handle, prefix := range p.tagHandles {
		if handle == "!" || handle == "!!" {
			continue
		}
		if strings.HasPrefix(raw, handle) {
			return prefix + decodeTagURI(raw[len(handle):]), nil
		}
	}
	if strings.HasPrefix(raw, "!") {
		if idx := strings.Index(raw[1:], "!"); idx >= 0 {
			handle := raw[:idx+2]
			return "", fmt.Errorf("undefined tag handle: %s", handle)
		}
		if prefix, ok := p.tagHandles["!"]; ok && prefix != "!" {
			return prefix + decodeTagURI(raw[1:]), nil
		}
	}
	return raw, nil
}

func decodeTagURI(s string) string {
	var buf []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi := unhex(s[i+1])
			lo := unhex(s[i+2])
			if hi >= 0 && lo >= 0 {
				buf = append(buf, byte(hi<<4|lo))
				i += 2
				continue
			}
		}
		buf = append(buf, s[i])
	}
	return string(buf)
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}

func (p *parser) parseDocument() (*node, error) {
	p.skipComments()

	doc := &node{
		kind: nodeDocument,
		pos:  p.peek().pos,
	}

	if p.peek().kind == tokenDocumentStart {
		doc.docStartExplicit = true
		doc.pos = p.peek().pos
		p.advance()
		p.skipComments()
	}

	if p.peek().kind == tokenDocumentEnd {
		doc.docEndExplicit = true
		if doc.docStartExplicit {
			doc.children = append(doc.children, &node{kind: nodeScalar, value: "", pos: p.peek().pos, implicit: true})
		}
		p.advance()
		if !doc.docStartExplicit && len(doc.children) == 0 {
			return nil, nil
		}
		return doc, nil
	}

	if p.peek().kind == tokenStreamEnd {
		if doc.docStartExplicit {
			doc.children = append(doc.children, &node{kind: nodeScalar, value: "", pos: p.peek().pos, implicit: true})
			return doc, nil
		}
		return nil, nil
	}

	content, err := p.parseNode()
	if err != nil {
		return nil, err
	}
	if content != nil {
		doc.children = append(doc.children, content)
	}

	p.skipComments()
	if p.peek().kind == tokenDocumentEnd {
		doc.docEndExplicit = true
		p.advance()
	} else if tk := p.peek().kind; tk != tokenDocumentStart && tk != tokenStreamEnd && tk != tokenBlockEnd {
		return nil, &SyntaxError{
			Message: "unexpected content after document root node",
			Pos:     p.peek().pos,
		}
	}

	return doc, nil
}

func (p *parser) parseNode() (*node, error) {
	p.skipComments()

	p.nodeCount++
	if p.maxNodes > 0 && p.nodeCount > p.maxNodes {
		return nil, &SyntaxError{
			Message: fmt.Sprintf("exceeded max node count %d", p.maxNodes),
			Pos:     p.peek().pos,
		}
	}

	var anchor string
	var tag string
	propLine := 0

	for {
		t := p.peek()
		if t.kind == tokenAnchor {
			if anchor != "" {
				return nil, &SyntaxError{Message: "only one anchor is allowed per node", Pos: t.pos}
			}
			anchor = t.value
			if propLine == 0 {
				propLine = t.pos.Line
			}
			p.advance()
			p.skipComments()
			continue
		}
		if t.kind == tokenTag {
			if tag != "" {
				return nil, &SyntaxError{Message: "only one tag is allowed per node", Pos: t.pos}
			}
			tag = t.value
			if propLine == 0 {
				propLine = t.pos.Line
			}
			p.advance()
			p.skipComments()
			continue
		}
		break
	}

	t := p.peek()

	if propLine > 0 && (anchor != "" || tag != "") &&
		(t.kind == tokenBlockMappingStart || t.kind == tokenBlockSequenceStart) &&
		t.pos.Line == propLine {
		return nil, &SyntaxError{
			Message: "block collection must start on a new line after properties",
			Pos:     t.pos,
		}
	}

	emptyTokens := t.kind == tokenKey || t.kind == tokenValue ||
		t.kind == tokenBlockEnd || t.kind == tokenDocumentStart ||
		t.kind == tokenDocumentEnd || t.kind == tokenStreamEnd ||
		t.kind == tokenFlowEntry || t.kind == tokenFlowMappingEnd ||
		t.kind == tokenFlowSequenceEnd
	if (anchor != "" || tag != "") && emptyTokens {
		n := &node{kind: nodeScalar, value: "", pos: t.pos, implicit: true}
		if anchor != "" {
			n.anchor = anchor
			p.anchors[anchor] = n
		}
		if tag != "" {
			resolved, err := p.resolveTag(tag)
			if err != nil {
				return nil, &SyntaxError{Message: err.Error(), Pos: t.pos}
			}
			n.tag = resolved
		}
		return n, nil
	}

	var n *node
	var err error

	switch t.kind {
	case tokenAlias:
		if anchor != "" {
			return nil, &SyntaxError{Message: "anchor on alias node is not allowed", Pos: t.pos}
		}
		n = &node{
			kind:  nodeAlias,
			alias: t.value,
			pos:   t.pos,
		}
		p.advance()

	case tokenScalar:
		n = &node{
			kind:  nodeScalar,
			value: t.value,
			style: t.style,
			pos:   t.pos,
		}
		p.advance()

	case tokenBlockMappingStart:
		n, err = p.parseBlockMapping()
		if err != nil {
			return nil, err
		}

	case tokenBlockSequenceStart:
		n, err = p.parseBlockSequence()
		if err != nil {
			return nil, err
		}

	case tokenFlowMappingStart:
		n, err = p.parseFlowMapping()
		if err != nil {
			return nil, err
		}

	case tokenFlowSequenceStart:
		n, err = p.parseFlowSequence()
		if err != nil {
			return nil, err
		}

	case tokenBlockEntry:
		n, err = p.parseBlockSequence()
		if err != nil {
			return nil, err
		}

	case tokenKey:
		n, err = p.parseBlockMapping()
		if err != nil {
			return nil, err
		}

	case tokenValue:
		p.advance()
		p.skipComments()
		return p.parseNode()

	case tokenBlockEnd, tokenDocumentStart, tokenDocumentEnd, tokenStreamEnd:
		n = &node{kind: nodeScalar, value: "", pos: t.pos, implicit: true}

	default:
		return nil, &SyntaxError{
			Message: fmt.Sprintf("unexpected token %s", t),
			Pos:     t.pos,
		}
	}

	if n != nil {
		if anchor != "" {
			n.anchor = anchor
			p.anchors[anchor] = n
		}
		if tag != "" {
			resolved, err := p.resolveTag(tag)
			if err != nil {
				return nil, &SyntaxError{Message: err.Error(), Pos: t.pos}
			}
			n.tag = resolved
		}
	}

	return n, nil
}

func (p *parser) parseBlockMapping() (*node, error) {
	mapping := &node{
		kind: nodeMapping,
		pos:  p.peek().pos,
	}

	if p.peek().kind == tokenBlockMappingStart {
		p.advance()
	}

	p.skipComments()
	if p.peek().kind == tokenAnchor || p.peek().kind == tokenTag {
		saved := p.pos
		var mapAnchor, mapTag string
		for p.peek().kind == tokenAnchor || p.peek().kind == tokenTag {
			t := p.peek()
			if t.kind == tokenAnchor {
				mapAnchor = t.value
			} else {
				mapTag = t.value
			}
			p.advance()
			p.skipComments()
		}
		if p.peek().kind == tokenKey || p.peek().kind == tokenValue {
			if mapAnchor != "" {
				mapping.anchor = mapAnchor
				p.anchors[mapAnchor] = mapping
			}
			if mapTag != "" {
				resolved, err := p.resolveTag(mapTag)
				if err != nil {
					return nil, &SyntaxError{Message: err.Error(), Pos: mapping.pos}
				}
				mapping.tag = resolved
			}
		} else {
			p.pos = saved
		}
	}

	for {
		p.skipComments()
		t := p.peek()

		if t.kind == tokenBlockEnd || t.kind == tokenDocumentStart || t.kind == tokenDocumentEnd || t.kind == tokenStreamEnd {
			break
		}

		if t.kind == tokenKey {
			p.advance()
			p.skipComments()
			key, val, err := p.parseMappingEntry()
			if err != nil {
				return nil, err
			}
			mapping.children = append(mapping.children, key, val)
			continue
		}

		if t.kind == tokenValue {
			key := &node{kind: nodeScalar, value: "", pos: t.pos, implicit: true}
			p.advance()
			p.skipComments()
			var val *node
			vk := p.peek().kind
			if vk == tokenBlockEnd || vk == tokenKey || vk == tokenDocumentStart ||
				vk == tokenDocumentEnd || vk == tokenStreamEnd || vk == tokenValue {
				val = &node{kind: nodeScalar, value: "", pos: t.pos, implicit: true}
			} else {
				var parseErr error
				val, parseErr = p.parseNode()
				if parseErr != nil {
					return nil, parseErr
				}
			}
			if val == nil {
				val = &node{kind: nodeScalar, value: "", pos: t.pos, implicit: true}
			}
			mapping.children = append(mapping.children, key, val)
			continue
		}

		if t.kind == tokenScalar || t.kind == tokenAnchor || t.kind == tokenAlias || t.kind == tokenTag {
			hasValue := false
			for j := p.pos; j < len(p.tokens); j++ {
				jk := p.tokens[j].kind
				if jk == tokenValue {
					hasValue = true
					break
				}
				if jk == tokenKey || jk == tokenBlockEnd || jk == tokenBlockMappingStart ||
					jk == tokenBlockSequenceStart || jk == tokenDocumentStart ||
					jk == tokenDocumentEnd || jk == tokenStreamEnd {
					break
				}
			}
			if !hasValue {
				return nil, &SyntaxError{Message: "invalid content in block mapping (missing ':')", Pos: t.pos}
			}
			key, val, err := p.parseMappingEntry()
			if err != nil {
				return nil, err
			}
			mapping.children = append(mapping.children, key, val)
			continue
		}

		break
	}

	if p.peek().kind == tokenBlockEnd {
		p.advance()
	}

	return mapping, nil
}

func (p *parser) parseMappingEntry() (*node, *node, error) {
	var key *node
	var err error
	if p.peek().kind == tokenValue {
		key = &node{kind: nodeScalar, value: "", pos: p.peek().pos, implicit: true}
	} else {
		key, err = p.parseNode()
		if err != nil {
			return nil, nil, err
		}
	}

	p.skipComments()

	if p.peek().kind == tokenValue {
		p.advance()
		p.skipComments()
	}

	var value *node
	tk := p.peek().kind
	if tk == tokenBlockEnd || tk == tokenKey || tk == tokenDocumentStart ||
		tk == tokenDocumentEnd || tk == tokenStreamEnd {
		value = &node{kind: nodeScalar, value: "", pos: key.pos, implicit: true}
	} else {
		value, err = p.parseNode()
		if err != nil {
			return nil, nil, err
		}
	}

	if value == nil {
		value = &node{kind: nodeScalar, value: "", pos: key.pos, implicit: true}
	}

	return key, value, nil
}

func (p *parser) parseBlockSequence() (*node, error) {
	seq := &node{
		kind: nodeSequence,
		pos:  p.peek().pos,
	}

	if p.peek().kind == tokenBlockSequenceStart {
		p.advance()
	}

	for {
		p.skipComments()
		if p.peek().kind != tokenBlockEntry {
			break
		}
		p.advance()
		p.skipComments()

		t := p.peek()
		if t.kind == tokenBlockEntry || t.kind == tokenBlockEnd ||
			t.kind == tokenDocumentStart || t.kind == tokenDocumentEnd ||
			t.kind == tokenStreamEnd {
			seq.children = append(seq.children, &node{kind: nodeScalar, value: "", pos: t.pos, implicit: true})
			continue
		}

		savedPos := p.pos
		var peekAnchor, peekTag string
		for {
			pk := p.peek()
			if pk.kind == tokenAnchor {
				peekAnchor = pk.value
				p.advance()
				p.skipComments()
			} else if pk.kind == tokenTag {
				peekTag = pk.value
				p.advance()
				p.skipComments()
			} else {
				break
			}
		}
		if (peekAnchor != "" || peekTag != "") && (p.peek().kind == tokenBlockEntry || p.peek().kind == tokenBlockEnd ||
			p.peek().kind == tokenDocumentStart || p.peek().kind == tokenDocumentEnd || p.peek().kind == tokenStreamEnd) {
			empty := &node{kind: nodeScalar, value: "", pos: t.pos, implicit: true}
			if peekAnchor != "" {
				empty.anchor = peekAnchor
				p.anchors[peekAnchor] = empty
			}
			if peekTag != "" {
				resolved, err := p.resolveTag(peekTag)
				if err != nil {
					return nil, &SyntaxError{Message: err.Error(), Pos: t.pos}
				}
				empty.tag = resolved
			}
			seq.children = append(seq.children, empty)
			continue
		}
		p.pos = savedPos

		item, err := p.parseNode()
		if err != nil {
			return nil, err
		}
		if item != nil {
			seq.children = append(seq.children, item)
		}
	}

	if p.peek().kind == tokenBlockEnd {
		p.advance()
	}

	return seq, nil
}

func (p *parser) parseFlowMapping() (*node, error) {
	mapping := &node{
		kind: nodeMapping,
		pos:  p.peek().pos,
		flow: true,
	}

	p.advance()
	p.skipComments()

	for p.peek().kind != tokenFlowMappingEnd {
		if p.peek().kind == tokenStreamEnd || p.peek().kind == tokenBlockEnd {
			return nil, &SyntaxError{Message: "unterminated flow mapping", Pos: mapping.pos}
		}

		p.skipComments()

		explicitKey := false
		if p.peek().kind == tokenKey {
			explicitKey = true
			p.advance()
			p.skipComments()
			if p.peek().kind == tokenKey {
				p.advance()
				p.skipComments()
			}
		}

		var key *node
		var err error
		if p.peek().kind == tokenValue || p.peek().kind == tokenFlowMappingEnd || (explicitKey && p.peek().kind == tokenFlowEntry) {
			key = &node{kind: nodeScalar, value: "", pos: p.peek().pos, implicit: true}
		} else {
			key, err = p.parseNode()
			if err != nil {
				return nil, err
			}
		}

		p.skipComments()
		var value *node
		if p.peek().kind == tokenValue {
			p.advance()
			p.skipComments()
			if p.peek().kind != tokenFlowMappingEnd && p.peek().kind != tokenFlowEntry {
				value, err = p.parseNode()
				if err != nil {
					return nil, err
				}
			}
		}
		if value == nil {
			value = &node{kind: nodeScalar, value: "", pos: key.pos, implicit: true}
		}

		mapping.children = append(mapping.children, key, value)

		p.skipComments()
		if p.peek().kind == tokenFlowEntry {
			p.advance()
			p.skipComments()
		} else if p.peek().kind != tokenFlowMappingEnd && p.peek().kind != tokenStreamEnd && p.peek().kind != tokenBlockEnd {
			return nil, &SyntaxError{Message: "missing comma between flow mapping entries", Pos: p.peek().pos}
		}
	}

	p.advance()
	return mapping, nil
}

func (p *parser) parseFlowSequence() (*node, error) {
	seq := &node{
		kind: nodeSequence,
		pos:  p.peek().pos,
		flow: true,
	}

	p.advance()
	p.skipComments()

	for p.peek().kind != tokenFlowSequenceEnd {
		if p.peek().kind == tokenStreamEnd || p.peek().kind == tokenBlockEnd {
			return nil, &SyntaxError{Message: "unterminated flow sequence", Pos: seq.pos}
		}

		p.skipComments()

		if p.peek().kind == tokenKey {
			keyPos := p.peek().pos
			p.advance()
			p.skipComments()

			var key *node
			if p.peek().kind == tokenValue {
				key = &node{kind: nodeScalar, value: "", pos: keyPos, implicit: true}
			} else {
				var err error
				key, err = p.parseNode()
				if err != nil {
					return nil, err
				}
			}
			p.skipComments()

			if p.peek().kind == tokenValue {
				p.advance()
				p.skipComments()
				var val *node
				if p.peek().kind != tokenFlowMappingEnd && p.peek().kind != tokenFlowEntry &&
					p.peek().kind != tokenFlowSequenceEnd {
					var err error
					val, err = p.parseNode()
					if err != nil {
						return nil, err
					}
				}
				if val == nil {
					val = &node{kind: nodeScalar, value: "", pos: key.pos, implicit: true}
				}
				pair := &node{kind: nodeMapping, pos: key.pos, flow: true}
				pair.children = append(pair.children, key, val)
				seq.children = append(seq.children, pair)
			} else {
				seq.children = append(seq.children, key)
			}
		} else {
			item, err := p.parseNode()
			if err != nil {
				return nil, err
			}
			if item != nil {
				seq.children = append(seq.children, item)
			}
		}

		p.skipComments()
		if p.peek().kind == tokenFlowEntry {
			p.advance()
			p.skipComments()
		} else if p.peek().kind != tokenFlowSequenceEnd && p.peek().kind != tokenStreamEnd && p.peek().kind != tokenBlockEnd {
			return nil, &SyntaxError{Message: "missing comma between flow sequence entries", Pos: p.peek().pos}
		}
	}

	p.advance()
	return seq, nil
}

func (p *parser) skipComments() {
	for p.peek().kind == tokenComment {
		p.advance()
	}
}

func (p *parser) collectComments() string {
	var comments []string
	for p.peek().kind == tokenComment {
		comments = append(comments, p.peek().value)
		p.advance()
	}
	if len(comments) == 0 {
		return ""
	}
	return strings.Join(comments, "\n")
}

func (p *parser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{kind: tokenStreamEnd}
	}
	return p.tokens[p.pos]
}

func (p *parser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}
