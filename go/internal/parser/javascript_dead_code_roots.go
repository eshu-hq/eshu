package parser

import (
	"path/filepath"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type javaScriptDeadCodeEvidence struct {
	registeredRootKinds map[string][]string
	fileRootKinds       []string
	hapiHandlerFile     bool
}

func javaScriptDeadCodeRootEvidence(
	repoRoot string,
	path string,
	root *tree_sitter.Node,
	source []byte,
) javaScriptDeadCodeEvidence {
	return javaScriptDeadCodeEvidence{
		registeredRootKinds: javaScriptRegisteredDeadCodeRootKinds(root, source),
		fileRootKinds:       javaScriptPackageFileRootKinds(repoRoot, path),
		hapiHandlerFile:     javaScriptIsHapiHandlerFile(repoRoot, path),
	}
}

var javaScriptRouteExportNames = map[string]struct{}{
	"GET":     {},
	"POST":    {},
	"PUT":     {},
	"PATCH":   {},
	"DELETE":  {},
	"HEAD":    {},
	"OPTIONS": {},
}

var javaScriptExpressRouteMethods = map[string]struct{}{
	"get":     {},
	"post":    {},
	"put":     {},
	"patch":   {},
	"delete":  {},
	"head":    {},
	"options": {},
}

func javaScriptRegisteredDeadCodeRootKinds(
	root *tree_sitter.Node,
	source []byte,
) map[string][]string {
	registered := make(map[string][]string)
	if root == nil || !javaScriptHasExpressImport(string(source)) {
		return registered
	}

	allowedBases := make(map[string]struct{})
	if express, ok := detectExpressSemantics(string(source)); ok {
		for _, symbol := range javaScriptExpressServerSymbols(express) {
			allowedBases[strings.ToLower(strings.TrimSpace(symbol))] = struct{}{}
		}
	}
	if len(allowedBases) == 0 {
		return registered
	}

	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		functionNode := node.ChildByFieldName("function")
		base, property, ok := javaScriptMemberBaseAndProperty(functionNode, source)
		if !ok {
			return
		}
		if _, ok := allowedBases[strings.ToLower(base)]; !ok {
			return
		}
		if _, ok := javaScriptExpressRouteMethods[strings.ToLower(property)]; !ok {
			return
		}

		argsNode := node.ChildByFieldName("arguments")
		if argsNode == nil {
			return
		}
		args := argsNode.NamedChildren(argsNode.Walk())
		if len(args) < 2 {
			return
		}

		handlerStart := 1
		if javaScriptIsExpressRouteChain(functionNode, source) {
			handlerStart = 0
		}
		for i := handlerStart; i < len(args); i++ {
			for _, handlerName := range javaScriptExpressHandlerNames(&args[i], source) {
				key := strings.ToLower(handlerName)
				registered[key] = appendUniqueString(registered[key], "javascript.express_route_registration")
			}
		}
	})

	return registered
}

// javaScriptExpressServerSymbols extracts the typed server_symbols contract
// emitted by detectExpressSemantics. The detector intentionally requires the
// stable []string shape so registration-driven dead-code roots fail closed if
// the upstream helper changes shape.
func javaScriptExpressServerSymbols(express map[string]any) []string {
	if len(express) == 0 {
		return nil
	}
	serverSymbols, ok := express["server_symbols"].([]string)
	if !ok {
		return nil
	}
	return serverSymbols
}

func javaScriptDeadCodeRootKinds(
	path string,
	node *tree_sitter.Node,
	name string,
	source []byte,
	evidence javaScriptDeadCodeEvidence,
) []string {
	rootKinds := append([]string(nil), evidence.registeredRootKinds[strings.ToLower(strings.TrimSpace(name))]...)
	if javaScriptIsNextJSRouteExport(path, node, name) {
		rootKinds = appendUniqueString(rootKinds, "javascript.nextjs_route_export")
	}
	for _, rootKind := range evidence.fileRootKinds {
		switch rootKind {
		case "javascript.node_package_entrypoint":
			if javaScriptIsNodeEntrypointFunctionName(name) {
				rootKinds = appendUniqueString(rootKinds, rootKind)
			}
		case "javascript.node_package_bin":
			if javaScriptIsNodeBinFunctionName(name) {
				rootKinds = appendUniqueString(rootKinds, rootKind)
			}
		case "javascript.node_package_export":
			if javaScriptIsExported(node) || javaScriptIsCommonJSExport(node, name, source) {
				rootKinds = appendUniqueString(rootKinds, rootKind)
			}
		}
	}
	if evidence.hapiHandlerFile && (javaScriptIsExported(node) || javaScriptIsCommonJSExport(node, name, source)) {
		rootKinds = appendUniqueString(rootKinds, "javascript.hapi_handler_export")
	}
	slices.Sort(rootKinds)
	return rootKinds
}

