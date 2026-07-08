// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var pythonAioHTTPRouteMethods = map[string]string{
	"add_delete":  "DELETE",
	"add_get":     "GET",
	"add_head":    "HEAD",
	"add_options": "OPTIONS",
	"add_patch":   "PATCH",
	"add_post":    "POST",
	"add_put":     "PUT",
	"delete":      "DELETE",
	"get":         "GET",
	"head":        "HEAD",
	"options":     "OPTIONS",
	"patch":       "PATCH",
	"post":        "POST",
	"put":         "PUT",
}

type pythonTornadoImports struct {
	moduleObjects           map[string]struct{}
	applicationConstructors map[string]struct{}
	urlSpecConstructors     map[string]struct{}
}

func detectPythonAioHTTPSemantics(root *tree_sitter.Node, source []byte) map[string]any {
	webSymbols := pythonAioHTTPWebSymbols(root, source)
	routeTableSymbols, appSymbols, paramAppSymbols := pythonAioHTTPSymbols(root, source, webSymbols)
	if len(webSymbols) == 0 && len(paramAppSymbols) == 0 {
		return nil
	}
	if len(routeTableSymbols) == 0 && len(appSymbols) == 0 && len(paramAppSymbols) == 0 {
		return nil
	}
	functionNames := pythonModuleFunctionNames(root, source)
	importedNames := pythonModuleImportedNames(root, source)
	entries := pythonAioHTTPRouteTableEntries(root, source, routeTableSymbols)
	entries = append(entries, pythonAioHTTPApplicationRouteEntries(root, source, appSymbols, webSymbols, functionNames, paramAppSymbols, importedNames)...)
	return pythonRouteSemantics(entries)
}

func detectPythonTornadoSemantics(root *tree_sitter.Node, source []byte) map[string]any {
	imports := pythonTornadoImportSymbols(root, source)
	if len(imports.moduleObjects) == 0 && len(imports.applicationConstructors) == 0 {
		return nil
	}
	classMethods := pythonHTTPMethodsByClass(root, source)
	entries := pythonTornadoApplicationEntries(root, source, classMethods, imports)
	return pythonRouteSemantics(entries)
}

func pythonAioHTTPSymbols(
	root *tree_sitter.Node,
	source []byte,
	webSymbols map[string]struct{},
) (map[string]struct{}, map[string]struct{}, map[uintptr]map[string]struct{}) {
	routeTableSymbols := make(map[string]struct{})
	appSymbols := make(map[string]struct{})
	pythonWalkServerAssignments(root, source, func(symbol string, call *tree_sitter.Node, callee string) {
		if pythonInsidePythonDefinition(call) {
			return
		}
		if !pythonCallTargetsAnyObjectAttribute(call.ChildByFieldName("function"), source, webSymbols, callee) {
			return
		}
		switch callee {
		case "RouteTableDef":
			routeTableSymbols[symbol] = struct{}{}
		case "Application":
			appSymbols[symbol] = struct{}{}
		}
	})
	paramAppSymbols := pythonAioHTTPParamAppSymbols(root, source)
	return routeTableSymbols, appSymbols, paramAppSymbols
}

func pythonAioHTTPRouteTableEntries(
	root *tree_sitter.Node,
	source []byte,
	routeTableSymbols map[string]struct{},
) []map[string]string {
	if len(routeTableSymbols) == 0 {
		return nil
	}
	entries := make([]map[string]string, 0)
	pythonWalkRouteDecorators(root, source, func(decorator *tree_sitter.Node, symbol string, attribute string, call *tree_sitter.Node) {
		if _, ok := routeTableSymbols[symbol]; !ok {
			return
		}
		method, path := pythonAioHTTPDecoratorMethodPath(attribute, call, source)
		handler := pythonDecoratorHandlerName(decorator, source)
		if method == "" || path == "" || handler == "" {
			return
		}
		entries = append(entries, routeEntry(method, path, handler))
	})
	return entries
}

