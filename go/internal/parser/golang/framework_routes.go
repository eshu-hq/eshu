// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"strconv"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type goRouteReceiver struct {
	framework string
	prefix    string
}

type goRouteReceiverBinding struct {
	variable   string
	receiver   goRouteReceiver
	known      bool
	line       int
	scopeStart int
	scopeEnd   int
}

type goRouteFrameworkConstructorSpec struct {
	importPath string
	framework  string
	fields     []string
}

var goRouteFrameworkConstructors = []goRouteFrameworkConstructorSpec{
	{importPath: "github.com/gin-gonic/gin", framework: "gin", fields: []string{"default", "new"}},
	{importPath: "github.com/labstack/echo/v4", framework: "echo", fields: []string{"new"}},
	{importPath: "github.com/go-chi/chi/v5", framework: "chi", fields: []string{"newrouter"}},
	{importPath: "github.com/gofiber/fiber/v2", framework: "fiber", fields: []string{"new"}},
}

func goHTTPFrameworkSemantics(
	root *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
) (map[string]any, bool) {
	if root == nil {
		return nil, false
	}
	serveMuxVars := goHTTPServeMuxVars(root, source, importAliases)
	lookup := goBuildParentLookup(root)
	routeReceivers := goThirdPartyRouteReceiverBindings(root, source, importAliases, lookup)
	frameworks := make([]string, 0)
	entriesByFramework := make(map[string][]map[string]string)
	methodsByFramework := make(map[string][]string)
	pathsByFramework := make(map[string][]string)

	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		entry, ok := goHTTPRouteEntry(node, source, importAliases, serveMuxVars)
		if !ok {
			entry, ok = goThirdPartyRouteEntry(node, source, routeReceivers)
			if !ok {
				return
			}
		}
		framework := entry["framework"]
		delete(entry, "framework")
		frameworks = appendUniqueImportAlias(frameworks, framework)
		entriesByFramework[framework] = append(entriesByFramework[framework], entry)
		methodsByFramework[framework] = appendUniqueImportAlias(methodsByFramework[framework], entry["method"])
		pathsByFramework[framework] = appendUniqueImportAlias(pathsByFramework[framework], entry["path"])
	})
	if len(frameworks) == 0 {
		return nil, false
	}

	semantics := map[string]any{"frameworks": frameworks}
	for _, framework := range frameworks {
		semantics[framework] = map[string]any{
			"route_methods": methodsByFramework[framework],
			"route_paths":   pathsByFramework[framework],
			"route_entries": entriesByFramework[framework],
		}
	}
	return semantics, true
}

func goHTTPRouteEntry(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	serveMuxVars map[string]struct{},
) (map[string]string, bool) {
	functionNode := node.ChildByFieldName("function")
	base, field, ok := goSelectorBaseAndField(functionNode, source)
	if !ok {
		return nil, false
	}

	base = strings.ToLower(base)
	field = strings.ToLower(field)
	if field != "handlefunc" && field != "handle" {
		return nil, false
	}

	if !goHTTPRegistrationBaseKnown(base, importAliases, serveMuxVars) {
		return nil, false
	}

	argsNode := node.ChildByFieldName("arguments")
	if argsNode == nil {
		return nil, false
	}
	args := argsNode.NamedChildren(argsNode.Walk())
	if len(args) < 2 {
		return nil, false
	}

	pattern, ok := goStringLiteralValue(&args[0], source)
	if !ok {
		return nil, false
	}
	method, routePath, ok := goHTTPRoutePattern(pattern)
	if !ok {
		return nil, false
	}

	handlerName := ""
	switch field {
	case "handlefunc":
		if args[1].Kind() == "identifier" {
			handlerName = strings.TrimSpace(nodeText(&args[1], source))
		}
	case "handle":
		handlerName = goHTTPHandlerWrapperTarget(&args[1], source, importAliases)
	}
	if handlerName == "" {
		return nil, false
	}

	return map[string]string{
		"framework": "net_http",
		"method":    method,
		"path":      routePath,
		"handler":   handlerName,
	}, true
}

