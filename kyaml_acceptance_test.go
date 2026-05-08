package yaml

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKYAMLAcceptance walks testdata/acceptance/kyaml/ and runs every golden
// .kyaml file through Parse, Unmarshal (to map[string]any), Format, and
// idempotence checks. It also formats the corresponding _fixtures/*.yaml
// (if present) and asserts byte-equality with the golden.
//
// Real-world fixtures cover ingress, CRD, kustomization, and Helm values —
// content shapes that exercise nested objects, lists of objects, lists of
// strings, empty containers, deeply nested schemas, and quoted special
// values like "true"/"yes".
func TestKYAMLAcceptance(t *testing.T) {
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

// TestKYAMLAcceptanceRoundTripGenericMap verifies that decoding any
// acceptance fixture into map[string]any and re-marshaling produces valid
// KYAML (though not necessarily byte-identical, since map ordering may
// differ from struct declaration order — we only check structural validity).
func TestKYAMLAcceptanceRoundTripGenericMap(t *testing.T) {
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
			if !IsKYAML(out) {
				var k *KYAMLError
				if errors.As(ValidateKYAML(out), &k) {
					t.Errorf("re-marshaled output is not KYAML: %d violations", len(k.Errors))
				}
			}
		})
	}
}
