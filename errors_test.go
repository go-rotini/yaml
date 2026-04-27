package yaml

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorIs(t *testing.T) {
	synErr := &SyntaxError{Message: "test", Pos: Position{Line: 1, Column: 1}}
	if !errors.Is(synErr, ErrSyntax) {
		t.Error("expected SyntaxError to match ErrSyntax")
	}

	typeErr := &TypeError{Errors: []string{"test"}}
	if !errors.Is(typeErr, ErrType) {
		t.Error("expected TypeError to match ErrType")
	}

	unkErr := &UnknownFieldError{Field: "test", Pos: Position{Line: 1, Column: 1}}
	if !errors.Is(unkErr, ErrUnknownField) {
		t.Error("expected UnknownFieldError to match ErrUnknownField")
	}

	cycErr := &CycleError{Anchor: "test", Pos: Position{Line: 1, Column: 1}}
	if !errors.Is(cycErr, ErrCycle) {
		t.Error("expected CycleError to match ErrCycle")
	}

	dupErr := &DuplicateKeyError{Key: "test", Pos: Position{Line: 1, Column: 1}}
	if !errors.Is(dupErr, ErrDuplicateKey) {
		t.Error("expected DuplicateKeyError to match ErrDuplicateKey")
	}
}

func TestErrorIsNonMatch(t *testing.T) {
	synErr := &SyntaxError{Message: "test"}
	if errors.Is(synErr, ErrType) {
		t.Error("SyntaxError should not match ErrType")
	}
	if errors.Is(synErr, ErrCycle) {
		t.Error("SyntaxError should not match ErrCycle")
	}

	typeErr := &TypeError{Errors: []string{"x"}}
	if errors.Is(typeErr, ErrSyntax) {
		t.Error("TypeError should not match ErrSyntax")
	}

	unkErr := &UnknownFieldError{Field: "f"}
	if errors.Is(unkErr, ErrDuplicateKey) {
		t.Error("UnknownFieldError should not match ErrDuplicateKey")
	}

	cycErr := &CycleError{Anchor: "a"}
	if errors.Is(cycErr, ErrUnknownField) {
		t.Error("CycleError should not match ErrUnknownField")
	}

	dupErr := &DuplicateKeyError{Key: "k"}
	if errors.Is(dupErr, ErrCycle) {
		t.Error("DuplicateKeyError should not match ErrCycle")
	}
}

func TestErrorStrings(t *testing.T) {
	syn := &SyntaxError{Message: "bad token", Pos: Position{Line: 5, Column: 3}}
	if !strings.Contains(syn.Error(), "line 5") {
		t.Errorf("expected line in error: %s", syn.Error())
	}

	typ := &TypeError{Errors: []string{"field1 bad", "field2 bad"}}
	if !strings.Contains(typ.Error(), "field1") {
		t.Errorf("expected field1 in error: %s", typ.Error())
	}

	unk := &UnknownFieldError{Field: "mystery", Pos: Position{Line: 3}}
	if !strings.Contains(unk.Error(), "mystery") {
		t.Errorf("expected field name in error: %s", unk.Error())
	}

	cyc := &CycleError{Anchor: "loop", Pos: Position{Line: 7}}
	if !strings.Contains(cyc.Error(), "loop") {
		t.Errorf("expected anchor in error: %s", cyc.Error())
	}

	dup := &DuplicateKeyError{Key: "name", Pos: Position{Line: 2}}
	if !strings.Contains(dup.Error(), "name") {
		t.Errorf("expected key in error: %s", dup.Error())
	}
}

func TestPositionString(t *testing.T) {
	p := Position{Line: 3, Column: 7}
	if p.String() != "3:7" {
		t.Errorf("expected '3:7', got %q", p.String())
	}
}

func TestPositionStringZero(t *testing.T) {
	p := Position{}
	if p.String() != "0:0" {
		t.Errorf("expected '0:0', got %q", p.String())
	}
}

func TestSyntaxErrorNoPosition(t *testing.T) {
	e := &SyntaxError{Message: "generic error"}
	s := e.Error()
	if !strings.Contains(s, "generic error") {
		t.Errorf("expected message in error: %s", s)
	}
	if strings.Contains(s, "line") {
		t.Errorf("should not contain line when Line is 0: %s", s)
	}
}

func TestSyntaxErrorWithPosition(t *testing.T) {
	e := &SyntaxError{Message: "bad", Pos: Position{Line: 10, Column: 5}}
	s := e.Error()
	if !strings.Contains(s, "line 10") {
		t.Errorf("expected line 10: %s", s)
	}
	if !strings.Contains(s, "column 5") {
		t.Errorf("expected column 5: %s", s)
	}
}

func TestFormatError(t *testing.T) {
	data := []byte("key: value\nbad: [unclosed")
	err := &SyntaxError{
		Message: "unterminated flow sequence",
		Pos:     Position{Line: 2, Column: 6},
	}
	formatted := FormatError(data, err)
	if !strings.Contains(formatted, "unterminated") {
		t.Error("expected error message in formatted output")
	}
	if !strings.Contains(formatted, "^") {
		t.Error("expected caret in formatted output")
	}
}

func TestFormatErrorNonSyntaxError(t *testing.T) {
	err := fmt.Errorf("some error")
	got := FormatError([]byte("test"), err)
	if got != "some error" {
		t.Errorf("expected 'some error', got %q", got)
	}
}

func TestFormatErrorOutOfRange(t *testing.T) {
	synErr := &SyntaxError{Message: "bad", Pos: Position{Line: 999, Column: 1}}
	got := FormatError([]byte("one line"), synErr)
	if got != synErr.Error() {
		t.Errorf("expected error string, got %q", got)
	}
}

func TestFormatErrorNegativeLine(t *testing.T) {
	synErr := &SyntaxError{Message: "bad", Pos: Position{Line: -1, Column: 1}}
	got := FormatError([]byte("test"), synErr)
	if got != synErr.Error() {
		t.Errorf("expected error string for negative line, got %q", got)
	}
}

func TestRepeatByte(t *testing.T) {
	if repeatByte(' ', 5) != "     " {
		t.Errorf("expected 5 spaces")
	}
	if repeatByte('x', 3) != "xxx" {
		t.Errorf("expected 'xxx'")
	}
	if repeatByte(' ', 0) != "" {
		t.Errorf("expected empty string for n=0")
	}
	if repeatByte(' ', -1) != "" {
		t.Errorf("expected empty string for n=-1")
	}
}

func TestFormatErrorColumn1(t *testing.T) {
	data := []byte("bad line")
	synErr := &SyntaxError{Message: "error", Pos: Position{Line: 1, Column: 1}}
	formatted := FormatError(data, synErr)
	if !strings.Contains(formatted, "^") {
		t.Error("expected caret")
	}
}
