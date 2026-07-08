// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type rustRoute struct {
	method    string
	path      string
	handler   string
	framework string
	order     int
}

// rustAxumCallCandidate is a call_expression node's text and end byte,
// captured during the single main payload walk in Parse so axum route
// detection does not require a dedicated tree walk. endByte preserves the
// same order-by-end-byte behavior rustAxumRoutes has always applied.
type rustAxumCallCandidate struct {
	text    string
	endByte int
}

// collectRustAxumCallCandidate appends node's text as an axum route
// candidate unless node sits under a cfg/cfg_attr-gated ancestor, matching
// the filter rustAxumRoutes applied when it walked the tree directly.
func collectRustAxumCallCandidate(candidates []rustAxumCallCandidate, node *tree_sitter.Node, source []byte) []rustAxumCallCandidate {
	if rustNodeHasCfgAncestor(node, source) {
		return candidates
	}
	return append(candidates, rustAxumCallCandidate{
		text:    shared.NodeText(node, source),
		endByte: int(node.EndByte()),
	})
}

type rustImportedRouteAttribute struct {
	framework string
	method    string
}

type rustRouteImports struct {
	attributes      map[string]rustImportedRouteAttribute
	axumMethods     map[string]string
	axumRouterNames map[string]struct{}
}

func buildRustFrameworkSemantics(payload map[string]any, axumCandidates []rustAxumCallCandidate) map[string]any {
	imports := rustRouteImportNames(payload)
	routesByFramework := map[string][]rustRoute{
		"actix_web": {},
		"axum":      {},
		"rocket":    {},
	}

	for _, route := range rustAttributeRoutes(payload, imports) {
		routesByFramework[route.framework] = append(routesByFramework[route.framework], route)
	}
	for _, route := range rustAxumRoutes(axumCandidates, imports) {
		routesByFramework[route.framework] = append(routesByFramework[route.framework], route)
	}

	semantics := map[string]any{"frameworks": []string{}}
	for _, framework := range []string{"actix_web", "axum", "rocket"} {
		appendRustRouteFramework(semantics, framework, routesByFramework[framework])
	}
	return semantics
}

