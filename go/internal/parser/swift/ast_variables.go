package swift

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// emitVariable appends one property/variable row. Properties are extracted
// wherever they appear, including inside function bodies, matching the prior
// whole-file line scan. Names are deduplicated file-wide with first-occurrence
// winning. context and class_context both resolve to the nearest enclosing
// concrete type (protocol scopes are skipped). The recorded type feeds receiver
// inference for later calls.
func (b *swiftPayloadBuilder) emitVariable(node *tree_sitter.Node, source []byte, scope []swiftScope) {
	name := swiftPropertyName(node, source)
	if name == "" {
		return
	}
	if _, seen := b.seenVariables[name]; seen {
		return
	}
	b.seenVariables[name] = struct{}{}

	contextName := swiftScopeTypeName(scope)
	varType := swiftPropertyType(node, source)
	item := map[string]any{
		"name":          name,
		"type":          varType,
		"context":       contextName,
		"class_context": contextName,
		"line_number":   shared.NodeLine(node),
		"end_line":      shared.NodeLine(node),
		"lang":          "swift",
	}
	if rootKinds := swiftVariableDeadCodeRootKinds(name, varType, contextName, b.facts); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	shared.AppendBucket(b.payload, "variables", item)
	b.variableTypes[name] = varType
}

// swiftPropertyName returns the bound identifier for a property_declaration
// (`let x` / `var x`). The grammar nests it as name: pattern > bound_identifier:
// simple_identifier.
func swiftPropertyName(node *tree_sitter.Node, source []byte) string {
	pattern := node.ChildByFieldName("name")
	if pattern == nil {
		return ""
	}
	bound := pattern.ChildByFieldName("bound_identifier")
	if bound != nil {
		return swiftTrimText(bound, source)
	}
	return swiftTrimText(pattern, source)
}

// swiftPropertyType returns the declared annotation type, or "" when the
// property has no type annotation. The type is the text after the colon in the
// type_annotation node (its `name` field child), trimmed, preserving array,
// optional, opaque, and generic spellings exactly as the prior regex did.
func swiftPropertyType(node *tree_sitter.Node, source []byte) string {
	for _, child := range swiftNamedChildren(node) {
		child := child
		if child.Kind() != "type_annotation" {
			continue
		}
		if typeName := child.ChildByFieldName("name"); typeName != nil {
			return swiftTrimText(typeName, source)
		}
		return ""
	}
	return ""
}
