# go-rotini/yaml

A Go YAML encoding and decoding package that implements the [YAML 1.2.2 specification](https://yaml.org/spec/1.2.2/) backed by the [YAML Test Suite](https://github.com/yaml/yaml-test-suite) conformance tests.

This package is used as the default YAML encoding/decoding package for the rotini cli framework.

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
    "log"

    "github.com/go-rotini/yaml"
)

type Config struct {
    Name string   `yaml:"name"`
    Port int      `yaml:"port"`
    Tags []string `yaml:"tags,flow"`
}

func main() {
    // Marshal
    c := Config{Name: "app", Port: 8080, Tags: []string{"web", "api"}}
    b, err := yaml.Marshal(c)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(string(b))

    // Unmarshal
    var c1 Config
    if err := yaml.Unmarshal(b, &c1); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("%+v\n", c1)

    // Generic unmarshal (no pointer required)
    c2, err := yaml.UnmarshalTo[Config](b)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("%+v\n", c2)
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
