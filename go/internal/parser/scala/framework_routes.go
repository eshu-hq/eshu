// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scala

import (
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type scalaRoute struct {
	method  string
	path    string
	handler string
}

func parseScalaPlayRoutesPayload(path string, source []byte, isDependency bool) map[string]any {
	payload := shared.BasePayload(path, "scala", isDependency)
	payload["traits"] = []map[string]any{}
	payload["framework_semantics"] = scalaFrameworkSemantics(map[string][]scalaRoute{
		"play": scalaPlayRouteEntries(source),
	})
	return payload
}

// buildScalaFrameworkSemantics gates the already-collected HTTP4s route
// evidence on the imports bucket. http4sRoutes is collected during Parse's
// combined shared.WalkNamed pass regardless of the import gate (the gate
// only needs the fully-populated imports bucket, not a second traversal), so
// this function only decides whether to surface the routes it was handed.
func buildScalaFrameworkSemantics(imports []map[string]any, http4sRoutes []scalaRoute) map[string]any {
	routes := make(map[string][]scalaRoute)
	if scalaImportsHTTP4s(imports) {
		routes["http4s"] = http4sRoutes
	}
	return scalaFrameworkSemantics(routes)
}

func scalaFrameworkSemantics(routes map[string][]scalaRoute) map[string]any {
	semantics := map[string]any{"frameworks": []string{}}
	for _, framework := range []string{"play", "http4s"} {
		appendScalaRouteFramework(semantics, framework, routes[framework])
	}
	return semantics
}

func scalaPlayRouteEntries(source []byte) []scalaRoute {
	var routes []scalaRoute
	for _, line := range strings.Split(string(source), "\n") {
		if index := strings.Index(line, "#"); index >= 0 {
			line = line[:index]
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		method, ok := scalaHTTPMethod(fields[0])
		if !ok || !scalaExactPlayPath(fields[1]) {
			continue
		}
		handler, ok := scalaPlayHandler(fields[2])
		if !ok {
			continue
		}
		routes = append(routes, scalaRoute{method: method, path: fields[1], handler: handler})
	}
	return routes
}

func scalaPlayHandler(raw string) (string, bool) {
	target := strings.TrimSpace(raw)
	if index := strings.Index(target, "("); index >= 0 {
		target = target[:index]
	}
	const prefix = "controllers."
	if !strings.HasPrefix(target, prefix) {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(target, prefix), ".")
	if len(parts) != 2 {
		return "", false
	}
	className, methodName := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if !strings.HasSuffix(className, "Controller") || !isScalaIdentifier(className) || !isScalaIdentifier(methodName) {
		return "", false
	}
	return className + "." + methodName, true
}

// scalaHTTP4sRoutesFromCall returns the HTTP4s route evidence for one
// call_expression node already confirmed by scalaIsHTTP4sRoutesOfCall to be
// an "HttpRoutes.of" call. Called from Parse's combined shared.WalkNamed
// pass instead of running its own full-tree walk.
func scalaHTTP4sRoutesFromCall(node *tree_sitter.Node, source []byte) []scalaRoute {
	var routes []scalaRoute
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "case_block" {
			continue
		}
		routes = append(routes, scalaHTTP4sCaseRoutes(&child, source)...)
	}
	return routes
}

func scalaHTTP4sCaseRoutes(caseBlock *tree_sitter.Node, source []byte) []scalaRoute {
	var routes []scalaRoute
	cursor := caseBlock.Walk()
	defer cursor.Close()
	for _, child := range caseBlock.NamedChildren(cursor) {
		child := child
		if child.Kind() != "case_clause" {
			continue
		}
		if route, ok := scalaHTTP4sCaseRoute(&child, source); ok {
			routes = append(routes, route)
		}
	}
	return routes
}

func scalaHTTP4sCaseRoute(caseClause *tree_sitter.Node, source []byte) (scalaRoute, bool) {
	children := scalaNamedChildren(caseClause)
	if len(children) < 2 {
		return scalaRoute{}, false
	}
	pattern := children[0]
	body := children[len(children)-1]
	method, path, ok := scalaHTTP4sPatternRoute(shared.NodeText(pattern, source))
	if !ok || body.Kind() != "identifier" {
		return scalaRoute{}, false
	}
	handler := shared.NodeText(body, source)
	if !isScalaIdentifier(handler) {
		return scalaRoute{}, false
	}
	if context := nearestNamedAncestor(caseClause, source, "class_definition", "object_definition", "trait_definition"); context != "" {
		handler = context + "." + handler
	}
	return scalaRoute{method: method, path: path, handler: handler}, true
}

func scalaNamedChildren(node *tree_sitter.Node) []*tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	children := make([]*tree_sitter.Node, 0, int(node.NamedChildCount()))
	for _, child := range node.NamedChildren(cursor) {
		child := child
		children = append(children, shared.CloneNode(&child))
	}
	return children
}

