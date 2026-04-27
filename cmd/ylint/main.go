package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/go-rotini/yaml"
)

type diagnostic struct {
	file    string
	pos     yaml.Position
	message string
}

func (d diagnostic) String() string {
	if d.file != "" {
		return fmt.Sprintf("%s:%d:%d: %s", d.file, d.pos.Line, d.pos.Column, d.message)
	}
	return fmt.Sprintf("%d:%d: %s", d.pos.Line, d.pos.Column, d.message)
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: ylint [file ...]\n\nLints YAML files for common issues:\n  - duplicate mapping keys\n  - undefined alias references\n  - inconsistent indentation\n  - syntax errors\n\nWith no files, reads from stdin.\n")
	}
	flag.Parse()

	files := flag.Args()
	if len(files) == 0 {
		files = []string{"/dev/stdin"}
	}

	exitCode := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ylint: %v\n", err)
			exitCode = 1
			continue
		}

		name := path
		if path == "/dev/stdin" {
			name = "<stdin>"
		}

		diags := lint(name, data)
		for _, d := range diags {
			fmt.Println(d)
			exitCode = 1
		}
	}

	os.Exit(exitCode)
}

func lint(filename string, data []byte) []diagnostic {
	var diags []diagnostic

	file, err := yaml.Parse(data)
	if err != nil {
		diags = append(diags, diagnostic{
			file:    filename,
			pos:     posFromError(err),
			message: err.Error(),
		})
		return diags
	}

	for _, doc := range file.Docs {
		anchors := map[string]yaml.Position{}

		yaml.Walk(doc, func(n *yaml.Node) bool {
			if n.Anchor != "" {
				if prev, ok := anchors[n.Anchor]; ok {
					diags = append(diags, diagnostic{
						file:    filename,
						pos:     n.Pos,
						message: fmt.Sprintf("duplicate anchor %q (first defined at %d:%d)", n.Anchor, prev.Line, prev.Column),
					})
				} else {
					anchors[n.Anchor] = n.Pos
				}
			}

			if n.Kind == yaml.AliasNode {
				if _, ok := anchors[n.Alias]; !ok {
					diags = append(diags, diagnostic{
						file:    filename,
						pos:     n.Pos,
						message: fmt.Sprintf("undefined alias %q", n.Alias),
					})
				}
			}

			if n.Kind == yaml.MappingNode {
				diags = checkDuplicateKeys(filename, n, diags)
				diags = checkIndentation(filename, n, diags)
			}

			if n.Kind == yaml.SequenceNode {
				diags = checkIndentation(filename, n, diags)
			}

			return true
		})
	}

	return diags
}

func checkDuplicateKeys(filename string, n *yaml.Node, diags []diagnostic) []diagnostic {
	seen := map[string]yaml.Position{}
	for i := 0; i+1 < len(n.Children); i += 2 {
		key := n.Children[i]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		if prev, ok := seen[key.Value]; ok {
			diags = append(diags, diagnostic{
				file:    filename,
				pos:     key.Pos,
				message: fmt.Sprintf("duplicate key %q (first defined at %d:%d)", key.Value, prev.Line, prev.Column),
			})
		} else {
			seen[key.Value] = key.Pos
		}
	}
	return diags
}

func checkIndentation(filename string, n *yaml.Node, diags []diagnostic) []diagnostic {
	if n.Flow || len(n.Children) < 2 {
		return diags
	}

	var columns []int
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Children); i += 2 {
			child := n.Children[i]
			if child.Pos.Line > 0 && child.Pos.Column > 0 {
				columns = append(columns, child.Pos.Column)
			}
		}
	} else {
		for _, child := range n.Children {
			if child.Pos.Line > 0 && child.Pos.Column > 0 {
				columns = append(columns, child.Pos.Column)
			}
		}
	}

	if len(columns) < 2 {
		return diags
	}

	expected := columns[0]
	for i := 1; i < len(columns); i++ {
		if columns[i] != expected {
			var childNode *yaml.Node
			if n.Kind == yaml.MappingNode {
				childNode = n.Children[i*2]
			} else {
				childNode = n.Children[i]
			}
			diags = append(diags, diagnostic{
				file:    filename,
				pos:     childNode.Pos,
				message: fmt.Sprintf("inconsistent indentation: column %d, expected %d", columns[i], expected),
			})
			break
		}
	}

	return diags
}

func posFromError(err error) yaml.Position {
	var synErr *yaml.SyntaxError
	if errors.As(err, &synErr) {
		return synErr.Pos
	}
	return yaml.Position{Line: 1, Column: 1}
}
