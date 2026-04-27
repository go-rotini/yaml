package yaml

import (
	"reflect"
	"testing"
)

func TestDefaultEncodeOptions(t *testing.T) {
	o := defaultEncodeOptions()
	if o.indent != 2 {
		t.Errorf("expected indent=2, got %d", o.indent)
	}
	if o.lineWidth != 80 {
		t.Errorf("expected lineWidth=80, got %d", o.lineWidth)
	}
	if o.indentSequence {
		t.Error("expected indentSequence=false")
	}
	if o.flow || o.jsonCompat || o.useLiteral || o.useSingleQuote || o.omitEmpty || o.autoInt {
		t.Error("expected all bool options false by default")
	}
	if o.comments != nil {
		t.Error("expected nil comments by default")
	}
	if o.customMarshalers != nil {
		t.Error("expected nil customMarshalers by default")
	}
}

func TestDefaultDecodeOptions(t *testing.T) {
	o := defaultDecodeOptions()
	if o.maxDepth != 100 {
		t.Errorf("expected maxDepth=100, got %d", o.maxDepth)
	}
	if o.maxAliasExpansion != 1000 {
		t.Errorf("expected maxAliasExpansion=1000, got %d", o.maxAliasExpansion)
	}
	if o.strict || o.disallowDuplicates || o.allowDuplicates || o.useOrderedMap || o.useJSONUnmarshaler || o.recursiveDir {
		t.Error("expected all bool options false by default")
	}
	if o.maxDocumentSize != 0 || o.maxNodes != 0 {
		t.Error("expected maxDocumentSize and maxNodes 0 by default")
	}
}

func TestIndentOption(t *testing.T) {
	o := defaultEncodeOptions()
	Indent(4)(o)
	if o.indent != 4 {
		t.Errorf("expected indent=4, got %d", o.indent)
	}
}

func TestIndentSequenceOption(t *testing.T) {
	o := defaultEncodeOptions()
	IndentSequence(true)(o)
	if !o.indentSequence {
		t.Error("expected indentSequence=true")
	}
}

func TestFlowOption(t *testing.T) {
	o := defaultEncodeOptions()
	Flow(true)(o)
	if !o.flow {
		t.Error("expected flow=true")
	}
}

func TestJSONOption(t *testing.T) {
	o := defaultEncodeOptions()
	JSON(true)(o)
	if !o.jsonCompat {
		t.Error("expected jsonCompat=true")
	}
}

func TestUseLiteralStyleOption(t *testing.T) {
	o := defaultEncodeOptions()
	UseLiteralStyleIfMultiline(true)(o)
	if !o.useLiteral {
		t.Error("expected useLiteral=true")
	}
}

func TestUseSingleQuoteOption(t *testing.T) {
	o := defaultEncodeOptions()
	UseSingleQuote(true)(o)
	if !o.useSingleQuote {
		t.Error("expected useSingleQuote=true")
	}
}

func TestOmitEmptyOption(t *testing.T) {
	o := defaultEncodeOptions()
	OmitEmpty(true)(o)
	if !o.omitEmpty {
		t.Error("expected omitEmpty=true")
	}
}

func TestAutoIntOption(t *testing.T) {
	o := defaultEncodeOptions()
	AutoInt(true)(o)
	if !o.autoInt {
		t.Error("expected autoInt=true")
	}
}

func TestLineWidthOptionSetter(t *testing.T) {
	o := defaultEncodeOptions()
	LineWidth(120)(o)
	if o.lineWidth != 120 {
		t.Errorf("expected lineWidth=120, got %d", o.lineWidth)
	}
}

func TestWithCommentOption(t *testing.T) {
	comments := map[string][]Comment{
		"key": {{Position: HeadCommentPos, Text: "head comment"}},
	}
	o := defaultEncodeOptions()
	WithComment(comments)(o)
	if o.comments == nil {
		t.Fatal("expected non-nil comments")
	}
	if len(o.comments["key"]) != 1 {
		t.Errorf("expected 1 comment for key, got %d", len(o.comments["key"]))
	}
}

