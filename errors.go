package yaml

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Position identifies a location within YAML source text. Line and Column
// start at 1; Column and Offset count bytes of the UTF-8 input (input in
// UTF-16 or UTF-32 is converted to UTF-8 first). The zero Position means
// the location is unknown. An error's position is where it was detected,
// which can be just past the offending text.
type Position struct {
	Line   int
	Column int
	Offset int
}

// String returns the position as "line:column".
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

// Is reports whether target is a [*SyntaxError], so any syntax error matches [ErrSyntax].
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

// Is reports whether target is a [*TypeError], so any type error matches [ErrType].
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

// Is reports whether target is an [*UnknownFieldError], so any such error matches [ErrUnknownField].
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

// Is reports whether target is a [*CycleError], so any such error matches [ErrCycle].
func (e *CycleError) Is(target error) bool {
	_, ok := target.(*CycleError)
	return ok
}

// AliasExpansionError is returned when alias expansion produces more nodes
// than the limit set by [WithMaxAliasNodes], as a "billion laughs" document
// does.
type AliasExpansionError struct {
	Limit int
	Pos   Position
}

func (e *AliasExpansionError) Error() string {
	return fmt.Sprintf("yaml: line %d: alias expansion exceeds %d nodes", e.Pos.Line, e.Limit)
}

