package yaml_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

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

func ExampleUnmarshalTo() {
	type Config struct {
		Name string `yaml:"name"`
		Port int    `yaml:"port"`
	}
	cfg, err := yaml.UnmarshalTo[Config]([]byte("name: api\nport: 9090\n"))
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Name, cfg.Port)
	// Output:
	// api 9090
}

func ExampleUnmarshalWithOptions() {
	data := []byte("name: test\n")
	var v map[string]any
	if err := yaml.UnmarshalWithOptions(data, &v, yaml.WithSchema(yaml.FailsafeSchema)); err != nil {
		panic(err)
	}
	fmt.Println(v["name"])
	// Output:
	// test
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

func ExampleNewEncoder() {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	if err := enc.Encode(map[string]string{"a": "1"}); err != nil {
		panic(err)
	}
	if err := enc.Encode(map[string]string{"b": "2"}); err != nil {
		panic(err)
	}
	if err := enc.Close(); err != nil {
		panic(err)
	}
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

func ExampleFormatError() {
	data := []byte("key: [bad\n")
	var v any
	err := yaml.Unmarshal(data, &v)
	if err != nil {
		formatted := yaml.FormatError(data, err)
		fmt.Print(formatted)
	}
}

// --- Parse / AST ---

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

func ExampleWalk() {
	file, _ := yaml.Parse([]byte("a: 1\nb: 2\n"))
	var keys []string
	yaml.Walk(file.Docs[0], func(n *yaml.Node) bool {
		if n.Kind == yaml.MappingNode {
			for i := 0; i < len(n.Children)-1; i += 2 {
				keys = append(keys, n.Children[i].Value)
			}
		}
		return true
	})
	fmt.Println(keys)
	// Output:
	// [a b]
}

func ExampleFilter() {
	file, _ := yaml.Parse([]byte("a: 1\nb: hello\nc: 3\n"))
	scalars := yaml.Filter(file.Docs[0], func(n *yaml.Node) bool {
		return n.Kind == yaml.ScalarNode
	})
	for _, s := range scalars {
		fmt.Println(s.Value)
	}
	// Output:
	// a
	// 1
	// b
	// hello
	// c
	// 3
}

func ExampleNodeToBytes() {
	file, _ := yaml.Parse([]byte("name: hello\n"))
	out, err := yaml.NodeToBytes(file.Docs[0])
	if err != nil {
		panic(err)
	}
	fmt.Print(string(out))
	// Output:
	// name: hello
}

func ExampleNodeToBytesWithOptions() {
	doc := &yaml.Node{
		Kind: yaml.DocumentNode,
		Children: []*yaml.Node{{
			Kind: yaml.MappingNode,
			Children: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "name"},
				{Kind: yaml.ScalarNode, Value: "hello"},
			},
		}},
	}
	out, err := yaml.NodeToBytesWithOptions(doc, yaml.WithSingleQuote(true))
	if err != nil {
		panic(err)
	}
	fmt.Print(string(out))
	// Output:
	// name: hello
}

func ExampleNode_Validate() {
	file, _ := yaml.Parse([]byte("a: 1\nb: 2\n"))
	err := file.Docs[0].Validate()
	fmt.Println(err)
	// Output:
	// <nil>
}

// --- Path ---

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

func ExamplePath_Read() {
	file, _ := yaml.Parse([]byte("servers:\n  - host: a\n  - host: b\n"))
	path, _ := yaml.PathString("$.servers[*].host")
	nodes, err := path.Read(file.Docs[0])
	if err != nil {
		panic(err)
	}
	for _, n := range nodes {
		fmt.Println(n.Value)
	}
	// Output:
	// a
	// b
}

func ExamplePath_ReadString() {
	path, _ := yaml.PathString("$.database.port")
	result, err := path.ReadString([]byte("database:\n  port: 5432\n"))
	if err != nil {
		panic(err)
	}
	fmt.Println(result)
	// Output:
	// 5432
}

func ExamplePath_ReadPositions() {
	file, _ := yaml.Parse([]byte("a: 1\nb: 2\nc: 3\n"))
	path, _ := yaml.PathString("$.b")
	positions, err := path.ReadPositions(file.Docs[0])
	if err != nil {
		panic(err)
	}
	for _, pos := range positions {
		fmt.Println(pos.Line)
	}
	// Output:
	// 2
}

