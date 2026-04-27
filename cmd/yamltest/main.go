package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-rotini/yaml"
)

type result struct {
	ID     string   `json:"id"`
	Status string   `json:"status"`
	Detail string   `json:"detail,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

type report struct {
	Total        int      `json:"total"`
	Pass         int      `json:"pass"`
	Fail         int      `json:"fail"`
	ErrorCorrect int      `json:"error_correct"`
	ErrorMissed  int      `json:"error_missed"`
	Skip         int      `json:"skip"`
	Timeout      int      `json:"timeout"`
	PassRate     float64  `json:"pass_rate"`
	Results      []result `json:"results"`
}

type failureReport struct {
	Total     int                 `json:"total"`
	Fail      int                 `json:"fail"`
	ErrMissed int                 `json:"error_missed"`
	Timeout   int                 `json:"timeout"`
	Failures  []result            `json:"failures"`
	ByTag     map[string][]string `json:"by_tag,omitempty"`
}

func main() {
	dir := flag.String("dir", "testdata/yaml-test-suite", "path to yaml-test-suite data directory")
	timeout := flag.Duration("timeout", 5*time.Second, "per-test timeout")
	failures := flag.Bool("failures", false, "only output failed/error_missed tests with details")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: yamltest [flags]\n\nRuns the YAML test suite and emits a JSON report to stdout.\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	entries, err := os.ReadDir(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "yamltest: %v\n", err)
		os.Exit(1)
	}

	var testIDs []string
	for _, e := range entries {
		if e.IsDir() {
			testIDs = append(testIDs, e.Name())
		}
	}
	sort.Strings(testIDs)

	r := report{}
	for _, id := range testIDs {
		ch := make(chan result, 1)
		go func() {
			ch <- runTest(*dir, id)
		}()

		timer := time.NewTimer(*timeout)
		var res result
		select {
		case res = <-ch:
			timer.Stop()
		case <-timer.C:
			res = result{ID: id, Status: "timeout", Detail: "exceeded " + timeout.String()}
		}

		r.Results = append(r.Results, res)
		r.Total++
		switch res.Status {
		case "pass":
			r.Pass++
		case "fail":
			r.Fail++
		case "error_correct":
			r.ErrorCorrect++
		case "error_missed":
			r.ErrorMissed++
		case "skip":
			r.Skip++
		case "timeout":
			r.Timeout++
		}
	}

	passing := r.Pass + r.ErrorCorrect
	if r.Total > 0 {
		r.PassRate = float64(passing) * 100 / float64(r.Total)
	}

	if *failures {
		fr := failureReport{
			Total:     r.Total,
			Fail:      r.Fail,
			ErrMissed: r.ErrorMissed,
			Timeout:   r.Timeout,
			ByTag:     make(map[string][]string),
		}
		for _, res := range r.Results {
			if res.Status == "fail" || res.Status == "error_missed" || res.Status == "timeout" {
				fr.Failures = append(fr.Failures, res)
				for _, tag := range res.Tags {
					fr.ByTag[tag] = append(fr.ByTag[tag], res.ID)
				}
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(fr); err != nil {
			fmt.Fprintf(os.Stderr, "yamltest: %v\n", err)
			os.Exit(1)
		}
	} else {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			fmt.Fprintf(os.Stderr, "yamltest: %v\n", err)
			os.Exit(1)
		}
	}

	if r.Fail > 0 || r.ErrorMissed > 0 || r.Timeout > 0 {
		os.Exit(1)
	}
}

func readTags(testDir string) []string {
	data, err := os.ReadFile(filepath.Join(testDir, "tags"))
	if err != nil {
		return nil
	}
	var tags []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

func runTest(dir, id string) result {
	testDir := filepath.Join(dir, id)
	tags := readTags(testDir)

	inYAML, err := os.ReadFile(filepath.Join(testDir, "in.yaml"))
	if err != nil {
		return result{ID: id, Status: "skip", Detail: "no in.yaml", Tags: tags}
	}

	_, isErrorCase := os.Stat(filepath.Join(testDir, "error"))
	expectError := isErrorCase == nil

	eventData, err := os.ReadFile(filepath.Join(testDir, "test.event"))
	if err != nil {
		return result{ID: id, Status: "skip", Detail: "no test.event", Tags: tags}
	}
	expectedEvents := parseTestEvents(eventData)

	file, parseErr := yaml.Parse(inYAML)
	if parseErr != nil {
		if expectError {
			return result{ID: id, Status: "error_correct", Detail: "parse error", Tags: tags}
		}
		return result{ID: id, Status: "fail", Detail: fmt.Sprintf("parse: %v", parseErr), Tags: tags}
	}

	if expectError {
		return result{ID: id, Status: "error_missed", Detail: "expected error but parsed OK", Tags: tags}
	}

	gotEvents := nodeEvents(file)
	if !eventsEqual(expectedEvents, gotEvents) {
		return result{ID: id, Status: "fail", Detail: eventDiff(expectedEvents, gotEvents), Tags: tags}
	}

	return result{ID: id, Status: "pass", Tags: tags}
}

func nodeEvents(file *yaml.File) []string {
	var events []string
	events = append(events, "+STR")
	for _, doc := range file.Docs {
		docStart := "+DOC"
		if doc.ExplicitStart {
			docStart += " ---"
		}
		events = append(events, docStart)
		for _, child := range doc.Children {
			events = nodeEventsWalk(child, events)
		}
		docEnd := "-DOC"
		if doc.ExplicitEnd {
			docEnd += " ..."
		}
		events = append(events, docEnd)
	}
	events = append(events, "-STR")
	return events
}

func nodeEventsWalk(n *yaml.Node, events []string) []string {
	if n == nil {
		return events
	}

	switch n.Kind {
	case yaml.MappingNode:
		ev := "+MAP"
		if n.Flow {
			ev += " {}"
		}
		if n.Anchor != "" {
			ev += " &" + n.Anchor
		}
		if n.Tag != "" {
			ev += " <" + n.Tag + ">"
		}
		events = append(events, ev)
		for _, child := range n.Children {
			events = nodeEventsWalk(child, events)
		}
		events = append(events, "-MAP")

	case yaml.SequenceNode:
		ev := "+SEQ"
		if n.Flow {
			ev += " []"
		}
		if n.Anchor != "" {
			ev += " &" + n.Anchor
		}
		if n.Tag != "" {
			ev += " <" + n.Tag + ">"
		}
		events = append(events, ev)
		for _, child := range n.Children {
			events = nodeEventsWalk(child, events)
		}
		events = append(events, "-SEQ")

	case yaml.ScalarNode:
		if n.MergeKey {
			ev := "=VAL"
			if n.Tag != "" {
				ev += " <" + n.Tag + ">"
			}
			ev += " :<<"
			events = append(events, ev)
		} else {
			ev := "=VAL"
			if n.Anchor != "" {
				ev += " &" + n.Anchor
			}
			if n.Tag != "" {
				ev += " <" + n.Tag + ">"
			}
			ev += " " + scalarStyleIndicator(n.Style) + escapeEventValue(n.Value)
			events = append(events, ev)
		}

	case yaml.AliasNode:
		events = append(events, "=ALI *"+n.Alias)
	}

	return events
}

func scalarStyleIndicator(s yaml.ScalarStyle) string {
	switch s {
	case yaml.SingleQuotedStyle:
		return "'"
	case yaml.DoubleQuotedStyle:
		return "\""
	case yaml.LiteralStyle:
		return "|"
	case yaml.FoldedStyle:
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
		if e != g {
			fmt.Fprintf(&sb, "! %3d: expected=%-40s got=%s\n", i, e, g)
		}
	}
	return sb.String()
}
