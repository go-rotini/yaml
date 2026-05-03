# go-rotini/yaml

A Go YAML encoding and decoding package that implements the [YAML 1.2.2 specification](https://yaml.org/spec/1.2.2/) backed by the [YAML Test Suite](https://github.com/yaml/yaml-test-suite) conformance tests.

This package is used as the default YAML support package for [rotini](https://github.com/go-rotini/rotini).

## Features

- Full [YAML 1.2.2](https://yaml.org/spec/1.2.2/) specification support with Core, JSON, and Failsafe schema resolution
- Tested against the official [YAML Test Suite](https://github.com/yaml/yaml-test-suite) for conformance
- Generic `UnmarshalTo[T]` API and type-safe custom marshaler/unmarshaler registration
- Multi-document streaming with `Encoder`/`Decoder`
- Struct field tags: `omitempty`, `flow`, `inline`, `required`
- Encode options: indent, flow style, literal blocks, single quotes, JSON-compatible output, comments
- Decode options: strict mode, duplicate key rejection, ordered maps, custom tag resolvers, struct validation
- Anchor/alias resolution with cycle detection and merge key (`<<`) support
- Cross-file anchor references via `WithReferenceFiles`/`WithReferenceDirs`
- AST access via `Parse`, `Walk`, `Filter`, and `Node` tree manipulation
- JSONPath-like query engine (`PathString`) with read, replace, append, and delete operations
- Bidirectional JSON conversion (`ToJSON`/`FromJSON`) and `WithJSONUnmarshaler` fallback
- `Valid` function for quick syntax validation without full decoding
- `FormatError` for human-readable error output with source line and column pointer
- Context-aware encoding/decoding via `EncodeContext`/`DecodeContext`
- UTF-8, UTF-16 (LE/BE), and UTF-32 (LE/BE) encoding detection
- DoS protection: exponential alias expansion (billion laughs), quadratic blowup, deep nesting stack exhaustion, and oversized document attacks

## Installation

```bash
go get github.com/go-rotini/yaml
```

Requires Go 1.26 or later.

## Quick Start

```go
package main

import (
	"fmt"
	"log"

	"github.com/go-rotini/yaml"
)

type Service struct {
	Host    string   `yaml:"host,required"`
	Port    int      `yaml:"port"`
	Debug   bool     `yaml:"debug,omitempty"`
	Tags    []string `yaml:"tags,flow"`
	Token   string   `yaml:"-"`
}

func main() {
	// Marshal
	s := Service{Host: "localhost", Port: 8080, Tags: []string{"v1", "v2"}, Token: "hidden"}
	b, err := yaml.Marshal(s)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(b))

	// Unmarshal
	var s1 Service
	if err := yaml.Unmarshal(b, &s1); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", s1)

	// Generic unmarshal (no pointer required)
	s2, err := yaml.UnmarshalTo[Service](b)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", s2)
}
```

## Documentation

Full API reference is available on [pkg.go.dev](https://pkg.go.dev/github.com/go-rotini/yaml).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to contribute to this project.

## Code of Conduct

This project follows a code of conduct to ensure a welcoming community. See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