func goThirdPartyRouteReceiverBindings(
	root *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	lookup *goParentLookup,
) []goRouteReceiverBinding {
	bindings := make([]goRouteReceiverBinding, 0)
	if root == nil {
		return bindings
	}

	walkNamed(root, func(node *tree_sitter.Node) {
		var leftNode, rightNode *tree_sitter.Node
		switch node.Kind() {
		case "function_declaration", "method_declaration", "func_literal":
			bindings = append(bindings, goUnknownRouteReceiverParameterBindings(node, source)...)
			return
		case "short_var_declaration", "assignment_statement":
			leftNode = node.ChildByFieldName("left")
			rightNode = node.ChildByFieldName("right")
		case "var_spec":
			leftNode = node.ChildByFieldName("name")
			rightNode = node.ChildByFieldName("value")
		default:
			return
		}
		rightNode = goUnwrapSingleExpression(rightNode)
		if leftNode == nil || rightNode == nil {
			return
		}
		receiver, known := goRouteReceiverFromExpression(rightNode, source, importAliases, bindings, nodeLine(node))
		for _, nameNode := range goIdentifierNodes(leftNode, source) {
			bindings = append(bindings, goRouteReceiverBindingForName(root, node, nameNode, receiver, known, source, lookup))
		}
	})

	return bindings
}

func goRouteReceiverFromExpression(
	node *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	bindings []goRouteReceiverBinding,
	line int,
) (goRouteReceiver, bool) {
	if node == nil || node.Kind() != "call_expression" {
		return goRouteReceiver{}, false
	}
	functionNode := node.ChildByFieldName("function")
	base, field, ok := goSelectorBaseAndField(functionNode, source)
	if !ok {
		return goRouteReceiver{}, false
	}
	base = strings.ToLower(strings.TrimSpace(base))
	field = strings.ToLower(strings.TrimSpace(field))

	if framework, ok := goRouteFrameworkConstructor(base, field, importAliases); ok {
		return goRouteReceiver{framework: framework}, true
	}
	if field != "group" {
		return goRouteReceiver{}, false
	}
	parent, ok := goInferredRouteReceiver(base, line, bindings)
	if !ok {
		return goRouteReceiver{}, false
	}
	argsNode := node.ChildByFieldName("arguments")
	if argsNode == nil {
		return goRouteReceiver{}, false
	}
	args := argsNode.NamedChildren(argsNode.Walk())
	if len(args) != 1 {
		return goRouteReceiver{}, false
	}
	prefix, ok := goStringLiteralValue(&args[0], source)
	if !ok {
		return goRouteReceiver{}, false
	}
	parent.prefix = goJoinRoutePath(parent.prefix, prefix)
	return parent, true
}

func goUnknownRouteReceiverParameterBindings(node *tree_sitter.Node, source []byte) []goRouteReceiverBinding {
	body := node.ChildByFieldName("body")
	parameters := node.ChildByFieldName("parameters")
	if body == nil || parameters == nil {
		return nil
	}
	bindings := make([]goRouteReceiverBinding, 0)
	walkDirectNamed(parameters, func(child *tree_sitter.Node) {
		if child.Kind() != "parameter_declaration" {
			return
		}
		for _, nameNode := range goIdentifierNodes(child.ChildByFieldName("name"), source) {
			variable := strings.ToLower(strings.TrimSpace(nodeText(nameNode, source)))
			if variable == "" {
				continue
			}
			bindings = append(bindings, goRouteReceiverBinding{
				variable:   variable,
				line:       nodeLine(node),
				scopeStart: nodeLine(body),
				scopeEnd:   nodeEndLine(body),
			})
		}
	})
	return bindings
}

func goRouteReceiverBindingForName(
	root *tree_sitter.Node,
	node *tree_sitter.Node,
	nameNode *tree_sitter.Node,
	receiver goRouteReceiver,
	known bool,
	source []byte,
	lookup *goParentLookup,
) goRouteReceiverBinding {
	scope := goNearestLexicalScope(node, lookup)
	if scope == nil {
		scope = root
	}
	return goRouteReceiverBinding{
		variable:   strings.ToLower(strings.TrimSpace(nodeText(nameNode, source))),
		receiver:   receiver,
		known:      known,
		line:       nodeLine(node),
		scopeStart: nodeLine(scope),
		scopeEnd:   nodeEndLine(scope),
	}
}

func goInferredRouteReceiver(
	receiver string,
	callLine int,
	bindings []goRouteReceiverBinding,
) (goRouteReceiver, bool) {
	receiver = strings.ToLower(strings.TrimSpace(receiver))
	if receiver == "" || callLine <= 0 {
		return goRouteReceiver{}, false
	}
	var best goRouteReceiverBinding
	for _, binding := range bindings {
		if binding.variable != receiver ||
			binding.line > callLine ||
			callLine < binding.scopeStart ||
			callLine > binding.scopeEnd {
			continue
		}
		if best.variable == "" ||
			binding.line > best.line ||
			goRouteReceiverBindingSpan(binding) < goRouteReceiverBindingSpan(best) {
			best = binding
		}
	}
	if !best.known || best.receiver.framework == "" {
		return goRouteReceiver{}, false
	}
	return best.receiver, true
}

