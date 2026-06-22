package swift

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// swiftSyntaxIndex holds the file-wide semantic facts gathered by the first
// tree walk. The migration to an AST-only extractor uses these facts during the
// second emit walk to classify dead-code roots that depend on whole-file
// knowledge (protocol conformances, protocol method sets, and Vapor route
// handlers). It carries no per-row payload state; row emission happens in the
// emit walk.
type swiftSyntaxIndex struct {
	protocolMethods    map[string]map[string]struct{}
	typeConformances   map[string]map[string]struct{}
	vaporRouteHandlers map[string]struct{}
}

// swiftScope is one entry in the lexical scope stack tracked during the AST
// walks. kind is the declaration kind ("class", "struct", "enum", "protocol",
// or "extension") and name is the declared or extended type name. Functions do
// not push a type scope, so properties declared inside function bodies still
// resolve to their enclosing type.
type swiftScope struct {
	kind string
	name string
}

func swiftSourceAndSyntax(path string, parser *tree_sitter.Parser) ([]byte, *tree_sitter.Tree, swiftSyntaxIndex, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, nil, swiftSyntaxIndex{}, err
	}
	tree, index, err := swiftTreeSyntax(source, parser)
	if err != nil {
		return nil, nil, swiftSyntaxIndex{}, err
	}
	return source, tree, index, nil
}

func swiftTreeSyntax(source []byte, parser *tree_sitter.Parser) (*tree_sitter.Tree, swiftSyntaxIndex, error) {
	if parser == nil {
		return nil, swiftSyntaxIndex{}, fmt.Errorf("parse swift tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, swiftSyntaxIndex{}, fmt.Errorf("parse swift tree: parser returned nil tree")
	}

	index := swiftSyntaxIndex{
		protocolMethods:    make(map[string]map[string]struct{}),
		typeConformances:   make(map[string]map[string]struct{}),
		vaporRouteHandlers: make(map[string]struct{}),
	}
	root := tree.RootNode()
	index.collectFacts(root, source, nil)
	return tree, index, nil
}

// collectFacts walks the tree once to record whole-file semantic facts used by
// dead-code root classification: each type's conformance set, each protocol's
// method set, and the function names referenced as Vapor route handlers.
func (i *swiftSyntaxIndex) collectFacts(node *tree_sitter.Node, source []byte, scope []swiftScope) {
	if node == nil {
		return
	}
	nextScope := scope
	switch node.Kind() {
	case "class_declaration":
		if name, kind := swiftTypeNameAndKind(node, source); name != "" {
			i.typeConformances[name] = swiftStringSet(swiftTreeInheritance(node, source))
			nextScope = append(scope, swiftScope{kind: kind, name: name})
		} else if extended := swiftExtensionTypeName(node, source); extended != "" {
			nextScope = append(scope, swiftScope{kind: "extension", name: extended})
		}
	case "protocol_declaration":
		nameNode := node.ChildByFieldName("name")
		name := swiftTrimText(nameNode, source)
		if name != "" {
			i.typeConformances[name] = swiftStringSet(swiftTreeInheritance(node, source))
			nextScope = append(scope, swiftScope{kind: "protocol", name: name})
		}
	case "function_declaration", "protocol_function_declaration":
		if protocol := swiftEnclosingProtocol(scope); protocol != "" {
			name := swiftTrimText(node.ChildByFieldName("name"), source)
			if name != "" {
				i.recordProtocolMethod(protocol, name)
			}
		}
	case "init_declaration":
		if protocol := swiftEnclosingProtocol(scope); protocol != "" {
			i.recordProtocolMethod(protocol, "init")
		}
	case "call_expression":
		i.recordVaporRouteHandler(node, source)
	}

	for _, child := range swiftNamedChildren(node) {
		child := child
		i.collectFacts(&child, source, nextScope)
	}
}

func (i *swiftSyntaxIndex) recordProtocolMethod(protocol string, method string) {
	if i.protocolMethods[protocol] == nil {
		i.protocolMethods[protocol] = make(map[string]struct{})
	}
	i.protocolMethods[protocol][method] = struct{}{}
}

// recordVaporRouteHandler captures the function name passed as a `use:` labeled
// argument (e.g. `app.get("health", use: health)`). The grammar models the
// argument as a value_argument whose label is `use` and whose value is a
// simple_identifier, so the handler name is read straight from the AST.
func (i *swiftSyntaxIndex) recordVaporRouteHandler(node *tree_sitter.Node, source []byte) {
	for _, argument := range swiftCallValueArguments(node) {
		argument := argument
		label := argument.ChildByFieldName("name")
		if swiftTrimText(label, source) != "use" {
			continue
		}
		value := argument.ChildByFieldName("value")
		if value == nil || value.Kind() != "simple_identifier" {
			continue
		}
		if handler := swiftTrimText(value, source); handler != "" {
			i.vaporRouteHandlers[handler] = struct{}{}
		}
	}
}

// facts exposes the gathered index as the semantic-fact view consumed by the
// dead-code root helpers. The two structs share the same field layout, so the
// conversion is a zero-copy reinterpretation.
func (i swiftSyntaxIndex) facts() swiftSemanticFacts {
	return swiftSemanticFacts(i)
}

// swiftEnclosingProtocol returns the nearest enclosing protocol name, or "" when
// the innermost type scope is not a protocol.
func swiftEnclosingProtocol(scope []swiftScope) string {
	for index := len(scope) - 1; index >= 0; index-- {
		switch scope[index].kind {
		case "protocol":
			return scope[index].name
		case "class", "struct", "enum", "extension":
			return ""
		}
	}
	return ""
}

// swiftScopeTypeName returns the nearest enclosing concrete type name, skipping
// protocol scopes the same way the prior `typeNameAtLineOr` lookup did, so a
// property declared inside a protocol carries no class context.
func swiftScopeTypeName(scope []swiftScope) string {
	for index := len(scope) - 1; index >= 0; index-- {
		switch scope[index].kind {
		case "class", "struct", "enum", "extension":
			return scope[index].name
		}
	}
	return ""
}

// swiftScopeClassContext returns the nearest enclosing type name for any of the
// type-bearing scope kinds, including protocols, matching the function
// class-context lookup.
func swiftScopeClassContext(scope []swiftScope) (string, string) {
	for index := len(scope) - 1; index >= 0; index-- {
		switch scope[index].kind {
		case "class", "struct", "enum", "protocol", "extension":
			return scope[index].name, scope[index].kind
		}
	}
	return "", ""
}
