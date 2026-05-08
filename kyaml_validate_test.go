package yaml

import (
	"bytes"
	"strings"
	"testing"
)

// TestKYAMLFormatBlockToFlow verifies Format converts arbitrary YAML into
// canonical KYAML.
func TestKYAMLFormatBlockToFlow(t *testing.T) {
	src := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  labels:
    app: demo
spec:
  containers:
  - name: nginx
    image: nginx:1.20
`)
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if !IsKYAML(out) {
		t.Errorf("Format output is not valid KYAML:\n%s\nerrors: %v", out, ValidateKYAML(out))
	}
}

// TestKYAMLFormatIdempotence verifies Format(Format(x)) == Format(x).
func TestKYAMLFormatIdempotence(t *testing.T) {
	inputs := [][]byte{
		[]byte("apiVersion: v1\nkind: Pod\n"),
		[]byte(`---
{
  name: "demo",
  count: 42,
}
`),
		[]byte(`shared: &x { a: 1 }
copy: *x
`),
		[]byte(`base: &b
  field1: hello
  field2: world
sub:
  <<: *b
  field3: extra
`),
	}
	for i, src := range inputs {
		t.Run("", func(t *testing.T) {
			once, err := Format(src)
			if err != nil {
				t.Fatalf("case %d first format: %v\n%s", i, err, src)
			}
			twice, err := Format(once)
			if err != nil {
				t.Fatalf("case %d second format: %v\n%s", i, err, once)
			}
			if !bytes.Equal(once, twice) {
				t.Errorf("case %d: Format is not idempotent\n=== once:\n%s=== twice:\n%s", i, once, twice)
			}
		})
	}
}

// TestKYAMLFormatStripsAnchors verifies anchors and aliases are reified.
func TestKYAMLFormatStripsAnchors(t *testing.T) {
	src := []byte("shared: &x foo\ncopy: *x\n")
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("&")) || bytes.Contains(out, []byte("*x")) {
		t.Errorf("anchors/aliases not reified:\n%s", out)
	}
	if !IsKYAML(out) {
		t.Errorf("Format output is not valid KYAML:\n%s", out)
	}
}

// TestKYAMLLintReturnsAllIssues verifies Lint accumulates all violations.
func TestKYAMLLintReturnsAllIssues(t *testing.T) {
	src := []byte(`---
{
  shared: &x { a: 1 },
  copy: *x,
  port: 0x50,
  enabled: yes,
}
`)
	issues, err := Lint(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) < 4 {
		t.Errorf("expected at least 4 issues (anchor, alias, hex, yes), got %d:\n%v", len(issues), issues)
	}
	for _, i := range issues {
		if i.Severity != SeverityError {
			t.Errorf("expected SeverityError for structural issue, got %v: %v", i.Severity, i)
		}
	}
}

// TestKYAMLLintCosmetic verifies the cosmetic check fires on non-canonical input.
func TestKYAMLLintCosmetic(t *testing.T) {
	// Valid KYAML structurally, but with extra whitespace that diverges from
	// canonical formatting.
	src := []byte(`---
{a:1,b:2}
`)
	issues, err := Lint(src, WithStrictKYAML(), WithKYAMLLintCosmetic())
	if err != nil {
		t.Fatal(err)
	}
	hasWarning := false
	for _, i := range issues {
		if i.Severity == SeverityWarning {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Errorf("expected cosmetic warning for non-canonical KYAML:\n%s\nissues: %v", src, issues)
	}
}

// TestKYAMLFormatErrorRenders verifies FormatError handles KYAMLError.
func TestKYAMLFormatErrorRenders(t *testing.T) {
	src := []byte("---\n{ port: 0x50 }\n")
	err := ValidateKYAML(src)
	if err == nil {
		t.Fatal("expected validation error")
	}
	rendered := FormatError(src, err)
	if !strings.Contains(rendered, "R12.11") {
		t.Errorf("expected rule ID R12.11 in formatted error:\n%s", rendered)
	}
	if !strings.Contains(rendered, "0x50") {
		t.Errorf("expected source line in formatted error:\n%s", rendered)
	}
}
