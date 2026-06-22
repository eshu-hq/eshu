package swift

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// swiftPayloadBuilder accumulates the parser payload during the emit walk. It
// holds the cross-row state the prior line scan kept in local maps:
// first-occurrence variable dedup, recorded variable types for receiver
// inference, and call dedup keyed by full name plus source line.
type swiftPayloadBuilder struct {
	payload       map[string]any
	facts         swiftSemanticFacts
	isDependency  bool
	indexSource   bool
	seenVariables map[string]struct{}
	variableTypes map[string]string
	seenCalls     map[string]struct{}
}

func newSwiftPayloadBuilder(payload map[string]any, facts swiftSemanticFacts, isDependency bool, indexSource bool) *swiftPayloadBuilder {
	return &swiftPayloadBuilder{
		payload:       payload,
		facts:         facts,
		isDependency:  isDependency,
		indexSource:   indexSource,
		seenVariables: make(map[string]struct{}),
		variableTypes: make(map[string]string),
		seenCalls:     make(map[string]struct{}),
	}
}

// emit walks the tree in source order, emitting every payload row from AST
// nodes. Scope tracks the enclosing type chain so members carry the right class
// context; functions do not push a type scope, so properties and calls inside
// function bodies still resolve to the enclosing type. Variables and calls are
// emitted in the same interleaved walk so recorded variable types are available
// to receiver inference exactly where they would be under a top-to-bottom scan.
func (b *swiftPayloadBuilder) emit(node *tree_sitter.Node, source []byte, scope []swiftScope) {
	if node == nil {
		return
	}
	nextScope := scope
	switch node.Kind() {
	case "import_declaration":
		b.emitImport(node, source)
	case "class_declaration":
		if name, kind := swiftTypeNameAndKind(node, source); name != "" {
			b.emitTypeDeclaration(node, source, swiftDeclarationAttributes(node, source))
			nextScope = append(scope, swiftScope{kind: kind, name: name})
		} else if extended := swiftExtensionTypeName(node, source); extended != "" {
			nextScope = append(scope, swiftScope{kind: "extension", name: extended})
		}
	case "protocol_declaration":
		b.emitProtocolDeclaration(node, source, swiftDeclarationAttributes(node, source))
		if name := swiftTrimText(node.ChildByFieldName("name"), source); name != "" {
			nextScope = append(scope, swiftScope{kind: "protocol", name: name})
		}
	case "function_declaration", "protocol_function_declaration":
		name := swiftTrimText(node.ChildByFieldName("name"), source)
		if name != "" {
			classContext, scopeKind := swiftScopeClassContext(scope)
			b.emitFunction(node, source, name, classContext, scopeKind, swiftDeclarationAttributes(node, source))
		}
	case "init_declaration":
		classContext, scopeKind := swiftScopeClassContext(scope)
		b.emitFunction(node, source, "init", classContext, scopeKind, swiftDeclarationAttributes(node, source))
	case "property_declaration", "protocol_property_declaration":
		b.emitVariable(node, source, scope)
	case "call_expression":
		b.emitCall(node, source)
	}

	for _, child := range swiftNamedChildren(node) {
		child := child
		b.emit(&child, source, nextScope)
	}
}
