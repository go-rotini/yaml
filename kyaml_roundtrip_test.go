package yaml

import (
	"bytes"
	"testing"
)

// TestKYAMLRoundTripPod verifies that a typical Kubernetes Pod struct
// survives MarshalKYAML → UnmarshalKYAML round-trip.
func TestKYAMLRoundTripPod(t *testing.T) {
	type Pod struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name   string            `json:"name"`
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Spec struct {
			Containers []struct {
				Name  string `json:"name"`
				Image string `json:"image"`
			} `json:"containers"`
		} `json:"spec"`
	}
	var p Pod
	p.APIVersion = "v1"
	p.Kind = "Pod"
	p.Metadata.Name = "demo"
	p.Metadata.Labels = map[string]string{"app": "demo", "tier": "frontend"}
	p.Spec.Containers = []struct {
		Name  string `json:"name"`
		Image string `json:"image"`
	}{{Name: "nginx", Image: "nginx:1.20"}}

	out, err := MarshalKYAML(p)
	if err != nil {
		t.Fatal(err)
	}
	if !IsKYAML(out) {
		t.Fatalf("output is not valid KYAML:\n%s\n%v", out, ValidateKYAML(out))
	}
	var got Pod
	if err := UnmarshalKYAML(out, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got.APIVersion != p.APIVersion || got.Kind != p.Kind ||
		got.Metadata.Name != p.Metadata.Name ||
		got.Metadata.Labels["app"] != "demo" ||
		got.Metadata.Labels["tier"] != "frontend" ||
		len(got.Spec.Containers) != 1 ||
		got.Spec.Containers[0].Name != "nginx" ||
		got.Spec.Containers[0].Image != "nginx:1.20" {
		t.Errorf("round-trip mismatch:\nwant: %+v\n got: %+v\n%s", p, got, out)
	}
}

// TestKYAMLMarshalIdempotence verifies that re-marshaling a parsed KYAML
// document produces byte-identical output.
func TestKYAMLMarshalIdempotence(t *testing.T) {
	// Use an ordered structure (struct) to avoid map-iteration nondeterminism.
	type S struct {
		A string `json:"a"`
		B int    `json:"b"`
		C []int  `json:"c"`
	}
	v := S{A: "one", B: 2, C: []int{1, 2, 3}}
	first, err := MarshalKYAML(v)
	if err != nil {
		t.Fatal(err)
	}

	var got S
	if err := UnmarshalKYAML(first, &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, first)
	}

	second, err := MarshalKYAML(got)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("MarshalKYAML round-trip not byte-identical:\n=== first:\n%s=== second:\n%s", first, second)
	}
}
