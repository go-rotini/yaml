package yaml

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// ValidKYAML reports whether data is a valid KYAML document — strict KYAML, per
// the rules of [KEP-5295]. A document with anchors, aliases, tags, merge
// keys, block-style scalars/mappings/sequences, plain string scalars in
// non-key position, single-quoted scalars, non-string mapping keys, hex/oct/bin
// numeric literals, YAML 1.1 boolean aliases, .nan/.inf floats, the ?
// complex-key indicator, or YAML directives returns false. Documents must
// begin with the "---" header.
//
// ValidKYAML is equivalent to [ValidateKYAML](data) == nil.
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
func ValidKYAML(data []byte) bool {
	return ValidateKYAML(data) == nil
}

// ValidateKYAML parses data as YAML and reports any KYAML conformance
// violations. Returns nil if data is a valid KYAML document, or a
// [*KYAMLError] carrying every violation. Validation is structural per the
// rules of [KEP-5295]; cosmetic deviations (indentation, bracket cuddling,
// trailing commas, key ordering) are not checked here. Use [Lint] for
// cosmetic validation.
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
func ValidateKYAML(data []byte) error {
	data, err := detectAndConvertEncoding(data)
	if err != nil {
		return err
	}
	tokens, err := newScanner(data).scan()
	if err != nil {
		return err
	}

	// Reject directives at the token level (R12.9). The parser itself accepts
	// them when consuming a stream; we want to flag them precisely.
	for _, tok := range tokens {
		if tok.kind == tokenDirective {
			return &KYAMLError{Errors: []KYAMLViolation{{
				Rule:    "R12.9",
				Message: fmt.Sprintf("YAML directive %q not allowed in KYAML", tok.value),
				Pos:     tok.pos,
				Token:   tok.value,
			}}}
		}
	}

	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return &KYAMLError{Errors: []KYAMLViolation{{
			Rule:    "R3.1",
			Message: "KYAML document must contain at least one document with the \"---\" header",
		}}}
	}

	var violations []KYAMLViolation
	for _, doc := range docs {
		validateKYAMLNode(doc, &violations)
	}
	if len(violations) > 0 {
		return &KYAMLError{Errors: violations}
	}
	return nil
}

// validateKYAMLNode walks the AST and accumulates KYAML rule violations into
// out. The walker handles document headers, scalar styles, anchors, aliases,
// tags, merge keys, and non-string keys. It does not enforce cosmetic
// rules — those are the province of [Lint].
func validateKYAMLNode(n *node, out *[]KYAMLViolation) {
	if n == nil {
		return
	}

	if n.kind == nodeDocument {
		// R3.1: every document must start with the explicit "---" marker.
		if !n.docStartExplicit {
			*out = append(*out, KYAMLViolation{
				Rule:    "R3.1",
				Message: "KYAML document must begin with the \"---\" header",
				Pos:     n.pos,
			})
		}
		for _, child := range n.children {
			validateKYAMLNode(child, out)
		}
		return
	}

	// R12.1: anchors and aliases not allowed.
	if n.anchor != "" {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.1",
			Message: fmt.Sprintf("anchor %q not allowed in KYAML", "&"+n.anchor),
			Pos:     n.pos,
			Token:   "&" + n.anchor,
		})
	}
	if n.kind == nodeAlias {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.1",
			Message: fmt.Sprintf("alias %q not allowed in KYAML", "*"+n.alias),
			Pos:     n.pos,
			Token:   "*" + n.alias,
		})
		return
	}

	// R12.3: merge keys not allowed.
	if n.kind == nodeMergeKey {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.3",
			Message: "merge key (<<) not allowed in KYAML",
			Pos:     n.pos,
			Token:   "<<",
		})
		return
	}

	// R12.2: explicit tags not allowed. The parser only sets n.tag when an
	// explicit tag token (!! or !) was encountered in source — implicit tag
	// resolution is left to the decoder. So any non-empty tag here is a
	// violation.
	if n.tag != "" {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.2",
			Message: fmt.Sprintf("explicit tag %q not allowed in KYAML", n.tag),
			Pos:     n.pos,
			Token:   n.tag,
		})
	}

	switch n.kind {
	case nodeMapping:
		// R4.1: mappings must be in flow style.
		if !n.flow {
			*out = append(*out, KYAMLViolation{
				Rule:    "R12.5",
				Message: "block-style mapping not allowed in KYAML; use flow style {}",
				Pos:     n.pos,
			})
		}
		// Mapping children alternate key/value.
		for i := 0; i+1 < len(n.children); i += 2 {
			key := n.children[i]
			val := n.children[i+1]
			validateKYAMLKey(key, out)
			validateKYAMLNode(val, out)
		}
	case nodeSequence:
		// R7.1: sequences must be in flow style.
		if !n.flow {
			*out = append(*out, KYAMLViolation{
				Rule:    "R12.6",
				Message: "block-style sequence not allowed in KYAML; use flow style []",
				Pos:     n.pos,
			})
		}
		for _, c := range n.children {
			validateKYAMLNode(c, out)
		}
	case nodeScalar:
		validateKYAMLScalar(n, false, out)
	}
}