func pythonAioHTTPApplicationRouteEntries(
	root *tree_sitter.Node,
	source []byte,
	appSymbols map[string]struct{},
	webSymbols map[string]struct{},
	functionNames map[string]struct{},
	paramAppSymbols map[uintptr]map[string]struct{},
	importedNames map[string]struct{},
) []map[string]string {
	if len(appSymbols) == 0 && len(paramAppSymbols) == 0 {
		return nil
	}
	entries := make([]map[string]string, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call" {
			return
		}
		funcDef := pythonEnclosingFunctionDef(node)
		if funcDef != nil {
			paramApps, ok := paramAppSymbols[funcDef.Id()]
			if !ok {
				return
			}
			effectiveSymbols := make(map[string]struct{}, len(appSymbols)+len(paramApps))
			for k := range appSymbols {
				effectiveSymbols[k] = struct{}{}
			}
			for k := range paramApps {
				effectiveSymbols[k] = struct{}{}
			}
			method, path, handler, ok := pythonAioHTTPParamApplicationRouteEntry(node, source, effectiveSymbols, functionNames, importedNames)
			if ok {
				entries = append(entries, routeEntry(method, path, handler))
				return
			}
			entries = append(entries, pythonAioHTTPAddRoutesEntries(node, source, effectiveSymbols, webSymbols, functionNames)...)
			return
		}
		if pythonInsidePythonDefinition(node) {
			return
		}
		method, path, handler, ok := pythonAioHTTPApplicationRouteEntry(node, source, appSymbols, functionNames)
		if ok {
			entries = append(entries, routeEntry(method, path, handler))
			return
		}
		entries = append(entries, pythonAioHTTPAddRoutesEntries(node, source, appSymbols, webSymbols, functionNames)...)
	})
	return entries
}

func pythonAioHTTPApplicationRouteEntry(
	call *tree_sitter.Node,
	source []byte,
	appSymbols map[string]struct{},
	functionNames map[string]struct{},
) (string, string, string, bool) {
	function := call.ChildByFieldName("function")
	if function == nil || function.Kind() != "attribute" {
		return "", "", "", false
	}
	attribute := strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source))
	method, ok := pythonAioHTTPRouteMethods[attribute]
	if !ok && attribute != "add_route" {
		return "", "", "", false
	}
	if !pythonAioHTTPCallTargetsAppRouter(function.ChildByFieldName("object"), source, appSymbols) {
		return "", "", "", false
	}
	if attribute == "add_route" {
		method = pythonAioHTTPMethodLiteral(pythonPositionalArgument(call, 0), source)
		path := pythonRoutePathLiteral(pythonPositionalArgument(call, 1), source)
		handler := pythonIdentifierName(pythonPositionalArgument(call, 2), source)
		return method, path, handler, method != "" && path != "" && pythonIsKnownPythonFunction(handler, functionNames)
	}
	path := pythonRoutePathLiteral(pythonPositionalArgument(call, 0), source)
	handler := pythonIdentifierName(pythonPositionalArgument(call, 1), source)
	return method, path, handler, method != "" && path != "" && pythonIsKnownPythonFunction(handler, functionNames)
}

// pythonAioHTTPParamApplicationRouteEntry resolves an aiohttp route entry from a
// call inside a function with a param-based app. It differs from
// pythonAioHTTPApplicationRouteEntry in how handlers are proven: rather than
// requiring the handler to be a same-file module-level function definition, it
// accepts module-level imported names and unshadowed module-level function
// definitions. A handler that is only a local variable (e.g.
// `handler = make_handler()`) is not emitted — the route still carries method
// and path but omits the handler field so downstream HANDLES_ROUTE projection
// does not fabricate a false edge.
func pythonAioHTTPParamApplicationRouteEntry(
	call *tree_sitter.Node,
	source []byte,
	appSymbols map[string]struct{},
	functionNames map[string]struct{},
	importedNames map[string]struct{},
) (string, string, string, bool) {
	function := call.ChildByFieldName("function")
	if function == nil || function.Kind() != "attribute" {
		return "", "", "", false
	}
	attribute := strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source))
	method, ok := pythonAioHTTPRouteMethods[attribute]
	if !ok && attribute != "add_route" {
		return "", "", "", false
	}
	if !pythonAioHTTPCallTargetsAppRouter(function.ChildByFieldName("object"), source, appSymbols) {
		return "", "", "", false
	}
	funcDef := pythonEnclosingFunctionDef(call)
	if attribute == "add_route" {
		method = pythonAioHTTPMethodLiteral(pythonPositionalArgument(call, 0), source)
		path := pythonRoutePathLiteral(pythonPositionalArgument(call, 1), source)
		handler := pythonIdentifierName(pythonPositionalArgument(call, 2), source)
		if method == "" || path == "" {
			return "", "", "", false
		}
		if handler != "" && pythonIsProvenParamAppHandler(handler, functionNames, importedNames, funcDef, call, source) {
			return method, path, handler, true
		}
		return method, path, "", true // proven handler not found; emit route without handler
	}
	path := pythonRoutePathLiteral(pythonPositionalArgument(call, 0), source)
	handler := pythonIdentifierName(pythonPositionalArgument(call, 1), source)
	if method == "" || path == "" {
		return "", "", "", false
	}
	if handler != "" && pythonIsProvenParamAppHandler(handler, functionNames, importedNames, funcDef, call, source) {
		return method, path, handler, true
	}
	return method, path, "", true // proven handler not found; emit route without handler
}

