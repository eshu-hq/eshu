// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package csharp

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type csharpRoute struct {
	method  string
	path    string
	handler string
}

type csharpAttribute struct {
	name string
	body string
}

func buildCSharpFrameworkSemantics(root *tree_sitter.Node, source []byte) map[string]any {
	semantics := map[string]any{"frameworks": []string{}}
	usings := csharpUsingNames(root, source)
	wantASPNetAttr := csharpHasAnyUsing(usings, "Microsoft.AspNetCore.Mvc")
	wantMinimalAPI := csharpHasAnyUsing(usings, "Microsoft.AspNetCore.Builder", "Microsoft.AspNetCore.Routing")
	if !wantASPNetAttr && !wantMinimalAPI {
		return semantics
	}

	aspnetRoutes, minimalRoutes := csharpFrameworkRoutes(root, source, wantASPNetAttr, wantMinimalAPI)
	if wantASPNetAttr {
		appendCSharpRouteFramework(semantics, "aspnet", aspnetRoutes)
	}
	if wantMinimalAPI {
		appendCSharpRouteFramework(semantics, "aspnet_minimal_api", minimalRoutes)
	}
	return semantics
}

// csharpFrameworkRoutes runs the ASP.NET attribute-route and minimal-API
// route detectors in a single shared.WalkNamed pass instead of one dedicated
// walk per framework: the two detectors look at disjoint node kinds
// ("method_declaration" vs "invocation_expression") and neither result
// depends on the other, so they can share one traversal. Each gate gets
// skipped when its using directive is absent, exactly like the two dedicated
// walks it replaces, and each returned slice preserves the same
// per-node-kind document-order sequence a dedicated walk would have produced.
func csharpFrameworkRoutes(root *tree_sitter.Node, source []byte, wantASPNetAttr, wantMinimalAPI bool) (aspnetRoutes, minimalRoutes []csharpRoute) {
	if root == nil {
		return nil, nil
	}
	if wantASPNetAttr {
		aspnetRoutes = make([]csharpRoute, 0)
	}
	if wantMinimalAPI {
		minimalRoutes = make([]csharpRoute, 0)
	}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "method_declaration":
			if !wantASPNetAttr {
				return
			}
			if route, ok := csharpASPNetAttributeRouteForNode(node, source); ok {
				aspnetRoutes = append(aspnetRoutes, route)
			}
		case "invocation_expression":
			if !wantMinimalAPI {
				return
			}
			minimalRoutes = append(minimalRoutes, csharpASPNetMinimalAPIRoutesForNode(node, source)...)
		}
	})
	return aspnetRoutes, minimalRoutes
}

