package yaml

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testSuiteDir = "testdata/yaml-test-suite"

type suiteTest struct {
	id  string
	dir string
}

func discoverTestDirs(t *testing.T) []suiteTest {
	t.Helper()
	entries, err := os.ReadDir(testSuiteDir)
	if err != nil {
		t.Skipf("yaml-test-suite not found at %s: %v", testSuiteDir, err)
	}
	var tests []suiteTest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "name" || name == "tags" {
			continue
		}
		dir := filepath.Join(testSuiteDir, name)
		if _, err := os.Stat(filepath.Join(dir, "in.yaml")); err == nil {
			tests = append(tests, suiteTest{id: name, dir: dir})
			continue
		}
		subs, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, sub := range subs {
			if !sub.IsDir() {
				continue
			}
			subDir := filepath.Join(dir, sub.Name())
			if _, err := os.Stat(filepath.Join(subDir, "in.yaml")); err == nil {
				tests = append(tests, suiteTest{id: name + "/" + sub.Name(), dir: subDir})
			}
		}
	}
	sort.Slice(tests, func(i, j int) bool { return tests[i].id < tests[j].id })
	return tests
}

func nodeEvents(docs []*node) []string {
	var events []string
	events = append(events, "+STR")
	for _, doc := range docs {
		docStart := "+DOC"
		if doc.docStartExplicit {
			docStart += " ---"
		}
		events = append(events, docStart)
		for _, child := range doc.children {
			events = nodeEventsWalk(child, events)
		}
		docEnd := "-DOC"
		if doc.docEndExplicit {
			docEnd += " ..."
		}
		events = append(events, docEnd)
	}
	events = append(events, "-STR")
	return events
}

func nodeEventsWalk(n *node, events []string) []string {
	if n == nil {
		return events
	}

	switch n.kind {
	case nodeMapping:
		ev := "+MAP"
		if n.flow {
			ev += " {}"
		}
		if n.anchor != "" {
			ev += " &" + n.anchor
		}
		if n.tag != "" {
			ev += " <" + n.tag + ">"
		}
		events = append(events, ev)
		for _, child := range n.children {
			events = nodeEventsWalk(child, events)
		}
		events = append(events, "-MAP")

	case nodeSequence:
		ev := "+SEQ"
		if n.flow {
			ev += " []"
		}
		if n.anchor != "" {
			ev += " &" + n.anchor
		}
		if n.tag != "" {
			ev += " <" + n.tag + ">"
		}
		events = append(events, ev)
		for _, child := range n.children {
			events = nodeEventsWalk(child, events)
		}
		events = append(events, "-SEQ")

	case nodeScalar:
		ev := "=VAL"
		if n.anchor != "" {
			ev += " &" + n.anchor
		}
		if n.tag != "" {
			ev += " <" + n.tag + ">"
		}
		ev += " " + scalarStyleIndicator(n.style) + escapeEventValue(n.value)
		events = append(events, ev)

	case nodeAlias:
		events = append(events, "=ALI *"+n.alias)

	case nodeMergeKey:
		ev := "=VAL"
		if n.tag != "" {
			ev += " <" + n.tag + ">"
		}
		ev += " :<<"
		events = append(events, ev)
	}

	return events
}

func scalarStyleIndicator(s scalarStyle) string {
	switch s {
	case scalarSingleQuoted:
		return "'"
	case scalarDoubleQuoted:
		return "\""
	case scalarLiteral:
		return "|"
	case scalarFolded:
		return ">"
	default:
		return ":"
	}
}

func escapeEventValue(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	s = strings.ReplaceAll(s, "\x00", "\\0")
	s = strings.ReplaceAll(s, "\b", "\\b")
	s = strings.ReplaceAll(s, "\x07", "\\a")
	s = strings.ReplaceAll(s, "\x1b", "\\e")
	s = strings.ReplaceAll(s, "\xc2\x85", "\\N")
	s = strings.ReplaceAll(s, "\xc2\xa0", "\\_")
	s = strings.ReplaceAll(s, "\xe2\x80\xa8", "\\L")
	s = strings.ReplaceAll(s, "\xe2\x80\xa9", "\\P")
	return s
}

func parseTestEvents(data []byte) []string {
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	var events []string
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			events = append(events, line)
		}
	}
	return events
}

type testResult struct {
	id     string
	status string // "pass", "fail", "error_correct", "error_missed", "skip", "timeout"
	detail string
}

