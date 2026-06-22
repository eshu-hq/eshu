package swift

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// swiftSourceAndTree reads a Swift file and parses it with the caller-owned
// tree-sitter parser, returning the source bytes and the parsed tree. The caller
// owns the returned tree and must Close it.
func swiftSourceAndTree(path string, parser *tree_sitter.Parser) ([]byte, *tree_sitter.Tree, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, nil, err
	}
	if parser == nil {
		return nil, nil, fmt.Errorf("parse swift tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, nil, fmt.Errorf("parse swift tree: parser returned nil tree")
	}
	return source, tree, nil
}

// collectSwiftSemanticFacts walks the AST once to gather the same-file evidence
// dead-code classification needs: type conformances, protocol method
// requirements, and Vapor route handler names. The Vapor `use:` route hint has no
// dedicated symbol node, so it is read as framework evidence from call argument
// labels rather than producing a call or symbol row.
func collectSwiftSemanticFacts(root *tree_sitter.Node, source []byte) swiftSemanticFacts {
	facts := swiftSemanticFacts{
		protocolMethods:    make(map[string]map[string]struct{}),
		typeConformances:   make(map[string]map[string]struct{}),
		vaporRouteHandlers: make(map[string]struct{}),
	}
	collectSwiftConformancesAndMethods(root, source, "", "", facts)
	collectSwiftVaporRouteHandlers(root, source, facts)
	return facts
}

// collectSwiftConformancesAndMethods records each nominal type's conformance set
// and each protocol's declared method names, descending with the enclosing type
// context so protocol requirements attribute to their protocol.
func collectSwiftConformancesAndMethods(
	node *tree_sitter.Node,
	source []byte,
	currentType string,
	currentKind string,
	facts swiftSemanticFacts,
) {
	if node == nil {
		return
	}
	nextType := currentType
	nextKind := currentKind

	switch node.Kind() {
	case "class_declaration":
		if nameNode := swiftFirstChildOfKind(node, "type_identifier"); nameNode != nil {
			name := strings.TrimSpace(shared.NodeText(nameNode, source))
			keyword := swiftDeclarationKeyword(node, nameNode.StartByte(), source)
			if bucket, kind := swiftTypeBucketKind(keyword); bucket != "" && name != "" {
				nextType = name
				nextKind = kind
				facts.typeConformances[name] = swiftStringSet(swiftInheritanceBases(node, source))
			}
		} else if extended := swiftExtensionTypeName(node, source); extended != "" {
			nextType = extended
			nextKind = "extension"
		}
	case "protocol_declaration":
		if nameNode := swiftFirstChildOfKind(node, "type_identifier"); nameNode != nil {
			name := strings.TrimSpace(shared.NodeText(nameNode, source))
			if name != "" {
				nextType = name
				nextKind = "protocol"
				facts.typeConformances[name] = swiftStringSet(swiftInheritanceBases(node, source))
			}
		}
	case "function_declaration", "protocol_function_declaration":
		if currentKind == "protocol" && currentType != "" {
			if nameNode := swiftFirstChildOfKind(node, "simple_identifier"); nameNode != nil {
				name := strings.TrimSpace(shared.NodeText(nameNode, source))
				if name != "" {
					if facts.protocolMethods[currentType] == nil {
						facts.protocolMethods[currentType] = make(map[string]struct{})
					}
					facts.protocolMethods[currentType][name] = struct{}{}
				}
			}
		}
	}

	for _, child := range swiftNamedChildren(node) {
		child := child
		collectSwiftConformancesAndMethods(&child, source, nextType, nextKind, facts)
	}
}

// collectSwiftVaporRouteHandlers records the handler names passed to a Vapor
// `use:` route registration. The grammar models the labeled argument as a
// value_argument whose value_argument_label is `use`, so the trailing identifier
// is the handler name. This is content/evidence classification, not symbol
// extraction, and is the documented permanent exception for Swift.
func collectSwiftVaporRouteHandlers(root *tree_sitter.Node, source []byte, facts swiftSemanticFacts) {
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "value_argument" {
			return
		}
		label := swiftFirstChildOfKind(node, "value_argument_label")
		if label == nil {
			return
		}
		if strings.TrimSpace(shared.NodeText(label, source)) != "use" {
			return
		}
		for _, child := range swiftNamedChildren(node) {
			child := child
			if child.Kind() == "simple_identifier" {
				name := strings.TrimSpace(shared.NodeText(&child, source))
				if name != "" {
					facts.vaporRouteHandlers[name] = struct{}{}
				}
			}
		}
	})
}

// swiftExtensionTypeName returns the extended type name for an `extension`
// declaration. The Swift grammar models `extension Foo { ... }` as a
// class_declaration whose extended type is a direct user_type child rather than a
// type_identifier name field. The leading `extension` keyword must be present in
// the text before the type so true class/struct/enum declarations are not misread
// as extensions.
func swiftExtensionTypeName(node *tree_sitter.Node, source []byte) string {
	userType := swiftFirstChildOfKind(node, "user_type")
	if userType == nil {
		return ""
	}
	prefix := string(source[node.StartByte():userType.StartByte()])
	if !swiftTextHasToken(prefix, "extension") {
		return ""
	}
	return swiftShortTypeName(strings.TrimSpace(shared.NodeText(userType, source)))
}