func ExamplePath_Replace() {
	file, _ := yaml.Parse([]byte("name: old\n"))
	path, _ := yaml.PathString("$.name")
	replacement := &yaml.Node{Kind: yaml.ScalarNode, Value: "new"}
	if err := path.Replace(file.Docs[0], replacement); err != nil {
		panic(err)
	}
	out, _ := yaml.NodeToBytes(file.Docs[0])
	fmt.Print(string(out))
	// Output:
	// name: new
}

func ExamplePath_Append() {
	file, _ := yaml.Parse([]byte("items:\n- a\n- b\n"))
	path, _ := yaml.PathString("$.items")
	newItem := &yaml.Node{Kind: yaml.ScalarNode, Value: "c"}
	if err := path.Append(file.Docs[0], newItem); err != nil {
		panic(err)
	}
	nodes, _ := path.Read(file.Docs[0])
	for _, child := range nodes[0].Children {
		fmt.Println(child.Value)
	}
	// Output:
	// a
	// b
	// c
}

func ExamplePath_Delete() {
	file, _ := yaml.Parse([]byte("a: x\nb: y\nc: z\n"))
	path, _ := yaml.PathString("$.b")
	if err := path.Delete(file.Docs[0]); err != nil {
		panic(err)
	}
	out, _ := yaml.NodeToBytes(file.Docs[0])
	fmt.Print(string(out))
	// Output:
	// a: x
	// c: z
}

// --- Encode Options ---

func ExampleWithIndent() {
	v := []string{"a", "b", "c"}
	data, _ := yaml.MarshalWithOptions(v, yaml.WithIndent(4))
	fmt.Print(string(data))
	// Output:
	// - a
	// - b
	// - c
}

func ExampleWithFlow() {
	v := map[string][]string{"tags": {"go", "yaml"}}
	data, _ := yaml.MarshalWithOptions(v, yaml.WithFlow(true))
	fmt.Print(string(data))
	// Output:
	// {tags: [go, yaml]}
}

func ExampleWithJSON() {
	v := map[string]any{"name": "test", "active": true}
	data, _ := yaml.MarshalWithOptions(v, yaml.WithJSON(true))
	fmt.Print(string(data))
	// Output:
	// "active": true
	// "name": "test"
}

func ExampleWithLiteralStyle() {
	type Msg struct {
		Text string `yaml:"text"`
	}
	v := Msg{Text: "line1\nline2\n"}
	data, _ := yaml.MarshalWithOptions(v, yaml.WithLiteralStyle(true))
	fmt.Print(string(data))
	// Output:
	// text: |
	//     line1
	//     line2
}

func ExampleWithSingleQuote() {
	v := map[string]string{"name": "true"}
	data, _ := yaml.MarshalWithOptions(v, yaml.WithSingleQuote(true))
	fmt.Print(string(data))
	// Output:
	// name: 'true'
}

func ExampleWithOmitEmpty() {
	type Config struct {
		Name  string   `yaml:"name"`
		Tags  []string `yaml:"tags,omitempty"`
		Debug bool     `yaml:"debug,omitempty"`
	}
	data, _ := yaml.MarshalWithOptions(Config{Name: "app"}, yaml.WithOmitEmpty(true))
	fmt.Print(string(data))
	// Output:
	// name: app
}

func ExampleWithComment() {
	v := map[string]int{"port": 8080}
	comments := map[string][]yaml.Comment{
		"port": {{Position: yaml.LineCommentPos, Text: "server port"}},
	}
	data, _ := yaml.MarshalWithOptions(v, yaml.WithComment(comments))
	fmt.Print(string(data))
	// Output:
	// port: 8080 # server port
}

func ExampleWithAutoInt() {
	v := map[string]float64{"count": 42.0}
	data, _ := yaml.MarshalWithOptions(v, yaml.WithAutoInt(true))
	fmt.Print(string(data))
	// Output:
	// count: 42
}

// --- Decode Options ---

func ExampleWithStrict() {
	type Config struct {
		Name string `yaml:"name"`
	}
	var cfg Config
	err := yaml.UnmarshalWithOptions([]byte("name: ok\nextra: bad\n"), &cfg, yaml.WithStrict())
	fmt.Println(errors.Is(err, yaml.ErrUnknownField))
	// Output:
	// true
}

