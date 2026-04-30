package yaml

import (
	"bytes"
	"errors"
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

// UnknownFieldError is returned when decoding with [WithStrict] and a YAML key
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
// the maximum expansion depth set by [WithMaxAliasExpansion].
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

// DuplicateKeyError is returned when decoding with [WithDisallowDuplicateKey]
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

// ValidationError wraps an error returned by a [StructValidator] with the
// [Position] of the YAML node that was decoded into the struct. This allows
// validation errors to be pretty-printed with [FormatError] just like syntax
// errors.
type ValidationError struct {
	Err error
	Pos Position
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("yaml: line %d: validation: %s", e.Pos.Line, e.Err.Error())
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

func (e *ValidationError) Is(target error) bool {
	_, ok := target.(*ValidationError)
	return ok
}

// DefaultError is returned when a default value from a struct tag cannot be
// applied — for example, when the default string cannot be parsed into the
// target type, or when default is combined with required.
type DefaultError struct {
	Field   string
	Message string
	Pos     Position
}

func (e *DefaultError) Error() string {
	return fmt.Sprintf("yaml: field %q: %s", e.Field, e.Message)
}

func (e *DefaultError) Is(target error) bool {
	_, ok := target.(*DefaultError)
	return ok
}

// Sentinel errors for use with [errors.Is].
var (
	ErrSyntax       = &SyntaxError{}
	ErrType         = &TypeError{}
	ErrUnknownField = &UnknownFieldError{}
	ErrCycle        = &CycleError{}
	ErrDuplicateKey = &DuplicateKeyError{}
	ErrValidation   = &ValidationError{}
	ErrDefault      = &DefaultError{}

	ErrPathSyntax   = errors.New("yaml: invalid path syntax")
	ErrPathNotFound = errors.New("yaml: path not found")
	ErrNilPointer   = errors.New("yaml: non-nil pointer required")
	ErrDocumentSize = errors.New("yaml: document size exceeds limit")
	ErrPathEscape   = errors.New("yaml: reference path escapes allowed directory")
)

var (
	errConflictingFields   = errors.New("conflicting field names")
	errUndefinedTag        = errors.New("undefined tag handle")
	errNotBool             = errors.New("invalid boolean value")
	errNotTime             = errors.New("invalid time value")
	errOddChildren         = errors.New("odd number of mapping children")
	errEmptyAlias          = errors.New("empty alias name")
	errNoDocuments         = errors.New("no documents in file")
	errPathTooShortReplace = errors.New("path too short for replace")
	errPathTooShortDelete  = errors.New("path too short for delete")
)

// FormatError returns a human-readable string for a [SyntaxError] or
// [ValidationError] that includes the offending source line and a column
// pointer. For other error types it returns err.Error(). Set color to true
// to include ANSI color escape sequences.
func FormatError(data []byte, err error, color ...bool) string {
	var pos Position
	var synErr *SyntaxError
	var valErr *ValidationError
	switch {
	case errors.As(err, &synErr):
		pos = synErr.Pos
	case errors.As(err, &valErr):
		pos = valErr.Pos
	default:
		return err.Error()
	}

	lines := bytes.Split(data, []byte("\n"))
	lineIdx := pos.Line - 1
	if lineIdx < 0 || lineIdx >= len(lines) {
		return err.Error()
	}

	useColor := len(color) > 0 && color[0]

	var buf bytes.Buffer
	if useColor {
		fmt.Fprintf(&buf, "\x1b[1;31m%s\x1b[0m\n", err.Error())
		fmt.Fprintf(&buf, "  %s\n", string(lines[lineIdx]))
		fmt.Fprintf(&buf, "  %s\x1b[1;31m^\x1b[0m\n", repeatByte(' ', pos.Column-1))
	} else {
		fmt.Fprintf(&buf, "%s\n", err.Error())
		fmt.Fprintf(&buf, "  %s\n", string(lines[lineIdx]))
		fmt.Fprintf(&buf, "  %s^\n", repeatByte(' ', pos.Column-1))
	}
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
