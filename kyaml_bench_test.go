package yaml

import (
	"testing"
)

// kyamlBenchPod is the standard medium-complexity input used for
// MarshalKYAML, UnmarshalKYAML, and Format benchmarks.
var kyamlBenchPod = map[string]any{
	"apiVersion": "v1",
	"kind":       "Pod",
	"metadata": map[string]any{
		"name":      "kyaml-bench-pod",
		"namespace": "default",
		"labels": map[string]string{
			"app":  "kyaml-bench",
			"tier": "frontend",
		},
	},
	"spec": map[string]any{
		"containers": []map[string]any{
			{
				"name":  "nginx",
				"image": "nginx:1.20",
				"ports": []map[string]any{
					{"containerPort": 80, "protocol": "TCP"},
				},
				"resources": map[string]any{
					"requests": map[string]string{"cpu": "100m", "memory": "64Mi"},
					"limits":   map[string]string{"cpu": "500m", "memory": "256Mi"},
				},
			},
		},
		"restartPolicy": "Always",
	},
}

var kyamlBenchYAMLBytes = []byte(`apiVersion: v1
kind: Pod
metadata:
  name: kyaml-bench-pod
  namespace: default
  labels:
    app: kyaml-bench
    tier: frontend
spec:
  containers:
  - name: nginx
    image: nginx:1.20
    ports:
    - containerPort: 80
      protocol: TCP
    resources:
      requests:
        cpu: 100m
        memory: 64Mi
      limits:
        cpu: 500m
        memory: 256Mi
  restartPolicy: Always
`)

func BenchmarkMarshalKYAML(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := MarshalKYAML(kyamlBenchPod); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalDefault(b *testing.B) {
	// Comparison baseline: same value, default block-style YAML.
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Marshal(kyamlBenchPod); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalKYAML(b *testing.B) {
	out, err := MarshalKYAML(kyamlBenchPod)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		var v map[string]any
		if err := UnmarshalKYAML(out, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateKYAMLPositive(b *testing.B) {
	out, _ := MarshalKYAML(kyamlBenchPod)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if err := ValidateKYAML(out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateKYAMLNegative(b *testing.B) {
	// Block-style YAML: should fail validation quickly.
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if err := ValidateKYAML(kyamlBenchYAMLBytes); err == nil {
			b.Fatal("expected validation failure")
		}
	}
}

func BenchmarkFormatKYAML(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Format(kyamlBenchYAMLBytes); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIsKYAML(b *testing.B) {
	out, _ := MarshalKYAML(kyamlBenchPod)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = IsKYAML(out)
	}
}