func appendCSharpRouteFramework(semantics map[string]any, name string, routes []csharpRoute) {
	if len(routes) == 0 {
		return
	}

	methods := make([]string, 0, len(routes))
	paths := make([]string, 0, len(routes))
	entries := make([]map[string]string, 0, len(routes))
	for _, route := range routes {
		methods = appendCSharpUnique(methods, route.method)
		paths = appendCSharpUnique(paths, route.path)
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

// csharpASPNetAttributeRouteForNode resolves at most one ASP.NET
// attribute-route entry for a single "method_declaration" node. Extracted
// from a former dedicated csharpASPNetAttributeRoutes walk so
// csharpFrameworkRoutes can call it inline from the merged single-pass walk.
func csharpASPNetAttributeRouteForNode(node *tree_sitter.Node, source []byte) (csharpRoute, bool) {
	name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
	if name == "" {
		return csharpRoute{}, false
	}
	syntax := csharpMethodSyntaxForNode(node, source)
	if csharpAttributesContainAny(syntax.attributes, "NonAction") {
		return csharpRoute{}, false
	}
	methodRoute, ok := csharpASPNetMethodRoute(csharpAttributesFromNode(node, source))
	if !ok {
		return csharpRoute{}, false
	}
	prefix, controllerName, prefixOK := csharpASPNetControllerPrefix(node, source)
	if !prefixOK {
		return csharpRoute{}, false
	}
	routePath := csharpJoinRoutePath(prefix, methodRoute.path)
	if routePath == "" {
		return csharpRoute{}, false
	}
	handler := name
	if controllerName != "" {
		handler = controllerName + "." + name
	}
	return csharpRoute{
		method:  methodRoute.method,
		path:    routePath,
		handler: handler,
	}, true
}

func csharpASPNetMethodRoute(attributes []csharpAttribute) (csharpRoute, bool) {
	for _, attribute := range attributes {
		if method, ok := csharpHTTPAttributeMethod(attribute.name); ok {
			path, pathOK := csharpOptionalAttributePath(attribute)
			if !pathOK {
				return csharpRoute{}, false
			}
			return csharpRoute{method: method, path: path}, true
		}
		if csharpShortAttributeName(attribute.name) == "Route" {
			path, pathOK := csharpRequiredAttributePath(attribute)
			if !pathOK {
				return csharpRoute{}, false
			}
			return csharpRoute{method: "ANY", path: path}, true
		}
	}
	return csharpRoute{}, false
}

func csharpASPNetControllerPrefix(node *tree_sitter.Node, source []byte) (string, string, bool) {
	for current := node.Parent(); current != nil; current = current.Parent() {
		if current.Kind() != "class_declaration" && current.Kind() != "record_declaration" {
			continue
		}
		controllerName := strings.TrimSpace(shared.NodeText(current.ChildByFieldName("name"), source))
		if !csharpASPNetControllerClass(current, controllerName, source) {
			return "", "", false
		}
		for _, attribute := range csharpAttributesFromNode(current, source) {
			if csharpShortAttributeName(attribute.name) != "Route" {
				continue
			}
			prefix, ok := csharpRequiredAttributePath(attribute)
			return prefix, controllerName, ok
		}
		return "", controllerName, true
	}
	return "", "", true
}

func csharpASPNetControllerClass(node *tree_sitter.Node, name string, source []byte) bool {
	if strings.HasSuffix(name, "Controller") {
		return true
	}
	for _, base := range csharpBaseNames(node, source) {
		switch csharpLastTypeSegment(base) {
		case "Controller", "ControllerBase":
			return true
		}
	}
	return false
}

// csharpASPNetMinimalAPIRoutesForNode resolves zero or more minimal-API route
// entries for a single "invocation_expression" node (a MapMethods call can
// register more than one HTTP method against the same path/handler).
// Extracted from a former dedicated csharpASPNetMinimalAPIRoutes walk so
// csharpFrameworkRoutes can call it inline from the merged single-pass walk.
func csharpASPNetMinimalAPIRoutesForNode(node *tree_sitter.Node, source []byte) []csharpRoute {
	methods, pathArgIndex, handlerArgIndex, ok := csharpMinimalAPIMethods(node, source)
	if !ok {
		return nil
	}
	args := csharpInvocationArguments(shared.NodeText(node, source))
	if len(args) <= handlerArgIndex || len(args) <= pathArgIndex {
		return nil
	}
	path, ok := csharpSingleStringLiteral(args[pathArgIndex])
	if !ok || !csharpRoutePathIsExact(path) {
		return nil
	}
	handler := csharpIdentifierArgument(args[handlerArgIndex])
	if handler == "" {
		return nil
	}
	path = csharpNormalizeRoutePath(path)
	routes := make([]csharpRoute, 0, len(methods))
	for _, method := range methods {
		routes = append(routes, csharpRoute{method: method, path: path, handler: handler})
	}
	return routes
}

func csharpMinimalAPIMethods(node *tree_sitter.Node, source []byte) ([]string, int, int, bool) {
	functionNode := node.ChildByFieldName("function")
	name := strings.TrimSpace(shared.NodeText(csharpCallNameNode(functionNode), source))
	switch name {
	case "MapGet":
		return []string{"GET"}, 0, 1, true
	case "MapPost":
		return []string{"POST"}, 0, 1, true
	case "MapPut":
		return []string{"PUT"}, 0, 1, true
	case "MapPatch":
		return []string{"PATCH"}, 0, 1, true
	case "MapDelete":
		return []string{"DELETE"}, 0, 1, true
	case "MapMethods":
		args := csharpInvocationArguments(shared.NodeText(node, source))
		if len(args) < 3 {
			return nil, 0, 0, false
		}
		methods := csharpHTTPMethodsFromArgument(args[1])
		if len(methods) == 0 {
			return nil, 0, 0, false
		}
		return methods, 0, 2, true
	default:
		return nil, 0, 0, false
	}
}

func csharpHTTPAttributeMethod(name string) (string, bool) {
	switch csharpShortAttributeName(name) {
	case "HttpGet":
		return "GET", true
	case "HttpPost":
		return "POST", true
	case "HttpPut":
		return "PUT", true
	case "HttpPatch":
		return "PATCH", true
	case "HttpDelete":
		return "DELETE", true
	case "HttpHead":
		return "HEAD", true
	case "HttpOptions":
		return "OPTIONS", true
	default:
		return "", false
	}
}

func csharpOptionalAttributePath(attribute csharpAttribute) (string, bool) {
	path, hasTemplate, ok := csharpAttributeTemplate(attribute)
	if !ok {
		return "", false
	}
	if !hasTemplate {
		return "", true
	}
	if !csharpRoutePathIsExact(path) {
		return "", false
	}
	return csharpNormalizeRoutePath(path), true
}

func csharpRequiredAttributePath(attribute csharpAttribute) (string, bool) {
	path, hasTemplate, ok := csharpAttributeTemplate(attribute)
	if !ok || !hasTemplate || !csharpRoutePathIsExact(path) {
		return "", false
	}
	return csharpNormalizeRoutePath(path), true
}

func csharpAttributeTemplate(attribute csharpAttribute) (string, bool, bool) {
	body := strings.TrimSpace(attribute.body)
	if body == "" {
		return "", false, true
	}
	for index, arg := range csharpSplitTopLevelArguments(body) {
		name, value, named := csharpSplitNamedAttributeArgument(arg)
		if named {
			if strings.EqualFold(name, "template") {
				path, ok := csharpSingleStringLiteral(value)
				return path, true, ok
			}
			continue
		}
		if index == 0 {
			path, ok := csharpSingleStringLiteral(arg)
			return path, true, ok
		}
	}
	return "", false, true
}
