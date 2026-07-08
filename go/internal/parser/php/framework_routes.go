// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type phpRoute struct {
	method  string
	path    string
	handler string
}

// buildPHPFrameworkSemantics resolves framework route semantics from the
// attribute candidates and Slim route candidates phase 1 already recorded
// while visiting every "attribute" node and "member_call_expression" node
// (see collectPHPDeclarations). It performs no AST traversal of its own;
// imports must be fully collected by the time this runs, which phase 1
// guarantees since it finishes the whole file before Parse calls this.
func buildPHPFrameworkSemantics(routeAttributes []*tree_sitter.Node, slimRouteCandidates []*tree_sitter.Node, source []byte, payload map[string]any) map[string]any {
	semantics := map[string]any{"frameworks": []string{}}
	appendPHPRouteFramework(semantics, "symfony", phpSymfonyRoutes(routeAttributes, source, phpImportedSymfonyRouteNames(payload)))
	appendPHPRouteFramework(semantics, "slim", phpSlimRoutes(slimRouteCandidates, source, payload))
	return semantics
}

func appendPHPRouteFramework(semantics map[string]any, name string, routes []phpRoute) {
	if len(routes) == 0 {
		return
	}

	methods := make([]string, 0, len(routes))
	paths := make([]string, 0, len(routes))
	entries := make([]map[string]string, 0, len(routes))
	for _, route := range routes {
		methods = appendPHPUnique(methods, route.method)
		paths = appendPHPUnique(paths, route.path)
		entries = append(entries, map[string]string{
			"method":  route.method,
			"path":    route.path,
			"handler": route.handler,
		})
	}

	semantics["frameworks"] = append(semantics["frameworks"].([]string), name)
	semantics[name] = map[string]any{
		"route_methods": methods,
		"route_paths":   paths,
		"route_entries": entries,
	}
}

// phpSymfonyRoutes resolves Symfony route entries from attribute candidates
// gathered during the phase 1 declarations walk (see collectPHPDeclarations),
// preserving the source-order sequence phase 1 recorded them in so route
// entries emit in the same order the prior dedicated route walk produced.
func phpSymfonyRoutes(candidates []*tree_sitter.Node, source []byte, importedRouteNames map[string]struct{}) []phpRoute {
	routes := make([]phpRoute, 0)
	for _, node := range candidates {
		if node.Kind() != "attribute" {
			continue
		}
		nameNode := phpAttributeNameNode(node)
		if nameNode == nil || !phpIsExactSymfonyRouteAttribute(strings.TrimSpace(shared.NodeText(nameNode, source)), importedRouteNames) {
			continue
		}
		method := phpAttributeOwningMethod(node)
		if method == nil {
			continue
		}
		handler := phpRouteHandlerName(method, source)
		if handler == "" {
			continue
		}
		routePath, methods, ok := phpSymfonyRouteAttribute(shared.NodeText(node, source))
		if !ok {
			continue
		}
		for _, httpMethod := range methods {
			routes = append(routes, phpRoute{
				method:  httpMethod,
				path:    routePath,
				handler: handler,
			})
		}
	}
	return routes
}

