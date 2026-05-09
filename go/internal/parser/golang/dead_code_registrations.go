package golang

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goRegisteredDeadCodeRootKinds(
	root *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
) map[string][]string {
	registered := make(map[string][]string)
	if root == nil {
		return registered
	}

	serveMuxVars := goKnownVariableNames(root, source, func(expr string) bool {
		httpAliases := goAliasesForImportPath(importAliases, "net/http")
		for _, alias := range httpAliases {
			lowerAlias := strings.ToLower(alias)
			if expr == lowerAlias+".newservemux()" ||
				expr == "&"+lowerAlias+".servemux{}" ||
				expr == lowerAlias+".servemux{}" {
				return true
			}
		}
		return false
	})
	cobraVars := goKnownVariableNames(root, source, func(expr string) bool {
		cobraAliases := goAliasesForImportPath(importAliases, "github.com/spf13/cobra")
		for _, alias := range cobraAliases {
			lowerAlias := strings.ToLower(alias)
			if strings.HasPrefix(expr, "&"+lowerAlias+".command{") ||
				strings.HasPrefix(expr, lowerAlias+".command{") {
				return true
			}
		}
		return false
	})

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "call_expression":
			goCollectHTTPRegistrationRoots(node, source, importAliases, serveMuxVars, registered)
		case "composite_literal":
			goCollectCobraLiteralRoots(node, source, importAliases, registered)
		case "assignment_statement":
			goCollectCobraAssignmentRoots(node, source, cobraVars, registered)
		}
	})

	return registered
}

func goKnownVariableNames(
	root *tree_sitter.Node,
	source []byte,
	matches func(string) bool,
) map[string]struct{} {
	known := make(map[string]struct{})
	if root == nil || matches == nil {
		return known
	}

	walkNamed(root, func(node *tree_sitter.Node) {
		var leftNode, rightNode *tree_sitter.Node
		switch node.Kind() {
		case "short_var_declaration", "assignment_statement":
			leftNode = node.ChildByFieldName("left")
			rightNode = node.ChildByFieldName("right")
		case "var_spec":
			leftNode = node.ChildByFieldName("name")
			rightNode = node.ChildByFieldName("value")
		default:
			return
		}
		if leftNode == nil || rightNode == nil {
			return
		}
		if !matches(goCompactSource(rightNode, source)) {
			return
		}
		for _, name := range goIdentifierNames(leftNode, source) {
			known[name] = struct{}{}
		}
	})

	return known
}

func goCollectHTTPRegistrationRoots(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	serveMuxVars map[string]struct{},
	registered map[string][]string,
) {
	functionNode := node.ChildByFieldName("function")
	base, field, ok := goSelectorBaseAndField(functionNode, source)
	if !ok {
		return
	}

	base = strings.ToLower(base)
	field = strings.ToLower(field)
	if field != "handlefunc" && field != "handle" {
		return
	}

	httpAliases := goAliasesForImportPath(importAliases, "net/http")
	knownHTTPBase := false
	for _, alias := range httpAliases {
		if strings.ToLower(alias) == base {
			knownHTTPBase = true
			break
		}
	}
	if !knownHTTPBase {
		if _, ok := serveMuxVars[base]; !ok {
			return
		}
	}

	argsNode := node.ChildByFieldName("arguments")
	if argsNode == nil {
		return
	}
	args := argsNode.NamedChildren(argsNode.Walk())
	if len(args) < 2 {
		return
	}

	handlerName := ""
	switch field {
	case "handlefunc":
		handlerName = strings.TrimSpace(nodeText(&args[1], source))
	case "handle":
		handlerName = goHTTPHandlerWrapperTarget(&args[1], source, importAliases)
	}
	if handlerName == "" {
		return
	}

	key := strings.ToLower(handlerName)
	registered[key] = appendUniqueImportAlias(registered[key], "go.net_http_handler_registration")
}

