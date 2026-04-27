package yaml

import "fmt"

type nodeKind int

const (
	nodeDocument nodeKind = iota
	nodeMapping
	nodeSequence
	nodeScalar
	nodeAnchor
	nodeAlias
	nodeMergeKey
)

type node struct {
	kind             nodeKind
	pos              Position
	tag              string
	anchor           string
	alias            string
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
	Comment       string
	HeadComment   string
	FootComment   string
}

// File is the result of parsing a YAML byte stream. It contains one [Node]
// per document in the stream.
type File struct {
	Docs []*Node
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

	file := &File{}
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

func importNode(n *Node) *node {
	if n == nil {
		return nil
	}
	k := importKind(n.Kind)
	if n.MergeKey {
		k = nodeMergeKey
	}
	internal := &node{
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
	for _, child := range n.Children {
		internal.children = append(internal.children, importNode(child))
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

// WalkFunc is the callback for [Walk]. Return true to recurse into the
// node's children, or false to skip the subtree.
type WalkFunc func(n *Node) bool

// Walk traverses the AST rooted at n in depth-first pre-order, calling fn
// for each node. If fn returns false, the node's children are not visited.
func Walk(n *Node, fn WalkFunc) {
	if n == nil {
		return
	}
	if !fn(n) {
		return
	}
	for _, child := range n.Children {
		Walk(child, fn)
	}
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

// NodeToBytes serializes a [Node] tree back into YAML bytes using default
// encoding options.
func NodeToBytes(n *Node) ([]byte, error) {
	internal := importNode(n)
	enc := newEncoder(defaultEncodeOptions())
	return enc.encodeNode(internal)
}

// NodeToBytesWithOptions serializes a [Node] tree back into YAML bytes using
// the provided encoding options.
func NodeToBytesWithOptions(n *Node, opts ...EncodeOption) ([]byte, error) {
	o := defaultEncodeOptions()
	for _, opt := range opts {
		opt(o)
	}
	internal := importNode(n)
	enc := newEncoder(o)
	return enc.encodeNode(internal)
}
