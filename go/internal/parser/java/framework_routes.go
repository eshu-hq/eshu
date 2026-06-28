// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func buildJavaFrameworkSemantics(root *tree_sitter.Node, source []byte) map[string]any {
	semantics := map[string]any{"frameworks": []string{}}
	appendJavaRouteFramework(semantics, "spring", javaSpringRoutes(root, source))
	appendJavaRouteFramework(semantics, "jax_rs", javaJAXRSRoutes(root, source))
	appendJavaRouteFramework(semantics, "micronaut", javaMicronautRoutes(root, source))
	return semantics
}

func appendJavaRouteFramework(semantics map[string]any, name string, routes []javaSpringRoute) {
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

func javaJAXRSRoutes(root *tree_sitter.Node, source []byte) []javaSpringRoute {
	if root == nil {
		return nil
	}

	routes := make([]javaSpringRoute, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_declaration" {
			return
		}
		name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if name == "" {
			return
		}
		annotations := javaLeadingAnnotations(nodeText(node, source))
		method, ok := javaJAXRSHTTPMethod(annotations)
		if !ok {
			return
		}
		methodPath, methodPathOK := javaRequiredPathAnnotation(annotations)
		if javaHasPathAnnotation(annotations) && !methodPathOK {
			return
		}
		prefix, prefixPresent, prefixOK := javaJAXRSClassPrefix(node, source)
		if prefixPresent && !prefixOK {
			return
		}
		if !methodPathOK && !prefixOK {
			return
		}
		routes = append(routes, javaSpringRoute{
			method:  method,
			path:    javaJoinSpringRoutePath(prefix, methodPath),
			handler: name,
		})
	})
	return routes
}

func javaMicronautRoutes(root *tree_sitter.Node, source []byte) []javaSpringRoute {
	if root == nil {
		return nil
	}

	routes := make([]javaSpringRoute, 0)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_declaration" {
			return
		}
		name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if name == "" {
			return
		}
		methodRoute, ok := javaMicronautMethodRoute(javaLeadingAnnotations(nodeText(node, source)))
		if !ok {
			return
		}
		prefix, prefixOK := javaMicronautClassPrefix(node, source)
		if !prefixOK && methodRoute.path == "" {
			return
		}
		routes = append(routes, javaSpringRoute{
			method:  methodRoute.method,
			path:    javaJoinSpringRoutePath(prefix, methodRoute.path),
			handler: name,
		})
	})
	return routes
}

func javaJAXRSClassPrefix(node *tree_sitter.Node, source []byte) (string, bool, bool) {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "record_declaration":
			annotations := javaLeadingAnnotations(nodeText(current, source))
			if !javaHasPathAnnotation(annotations) {
				return "", false, false
			}
			prefix, ok := javaRequiredPathAnnotation(annotations)
			return prefix, true, ok
		}
	}
	return "", false, false
}

func javaMicronautClassPrefix(node *tree_sitter.Node, source []byte) (string, bool) {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "record_declaration":
			for _, annotation := range javaLeadingAnnotations(nodeText(current, source)) {
				if annotation.name != "Controller" {
					continue
				}
				return javaOptionalAnnotationPath(annotation)
			}
			return "", false
		}
	}
	return "", false
}

func javaJAXRSHTTPMethod(annotations []javaSpringAnnotation) (string, bool) {
	for _, annotation := range annotations {
		switch annotation.name {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
			return annotation.name, true
		}
	}
	return "", false
}

func javaMicronautMethodRoute(annotations []javaSpringAnnotation) (javaSpringRoute, bool) {
	for _, annotation := range annotations {
		method, ok := javaMicronautHTTPMethod(annotation.name)
		if !ok {
			continue
		}
		path, pathOK := javaOptionalAnnotationPath(annotation)
		if !pathOK {
			return javaSpringRoute{}, false
		}
		return javaSpringRoute{method: method, path: path}, true
	}
	return javaSpringRoute{}, false
}

func javaMicronautHTTPMethod(name string) (string, bool) {
	switch name {
	case "Get":
		return "GET", true
	case "Post":
		return "POST", true
	case "Put":
		return "PUT", true
	case "Patch":
		return "PATCH", true
	case "Delete":
		return "DELETE", true
	case "Head":
		return "HEAD", true
	case "Options":
		return "OPTIONS", true
	default:
		return "", false
	}
}

func javaHasPathAnnotation(annotations []javaSpringAnnotation) bool {
	for _, annotation := range annotations {
		if annotation.name == "Path" {
			return true
		}
	}
	return false
}

func javaRequiredPathAnnotation(annotations []javaSpringAnnotation) (string, bool) {
	for _, annotation := range annotations {
		if annotation.name != "Path" {
			continue
		}
		values := javaStringLiterals(annotation.body)
		if len(values) != 1 {
			return "", false
		}
		return javaNormalizeSpringRoutePath(values[0]), true
	}
	return "", false
}

func javaOptionalAnnotationPath(annotation javaSpringAnnotation) (string, bool) {
	if strings.TrimSpace(annotation.body) == "" {
		return "", true
	}
	values := javaStringLiterals(annotation.body)
	if len(values) != 1 {
		return "", false
	}
	return javaNormalizeSpringRoutePath(values[0]), true
}