func goRouteReceiverBindingSpan(binding goRouteReceiverBinding) int {
	return binding.scopeEnd - binding.scopeStart
}

func goRouteFrameworkConstructor(
	base string,
	field string,
	importAliases map[string][]string,
) (string, bool) {
	for _, spec := range goRouteFrameworkConstructors {
		if !goRouteConstructorField(spec.fields, field) {
			continue
		}
		for _, alias := range goAliasesForImportPath(importAliases, spec.importPath) {
			if strings.EqualFold(alias, base) {
				return spec.framework, true
			}
		}
	}
	return "", false
}

func goRouteConstructorField(fields []string, field string) bool {
	for _, candidate := range fields {
		if candidate == field {
			return true
		}
	}
	return false
}

func goThirdPartyRouteEntry(
	node *tree_sitter.Node,
	source []byte,
	receivers []goRouteReceiverBinding,
) (map[string]string, bool) {
	functionNode := node.ChildByFieldName("function")
	base, field, ok := goSelectorBaseAndField(functionNode, source)
	if !ok {
		return nil, false
	}
	receiver, ok := goInferredRouteReceiver(base, nodeLine(node), receivers)
	if !ok {
		return nil, false
	}

	argsNode := node.ChildByFieldName("arguments")
	if argsNode == nil {
		return nil, false
	}
	args := argsNode.NamedChildren(argsNode.Walk())
	method, routePath, handlerIndex, ok := goThirdPartyRouteShape(receiver.framework, field, args, source)
	if !ok {
		return nil, false
	}
	handlerNode := &args[handlerIndex]
	if handlerNode.Kind() != "identifier" {
		return nil, false
	}
	handlerName := strings.TrimSpace(nodeText(handlerNode, source))
	if handlerName == "" {
		return nil, false
	}

	return map[string]string{
		"framework": receiver.framework,
		"method":    method,
		"path":      goJoinRoutePath(receiver.prefix, routePath),
		"handler":   handlerName,
	}, true
}

func goThirdPartyRouteShape(
	framework string,
	field string,
	args []tree_sitter.Node,
	source []byte,
) (method string, routePath string, handlerIndex int, ok bool) {
	field = strings.TrimSpace(field)
	lowerField := strings.ToLower(field)
	if framework == "chi" && lowerField == "methodfunc" {
		if len(args) != 3 {
			return "", "", 0, false
		}
		methodValue, ok := goStringLiteralValue(&args[0], source)
		if !ok || !goKnownHTTPMethod(methodValue) {
			return "", "", 0, false
		}
		routePath, ok := goStringLiteralValue(&args[1], source)
		if !ok {
			return "", "", 0, false
		}
		return strings.ToUpper(methodValue), routePath, 2, true
	}
	if len(args) != 2 || !goKnownHTTPMethod(field) {
		return "", "", 0, false
	}
	routePath, ok = goStringLiteralValue(&args[0], source)
	if !ok {
		return "", "", 0, false
	}
	return strings.ToUpper(field), routePath, 1, true
}

func goJoinRoutePath(prefix string, routePath string) string {
	prefix = strings.TrimSpace(prefix)
	routePath = strings.TrimSpace(routePath)
	if prefix == "" || prefix == "/" {
		if strings.HasPrefix(routePath, "/") {
			return routePath
		}
		return "/" + routePath
	}
	if routePath == "" || routePath == "/" {
		return prefix
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(routePath, "/")
}

func goHTTPRegistrationBaseKnown(
	base string,
	importAliases map[string][]string,
	serveMuxVars map[string]struct{},
) bool {
	for _, alias := range goAliasesForImportPath(importAliases, "net/http") {
		if strings.ToLower(alias) == base {
			return true
		}
	}
	_, ok := serveMuxVars[base]
	return ok
}

func goStringLiteralValue(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil {
		return "", false
	}
	switch node.Kind() {
	case "interpreted_string_literal", "raw_string_literal":
	default:
		return "", false
	}
	value, err := strconv.Unquote(strings.TrimSpace(nodeText(node, source)))
	if err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

func goHTTPRoutePattern(pattern string) (string, string, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", "", false
	}
	fields := strings.Fields(pattern)
	if len(fields) == 2 && goKnownHTTPMethod(fields[0]) {
		routePath := strings.TrimSpace(fields[1])
		return strings.ToUpper(fields[0]), routePath, routePath != ""
	}
	return "ANY", pattern, true
}

func goKnownHTTPMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT", "TRACE":
		return true
	default:
		return false
	}
}
