package yaml

type tokenKind int

const (
	tokenError tokenKind = iota
	tokenStreamStart
	tokenStreamEnd
	tokenDocumentStart
	tokenDocumentEnd
	tokenDirective
	tokenBlockMappingStart
	tokenBlockSequenceStart
	tokenBlockEnd
	tokenFlowMappingStart
	tokenFlowMappingEnd
	tokenFlowSequenceStart
	tokenFlowSequenceEnd
	tokenKey
	tokenValue
	tokenBlockEntry
	tokenFlowEntry
	tokenAnchor
	tokenAlias
	tokenTag
	tokenScalar
	tokenComment
)

type scalarStyle int

const (
	scalarPlain scalarStyle = iota
	scalarSingleQuoted
	scalarDoubleQuoted
	scalarLiteral
	scalarFolded
)

type token struct {
	kind  tokenKind
	value string
	pos   Position
	style scalarStyle
}

func (t token) String() string {
	switch t.kind {
	case tokenStreamStart:
		return "STREAM-START"
	case tokenStreamEnd:
		return "STREAM-END"
	case tokenDocumentStart:
		return "DOCUMENT-START"
	case tokenDocumentEnd:
		return "DOCUMENT-END"
	case tokenBlockMappingStart:
		return "BLOCK-MAPPING-START"
	case tokenBlockSequenceStart:
		return "BLOCK-SEQUENCE-START"
	case tokenBlockEnd:
		return "BLOCK-END"
	case tokenFlowMappingStart:
		return "FLOW-MAPPING-START"
	case tokenFlowMappingEnd:
		return "FLOW-MAPPING-END"
	case tokenFlowSequenceStart:
		return "FLOW-SEQUENCE-START"
	case tokenFlowSequenceEnd:
		return "FLOW-SEQUENCE-END"
	case tokenKey:
		return "KEY"
	case tokenValue:
		return "VALUE"
	case tokenBlockEntry:
		return "BLOCK-ENTRY"
	case tokenFlowEntry:
		return "FLOW-ENTRY"
	case tokenAnchor:
		return "ANCHOR(" + t.value + ")"
	case tokenAlias:
		return "ALIAS(" + t.value + ")"
	case tokenTag:
		return "TAG(" + t.value + ")"
	case tokenScalar:
		return "SCALAR(" + t.value + ")"
	case tokenComment:
		return "COMMENT(" + t.value + ")"
	case tokenDirective:
		return "DIRECTIVE(" + t.value + ")"
	default:
		return "UNKNOWN"
	}
}
