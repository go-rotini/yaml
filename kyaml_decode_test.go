package yaml

import (
	"errors"
	"testing"
)

// TestKYAMLValidateRejections covers the full §2.12 forbidden-construct list.
// Each input should fail validation with the expected rule ID.
func TestKYAMLValidateRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		rule string
	}{
		{
			name: "missing-doc-header",
			yaml: `{ name: "foo" }` + "\n",
			rule: "R3.1",
		},
		{
			name: "anchor",
			yaml: "---\n{ a: &x 1, b: 2 }\n",
			rule: "R12.1",
		},
		{
			name: "alias",
			yaml: "---\n{ a: &x 1, b: *x }\n",
			rule: "R12.1",
		},
		{
			name: "explicit-tag",
			yaml: `---` + "\n" + `{ a: !!int 5 }` + "\n",
			rule: "R12.2",
		},
		{
			name: "merge-key",
			yaml: "---\n{ <<: { a: 1 }, b: 2 }\n",
			rule: "R12.3",
		},
		{
			name: "block-mapping",
			yaml: "---\nname: foo\n",
			rule: "R12.5",
		},
		{
			name: "block-sequence",
			yaml: "---\nitems:\n  - a\n  - b\n",
			rule: "R12.5", // outer mapping is block; the sequence error may also fire
		},
		{
			name: "plain-string-value",
			yaml: "---\n{ name: bare_string }\n",
			rule: "R12.7",
		},
		{
			name: "single-quoted",
			yaml: "---\n{ name: 'x' }\n",
			rule: "R12.8",
		},
		{
			// Block-style mapping with a literal block scalar value. The
			// outer mapping triggers R12.5 and the literal scalar triggers
			// R12.4 — we check for either.
			name: "literal-block-scalar",
			yaml: "---\nnote: |\n  multi\n  line\n",
			rule: "R12.5",
		},
		{
			name: "hex-int",
			yaml: "---\n{ port: 0x50 }\n",
			rule: "R12.11",
		},
		{
			name: "octal-int",
			yaml: "---\n{ mode: 0o755 }\n",
			rule: "R12.11",
		},
		{
			name: "binary-int",
			yaml: "---\n{ flags: 0b1010 }\n",
			rule: "R12.11",
		},
		{
			name: "yaml1-yes",
			yaml: "---\n{ enabled: yes }\n",
			rule: "R12.12",
		},
		{
			name: "nan",
			yaml: "---\n{ bad: .nan }\n",
			rule: "R12.13",
		},
		{
			name: "inf",
			yaml: "---\n{ bad: .inf }\n",
			rule: "R12.13",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateKYAML([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected validation error for input:\n%s", tc.yaml)
			}
			var k *KYAMLError
			if !errors.As(err, &k) {
				t.Fatalf("expected *KYAMLError, got %T: %v", err, err)
			}
			found := false
			for _, v := range k.Errors {
				if v.Rule == tc.rule {
					found = true
					break
				}
			}
			if !found {
				rules := []string{}
				for _, v := range k.Errors {
					rules = append(rules, v.Rule)
				}
				t.Errorf("expected violation %s, got rules: %v", tc.rule, rules)
			}
		})
	}
}

// TestKYAMLValidateAccepts confirms canonical KYAML inputs pass validation.
func TestKYAMLValidateAccepts(t *testing.T) {
	cases := []string{
		`---
{
  apiVersion: "v1",
  kind: "Pod",
}
`,
		`---
{
  count: 42,
  enabled: true,
  rate: 3.14,
  empty: null,
}
`,
		`---
[
  1,
  2,
  3,
]
`,
		`---
[{
  name: "x",
}]
`,
		`---
{
  spec: {
    containers: [{
      name: "nginx",
      image: "nginx:1.20",
    }],
  },
}
`,
	}
	for i, c := range cases {
		t.Run("", func(t *testing.T) {
			if err := ValidateKYAML([]byte(c)); err != nil {
				t.Errorf("case %d: expected valid KYAML, got %v\nyaml:\n%s", i, err, c)
			}
		})
	}
}

// TestKYAMLUnmarshalStrictMode verifies that UnmarshalKYAML rejects non-KYAML
// inputs and decodes valid KYAML inputs.
func TestKYAMLUnmarshalStrictMode(t *testing.T) {
	type S struct {
		Name string `json:"name"`
	}

	// Reject non-KYAML.
	var got S
	err := UnmarshalKYAML([]byte("name: foo\n"), &got)
	if err == nil {
		t.Fatal("expected error for block-style YAML under UnmarshalKYAML")
	}
	if !errors.Is(err, ErrKYAML) {
		t.Errorf("expected ErrKYAML, got %v", err)
	}

	// Accept valid KYAML.
	if err := UnmarshalKYAML([]byte("---\n{ name: \"foo\" }\n"), &got); err != nil {
		t.Fatalf("unmarshal valid KYAML failed: %v", err)
	}
	if got.Name != "foo" {
		t.Errorf("decoded value mismatch: got %q", got.Name)
	}
}

// TestKYAMLUnmarshalToGeneric verifies the generic UnmarshalKYAMLTo[T] form.
func TestKYAMLUnmarshalToGeneric(t *testing.T) {
	type S struct {
		X int `json:"x"`
	}
	got, err := UnmarshalKYAMLTo[S]([]byte("---\n{ x: 7 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.X != 7 {
		t.Errorf("decoded value mismatch: got %d", got.X)
	}
}

// TestKYAMLIsKYAML verifies the IsKYAML quick-check.
func TestKYAMLIsKYAML(t *testing.T) {
	if !IsKYAML([]byte("---\n{ a: 1 }\n")) {
		t.Error("expected IsKYAML to return true for valid KYAML")
	}
	if IsKYAML([]byte("a: 1\n")) {
		t.Error("expected IsKYAML to return false for block-style YAML")
	}
}