func TestStrictOption(t *testing.T) {
	o := defaultDecodeOptions()
	Strict()(o)
	if !o.strict {
		t.Error("expected strict=true")
	}
}

func TestDisallowDuplicateKeyOption(t *testing.T) {
	o := defaultDecodeOptions()
	DisallowDuplicateKey()(o)
	if !o.disallowDuplicates {
		t.Error("expected disallowDuplicates=true")
	}
}

func TestAllowDuplicateMapKeyOption(t *testing.T) {
	o := defaultDecodeOptions()
	AllowDuplicateMapKey()(o)
	if !o.allowDuplicates {
		t.Error("expected allowDuplicates=true")
	}
}

func TestUseOrderedMapOption(t *testing.T) {
	o := defaultDecodeOptions()
	UseOrderedMap()(o)
	if !o.useOrderedMap {
		t.Error("expected useOrderedMap=true")
	}
}

func TestUseJSONUnmarshalerOption(t *testing.T) {
	o := defaultDecodeOptions()
	UseJSONUnmarshaler()(o)
	if !o.useJSONUnmarshaler {
		t.Error("expected useJSONUnmarshaler=true")
	}
}

func TestMaxDepthOption(t *testing.T) {
	o := defaultDecodeOptions()
	MaxDepth(50)(o)
	if o.maxDepth != 50 {
		t.Errorf("expected maxDepth=50, got %d", o.maxDepth)
	}
}

func TestMaxAliasExpansionOption(t *testing.T) {
	o := defaultDecodeOptions()
	MaxAliasExpansion(500)(o)
	if o.maxAliasExpansion != 500 {
		t.Errorf("expected maxAliasExpansion=500, got %d", o.maxAliasExpansion)
	}
}

func TestMaxDocumentSizeOption(t *testing.T) {
	o := defaultDecodeOptions()
	MaxDocumentSize(1024)(o)
	if o.maxDocumentSize != 1024 {
		t.Errorf("expected maxDocumentSize=1024, got %d", o.maxDocumentSize)
	}
}

func TestMaxNodesOption(t *testing.T) {
	o := defaultDecodeOptions()
	MaxNodes(100)(o)
	if o.maxNodes != 100 {
		t.Errorf("expected maxNodes=100, got %d", o.maxNodes)
	}
}

func TestRecursiveDirOptionSetter(t *testing.T) {
	o := defaultDecodeOptions()
	RecursiveDir(true)(o)
	if !o.recursiveDir {
		t.Error("expected recursiveDir=true")
	}
}

func TestValidatorOptionSetter(t *testing.T) {
	o := defaultDecodeOptions()
	Validator(nil)(o)
	if o.validator != nil {
		t.Error("expected nil validator")
	}
}

func TestReferenceFilesOption(t *testing.T) {
	o := defaultDecodeOptions()
	ReferenceFiles("a.yaml", "b.yaml")(o)
	if len(o.referenceFiles) != 2 {
		t.Fatalf("expected 2 reference files, got %d", len(o.referenceFiles))
	}
	if o.referenceFiles[0] != "a.yaml" || o.referenceFiles[1] != "b.yaml" {
		t.Errorf("expected [a.yaml b.yaml], got %v", o.referenceFiles)
	}
}

func TestReferenceFilesOptionAppend(t *testing.T) {
	o := defaultDecodeOptions()
	ReferenceFiles("a.yaml")(o)
	ReferenceFiles("b.yaml")(o)
	if len(o.referenceFiles) != 2 {
		t.Fatalf("expected 2 reference files, got %d", len(o.referenceFiles))
	}
}

func TestReferenceDirsOption(t *testing.T) {
	o := defaultDecodeOptions()
	ReferenceDirs("/dir1", "/dir2")(o)
	if len(o.referenceDirs) != 2 {
		t.Fatalf("expected 2 reference dirs, got %d", len(o.referenceDirs))
	}
}

func TestReferenceDirsOptionAppend(t *testing.T) {
	o := defaultDecodeOptions()
	ReferenceDirs("/dir1")(o)
	ReferenceDirs("/dir2")(o)
	if len(o.referenceDirs) != 2 {
		t.Fatalf("expected 2 reference dirs, got %d", len(o.referenceDirs))
	}
}