func runSingleTest(id, dir string) testResult {
	inYAML, err := os.ReadFile(filepath.Join(dir, "in.yaml"))
	if err != nil {
		return testResult{id, "skip", "no in.yaml"}
	}

	_, isErrorCase := os.Stat(filepath.Join(dir, "error"))
	expectError := isErrorCase == nil

	eventData, err := os.ReadFile(filepath.Join(dir, "test.event"))
	if err != nil {
		return testResult{id, "skip", "no test.event"}
	}
	expectedEvents := parseTestEvents(eventData)

	converted, convErr := detectAndConvertEncoding(inYAML)
	if convErr != nil {
		if expectError {
			return testResult{id, "error_correct", "encoding error"}
		}
		return testResult{id, "fail", fmt.Sprintf("encoding: %v", convErr)}
	}

	tokens, scanErr := newScanner(converted).scan()
	if scanErr != nil {
		if expectError {
			return testResult{id, "error_correct", "scanner error"}
		}
		return testResult{id, "fail", fmt.Sprintf("scanner: %v", scanErr)}
	}

	p := newParser(tokens)
	docs, parseErr := p.parse()
	if parseErr != nil {
		if expectError {
			return testResult{id, "error_correct", "parser error"}
		}
		return testResult{id, "fail", fmt.Sprintf("parser: %v", parseErr)}
	}

	if expectError {
		return testResult{id, "error_missed", "expected error but parsed OK"}
	}

	gotEvents := nodeEvents(docs)
	if !eventsEqual(expectedEvents, gotEvents) {
		return testResult{id, "fail", eventDiff(expectedEvents, gotEvents)}
	}

	return testResult{id, "pass", ""}
}

func TestYAMLTestSuite(t *testing.T) {
	tests := discoverTestDirs(t)

	var (
		passed     atomic.Int32
		failed     atomic.Int32
		errCorrect atomic.Int32
		errMissed  atomic.Int32
		skipped    atomic.Int32
		timedOut   atomic.Int32
		failedIDs  []string
		failedMu   sync.Mutex
	)

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			ch := make(chan testResult, 1)
			go func() {
				ch <- runSingleTest(tc.id, tc.dir)
			}()

			select {
			case <-ctx.Done():
				timedOut.Add(1)
				t.Errorf("timeout (>5s)")
			case res := <-ch:
				switch res.status {
				case "pass":
					passed.Add(1)
				case "fail":
					failed.Add(1)
					failedMu.Lock()
					failedIDs = append(failedIDs, res.id)
					failedMu.Unlock()
					t.Errorf("%s", res.detail)
				case "error_correct":
					errCorrect.Add(1)
				case "error_missed":
					errMissed.Add(1)
					failedMu.Lock()
					failedIDs = append(failedIDs, res.id)
					failedMu.Unlock()
					t.Errorf("%s", res.detail)
				case "skip":
					skipped.Add(1)
					t.Skipf("%s", res.detail)
				}
			}
		})
	}

	total := int(passed.Load()) + int(errCorrect.Load())
	t.Logf("\n=== YAML Test Suite Results ===")
	t.Logf("Total:          %d", len(tests))
	t.Logf("Events match:   %d", passed.Load())
	t.Logf("Event mismatch: %d", failed.Load())
	t.Logf("Error correct:  %d (correctly rejected invalid input)", errCorrect.Load())
	t.Logf("Error missed:   %d (should reject but accepted)", errMissed.Load())
	t.Logf("Timed out:      %d", timedOut.Load())
	t.Logf("Skipped:        %d", skipped.Load())
	t.Logf("Pass rate:      %.1f%% (%d/%d)", float64(total)*100/float64(len(tests)), total, len(tests))

	if len(failedIDs) > 0 {
		sort.Strings(failedIDs)
		t.Logf("Failed IDs:     %s", strings.Join(failedIDs, ", "))
	}
}

