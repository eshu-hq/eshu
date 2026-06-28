// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin

import (
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type kotlinSpringAnnotation struct {
	name string
	body string
}

type kotlinSpringRoute struct {
	method  string
	path    string
	handler string
}

func kotlinSpringRoutes(root *tree_sitter.Node, source []byte) []kotlinSpringRoute {
	if root == nil {
		return nil
	}

	routes := make([]kotlinSpringRoute, 0)
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
		if name == "" {
			return
		}
		methodRoute, ok := kotlinSpringMethodRoute(kotlinLeadingAnnotations(shared.NodeText(node, source)))
		if !ok {
			return
		}
		prefix := kotlinSpringClassPrefix(node, source)
		routes = append(routes, kotlinSpringRoute{
			method:  methodRoute.method,
			path:    kotlinJoinSpringRoutePath(prefix, methodRoute.path),
			handler: name,
		})
	})
	return routes
}

func kotlinSpringClassPrefix(node *tree_sitter.Node, source []byte) string {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "object_declaration":
			prefix, _ := kotlinSpringRequestMappingPath(kotlinLeadingAnnotations(shared.NodeText(current, source)))
			return prefix
		}
	}
	return ""
}

func kotlinSpringMethodRoute(annotations []kotlinSpringAnnotation) (kotlinSpringRoute, bool) {
	for _, annotation := range annotations {
		method, ok := kotlinSpringAnnotationMethod(annotation)
		if !ok {
			continue
		}
		path, ok := kotlinSpringAnnotationPath(annotation)
		if !ok {
			continue
		}
		return kotlinSpringRoute{method: method, path: path}, true
	}
	return kotlinSpringRoute{}, false
}

func kotlinSpringRequestMappingPath(annotations []kotlinSpringAnnotation) (string, bool) {
	for _, annotation := range annotations {
		if annotation.name != "RequestMapping" {
			continue
		}
		return kotlinSpringAnnotationPath(annotation)
	}
	return "", false
}

func kotlinSpringAnnotationMethod(annotation kotlinSpringAnnotation) (string, bool) {
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
		if method, ok := kotlinSpringRequestMethod(annotation.body); ok {
			return method, true
		}
		return "ANY", true
	default:
		return "", false
	}
}

func kotlinSpringAnnotationPath(annotation kotlinSpringAnnotation) (string, bool) {
	values := kotlinStringLiterals(annotation.body)
	if len(values) != 1 {
		return "", false
	}
	return kotlinNormalizeSpringRoutePath(values[0]), true
}

func kotlinSpringRequestMethod(body string) (string, bool) {
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		if strings.Contains(body, "RequestMethod."+method) {
			return method, true
		}
	}
	return "", false
}

func kotlinLeadingAnnotations(raw string) []kotlinSpringAnnotation {
	annotations := make([]kotlinSpringAnnotation, 0)
	for index := 0; index < len(raw); {
		for index < len(raw) && unicode.IsSpace(rune(raw[index])) {
			index++
		}
		if index >= len(raw) || raw[index] != '@' {
			break
		}
		index++
		nameStart := index
		for index < len(raw) && isKotlinAnnotationNameByte(raw[index]) {
			index++
		}
		name := kotlinShortName(raw[nameStart:index])
		body := ""
		if index < len(raw) && raw[index] == '(' {
			close := kotlinMatchingParen(raw, index)
			if close < 0 {
				break
			}
			body = raw[index+1 : close]
			index = close + 1
		}
		annotations = append(annotations, kotlinSpringAnnotation{name: name, body: body})
	}
	return annotations
}

func kotlinStringLiterals(raw string) []string {
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

func kotlinJoinSpringRoutePath(prefix string, path string) string {
	prefix = kotlinNormalizeSpringRoutePath(prefix)
	path = kotlinNormalizeSpringRoutePath(path)
	if prefix == "" || prefix == "/" {
		return path
	}
	if path == "" || path == "/" {
		return prefix
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(path, "/")
}

func kotlinNormalizeSpringRoutePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func kotlinMatchingParen(raw string, open int) int {
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

func appendKotlinUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func isKotlinAnnotationNameByte(value byte) bool {
	return value == '_' || value == '.' ||
		(value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z') ||
		(value >= '0' && value <= '9')
}
