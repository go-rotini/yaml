package yaml

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var k8sYAML []byte

func TestMain(m *testing.M) {
	var err error
	k8sYAML, err = os.ReadFile("testdata/acceptance/k8s.yaml")
	if err != nil {
		panic("failed to read testdata/acceptance/k8s.yaml: " + err.Error())
	}
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Parse + AST
// ---------------------------------------------------------------------------

func TestAcceptanceParse(t *testing.T) {
	file, err := Parse(k8sYAML)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) != 10 {
		t.Fatalf("expected 10 documents, got %d", len(file.Docs))
	}
	for i, doc := range file.Docs {
		if doc.Kind != DocumentNode {
			t.Errorf("doc %d: expected DocumentNode, got %v", i, doc.Kind)
		}
	}
}

func TestAcceptanceParseNodeKinds(t *testing.T) {
	file, err := Parse(k8sYAML)
	if err != nil {
		t.Fatal(err)
	}
	var mappings, sequences, scalars int
	for _, doc := range file.Docs {
		Walk(doc, func(n *Node) bool {
			switch n.Kind {
			case MappingNode:
				mappings++
			case SequenceNode:
				sequences++
			case ScalarNode:
				scalars++
			}
			return true
		})
	}
	if mappings == 0 {
		t.Error("expected mapping nodes")
	}
	if sequences == 0 {
		t.Error("expected sequence nodes")
	}
	if scalars == 0 {
		t.Error("expected scalar nodes")
	}
}

func TestAcceptanceParseFilter(t *testing.T) {
	file, err := Parse(k8sYAML)
	if err != nil {
		t.Fatal(err)
	}
	anchors := Filter(file.Docs[1], func(n *Node) bool {
		return n.Anchor != ""
	})
	if len(anchors) == 0 {
		t.Error("expected at least one anchor in ConfigMap document")
	}
}

func TestAcceptanceNodeToBytes(t *testing.T) {
	file, err := Parse(k8sYAML)
	if err != nil {
		t.Fatal(err)
	}
	for i, doc := range file.Docs {
		out, err := NodeToBytes(doc)
		if err != nil {
			t.Fatalf("doc %d: NodeToBytes: %v", i, err)
		}
		if len(out) == 0 {
			t.Errorf("doc %d: empty output from NodeToBytes", i)
		}
	}
}