func ExampleWithDisallowDuplicateKey() {
	var v map[string]int
	err := yaml.UnmarshalWithOptions([]byte("a: 1\na: 2\n"), &v, yaml.WithDisallowDuplicateKey())
	fmt.Println(errors.Is(err, yaml.ErrDuplicateKey))
	// Output:
	// true
}

func ExampleWithOrderedMap() {
	var v any
	if err := yaml.UnmarshalWithOptions([]byte("b: 2\na: 1\n"), &v, yaml.WithOrderedMap()); err != nil {
		panic(err)
	}
	ms := v.(yaml.MapSlice)
	for _, item := range ms {
		fmt.Printf("%s=%v\n", item.Key, item.Value)
	}
	// Output:
	// b=2
	// a=1
}

func ExampleWithSchema() {
	var v any
	if err := yaml.UnmarshalWithOptions([]byte("val: true\n"), &v, yaml.WithSchema(yaml.FailsafeSchema)); err != nil {
		panic(err)
	}
	m := v.(map[string]any)
	fmt.Printf("%T: %v\n", m["val"], m["val"])
	// Output:
	// string: true
}

func ExampleWithMaxDepth() {
	type Inner struct {
		Value int `yaml:"value"`
	}
	type Outer struct {
		Inner Inner `yaml:"inner"`
	}
	var v Outer
	err := yaml.UnmarshalWithOptions([]byte("inner:\n  value: 1\n"), &v, yaml.WithMaxDepth(100))
	if err != nil {
		panic(err)
	}
	fmt.Println(v.Inner.Value)
	// Output:
	// 1
}

func ExampleWithMaxAliasExpansion() {
	data := []byte("a: &x [1,2]\nb: &y [*x,*x]\nc: &z [*y,*y]\nd: [*z,*z]\n")
	var v any
	err := yaml.UnmarshalWithOptions(data, &v, yaml.WithMaxAliasExpansion(1))
	fmt.Println(err != nil)
	// Output:
	// true
}

func ExampleWithMaxDocumentSize() {
	big := bytes.Repeat([]byte("a"), 1000)
	var v any
	err := yaml.UnmarshalWithOptions(big, &v, yaml.WithMaxDocumentSize(100))
	fmt.Println(errors.Is(err, yaml.ErrDocumentSize))
	// Output:
	// true
}

func ExampleWithCustomMarshaler() {
	type Hex int
	data, _ := yaml.MarshalWithOptions(
		map[string]Hex{"code": 255},
		yaml.WithCustomMarshaler(func(h Hex) ([]byte, error) {
			return fmt.Appendf(nil, "0x%X", int(h)), nil
		}),
	)
	fmt.Print(string(data))
	// Output:
	// code: 0xFF
}

func ExampleWithCustomUnmarshaler() {
	type IP [4]byte
	var v struct {
		Addr IP `yaml:"addr"`
	}
	err := yaml.UnmarshalWithOptions([]byte("addr: 10.0.0.1\n"), &v,
		yaml.WithCustomUnmarshaler(func(ip *IP, data []byte) error {
			s := strings.TrimSpace(string(data))
			var a, b, c, d byte
			fmt.Sscanf(s, "%d.%d.%d.%d", &a, &b, &c, &d)
			*ip = IP{a, b, c, d}
			return nil
		}),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(v.Addr)
	// Output:
	// [10 0 0 1]
}

func ExampleWithTagResolver() {
	resolver := &yaml.TagResolver{
		Tag:    "!upper",
		GoType: reflect.TypeOf(""),
		Resolve: func(value string) (any, error) {
			return strings.ToUpper(value), nil
		},
	}
	var v map[string]string
	err := yaml.UnmarshalWithOptions([]byte("name: !upper hello\n"), &v, yaml.WithTagResolver(resolver))
	if err != nil {
		panic(err)
	}
	fmt.Println(v["name"])
	// Output:
	// HELLO
}

func ExampleWithValidator() {
	type Config struct {
		Port int `yaml:"port"`
	}
	validator := validatorFunc(func(v any) error {
		if c, ok := v.(*Config); ok && c.Port < 1 {
			return fmt.Errorf("port must be positive")
		}
		return nil
	})
	var cfg Config
	err := yaml.UnmarshalWithOptions([]byte("port: 0\n"), &cfg, yaml.WithValidator(validator))
	fmt.Println(err != nil)
	// Output:
	// true
}

type validatorFunc func(any) error

func (f validatorFunc) Struct(v any) error { return f(v) }