func appendRustRouteFramework(semantics map[string]any, name string, routes []rustRoute) {
	if len(routes) == 0 {
		return
	}

	methods := make([]string, 0, len(routes))
	paths := make([]string, 0, len(routes))
	entries := make([]map[string]string, 0, len(routes))
	for _, route := range routes {
		methods = appendUniqueString(methods, route.method)
		paths = appendUniqueString(paths, route.path)
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

func rustRouteImportNames(payload map[string]any) rustRouteImports {
	imports := rustRouteImports{
		attributes:      map[string]rustImportedRouteAttribute{},
		axumMethods:     map[string]string{},
		axumRouterNames: map[string]struct{}{},
	}
	rows, _ := payload["imports"].([]map[string]any)
	for _, row := range rows {
		name := strings.TrimSpace(rustStringValue(row["name"]))
		alias := strings.TrimSpace(rustStringValue(row["alias"]))
		importType := strings.TrimSpace(rustStringValue(row["import_type"]))
		if importType == "glob" {
			continue
		}
		local := alias
		if local == "" {
			local = shared.LastPathSegment(name, "::")
		}
		if local == "" {
			continue
		}
		if framework, method, ok := rustQualifiedAttributeFrameworkMethod(name); ok {
			imports.attributes[local] = rustImportedRouteAttribute{framework: framework, method: method}
		}
		if method, ok := rustQualifiedAxumMethod(name); ok {
			imports.axumMethods[local] = method
		}
		if strings.TrimSpace(name) == "axum::Router" {
			imports.axumRouterNames[local] = struct{}{}
		}
	}
	return imports
}

func rustAttributeRoutes(payload map[string]any, imports rustRouteImports) []rustRoute {
	functions, _ := payload["functions"].([]map[string]any)
	functions = append([]map[string]any(nil), functions...)
	sort.SliceStable(functions, func(i, j int) bool {
		return rustIntValue(functions[i]["line_number"]) < rustIntValue(functions[j]["line_number"])
	})

	routes := make([]rustRoute, 0)
	for _, fn := range functions {
		if _, blocked := fn["exactness_blockers"]; blocked {
			continue
		}
		handler := strings.TrimSpace(rustStringValue(fn["name"]))
		if handler == "" {
			continue
		}
		for _, attribute := range rustStringSliceValue(fn["decorators"]) {
			framework, method, routePath, ok := rustRouteAttribute(attribute, imports)
			if !ok {
				continue
			}
			routes = append(routes, rustRoute{
				method:    method,
				path:      routePath,
				handler:   handler,
				framework: framework,
				order:     rustIntValue(fn["line_number"]),
			})
		}
	}
	return routes
}

func rustRouteAttribute(attribute string, imports rustRouteImports) (string, string, string, bool) {
	attrPath := rustAttributePath(attribute)
	framework, method, ok := rustAttributeFrameworkMethod(attrPath, imports)
	if !ok {
		return "", "", "", false
	}
	routePath, ok := rustRouteAttributePath(attribute)
	if !ok {
		return "", "", "", false
	}
	return framework, method, routePath, true
}

func rustAttributeFrameworkMethod(attrPath string, imports rustRouteImports) (string, string, bool) {
	if framework, method, ok := rustQualifiedAttributeFrameworkMethod(attrPath); ok {
		return framework, method, true
	}
	if strings.Contains(attrPath, "::") {
		return "", "", false
	}
	imported, ok := imports.attributes[attrPath]
	return imported.framework, imported.method, ok
}

func rustQualifiedAttributeFrameworkMethod(name string) (string, string, bool) {
	trimmed := strings.TrimSpace(name)
	parts := strings.Split(trimmed, "::")
	if len(parts) != 2 {
		return "", "", false
	}
	switch parts[0] {
	case "actix_web":
		method, ok := rustHTTPMethodName(parts[1])
		return "actix_web", method, ok
	case "rocket":
		method, ok := rustHTTPMethodName(parts[1])
		return "rocket", method, ok
	default:
		return "", "", false
	}
}

func rustRouteAttributePath(attribute string) (string, bool) {
	open := strings.Index(attribute, "(")
	closeIndex := strings.LastIndex(attribute, ")")
	if open < 0 || closeIndex <= open {
		return "", false
	}
	args := rustSplitTopLevel(attribute[open+1:closeIndex], ',')
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		name, value, named := strings.Cut(arg, "=")
		if named && rustIdentifierPattern.MatchString(strings.TrimSpace(name)) {
			switch strings.TrimSpace(name) {
			case "path", "uri":
				return rustExactStringLiteral(value)
			default:
				continue
			}
		}
		if path, ok := rustExactStringLiteral(arg); ok {
			return path, true
		}
	}
	return "", false
}

// rustAxumRoutes resolves axum routes from candidates gathered during the
// main payload walk in Parse (see rustAxumCallCandidate), rather than
// re-walking the tree. Resolution is deferred to here because it needs the
// full import table, which is only complete once the main walk finishes.
func rustAxumRoutes(candidates []rustAxumCallCandidate, imports rustRouteImports) []rustRoute {
	routes := make([]rustRoute, 0)
	for _, candidate := range candidates {
		route, ok := rustAxumRouteCall(candidate.text, imports)
		if !ok {
			continue
		}
		route.order = candidate.endByte
		routes = append(routes, route)
	}
	sort.SliceStable(routes, func(i, j int) bool {
		return routes[i].order < routes[j].order
	})
	return routes
}

func rustAxumRouteCall(text string, imports rustRouteImports) (rustRoute, bool) {
	routeIndex := strings.LastIndex(text, ".route(")
	if routeIndex < 0 {
		return rustRoute{}, false
	}
	receiver := strings.TrimSpace(text[:routeIndex])
	if !rustIsAxumRouterNewChain(receiver, imports) {
		return rustRoute{}, false
	}
	body, ok := rustCallArgumentBody(text, routeIndex+len(".route"))
	if !ok {
		return rustRoute{}, false
	}
	args := rustSplitTopLevel(body, ',')
	if len(args) < 2 {
		return rustRoute{}, false
	}
	routePath, ok := rustExactStringLiteral(args[0])
	if !ok {
		return rustRoute{}, false
	}
	method, handler, ok := rustAxumMethodHandler(args[1], imports)
	if !ok {
		return rustRoute{}, false
	}
	return rustRoute{
		method:    method,
		path:      routePath,
		handler:   handler,
		framework: "axum",
	}, true
}

func rustIsAxumRouterNewChain(receiver string, imports rustRouteImports) bool {
	if rustContainsRouteConstructor(receiver, "axum::Router") {
		return true
	}
	for local := range imports.axumRouterNames {
		if rustContainsRouteConstructor(receiver, local) {
			return true
		}
	}
	return false
}

func rustContainsRouteConstructor(receiver string, target string) bool {
	needle := target + "::new()"
	for offset := 0; offset <= len(receiver)-len(needle); {
		idx := strings.Index(receiver[offset:], needle)
		if idx < 0 {
			return false
		}
		idx += offset
		if idx == 0 || !rustIdentifierByte(receiver[idx-1]) {
			return true
		}
		offset = idx + 1
	}
	return false
}

func rustIdentifierByte(ch byte) bool {
	return (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func rustAxumMethodHandler(text string, imports rustRouteImports) (string, string, bool) {
	trimmed := strings.TrimSpace(text)
	open := strings.Index(trimmed, "(")
	if open < 0 {
		return "", "", false
	}
	methodCall := strings.TrimSpace(trimmed[:open])
	body, ok := rustCallArgumentBody(trimmed, open)
	if !ok {
		return "", "", false
	}
	method, ok := rustAxumMethodName(methodCall, imports)
	if !ok {
		return "", "", false
	}
	args := rustSplitTopLevel(body, ',')
	if len(args) == 0 {
		return "", "", false
	}
	handler := strings.TrimSpace(args[0])
	if !rustIdentifierPattern.MatchString(handler) {
		return "", "", false
	}
	return method, handler, true
}

func rustAxumMethodName(name string, imports rustRouteImports) (string, bool) {
	if method, ok := rustQualifiedAxumMethod(name); ok {
		return method, true
	}
	if strings.Contains(name, "::") {
		return "", false
	}
	method, ok := imports.axumMethods[name]
	return method, ok
}

func rustQualifiedAxumMethod(name string) (string, bool) {
	trimmed := strings.TrimSpace(name)
	if !strings.HasPrefix(trimmed, "axum::routing::") {
		return "", false
	}
	return rustHTTPMethodName(shared.LastPathSegment(trimmed, "::"))
}

func rustHTTPMethodName(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "connect":
		return "CONNECT", true
	case "delete":
		return "DELETE", true
	case "get":
		return "GET", true
	case "head":
		return "HEAD", true
	case "options":
		return "OPTIONS", true
	case "patch":
		return "PATCH", true
	case "post":
		return "POST", true
	case "put":
		return "PUT", true
	case "trace":
		return "TRACE", true
	default:
		return "", false
	}
}
