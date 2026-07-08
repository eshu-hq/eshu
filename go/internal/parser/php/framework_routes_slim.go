// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// phpIsSlimVerbCandidate reports whether a member_call_expression node has a
// method name matching one of the Slim route-registration verbs. This is a
// cheap phase-1 filter; the full Slim import gate and argument extraction
// happen in phpSlimRoutes after phase 1 completes.
func phpIsSlimVerbCandidate(node *tree_sitter.Node, source []byte) bool {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return false
	}
	methodName := strings.ToLower(strings.TrimSpace(shared.NodeText(nameNode, source)))
	switch methodName {
	case "get", "post", "put", "patch", "delete", "options", "any", "map":
		return true
	}
	return false
}

// phpSlimRoutes resolves Slim route entries from member_call_expression
// candidates gathered during the phase 1 declarations walk. Routes are only
// emitted when a Slim import (use Slim\...) is present in the file, which
// prevents false positives from non-Slim get/post/... calls. The first string-
// literal argument is the route path; the second argument names the handler
// (class::class, string literal, or closure — closures produce an empty
// handler). For the map() verb the first argument is an array of HTTP methods.
//
// Enclosing group() prefixes are resolved by walking the AST parent chain so
// inner routes registered under $app->group('/prefix', fn($group) =>
// $group->get('/sub', ...)) emit the concatenated path '/prefix/sub'. Nested
// groups are supported. A non-literal group prefix is treated as unresolvable
// and causes the inner route to be skipped — a wrong path is worse than a
// missing one. Empty paths (including empty-subpath routes with no enclosing
// group) are never emitted.
func phpSlimRoutes(candidates []*tree_sitter.Node, slimReceiverVars map[string]struct{}, source []byte, payload map[string]any) []phpRoute {
	if !phpHasSlimImport(payload) {
		return nil
	}

	routes := make([]phpRoute, 0)
	for _, node := range candidates {
		if node.Kind() != "member_call_expression" {
			continue
		}
		// Only emit routes whose receiver has proven Slim provenance.
		// A bare ->get('literal') on an untracked variable (e.g.
		// $container->get('settings')) must not be treated as a route.
		objectNode := node.ChildByFieldName("object")
		if objectNode == nil {
			continue
		}
		receiverName := phpSlimReceiverName(objectNode, source)
		if receiverName == "" {
			continue
		}
		if _, ok := slimReceiverVars[receiverName]; !ok {
			continue
		}
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		verb := strings.ToLower(strings.TrimSpace(shared.NodeText(nameNode, source)))
		args := phpCallArguments(node, source)

		// Resolve enclosing group() prefixes. A non-literal prefix
		// makes the accurate path unknowable; skip rather than emit
		// a wrong one.
		prefixes, prefixesOk := phpEnclosingGroupPrefixes(node, source)
		if !prefixesOk {
			continue
		}

		if verb == "map" {
			// map(['GET', 'POST'], '/path', handler)
			if len(args) < 2 {
				continue
			}
			methods := phpParseSlimMethodsArg(args[0])
			if len(methods) == 0 {
				continue
			}
			subpath, ok := phpExactStringLiteral(args[1])
			if !ok {
				continue
			}
			fullPath := phpSlimRoutePath(prefixes, subpath)
			if fullPath == "" {
				continue
			}
			var handler string
			if len(args) >= 3 {
				handler = phpSlimHandlerName(args[2])
			}
			for _, method := range methods {
				routes = append(routes, phpRoute{
					method:  method,
					path:    fullPath,
					handler: handler,
				})
			}
		} else {
			// verb(path, handler)
			if len(args) < 1 {
				continue
			}
			subpath, ok := phpExactStringLiteral(args[0])
			if !ok {
				continue
			}
			fullPath := phpSlimRoutePath(prefixes, subpath)
			if fullPath == "" {
				continue
			}
			var handler string
			if len(args) >= 2 {
				handler = phpSlimHandlerName(args[1])
			}
			routes = append(routes, phpRoute{
				method:  strings.ToUpper(verb),
				path:    fullPath,
				handler: handler,
			})
		}
	}
	return routes
}