func goHTTPHandlerWrapperTarget(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
) string {
	if node == nil || node.Kind() != "call_expression" {
		return ""
	}
	functionNode := node.ChildByFieldName("function")
	base, field, ok := goSelectorBaseAndField(functionNode, source)
	if !ok || strings.ToLower(field) != "handlerfunc" {
		return ""
	}

	httpAliases := goAliasesForImportPath(importAliases, "net/http")
	matchedBase := false
	for _, alias := range httpAliases {
		if strings.EqualFold(alias, base) {
			matchedBase = true
			break
		}
	}
	if !matchedBase {
		return ""
	}

	argsNode := node.ChildByFieldName("arguments")
	if argsNode == nil {
		return ""
	}
	args := argsNode.NamedChildren(argsNode.Walk())
	if len(args) == 0 || args[0].Kind() != "identifier" {
		return ""
	}
	return strings.TrimSpace(nodeText(&args[0], source))
}

func goCollectCobraLiteralRoots(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	registered map[string][]string,
) {
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil || !goNodeMatchesAnyQualifiedType(typeNode, source, goAliasesForImportPath(importAliases, "github.com/spf13/cobra"), "command") {
		return
	}

	compact := goCompactSource(node, source)
	for _, prefix := range []string{"run:", "rune:"} {
		start := strings.Index(compact, prefix)
		if start < 0 {
			continue
		}
		if value := goLeadingIdentifier(compact[start+len(prefix):]); value != "" {
			key := strings.ToLower(value)
			registered[key] = appendUniqueImportAlias(registered[key], "go.cobra_run_registration")
		}
	}
}

func goCollectCobraAssignmentRoots(
	node *tree_sitter.Node,
	source []byte,
	cobraVars map[string]struct{},
	registered map[string][]string,
) {
	leftNode := goUnwrapSingleExpression(node.ChildByFieldName("left"))
	rightNode := goUnwrapSingleExpression(node.ChildByFieldName("right"))
	if leftNode == nil || rightNode == nil || rightNode.Kind() != "identifier" {
		return
	}

	base, field, ok := goSelectorBaseAndField(leftNode, source)
	if !ok {
		return
	}
	if _, ok := cobraVars[strings.ToLower(base)]; !ok {
		return
	}
	switch strings.ToLower(field) {
	case "run", "rune":
		name := strings.TrimSpace(nodeText(rightNode, source))
		if name != "" {
			key := strings.ToLower(name)
			registered[key] = appendUniqueImportAlias(registered[key], "go.cobra_run_registration")
		}
	}
}

func goIdentifierNames(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "field_identifier", "identifier", "type_identifier":
		name := strings.TrimSpace(nodeText(node, source))
		if name == "" {
			return nil
		}
		return []string{strings.ToLower(name)}
	default:
		cursor := node.Walk()
		defer cursor.Close()
		values := make([]string, 0)
		for _, child := range node.NamedChildren(cursor) {
			values = append(values, goIdentifierNames(&child, source)...)
		}
		return values
	}
}

func goSelectorBaseAndField(node *tree_sitter.Node, source []byte) (string, string, bool) {
	if node == nil || node.Kind() != "selector_expression" {
		return "", "", false
	}
	fieldNode := node.ChildByFieldName("field")
	if fieldNode == nil {
		return "", "", false
	}
	baseNode := node.ChildByFieldName("operand")
	if baseNode == nil {
		cursor := node.Walk()
		defer cursor.Close()
		children := node.NamedChildren(cursor)
		if len(children) == 0 {
			return "", "", false
		}
		baseNode = &children[0]
	}
	return strings.TrimSpace(nodeText(baseNode, source)), strings.TrimSpace(nodeText(fieldNode, source)), true
}

func goNodeMatchesAnyQualifiedType(node *tree_sitter.Node, source []byte, aliases []string, typeName string) bool {
	if node == nil {
		return false
	}
	text := goCompactSource(node, source)
	for _, alias := range aliases {
		lowerAlias := strings.ToLower(alias)
		if text == lowerAlias+"."+typeName ||
			text == "*"+lowerAlias+"."+typeName {
			return true
		}
	}
	return false
}

func goCompactSource(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(nodeText(node, source)), ""))
}

func goLeadingIdentifier(value string) string {
	if value == "" {
		return ""
	}
	end := 0
	for end < len(value) {
		ch := value[end]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' {
			end++
			continue
		}
		break
	}
	return value[:end]
}

func goUnwrapSingleExpression(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() != "expression_list" {
		return node
	}
	cursor := node.Walk()
	defer cursor.Close()
	children := node.NamedChildren(cursor)
	if len(children) != 1 {
		return node
	}
	return &children[0]
}
