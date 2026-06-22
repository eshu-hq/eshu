package swift

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// swiftExtractor walks the Swift tree-sitter AST once and emits every payload
// bucket directly from node ranges. It replaces the prior line-scan regex
// extraction so imports, nominal types, functions, variables, and calls are keyed
// by AST spans rather than text-split trimmed lines.
type swiftExtractor struct {
	payload       map[string]any
	source        []byte
	isDependency  bool
	options       shared.Options
	facts         swiftSemanticFacts
	seenVariables map[string]struct{}
	variableTypes map[string]string
	seenCalls     map[string]struct{}
}

// swiftTypeScope carries the enclosing nominal-type identity discovered while
// descending into a declaration body. Functions, variables, and calls resolve
// their class context from the innermost scope.
type swiftTypeScope struct {
	name string
	kind string
}

func newSwiftExtractor(
	payload map[string]any,
	source []byte,
	isDependency bool,
	options shared.Options,
	facts swiftSemanticFacts,
) *swiftExtractor {
	return &swiftExtractor{
		payload:       payload,
		source:        source,
		isDependency:  isDependency,
		options:       options,
		facts:         facts,
		seenVariables: make(map[string]struct{}),
		variableTypes: make(map[string]string),
		seenCalls:     make(map[string]struct{}),
	}
}

// extract walks the whole tree once, populating payload buckets in AST order.
func (e *swiftExtractor) extract(root *tree_sitter.Node) {
	e.walk(root, swiftTypeScope{})
}

// walk descends through named children, dispatching the Swift declaration and
// expression node kinds that produce payload rows. scope is the innermost
// enclosing nominal type. Declaration attributes (`@main`, `@Test`, ...) live in
// a `modifiers` child of the declaration node, so each handler reads its own
// modifiers rather than receiving them from the parent.
func (e *swiftExtractor) walk(node *tree_sitter.Node, scope swiftTypeScope) {
	if node == nil {
		return
	}

	switch node.Kind() {
	case "import_declaration":
		e.handleImport(node)
		return
	case "class_declaration":
		e.handleTypeDeclaration(node, scope)
		return
	case "protocol_declaration":
		e.handleProtocolDeclaration(node)
		return
	case "function_declaration", "protocol_function_declaration":
		e.handleFunction(node, scope, false)
		return
	case "init_declaration":
		e.handleFunction(node, scope, true)
		return
	case "property_declaration", "protocol_property_declaration":
		e.handleProperty(node, scope)
		e.walkChildren(node, scope)
		return
	case "call_expression":
		e.handleCall(node)
		e.walkChildren(node, scope)
		return
	}

	e.walkChildren(node, scope)
}

// walkChildren visits the named children of node with the given scope.
func (e *swiftExtractor) walkChildren(node *tree_sitter.Node, scope swiftTypeScope) {
	for _, child := range swiftNamedChildren(node) {
		child := child
		e.walk(&child, scope)
	}
}

// handleImport records an import row from the import_declaration's identifier
// child. The grammar joins dotted module paths (`SwiftUI.Color`) into one
// identifier node, so the node text is the full imported module name.
func (e *swiftExtractor) handleImport(node *tree_sitter.Node) {
	identifier := swiftFirstChildOfKind(node, "identifier")
	if identifier == nil {
		return
	}
	name := strings.TrimSpace(shared.NodeText(identifier, e.source))
	if name == "" {
		return
	}
	shared.AppendBucket(e.payload, "imports", map[string]any{
		"name":             name,
		"full_import_name": name,
		"alias":            nil,
		"context":          nil,
		"is_dependency":    e.isDependency,
		"line_number":      shared.NodeLine(node),
		"lang":             "swift",
	})
}

// handleTypeDeclaration records a class/struct/enum/actor row and recurses into
// the body with the new type scope. An `extension` declaration emits no type
// entity; it only pushes a scope so members attribute to the extended type.
func (e *swiftExtractor) handleTypeDeclaration(node *tree_sitter.Node, scope swiftTypeScope) {
	nameNode := swiftFirstChildOfKind(node, "type_identifier")
	if nameNode != nil {
		keyword := swiftDeclarationKeyword(node, nameNode.StartByte(), e.source)
		bucket, kind := swiftTypeBucketKind(keyword)
		if bucket != "" {
			name := strings.TrimSpace(shared.NodeText(nameNode, e.source))
			bases := swiftInheritanceBases(node, e.source)
			item := map[string]any{
				"name":        name,
				"line_number": shared.NodeLine(node),
				"end_line":    shared.NodeEndLine(node),
				"bases":       bases,
				"lang":        "swift",
			}
			if rootKinds := swiftTypeDeadCodeRootKinds(kind, bases, swiftNodeModifiers(node, e.source)); len(rootKinds) > 0 {
				item["dead_code_root_kinds"] = rootKinds
			}
			shared.AppendBucket(e.payload, bucket, item)
			e.walkChildren(node, swiftTypeScope{name: name, kind: kind})
			return
		}
	}

	// `extension Foo { ... }` parses as a class_declaration whose extended type is
	// a user_type child rather than a type_identifier name field. Attribute
	// members to the extended type without emitting a new type entity.
	if extended := swiftExtensionTypeName(node, e.source); extended != "" {
		e.walkChildren(node, swiftTypeScope{name: extended, kind: "extension"})
		return
	}
	e.walkChildren(node, scope)
}

