package yaml

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParserIndentlessSequenceNesting(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "reported case",
			src:  "a:\n  b:\n  - 0\nc: x\n",
			want: `{"a":{"b":[0]},"c":"x"}`,
		},
		{
			name: "sibling key at same level",
			src:  "a:\n  b:\n  - 0\n  d: 1\nc: x\n",
			want: `{"a":{"b":[0],"d":1},"c":"x"}`,
		},
		{
			name: "top-level indentless under key",
			src:  "b:\n- 0\n- 1\nc: x\n",
			want: `{"b":[0,1],"c":"x"}`,
		},
		{
			name: "two levels down",
			src:  "a:\n  b:\n    c:\n    - 1\n    d: 2\n  e: 3\nf: 4\n",
			want: `{"a":{"b":{"c":[1],"d":2},"e":3},"f":4}`,
		},
		{
			name: "two levels down then root key",
			src:  "a:\n  b:\n    c:\n    - 1\nf: 4\n",
			want: `{"a":{"b":{"c":[1]}},"f":4}`,
		},
		{
			name: "three levels down then each shallower level",
			src:  "a:\n  b:\n    c:\n      d:\n      - 1\n      - 2\n    e: 3\n  f: 4\ng: 5\n",
			want: `{"a":{"b":{"c":{"d":[1,2]},"e":3},"f":4},"g":5}`,
		},
		{
			name: "three levels down then root key",
			src:  "a:\n  b:\n    c:\n      d:\n      - 1\ng: 5\n",
			want: `{"a":{"b":{"c":{"d":[1]}}},"g":5}`,
		},
		{
			name: "three levels down then middle key",
			src:  "a:\n  b:\n    c:\n      d:\n      - 1\n  f: 4\n",
			want: `{"a":{"b":{"c":{"d":[1]}},"f":4}}`,
		},
		{
			name: "sequence of mappings",
			src:  "a:\n  b:\n  - k: v\n    m: n\n  - k: w\nc: x\n",
			want: `{"a":{"b":[{"k":"v","m":"n"},{"k":"w"}]},"c":"x"}`,
		},
		{
			name: "sequence of mappings with nested indentless sequence",
			src:  "a:\n  b:\n  - k:\n    - 1\n    m: n\n  d: 2\nc: x\n",
			want: `{"a":{"b":[{"k":[1],"m":"n"}],"d":2},"c":"x"}`,
		},
		{
			name: "sequence of mappings then root key",
			src:  "a:\n  b:\n  - k:\n      deep: v\nc: x\n",
			want: `{"a":{"b":[{"k":{"deep":"v"}}]},"c":"x"}`,
		},
		{
			name: "sequence ending at EOF",
			src:  "a:\n  b:\n  - 0\n  - 1",
			want: `{"a":{"b":[0,1]}}`,
		},
		{
			name: "sequence ending at EOF three levels down",
			src:  "a:\n  b:\n    c:\n    - k: v\n",
			want: `{"a":{"b":{"c":[{"k":"v"}]}}}`,
		},
		{
			name: "comments and blank lines between",
			src:  "a:\n  b:\n  # head\n  - 0 # line\n\n  # between\n  - 1\n\n# before c\nc: x\n",
			want: `{"a":{"b":[0,1]},"c":"x"}`,
		},
		{
			name: "comment at deeper column before shallower key",
			src:  "a:\n  b:\n  - 0\n      # stray\nc: x\n",
			want: `{"a":{"b":[0]},"c":"x"}`,
		},
		{
			name: "document end marker",
			src:  "a:\n  b:\n  - 0\n...\n",
			want: `{"a":{"b":[0]}}`,
		},
	}
	for _, tt := range tests {
		for _, eol := range []string{"\n", "\r\n"} {
			name := tt.name
			if eol == "\r\n" {
				name += " CRLF"
			}
			t.Run(name, func(t *testing.T) {
				src := strings.ReplaceAll(tt.src, "\n", eol)
				var v any
				if err := Unmarshal([]byte(src), &v); err != nil {
					t.Fatalf("Unmarshal: %v", err)
				}
				got, err := json.Marshal(v)
				if err != nil {
					t.Fatalf("json.Marshal: %v", err)
				}
				if string(got) != tt.want {
					t.Errorf("got  %s\nwant %s", got, tt.want)
				}
			})
		}
	}
}

func TestParserIndentlessSequenceMultiDocument(t *testing.T) {
	src := "a:\n  b:\n  - 0\nc: x\n---\nd:\n- 1\ne: y\n"
	docs := parseNodes(t, src)
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
	for i, wantKeys := range [][]string{{"a", "c"}, {"d", "e"}} {
		root := docs[i]
		if root.kind == nodeDocument && len(root.children) == 1 {
			root = root.children[0]
		}
		if root.kind != nodeMapping {
			t.Fatalf("doc %d: expected mapping, got %v", i, root.kind)
		}
		var keys []string
		for j := 0; j < len(root.children); j += 2 {
			keys = append(keys, root.children[j].value)
		}
		if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
			t.Errorf("doc %d: keys %v, want %v", i, keys, wantKeys)
		}
	}
}