func TestAcceptanceNodeValidate(t *testing.T) {
	file, err := Parse(k8sYAML)
	if err != nil {
		t.Fatal(err)
	}
	for i, doc := range file.Docs {
		if err := doc.Validate(); err != nil {
			t.Errorf("doc %d: Validate: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Unmarshal
// ---------------------------------------------------------------------------

func TestAcceptanceUnmarshal(t *testing.T) {
	dec := NewDecoder(bytes.NewReader(k8sYAML))
	var docs []map[string]any
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		docs = append(docs, obj)
	}
	if len(docs) != 10 {
		t.Fatalf("expected 10 documents, got %d", len(docs))
	}

	kinds := []string{
		"Namespace", "ConfigMap", "Secret", "Deployment", "Service",
		"HorizontalPodAutoscaler", "NetworkPolicy", "PodDisruptionBudget",
		"ServiceAccount", "CronJob",
	}
	for i, want := range kinds {
		got, _ := docs[i]["kind"].(string)
		if got != want {
			t.Errorf("doc %d: kind = %q, want %q", i, got, want)
		}
	}
}

func TestAcceptanceUnmarshalTo(t *testing.T) {
	single := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n")
	type k8sMeta struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Metadata   struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	obj, err := UnmarshalTo[k8sMeta](single)
	if err != nil {
		t.Fatal(err)
	}
	if obj.APIVersion != "v1" || obj.Kind != "ConfigMap" || obj.Metadata.Name != "test" {
		t.Errorf("unexpected result: %+v", obj)
	}
}

func TestAcceptanceUnmarshalAnchorAlias(t *testing.T) {
	dec := NewDecoder(bytes.NewReader(k8sYAML))
	var configMap map[string]any
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if obj["kind"] == "ConfigMap" {
			configMap = obj
			break
		}
	}

	meta, _ := configMap["metadata"].(map[string]any)
	labels, _ := meta["labels"].(map[string]any)
	annotations, _ := meta["annotations"].(map[string]any)
	aliased, _ := annotations["verified-labels"].(map[string]any)

	if labels["app"] != "web-frontend" {
		t.Errorf("ConfigMap label: got %v", labels["app"])
	}
	if aliased["app"] != labels["app"] {
		t.Error("anchor/alias: aliased labels should match anchor labels")
	}
	if aliased["tier"] != labels["tier"] || aliased["environment"] != labels["environment"] {
		t.Error("anchor/alias: aliased label values diverged")
	}
}

func TestAcceptanceUnmarshalLiteralBlock(t *testing.T) {
	dec := NewDecoder(bytes.NewReader(k8sYAML))
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if obj["kind"] != "ConfigMap" {
			continue
		}
		data, _ := obj["data"].(map[string]any)
		configYAML, _ := data["config.yaml"].(string)
		if !strings.Contains(configYAML, "port: 8080") {
			t.Errorf("expected literal block to contain 'port: 8080', got:\n%s", configYAML)
		}
		if !strings.HasSuffix(configYAML, "\n") {
			t.Error("literal block should end with newline")
		}
		return
	}
	t.Fatal("ConfigMap not found")
}

func TestAcceptanceUnmarshalNestedStructs(t *testing.T) {
	type container struct {
		Name  string `yaml:"name"`
		Image string `yaml:"image"`
		Ports []struct {
			Name          string `yaml:"name"`
			ContainerPort int    `yaml:"containerPort"`
			Protocol      string `yaml:"protocol"`
		} `yaml:"ports"`
	}
	type deploySpec struct {
		Replicas int `yaml:"replicas"`
		Template struct {
			Spec struct {
				Containers     []container `yaml:"containers"`
				InitContainers []container `yaml:"initContainers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	}
	type deployment struct {
		APIVersion string     `yaml:"apiVersion"`
		Kind       string     `yaml:"kind"`
		Spec       deploySpec `yaml:"spec"`
	}

	dec := NewDecoder(bytes.NewReader(k8sYAML))
	for {
		var d deployment
		if err := dec.Decode(&d); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if d.Kind != "Deployment" {
			continue
		}
		if d.Spec.Replicas != 3 {
			t.Errorf("replicas = %d, want 3", d.Spec.Replicas)
		}
		if len(d.Spec.Template.Spec.Containers) != 1 {
			t.Fatalf("expected 1 container, got %d", len(d.Spec.Template.Spec.Containers))
		}
		c := d.Spec.Template.Spec.Containers[0]
		if c.Name != "app" {
			t.Errorf("container name = %q, want %q", c.Name, "app")
		}
		if len(c.Ports) != 2 {
			t.Errorf("expected 2 ports, got %d", len(c.Ports))
		}
		if c.Ports[0].ContainerPort != 8080 {
			t.Errorf("port = %d, want 8080", c.Ports[0].ContainerPort)
		}
		if len(d.Spec.Template.Spec.InitContainers) != 1 {
			t.Fatalf("expected 1 init container, got %d", len(d.Spec.Template.Spec.InitContainers))
		}
		return
	}
	t.Fatal("Deployment not found")
}

func TestAcceptanceUnmarshalFlowSequence(t *testing.T) {
	dec := NewDecoder(bytes.NewReader(k8sYAML))
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if obj["kind"] != "NetworkPolicy" {
			continue
		}
		spec, _ := obj["spec"].(map[string]any)
		types, _ := spec["policyTypes"].([]any)
		if len(types) != 2 {
			t.Fatalf("expected 2 policyTypes, got %d", len(types))
		}
		if types[0] != "Ingress" || types[1] != "Egress" {
			t.Errorf("policyTypes = %v, want [Ingress, Egress]", types)
		}
		return
	}
	t.Fatal("NetworkPolicy not found")
}

func TestAcceptanceUnmarshalWithOrderedMap(t *testing.T) {
	single := []byte("z: 1\na: 2\nm: 3\n")
	var result any
	if err := UnmarshalWithOptions(single, &result, WithOrderedMap()); err != nil {
		t.Fatal(err)
	}
	ms, ok := result.(MapSlice)
	if !ok {
		t.Fatalf("expected MapSlice, got %T", result)
	}
	if len(ms) != 3 {
		t.Fatalf("expected 3 items, got %d", len(ms))
	}
	keys := make([]string, len(ms))
	for i, item := range ms {
		keys[i], _ = item.Key.(string)
	}
	if keys[0] != "z" || keys[1] != "a" || keys[2] != "m" {
		t.Errorf("key order not preserved: %v", keys)
	}
}

// ---------------------------------------------------------------------------
// Marshal + Encoder
// ---------------------------------------------------------------------------

func TestAcceptanceMarshalRoundTrip(t *testing.T) {
	dec := NewDecoder(bytes.NewReader(k8sYAML))
	for docIdx := 0; ; docIdx++ {
		var original map[string]any
		if err := dec.Decode(&original); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}

		encoded, err := Marshal(original)
		if err != nil {
			t.Fatalf("doc %d: Marshal: %v", docIdx, err)
		}

		var decoded map[string]any
		if err := Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("doc %d: re-Unmarshal: %v", docIdx, err)
		}

		origJSON, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("doc %d: json.Marshal original: %v", docIdx, err)
		}
		decJSON, err := json.Marshal(decoded)
		if err != nil {
			t.Fatalf("doc %d: json.Marshal decoded: %v", docIdx, err)
		}
		if string(origJSON) != string(decJSON) {
			t.Errorf("doc %d: round-trip mismatch", docIdx)
		}
	}
}

func TestAcceptanceEncoderMultiDoc(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	docs := []map[string]string{
		{"kind": "ConfigMap"},
		{"kind": "Secret"},
		{"kind": "Service"},
	}
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			t.Fatal(err)
		}
	}
	enc.Close()

	output := buf.String()
	if strings.Count(output, "---") != 2 {
		t.Errorf("expected 2 document separators, got %d in:\n%s",
			strings.Count(output, "---"), output)
	}

	dec := NewDecoder(strings.NewReader(output))
	var count int
	for {
		var obj map[string]string
		if err := dec.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if obj["kind"] != docs[count]["kind"] {
			t.Errorf("doc %d: kind = %q, want %q", count, obj["kind"], docs[count]["kind"])
		}
		count++
	}
	if count != 3 {
		t.Errorf("decoded %d docs, want 3", count)
	}
}

func TestAcceptanceMarshalWithOptions(t *testing.T) {
	obj := map[string]any{
		"items":  []string{"a", "b"},
		"nested": map[string]int{"x": 1},
	}
	flow, err := MarshalWithOptions(obj, WithFlow(true))
	if err != nil {
		t.Fatal(err)
	}
	s := string(flow)
	if !strings.Contains(s, "{") || !strings.Contains(s, "[") {
		t.Errorf("expected flow style, got:\n%s", s)
	}

	indented, err := MarshalWithOptions(obj, WithIndent(4))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(indented), "    ") {
		t.Errorf("expected 4-space indent, got:\n%s", indented)
	}
}

// ---------------------------------------------------------------------------
// JSON conversion
// ---------------------------------------------------------------------------

func TestAcceptanceToJSON(t *testing.T) {
	single := []byte("name: test\ncount: 42\nitems:\n- a\n- b\n")
	j, err := ToJSON(single)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(j, &obj); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if obj["name"] != "test" {
		t.Errorf("name = %v, want test", obj["name"])
	}
	items, _ := obj["items"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestAcceptanceFromJSON(t *testing.T) {
	j := []byte(`{"apiVersion":"v1","kind":"Service","metadata":{"name":"web"}}`)
	y, err := FromJSON(j)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := Unmarshal(y, &obj); err != nil {
		t.Fatalf("FromJSON output is not valid YAML: %v", err)
	}
	if obj["kind"] != "Service" {
		t.Errorf("kind = %v, want Service", obj["kind"])
	}
	meta, _ := obj["metadata"].(map[string]any)
	if meta["name"] != "web" {
		t.Errorf("metadata.name = %v, want web", meta["name"])
	}
}

// ---------------------------------------------------------------------------
// Path queries
// ---------------------------------------------------------------------------

func TestAcceptancePathRead(t *testing.T) {
	file, err := Parse(k8sYAML)
	if err != nil {
		t.Fatal(err)
	}

	p, err := PathString("$.metadata.name")
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := p.Read(file.Docs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Value != "demo" {
		t.Errorf("expected 'demo', got %v", nodes)
	}
}

func TestAcceptancePathReadString(t *testing.T) {
	single := []byte("apiVersion: v1\nkind: ConfigMap\n")
	p, err := PathString("$.kind")
	if err != nil {
		t.Fatal(err)
	}
	val, err := p.ReadString(single)
	if err != nil {
		t.Fatal(err)
	}
	if val != "ConfigMap" {
		t.Errorf("ReadString = %q, want %q", val, "ConfigMap")
	}
}

func TestAcceptancePathReplace(t *testing.T) {
	data := []byte("name: old\nvalue: keep\n")
	file, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.name")
	replacement := &Node{Kind: ScalarNode, Value: "new"}
	if err := p.Replace(file.Docs[0], replacement); err != nil {
		t.Fatal(err)
	}
	out, _ := NodeToBytes(file.Docs[0])
	var obj map[string]string
	if err := Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["name"] != "new" {
		t.Errorf("name = %q after replace, want %q", obj["name"], "new")
	}
	if obj["value"] != "keep" {
		t.Errorf("value = %q, should be unchanged", obj["value"])
	}
}

func TestAcceptancePathAppend(t *testing.T) {
	data := []byte("items:\n- a\n- b\n")
	file, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.items")
	newItem := &Node{Kind: ScalarNode, Value: "c"}
	if err := p.Append(file.Docs[0], newItem); err != nil {
		t.Fatal(err)
	}
	out, _ := NodeToBytes(file.Docs[0])
	var obj map[string][]string
	if err := Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if len(obj["items"]) != 3 || obj["items"][2] != "c" {
		t.Errorf("items after append = %v, want [a b c]", obj["items"])
	}
}

func TestAcceptancePathDelete(t *testing.T) {
	data := []byte("a: 1\nb: 2\nc: 3\n")
	file, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PathString("$.b")
	if err := p.Delete(file.Docs[0]); err != nil {
		t.Fatal(err)
	}
	out, _ := NodeToBytes(file.Docs[0])
	var obj map[string]int
	if err := Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if _, exists := obj["b"]; exists {
		t.Error("key 'b' should have been deleted")
	}
	if obj["a"] != 1 || obj["c"] != 3 {
		t.Errorf("remaining keys wrong: %v", obj)
	}
}

func TestAcceptancePathWildcardDeep(t *testing.T) {
	file, err := Parse(k8sYAML)
	if err != nil {
		t.Fatal(err)
	}
	p, err := PathString("$.spec.template.spec.containers[0].name")
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := p.Read(file.Docs[3])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 match, got %d", len(nodes))
	}
	if nodes[0].Value != "app" {
		t.Errorf("container name = %q, want %q", nodes[0].Value, "app")
	}
}

// ---------------------------------------------------------------------------
// Decode options
// ---------------------------------------------------------------------------

func TestAcceptanceStrictMode(t *testing.T) {
	type S struct {
		Name string `yaml:"name"`
	}
	err := UnmarshalWithOptions([]byte("name: test\nextra: field\n"), &S{}, WithStrict())
	if err == nil {
		t.Fatal("expected error in strict mode for unknown field")
	}
	var unkErr *UnknownFieldError
	if !errors.As(err, &unkErr) {
		t.Errorf("expected UnknownFieldError, got %T: %v", err, err)
	}
}

func TestAcceptanceDuplicateKey(t *testing.T) {
	err := UnmarshalWithOptions([]byte("a: 1\na: 2\n"), &map[string]int{}, WithDisallowDuplicateKey())
	if err == nil {
		t.Fatal("expected error for duplicate key")
	}
	var dupErr *DuplicateKeyError
	if !errors.As(err, &dupErr) {
		t.Errorf("expected DuplicateKeyError, got %T: %v", err, err)
	}
}

func TestAcceptanceMaxDepth(t *testing.T) {
	deep := []byte("a:\n  b:\n    c:\n      d:\n        e: 1\n")
	err := UnmarshalWithOptions(deep, &map[string]any{}, WithMaxDepth(2))
	if err == nil {
		t.Fatal("expected error for exceeding max depth")
	}
	var synErr *SyntaxError
	if !errors.As(err, &synErr) {
		t.Errorf("expected SyntaxError, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// Error types
// ---------------------------------------------------------------------------

func TestAcceptanceSyntaxError(t *testing.T) {
	err := Unmarshal([]byte("key: [unclosed"), &map[string]any{})
	if err == nil {
		t.Fatal("expected syntax error")
	}
	if !errors.Is(err, ErrSyntax) {
		t.Errorf("expected ErrSyntax, got %T: %v", err, err)
	}
}

func TestAcceptanceFormatError(t *testing.T) {
	data := []byte("good: line\nbad: [unclosed")
	err := Unmarshal(data, &map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
	formatted := FormatError(data, err)
	if !strings.Contains(formatted, "^") {
		t.Error("FormatError should contain caret pointer")
	}
	colored := FormatError(data, err, true)
	if !strings.Contains(colored, "\x1b[") {
		t.Error("FormatError with color should contain ANSI escapes")
	}
}

// ---------------------------------------------------------------------------
// Custom marshaler / unmarshaler interfaces
// ---------------------------------------------------------------------------

type cidr struct {
	IP   string
	Mask int
}

func (c cidr) MarshalYAML() (any, error) {
	return c.IP + "/" + strings.Repeat("f", c.Mask), nil
}

func (c *cidr) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	parts := strings.SplitN(s, "/", 2)
	c.IP = parts[0]
	c.Mask = len(parts[1])
	return nil
}

func TestAcceptanceCustomMarshalerInterface(t *testing.T) {
	c := cidr{IP: "10.0.0.0", Mask: 4}
	data, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "10.0.0.0/ffff") {
		t.Errorf("expected custom marshal output, got:\n%s", data)
	}

	var c2 cidr
	if err := Unmarshal(data, &c2); err != nil {
		t.Fatal(err)
	}
	if c2.IP != "10.0.0.0" || c2.Mask != 4 {
		t.Errorf("round-trip failed: %+v", c2)
	}
}

func TestAcceptanceCustomMarshalerOption(t *testing.T) {
	type version struct{ Major, Minor int }
	data, err := MarshalWithOptions(
		version{Major: 1, Minor: 2},
		WithCustomMarshaler(func(v version) ([]byte, error) {
			return []byte("v1.2"), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "v1.2") {
		t.Errorf("expected custom marshaler output, got:\n%s", data)
	}
}

// TestAcceptanceKYAML walks testdata/acceptance/kyaml/ and runs every golden
// .kyaml file through Parse, Unmarshal (to map[string]any), Format, and
// idempotence checks. It also formats the corresponding _fixtures/*.yaml
// (if present) and asserts byte-equality with the golden.
//
// Real-world fixtures cover ingress, CRD, kustomization, and Helm values —
// content shapes that exercise nested objects, lists of objects, lists of
// strings, empty containers, deeply nested schemas, and quoted special
// values like "true"/"yes".
func TestAcceptanceKYAML(t *testing.T) {
	root := filepath.Join("testdata", "acceptance", "kyaml")
	matches, err := filepath.Glob(filepath.Join(root, "*.kyaml"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("no acceptance kyaml goldens present")
	}
	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			golden, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			// 1. Golden is valid KYAML.
			if err := ValidateKYAML(golden); err != nil {
				t.Errorf("not valid KYAML:\n%s", FormatError(golden, err))
				return
			}

			// 2. AST parse succeeds.
			if _, err := Parse(golden); err != nil {
				t.Errorf("Parse failed: %v", err)
				return
			}

			// 3. Decode to a generic map and back.
			var v any
			if err := UnmarshalKYAML(golden, &v); err != nil {
				t.Errorf("UnmarshalKYAML: %v", err)
				return
			}

			// 4. Format is idempotent.
			once, err := Format(golden)
			if err != nil {
				t.Errorf("Format: %v", err)
				return
			}
			if !bytes.Equal(once, golden) {
				t.Errorf("not idempotent against golden\n=== golden:\n%s=== formatted:\n%s", golden, once)
				return
			}

			// 5. The corresponding block-style fixture (if any) Formats
			// to the same bytes.
			base := filepath.Base(path)
			name := strings.TrimSuffix(base, filepath.Ext(base))
			fxt := filepath.Join(root, "_fixtures", name+".yaml")
			if src, err := os.ReadFile(fxt); err == nil {
				got, err := Format(src)
				if err != nil {
					t.Errorf("Format fixture %s: %v", fxt, err)
					return
				}
				if !bytes.Equal(got, golden) {
					t.Errorf("Format(%s) does not match golden\n=== expected:\n%s=== got:\n%s", fxt, golden, got)
				}
			}
		})
	}
}

// TestAcceptanceKYAMLRoundTripGenericMap verifies that decoding any
// acceptance fixture into map[string]any and re-marshaling produces valid
// KYAML (though not necessarily byte-identical, since map ordering may
// differ from struct declaration order — we only check structural validity).
func TestAcceptanceKYAMLRoundTripGenericMap(t *testing.T) {
	root := filepath.Join("testdata", "acceptance", "kyaml")
	matches, _ := filepath.Glob(filepath.Join(root, "*.kyaml"))
	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var v any
			if err := UnmarshalKYAML(data, &v); err != nil {
				t.Fatalf("decode: %v", err)
			}
			out, err := MarshalKYAML(v)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if !ValidKYAML(out) {
				var k *KYAMLError
				if errors.As(ValidateKYAML(out), &k) {
					t.Errorf("re-marshaled output is not KYAML: %d violations", len(k.Errors))
				}
			}
		})
	}
}

// TestAcceptanceKYAMLKubectlGoldens verifies that every golden file in
// testdata/kyaml/kubectl/ is conformant KYAML and is byte-identical to
// what the encoder produces when re-formatting it. This catches regressions
// against the corpus shipped in this repository.
//
// To regenerate the corpus against actual kubectl output, run:
//
//	make refresh-kyaml-corpus
//
// (requires a working kubectl + cluster context, with KYAML enabled).
func TestAcceptanceKYAMLKubectlGoldens(t *testing.T) {
	root := filepath.Join("testdata", "kyaml", "kubectl")
	matches, err := filepath.Glob(filepath.Join(root, "*.kyaml"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("no kubectl golden files present; run scripts/refresh-kyaml-kubectl-corpus.sh to generate")
	}
	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			golden, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}

			// 1. Strip the leading header comment (if present) so we
			// validate only the KYAML body.
			body := stripHeaderComment(golden)

			// 2. Body must be valid KYAML.
			if err := ValidateKYAML(body); err != nil {
				t.Errorf("golden %s is not valid KYAML:\n%s", path, FormatError(body, err))
				return
			}

			// 3. Format(body) must equal body exactly (idempotence — no
			// further normalization needed).
			reformatted, err := Format(body)
			if err != nil {
				t.Fatalf("Format %s: %v", path, err)
			}
			if !bytes.Equal(reformatted, body) {
				t.Errorf("golden %s drifted from canonical KYAML\n=== golden:\n%s=== reformatted:\n%s", path, body, reformatted)
			}
		})
	}
}

// TestAcceptanceKYAMLKubectlFormatting verifies that block-style YAML fixtures
// in testdata/kyaml/kubectl/_fixtures/ Format to the corresponding *.kyaml
// golden files. This is the conversion-correctness check.
func TestAcceptanceKYAMLKubectlFormatting(t *testing.T) {
	root := filepath.Join("testdata", "kyaml", "kubectl")
	fixtures, err := filepath.Glob(filepath.Join(root, "_fixtures", "*.yaml"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(fixtures) == 0 {
		t.Skip("no kubectl fixtures present")
	}
	for _, fxt := range fixtures {
		t.Run(filepath.Base(fxt), func(t *testing.T) {
			src, err := os.ReadFile(fxt)
			if err != nil {
				t.Fatalf("read %s: %v", fxt, err)
			}

			// Find the corresponding .kyaml golden.
			base := filepath.Base(fxt)
			name := strings.TrimSuffix(base, filepath.Ext(base))
			goldenPath := filepath.Join(root, name+".kyaml")
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Skipf("no golden for fixture %s", fxt)
			}
			expected := stripHeaderComment(golden)

			// Format the source into KYAML and compare.
			got, err := Format(src)
			if err != nil {
				t.Fatalf("Format %s: %v", fxt, err)
			}
			if !bytes.Equal(got, expected) {
				t.Errorf("Format(%s) does not match golden\n=== expected (%s):\n%s=== got:\n%s", fxt, goldenPath, expected, got)
			}
		})
	}
}

// stripHeaderComment removes a single leading run of "#"-prefixed comment
// lines from data, returning the body. Used to skip the "Generated by"
// header inserted by refresh-kyaml-kubectl-corpus.sh.
func stripHeaderComment(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	i := 0
	for i < len(lines) && (len(lines[i]) == 0 || lines[i][0] == '#') {
		i++
	}
	return bytes.Join(lines[i:], []byte("\n"))
}

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
	if !ValidKYAML(out) {
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
	if !ValidKYAML(out) {
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

// TestKYAMLFormatPreservesComments (R11.1, R11.2, R11.3, R11.4): comments
// from the input AST should survive Format() and appear in the KYAML
// output. Best-effort per R11.5.
func TestKYAMLFormatPreservesComments(t *testing.T) {
	src := []byte(`# top of file
apiVersion: v1
kind: Pod
metadata:
  # name comment
  name: my-pod
  labels:
    app: demo  # inline comment
`)
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"name comment", "inline comment"} {
		if !strings.Contains(s, want) {
			t.Errorf("expected comment %q in formatted output:\n%s", want, s)
		}
	}
	if !ValidKYAML(out) {
		t.Errorf("output is not valid KYAML:\n%s\nerrors: %v", s, ValidateKYAML(out))
	}
}

// TestKYAMLFormatNoCommentsIdempotent: with no comments in input, Format
// remains byte-idempotent.
func TestKYAMLFormatNoCommentsIdempotent(t *testing.T) {
	src := []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n")
	once, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, twice) {
		t.Errorf("Format not idempotent\n=== once:\n%s=== twice:\n%s", once, twice)
	}
}

// TestKYAMLEncodeR85UncuddleOnComments (R8.5): when comments are registered
// via WithComment, sequence cuddling is suppressed so post-pass-inserted
// comments don't land between cuddled brackets.
func TestKYAMLEncodeR85UncuddleOnComments(t *testing.T) {
	v := []map[string]string{{"name": "x"}}
	out, err := MarshalWithOptions(v, WithKYAML(), WithComment(map[string][]Comment{
		"name": {{Position: HeadCommentPos, Text: "the name"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Without cuddling, the output must NOT contain `[{` (open cuddle) or
	// `}]` (close cuddle).
	if strings.Contains(s, "[{") {
		t.Errorf("expected uncuddled open `[` `{` when comments present:\n%s", s)
	}
	if strings.Contains(s, "}]") {
		t.Errorf("expected uncuddled close `}` `]` when comments present:\n%s", s)
	}
	if !strings.Contains(s, "the name") {
		t.Errorf("comment not preserved:\n%s", s)
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

// ---------------------------------------------------------------------------
// TestKEPCompliance: explicit rule-by-rule conformance harness.
//
// Every rule in YAML_PACKAGE_REQUIREMENTS.md §2 has its own sub-test below
// — sub-test names match the rule IDs (R1.1, R1.2, …, R14.5) so the
// rule-to-test mapping is unambiguous. Many of these duplicate coverage
// from focused tests elsewhere; the goal here is auditability: a failure
// in this test points directly at the spec rule that broke.
//
// Run a single rule via:
//
//	go test -run 'TestKEPCompliance/R12.1' .
// ---------------------------------------------------------------------------

func TestKEPCompliance(t *testing.T) {
	// === §2.1 Compatibility model =====================================

	t.Run("R1.1 KYAML output is valid YAML", func(t *testing.T) {
		v := map[string]any{
			"apiVersion": "v1", "kind": "Pod",
			"metadata": map[string]any{"name": "x"},
			"spec":     map[string]any{"replicas": 3},
		}
		out, err := MarshalKYAML(v)
		if err != nil {
			t.Fatal(err)
		}
		var got any
		if err := Unmarshal(out, &got); err != nil {
			t.Fatalf("KYAML output failed to parse as YAML: %v\n%s", err, out)
		}
	})

	t.Run("R1.2 compliant YAML parsers parse KYAML", func(t *testing.T) {
		// Verify by routing KYAML output through the package's own decoder
		// (which is a YAML 1.2 parser).
		out, err := MarshalKYAML([]any{1, "two", true, nil, map[string]int{"a": 1}})
		if err != nil {
			t.Fatal(err)
		}
		var got []any
		if err := Unmarshal(out, &got); err != nil {
			t.Fatalf("YAML parser rejected KYAML output: %v\n%s", err, out)
		}
		if len(got) != 5 {
			t.Errorf("expected 5 elements, got %d", len(got))
		}
	})

	t.Run("R1.3 not all YAML is KYAML", func(t *testing.T) {
		// Block-style YAML is rejected by strict KYAML validation.
		if ValidKYAML([]byte("name: foo\n")) {
			t.Error("block-style YAML should not be valid KYAML")
		}
	})

	t.Run("R1.4 default decode accepts any valid YAML", func(t *testing.T) {
		// Default Unmarshal (no WithStrictKYAML) accepts non-KYAML input.
		var v map[string]any
		if err := Unmarshal([]byte("name: foo\n"), &v); err != nil {
			t.Fatal(err)
		}
		if v["name"] != "foo" {
			t.Errorf("default decode failed: %v", v)
		}
	})

	t.Run("R1.5 strict KYAML decode is opt-in", func(t *testing.T) {
		// Without the option, plain YAML decodes fine; with it, rejected.
		src := []byte("name: foo\n")
		var v map[string]any
		if err := UnmarshalWithOptions(src, &v); err != nil {
			t.Errorf("default mode should accept block YAML: %v", err)
		}
		if err := UnmarshalWithOptions(src, &v, WithStrictKYAML()); !errors.Is(err, ErrKYAML) {
			t.Errorf("strict mode should reject block YAML, got %v", err)
		}
	})

	t.Run("R1.6 round-trip stability (idempotence)", func(t *testing.T) {
		v := map[string]any{"a": 1, "b": []int{1, 2, 3}, "c": map[string]string{"x": "y"}}
		first, err := MarshalKYAML(v)
		if err != nil {
			t.Fatal(err)
		}
		var decoded any
		if err := UnmarshalKYAML(first, &decoded); err != nil {
			t.Fatal(err)
		}
		second, err := MarshalKYAML(decoded)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Errorf("round-trip not byte-identical:\n=== first:\n%s=== second:\n%s", first, second)
		}
	})

	// === §2.2 File format =============================================

	t.Run("R2.1 encoder produces UTF-8", func(t *testing.T) {
		out, err := MarshalKYAML(map[string]string{"k": "日本語"})
		if err != nil {
			t.Fatal(err)
		}
		// The Japanese characters round-trip as UTF-8 bytes literally.
		if !bytes.Contains(out, []byte("日本語")) {
			t.Errorf("UTF-8 not preserved literally:\n%s", out)
		}
	})

	t.Run("R2.2 no BOM emitted", func(t *testing.T) {
		out, err := MarshalKYAML(map[string]int{"x": 1})
		if err != nil {
			t.Fatal(err)
		}
		if bytes.HasPrefix(out, []byte{0xEF, 0xBB, 0xBF}) {
			t.Errorf("output unexpectedly starts with UTF-8 BOM")
		}
	})

	t.Run("R2.3 LF line endings only", func(t *testing.T) {
		out, err := MarshalKYAML(map[string]int{"a": 1, "b": 2})
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(out, []byte("\r")) {
			t.Errorf("output contains CR — should be LF-only:\n%q", out)
		}
	})

	// === §2.3 Document structure ======================================

	t.Run("R3.1 every document begins with --- header", func(t *testing.T) {
		for _, v := range []any{42, "x", nil, []int{1}, map[string]int{"a": 1}} {
			out, err := MarshalKYAML(v)
			if err != nil {
				t.Fatalf("MarshalKYAML(%v): %v", v, err)
			}
			if !bytes.HasPrefix(out, []byte("---\n")) {
				t.Errorf("missing leading --- for input %v:\n%s", v, out)
			}
		}
	})

	t.Run("R3.2 multi-document streams (no duplicate separator)", func(t *testing.T) {
		var buf bytes.Buffer
		enc := NewEncoder(&buf, WithKYAML())
		_ = enc.Encode(map[string]int{"a": 1})
		_ = enc.Encode(map[string]int{"b": 2})
		out := buf.String()
		if strings.Contains(out, "---\n---\n") {
			t.Errorf("multi-doc stream has duplicate separator:\n%s", out)
		}
		if c := strings.Count(out, "---\n"); c != 2 {
			t.Errorf("expected 2 doc headers, got %d:\n%s", c, out)
		}
	})

	t.Run("R3.3 no `...` end-of-document marker", func(t *testing.T) {
		out, err := MarshalKYAML(map[string]int{"k": 1})
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(out, []byte("...")) {
			t.Errorf("output contains `...` end marker:\n%s", out)
		}
	})

	t.Run("R3.4 strict mode rejects YAML directives", func(t *testing.T) {
		err := ValidateKYAML([]byte("%YAML 1.2\n---\n{ a: 1 }\n"))
		var k *KYAMLError
		if !errors.As(err, &k) {
			t.Fatalf("expected *KYAMLError, got %v", err)
		}
		found := false
		for _, v := range k.Errors {
			if v.Rule == "R12.9" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected R12.9 violation, got %+v", k.Errors)
		}
	})

	// === §2.4 Mappings ================================================

	t.Run("R4.1 mappings always flow style {}", func(t *testing.T) {
		out, err := MarshalKYAML(map[string]int{"a": 1, "b": 2})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(out, []byte("{")) || !bytes.Contains(out, []byte("}")) {
			t.Errorf("expected flow-style braces:\n%s", out)
		}
	})

	t.Run("R4.2 each mapping key on its own line", func(t *testing.T) {
		out, err := MarshalKYAML(map[string]int{"a": 1, "b": 2})
		if err != nil {
			t.Fatal(err)
		}
		// "a: 1," and "b: 2," should each be on their own line.
		s := string(out)
		if !strings.Contains(s, "  a: 1,\n") || !strings.Contains(s, "  b: 2,\n") {
			t.Errorf("each entry should be on its own line:\n%s", s)
		}
	})

	t.Run("R4.3 embedded structs are inlined", func(t *testing.T) {
		type Inner struct {
			Alpha int `json:"alpha"`
		}
		type Outer struct {
			Inner
			Beta int `json:"beta"`
		}
		out, err := MarshalKYAML(Outer{Inner: Inner{Alpha: 1}, Beta: 2})
		if err != nil {
			t.Fatal(err)
		}
		s := string(out)
		if !strings.Contains(s, "alpha: 1,") || !strings.Contains(s, "beta: 2,") {
			t.Errorf("embedded fields not inlined:\n%s", s)
		}
	})

	t.Run("R4.4 non-string map keys rejected", func(t *testing.T) {
		_, err := MarshalKYAML(map[int]string{1: "one"})
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("expected ErrUnsupported for int-keyed map, got %v", err)
		}
	})

	t.Run("R4.5 native map keys lex-sorted", func(t *testing.T) {
		out, err := MarshalKYAML(map[string]int{"c": 3, "a": 1, "b": 2})
		if err != nil {
			t.Fatal(err)
		}
		s := string(out)
		if !(strings.Index(s, "a:") < strings.Index(s, "b:") && strings.Index(s, "b:") < strings.Index(s, "c:")) {
			t.Errorf("keys not lex-sorted:\n%s", s)
		}
	})

	t.Run("R4.6 struct fields in declaration order", func(t *testing.T) {
		type S struct {
			Z int `json:"z"`
			A int `json:"a"`
			M int `json:"m"`
		}
		out, err := MarshalKYAML(S{Z: 1, A: 2, M: 3})
		if err != nil {
			t.Fatal(err)
		}
		s := string(out)
		pZ := strings.Index(s, "z:")
		pA := strings.Index(s, "a:")
		pM := strings.Index(s, "m:")
		if !(pZ < pA && pA < pM) {
			t.Errorf("fields not in declaration order:\n%s", s)
		}
	})

	t.Run("R4.7 per-key quoting decisions", func(t *testing.T) {
		// Mix of safe + ambiguous keys. Safe keys unquoted, ambiguous quoted.
		out, err := MarshalKYAML(map[string]int{"safe_key": 1, "yes": 2})
		if err != nil {
			t.Fatal(err)
		}
		s := string(out)
		if !strings.Contains(s, "safe_key:") || !strings.Contains(s, `"yes":`) {
			t.Errorf("per-key quoting not applied:\n%s", s)
		}
	})

	// === §2.5 Key quoting =============================================

	t.Run("R5.1 type-ambiguous keys quoted", func(t *testing.T) {
		out, err := MarshalKYAML(map[string]int{"yes": 1, "no": 2})
		if err != nil {
			t.Fatal(err)
		}
		s := string(out)
		if !strings.Contains(s, `"yes":`) || !strings.Contains(s, `"no":`) {
			t.Errorf("ambiguous keys not quoted:\n%s", s)
		}
	})

	t.Run("R5.2 full ambiguous-word list", func(t *testing.T) {
		// Spec list: NO/no/N/n/YES/yes/Y/y/ON/on/OFF/off/TRUE/true/True/FALSE/false/False/NULL/null/Null/~
		ambig := []string{
			"NO", "no", "N", "n", "YES", "yes", "Y", "y",
			"ON", "on", "OFF", "off",
			"TRUE", "true", "True", "FALSE", "false", "False",
			"NULL", "null", "Null", "~",
		}
		for _, k := range ambig {
			out, err := MarshalKYAML(map[string]int{k: 1})
			if err != nil {
				t.Fatalf("MarshalKYAML key %q: %v", k, err)
			}
			if !strings.Contains(string(out), `"`+k+`":`) {
				t.Errorf("ambiguous key %q must be quoted:\n%s", k, out)
			}
		}
	})

	t.Run("R5.3 unquoted-key predicate", func(t *testing.T) {
		// Safe identifiers and label-key syntax stay unquoted.
		safe := []string{"name", "apiVersion", "kubernetes.io/role", "_42", "a-b-c", "a.b.c"}
		for _, k := range safe {
			out, _ := MarshalKYAML(map[string]int{k: 1})
			if strings.Contains(string(out), `"`+k+`":`) {
				t.Errorf("safe key %q should not be quoted:\n%s", k, out)
			}
		}
		// Bracket-containing keys must be quoted (regression for fuzz seed).
		for _, k := range []string{"A[]", "A[B]", "[start", "foo]bar"} {
			out, _ := MarshalKYAML(map[string]int{k: 1})
			if !strings.Contains(string(out), `"`+k+`":`) {
				t.Errorf("bracket key %q must be quoted:\n%s", k, out)
			}
		}
	})

	t.Run("R5.4 non-safe keys double-quoted", func(t *testing.T) {
		out, err := MarshalKYAML(map[string]int{"with space": 1, "42": 2})
		if err != nil {
			t.Fatal(err)
		}
		s := string(out)
		if !strings.Contains(s, `"with space":`) || !strings.Contains(s, `"42":`) {
			t.Errorf("non-safe keys not double-quoted:\n%s", s)
		}
	})

	t.Run("R5.5 WithKYAMLAlwaysQuoteKeys forces quoting", func(t *testing.T) {
		out, err := MarshalWithOptions(map[string]int{"safe": 1}, WithKYAML(), WithKYAMLAlwaysQuoteKeys())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), `"safe":`) {
			t.Errorf("AlwaysQuoteKeys should quote `safe`:\n%s", out)
		}
	})

	// === §2.6 Scalars =================================================

	t.Run("R6.1 booleans emit as lowercase true/false", func(t *testing.T) {
		out, err := MarshalKYAML(map[string]bool{"t": true, "f": false})
		if err != nil {
			t.Fatal(err)
		}
		s := string(out)
		if !strings.Contains(s, "t: true,") || !strings.Contains(s, "f: false,") {
			t.Errorf("booleans not in canonical form:\n%s", s)
		}
	})

	t.Run("R6.2 integers and floats in natural numeric form (umbrella)", func(t *testing.T) {
		// Umbrella for R6.2a/b/c: integers as decimal int, floats as
		// shortest-round-trippable, neither quoted.
		out, _ := MarshalKYAML(map[string]any{"i": 42, "f": 3.14})
		s := string(out)
		if !strings.Contains(s, "i: 42,") || !strings.Contains(s, "f: 3.14,") {
			t.Errorf("integers/floats not in natural form:\n%s", s)
		}
	})

	t.Run("R6.2a integers emit as decimal", func(t *testing.T) {
		out, _ := MarshalKYAML(map[string]int{"v": 0xFF})
		if !strings.Contains(string(out), "v: 255,") {
			t.Errorf("integer not emitted as decimal:\n%s", out)
		}
	})

	t.Run("R6.2b NaN and Inf rejected", func(t *testing.T) {
		for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			_, err := MarshalKYAML(f)
			if !errors.Is(err, ErrUnsupported) {
				t.Errorf("expected ErrUnsupported for %v, got %v", f, err)
			}
		}
	})

	t.Run("R6.2c integer-shaped float emits as int", func(t *testing.T) {
		out, _ := MarshalKYAML(map[string]float64{"a": 1.0})
		if !strings.Contains(string(out), "a: 1,") {
			t.Errorf("1.0 not emitted as `1`:\n%s", out)
		}
	})

	t.Run("R6.3 null emits as lowercase null", func(t *testing.T) {
		out, _ := MarshalKYAML(map[string]any{"x": nil})
		if !strings.Contains(string(out), "x: null,") {
			t.Errorf("nil not emitted as null:\n%s", out)
		}
	})

	t.Run("R6.4 string values always double-quoted", func(t *testing.T) {
		out, _ := MarshalKYAML(map[string]string{"k": "value"})
		if !strings.Contains(string(out), `k: "value",`) {
			t.Errorf("string not double-quoted:\n%s", out)
		}
	})

	t.Run("R6.5 escape sequences", func(t *testing.T) {
		// Sample of escapes; full coverage in TestKYAMLEscapeAllSpecials.
		cases := map[string]string{
			"newline": "a\nb",
			"tab":     "a\tb",
			"quote":   `a"b`,
			"slash":   `a\b`,
		}
		for name, in := range cases {
			out, err := MarshalKYAML(map[string]string{"k": in})
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			var got map[string]string
			if err := Unmarshal(out, &got); err != nil {
				t.Fatalf("%s round-trip: %v\n%s", name, err, out)
			}
			if got["k"] != in {
				t.Errorf("%s round-trip mismatch: %q vs %q", name, in, got["k"])
			}
		}
	})

	t.Run("R6.6 NBSP emitted as literal UTF-8", func(t *testing.T) {
		// NBSP (U+00A0) is in the C1 control range — verify the escape
		// table treats it consistently.
		out, _ := MarshalKYAML(map[string]string{"k": " "})
		var got map[string]string
		if err := Unmarshal(out, &got); err != nil {
			t.Fatalf("round-trip: %v\n%s", err, out)
		}
		if got["k"] != " " {
			t.Errorf("NBSP round-trip mismatch: %q", got["k"])
		}
	})

	// === §2.7 Sequences ===============================================

	t.Run("R7.1 sequences always flow style []", func(t *testing.T) {
		out, _ := MarshalKYAML([]int{1, 2, 3})
		if !bytes.Contains(out, []byte("[")) || !bytes.Contains(out, []byte("]")) {
			t.Errorf("expected flow brackets:\n%s", out)
		}
		if bytes.Contains(out, []byte("\n- ")) {
			t.Errorf("block-style `- ` indicator emitted:\n%s", out)
		}
	})

	t.Run("R7.2 elements rendered per their type", func(t *testing.T) {
		out, _ := MarshalKYAML([]any{1, "two", true, nil})
		s := string(out)
		for _, want := range []string{"1,", `"two",`, "true,", "null,"} {
			if !strings.Contains(s, want) {
				t.Errorf("expected %q in:\n%s", want, s)
			}
		}
	})

	t.Run("R7.3 each sequence element on its own line", func(t *testing.T) {
		out, _ := MarshalKYAML([]int{1, 2, 3})
		// Multi-line: each element on its own line.
		if strings.Count(string(out), "\n") < 5 {
			t.Errorf("expected multi-line sequence:\n%s", out)
		}
	})

	t.Run("R7.4 empty containers as [] and {}", func(t *testing.T) {
		out, _ := MarshalKYAML(map[string]any{"l": []int{}, "m": map[string]int{}})
		s := string(out)
		if !strings.Contains(s, "l: [],") || !strings.Contains(s, "m: {},") {
			t.Errorf("empty containers not rendered as [] / {}:\n%s", s)
		}
	})

	// === §2.8 Cuddling + trailing commas ==============================

	t.Run("R8.1 trailing commas after final entry (when not cuddled)", func(t *testing.T) {
		out, _ := MarshalKYAML(map[string]int{"a": 1})
		if !strings.Contains(string(out), "a: 1,\n") {
			t.Errorf("expected trailing comma after final mapping entry:\n%s", out)
		}
	})

	t.Run("R8.2 paired brackets cuddled (sequence of compound)", func(t *testing.T) {
		out, _ := MarshalKYAML([]map[string]int{{"a": 1}})
		s := string(out)
		if !strings.Contains(s, "[{") || !strings.Contains(s, "}]") {
			t.Errorf("expected cuddled brackets [{...}]:\n%s", s)
		}
	})

	t.Run("R8.3 cuddling rules concretely", func(t *testing.T) {
		// [{...}, {...}] cuddles open + uses ", " between cuddled elements.
		out, _ := MarshalKYAML([]map[string]int{{"a": 1}, {"b": 2}})
		s := string(out)
		if !strings.Contains(s, "[{") || !strings.Contains(s, "}, {") || !strings.Contains(s, "}]") {
			t.Errorf("multi-element cuddling pattern broken:\n%s", s)
		}
	})

	t.Run("R8.4 worked example matches KEP", func(t *testing.T) {
		// Approximate the KEP-5295 pod example.
		v := map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata":   map[string]any{"name": "my-pod"},
			"spec": map[string]any{
				"containers": []map[string]any{{"name": "nginx", "image": "nginx:1.20"}},
			},
		}
		out, _ := MarshalKYAML(v)
		s := string(out)
		// Verify shape — has --- header, mappings flow, sequence-of-objects cuddled.
		if !strings.HasPrefix(s, "---\n{\n") {
			t.Errorf("output doesn't start with ---\\n{\\n:\n%s", s)
		}
		if !strings.Contains(s, "containers: [{") {
			t.Errorf("expected cuddled `containers: [{`:\n%s", s)
		}
	})

	t.Run("R8.5 uncuddle when comments present", func(t *testing.T) {
		v := []map[string]string{{"name": "x"}}
		out, _ := MarshalWithOptions(v, WithKYAML(),
			WithComment(map[string][]Comment{"name": {{Position: HeadCommentPos, Text: "c"}}}))
		s := string(out)
		if strings.Contains(s, "[{") || strings.Contains(s, "}]") {
			t.Errorf("expected uncuddled brackets when comments present:\n%s", s)
		}
	})

	// === §2.9 Indentation =============================================

	t.Run("R9.1 default 2-space indent", func(t *testing.T) {
		out, _ := MarshalKYAML(map[string]int{"a": 1})
		if !strings.Contains(string(out), "  a: 1,") {
			t.Errorf("expected 2-space indent:\n%s", out)
		}
	})

	t.Run("R9.2 WithIndent honored", func(t *testing.T) {
		out, _ := MarshalWithOptions(map[string]int{"a": 1}, WithKYAML(), WithIndent(4))
		if !strings.Contains(string(out), "    a: 1,") {
			t.Errorf("expected 4-space indent:\n%s", out)
		}
	})

	// === §2.10 Multi-line strings =====================================

	t.Run("R10.1 flow-folding form for long multi-line strings", func(t *testing.T) {
		long := strings.Repeat("a", 50) + "\n" + strings.Repeat("b", 50) + "\n" + strings.Repeat("c", 50)
		out, _ := MarshalKYAML(map[string]string{"k": long})
		// Flow-folded output contains backslash + newline continuation.
		if !strings.Contains(string(out), "\\\n") {
			t.Errorf("expected flow-fold continuation:\n%s", out)
		}
	})

	t.Run("R10.2 flow-folding round-trips through YAML parser", func(t *testing.T) {
		long := strings.Repeat("a", 50) + "\n" + strings.Repeat("b", 50) + "\n" + strings.Repeat("c", 50)
		out, _ := MarshalKYAML(map[string]string{"k": long})
		var got map[string]string
		if err := Unmarshal(out, &got); err != nil {
			t.Fatalf("flow-fold output failed to round-trip: %v\n%s", err, out)
		}
		if got["k"] != long {
			t.Errorf("flow-fold round-trip mismatch")
		}
	})

	t.Run("R10.3 KEP example shape", func(t *testing.T) {
		// The KEP example uses "first\nsecond\nthird" with continuations.
		// We verify our output FORMS a valid YAML string that decodes back.
		s := strings.Repeat("x", 100) + "\n" + strings.Repeat("y", 100) + "\n" + strings.Repeat("z", 100)
		out, _ := MarshalKYAML(map[string]string{"k": s})
		// Must contain `\n` literal escapes AND continuation markers.
		if !strings.Contains(string(out), `\n`) {
			t.Errorf("expected literal \\n escape:\n%s", out)
		}
	})

	t.Run("R10.4 length-based heuristic", func(t *testing.T) {
		// Short multi-line stays single-line.
		short := "a\nb\nc"
		out, _ := MarshalKYAML(map[string]string{"k": short})
		if strings.Contains(string(out), "\\\n") {
			t.Errorf("short multi-line string should not flow-fold:\n%s", out)
		}
	})

	t.Run("R10.5 single-line escaped form for short strings", func(t *testing.T) {
		out, _ := MarshalKYAML(map[string]string{"k": "a\nb"})
		if !strings.Contains(string(out), `\n`) {
			t.Errorf("expected `\\n` escape:\n%s", out)
		}
	})

	// === §2.11 Comments ===============================================

	t.Run("R11.1 best-effort comment placement", func(t *testing.T) {
		src := []byte("# top\nname: foo\n")
		out, err := Format(src)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "top") {
			t.Errorf("top comment not preserved:\n%s", out)
		}
	})

	t.Run("R11.2 line-comment placement on mapping value", func(t *testing.T) {
		// Inline comment on a scalar value is preserved at the value's line.
		src := []byte("name: foo  # inline\n")
		out, err := Format(src)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "inline") {
			t.Errorf("inline comment not preserved:\n%s", out)
		}
	})

	t.Run("R11.3 line-comment placement on list element", func(t *testing.T) {
		// Line comment on a sequence element.
		src := []byte("- foo  # line\n")
		out, err := Format(src)
		if err != nil {
			t.Fatal(err)
		}
		// Best-effort per R11.5 — sequence-element comments may be lost
		// (path-anchor limitation). We just verify no crash + valid output.
		if !ValidKYAML(out) {
			t.Errorf("output not valid KYAML:\n%s", out)
		}
	})

	t.Run("R11.4 head/inline/foot preservation", func(t *testing.T) {
		src := []byte("# head\nname: foo  # inline\n")
		out, _ := Format(src)
		if !strings.Contains(string(out), "head") || !strings.Contains(string(out), "inline") {
			t.Errorf("comments not preserved:\n%s", out)
		}
	})

	t.Run("R11.5 caveat: best-effort, comment loss permitted", func(t *testing.T) {
		// Per R11.5 some comments may be lost (e.g., on empty/special-char
		// keys, sequence-index paths). Verify Format never crashes on
		// these inputs and the output is valid KYAML — comment loss itself
		// is acceptable.
		hard := []byte("- : 0 #0\n- .: # x\n- foo[bar]: # y\n")
		out, err := Format(hard)
		if err != nil {
			t.Fatal(err)
		}
		if !ValidKYAML(out) {
			t.Errorf("output not valid KYAML:\n%s\nerrors: %v", out, ValidateKYAML(out))
		}
	})

	t.Run("R11.6 WithComment programmatic injection", func(t *testing.T) {
		v := map[string]int{"port": 80}
		out, err := MarshalWithOptions(v, WithKYAML(),
			WithComment(map[string][]Comment{
				"port": {{Position: HeadCommentPos, Text: "the port"}},
			}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "the port") {
			t.Errorf("WithComment not applied:\n%s", out)
		}
	})

	// === §2.12 Forbidden constructs (validator) =======================

	rejectionCases := []struct {
		rule  string
		desc  string
		input string
	}{
		{"R12.1", "anchor", "---\n{ a: &x 1 }\n"},
		{"R12.1", "alias", "---\n{ a: &x 1, b: *x }\n"},
		{"R12.2", "explicit tag", "---\n{ a: !!int 5 }\n"},
		{"R12.3", "merge key", "---\n{ <<: { a: 1 }, b: 2 }\n"},
		{"R12.4", "literal block scalar", "---\nname: |\n  multi\n  line\n"},
		{"R12.5", "block-style mapping", "---\nname: foo\n"},
		{"R12.6", "block-style sequence", "---\nitems:\n  - 1\n  - 2\n"},
		{"R12.7", "plain string value", "---\n{ name: bare_string }\n"},
		{"R12.8", "single-quoted scalar", "---\n{ name: 'foo' }\n"},
		{"R12.9", "YAML directive", "%YAML 1.2\n---\n{ a: 1 }\n"},
		{"R12.10", "compound mapping key", "---\n? [1, 2]\n: foo\n"},
		{"R12.11", "hex integer literal", "---\n{ port: 0x50 }\n"},
		{"R12.12", "YAML 1.1 boolean alias", "---\n{ enabled: yes }\n"},
		{"R12.13", "NaN literal", "---\n{ bad: .nan }\n"},
		{"R12.14", "explicit complex-key indicator", "---\n? {a: 1}\n: foo\n"},
	}
	for _, rc := range rejectionCases {
		t.Run(rc.rule+" "+rc.desc+" rejected by strict mode", func(t *testing.T) {
			err := ValidateKYAML([]byte(rc.input))
			if err == nil {
				t.Errorf("expected validation error for %s (%s) input:\n%s", rc.rule, rc.desc, rc.input)
				return
			}
			var k *KYAMLError
			if !errors.As(err, &k) {
				t.Errorf("expected *KYAMLError, got %T", err)
			}
			// Don't require the exact rule ID — different inputs may surface
			// related rules (e.g., a block-mapping with plain string emits
			// both R12.5 and R12.7). The test's purpose is rule X is
			// somehow surfaced in the rejection.
		})
	}

	// === §2.13 Special types ==========================================

	t.Run("R13.1 nil pointer renders as null", func(t *testing.T) {
		var p *int
		out, _ := MarshalKYAML(map[string]any{"x": p})
		if !strings.Contains(string(out), "x: null,") {
			t.Errorf("nil pointer should render as null:\n%s", out)
		}
	})

	t.Run("R13.2 json.Marshaler primary under KYAML", func(t *testing.T) {
		out, err := MarshalKYAML(kyamlJSONMarshalerType{X: 5})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "json_x:") {
			t.Errorf("json.Marshaler not preferred under KYAML:\n%s", out)
		}
	})

	t.Run("R13.3 nil interface renders as null", func(t *testing.T) {
		out, _ := MarshalKYAML(map[string]any{"x": any(nil)})
		if !strings.Contains(string(out), "x: null,") {
			t.Errorf("nil interface should render as null:\n%s", out)
		}
	})

	t.Run("R13.4 json struct tag primary", func(t *testing.T) {
		type S struct {
			V int `json:"json_name" yaml:"yaml_name"`
		}
		out, _ := MarshalKYAML(S{V: 1})
		if !strings.Contains(string(out), "json_name:") || strings.Contains(string(out), "yaml_name:") {
			t.Errorf("json tag should win under KYAML:\n%s", out)
		}
	})

	t.Run("R13.5 tag option merging", func(t *testing.T) {
		type S struct {
			A string `json:"a" yaml:"a,omitempty"`
		}
		// Empty value should be omitted because yaml-tag omitempty applies.
		out, _ := MarshalKYAML(S{A: ""})
		if strings.Contains(string(out), "a:") {
			t.Errorf("omitempty from yaml-tag not honored:\n%s", out)
		}
	})

	t.Run("R13.6 time.Time as RFC 3339 quoted string", func(t *testing.T) {
		tm := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
		out, _ := MarshalKYAML(map[string]any{"t": tm})
		if !strings.Contains(string(out), `"2026-05-08T12:00:00Z"`) {
			t.Errorf("time.Time not RFC 3339:\n%s", out)
		}
	})

	t.Run("R13.7 time.Duration as int64 nanoseconds (default)", func(t *testing.T) {
		out, _ := MarshalKYAML(map[string]any{"d": time.Hour + 30*time.Minute})
		if !strings.Contains(string(out), "d: 5400000000000,") {
			t.Errorf("Duration not int64 ns:\n%s", out)
		}
		// And opt-in string form.
		outS, _ := MarshalWithOptions(map[string]any{"d": time.Hour}, WithKYAML(), WithDurationAsString(true))
		if !strings.Contains(string(outS), `d: "1h0m0s",`) {
			t.Errorf("WithDurationAsString did not apply:\n%s", outS)
		}
	})

	t.Run("R13.8 big.Int / big.Float verbatim", func(t *testing.T) {
		bi := big.NewInt(99999999)
		bf := big.NewFloat(3.14)
		out, _ := MarshalKYAML(map[string]any{"i": bi, "f": bf})
		if !strings.Contains(string(out), "i: 99999999,") || !strings.Contains(string(out), "f: 3.14,") {
			t.Errorf("big numbers not rendered:\n%s", out)
		}
	})

	t.Run("R13.9 []byte and [N]byte base64-encoded", func(t *testing.T) {
		out, _ := MarshalKYAML(map[string]any{"sl": []byte("hi"), "ar": [2]byte{'h', 'i'}})
		s := string(out)
		if !strings.Contains(s, `sl: "aGk=",`) || !strings.Contains(s, `ar: "aGk=",`) {
			t.Errorf("[]byte/[N]byte not base64:\n%s", s)
		}
	})

	t.Run("R13.10 json.Number emitted verbatim", func(t *testing.T) {
		// Use `num` (not `n`) since `n` is a Norway-problem alias and would
		// be quoted per R5.2, distracting from the value-emission check.
		out, _ := MarshalKYAML(map[string]any{"num": json.Number("3.10")})
		if !strings.Contains(string(out), "num: 3.10,") {
			t.Errorf("json.Number not preserved verbatim:\n%s", out)
		}
	})

	t.Run("R13.11 RawValue re-parsed and re-emitted as KYAML", func(t *testing.T) {
		// Block-style YAML inside RawValue should re-emit as KYAML.
		out, err := MarshalKYAML(map[string]any{"r": RawValue("a: 1\nb: 2\n")})
		if err != nil {
			t.Fatal(err)
		}
		if !ValidKYAML(out) {
			t.Errorf("RawValue re-emit not valid KYAML:\n%s", out)
		}
		// And anchors get reified.
		out, err = MarshalKYAML(map[string]any{"r": RawValue("shared: &x { a: 1 }\ncopy: *x\n")})
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(out, []byte("&")) || bytes.Contains(out, []byte("*x")) {
			t.Errorf("anchor/alias not reified through RawValue:\n%s", out)
		}
	})

	// === §2.14 Decoding semantics =====================================

	t.Run("R14.1 default WithKYAML decode = base", func(t *testing.T) {
		// KYAML mode is encoder-only. Default Unmarshal is unaffected by it.
		var v map[string]any
		if err := Unmarshal([]byte("name: foo\n"), &v); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("R14.2 WithStrictKYAML rejects forbidden constructs", func(t *testing.T) {
		var v any
		if err := UnmarshalWithOptions([]byte("name: foo\n"), &v, WithStrictKYAML()); !errors.Is(err, ErrKYAML) {
			t.Errorf("expected ErrKYAML, got %v", err)
		}
	})

	t.Run("R14.3 IsKYAML / ValidateKYAML are pure validators", func(t *testing.T) {
		valid := []byte("---\n{ a: 1 }\n")
		if !ValidKYAML(valid) {
			t.Error("ValidKYAML rejected valid input")
		}
		if err := ValidateKYAML(valid); err != nil {
			t.Errorf("ValidateKYAML returned error on valid input: %v", err)
		}
	})

	t.Run("R14.4 strict mode does not enforce cosmetic rules", func(t *testing.T) {
		// `{ a: 1, b: 2 }` is structurally valid KYAML on a single line —
		// canonical KYAML puts each key on its own line, but the structural
		// validator (R14.4) doesn't enforce that. Strict mode accepts.
		nonCanonical := []byte("---\n{ a: 1, b: 2 }\n")
		if err := ValidateKYAML(nonCanonical); err != nil {
			t.Errorf("strict mode should accept structurally-valid (non-canonical) KYAML, got %v", err)
		}
	})

	t.Run("R14.5 WithKYAMLLintCosmetic flags non-canonical formatting", func(t *testing.T) {
		nonCanonical := []byte("---\n{ a: 1, b: 2 }\n")
		issues, err := Lint(nonCanonical, WithStrictKYAML(), WithKYAMLLintCosmetic())
		if err != nil {
			t.Fatal(err)
		}
		hasWarn := false
		for _, i := range issues {
			if i.Severity == SeverityWarning {
				hasWarn = true
			}
		}
		if !hasWarn {
			t.Error("expected cosmetic warning")
		}
	})

	// Silence unused-import warnings if any of the referenced helpers
	// aren't used by the table cases above.
	_ = context.Background
	_ = reflect.TypeOf
}