func pythonAioHTTPAddRoutesEntries(
	call *tree_sitter.Node,
	source []byte,
	appSymbols map[string]struct{},
	webSymbols map[string]struct{},
	functionNames map[string]struct{},
) []map[string]string {
	if !pythonAioHTTPIsAddRoutesCall(call, source, appSymbols) {
		return nil
	}
	list := pythonPositionalArgument(call, 0)
	if list == nil || list.Kind() != "list" {
		return nil
	}
	entries := make([]map[string]string, 0)
	cursor := list.Walk()
	defer cursor.Close()
	for _, child := range list.NamedChildren(cursor) {
		child := child
		method, path, handler, ok := pythonAioHTTPWebRouteEntry(&child, source, webSymbols, functionNames)
		if ok {
			entries = append(entries, routeEntry(method, path, handler))
		}
	}
	return entries
}

func pythonAioHTTPWebRouteEntry(
	call *tree_sitter.Node,
	source []byte,
	webSymbols map[string]struct{},
	functionNames map[string]struct{},
) (string, string, string, bool) {
	if call.Kind() != "call" {
		return "", "", "", false
	}
	function := call.ChildByFieldName("function")
	if function == nil || function.Kind() != "attribute" {
		return "", "", "", false
	}
	attribute := strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source))
	method, ok := pythonAioHTTPRouteMethods[attribute]
	if !ok {
		return "", "", "", false
	}
	if !pythonNodeTextInSet(function.ChildByFieldName("object"), source, webSymbols) {
		return "", "", "", false
	}
	path := pythonRoutePathLiteral(pythonPositionalArgument(call, 0), source)
	handler := pythonIdentifierName(pythonPositionalArgument(call, 1), source)
	return method, path, handler, method != "" && path != "" && pythonIsKnownPythonFunction(handler, functionNames)
}

func pythonAioHTTPDecoratorMethodPath(attribute string, call *tree_sitter.Node, source []byte) (string, string) {
	if attribute == "route" {
		return pythonAioHTTPMethodLiteral(pythonPositionalArgument(call, 0), source),
			pythonRoutePathLiteral(pythonPositionalArgument(call, 1), source)
	}
	method, ok := pythonAioHTTPRouteMethods[attribute]
	if !ok {
		return "", ""
	}
	return method, pythonRoutePathLiteral(pythonPositionalArgument(call, 0), source)
}

func pythonAioHTTPIsAddRoutesCall(
	call *tree_sitter.Node,
	source []byte,
	appSymbols map[string]struct{},
) bool {
	function := call.ChildByFieldName("function")
	if function == nil || function.Kind() != "attribute" ||
		strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source)) != "add_routes" {
		return false
	}
	objectText := strings.TrimSpace(nodeText(function.ChildByFieldName("object"), source))
	if _, ok := appSymbols[objectText]; ok {
		return true
	}
	return pythonAioHTTPCallTargetsAppRouter(function.ChildByFieldName("object"), source, appSymbols)
}

func pythonAioHTTPCallTargetsAppRouter(object *tree_sitter.Node, source []byte, appSymbols map[string]struct{}) bool {
	if object == nil || object.Kind() != "attribute" ||
		strings.TrimSpace(nodeText(object.ChildByFieldName("attribute"), source)) != "router" {
		return false
	}
	appName := strings.TrimSpace(nodeText(object.ChildByFieldName("object"), source))
	_, ok := appSymbols[appName]
	return ok
}

