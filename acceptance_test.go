package yaml

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

var k8sYAML []byte

func init() {
	var err error
	k8sYAML, err = os.ReadFile("testdata/acceptance/k8s.yaml")
	if err != nil {
		panic("failed to read testdata/acceptance/k8s.yaml: " + err.Error())
	}
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

		origJSON, _ := json.Marshal(original)
		decJSON, _ := json.Marshal(decoded)
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
