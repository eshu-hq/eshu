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
	if _, ok := usings["Microsoft.AspNetCore.Mvc"]; ok {
		appendCSharpRouteFramework(semantics, "aspnet", csharpASPNetAttributeRoutes(root, source))
	}
	if csharpHasAnyUsing(usings, "Microsoft.AspNetCore.Builder", "Microsoft.AspNetCore.Routing") {
		appendCSharpRouteFramework(semantics, "aspnet_minimal_api", csharpASPNetMinimalAPIRoutes(root, source))
	}
	return semantics
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

func csharpASPNetAttributeRoutes(root *tree_sitter.Node, source []byte) []csharpRoute {
	if root == nil {
		return nil
	}

	routes := make([]csharpRoute, 0)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_declaration" {
			return
		}
		name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
		if name == "" {
			return
		}
		syntax := csharpMethodSyntaxForNode(node, source)
		if csharpAttributesContainAny(syntax.attributes, "NonAction") {
			return
		}
		methodRoute, ok := csharpASPNetMethodRoute(csharpAttributesFromNode(node, source))
		if !ok {
			return
		}
		prefix, controllerName, prefixOK := csharpASPNetControllerPrefix(node, source)
		if !prefixOK {
			return
		}
		routePath := csharpJoinRoutePath(prefix, methodRoute.path)
		if routePath == "" {
			return
		}
		handler := name
		if controllerName != "" {
			handler = controllerName + "." + name
		}
		routes = append(routes, csharpRoute{
			method:  methodRoute.method,
			path:    routePath,
			handler: handler,
		})
	})
	return routes
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

func csharpASPNetMinimalAPIRoutes(root *tree_sitter.Node, source []byte) []csharpRoute {
	if root == nil {
		return nil
	}

	routes := make([]csharpRoute, 0)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "invocation_expression" {
			return
		}
		methods, pathArgIndex, handlerArgIndex, ok := csharpMinimalAPIMethods(node, source)
		if !ok {
			return
		}
		args := csharpInvocationArguments(shared.NodeText(node, source))
		if len(args) <= handlerArgIndex || len(args) <= pathArgIndex {
			return
		}
		path, ok := csharpSingleStringLiteral(args[pathArgIndex])
		if !ok || !csharpRoutePathIsExact(path) {
			return
		}
		handler := csharpIdentifierArgument(args[handlerArgIndex])
		if handler == "" {
			return
		}
		path = csharpNormalizeRoutePath(path)
		for _, method := range methods {
			routes = append(routes, csharpRoute{method: method, path: path, handler: handler})
		}
	})
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