func javaScriptIsNextJSRouteExport(path string, node *tree_sitter.Node, name string) bool {
	if !javaScriptIsNextJSRouteModule(path) {
		return false
	}
	if _, ok := javaScriptRouteExportNames[strings.ToUpper(strings.TrimSpace(name))]; !ok {
		return false
	}
	return javaScriptIsExported(node)
}

func javaScriptIsNextJSRouteModule(path string) bool {
	switch filepath.Base(path) {
	case "route.js", "route.jsx", "route.ts", "route.tsx":
		return true
	default:
		return false
	}
}

func javaScriptIsExported(node *tree_sitter.Node) bool {
	for current := node; current != nil; current = current.Parent() {
		switch current.Kind() {
		case "export_statement":
			return true
		case "program":
			return false
		}
	}
	return false
}

func javaScriptIsCommonJSExport(node *tree_sitter.Node, name string, source []byte) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for current := node; current != nil; current = current.Parent() {
		if current.Kind() != "assignment_expression" {
			continue
		}
		leftNode := current.ChildByFieldName("left")
		exportName := javaScriptCommonJSExportName(leftNode, source)
		return exportName == name
	}
	return false
}

func javaScriptCommonJSExportName(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "member_expression" {
		return ""
	}
	objectNode := node.ChildByFieldName("object")
	propertyNode := node.ChildByFieldName("property")
	if objectNode == nil || propertyNode == nil {
		return ""
	}
	objectText := strings.TrimSpace(nodeText(objectNode, source))
	switch objectText {
	case "module.exports":
		return javaScriptIdentifierName(propertyNode, source)
	case "exports":
		return javaScriptIdentifierName(propertyNode, source)
	default:
		return ""
	}
}

func javaScriptExportAssignmentNameNode(node *tree_sitter.Node, source []byte) *tree_sitter.Node {
	if node == nil || node.Kind() != "member_expression" {
		return nil
	}
	if javaScriptCommonJSExportName(node, source) == "" {
		return nil
	}
	propertyNode := node.ChildByFieldName("property")
	return cloneNode(propertyNode)
}

func javaScriptIsNodeEntrypointFunctionName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "main", "bootstrap", "start", "run", "handler":
		return true
	default:
		return false
	}
}

func javaScriptIsNodeBinFunctionName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if javaScriptIsNodeEntrypointFunctionName(normalized) {
		return true
	}
	return strings.HasPrefix(normalized, "run") || strings.HasSuffix(normalized, "cli")
}


func javaScriptMemberBaseAndProperty(node *tree_sitter.Node, source []byte) (string, string, bool) {
	if node == nil || node.Kind() != "member_expression" {
		return "", "", false
	}
	objectNode := node.ChildByFieldName("object")
	propertyNode := node.ChildByFieldName("property")
	base := javaScriptMemberExpressionBase(objectNode, source)
	property := javaScriptIdentifierName(propertyNode, source)
	if base == "" || property == "" {
		return "", "", false
	}
	return base, property, true
}

func javaScriptMemberExpressionBase(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if base := javaScriptIdentifierName(node, source); base != "" {
		return base
	}
	switch node.Kind() {
	case "call_expression":
		functionNode := node.ChildByFieldName("function")
		if functionNode == nil || functionNode.Kind() != "member_expression" {
			return ""
		}
		routeBase, routeProperty, ok := javaScriptMemberBaseAndProperty(functionNode, source)
		if !ok || strings.ToLower(routeProperty) != "route" {
			return ""
		}
		return routeBase
	default:
		return ""
	}
}

func javaScriptIsExpressRouteChain(node *tree_sitter.Node, source []byte) bool {
	if node == nil || node.Kind() != "member_expression" {
		return false
	}
	objectNode := node.ChildByFieldName("object")
	if objectNode == nil || objectNode.Kind() != "call_expression" {
		return false
	}
	functionNode := objectNode.ChildByFieldName("function")
	_, property, ok := javaScriptMemberBaseAndProperty(functionNode, source)
	return ok && strings.ToLower(property) == "route"
}

// javaScriptExpressHandlerNames returns named route callbacks from an Express
// route argument, including handler arrays. Anonymous inline callbacks are not
// roots because the parser has no stable symbol to annotate.
func javaScriptExpressHandlerNames(node *tree_sitter.Node, source []byte) []string {
	if node == nil {
		return nil
	}
	if name := javaScriptIdentifierName(node, source); name != "" {
		return []string{name}
	}
	switch node.Kind() {
	case "array", "parenthesized_expression":
	default:
		return nil
	}

	names := []string{}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		for _, name := range javaScriptExpressHandlerNames(&children[i], source) {
			names = appendUniqueString(names, name)
		}
	}
	return names
}

func javaScriptIdentifierName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier", "property_identifier":
		return strings.TrimSpace(nodeText(node, source))
	default:
		return ""
	}
}
