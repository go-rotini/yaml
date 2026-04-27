package yaml

import (
	"fmt"
	"strconv"
	"strings"
)

// Path is a compiled JSONPath-like expression for querying and mutating a
// YAML [Node] tree. Create one with [PathString].
type Path struct {
	segments []pathSegment
}

type pathSegment interface {
	match(n *Node) []*Node
}

type rootSegment struct{}
type childSegment struct{ name string }
type indexSegment struct{ idx int }
type wildcardSegment struct{}
type recursiveSegment struct{}

func (rootSegment) match(n *Node) []*Node { return []*Node{n} }

func (s childSegment) match(n *Node) []*Node {
	if n.Kind != MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Children); i += 2 {
		if n.Children[i].Value == s.name {
			return []*Node{n.Children[i+1]}
		}
	}
	return nil
}

func (s indexSegment) match(n *Node) []*Node {
	if n.Kind != SequenceNode {
		return nil
	}
	idx := s.idx
	if idx < 0 {
		idx = len(n.Children) + idx
	}
	if idx < 0 || idx >= len(n.Children) {
		return nil
	}
	return []*Node{n.Children[idx]}
}

func (wildcardSegment) match(n *Node) []*Node {
	switch n.Kind {
	case MappingNode:
		var result []*Node
		for i := 1; i < len(n.Children); i += 2 {
			result = append(result, n.Children[i])
		}
		return result
	case SequenceNode:
		return n.Children
	}
	return nil
}

func (recursiveSegment) match(n *Node) []*Node {
	var result []*Node
	Walk(n, func(node *Node) bool {
		result = append(result, node)
		return true
	})
	return result
}

// PathString compiles a JSONPath-like expression into a [Path].
//
// Supported syntax:
//   - $ — root node
//   - .key — child mapping key
//   - [n] — sequence index (negative indexes count from the end)
//   - .* or [*] — wildcard (all children)
//   - .. — recursive descent
//
// Example: "$.servers[0].host" selects the host field of the first server.
func PathString(expr string) (*Path, error) {
	if expr == "" {
		return nil, fmt.Errorf("yaml: empty path expression")
	}

	p := &Path{}
	expr = strings.TrimSpace(expr)

	if !strings.HasPrefix(expr, "$") {
		return nil, fmt.Errorf("yaml: path must start with $")
	}
	p.segments = append(p.segments, rootSegment{})
	expr = expr[1:]

	for len(expr) > 0 {
		switch {
		case strings.HasPrefix(expr, ".."):
			p.segments = append(p.segments, recursiveSegment{})
			expr = expr[2:]

		case expr[0] == '.':
			expr = expr[1:]
			if len(expr) == 0 {
				return nil, fmt.Errorf("yaml: trailing dot in path")
			}
			if expr[0] == '*' {
				p.segments = append(p.segments, wildcardSegment{})
				expr = expr[1:]
			} else {
				end := strings.IndexAny(expr, ".[")
				if end == -1 {
					end = len(expr)
				}
				name := expr[:end]
				if name == "" {
					return nil, fmt.Errorf("yaml: empty field name in path")
				}
				p.segments = append(p.segments, childSegment{name: name})
				expr = expr[end:]
			}

		case expr[0] == '[':
			end := strings.IndexByte(expr, ']')
			if end == -1 {
				return nil, fmt.Errorf("yaml: unclosed bracket in path")
			}
			inner := expr[1:end]
			if inner == "*" {
				p.segments = append(p.segments, wildcardSegment{})
			} else {
				idx, err := strconv.Atoi(inner)
				if err != nil {
					return nil, fmt.Errorf("yaml: invalid index %q in path", inner)
				}
				p.segments = append(p.segments, indexSegment{idx: idx})
			}
			expr = expr[end+1:]

		default:
			return nil, fmt.Errorf("yaml: unexpected character %q in path", expr[0])
		}
	}

	return p, nil
}