func TestYAMLTestSuiteJSON(t *testing.T) {
	tests := discoverTestDirs(t)

	var passed, failed, errCorrect, errMissed, skipped int

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			inYAML, err := os.ReadFile(filepath.Join(tc.dir, "in.yaml"))
			if err != nil {
				skipped++
				t.Skip("no in.yaml")
				return
			}

			_, isErrorCase := os.Stat(filepath.Join(tc.dir, "error"))
			expectError := isErrorCase == nil

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			type decodeResult struct {
				docs []any
				err  error
			}
			ch := make(chan decodeResult, 1)
			go func() {
				dec := NewDecoder(bytes.NewReader(inYAML))
				var docs []any
				for {
					var v any
					if err := dec.Decode(&v); err != nil {
						if err == io.EOF {
							break
						}
						ch <- decodeResult{nil, err}
						return
					}
					docs = append(docs, v)
				}
				ch <- decodeResult{docs, nil}
			}()

			var docs []any
			var decErr error
			select {
			case <-ctx.Done():
				t.Errorf("timeout")
				failed++
				return
			case res := <-ch:
				docs = res.docs
				decErr = res.err
			}

			if expectError {
				if decErr != nil {
					errCorrect++
				} else {
					errMissed++
					t.Errorf("expected error but decode succeeded")
				}
				return
			}

			if decErr != nil {
				// A mapping or sequence key cannot be a key of the
				// map[string]any these cases decode into; cases using one
				// have no JSON form to compare against.
				var typeErr *TypeError
				if _, statErr := os.Stat(filepath.Join(tc.dir, "in.json")); statErr != nil &&
					errors.As(decErr, &typeErr) && strings.Contains(decErr.Error(), "as a key of") {
					passed++
					return
				}
				t.Errorf("decode failed: %v", decErr)
				failed++
				return
			}

			inJSON, err := os.ReadFile(filepath.Join(tc.dir, "in.json"))
			if err != nil {
				passed++
				return
			}

			var expectedDocs []any
			jdec := json.NewDecoder(bytes.NewReader(inJSON))
			for jdec.More() {
				var v any
				if err := jdec.Decode(&v); err != nil {
					skipped++
					t.Skipf("invalid expected JSON: %v", err)
					return
				}
				expectedDocs = append(expectedDocs, v)
			}

			if len(expectedDocs) == 0 && len(docs) == 0 {
				passed++
				return
			}

			if len(docs) != len(expectedDocs) {
				t.Errorf("document count mismatch: expected %d, got %d", len(expectedDocs), len(docs))
				failed++
				return
			}

			for i := range docs {
				gotBytes, err := json.Marshal(docs[i])
				if err != nil {
					t.Errorf("doc %d: json marshal failed: %v", i, err)
					failed++
					return
				}
				var got any
				if err := json.Unmarshal(gotBytes, &got); err != nil {
					t.Errorf("doc %d: invalid generated JSON: %v\nraw: %s", i, err, gotBytes)
					failed++
					return
				}
				if !reflect.DeepEqual(normalizeJSON(expectedDocs[i]), normalizeJSON(got)) {
					expBytes, _ := json.Marshal(expectedDocs[i])
					t.Errorf("doc %d JSON mismatch:\nexpected: %s\ngot:      %s", i, expBytes, gotBytes)
					failed++
					return
				}
			}

			passed++
		})
	}

	total := passed + errCorrect
	t.Logf("\n=== YAML Test Suite JSON Results ===")
	t.Logf("Total:          %d", len(tests))
	t.Logf("JSON match:     %d", passed)
	t.Logf("JSON mismatch:  %d", failed)
	t.Logf("Error correct:  %d (correctly rejected invalid input)", errCorrect)
	t.Logf("Error missed:   %d (should reject but accepted)", errMissed)
	t.Logf("Skipped:        %d", skipped)
	t.Logf("Pass rate:      %.1f%% (%d/%d)", float64(total)*100/float64(len(tests)), total, len(tests))
}

func normalizeJSON(v any) any {
	switch val := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(val))
		for k, v := range val {
			m[k] = normalizeJSON(v)
		}
		return m
	case []any:
		s := make([]any, len(val))
		for i, v := range val {
			s[i] = normalizeJSON(v)
		}
		return s
	case float64:
		if val == float64(int64(val)) {
			return int64(val)
		}
		return val
	default:
		return v
	}
}

func eventsEqual(expected, got []string) bool {
	if len(expected) != len(got) {
		return false
	}
	for i := range expected {
		if expected[i] != got[i] {
			return false
		}
	}
	return true
}

func eventDiff(expected, got []string) string {
	var sb strings.Builder
	maxLen := max(len(expected), len(got))
	for i := range maxLen {
		var e, g string
		if i < len(expected) {
			e = expected[i]
		} else {
			e = "<missing>"
		}
		if i < len(got) {
			g = got[i]
		} else {
			g = "<missing>"
		}
		marker := "  "
		if e != g {
			marker = "! "
		}
		fmt.Fprintf(&sb, "%s%3d: expected=%-40s got=%s\n", marker, i, e, g)
	}
	return sb.String()
}