func phpImportedSymfonyRouteNames(payload map[string]any) map[string]struct{} {
	names := make(map[string]struct{})
	imports, _ := payload["imports"].([]map[string]any)
	for _, item := range imports {
		importName := strings.Trim(strings.TrimSpace(phpStringValue(item["name"])), `\`)
		if !phpIsFullyQualifiedSymfonyRouteAttribute(importName) {
			continue
		}
		alias := strings.TrimSpace(phpStringValue(item["alias"]))
		if alias != "" {
			names[alias] = struct{}{}
			continue
		}
		names[shared.LastPathSegment(importName, `\`)] = struct{}{}
	}
	return names
}

func phpIsExactSymfonyRouteAttribute(name string, importedRouteNames map[string]struct{}) bool {
	normalized := strings.Trim(strings.TrimSpace(name), `\`)
	if phpIsFullyQualifiedSymfonyRouteAttribute(normalized) {
		return true
	}
	if strings.Contains(normalized, `\`) {
		return false
	}
	_, ok := importedRouteNames[normalized]
	return ok
}

func phpIsFullyQualifiedSymfonyRouteAttribute(name string) bool {
	switch strings.Trim(strings.TrimSpace(name), `\`) {
	case "Symfony\\Component\\Routing\\Annotation\\Route",
		"Symfony\\Component\\Routing\\Attribute\\Route":
		return true
	default:
		return false
	}
}

func phpStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func phpRouteHandlerName(method *tree_sitter.Node, source []byte) string {
	name := phpDeclarationName(method, source)
	if name == "" {
		return ""
	}
	className := ""
	for current := method.Parent(); current != nil; current = current.Parent() {
		if current.Kind() == "class_declaration" {
			className = phpDeclarationName(current, source)
			break
		}
	}
	if className == "" {
		return name
	}
	return className + "." + name
}

func phpSymfonyRouteAttribute(text string) (string, []string, bool) {
	open := strings.Index(text, "(")
	closeIndex := strings.LastIndex(text, ")")
	if open < 0 || closeIndex <= open {
		return "", nil, false
	}
	args := phpSplitTopLevel(text[open+1 : closeIndex])
	path := ""
	methods := []string{"ANY"}
	for _, arg := range args {
		name, value, named := strings.Cut(arg, ":")
		if named {
			name = strings.TrimSpace(name)
			value = strings.TrimSpace(value)
			switch name {
			case "path":
				literal, ok := phpExactStringLiteral(value)
				if !ok {
					return "", nil, false
				}
				path = literal
			case "methods":
				parsed, ok := phpExactHTTPMethods(value)
				if !ok {
					return "", nil, false
				}
				methods = parsed
			}
			continue
		}
		if path == "" {
			literal, ok := phpExactStringLiteral(arg)
			if !ok {
				return "", nil, false
			}
			path = literal
		}
	}
	if strings.TrimSpace(path) == "" || len(methods) == 0 {
		return "", nil, false
	}
	return path, methods, true
}

func phpExactHTTPMethods(value string) ([]string, bool) {
	trimmed := strings.TrimSpace(value)
	if method, ok := phpExactHTTPMethod(trimmed); ok {
		return []string{method}, true
	}

	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		return phpExactHTTPMethodList(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
	}
	if strings.HasPrefix(trimmed, "array(") && strings.HasSuffix(trimmed, ")") {
		return phpExactHTTPMethodList(strings.TrimSpace(trimmed[len("array(") : len(trimmed)-1]))
	}
	return nil, false
}

func phpExactHTTPMethodList(value string) ([]string, bool) {
	parts := phpSplitTopLevel(value)
	if len(parts) == 0 {
		return nil, false
	}
	methods := make([]string, 0, len(parts))
	for _, part := range parts {
		method, ok := phpExactHTTPMethod(part)
		if !ok {
			return nil, false
		}
		methods = append(methods, method)
	}
	return methods, true
}

func phpExactHTTPMethod(value string) (string, bool) {
	method, ok := phpExactStringLiteral(value)
	if !ok {
		return "", false
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case "CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT", "TRACE":
		return method, true
	default:
		return "", false
	}
}

func phpExactStringLiteral(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 {
		return "", false
	}
	quote := trimmed[0]
	if (quote != '\'' && quote != '"') || trimmed[len(trimmed)-1] != quote {
		return "", false
	}
	body := trimmed[1 : len(trimmed)-1]
	if quote == '"' && strings.Contains(body, "$") {
		return "", false
	}
	return body, true
}

func phpSplitTopLevel(value string) []string {
	parts := make([]string, 0)
	start := 0
	depth := 0
	quote := byte(0)
	escaped := false
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '[', '(':
			depth++
		case ']', ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				if part := strings.TrimSpace(value[start:i]); part != "" {
					parts = append(parts, part)
				}
				start = i + 1
			}
		}
	}
	if part := strings.TrimSpace(value[start:]); part != "" {
		parts = append(parts, part)
	}
	return parts
}

func appendPHPUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

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
func phpSlimRoutes(candidates []*tree_sitter.Node, source []byte, payload map[string]any) []phpRoute {
	if !phpHasSlimImport(payload) {
		return nil
	}

	routes := make([]phpRoute, 0)
	for _, node := range candidates {
		if node.Kind() != "member_call_expression" {
			continue
		}
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		verb := strings.ToLower(strings.TrimSpace(shared.NodeText(nameNode, source)))
		args := phpCallArguments(node, source)

		if verb == "map" {
			// map(['GET', 'POST'], '/path', handler)
			if len(args) < 2 {
				continue
			}
			methods := phpParseSlimMethodsArg(args[0])
			if len(methods) == 0 {
				continue
			}
			path, ok := phpExactStringLiteral(args[1])
			if !ok {
				continue
			}
			var handler string
			if len(args) >= 3 {
				handler = phpSlimHandlerName(args[2])
			}
			for _, method := range methods {
				routes = append(routes, phpRoute{
					method:  method,
					path:    path,
					handler: handler,
				})
			}
		} else {
			// verb(path, handler)
			if len(args) < 1 {
				continue
			}
			path, ok := phpExactStringLiteral(args[0])
			if !ok {
				continue
			}
			var handler string
			if len(args) >= 2 {
				handler = phpSlimHandlerName(args[1])
			}
			routes = append(routes, phpRoute{
				method:  strings.ToUpper(verb),
				path:    path,
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
