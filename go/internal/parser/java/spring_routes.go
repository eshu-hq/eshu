// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"strings"
	"unicode"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type javaSpringAnnotation struct {
	name string
	body string
}

type javaSpringRoute struct {
	method  string
	path    string
	handler string
}

func buildJavaSpringFrameworkSemantics(root *tree_sitter.Node, source []byte) map[string]any {
	routes := javaSpringRoutes(root, source)
	if len(routes) == 0 {
		return map[string]any{"frameworks": []string{}}
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

	return map[string]any{
		"frameworks": []string{"spring"},
		"spring": map[string]any{
			"route_methods": methods,
			"route_paths":   paths,
			"route_entries": entries,
		},
	}
}

func javaSpringRoutes(root *tree_sitter.Node, source []byte) []javaSpringRoute {
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
		methodRoute, ok := javaSpringMethodRoute(javaLeadingAnnotations(nodeText(node, source)))
		if !ok {
			return
		}
		prefix := javaSpringClassPrefix(node, source)
		routes = append(routes, javaSpringRoute{
			method:  methodRoute.method,
			path:    javaJoinSpringRoutePath(prefix, methodRoute.path),
			handler: name,
		})
	})
	return routes
}

func javaSpringClassPrefix(node *tree_sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "record_declaration":
			prefix, _ := javaSpringRequestMappingPath(javaLeadingAnnotations(nodeText(current, source)))
			return prefix
		}
	}
	return ""
}

func javaSpringMethodRoute(annotations []javaSpringAnnotation) (javaSpringRoute, bool) {
	for _, annotation := range annotations {
		method, ok := javaSpringAnnotationMethod(annotation)
		if !ok {
			continue
		}
		path, ok := javaSpringAnnotationPath(annotation)
		if !ok {
			continue
		}
		return javaSpringRoute{method: method, path: path}, true
	}
	return javaSpringRoute{}, false
}

func javaSpringRequestMappingPath(annotations []javaSpringAnnotation) (string, bool) {
	for _, annotation := range annotations {
		if annotation.name != "RequestMapping" {
			continue
		}
		return javaSpringAnnotationPath(annotation)
	}
	return "", false
}

func javaSpringAnnotationMethod(annotation javaSpringAnnotation) (string, bool) {
	switch annotation.name {
	case "GetMapping":
		return "GET", true
	case "PostMapping":
		return "POST", true
	case "PutMapping":
		return "PUT", true
	case "PatchMapping":
		return "PATCH", true
	case "DeleteMapping":
		return "DELETE", true
	case "RequestMapping":
		if method, ok := javaSpringRequestMethod(annotation.body); ok {
			return method, true
		}
		return "ANY", true
	default:
		return "", false
	}
}

func javaSpringAnnotationPath(annotation javaSpringAnnotation) (string, bool) {
	values := javaStringLiterals(annotation.body)
	if len(values) != 1 {
		return "", false
	}
	return javaNormalizeSpringRoutePath(values[0]), true
}

func javaSpringRequestMethod(body string) (string, bool) {
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		if strings.Contains(body, "RequestMethod."+method) {
			return method, true
		}
	}
	return "", false
}

func javaLeadingAnnotations(raw string) []javaSpringAnnotation {
	annotations := make([]javaSpringAnnotation, 0)
	for index := 0; index < len(raw); {
		for index < len(raw) && unicode.IsSpace(rune(raw[index])) {
			index++
		}
		if index >= len(raw) || raw[index] != '@' {
			break
		}
		index++
		nameStart := index
		for index < len(raw) && isJavaAnnotationNameByte(raw[index]) {
			index++
		}
		name := javaShortAnnotationName(raw[nameStart:index])
		body := ""
		if index < len(raw) && raw[index] == '(' {
			close := javaMatchingParen(raw, index)
			if close < 0 {
				break
			}
			body = raw[index+1 : close]
			index = close + 1
		}
		annotations = append(annotations, javaSpringAnnotation{name: name, body: body})
	}
	return annotations
}

func javaStringLiterals(raw string) []string {
	values := make([]string, 0, 1)
	for index := 0; index < len(raw); index++ {
		if raw[index] != '"' {
			continue
		}
		start := index + 1
		escaped := false
		for index = start; index < len(raw); index++ {
			switch {
			case escaped:
				escaped = false
			case raw[index] == '\\':
				escaped = true
			case raw[index] == '"':
				values = append(values, raw[start:index])
				goto next
			}
		}
	next:
	}
	return values
}

func javaJoinSpringRoutePath(prefix string, path string) string {
	prefix = javaNormalizeSpringRoutePath(prefix)
	path = javaNormalizeSpringRoutePath(path)
	if prefix == "" || prefix == "/" {
		return path
	}
	if path == "" || path == "/" {
		return prefix
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(path, "/")
}

func javaNormalizeSpringRoutePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func javaMatchingParen(raw string, open int) int {
	depth := 0
	for index := open; index < len(raw); index++ {
		switch raw[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func javaShortAnnotationName(name string) string {
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		return name[dot+1:]
	}
	return name
}

func isJavaAnnotationNameByte(value byte) bool {
	return value == '_' || value == '.' ||
		(value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z') ||
		(value >= '0' && value <= '9')
}