// suiteValidInputs returns the in.yaml of every suite case that is not
// expected to fail.
func suiteValidInputs(t *testing.T) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte)
	for _, tc := range discoverTestDirs(t) {
		if _, err := os.Stat(filepath.Join(tc.dir, "error")); err == nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tc.dir, "in.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		out[tc.id] = data
	}
	return out
}

// decodeStream decodes every document of data into any.
func decodeStream(data []byte, opts ...DecodeOption) ([]any, error) {
	dec := NewDecoder(bytes.NewReader(data), opts...)
	var docs []any
	for {
		var v any
		if err := dec.Decode(&v); err != nil {
			if errors.Is(err, io.EOF) {
				return docs, nil
			}
			return nil, err
		}
		docs = append(docs, v)
	}
}

// TestYAMLTestSuiteNodeRoundTrip checks that every valid suite input
// written back with NodeToBytes decodes to the same values, and that
// writing it again gives the same bytes.
func TestYAMLTestSuiteNodeRoundTrip(t *testing.T) {
	for id, data := range suiteValidInputs(t) {
		if _, err := Parse(data); err != nil {
			t.Errorf("%s: Parse: %v", id, err)
			continue
		}
		if err := nodeRoundTrip(data); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

// nodeRoundTrip checks that each document of data, parsed and written back
// with NodeToBytes, decodes to the values data decodes to, and that writing
// the result again gives the same bytes. Input that does not parse or
// decode is not checked.
func nodeRoundTrip(data []byte) error {
	f, parseErr := Parse(data)
	want, decodeErr := decodeStream(data, WithOrderedMap())
	// Not checked: e.g. an alias to a later anchor does not decode.
	if decodes := parseErr == nil && decodeErr == nil; !decodes {
		return nil
	}
	if len(want) != len(f.Docs) {
		return fmt.Errorf("%d decoded documents, %d parsed", len(want), len(f.Docs))
	}
	for i, doc := range f.Docs {
		out, err := NodeToBytes(doc)
		if err != nil {
			return fmt.Errorf("doc %d: NodeToBytes: %w", i, err)
		}
		var got any
		if err := UnmarshalWithOptions(out, &got, WithOrderedMap()); err != nil {
			return fmt.Errorf("doc %d: decoding %q: %w", i, out, err)
		}
		// Keys keep their source text, and an empty key is written as
		// null, so null key spellings compare equal here.
		if !sameValue(nullKeys(want[i]), nullKeys(got)) {
			return fmt.Errorf("doc %d: wrote %q\nwant %#v\ngot  %#v", i, out, want[i], got)
		}
		f2, err := Parse(out)
		if err != nil || len(f2.Docs) != 1 {
			return fmt.Errorf("doc %d: reparsing %q: %w", i, out, err)
		}
		if again, _ := NodeToBytes(f2.Docs[0]); !bytes.Equal(out, again) {
			return fmt.Errorf("doc %d: not stable:\n%s\n%s", i, out, again)
		}
	}
	return nil
}

// TestYAMLTestSuiteAnyRoundTrip checks that every valid suite input decoded
// into any, encoded with each encoder style and decoded again, gives the
// same values.
func TestYAMLTestSuiteAnyRoundTrip(t *testing.T) {
	styles := [][]EncodeOption{
		nil,
		{WithFlow(true)},
		{WithLiteralStyle(true)},
		{WithSingleQuote(true)},
		{WithJSON(true)},
		{WithQuoteAllStrings(true)},
		{WithIndent(3), WithIndentSequence(false)},
	}
	for id, data := range suiteValidInputs(t) {
		var v any
		if err := Unmarshal(data, &v); err != nil {
			continue
		}
		for _, opts := range styles {
			out, err := MarshalWithOptions(v, opts...)
			if err != nil {
				t.Errorf("%s: Marshal(%#v): %v", id, v, err)
				continue
			}
			var got any
			if err := Unmarshal(out, &got); err != nil || !sameValue(v, got) {
				t.Errorf("%s: wrote %q\nwant %#v\ngot  %#v (%v)", id, out, v, got, err)
			}
		}
	}
}

// nullKeys returns v with every MapSlice key that spells null replaced by
// nil.
func nullKeys(v any) any {
	switch x := v.(type) {
	case MapSlice:
		out := make(MapSlice, len(x))
		for i, item := range x {
			k := nullKeys(item.Key)
			if s, ok := k.(string); ok && isNullValue(s) {
				k = nil
			}
			out[i] = MapItem{Key: k, Value: nullKeys(item.Value)}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = nullKeys(item)
		}
		return out
	}
	return v
}