// phpHasSlimImport reports whether the file imports a Slim namespace, which
// gates Slim route detection to avoid false positives on unrelated ->get(),
// ->post(), etc. calls.
func phpHasSlimImport(payload map[string]any) bool {
	imports, _ := payload["imports"].([]map[string]any)
	for _, item := range imports {
		importName := strings.TrimSpace(phpStringValue(item["name"]))
		if strings.HasPrefix(importName, "Slim\\") || strings.HasPrefix(importName, "Slim/") {
			return true
		}
	}
	return false
}

// phpSlimHandlerName extracts a human-readable handler name from a Slim route
// callable argument. Class::class expressions produce the class short name;
// string literals are returned as-is; closures and other callables produce the
// empty string.
func phpSlimHandlerName(argText string) string {
	argText = strings.TrimSpace(argText)
	if strings.HasSuffix(argText, "::class") {
		return shared.LastPathSegment(strings.TrimSuffix(argText, "::class"), `\`)
	}
	if literal, ok := phpExactStringLiteral(argText); ok {
		return literal
	}
	return ""
}

// phpParseSlimMethodsArg extracts a list of HTTP methods from a Slim map() first
// argument, which may be a short array ['GET', 'POST'] or a long array(...)
// expression. It delegates to the shared phpExactHTTPMethods parser.
func phpParseSlimMethodsArg(argText string) []string {
	methods, _ := phpExactHTTPMethods(argText)
	return methods
}

// phpSlimRoutePath concatenates enclosing group prefixes with the route's
// subpath. Empty paths are never emitted — an empty-subpath route with no
// enclosing group is skipped.
func phpSlimRoutePath(prefixes []string, subpath string) string {
	parts := make([]string, 0, len(prefixes)+1)
	parts = append(parts, prefixes...)
	parts = append(parts, subpath)
	path := strings.Join(parts, "")
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return path
}

// phpEnclosingGroupPrefixes walks up the AST parent chain from a route
// candidate node, collecting the literal prefix arguments of every enclosing
// member_call_expression whose method name is "group". Prefixes are returned
// outermost-first for concatenation. The second return value is false when any
// group's prefix is not a string literal, meaning the accurate path cannot be
// determined and the route must be skipped.
func phpEnclosingGroupPrefixes(node *tree_sitter.Node, source []byte) ([]string, bool) {
	var prefixes []string
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() != "member_call_expression" {
			continue
		}
		nameNode := current.ChildByFieldName("name")
		if nameNode == nil || strings.ToLower(strings.TrimSpace(shared.NodeText(nameNode, source))) != "group" {
			continue
		}
		args := phpCallArguments(current, source)
		if len(args) < 1 {
			continue
		}
		prefix, ok := phpExactStringLiteral(args[0])
		if !ok {
			return nil, false
		}
		prefixes = append(prefixes, prefix)
	}
	// Reverse to get outermost-first order (parent walk yields innermost-first).
	for i, j := 0, len(prefixes)-1; i < j; i, j = i+1, j-1 {
		prefixes[i], prefixes[j] = prefixes[j], prefixes[i]
	}
	return prefixes, true
}

// collectPHPSlimParameter checks whether a simple_parameter node declares a
// variable typed with a Slim class or interface. If it does, the variable
// name (without $) is recorded in slimReceiverVars. This covers the common
// bootstrap pattern return function (App $app) { $app->get(...); } and the
// group-closure pattern function (RouteCollectorProxyInterface $group) { ... }.
func collectPHPSlimParameter(state *phpParseState, node *tree_sitter.Node) {
	typeName, varName := phpSimpleParameterParts(node, state.source)
	if typeName == "" || varName == "" {
		return
	}
	if phpIsSlimTypeName(typeName, state.payload) {
		state.slimReceiverVars[varName] = struct{}{}
	}
}

// collectPHPSlimScopedCall records the LHS variable of a Slim factory scoped
// call such as AppFactory::create() or \Slim\Factory\AppFactory::create(...).
// Only the create() method on a Slim-typed scope is recognized as a factory.
func collectPHPSlimScopedCall(state *phpParseState, node *tree_sitter.Node) {
	scopeNode := node.ChildByFieldName("scope")
	nameNode := node.ChildByFieldName("name")
	if scopeNode == nil || nameNode == nil {
		return
	}
	methodName := strings.ToLower(strings.TrimSpace(shared.NodeText(nameNode, state.source)))
	if methodName != "create" {
		return
	}
	scopeName := strings.TrimSpace(shared.NodeText(scopeNode, state.source))
	if !phpIsSlimTypeName(scopeName, state.payload) {
		return
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() == "assignment_expression" {
			if lhs := phpAssignmentLHS(current, state.source); lhs != "" {
				state.slimReceiverVars[lhs] = struct{}{}
			}
			return
		}
	}
}

// collectPHPSlimObjectCreation records the LHS variable of a Slim constructor
// call such as new \Slim\App(...) or new App(...) where App resolves to
// Slim\App through imports.
func collectPHPSlimObjectCreation(state *phpParseState, node *tree_sitter.Node) {
	classNode := phpObjectCreationTypeNode(node)
	if classNode == nil {
		return
	}
	className := strings.TrimSpace(shared.NodeText(classNode, state.source))
	if !phpIsSlimTypeName(className, state.payload) {
		return
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() == "assignment_expression" {
			if lhs := phpAssignmentLHS(current, state.source); lhs != "" {
				state.slimReceiverVars[lhs] = struct{}{}
			}
			return
		}
	}
}

// phpIsSlimTypeName reports whether a type name resolves to a Slim namespace,
// either as a fully qualified name starting with Slim\ or through the import
// payload (explicit alias or last-segment short name). It does NOT use
// importAliases because normalizePHPTypeName strips the namespace.
func phpIsSlimTypeName(name string, payload map[string]any) bool {
	name = strings.Trim(name, `\`)
	if strings.HasPrefix(name, "Slim\\") {
		return true
	}
	imports, _ := payload["imports"].([]map[string]any)
	for _, item := range imports {
		importName := strings.Trim(strings.TrimSpace(phpStringValue(item["name"])), `\`)
		if !strings.HasPrefix(importName, "Slim\\") {
			continue
		}
		alias := strings.TrimSpace(phpStringValue(item["alias"]))
		if alias != "" && alias == name {
			return true
		}
		if shared.LastPathSegment(importName, `\`) == name {
			return true
		}
	}
	return false
}

// phpAssignmentLHS returns the bare variable name (without $) from the
// left-hand side of an assignment_expression node, or the empty string when
// the LHS is not a simple variable_name.
func phpAssignmentLHS(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "variable_name" {
			return strings.TrimPrefix(strings.TrimSpace(shared.NodeText(&child, source)), "$")
		}
	}
	return ""
}

// phpSimpleParameterParts extracts the type name and bare variable name from
// a simple_parameter node such as "App $app" or "int $count". Primitive types
// are returned but will not match a Slim namespace.
func phpSimpleParameterParts(node *tree_sitter.Node, source []byte) (string, string) {
	var typeName, varName string
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "named_type", "primitive_type":
			typeName = strings.TrimSpace(shared.NodeText(&child, source))
		case "variable_name":
			varName = strings.TrimPrefix(strings.TrimSpace(shared.NodeText(&child, source)), "$")
		}
	}
	return typeName, varName
}

// phpSlimReceiverName returns the bare variable name (without $) from the
// object field of a member_call_expression, or the empty string when the
// receiver is not a simple variable_name (e.g. a chained expression).
func phpSlimReceiverName(objectNode *tree_sitter.Node, source []byte) string {
	if objectNode.Kind() != "variable_name" {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(shared.NodeText(objectNode, source)), "$")
}
