package yaml_test

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/go-rotini/yaml"
)

func ExampleMarshal() {
	type Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	}
	v := Server{Host: "localhost", Port: 8080}
	data, err := yaml.Marshal(v)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(data))
	// Output:
	// host: localhost
	// port: 8080
}

func ExampleUnmarshal() {
	data := []byte("name: app\nport: 3000\n")
	var v map[string]any
	if err := yaml.Unmarshal(data, &v); err != nil {
		panic(err)
	}
	fmt.Println(v["name"])
	fmt.Println(v["port"])
	// Output:
	// app
	// 3000
}

func ExampleMarshalWithOptions() {
	v := map[string][]int{"nums": {1, 2, 3}}
	data, err := yaml.MarshalWithOptions(v, yaml.WithFlow(true))
	if err != nil {
		panic(err)
	}
	fmt.Print(string(data))
	// Output:
	// {nums: [1, 2, 3]}
}

func ExampleNewEncoder() {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.Encode(map[string]string{"a": "1"})
	enc.Encode(map[string]string{"b": "2"})
	enc.Close()
	fmt.Print(buf.String())
	// Output:
	// a: "1"
	// ---
	// b: "2"
}

func ExampleNewDecoder() {
	data := "---\nhello: world\n---\nfoo: bar\n"
	dec := yaml.NewDecoder(bytes.NewReader([]byte(data)))
	for {
		var v map[string]string
		err := dec.Decode(&v)
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		fmt.Println(v)
	}
	// Output:
	// map[hello:world]
	// map[foo:bar]
}

func ExampleValid() {
	fmt.Println(yaml.Valid([]byte("key: value")))
	fmt.Println(yaml.Valid([]byte("key: [invalid")))
	// Output:
	// true
	// false
}

func ExampleToJSON() {
	data, err := yaml.ToJSON([]byte("name: test\ncount: 42\n"))
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))
	// Output:
	// {"count":42,"name":"test"}
}

func ExampleFromJSON() {
	data, err := yaml.FromJSON([]byte(`{"name":"test","port":8080}`))
	if err != nil {
		panic(err)
	}
	fmt.Print(string(data))
	// Output:
	// name: test
	// port: 8080
}

func ExampleParse() {
	data := []byte("name: hello\nitems:\n  - a\n  - b\n")
	file, err := yaml.Parse(data)
	if err != nil {
		panic(err)
	}
	for _, doc := range file.Docs {
		yaml.Walk(doc, func(n *yaml.Node) bool {
			if n.Kind == yaml.ScalarNode {
				fmt.Println(n.Value)
			}
			return true
		})
	}
	// Output:
	// name
	// hello
	// items
	// a
	// b
}

func ExamplePathString() {
	path, err := yaml.PathString("$.name")
	if err != nil {
		panic(err)
	}
	data := []byte("name: world\nport: 80\n")
	result, err := path.ReadString(data)
	if err != nil {
		panic(err)
	}
	fmt.Println(result)
	// Output:
	// world
}

func ExampleFormatError() {
	data := []byte("key: [bad\n")
	var v any
	err := yaml.Unmarshal(data, &v)
	if err != nil {
		formatted := yaml.FormatError(data, err)
		fmt.Println(formatted)
	}
}

func ExampleUnmarshalWithOptions_strict() {
	type Config struct {
		Name string `yaml:"name"`
	}
	data := []byte("name: test\nunknown: field\n")
	var cfg Config
	err := yaml.UnmarshalWithOptions(data, &cfg, yaml.WithStrict())
	fmt.Println(err != nil)
	// Output:
	// true
}

// Prevent unused import errors.
var _ = os.Stdin