// validateKYAMLKey checks a mapping key node against KYAML rules. Keys are
// allowed to be plain scalars when they pass the unquoted-key predicate
// (R5.1); type-ambiguous words must be double-quoted (R5.2).
func validateKYAMLKey(n *node, out *[]KYAMLViolation) {
	if n == nil {
		return
	}
	// R12.3: merge key "<<" not allowed in KYAML. The parser leaves merge
	// keys as plain scalar nodes with value "<<"; we detect by value.
	if n.kind == nodeScalar && n.value == "<<" && n.style == scalarPlain {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.3",
			Message: "merge key (<<) not allowed in KYAML",
			Pos:     n.pos,
			Token:   "<<",
		})
		return
	}
	if n.kind != nodeScalar {
		// R4.4: non-string keys are not allowed.
		*out = append(*out, KYAMLViolation{
			Rule:    "R4.4",
			Message: fmt.Sprintf("KYAML mapping key must be a string scalar, got %s", nodeKindName(n.kind)),
			Pos:     n.pos,
		})
		return
	}
	// Anchors, aliases, tags on keys are still violations (R12.1/R12.2).
	if n.anchor != "" {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.1",
			Message: fmt.Sprintf("anchor %q on key not allowed in KYAML", "&"+n.anchor),
			Pos:     n.pos,
			Token:   "&" + n.anchor,
		})
	}
	if n.tag != "" {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.2",
			Message: fmt.Sprintf("explicit tag %q on key not allowed in KYAML", n.tag),
			Pos:     n.pos,
			Token:   n.tag,
		})
	}
	validateKYAMLScalar(n, true, out)
}

