# Requirements: Go YAML Marshalling/Unmarshalling Package

A list of requirements for implementing the [YAML 1.2.2 specification](https://yaml.org/spec/1.2.2/) in go.

---

## 1. Scope and Goals

- 1.1. The package MUST provide both marshalling (Go value → YAML byte stream) and unmarshalling (YAML byte stream → Go value).
- 1.2. The package MUST be a pure-Go implementation with no required CGO dependencies.
- 1.3. The package MUST target a stable, currently-supported Go toolchain (e.g. the two most recent Go releases) and MUST be `go vet` and `go test -race` clean.
- 1.4. The package SHOULD be importable as a single module with a stable v1 public API and SemVer guarantees.
- 1.5. The package SHOULD be drop-in friendly for users migrating from `gopkg.in/yaml.v3` and `goccy/go-yaml` where feasible (similar `Marshal`/`Unmarshal` signatures and struct tag semantics).

## 2. YAML 1.2.2 Specification Compliance

### 2.1. Character Stream
- 2.1.1. MUST accept input encoded as UTF-8, UTF-16 (LE/BE), and UTF-32 (LE/BE) per §5.2 of the spec.
- 2.1.2. MUST detect and strip a Byte Order Mark (BOM) at the stream start and reject inner BOMs as errors.
- 2.1.3. MUST normalize line breaks (CR, LF, CRLF, NEL, LS, PS) to LF as required.
- 2.1.4. MUST handle the printable character set (`#x9 | #xA | #xD | [#x20-#x7E] | #x85 | [#xA0-#xD7FF] | [#xE000-#xFFFD] | [#x10000-#x10FFFF]`) and reject non-printable input outside escapes.

### 2.2. Lexical Productions
- 2.2.1. MUST tokenize all indicators: `-`, `?`, `:`, `,`, `[`, `]`, `{`, `}`, `#`, `&`, `*`, `!`, `|`, `>`, `'`, `"`, `%`, `@`, `` ` ``.
- 2.2.2. MUST handle indentation correctly, including indentation indicators on block scalars (`|2`, `>-2`, etc.) and detection of indentation errors.
- 2.2.3. MUST treat tabs as illegal where the spec forbids them and as content where the spec allows them.
- 2.2.4. MUST support comments (`#`) attached to lines, including end-of-line and standalone comments, with comment content excluded from the data model unless comment preservation is requested.

### 2.3. Directives
- 2.3.1. MUST parse the `%YAML` directive and validate the version (warn on unsupported minor, reject on unsupported major).
- 2.3.2. MUST parse the `%TAG` directive, including primary (`!`), secondary (`!!`), and named (`!foo!`) tag handles, and resolve handles per scope.
- 2.3.3. MUST support multiple directives in a single document and reset directive state at document boundaries.
- 2.3.4. MUST report unknown directives as a recoverable warning, preserving the unknown directive in the AST when comment/directive preservation is enabled.

### 2.4. Documents and Streams
- 2.4.1. MUST support the `---` directives-end marker and the `...` document-end marker.
- 2.4.2. MUST support multi-document streams via a streaming `Decoder` and a streaming `Encoder`.
- 2.4.3. MUST preserve document boundaries through round-trips.
- 2.4.4. MUST allow bare documents (no leading `---`).

### 2.5. Node Kinds
- 2.5.1. MUST represent the three node kinds: scalar, sequence, mapping.
- 2.5.2. MUST support both block style and flow style for sequences and mappings.
- 2.5.3. MUST support all five scalar styles: plain, single-quoted, double-quoted, literal (`|`), folded (`>`).
- 2.5.4. MUST honour block scalar chomping indicators (`-`, `+`, default "clip").
- 2.5.5. MUST honour line-folding rules in folded scalars and double-quoted scalars (including `\<newline>` line continuations).
- 2.5.6. MUST decode all escape sequences listed in §5.7 of the spec for double-quoted scalars (`\0 \a \b \t \n \v \f \r \e \" \\ \N \_ \L \P \xHH \uHHHH \UHHHHHHHH` and slash escape).
- 2.5.7. MUST recognise the special plain scalar values for the core schema: `null` / `Null` / `NULL` / `~` / empty, booleans (`true`/`false` and case variants), and numeric forms (decimal int, hex `0x`, octal `0o`, infinity `.inf`, NaN `.nan`).

### 2.6. Tags and Schemas
- 2.6.1. MUST implement the **Failsafe**, **JSON**, and **Core** schemas with a configurable default of Core (matching YAML 1.2.2 behaviour).
- 2.6.2. MUST support explicit verbatim tags (`!<tag:yaml.org,2002:str>`), shorthand tags (`!!str`, `!foo!bar`), and non-specific tags (`!`).
- 2.6.3. MUST resolve implicit tags via the active schema's resolution rules.
- 2.6.4. MUST allow user-registered custom tag resolvers and tag → Go type mappings.
- 2.6.5. MUST NOT silently coerce types across schemas; mismatches MUST produce typed errors.

### 2.7. Anchors, Aliases, and Merge Keys
- 2.7.1. MUST support node anchors (`&name`) and aliases (`*name`).
- 2.7.2. MUST resolve aliases lazily so that recursive/self-referential structures can be represented (with cycle detection bounded by configurable depth/size limits to defeat billion-laughs/quadratic-blowup attacks).
- 2.7.3. MUST implement merge keys (`<<:`) per the (legacy) YAML merge type, configurable on/off.
- 2.7.4. MUST preserve anchors on round-trip when the AST/Node API is used.

### 2.8. Conformance and Test Suite
- 2.8.1. MUST be validated against the official [YAML test suite](https://github.com/yaml/yaml-test-suite) with a published pass rate.
- 2.8.2. MUST include a CLI (`yamltest` or similar) that runs the suite and emits a JSON report.
- 2.8.3. SHOULD publish per-tag failure analyses and track them as issues against the package.

## 3. Public API

### 3.1. Top-Level Functions
- 3.1.1. MUST provide `Marshal(v any) ([]byte, error)` and `Unmarshal(data []byte, v any) error` as primary entry points.
- 3.1.2. MUST provide `MarshalWithOptions(v any, opts ...EncodeOption) ([]byte, error)` and `UnmarshalWithOptions(data []byte, v any, opts ...DecodeOption) error`.
- 3.1.3. MUST provide `NewEncoder(w io.Writer, opts ...EncodeOption) *Encoder` and `NewDecoder(r io.Reader, opts ...DecodeOption) *Decoder` for streaming use.
- 3.1.4. MUST provide `Valid(data []byte) bool` and `JSONToYAML` / `YAMLToJSON` helpers.

### 3.2. Encoder/Decoder Types
- 3.2.1. `Encoder` MUST support `Encode`, `EncodeContext` (context-aware), `Close`, and configurable indentation, line width, and document separators.
- 3.2.2. `Decoder` MUST support `Decode`, `DecodeContext`, and reading multiple documents from a single reader until `io.EOF`.
- 3.2.3. Both MUST be safe to use without external locking from a single goroutine; concurrent use across goroutines MUST require external synchronisation and this MUST be documented.

### 3.3. Options (functional options pattern)
- 3.3.1. Encoding options MUST include at minimum: `Indent(int)`, `IndentSequence(bool)`, `Flow(bool)`, `JSON(bool)` (JSON-compatible output), `UseLiteralStyleIfMultiline(bool)`, `UseSingleQuote(bool)`, `OmitEmpty(bool)`, `WithComment(map[string][]Comment)`, `AutoInt(bool)`, `CustomMarshaler[T](func(T) ([]byte, error))`.
- 3.3.2. Decoding options MUST include at minimum: `Strict()` (error on unknown fields), `DisallowDuplicateKey()`, `UseOrderedMap()`, `UseJSONUnmarshaler()`, `AllowDuplicateMapKey()`, `ReferenceFiles(...string)`, `ReferenceDirs(...string)`, `RecursiveDir(bool)`, `Validator(StructValidator)`, `CustomUnmarshaler[T](func(*T, []byte) error)`.
- 3.3.3. Options MUST be additive and order-independent where possible; conflicts MUST produce a clear error at construction time.

### 3.4. Struct Tag Semantics
- 3.4.1. MUST honour the `yaml:"..."` struct tag with field name override.
- 3.4.2. MUST support tag options: `omitempty`, `flow`, `inline`, `anchor`, `alias`, `,inline`, `-` (skip), and `,omitempty`.
- 3.4.3. MUST support anonymous (embedded) struct flattening via `,inline` and the standard Go embedding rules.
- 3.4.4. MUST support the `json:"..."` tag as a fallback when no `yaml` tag is present, configurable on/off.
- 3.4.5. MUST handle conflicting tags between exported fields by producing a deterministic, documented precedence error.

### 3.5. Custom (Un)marshaler Interfaces
- 3.5.1. MUST define `Marshaler` (`MarshalYAML() (any, error)`) and `Unmarshaler` (`UnmarshalYAML(func(any) error) error`) interfaces compatible in spirit with both `gopkg.in/yaml.v3` and `goccy/go-yaml`.
- 3.5.2. MUST define byte-level interfaces `BytesMarshaler` (`MarshalYAML() ([]byte, error)`) and `BytesUnmarshaler` (`UnmarshalYAML([]byte) error`).
- 3.5.3. MUST define context-aware variants `InterfaceMarshalerContext` and `InterfaceUnmarshalerContext` accepting `context.Context`.
- 3.5.4. MUST fall back to `encoding.TextMarshaler` / `encoding.TextUnmarshaler` and to `json.Marshaler` / `json.Unmarshaler` (configurable) when YAML interfaces are absent.

### 3.6. Type Mapping
- 3.6.1. MUST support all Go primitive types, `string`, `[]byte` (base64 binary tag option), arrays, slices, maps with comparable keys, structs, pointers, and interfaces.
- 3.6.2. MUST support `time.Time` and `time.Duration` with configurable format.
- 3.6.3. MUST support `*big.Int`, `*big.Float`, `*big.Rat` when present.
- 3.6.4. MUST support `encoding.TextMarshaler`/`Unmarshaler` types.
- 3.6.5. MUST support a built-in ordered map type (e.g. `yaml.MapSlice` or `yaml.OrderedMap`) preserving key insertion order on both decode and encode.
- 3.6.6. MUST handle decoding into `any` (a.k.a. `interface{}`) producing canonical Go types: `map[string]any` (or ordered-map when option enabled), `[]any`, `string`, `int64`, `uint64`, `float64`, `bool`, or `nil`.

## 4. AST / Node API

- 4.1. MUST expose a documented AST package providing typed nodes for: `DocumentNode`, `MappingNode`, `SequenceNode`, `ScalarNode` (with style variants), `AnchorNode`, `AliasNode`, `TagNode`, `CommentNode`, `DirectiveNode`.
- 4.2. MUST allow parsing into the AST without value coercion (`parser.Parse(src) (*ast.File, error)`).
- 4.3. MUST allow round-tripping AST → bytes preserving styles, comments, anchors, aliases, tags, and indentation as faithfully as possible.
- 4.4. MUST provide visitor / walker utilities (`ast.Walk`, `ast.Filter`).
- 4.5. MUST allow programmatic editing of the AST (insert, delete, replace nodes) with consistency checks.

## 5. Path Queries

- 5.1. MUST provide a YAMLPath expression engine inspired by `goccy/go-yaml`'s `yaml.PathString` (e.g. `$.foo.bar[0].baz`).
- 5.2. MUST support: root selector `$`, child accessor `.name`, index accessor `[N]`, wildcard `*`, recursive descent `..`.
- 5.3. MUST allow path-based read (`Read`), write (`Replace`), append (`Append`), and delete (`Delete`) operations against either a byte stream or an AST.
- 5.4. MUST return precise location information (file, line, column, byte offset) for every match.

## 6. Error Reporting and Diagnostics

- 6.1. Errors MUST include source position (line, column, byte offset) and the offending token where applicable.
- 6.2. The package MUST provide pretty-printed errors with a source snippet and a caret pointing at the error column, optionally with ANSI colour (configurable / auto-detected from terminal).
- 6.3. Errors MUST be inspectable via `errors.Is` / `errors.As` against typed sentinel errors (e.g. `ErrSyntax`, `ErrType`, `ErrUnknownField`, `ErrCycle`, `ErrDuplicateKey`).
- 6.4. Decoding into a concrete type MUST yield a `TypeError` that aggregates per-field issues rather than failing on the first issue, when running in non-strict mode.
- 6.5. The package MUST log nothing to stdout/stderr by default; diagnostics MUST be returned through error values or supplied callbacks.

## 7. Comment Handling

- 7.1. MUST support attaching comments to nodes (head, line, foot) on the AST.
- 7.2. MUST support encoding with comments via either AST construction or a `WithComment` option that maps YAMLPath strings to comment strings.
- 7.3. MUST round-trip comments without reordering or duplication when only AST mutations are performed.

## 8. Reference / Include Resolution

- 8.1. MUST support resolving anchors defined in external files via `ReferenceFiles` and `ReferenceDirs` options.
- 8.2. MUST detect duplicate anchor definitions across files and surface a clear error.
- 8.3. MUST guard against directory traversal and symlink escape when using `ReferenceDirs`.

## 9. Validation

- 9.1. MUST allow plugging in a struct validator (e.g. `go-playground/validator`) via a `Validator` option that runs after decoding.
- 9.2. MUST surface validation errors with the same positional metadata as syntax errors so users can pretty-print them identically.
- 9.3. SHOULD provide a small built-in tag-driven validator (`yaml:"name,required"`) for simple cases without forcing a third-party dependency.

## 10. JSON Interoperability

- 10.1. MUST provide `YAMLToJSON([]byte) ([]byte, error)` and `JSONToYAML([]byte) ([]byte, error)` helpers.
- 10.2. MUST provide a JSON-compatible encoding mode that emits valid JSON (no comments, no anchors, double-quoted strings, flow style only).
- 10.3. MUST honour `json:` struct tags as documented in 3.4.4.

## 11. Performance

- 11.1. SHOULD parse at a throughput within 2× of `goccy/go-yaml` for a representative corpus, measured on a documented benchmark suite.
- 11.2. MUST avoid quadratic-time pathologies on adversarial input (deeply nested aliases, billion-laughs).
- 11.3. MUST allow capping decode resource usage via options: `MaxDocumentSize`, `MaxAliasExpansion`, `MaxDepth`, `MaxNodes`.
- 11.4. MUST minimise allocations in the hot path for the lexer; the public API MAY accept `[]byte` or `io.Reader` and SHOULD avoid copying when given `[]byte`.
- 11.5. MUST ship benchmarks in the repository under `./bench` covering small, medium, and large representative documents.

## 12. Security

- 12.1. MUST default to safe behaviour: bounded alias expansion, bounded depth, no arbitrary type construction unless registered.
- 12.2. MUST NOT execute or evaluate any embedded code, expressions, or templates.
- 12.3. MUST treat tag-driven type construction as opt-in via a registry; unknown explicit tags MUST NOT cause panics.
- 12.4. MUST be fuzz-tested using Go's native fuzzing (`go test -fuzz`); fuzz corpora MUST be committed.
- 12.5. MUST be free of `panic` in the public API for any input; all error paths MUST return `error`.

## 13. Concurrency

- 13.1. The top-level `Marshal`/`Unmarshal` functions MUST be safe for concurrent use.
- 13.2. `Encoder`/`Decoder` instances MUST NOT require concurrency safety; documentation MUST state they are intended for single-goroutine use.
- 13.3. Internal caches (e.g. struct field cache) MUST be concurrency-safe.

## 14. Testing

- 14.1. MUST achieve a documented unit-test line coverage target (e.g. ≥85%) on the core packages.
- 14.2. MUST include conformance tests against the official YAML test suite.
- 14.3. MUST include round-trip tests: parse → emit → parse and assert structural equivalence.
- 14.4. MUST include differential tests against `gopkg.in/yaml.v3` and/or `goccy/go-yaml` for the Core schema.
- 14.5. MUST include fuzz tests for the lexer, parser, and decoder.
- 14.6. MUST include benchmarks tracked in CI to detect regressions.

## 15. Documentation

- 15.1. MUST ship a `README.md` covering installation, quick start, feature matrix, and migration notes from `gopkg.in/yaml.v3` and `goccy/go-yaml`.
- 15.2. MUST ship complete `godoc` for every exported identifier with at least one runnable example per major API.
- 15.3. MUST ship a `CHANGELOG.md` following Keep-a-Changelog conventions.
- 15.4. MUST ship a `SECURITY.md` describing the vulnerability disclosure process and the resource-limit defaults.
- 15.5. SHOULD ship a documentation site (e.g. via `pkg.go.dev` plus a small `docs/` site) covering: the AST, YAMLPath, custom tags, comment preservation, and JSON interop.

## 16. Tooling

- 16.1. SHOULD provide a `cmd/yfmt` (or similar) command-line YAML formatter built on the AST, supporting `--check`, `--write`, `--indent N`, and `--flow`.
- 16.2. SHOULD provide a `cmd/ylint` linter exposing duplicate-key, undefined-anchor, and indentation diagnostics.
- 16.3. SHOULD provide a `cmd/yp` CLI for evaluating YAMLPath expressions against files.

## 17. Repository / Project Hygiene

- 17.1. MUST be hosted under a clear OSS license (e.g. MIT or Apache-2.0), with the license file at the repo root.
- 17.2. MUST configure CI for: build, vet, staticcheck, race-enabled tests, fuzz smoke runs, conformance suite, benchmarks.
- 17.3. MUST publish releases as tagged Git versions usable by `go get`.
- 17.4. SHOULD adopt Conventional Commits and an automated release-notes workflow.
- 17.5. MUST include `CONTRIBUTING.md` and a `CODE_OF_CONDUCT.md`.

## 18. Out of Scope (explicitly noted)

18.1. YAML 1.1 quirks beyond what the Core schema and merge-key support cover (e.g. sexagesimal numbers, `yes`/`no` booleans) are out of scope by default but MAY be enabled via an opt-in `YAML11Compat` option.
18.2. Schema-aware validation beyond the plug-in `Validator` hook (e.g. JSON Schema enforcement) is out of scope for v1.
18.3. Streaming partial decode of a single huge document (event-based SAX-style API) is out of scope for v1 but SHOULD be considered for v2.

---

## Appendix A: Feature Parity Checklist with goccy/go-yaml

The following items from `goccy/go-yaml` MUST be supported (see §3 and §4 above for the formal requirements):

- `yaml.Marshal` / `yaml.Unmarshal` top-level functions
- `yaml.NewEncoder` / `yaml.NewDecoder` with functional options
- Pretty error messages with source snippet and caret
- `yaml.PathString` and the YAMLPath query/edit API
- Anchor and alias support, including encoding to anchors via struct tags
- Reference resolution via `ReferenceFiles` / `ReferenceDirs`
- `yaml.MapSlice` ordered-map type
- `yaml.CustomMarshaler` / `yaml.CustomUnmarshaler` registration helpers
- AST package with full round-trip preservation
- `yaml.JSONToYAML` / `yaml.YAMLToJSON`
- Strict decoding mode (unknown-field error)
- `UseOrderedMap` decode option
- Comment preservation and `WithComment` encode option
- Validator integration

## Appendix B: YAML 1.2.2 Production Coverage Checklist

Track each numbered production from the spec to a passing test:

- §5 Character productions (c-printable, nb-json, b-break, s-white, ns-char, etc.)
- §6 Structural productions (s-indent, s-separate, comments, directives)
- §7 Flow style productions (flow scalars, flow sequences, flow mappings, alias, anchor, tag)
- §8 Block style productions (literal, folded, block sequences, block mappings)
- §9 Document stream productions (bare/explicit document, stream)
- §10 Schema resolution (failsafe, JSON, core) and their tag resolution tables
