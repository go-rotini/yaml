package yaml

import (
	"context"
	"encoding/json"
	"fmt"
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

	var passed, failed, skipped int

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			if _, err := os.Stat(filepath.Join(tc.dir, "error")); err == nil {
				skipped++
				t.Skip("error case")
				return
			}

			inJSON, err := os.ReadFile(filepath.Join(tc.dir, "in.json"))
			if err != nil {
				skipped++
				t.Skip("no in.json")
				return
			}

			inYAML, err := os.ReadFile(filepath.Join(tc.dir, "in.yaml"))
			if err != nil {
				skipped++
				t.Skip("no in.yaml")
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			type jsonResult struct {
				data []byte
				err  error
			}
			ch := make(chan jsonResult, 1)
			go func() {
				d, e := ToJSON(inYAML)
				ch <- jsonResult{d, e}
			}()

			var gotJSON []byte
			select {
			case <-ctx.Done():
				t.Errorf("timeout")
				failed++
				return
			case res := <-ch:
				if res.err != nil {
					t.Errorf("ToJSON failed: %v", res.err)
					failed++
					return
				}
				gotJSON = res.data
			}

			var expected, got any
			if err := json.Unmarshal(inJSON, &expected); err != nil {
				t.Skipf("invalid expected JSON: %v", err)
				skipped++
				return
			}
			if err := json.Unmarshal(gotJSON, &got); err != nil {
				t.Errorf("invalid generated JSON: %v\nraw: %s", err, gotJSON)
				failed++
				return
			}

			if !reflect.DeepEqual(normalizeJSON(expected), normalizeJSON(got)) {
				t.Errorf("JSON mismatch:\nexpected: %s\ngot:      %s", string(inJSON), string(gotJSON))
				failed++
				return
			}

			passed++
		})
	}

	t.Logf("\n=== YAML Test Suite JSON Results ===")
	t.Logf("Passed:  %d", passed)
	t.Logf("Failed:  %d", failed)
	t.Logf("Skipped: %d", skipped)
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
	maxLen := len(expected)
	if len(got) > maxLen {
		maxLen = len(got)
	}
	for i := 0; i < maxLen; i++ {
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