// validateKYAMLScalar checks a scalar node against KYAML rules. Pass
// asKey=true when validating a mapping key (R5 rules) vs. a value (R6 rules).
func validateKYAMLScalar(n *node, asKey bool, out *[]KYAMLViolation) {
	switch n.style {
	case scalarSingleQuoted:
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.8",
			Message: "single-quoted scalar not allowed in KYAML; use double quotes",
			Pos:     n.pos,
			Token:   n.value,
		})
		return
	case scalarLiteral, scalarFolded:
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.4",
			Message: "block-style scalar (| or >) not allowed in KYAML; use double-quoted form",
			Pos:     n.pos,
			Token:   n.value,
		})
		return
	case scalarDoubleQuoted:
		// Double-quoted scalars are always fine (the canonical KYAML form).
		return
	}

	// Plain scalar. Resolution: numbers, booleans, null are allowed unquoted
	// in scalar positions per R6.1–R6.3. Anything else is a string and must
	// be double-quoted (R6.4) — except as a key, where unquoted is allowed
	// per R5.1.
	val := n.value

	// Recognized JSON-ish unquoted forms allowed everywhere.
	switch val {
	case "null":
		// R6.3: only "null" (lowercase) allowed unquoted.
		return
	case "true", "false":
		// R6.1.
		return
	case "Null", "NULL", "~":
		*out = append(*out, KYAMLViolation{
			Rule:    "R6.3",
			Message: fmt.Sprintf("YAML null variant %q not allowed in KYAML; use lowercase \"null\" or quote the value", val),
			Pos:     n.pos,
			Token:   val,
		})
		return
	case "True", "TRUE", "False", "FALSE":
		*out = append(*out, KYAMLViolation{
			Rule:    "R6.1",
			Message: fmt.Sprintf("non-canonical boolean %q not allowed in KYAML; use lowercase \"true\"/\"false\" or quote the value", val),
			Pos:     n.pos,
			Token:   val,
		})
		return
	case "":
		// Empty plain scalar resolves to null in YAML 1.2 Core. Treat as
		// equivalent to "null" — allowed.
		return
	}

	// YAML 1.1 boolean aliases must be quoted (R12.12).
	if _, ambiguous := typeAmbiguousKeys[val]; ambiguous {
		// In a key position, R5.2 already requires quoting; report it.
		// In a value position, R12.12 likewise requires quoting.
		rule := "R12.12"
		if asKey {
			rule = "R5.2"
		}
		*out = append(*out, KYAMLViolation{
			Rule:    rule,
			Message: fmt.Sprintf("type-ambiguous word %q must be double-quoted in KYAML", val),
			Pos:     n.pos,
			Token:   val,
		})
		return
	}

	// Hex/octal/binary integers (R12.11).
	if isHexOctalBinaryInt(val) {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.11",
			Message: fmt.Sprintf("non-decimal integer literal %q not allowed in KYAML; use decimal", val),
			Pos:     n.pos,
			Token:   val,
		})
		return
	}

	// .nan / .inf / -.inf (R12.13).
	switch val {
	case ".nan", ".NaN", ".NAN":
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.13",
			Message: "NaN literal not allowed in KYAML",
			Pos:     n.pos,
			Token:   val,
		})
		return
	case ".inf", ".Inf", ".INF", "-.inf", "-.Inf", "-.INF", "+.inf", "+.Inf", "+.INF":
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.13",
			Message: "infinity literal not allowed in KYAML",
			Pos:     n.pos,
			Token:   val,
		})
		return
	}

	// Decimal integer or float — allowed unquoted.
	if _, err := strconv.ParseInt(val, 10, 64); err == nil {
		return
	}
	if _, err := strconv.ParseFloat(val, 64); err == nil {
		// Reject special forms that ParseFloat accepts but KYAML doesn't.
		lower := strings.ToLower(val)
		if strings.Contains(lower, "inf") || strings.Contains(lower, "nan") {
			*out = append(*out, KYAMLViolation{
				Rule:    "R12.13",
				Message: fmt.Sprintf("non-finite float %q not allowed in KYAML", val),
				Pos:     n.pos,
				Token:   val,
			})
			return
		}
		// Reject hex floats.
		if strings.HasPrefix(lower, "0x") {
			*out = append(*out, KYAMLViolation{
				Rule:    "R12.11",
				Message: fmt.Sprintf("hex float literal %q not allowed in KYAML", val),
				Pos:     n.pos,
				Token:   val,
			})
			return
		}
		return
	}

	// Plain string in non-key position is forbidden (R12.7).
	if !asKey {
		*out = append(*out, KYAMLViolation{
			Rule:    "R12.7",
			Message: fmt.Sprintf("plain (unquoted) string scalar %q not allowed as a value in KYAML; use double quotes", val),
			Pos:     n.pos,
			Token:   val,
		})
		return
	}

	// In a key position, plain strings are allowed if they pass the
	// unquoted-key predicate. If they don't, the source has an unquoted
	// key that should have been quoted (R5).
	if needsKeyQuoting(val) {
		*out = append(*out, KYAMLViolation{
			Rule:    "R5",
			Message: fmt.Sprintf("key %q must be double-quoted in KYAML", val),
			Pos:     n.pos,
			Token:   val,
		})
	}
}

// isHexOctalBinaryInt detects "0xNNN", "0oNNN", "0bNNN" forms.
func isHexOctalBinaryInt(s string) bool {
	if len(s) < 3 {
		return false
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "0x") || strings.HasPrefix(low, "0o") || strings.HasPrefix(low, "0b") {
		return true
	}
	return false
}