// Read evaluates the path against the [Node] tree rooted at n and returns
// all matching nodes.
func (p *Path) Read(n *Node) ([]*Node, error) {
	current := []*Node{n}

	for i, seg := range p.segments {
		if _, ok := seg.(rootSegment); ok {
			if n.Kind == DocumentNode && len(n.Children) > 0 {
				current = []*Node{n.Children[0]}
			}
			continue
		}

		var next []*Node
		for _, node := range current {
			if _, ok := seg.(recursiveSegment); ok {
				next = append(next, seg.match(node)...)
				if i+1 < len(p.segments) {
					nextSeg := p.segments[i+1]
					var filtered []*Node
					for _, candidate := range next {
						matches := nextSeg.match(candidate)
						if len(matches) > 0 {
							filtered = append(filtered, candidate)
						}
					}
					_ = filtered
				}
			} else {
				next = append(next, seg.match(node)...)
			}
		}
		current = next
	}

	return current, nil
}

// ReadString is a convenience that parses YAML data, evaluates the path
// against the first document, and returns the scalar Value of the first match.
func (p *Path) ReadString(data []byte) (string, error) {
	file, err := Parse(data)
	if err != nil {
		return "", err
	}
	if len(file.Docs) == 0 {
		return "", fmt.Errorf("yaml: no documents")
	}
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "", fmt.Errorf("yaml: path not found")
	}
	return nodes[0].Value, nil
}

// Replace finds all nodes matching the path within the tree rooted at n and
// replaces each with replacement. The path must have at least two segments
// (root + child).
func (p *Path) Replace(n *Node, replacement *Node) error {
	if len(p.segments) < 2 {
		return fmt.Errorf("yaml: path too short for replace")
	}

	parentSegs := p.segments[:len(p.segments)-1]
	lastSeg := p.segments[len(p.segments)-1]

	parentPath := &Path{segments: parentSegs}
	parents, err := parentPath.Read(n)
	if err != nil {
		return err
	}

	for _, parent := range parents {
		switch seg := lastSeg.(type) {
		case childSegment:
			if parent.Kind == MappingNode {
				for i := 0; i+1 < len(parent.Children); i += 2 {
					if parent.Children[i].Value == seg.name {
						parent.Children[i+1] = replacement
					}
				}
			}
		case indexSegment:
			if parent.Kind == SequenceNode {
				idx := seg.idx
				if idx < 0 {
					idx = len(parent.Children) + idx
				}
				if idx >= 0 && idx < len(parent.Children) {
					parent.Children[idx] = replacement
				}
			}
		}
	}

	return nil
}

// Append adds value as a new child to each [SequenceNode] matched by the
// path within the tree rooted at n. Non-sequence matches are ignored.
func (p *Path) Append(n *Node, value *Node) error {
	nodes, err := p.Read(n)
	if err != nil {
		return err
	}
	for _, target := range nodes {
		if target.Kind == SequenceNode {
			target.Children = append(target.Children, value)
		}
	}
	return nil
}

// Delete removes all nodes matching the path from the tree rooted at n.
// For mappings, both the key and value are removed. The path must have at
// least two segments (root + child).
func (p *Path) Delete(n *Node) error {
	if len(p.segments) < 2 {
		return fmt.Errorf("yaml: path too short for delete")
	}

	parentSegs := p.segments[:len(p.segments)-1]
	lastSeg := p.segments[len(p.segments)-1]

	parentPath := &Path{segments: parentSegs}
	parents, err := parentPath.Read(n)
	if err != nil {
		return err
	}

	for _, parent := range parents {
		switch seg := lastSeg.(type) {
		case childSegment:
			if parent.Kind == MappingNode {
				for i := 0; i+1 < len(parent.Children); i += 2 {
					if parent.Children[i].Value == seg.name {
						parent.Children = append(parent.Children[:i], parent.Children[i+2:]...)
						break
					}
				}
			}
		case indexSegment:
			if parent.Kind == SequenceNode {
				idx := seg.idx
				if idx < 0 {
					idx = len(parent.Children) + idx
				}
				if idx >= 0 && idx < len(parent.Children) {
					parent.Children = append(parent.Children[:idx], parent.Children[idx+1:]...)
				}
			}
		}
	}

	return nil
}

// String returns the canonical string representation of the path expression.
func (p *Path) String() string {
	var buf strings.Builder
	for _, seg := range p.segments {
		switch s := seg.(type) {
		case rootSegment:
			buf.WriteByte('$')
		case childSegment:
			buf.WriteByte('.')
			buf.WriteString(s.name)
		case indexSegment:
			fmt.Fprintf(&buf, "[%d]", s.idx)
		case wildcardSegment:
			buf.WriteString(".*")
		case recursiveSegment:
			buf.WriteString("..")
		}
	}
	return buf.String()
}
