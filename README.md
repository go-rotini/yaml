# yaml

A pure go YAML marshalling/unmarshalling package that implements the [YAML 1.2.2 specification](https://yaml.org/spec/1.2.2/) backed by the [YAML Test Suite](https://github.com/yaml/yaml-test-suite) conformance tests.

## Installation

```bash
go get github.com/go-rotini/yaml
```

Requires Go 1.21 or later.

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/go-rotini/yaml"
)

type Config struct {
    Name    string   `yaml:"name"`
    Port    int      `yaml:"port"`
    Tags    []string `yaml:"tags,flow"`
}

func main() {
    // Marshal
    cfg := Config{Name: "app", Port: 8080, Tags: []string{"web", "api"}}
    data, _ := yaml.Marshal(cfg)
    fmt.Println(string(data))

    // Unmarshal
    var out Config
    yaml.Unmarshal(data, &out)
    fmt.Printf("%+v\n", out)
}
```

## Features

| Feature | Status |
|---|---|
| YAML 1.2.2 core schema | Yes |
| UTF-8/16/32 input | Yes |
| Block & flow styles | Yes |
| All five scalar styles | Yes |
| Anchors & aliases | Yes |
| Merge keys (`<<`) | Yes |
| Multi-document streams | Yes |
| Struct tags (`yaml:"..."`) | Yes |
| JSON tag fallback | Yes |
| Custom marshalers | Yes |
| Context-aware marshalers | Yes |
| `time.Time` / `time.Duration` | Yes |
| `math/big` types | Yes |
| `encoding.TextMarshaler` | Yes |
| Ordered maps (`MapSlice`) | Yes |
| AST / Node API | Yes |
| YAMLPath queries | Yes |
| Comment preservation | Yes |
| Reference file resolution | Yes |
| Struct validation | Yes |
| JSON interop | Yes |
| Fuzz tested | Yes |

## Encoding Options

```go
yaml.MarshalWithOptions(v,
    yaml.Indent(4),
    yaml.IndentSequence(true),
    yaml.Flow(true),
    yaml.JSON(true),
    yaml.UseLiteralStyleIfMultiline(true),
    yaml.UseSingleQuote(true),
    yaml.OmitEmpty(true),
)
```

## Decoding Options

```go
yaml.UnmarshalWithOptions(data, &v,
    yaml.Strict(),
    yaml.DisallowDuplicateKey(),
    yaml.UseOrderedMap(),
    yaml.UseJSONUnmarshaler(),
    yaml.MaxDepth(50),
    yaml.MaxAliasExpansion(500),
    yaml.Validator(myValidator),
    yaml.ReferenceFiles("shared.yaml"),
    yaml.ReferenceDirs("refs/"),
)
```

## Streaming

```go
// Encode multiple documents
enc := yaml.NewEncoder(os.Stdout)
enc.Encode(doc1)
enc.Encode(doc2)
enc.Close()

// Decode multiple documents
dec := yaml.NewDecoder(file)
for {
    var v any
    if err := dec.Decode(&v); err == io.EOF {
        break
    }
}
```

## AST / Node API

```go
file, _ := yaml.Parse(data)
for _, doc := range file.Docs {
    yaml.Walk(doc, func(n *yaml.Node) bool {
        if n.Kind == yaml.ScalarNode {
            fmt.Println(n.Value)
        }
        return true
    })
}
```

## YAMLPath

```go
path, _ := yaml.PathString("$.servers[0].host")
result, _ := path.ReadString(data)
fmt.Println(result) // "localhost"
```

## JSON Interop

```go
jsonBytes, _ := yaml.YAMLToJSON(yamlData)
yamlBytes, _ := yaml.JSONToYAML(jsonData)
```

## Custom Marshalers

Implement any of these interfaces:

```go
type Marshaler interface {
    MarshalYAML() (any, error)
}
type Unmarshaler interface {
    UnmarshalYAML(func(any) error) error
}
type BytesMarshaler interface {
    MarshalYAML() ([]byte, error)
}
type BytesUnmarshaler interface {
    UnmarshalYAML([]byte) error
}
```

Context-aware variants (`MarshalerContext`, `UnmarshalerContext`) are also supported.

Falls back to `encoding.TextMarshaler`/`TextUnmarshaler` and optionally `json.Marshaler`/`json.Unmarshaler`.

## Struct Tags

```go
type Example struct {
    Name    string `yaml:"name"`
    Ignore  string `yaml:"-"`
    Empty   string `yaml:"empty,omitempty"`
    Inline  Inner  `yaml:",inline"`
    Items   []int  `yaml:"items,flow"`
    Ref     string `yaml:"ref,anchor"`
    Alias   string `yaml:"alias,alias"`
}
```

The `required` option (`yaml:"field,required"`) causes an error if the field is missing during decode.

## Migration from yaml.v3

| yaml.v3 | This package |
|---|---|
| `yaml.Marshal(v)` | `yaml.Marshal(v)` |
| `yaml.Unmarshal(data, &v)` | `yaml.Unmarshal(data, &v)` |
| `yaml.NewEncoder(w)` | `yaml.NewEncoder(w, opts...)` |
| `yaml.NewDecoder(r)` | `yaml.NewDecoder(r, opts...)` |
| `yaml.Node` | `yaml.Node` (different fields) |

Key differences:
- Functional options instead of setter methods
- Richer error types with source positions
- Built-in YAMLPath queries
- Reference file resolution
- Struct validation hooks

## Migration from goccy/go-yaml

Most API names are identical. Key differences:
- Module path changes to `github.com/go-rotini/yaml`
- `yaml.PathString` returns `(*Path, error)` with the same query syntax
- `MapSlice` replaces `MapItem` for ordered maps
- Options use the same functional pattern

## Error Handling

Errors include source position information:

```go
err := yaml.Unmarshal(badData, &v)
fmt.Println(yaml.FormatError(badData, err))
// error at line 3, column 5: unexpected token
//   bad: [value
//        ^
```

Use `errors.As` to inspect typed errors: `SyntaxError`, `TypeError`, `DuplicateKeyError`.
