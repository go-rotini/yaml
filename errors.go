package yaml

import (
	"bytes"
	"fmt"
	"strings"
)

// Position identifies a location within YAML source text.
type Position struct {
	Line   int
	Column int
	Offset int
}

func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// SyntaxError is returned when the YAML input is malformed.
// Use [errors.Is](err, [ErrSyntax]) to test for syntax errors generically.
type SyntaxError struct {
	Message string
	Pos     Position
	Token   string
}

func (e *SyntaxError) Error() string {
	if e.Pos.Line > 0 {
		return fmt.Sprintf("yaml: line %d, column %d: %s", e.Pos.Line, e.Pos.Column, e.Message)
	}
	return fmt.Sprintf("yaml: %s", e.Message)
}

func (e *SyntaxError) Is(target error) bool {
	_, ok := target.(*SyntaxError)
	return ok
}

// TypeError is returned when one or more YAML values cannot be assigned to
// the target Go types. Errors contains a message for each failed conversion.
type TypeError struct {
	Errors []string
}

func (e *TypeError) Error() string {
	return fmt.Sprintf("yaml: unmarshal errors:\n  %s", strings.Join(e.Errors, "\n  "))
}

func (e *TypeError) Is(target error) bool {
	_, ok := target.(*TypeError)
	return ok
}

// UnknownFieldError is returned when decoding with [Strict] and a YAML key
// has no corresponding struct field.
type UnknownFieldError struct {
	Field string
	Pos   Position
}

func (e *UnknownFieldError) Error() string {
	return fmt.Sprintf("yaml: line %d: unknown field %q", e.Pos.Line, e.Field)
}

func (e *UnknownFieldError) Is(target error) bool {
	_, ok := target.(*UnknownFieldError)
	return ok
}

// CycleError is returned when alias expansion detects a cycle or exceeds
// the maximum expansion depth set by [MaxAliasExpansion].
type CycleError struct {
	Anchor string
	Pos    Position
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("yaml: line %d: alias %q creates a cycle", e.Pos.Line, e.Anchor)
}

func (e *CycleError) Is(target error) bool {
	_, ok := target.(*CycleError)
	return ok
}

// DuplicateKeyError is returned when decoding with [DisallowDuplicateKey]
// and a mapping contains the same key more than once.
type DuplicateKeyError struct {
	Key string
	Pos Position
}

func (e *DuplicateKeyError) Error() string {
	return fmt.Sprintf("yaml: line %d: duplicate key %q", e.Pos.Line, e.Key)
}

func (e *DuplicateKeyError) Is(target error) bool {
	_, ok := target.(*DuplicateKeyError)
	return ok
}

// Sentinel errors for use with [errors.Is].
var (
	ErrSyntax       = &SyntaxError{}
	ErrType         = &TypeError{}
	ErrUnknownField = &UnknownFieldError{}
	ErrCycle        = &CycleError{}
	ErrDuplicateKey = &DuplicateKeyError{}
)

// FormatError returns a human-readable string for a [SyntaxError] that includes
// the offending source line and a column pointer. For non-syntax errors it
// returns err.Error().
func FormatError(data []byte, err error) string {
	synErr, ok := err.(*SyntaxError)
	if !ok {
		return err.Error()
	}

	lines := bytes.Split(data, []byte("\n"))
	lineIdx := synErr.Pos.Line - 1
	if lineIdx < 0 || lineIdx >= len(lines) {
		return synErr.Error()
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s\n", synErr.Error())
	fmt.Fprintf(&buf, "  %s\n", string(lines[lineIdx]))
	fmt.Fprintf(&buf, "  %s^\n", repeatByte(' ', synErr.Pos.Column-1))
	return buf.String()
}

func repeatByte(b byte, n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = b
	}
	return string(buf)
}
