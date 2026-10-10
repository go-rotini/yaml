package yaml

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// indentlessCase is a document using an indentless block sequence (entries at
// the same column as the parent key) and its expected decoding as JSON.
type indentlessCase struct {
	name string
	src  string
	want string
}

var indentlessNestingCases = []indentlessCase{
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

// indentlessEmptyEntryCases hold empty entries (a bare `-`) in indentless
// sequences. A following key at the parent's column belongs to the parent
// mapping, not to the entry.
var indentlessEmptyEntryCases = []indentlessCase{
	{
		name: "empty entry then sibling key",
		src:  "b:\n-\nc: 1\n",
		want: `{"b":[null],"c":1}`,
	},
	{
		name: "empty entry after an item",
		src:  "b:\n- 1\n-\nc: 1\n",
		want: `{"b":[1,null],"c":1}`,
	},
	{
		name: "empty entry one level down then root key",
		src:  "a:\n  b:\n  -\n  c: 1\nd: 2\n",
		want: `{"a":{"b":[null],"c":1},"d":2}`,
	},
	{
		name: "empty entry with comment",
		src:  "a:\n  b:\n  - # comment\n  c: 1\nd: 2\n",
		want: `{"a":{"b":[null],"c":1},"d":2}`,
	},
	{
		name: "empty entry two levels down",
		src:  "a:\n  b:\n    c:\n    -\n    d: 1\n  e: 2\nf: 3\n",
		want: `{"a":{"b":{"c":[null],"d":1},"e":2},"f":3}`,
	},
	{
		name: "empty entry two levels down then root key",
		src:  "a:\n  b:\n    c:\n    -\nf: 3\n",
		want: `{"a":{"b":{"c":[null]}},"f":3}`,
	},
	{
		name: "empty entry three levels down",
		src:  "a:\n  b:\n    c:\n      d:\n      -\n      e: 1\n    f: 2\n  g: 3\nh: 4\n",
		want: `{"a":{"b":{"c":{"d":[null],"e":1},"f":2},"g":3},"h":4}`,
	},
	{
		name: "empty entry inside a sequence of mappings",
		src:  "a:\n  - k:\n    -\n    j: 2\n  - 3\nb: 4\n",
		want: `{"a":[{"j":2,"k":[null]},3],"b":4}`,
	},
	{
		name: "consecutive empty entries",
		src:  "b:\n-\n-\nc: 1\n",
		want: `{"b":[null,null],"c":1}`,
	},
	{
		name: "consecutive empty entries one level down",
		src:  "a:\n  b:\n  -\n  -\n  c: 1\nd: 2\n",
		want: `{"a":{"b":[null,null],"c":1},"d":2}`,
	},
	{
		name: "empty entry then scalar entry",
		src:  "b:\n-\n- 1\nc: 2\n",
		want: `{"b":[null,1],"c":2}`,
	},
	{
		name: "empty entry then mapping entry",
		src:  "b:\n-\n- k: v\nc: 2\n",
		want: `{"b":[null,{"k":"v"}],"c":2}`,
	},
	{
		name: "entry content on the next line",
		src:  "b:\n-\n  c: 1\nd: 2\n",
		want: `{"b":[{"c":1}],"d":2}`,
	},
	{
		name: "empty entry at EOF",
		src:  "a:\n  b:\n  -\n  -",
		want: `{"a":{"b":[null,null]}}`,
	},
	{
		name: "comment line before sibling key",
		src:  "a:\n  b:\n  -\n  # c\n  c: 1\nd: 2\n",
		want: `{"a":{"b":[null],"c":1},"d":2}`,
	},
	{
		name: "comment at column 0 before sibling key",
		src:  "a:\n  b:\n  -\n# c\n  c: 1\nd: 2\n",
		want: `{"a":{"b":[null],"c":1},"d":2}`,
	},
	{
		name: "blank line before sibling key",
		src:  "a:\n  b:\n  -\n\n  c: 1\nd: 2\n",
		want: `{"a":{"b":[null],"c":1},"d":2}`,
	},
	{
		name: "quoted sibling key",
		src:  "b:\n-\n\"c\": 1\n",
		want: `{"b":[null],"c":1}`,
	},
	{
		name: "explicit sibling key",
		src:  "b:\n-\n? c\n: 1\n",
		want: `{"b":[null],"c":1}`,
	},
	{
		name: "empty sibling key",
		src:  "b:\n-\n: 1\n",
		want: `{"":1,"b":[null]}`,
	},
	{
		name: "sibling key with flow value",
		src:  "b:\n-\nc: [1]\n",
		want: `{"b":[null],"c":[1]}`,
	},
	{
		name: "anchored empty entry",
		src:  "b:\n- &x\nc: *x\n",
		want: `{"b":[null],"c":null}`,
	},
	{
		name: "tagged empty entry",
		src:  "a:\n  b:\n  - !!str\n  c: 1\nd: 2\n",
		want: `{"a":{"b":[""],"c":1},"d":2}`,
	},
}

func TestParserIndentlessSequenceNesting(t *testing.T) {
	runIndentlessDecodeCases(t, indentlessNestingCases)
}

func TestParserIndentlessSequenceEmptyEntry(t *testing.T) {
	runIndentlessDecodeCases(t, indentlessEmptyEntryCases)
}

// forEachEOL runs fn as a subtest for each case with LF and with CRLF line
// endings.
func forEachEOL(t *testing.T, cases []indentlessCase, fn func(t *testing.T, src string, tc indentlessCase)) {
	t.Helper()
	for _, tc := range cases {
		for _, eol := range []string{"\n", "\r\n"} {
			name := tc.name
			if eol == "\r\n" {
				name += " CRLF"
			}
			t.Run(name, func(t *testing.T) {
				fn(t, strings.ReplaceAll(tc.src, "\n", eol), tc)
			})
		}
	}
}

func decodeJSON(t *testing.T, src []byte) string {
	t.Helper()
	var v any
	if err := Unmarshal(src, &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(got)
}

func runIndentlessDecodeCases(t *testing.T, cases []indentlessCase) {
	t.Helper()
	forEachEOL(t, cases, func(t *testing.T, src string, tc indentlessCase) {
		t.Helper()
		if got := decodeJSON(t, []byte(src)); got != tc.want {
			t.Errorf("got  %s\nwant %s", got, tc.want)
		}
	})
}

func TestFormatIndentlessSequenceRoundTrip(t *testing.T) {
	cases := append(append([]indentlessCase{}, indentlessNestingCases...), indentlessEmptyEntryCases...)
	forEachEOL(t, cases, func(t *testing.T, src string, tc indentlessCase) {
		t.Helper()
		out, err := Format([]byte(src))
		if err != nil {
			t.Fatalf("Format: %v", err)
		}
		if got := decodeJSON(t, out); got != tc.want {
			t.Errorf("Format output decodes to %s\nwant %s\noutput:\n%s", got, tc.want, out)
		}
		again, err := Format(out)
		if err != nil {
			t.Fatalf("Format of Format output: %v", err)
		}
		if string(again) != string(out) {
			t.Errorf("Format is not idempotent:\nfirst:\n%s\nsecond:\n%s", out, again)
		}
	})
}

// dumpTree renders a parsed node tree as one line per node: kind, value and
// line:column, indented by depth.
func dumpTree(b *strings.Builder, n *Node, depth int) {
	kinds := map[NodeKind]string{
		DocumentNode: "doc", MappingNode: "map", SequenceNode: "seq",
		ScalarNode: "scalar", AliasNode: "alias",
	}
	fmt.Fprintf(b, "%s%s", strings.Repeat("  ", depth), kinds[n.Kind])
	if n.Kind == ScalarNode {
		fmt.Fprintf(b, " %q", n.Value)
	}
	fmt.Fprintf(b, " %d:%d\n", n.Pos.Line, n.Pos.Column)
	for _, c := range n.Children {
		dumpTree(b, c, depth+1)
	}
}

func TestParseIndentlessSequenceTree(t *testing.T) {
	cases := []indentlessCase{
		{
			name: "sequence then shallower key",
			src:  "a:\n  b:\n  - 0\nc: x\n",
			want: `doc 1:1
  map 1:1
    scalar "a" 1:1
    map 2:3
      scalar "b" 2:3
      seq 3:3
        scalar "0" 3:5
    scalar "c" 4:1
    scalar "x" 4:4
`,
		},
		{
			name: "empty entry then sibling key",
			src:  "b:\n-\nc: 1\n",
			want: `doc 1:1
  map 1:1
    scalar "b" 1:1
    seq 2:1
      scalar "" 3:1
    scalar "c" 3:1
    scalar "1" 3:4
`,
		},
		{
			name: "empty entries one level down then root key",
			src:  "a:\n  b:\n  - # comment\n  -\n  c: 1\nd: 2\n",
			want: `doc 1:1
  map 1:1
    scalar "a" 1:1
    map 2:3
      scalar "b" 2:3
      seq 3:3
        scalar "" 4:3
        scalar "" 5:3
      scalar "c" 5:3
      scalar "1" 5:6
    scalar "d" 6:1
    scalar "2" 6:4
`,
		},
	}
	forEachEOL(t, cases, func(t *testing.T, src string, tc indentlessCase) {
		t.Helper()
		f, err := Parse([]byte(src))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if len(f.Docs) != 1 {
			t.Fatalf("got %d documents, want 1", len(f.Docs))
		}
		var b strings.Builder
		dumpTree(&b, f.Docs[0], 0)
		if got := b.String(); got != tc.want {
			t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
		}
	})
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
