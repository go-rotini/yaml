package yaml

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type nodeKind int

const (
	nodeDocument nodeKind = iota
	nodeMapping
	nodeSequence
	nodeScalar
	nodeAlias
	nodeMergeKey
)

type node struct {
	kind             nodeKind
	pos              Position
	tag              string
	anchor           string
	alias            string
	ref              *anchorSlot // the anchor an alias refers to, nil if not defined before it in its document
	value            string
	style            scalarStyle
	children         []*node
	implicit         bool
	flow             bool
	docStartExplicit bool
	docEndExplicit   bool
	headComment      string
	lineComment      string
	footComment      string
}

// NodeKind identifies the type of a YAML [Node] in the AST.
type NodeKind int

const (
	DocumentNode NodeKind = iota // a YAML document (--- ... ---)
	MappingNode                  // a mapping (key: value pairs)
	SequenceNode                 // a sequence (- items)
	ScalarNode                   // a scalar value (string, number, bool, null)
	AliasNode                    // an alias reference (*name)
)

// ScalarStyle indicates how a scalar was (or should be) represented in YAML.
type ScalarStyle int

const (
	PlainStyle        ScalarStyle = iota // unquoted (foo)
	SingleQuotedStyle                    // single-quoted ('foo')
	DoubleQuotedStyle                    // double-quoted ("foo")
	LiteralStyle                         // literal block (|)
	FoldedStyle                          // folded block (>)
)

// Node is a YAML AST node. For mappings, Children alternates key-value pairs
// (children[0] is the first key, children[1] its value, etc.). For sequences,
// each child is a list element. For documents, Children holds the root node.
type Node struct {
	Kind          NodeKind
	Tag           string
	Anchor        string
	Alias         string
	Value         string
	Style         ScalarStyle
	Flow          bool // flow style ({} or []) for mappings and sequences
	ExplicitStart bool // explicit document start marker (---)
	ExplicitEnd   bool // explicit document end marker (...)
	MergeKey      bool // node represents a merge key (<<)
	Children      []*Node
	Pos           Position
	Comment       string // line comment, on the same line as the node
	HeadComment   string
	FootComment   string
}

// File is the result of parsing a YAML byte stream. It contains one [Node]
// per document in the stream.
type File struct {
	Docs []*Node
	// Warnings lists non-fatal problems found while parsing, such as
	// unknown directives.
	Warnings []string
}

// Parse tokenizes and parses data into an AST. The returned [File] provides
// direct access to the document tree without decoding into Go values.
func Parse(data []byte) (*File, error) {
	data, err := detectAndConvertEncoding(data)
	if err != nil {
		return nil, err
	}

	tokens, err := newScanner(data).scan()
	if err != nil {
		return nil, err
	}

	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		return nil, err
	}

	file := &File{Warnings: p.warnings}
	for _, doc := range docs {
		file.Docs = append(file.Docs, exportNode(doc))
	}
	return file, nil
}

func exportNode(n *node) *Node {
	if n == nil {
		return nil
	}
	pub := &Node{
		Kind:          exportKind(n.kind),
		Tag:           n.tag,
		Anchor:        n.anchor,
		Alias:         n.alias,
		Value:         n.value,
		Style:         exportStyle(n.style),
		Flow:          n.flow,
		ExplicitStart: n.docStartExplicit,
		ExplicitEnd:   n.docEndExplicit,
		MergeKey:      n.kind == nodeMergeKey,
		Pos:           n.pos,
		HeadComment:   n.headComment,
		Comment:       n.lineComment,
		FootComment:   n.footComment,
	}
	for _, child := range n.children {
		pub.Children = append(pub.Children, exportNode(child))
	}
	return pub
}

func exportKind(k nodeKind) NodeKind {
	switch k {
	case nodeDocument:
		return DocumentNode
	case nodeMapping:
		return MappingNode
	case nodeSequence:
		return SequenceNode
	case nodeScalar:
		return ScalarNode
	case nodeAlias:
		return AliasNode
	default:
		return ScalarNode
	}
}