func pythonTornadoApplicationEntries(
	root *tree_sitter.Node,
	source []byte,
	classMethods map[string][]string,
	imports pythonTornadoImports,
) []map[string]string {
	entries := make([]map[string]string, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call" || !pythonIsTornadoApplicationCall(node, source, imports) {
			return
		}
		if pythonInsidePythonDefinition(node) {
			return
		}
		routeList := pythonPositionalArgument(node, 0)
		if routeList == nil || routeList.Kind() != "list" {
			return
		}
		cursor := routeList.Walk()
		defer cursor.Close()
		for _, child := range routeList.NamedChildren(cursor) {
			child := child
			path, handlerClass := pythonTornadoURLSpec(&child, source, imports)
			if path == "" || handlerClass == "" || len(classMethods[handlerClass]) == 0 {
				continue
			}
			for _, method := range classMethods[handlerClass] {
				entries = append(entries, routeEntry(method, path, handlerClass+"."+strings.ToLower(method)))
			}
		}
	})
	return entries
}

func pythonIsTornadoApplicationCall(call *tree_sitter.Node, source []byte, imports pythonTornadoImports) bool {
	function := call.ChildByFieldName("function")
	if pythonCallTargetsAnyObjectAttribute(function, source, imports.moduleObjects, "Application") {
		return true
	}
	if function == nil || function.Kind() != "identifier" {
		return false
	}
	_, ok := imports.applicationConstructors[strings.TrimSpace(nodeText(function, source))]
	return ok
}

func pythonTornadoURLSpec(node *tree_sitter.Node, source []byte, imports pythonTornadoImports) (string, string) {
	switch node.Kind() {
	case "tuple", "list":
		return pythonTornadoRoutePathLiteral(pythonSequenceElement(node, 0), source),
			pythonIdentifierName(pythonSequenceElement(node, 1), source)
	case "call":
		if !pythonIsTornadoURLSpecCall(node.ChildByFieldName("function"), source, imports) {
			return "", ""
		}
		return pythonTornadoRoutePathLiteral(pythonPositionalArgument(node, 0), source),
			pythonIdentifierName(pythonPositionalArgument(node, 1), source)
	default:
		return "", ""
	}
}

func pythonIsTornadoURLSpecCall(function *tree_sitter.Node, source []byte, imports pythonTornadoImports) bool {
	if function == nil {
		return false
	}
	switch function.Kind() {
	case "identifier":
		_, ok := imports.urlSpecConstructors[strings.TrimSpace(nodeText(function, source))]
		return ok
	case "attribute":
		attribute := strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source))
		if attribute != "URLSpec" && attribute != "url" {
			return false
		}
		return pythonNodeTextInSet(function.ChildByFieldName("object"), source, imports.moduleObjects)
	default:
		return false
	}
}

func pythonSequenceElement(node *tree_sitter.Node, index int) *tree_sitter.Node {
	if node == nil || index < 0 {
		return nil
	}
	seen := 0
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if seen == index {
			return &child
		}
		seen++
	}
	return nil
}

func pythonIdentifierName(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "identifier" {
		return ""
	}
	return strings.TrimSpace(nodeText(node, source))
}

func pythonIsKnownPythonFunction(name string, functionNames map[string]struct{}) bool {
	if name == "" {
		return false
	}
	_, ok := functionNames[name]
	return ok
}

func pythonAioHTTPMethodLiteral(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "string" {
		return ""
	}
	method := strings.ToLower(pythonStringLiteralValue(node, source))
	if _, ok := pythonHTTPRouteMethods[method]; !ok {
		return ""
	}
	return strings.ToUpper(method)
}

func pythonRoutePathLiteral(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "string" {
		return ""
	}
	return pythonEnsureRoutePath(pythonStringLiteralValue(node, source))
}

func pythonTornadoRoutePathLiteral(node *tree_sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "string" {
		return ""
	}
	return pythonStringLiteralValue(node, source)
}

func pythonInsidePythonDefinition(node *tree_sitter.Node) bool {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_definition", "class_definition", "lambda":
			return true
		}
	}
	return false
}