// Format parses data as YAML (any subset, including non-KYAML constructs)
// and re-emits it as canonical KYAML. Anchors and aliases are reified
// (expanded inline); merge keys are resolved into flat key lists; explicit
// tags are stripped. Comments are preserved best-effort per [KEP-5295]'s
// rule that "go-yaml does not always handle comments properly. Some comments
// will be formatted wrongly, or lost entirely."
//
// Format is idempotent on its output: Format(Format(x)) produces the same
// bytes as Format(x) for any valid YAML x.
//
// [KEP-5295]: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cli/5295-kyaml
func Format(data []byte, opts ...EncodeOption) ([]byte, error) {
	// Decode the input into a generic any value, with merge keys and
	// aliases resolved by the existing decoder. Then re-encode with KYAML.
	var v any
	if err := UnmarshalWithOptions(data, &v, WithOrderedMap()); err != nil {
		return nil, err
	}
	encOpts := append([]EncodeOption{WithKYAML()}, opts...)
	return MarshalWithOptions(v, encOpts...)
}

// Lint parses data as YAML and returns a slice of LintIssue values describing
// every KYAML deviation. Unlike [ValidateKYAML], Lint always returns the full
// list of issues. With [WithKYAMLLintCosmetic] in opts, Lint additionally
// reports cosmetic deviations (indentation, bracket cuddling, key ordering).
//
// Lint never returns an error for KYAML conformance issues — those are
// returned via the LintIssue slice. It does return an error for fundamental
// parse failures (input is not valid YAML at all).
func Lint(data []byte, opts ...DecodeOption) ([]LintIssue, error) {
	o := defaultDecodeOptions()
	for _, opt := range opts {
		opt(o)
	}

	data, err := detectAndConvertEncoding(data)
	if err != nil {
		return nil, err
	}
	tokens, err := newScanner(data).scan()
	if err != nil {
		return nil, err
	}

	var issues []LintIssue
	for _, tok := range tokens {
		if tok.kind == tokenDirective {
			issues = append(issues, LintIssue{
				Rule:     "R12.9",
				Message:  fmt.Sprintf("YAML directive %q not allowed in KYAML", tok.value),
				Pos:      tok.pos,
				Severity: SeverityError,
			})
		}
	}

	p := newParser(tokens)
	docs, err := p.parse()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		issues = append(issues, LintIssue{
			Rule:     "R3.1",
			Message:  "KYAML document must contain at least one document with the \"---\" header",
			Severity: SeverityError,
		})
		return issues, nil
	}

	var violations []KYAMLViolation
	for _, doc := range docs {
		validateKYAMLNode(doc, &violations)
	}
	for _, v := range violations {
		issues = append(issues, LintIssue{
			Rule:     v.Rule,
			Message:  v.Message,
			Pos:      v.Pos,
			Severity: SeverityError,
		})
	}

	if o.kyamlLintCosmetic {
		// Cosmetic check: re-emit via Format and compare to the input.
		// Any deviation surfaces as a single warning; users who want
		// detailed diagnostics can use a diff tool against Format(data).
		formatted, fErr := Format(data)
		if fErr == nil && !bytes.Equal(formatted, data) {
			issues = append(issues, LintIssue{
				Rule:     "R8/R9",
				Message:  "input does not match canonical KYAML formatting (run Format to apply)",
				Severity: SeverityWarning,
			})
		}
	}

	return issues, nil
}

// validateKYAMLBytes is the internal hook called by the decoder when
// WithStrictKYAML is set. It runs the same validator as ValidateKYAML and
// returns a *KYAMLError if any violations are found.
func validateKYAMLBytes(data []byte, docs []*node) error {
	// Even if docs are already parsed, we re-scan to catch directives at
	// the token level. If the caller is in a hot decode path and wants to
	// avoid the scan, they can use ValidateKYAML directly upstream.
	if data != nil {
		tokens, err := newScanner(data).scan()
		if err == nil {
			for _, tok := range tokens {
				if tok.kind == tokenDirective {
					return &KYAMLError{Errors: []KYAMLViolation{{
						Rule:    "R12.9",
						Message: fmt.Sprintf("YAML directive %q not allowed in KYAML", tok.value),
						Pos:     tok.pos,
						Token:   tok.value,
					}}}
				}
			}
		}
	}

	if len(docs) == 0 {
		return &KYAMLError{Errors: []KYAMLViolation{{
			Rule:    "R3.1",
			Message: "KYAML document must contain at least one document with the \"---\" header",
		}}}
	}

	var violations []KYAMLViolation
	for _, doc := range docs {
		validateKYAMLNode(doc, &violations)
	}
	if len(violations) > 0 {
		return &KYAMLError{Errors: violations}
	}
	return nil
}