// Is reports whether target is an [*AliasExpansionError], so any such error matches [ErrAliasExpansion].
func (e *AliasExpansionError) Is(target error) bool {
	_, ok := target.(*AliasExpansionError)
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

// Is reports whether target is a [*DuplicateKeyError], so any such error matches [ErrDuplicateKey].
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

// Unwrap returns the validator's error.
func (e *ValidationError) Unwrap() error {
	return e.Err
}

// Is reports whether target is a [*ValidationError], so any such error matches [ErrValidation].
func (e *ValidationError) Is(target error) bool {
	_, ok := target.(*ValidationError)
	return ok
}

// RequiredFieldError is returned when a struct field tagged required is
// missing from the input, including when the struct's whole mapping is
// absent or null. errors.Is matches it with [ErrSyntax] as well as
// [ErrRequiredField], but it is not a [*SyntaxError], so errors.As to a
// *SyntaxError does not match it; read Pos from the RequiredFieldError. Pos
// is the zero Position when the input holds no document.
type RequiredFieldError struct {
	Field string
	Pos   Position
}

func (e *RequiredFieldError) Error() string {
	if e.Pos.Line == 0 {
		return fmt.Sprintf("yaml: required field %q is missing", e.Field)
	}
	return fmt.Sprintf("yaml: line %d, column %d: required field %q is missing", e.Pos.Line, e.Pos.Column, e.Field)
}

// Is reports whether target is a [*RequiredFieldError] or a [*SyntaxError],
// so the error matches both [ErrRequiredField] and [ErrSyntax].
func (e *RequiredFieldError) Is(target error) bool {
	switch target.(type) {
	case *RequiredFieldError, *SyntaxError:
		return true
	}
	return false
}

// UnmarshalerError wraps an error returned by a type's own unmarshaling
// method ([Unmarshaler], [UnmarshalerContext], [BytesUnmarshaler],
// [encoding.TextUnmarshaler], a JSON unmarshaler or a
// [WithCustomUnmarshaler] function) with the [Position] of the value it was
// decoding.
type UnmarshalerError struct {
	Err error
	Pos Position
}

func (e *UnmarshalerError) Error() string {
	// An error from this package already says where it comes from.
	msg := strings.TrimPrefix(e.Err.Error(), "yaml: ")
	return fmt.Sprintf("yaml: line %d, column %d: %s", e.Pos.Line, e.Pos.Column, msg)
}

// Unwrap returns the unmarshaler's error.
func (e *UnmarshalerError) Unwrap() error {
	return e.Err
}

// DefaultError is returned when a default value from a struct tag cannot be
// applied: for example, when the default string cannot be parsed into the
// target type, or when default is combined with required.
type DefaultError struct {
	Field   string
	Message string
	Pos     Position
}

func (e *DefaultError) Error() string {
	return fmt.Sprintf("yaml: field %q: %s", e.Field, e.Message)
}

// Is reports whether target is a [*DefaultError], so any such error matches [ErrDefault].
func (e *DefaultError) Is(target error) bool {
	_, ok := target.(*DefaultError)
	return ok
}

// KYAMLError reports one or more KYAML conformance violations. It is
// returned by [ValidateKYAML], [UnmarshalKYAML] and [UnmarshalWithOptions]
// with [WithStrictKYAML].
//
// Use [errors.Is](err, [ErrKYAML]) to test for KYAML errors generically.
type KYAMLError struct {
	Errors []KYAMLViolation
}

// KYAMLViolation describes a single KYAML rule violation.
type KYAMLViolation struct {
	// Rule is a stable identifier of the rule broken, for code that
	// branches on the kind of violation. It is not part of the message.
	Rule    string
	Message string
	Pos     Position
	// Token is the offending source text, when known.
	Token string
}

func (e *KYAMLError) Error() string {
	if len(e.Errors) == 0 {
		return "yaml: KYAML validation failed"
	}
	if len(e.Errors) == 1 {
		v := e.Errors[0]
		if v.Pos.Line > 0 {
			return fmt.Sprintf("yaml: line %d, column %d: %s", v.Pos.Line, v.Pos.Column, v.Message)
		}
		return "yaml: " + v.Message
	}
	parts := make([]string, 0, len(e.Errors))
	for _, v := range e.Errors {
		if v.Pos.Line > 0 {
			parts = append(parts, fmt.Sprintf("line %d, column %d: %s", v.Pos.Line, v.Pos.Column, v.Message))
		} else {
			parts = append(parts, v.Message)
		}
	}
	return fmt.Sprintf("yaml: %d KYAML violations:\n  %s", len(e.Errors), strings.Join(parts, "\n  "))
}

// Is reports whether target is a [*KYAMLError], so any such error matches [ErrKYAML].
func (e *KYAMLError) Is(target error) bool {
	_, ok := target.(*KYAMLError)
	return ok
}

// Severity classifies a [LintIssue] as either an error (KYAML structural
// violation) or a warning (KYAML cosmetic deviation).
type Severity int

const (
	SeverityError   Severity = iota // structural KYAML violation
	SeverityWarning                 // cosmetic KYAML deviation (indentation, ordering, etc.)
)

// LintIssue describes a single KYAML conformance deviation reported by [Lint].
type LintIssue struct {
	// Rule is a stable identifier of the rule broken, for code that
	// branches on the kind of issue. It is not part of the message.
	Rule     string
	Message  string
	Pos      Position
	Severity Severity
}

// String renders the issue with its source position, when known. LintIssue
// is a finding rather than an error, so it implements [fmt.Stringer]
// instead of error.
func (i LintIssue) String() string {
	if i.Pos.Line > 0 {
		return fmt.Sprintf("line %d, column %d: %s", i.Pos.Line, i.Pos.Column, i.Message)
	}
	return i.Message
}

// Sentinel errors for use with [errors.Is]. Never pass one's address to
// [errors.As] (as in &ErrSyntax): that overwrites the shared sentinel.
// Declare a variable of the error type for errors.As instead.
var (
	ErrSyntax         = &SyntaxError{}
	ErrType           = &TypeError{}
	ErrUnknownField   = &UnknownFieldError{}
	ErrCycle          = &CycleError{}
	ErrAliasExpansion = &AliasExpansionError{}
	ErrDuplicateKey   = &DuplicateKeyError{}
	ErrValidation     = &ValidationError{}
	ErrDefault        = &DefaultError{}
	ErrKYAML          = &KYAMLError{}
	ErrRequiredField  = &RequiredFieldError{}

	ErrPathSyntax     = errors.New("yaml: invalid path syntax")
	ErrPathNotFound   = errors.New("yaml: path not found")
	ErrPointerSyntax  = errors.New("yaml: invalid JSON Pointer syntax")
	ErrNilPointer     = errors.New("yaml: non-nil pointer required")
	ErrDocumentSize   = errors.New("yaml: document size exceeds limit")
	ErrPathEscape     = errors.New("yaml: reference path escapes allowed directory")
	ErrOptionConflict = errors.New("yaml: option conflict")
	ErrUnsupported    = errors.New("yaml: unsupported value")
)

var (
	errConflictingFields      = errors.New("conflicting field names")
	errUndefinedTag           = errors.New("undefined tag handle")
	errNotBool                = errors.New("invalid boolean value")
	errNotTime                = errors.New("invalid time value")
	errOddChildren            = errors.New("odd number of mapping children")
	errEmptyAlias             = errors.New("empty alias name")
	errInvalidAnchor          = errors.New("invalid anchor name")
	errNoDocuments            = errors.New("no documents in file")
	errJSONTrailingData       = errors.New("invalid data after top-level value")
	errPathTooShortReplace    = fmt.Errorf("yaml: path too short for replace: %w", ErrPathSyntax)
	errPathTooShortDelete     = fmt.Errorf("yaml: path too short for delete: %w", ErrPathSyntax)
	errInvalidNumber          = errors.New("yaml: invalid number")
	errInvalidBoolDefault     = errors.New("invalid bool default")
	errInvalidDurationDefault = errors.New("invalid duration default")
	errInvalidIntDefault      = errors.New("invalid int default")
	errInvalidUintDefault     = errors.New("invalid uint default")
	errInvalidFloatDefault    = errors.New("invalid float default")
	errUnsupportedDefault     = errors.New("default tag is not supported for type")
)

// FormatError returns a human-readable rendering of err, including the
// offending source line and a column pointer when err carries a position
// (every error type in this package that has a Pos field, and
// [KYAMLError]). For other errors, FormatError returns err.Error(), and for
// a nil err it returns "". Pass color=true to include ANSI color escape
// sequences.
func FormatError(data []byte, err error, color ...bool) string {
	if err == nil {
		return ""
	}
	useColor := len(color) > 0 && color[0]
	lines := bytes.Split(data, []byte("\n"))
	var buf bytes.Buffer

	var kyamlErr *KYAMLError
	if errors.As(err, &kyamlErr) && len(kyamlErr.Errors) > 0 {
		for i, v := range kyamlErr.Errors {
			if i > 0 {
				buf.WriteByte('\n')
			}
			msg := "yaml: " + v.Message
			if v.Pos.Line > 0 {
				msg = fmt.Sprintf("yaml: line %d, column %d: %s", v.Pos.Line, v.Pos.Column, v.Message)
			}
			writeErrorAt(&buf, lines, msg, v.Pos, useColor)
		}
		return buf.String()
	}

	pos, ok := errorPosition(err)
	if !ok || pos.Line > len(lines) {
		return err.Error()
	}
	writeErrorAt(&buf, lines, err.Error(), pos, useColor)
	return buf.String()
}

// writeErrorAt writes msg and, when pos is within lines, the source line
// with a caret under pos. The caret counts characters, not bytes, so it
// lines up after multibyte characters.
func writeErrorAt(buf *bytes.Buffer, lines [][]byte, msg string, pos Position, useColor bool) {
	if useColor {
		fmt.Fprintf(buf, "\x1b[1;31m%s\x1b[0m\n", msg)
	} else {
		fmt.Fprintf(buf, "%s\n", msg)
	}
	lineIdx := pos.Line - 1
	if lineIdx < 0 || lineIdx >= len(lines) {
		return
	}
	line := lines[lineIdx]
	col := min(max(pos.Column-1, 0), len(line))
	pad := repeatByte(' ', utf8.RuneCount(line[:col]))
	fmt.Fprintf(buf, "  %s\n", string(line))
	if useColor {
		fmt.Fprintf(buf, "  %s\x1b[1;31m^\x1b[0m\n", pad)
	} else {
		fmt.Fprintf(buf, "  %s^\n", pad)
	}
}

// errorPosition returns the source position err carries, if any.
func errorPosition(err error) (Position, bool) {
	var (
		syn   *SyntaxError
		val   *ValidationError
		unk   *UnknownFieldError
		dup   *DuplicateKeyError
		cyc   *CycleError
		alias *AliasExpansionError
		req   *RequiredFieldError
		um    *UnmarshalerError
		def   *DefaultError
	)
	// An UnmarshalerError comes first: an error it wraps may hold a position
	// in re-encoded bytes rather than in the source.
	var pos Position
	switch {
	case errors.As(err, &um):
		pos = um.Pos
	case errors.As(err, &syn):
		pos = syn.Pos
	case errors.As(err, &req):
		pos = req.Pos
	case errors.As(err, &val):
		pos = val.Pos
	case errors.As(err, &unk):
		pos = unk.Pos
	case errors.As(err, &dup):
		pos = dup.Pos
	case errors.As(err, &cyc):
		pos = cyc.Pos
	case errors.As(err, &alias):
		pos = alias.Pos
	case errors.As(err, &def):
		pos = def.Pos
	default:
		return Position{}, false
	}
	return pos, pos.Line > 0
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
