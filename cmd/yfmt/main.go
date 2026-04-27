package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/go-rotini/yaml"
)

func main() {
	check := flag.Bool("check", false, "check formatting without writing (exit 1 if unformatted)")
	write := flag.Bool("write", false, "write result to source file instead of stdout")
	indent := flag.Int("indent", 2, "indentation width")
	flow := flag.Bool("flow", false, "use flow style for sequences and mappings")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: yfmt [flags] [file ...]\n\nFormats YAML files via parse and re-emit.\nWith no files, reads from stdin.\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *check && *write {
		fmt.Fprintln(os.Stderr, "yfmt: --check and --write are mutually exclusive")
		os.Exit(2)
	}

	var opts []yaml.EncodeOption
	opts = append(opts, yaml.WithIndent(*indent))
	if *flow {
		opts = append(opts, yaml.WithFlow(true))
	}

	files := flag.Args()
	if len(files) == 0 {
		if err := processStream(os.Stdin, os.Stdout, opts); err != nil {
			fmt.Fprintf(os.Stderr, "yfmt: %v\n", err)
			os.Exit(1)
		}
		return
	}

	unformatted := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "yfmt: %v\n", err)
			os.Exit(1)
		}

		var buf bytes.Buffer
		if err := processBytes(data, &buf, opts); err != nil {
			fmt.Fprintf(os.Stderr, "yfmt: %s: %v\n", path, err)
			os.Exit(1)
		}

		formatted := buf.Bytes()

		if *check {
			if !bytes.Equal(data, formatted) {
				fmt.Println(path)
				unformatted++
			}
			continue
		}

		if *write {
			if !bytes.Equal(data, formatted) {
				if err := os.WriteFile(path, formatted, 0o600); err != nil {
					fmt.Fprintf(os.Stderr, "yfmt: %v\n", err)
					os.Exit(1)
				}
			}
			continue
		}

		if _, err := os.Stdout.Write(formatted); err != nil {
			fmt.Fprintf(os.Stderr, "yfmt: %v\n", err)
			os.Exit(1)
		}
	}

	if *check && unformatted > 0 {
		os.Exit(1)
	}
}

func processStream(r io.Reader, w io.Writer, opts []yaml.EncodeOption) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return processBytes(data, w, opts)
}

func processBytes(data []byte, w io.Writer, opts []yaml.EncodeOption) error {
	file, err := yaml.Parse(data)
	if err != nil {
		return err
	}

	for _, doc := range file.Docs {
		out, err := yaml.NodeToBytesWithOptions(doc, opts...)
		if err != nil {
			return err
		}
		if _, err := w.Write(out); err != nil {
			return err
		}
	}
	return nil
}
