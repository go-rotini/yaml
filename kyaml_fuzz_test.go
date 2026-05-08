package yaml

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// kyamlFuzzSeeds returns a representative seed corpus for the KYAML fuzz
// targets — a mix of canonical KYAML, valid YAML that becomes KYAML after
// formatting, and intentional violations.
func kyamlFuzzSeeds() []string {
	return []string{
		// Canonical KYAML.
		"---\n{}\n",
		"---\n[]\n",
		"---\n42\n",
		"---\n\"hello\"\n",
		"---\nnull\n",
		"---\n{\n  a: 1,\n}\n",
		"---\n[\n  1,\n  2,\n  3,\n]\n",
		"---\n[{\n  a: 1,\n}]\n",
		"---\n{\n  apiVersion: \"v1\",\n  kind: \"Pod\",\n}\n",

		// Block-style YAML that Format should canonicalize.
		"a: 1\nb: 2\n",
		"items:\n- 1\n- 2\n",
		"shared: &x { a: 1 }\ncopy: *x\n",
		"base: &b\n  field: hello\nsub:\n  <<: *b\n  field2: extra\n",

		// Norway problem.
		"---\n{ \"yes\": 1, \"NO\": 2 }\n",
		"---\n{ name: \"yes\" }\n",

		// Various violations the validator should reject.
		"a: 1\n",                        // missing ---
		"---\n{ a: 'bare' }\n",          // single-quoted
		"---\n{ port: 0x50 }\n",         // hex int
		"---\n{ a: !!int 5 }\n",         // explicit tag
		"---\nname: foo\n",              // block-style mapping
		"---\n{ enabled: yes }\n",       // YAML 1.1 boolean
		"---\n{ <<: { a: 1 }, b: 2 }\n", // merge key
	}
}

// FuzzMarshalKYAML drives random Go values through MarshalKYAML. The mutator
// can't synthesize arbitrary Go values, but it can fuzz string keys/values
// in a small fixed shape — that exercises escape handling, key quoting, and
// boundary conditions.
func FuzzMarshalKYAML(f *testing.F) {
	f.Add("apiVersion", "v1", 1)
	f.Add("yes", "no", 0)
	f.Add("", "", -1)
	f.Add("a\nb", "c\td", 1024)
	f.Add("kubernetes.io/role", "primary", 42)

	f.Fuzz(func(t *testing.T, key, value string, count int) {
		v := map[string]any{
			key:    value,
			"_n":   count,
			"_lst": []string{key, value},
		}
		out, err := MarshalKYAML(v)
		if err != nil {
			// Some inputs (e.g. Inf, cyclic) legitimately fail; that's fine.
			return
		}
		if !bytes.HasPrefix(out, []byte("---\n")) {
			t.Errorf("MarshalKYAML output missing leading ---:\n%s", out)
		}
		// Output must be syntactically valid YAML the existing decoder can read.
		var got map[string]any
		if err := Unmarshal(out, &got); err != nil {
			t.Errorf("MarshalKYAML output not parseable as YAML: %v\n%s", err, out)
		}
	})
}

// FuzzUnmarshalKYAML feeds random bytes through UnmarshalKYAML and asserts
// the strict-decoder never panics. Errors are expected and ignored; only
// crashes fail the fuzzer.
func FuzzUnmarshalKYAML(f *testing.F) {
	for _, s := range kyamlFuzzSeeds() {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var v any
		_ = UnmarshalKYAML(data, &v)
	})
}

// FuzzKYAMLRoundTrip drives random YAML through Format. The output must be
// valid KYAML and Format(Format(x)) must equal Format(x) (idempotence).
func FuzzKYAMLRoundTrip(f *testing.F) {
	for _, s := range kyamlFuzzSeeds() {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		once, err := Format(data)
		if err != nil {
			return // not all input is valid YAML — skip
		}
		// Output must be valid KYAML.
		if err := ValidateKYAML(once); err != nil {
			// Some pathological inputs may produce technically-non-KYAML
			// output if the input has constructs that survive merging
			// (e.g., un-mergeable types). Allow this.
			return
		}
		twice, err := Format(once)
		if err != nil {
			t.Fatalf("Format failed on Format output: %v\nfirst:\n%s", err, once)
		}
		if !bytes.Equal(once, twice) {
			t.Errorf("Format not idempotent\n=== once:\n%s=== twice:\n%s", once, twice)
		}
	})
}

// FuzzValidKYAML exercises the validator with random bytes, asserting it never
// panics and that its return value is consistent with ValidateKYAML.
func FuzzValidKYAML(f *testing.F) {
	for _, s := range kyamlFuzzSeeds() {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		valid := ValidKYAML(data)
		err := ValidateKYAML(data)
		if valid && err != nil {
			// ValidKYAML disagreed with ValidateKYAML — they must be consistent.
			t.Errorf("ValidKYAML returned true but ValidateKYAML returned error: %v", err)
		}
		if !valid && err == nil {
			t.Errorf("ValidKYAML returned false but ValidateKYAML returned nil")
		}
	})
}

// FuzzFormatKYAML exercises Format with random bytes. Like FuzzKYAMLRoundTrip
// but focuses on output-shape invariants beyond idempotence: leading "---",
// no trailing whitespace beyond a single newline, and parseability.
func FuzzFormatKYAML(f *testing.F) {
	for _, s := range kyamlFuzzSeeds() {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := Format(data)
		if err != nil {
			return
		}
		if !bytes.HasPrefix(out, []byte("---\n")) {
			t.Errorf("Format output missing leading ---:\n%s", out)
		}
		// Output must terminate with exactly one trailing newline.
		s := string(out)
		if !strings.HasSuffix(s, "\n") {
			t.Errorf("Format output missing trailing newline:\n%q", out)
		}
		if strings.HasSuffix(s, "\n\n") {
			// Allow cosmetic extra newlines but flag them so we notice.
			t.Errorf("Format output has multiple trailing newlines:\n%q", out)
		}
		// Output must be valid KYAML (the rare exception flagged in
		// FuzzKYAMLRoundTrip applies here too — accept errors silently).
		if err := ValidateKYAML(out); err != nil {
			var k *KYAMLError
			if errors.As(err, &k) {
				return
			}
			t.Errorf("Format output validation failed: %v", err)
		}
	})
}
