package yaml

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Representative decode, encode and parse benchmarks over a small config, a
// Kubernetes-style manifest and a large generated config, into structs and
// into generic values.

type perfSmall struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Debug   bool   `yaml:"debug"`
	Port    int    `yaml:"port"`
}

type perfContainer struct {
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
	Ports []struct {
		ContainerPort int `yaml:"containerPort"`
	} `yaml:"ports"`
	Resources struct {
		Limits   map[string]string `yaml:"limits"`
		Requests map[string]string `yaml:"requests"`
	} `yaml:"resources"`
	Env []struct {
		Name  string `yaml:"name"`
		Value string `yaml:"value"`
	} `yaml:"env"`
}

type perfMeta struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace,omitempty"`
	Labels    map[string]string `yaml:"labels"`
}

type perfMedium struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   perfMeta `yaml:"metadata"`
	Spec       struct {
		Replicas int `yaml:"replicas"`
		Selector struct {
			MatchLabels map[string]string `yaml:"matchLabels"`
		} `yaml:"selector"`
		Template struct {
			Metadata perfMeta `yaml:"metadata"`
			Spec     struct {
				Containers []perfContainer `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

type perfService struct {
	Name        string            `yaml:"name"`
	Image       string            `yaml:"image"`
	Replicas    int               `yaml:"replicas"`
	Enabled     bool              `yaml:"enabled"`
	Ratio       float64           `yaml:"ratio"`
	Ports       []int             `yaml:"ports"`
	Env         map[string]string `yaml:"env"`
	Labels      map[string]string `yaml:"labels"`
	Command     []string          `yaml:"command"`
	Description string            `yaml:"description"`
}

type perfLarge struct {
	Version  string        `yaml:"version"`
	Services []perfService `yaml:"services"`
}

var perfLargeYAML = sync.OnceValue(func() []byte {
	var sb strings.Builder
	sb.WriteString("# generated benchmark config\nversion: \"3.9\"\nservices:\n")
	for i := range 400 {
		fmt.Fprintf(&sb, "  - name: service-%d\n", i)
		fmt.Fprintf(&sb, "    image: registry.example.com/team/service-%d:v1.%d.%d\n", i, i%7, i%13)
		fmt.Fprintf(&sb, "    replicas: %d\n", i%5+1)
		fmt.Fprintf(&sb, "    enabled: %t\n", i%3 != 0)
		fmt.Fprintf(&sb, "    ratio: %d.%d\n", i%10, i%100)
		fmt.Fprintf(&sb, "    ports: [%d, %d]\n", 8000+i, 9000+i)
		sb.WriteString("    env:\n")
		fmt.Fprintf(&sb, "      LOG_LEVEL: info\n      REGION: 'us-east-%d'\n      DSN: \"postgres://db-%d:5432/app?sslmode=disable\"\n", i%4, i)
		sb.WriteString("    labels: {tier: backend, team: platform}\n")
		sb.WriteString("    command:\n      - /bin/server\n      - --listen=:8080\n      - --verbose\n")
		fmt.Fprintf(&sb, "    description: |\n      Service %d handles requests for the platform.\n      It is generated for benchmarking.\n", i)
	}
	return []byte(sb.String())
})

func benchDecode[T any](b *testing.B, data []byte) {
	b.Helper()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		var out T
		if err := Unmarshal(data, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func benchEncode[T any](b *testing.B, data []byte) {
	b.Helper()
	var v T
	if err := Unmarshal(data, &v); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPerfDecodeSmallStruct(b *testing.B)  { benchDecode[perfSmall](b, benchSmallYAML) }
func BenchmarkPerfDecodeSmallMap(b *testing.B)     { benchDecode[map[string]any](b, benchSmallYAML) }
func BenchmarkPerfDecodeMediumStruct(b *testing.B) { benchDecode[perfMedium](b, benchMediumYAML) }
func BenchmarkPerfDecodeMediumMap(b *testing.B)    { benchDecode[map[string]any](b, benchMediumYAML) }
func BenchmarkPerfDecodeLargeStruct(b *testing.B)  { benchDecode[perfLarge](b, perfLargeYAML()) }
func BenchmarkPerfDecodeLargeMap(b *testing.B)     { benchDecode[map[string]any](b, perfLargeYAML()) }

func BenchmarkPerfEncodeSmallStruct(b *testing.B)  { benchEncode[perfSmall](b, benchSmallYAML) }
func BenchmarkPerfEncodeSmallMap(b *testing.B)     { benchEncode[map[string]any](b, benchSmallYAML) }
func BenchmarkPerfEncodeMediumStruct(b *testing.B) { benchEncode[perfMedium](b, benchMediumYAML) }
func BenchmarkPerfEncodeMediumMap(b *testing.B)    { benchEncode[map[string]any](b, benchMediumYAML) }
func BenchmarkPerfEncodeLargeStruct(b *testing.B)  { benchEncode[perfLarge](b, perfLargeYAML()) }
func BenchmarkPerfEncodeLargeMap(b *testing.B)     { benchEncode[map[string]any](b, perfLargeYAML()) }

func BenchmarkPerfParseLarge(b *testing.B) {
	data := perfLargeYAML()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}