func scalaIsHTTP4sRoutesOfCall(node *tree_sitter.Node, source []byte) bool {
	name := strings.TrimSpace(shared.NodeText(scalaCallNameNode(node), source))
	return strings.HasPrefix(name, "HttpRoutes.of") || strings.HasPrefix(name, "org.http4s.HttpRoutes.of")
}

func scalaHTTP4sPatternRoute(pattern string) (string, string, bool) {
	pattern = strings.TrimSpace(pattern)
	method, rest, ok := scalaHTTP4sPatternMethod(pattern)
	if !ok {
		return "", "", false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return method, "/", true
	}
	path := ""
	for rest != "" {
		if !strings.HasPrefix(rest, "/") {
			return "", "", false
		}
		segment, remaining, ok := scalaQuotedSegment(strings.TrimSpace(rest[1:]))
		if !ok {
			return "", "", false
		}
		path += "/" + segment
		rest = strings.TrimSpace(remaining)
	}
	if path == "" {
		path = "/"
	}
	return method, path, true
}

func scalaHTTP4sPatternMethod(pattern string) (string, string, bool) {
	for _, candidate := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		prefix := candidate + " -> Root"
		if pattern == prefix {
			return candidate, "", true
		}
		if strings.HasPrefix(pattern, prefix+" ") {
			return candidate, strings.TrimSpace(strings.TrimPrefix(pattern, prefix)), true
		}
	}
	return "", "", false
}

func scalaQuotedSegment(text string) (string, string, bool) {
	if text == "" || text[0] != '"' {
		return "", "", false
	}
	escaped := false
	for index := 1; index < len(text); index++ {
		ch := text[index]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch != '"' {
			continue
		}
		value, err := strconv.Unquote(text[:index+1])
		if err != nil || value == "" || strings.Contains(value, "/") {
			return "", "", false
		}
		return value, text[index+1:], true
	}
	return "", "", false
}

func appendScalaRouteFramework(semantics map[string]any, name string, routes []scalaRoute) {
	if name == "" || len(routes) == 0 {
		return
	}
	methods := make([]string, 0, len(routes))
	paths := make([]string, 0, len(routes))
	entries := make([]map[string]string, 0, len(routes))
	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		key := route.method + "\x00" + route.path + "\x00" + route.handler
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		methods = appendUniqueScalaRouteValue(methods, route.method)
		paths = appendUniqueScalaRouteValue(paths, route.path)
		entries = append(entries, map[string]string{"method": route.method, "path": route.path, "handler": route.handler})
	}
	if len(entries) == 0 {
		return
	}
	semantics["frameworks"] = append(semantics["frameworks"].([]string), name)
	semantics[name] = map[string]any{
		"route_methods": methods,
		"route_paths":   paths,
		"route_entries": entries,
	}
}

func appendUniqueScalaRouteValue(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func scalaImportsHTTP4s(imports []map[string]any) bool {
	for _, imported := range imports {
		switch strings.TrimSpace(anyScalaString(imported["name"])) {
		case "org.http4s.HttpRoutes", "org.http4s._", "org.http4s.dsl.io._", "org.http4s.dsl.Http4sDsl":
			return true
		}
	}
	return false
}

func isScalaPlayRoutesPath(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	return normalized == "conf/routes" ||
		strings.HasSuffix(normalized, "/conf/routes") ||
		strings.HasSuffix(normalized, ".routes")
}

func scalaHTTPMethod(value string) (string, bool) {
	method := strings.ToUpper(strings.TrimSpace(value))
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return method, true
	default:
		return "", false
	}
}

func scalaExactPlayPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/") && !strings.ContainsAny(path, "$*<>")
}

func isScalaIdentifier(value string) bool {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) == 0 || (!unicode.IsLetter(runes[0]) && runes[0] != '_') {
		return false
	}
	for _, ch := range runes[1:] {
		if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' {
			return false
		}
	}
	return true
}

func anyScalaString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
