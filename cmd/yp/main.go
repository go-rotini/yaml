package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/go-rotini/yaml"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: yp <path-expression> [file ...]\n\nEvaluates a YAMLPath expression against YAML files.\nWith no files, reads from stdin.\n\nExamples:\n  yp '$.name' config.yaml\n  yp '$.servers[0].host' config.yaml\n  yp '$.items[*].id' data.yaml\n  cat config.yaml | yp '$.database.port'\n")
	}
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(2)
	}

	expr := args[0]
	path, err := yaml.PathString(expr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "yp: invalid path %q: %v\n", expr, err)
		os.Exit(2)
	}

	files := args[1:]
	if len(files) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "yp: %v\n", err)
			os.Exit(1)
		}
		if err := evalPath(path, data); err != nil {
			fmt.Fprintf(os.Stderr, "yp: %v\n", err)
			os.Exit(1)
		}
		return
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "yp: %v\n", err)
			os.Exit(1)
		}
		if err := evalPath(path, data); err != nil {
			fmt.Fprintf(os.Stderr, "yp: %s: %v\n", f, err)
			os.Exit(1)
		}
	}
}

func evalPath(path *yaml.Path, data []byte) error {
	file, err := yaml.Parse(data)
	if err != nil {
		return err
	}

	for _, doc := range file.Docs {
		nodes, err := path.Read(doc)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			if err := printNode(n); err != nil {
				return err
			}
		}
	}
	return nil
}

func printNode(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		fmt.Println(n.Value)
	case yaml.MappingNode, yaml.SequenceNode, yaml.DocumentNode:
		out, err := yaml.NodeToBytes(n)
		if err != nil {
			return err
		}
		if _, err := os.Stdout.Write(out); err != nil {
			return err
		}
	case yaml.AliasNode:
		fmt.Printf("*%s\n", n.Alias)
	}
	return nil
}