func TestCustomMarshalerOption(t *testing.T) {
	o := defaultEncodeOptions()
	CustomMarshaler(func(s string) ([]byte, error) {
		return []byte(s), nil
	})(o)
	if o.customMarshalers == nil {
		t.Fatal("expected non-nil customMarshalers")
	}
	if _, ok := o.customMarshalers[reflect.TypeFor[string]()]; !ok {
		t.Error("expected string type in customMarshalers")
	}
}

func TestCustomMarshalerOptionMultiple(t *testing.T) {
	o := defaultEncodeOptions()
	CustomMarshaler(func(s string) ([]byte, error) {
		return []byte(s), nil
	})(o)
	CustomMarshaler(func(n int) ([]byte, error) {
		return nil, nil
	})(o)
	if len(o.customMarshalers) != 2 {
		t.Errorf("expected 2 custom marshalers, got %d", len(o.customMarshalers))
	}
}

func TestCustomUnmarshalerOption(t *testing.T) {
	o := defaultDecodeOptions()
	CustomUnmarshaler(func(s *string, raw []byte) error {
		*s = string(raw)
		return nil
	})(o)
	if o.customUnmarshalers == nil {
		t.Fatal("expected non-nil customUnmarshalers")
	}
	if _, ok := o.customUnmarshalers[reflect.TypeFor[string]()]; !ok {
		t.Error("expected string type in customUnmarshalers")
	}
}

func TestCustomUnmarshalerOptionMultiple(t *testing.T) {
	o := defaultDecodeOptions()
	CustomUnmarshaler(func(s *string, raw []byte) error {
		return nil
	})(o)
	CustomUnmarshaler(func(n *int, raw []byte) error {
		return nil
	})(o)
	if len(o.customUnmarshalers) != 2 {
		t.Errorf("expected 2 custom unmarshalers, got %d", len(o.customUnmarshalers))
	}
}

func TestWithTagResolverOption(t *testing.T) {
	resolver := &TagResolver{
		Tag:    "!custom",
		GoType: reflect.TypeFor[string](),
		Resolve: func(value string) (any, error) {
			return value, nil
		},
	}
	o := defaultDecodeOptions()
	WithTagResolver(resolver)(o)
	if o.tagResolvers == nil {
		t.Fatal("expected non-nil tagResolvers")
	}
	if _, ok := o.tagResolvers["!custom"]; !ok {
		t.Error("expected !custom in tagResolvers")
	}
}

func TestWithTagResolverOptionMultiple(t *testing.T) {
	o := defaultDecodeOptions()
	WithTagResolver(&TagResolver{Tag: "!a"})(o)
	WithTagResolver(&TagResolver{Tag: "!b"})(o)
	if len(o.tagResolvers) != 2 {
		t.Errorf("expected 2 tag resolvers, got %d", len(o.tagResolvers))
	}
}

func TestCommentPositionConstants(t *testing.T) {
	if HeadCommentPos != 0 {
		t.Errorf("expected HeadCommentPos=0, got %d", HeadCommentPos)
	}
	if LineCommentPos != 1 {
		t.Errorf("expected LineCommentPos=1, got %d", LineCommentPos)
	}
	if FootCommentPos != 2 {
		t.Errorf("expected FootCommentPos=2, got %d", FootCommentPos)
	}
}

func TestCommentStruct(t *testing.T) {
	c := Comment{Position: HeadCommentPos, Text: "test"}
	if c.Position != HeadCommentPos {
		t.Error("expected HeadCommentPos")
	}
	if c.Text != "test" {
		t.Errorf("expected test, got %q", c.Text)
	}
}

func TestEncodeOptionIsFunc(t *testing.T) {
	var opt EncodeOption = Indent(4)
	if opt == nil {
		t.Error("expected non-nil option")
	}
}

func TestDecodeOptionIsFunc(t *testing.T) {
	var opt DecodeOption = Strict()
	if opt == nil {
		t.Error("expected non-nil option")
	}
}