// handleProtocolDeclaration records a protocol row and recurses into the body so
// protocol method requirements carry the protocol as their class context.
func (e *swiftExtractor) handleProtocolDeclaration(node *tree_sitter.Node) {
	nameNode := swiftFirstChildOfKind(node, "type_identifier")
	if nameNode == nil {
		e.walkChildren(node, swiftTypeScope{})
		return
	}
	name := strings.TrimSpace(shared.NodeText(nameNode, e.source))
	bases := swiftInheritanceBases(node, e.source)
	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeEndLine(node),
		"bases":       bases,
		"lang":        "swift",
	}
	if rootKinds := swiftTypeDeadCodeRootKinds("protocol", bases, swiftNodeModifiers(node, e.source)); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	shared.AppendBucket(e.payload, "protocols", item)
	e.walkChildren(node, swiftTypeScope{name: name, kind: "protocol"})
}

// handleProperty records a variable row for a property_declaration, deduplicating
// by name within a file and tracking declared types for receiver-call inference.
func (e *swiftExtractor) handleProperty(node *tree_sitter.Node, scope swiftTypeScope) {
	pattern := swiftFirstChildOfKind(node, "pattern", "simple_identifier")
	name := swiftPatternName(pattern, e.source)
	if name == "" {
		return
	}
	if _, ok := e.seenVariables[name]; ok {
		return
	}
	e.seenVariables[name] = struct{}{}

	varType := swiftTypeAnnotationText(node, e.source)
	contextName := scope.name
	if scope.kind == "protocol" {
		contextName = ""
	}
	item := map[string]any{
		"name":          name,
		"type":          varType,
		"context":       contextName,
		"class_context": contextName,
		"line_number":   shared.NodeLine(node),
		"end_line":      shared.NodeLine(node),
		"lang":          "swift",
	}
	if rootKinds := swiftVariableDeadCodeRootKinds(name, varType, contextName, e.facts); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	shared.AppendBucket(e.payload, "variables", item)
	e.variableTypes[name] = varType
}

// handleFunction records a function or initializer row, attributing class context
// and dead-code roots from the enclosing scope and declaration modifiers.
func (e *swiftExtractor) handleFunction(node *tree_sitter.Node, scope swiftTypeScope, isInit bool) {
	name := "init"
	if !isInit {
		nameNode := swiftFirstChildOfKind(node, "simple_identifier")
		if nameNode == nil {
			e.walkChildren(node, scope)
			return
		}
		name = strings.TrimSpace(shared.NodeText(nameNode, e.source))
	}

	args := swiftParameterNames(node, e.source)
	source := shared.NodeText(node, e.source)
	item := map[string]any{
		"name":        name,
		"args":        args,
		"context":     scope.name,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeEndLine(node),
		"lang":        "swift",
		"decorators":  []string{},
	}
	if scope.name != "" {
		item["class_context"] = scope.name
	}
	if e.options.IndexSource {
		item["source"] = source
	}
	modifiers := swiftNodeModifiers(node, e.source)
	isOverride := swiftNodeHasMemberModifier(node, e.source, "override")
	if rootKinds := swiftFunctionDeadCodeRootKinds(name, isOverride, scope.name, scope.kind, modifiers, e.facts); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	shared.AppendBucket(e.payload, "functions", item)

	e.walkChildren(node, scope)
}

// swiftTypeBucketKind maps a declaration keyword to its payload bucket and dead-
// code kind. An empty bucket means the keyword does not declare a nominal type
// row (extension, protocol, or an unrecognized keyword).
func swiftTypeBucketKind(keyword string) (string, string) {
	switch keyword {
	case "actor", "class":
		return "classes", "class"
	case "struct":
		return "structs", "struct"
	case "enum":
		return "enums", "enum"
	default:
		return "", ""
	}
}

// swiftNodeModifiers returns the attribute names from a declaration's leading
// modifiers child (for example @main, @Test, @available). The grammar models the
// attributes as `modifiers > attribute > user_type`, so the user_type text after
// the leading @ is the attribute name. An empty result means no attributes.
func swiftNodeModifiers(node *tree_sitter.Node, source []byte) []string {
	modifiers := swiftFirstChildOfKind(node, "modifiers")
	if modifiers == nil {
		return nil
	}
	names := make([]string, 0, 2)
	for _, child := range swiftNamedChildren(modifiers) {
		child := child
		if child.Kind() != "attribute" {
			continue
		}
		userType := swiftFirstChildOfKind(&child, "user_type")
		if userType == nil {
			continue
		}
		name := swiftShortTypeName(strings.TrimSpace(shared.NodeText(userType, source)))
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}