func exportStyle(s scalarStyle) ScalarStyle {
	switch s {
	case scalarPlain:
		return PlainStyle
	case scalarSingleQuoted:
		return SingleQuotedStyle
	case scalarDoubleQuoted:
		return DoubleQuotedStyle
	case scalarLiteral:
		return LiteralStyle
	case scalarFolded:
		return FoldedStyle
	default:
		return PlainStyle
	}
}

// importNode converts the public tree n to the internal form, allocating
// every node of the tree at once.
func importNode(n *Node) *node {
	if n == nil {
		return nil
	}
	slab := make([]node, countNodes(n))
	return importInto(n, &slab)
}

// countNodes returns the number of non-nil nodes in the tree n.
func countNodes(n *Node) int {
	if n == nil {
		return 0
	}
	c := 1
	for _, child := range n.Children {
		c += countNodes(child)
	}
	return c
}

// importInto converts n using the next unused node of slab.
func importInto(n *Node, slab *[]node) *node {
	if n == nil {
		return nil
	}
	k := importKind(n.Kind)
	if n.MergeKey {
		k = nodeMergeKey
	}
	internal := &(*slab)[0]
	*slab = (*slab)[1:]
	*internal = node{
		kind:             k,
		tag:              n.Tag,
		anchor:           n.Anchor,
		alias:            n.Alias,
		value:            n.Value,
		style:            importStyle(n.Style),
		flow:             n.Flow,
		docStartExplicit: n.ExplicitStart,
		docEndExplicit:   n.ExplicitEnd,
		pos:              n.Pos,
	}
	if len(n.Children) > 0 {
		internal.children = make([]*node, len(n.Children))
		for i, child := range n.Children {
			internal.children[i] = importInto(child, slab)
		}
	}
	return internal
}

func importKind(k NodeKind) nodeKind {
	switch k {
	case DocumentNode:
		return nodeDocument
	case MappingNode:
		return nodeMapping
	case SequenceNode:
		return nodeSequence
	case ScalarNode:
		return nodeScalar
	case AliasNode:
		return nodeAlias
	default:
		return nodeScalar
	}
}

func importStyle(s ScalarStyle) scalarStyle {
	switch s {
	case PlainStyle:
		return scalarPlain
	case SingleQuotedStyle:
		return scalarSingleQuoted
	case DoubleQuotedStyle:
		return scalarDoubleQuoted
	case LiteralStyle:
		return scalarLiteral
	case FoldedStyle:
		return scalarFolded
	default:
		return scalarPlain
	}
}

// String returns a short human-readable description of the node: the scalar
// value for scalars, a summary like "{mapping: 3 pairs}" for collections.
func (n *Node) String() string {
	switch n.Kind {
	case ScalarNode:
		return n.Value
	case MappingNode:
		return fmt.Sprintf("{mapping: %d pairs}", len(n.Children)/2)
	case SequenceNode:
		return fmt.Sprintf("[sequence: %d items]", len(n.Children))
	case DocumentNode:
		return "---"
	case AliasNode:
		return "*" + n.Alias
	}
	return ""
}

// Validate checks the structural consistency of the node tree rooted at n.
// It returns an error if a mapping has an odd number of children (missing
// value), if an alias node references an empty name, if an anchor or alias
// name holds a character a name cannot (white space, a flow indicator, a
// non-printable character or invalid UTF-8), or, wrapping
// [ErrUnsupported], if a node contains itself or the tree nests more than
// 10,000 levels deep.
func (n *Node) Validate() error {
	return checkTree(n, true)
}

// checkTree returns an error for a tree that cannot be written as YAML: one
// with a cycle, nested too deep, or with an anchor or alias name that does
// not read back. structure also checks mappings and empty alias names.
func checkTree(root *Node, structure bool) error {
	onPath := make(map[*Node]bool)
	var check func(n *Node, depth int) error
	check = func(n *Node, depth int) error {
		if n == nil {
			return nil
		}
		if onPath[n] {
			return fmt.Errorf("yaml: node at %s contains itself: %w", n.Pos, ErrUnsupported)
		}
		if depth > maxNestingDepth {
			return fmt.Errorf("yaml: node tree nested more than %d levels deep: %w", maxNestingDepth, ErrUnsupported)
		}
		switch {
		case structure && n.Kind == MappingNode && len(n.Children)%2 != 0:
			return fmt.Errorf("yaml: mapping at %s has odd number of children (%d): %w", n.Pos, len(n.Children), errOddChildren)
		case structure && n.Kind == AliasNode && n.Alias == "":
			return fmt.Errorf("yaml: alias at %s has %w", n.Pos, errEmptyAlias)
		case n.Anchor != "" && !validAnchorName(n.Anchor):
			return fmt.Errorf("yaml: anchor %q at %s: %w", n.Anchor, n.Pos, errInvalidAnchor)
		case n.Kind == AliasNode && !validAnchorName(n.Alias):
			return fmt.Errorf("yaml: alias %q at %s: %w", n.Alias, n.Pos, errInvalidAnchor)
		}
		onPath[n] = true
		for _, child := range n.Children {
			if err := check(child, depth+1); err != nil {
				return err
			}
		}
		delete(onPath, n)
		return nil
	}
	return check(root, 0)
}

// validAnchorName reports whether name can be written after & or * and
// read back whole: it is not empty and holds no white space, line break,
// flow indicator, non-printable character or invalid UTF-8.
func validAnchorName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if !isPrintableYAML(r) || strings.ContainsRune(" \t\n\r,[]{}", r) {
			return false
		}
	}
	return true
}

// WalkFunc is the callback for [Walk]. Return true to recurse into the
// node's children, or false to skip the subtree.
type WalkFunc func(n *Node) bool

// Walk traverses the AST rooted at n in depth-first pre-order, calling fn
// for each node. If fn returns false, the node's children are not visited.
// A node that contains itself is not entered again below itself.
func Walk(n *Node, fn WalkFunc) {
	walk(n, fn, make(map[*Node]bool))
}

func walk(n *Node, fn WalkFunc, onPath map[*Node]bool) {
	if n == nil || onPath[n] {
		return
	}
	if !fn(n) {
		return
	}
	onPath[n] = true
	for _, child := range n.Children {
		walk(child, fn, onPath)
	}
	delete(onPath, n)
}

// Filter walks the AST rooted at n and returns all nodes for which fn
// returns true.
func Filter(n *Node, fn func(*Node) bool) []*Node {
	var result []*Node
	Walk(n, func(node *Node) bool {
		if fn(node) {
			result = append(result, node)
		}
		return true
	})
	return result
}

// NodeToBytes serializes a [Node] tree back into block-style YAML bytes using
// default encoding options. Structure, scalar values, anchors, aliases,
// merge keys and tags are written, and the output parses back to the same
// values and types: a scalar with a quoted [ScalarStyle] or a !!str tag
// reads back as a string, a plain scalar that reads as null, a bool or a
// number is written unquoted, and a mapping or sequence key uses the
// explicit "? key" form. A tag the written form already implies, such as
// !!int on a plain 5, is left out. Comments, flow style and the choice of
// quote character are not preserved. An anchor or alias name that would not
// read back is an error, and so is a node that contains itself or a tree
// nested more than 10,000 levels deep (wrapping [ErrUnsupported]).
func NodeToBytes(n *Node) ([]byte, error) {
	return NodeToBytesWithOptions(n)
}

// NodeToBytesWithOptions is like [NodeToBytes] but applies opts. Only the
// options that affect scalars and indentation apply; [WithKYAML] and
// [WithFlow] do not change the block-style output.
func NodeToBytesWithOptions(n *Node, opts ...EncodeOption) ([]byte, error) {
	o := defaultEncodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	if err := checkBlockIndent(&encoderOptions{indent: o.indent}); err != nil {
		return nil, err
	}
	if err := checkTree(n, false); err != nil {
		return nil, err
	}
	internal := importNode(n)
	enc := newEncoder(o)
	return enc.encodeNode(internal)
}
