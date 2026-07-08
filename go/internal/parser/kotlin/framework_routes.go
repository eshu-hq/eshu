// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// kotlinFrameworkSemantics detects Spring, JAX-RS, Micronaut, and Ktor route
// evidence in one combined shared.WalkNamed pass. Before the walk-collapse fix
// (issue #4841, epic #4831) each framework ran its own full-tree WalkNamed
// pass over root; the four passes are independent (no shared mutable state,
// no pass consuming another's output), so they collapse into a single
// traversal that dispatches Spring/JAX-RS/Micronaut on "function_declaration"
// nodes and Ktor on "call_expression" nodes, matching the per-node kind each
// original pass already filtered on. Route slices are still built in exactly
// the traversal order shared.WalkNamed visits nodes, so per-framework route
// order is unchanged.
func kotlinFrameworkSemantics(root *tree_sitter.Node, source []byte) map[string]any {
	semantics := map[string]any{"frameworks": []string{}}
	if root == nil {
		return nil
	}

	var springRoutes, jaxRSRoutes, micronautRoutes, ktorRoutes []kotlinSpringRoute
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration":
			if route, ok := kotlinSpringRouteFromFunction(node, source); ok {
				springRoutes = append(springRoutes, route)
			}
			if route, ok := kotlinJAXRSRouteFromFunction(node, source); ok {
				jaxRSRoutes = append(jaxRSRoutes, route)
			}
			if route, ok := kotlinMicronautRouteFromFunction(node, source); ok {
				micronautRoutes = append(micronautRoutes, route)
			}
		case "call_expression":
			if route, ok := kotlinKtorRouteFromCall(node, source); ok {
				ktorRoutes = append(ktorRoutes, route)
			}
		}
	})

	appendKotlinRouteFramework(semantics, "spring", springRoutes)
	appendKotlinRouteFramework(semantics, "jax_rs", jaxRSRoutes)
	appendKotlinRouteFramework(semantics, "micronaut", micronautRoutes)
	appendKotlinRouteFramework(semantics, "ktor", ktorRoutes)
	if len(semantics["frameworks"].([]string)) == 0 {
		return nil
	}
	return semantics
}

func appendKotlinRouteFramework(semantics map[string]any, name string, routes []kotlinSpringRoute) {
	if len(routes) == 0 {
		return
	}

	methods := make([]string, 0, len(routes))
	paths := make([]string, 0, len(routes))
	entries := make([]map[string]string, 0, len(routes))
	for _, route := range routes {
		methods = appendKotlinUnique(methods, route.method)
		paths = appendKotlinUnique(paths, route.path)
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

// kotlinJAXRSRouteFromFunction returns the JAX-RS route evidence for one
// function_declaration node, or ok=false when the function carries no JAX-RS
// HTTP-method annotation with a resolvable path. Called from the combined
// kotlinFrameworkSemantics shared.WalkNamed pass instead of running its own
// full-tree walk.
func kotlinJAXRSRouteFromFunction(node *tree_sitter.Node, source []byte) (kotlinSpringRoute, bool) {
	name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
	if name == "" {
		return kotlinSpringRoute{}, false
	}
	annotations := kotlinLeadingAnnotations(shared.NodeText(node, source))
	method, ok := kotlinJAXRSHTTPMethod(annotations)
	if !ok {
		return kotlinSpringRoute{}, false
	}
	methodPath, methodPathOK := kotlinRequiredPathAnnotation(annotations)
	if kotlinHasPathAnnotation(annotations) && !methodPathOK {
		return kotlinSpringRoute{}, false
	}
	prefix, prefixPresent, prefixOK := kotlinJAXRSClassPrefix(node, source)
	if prefixPresent && !prefixOK {
		return kotlinSpringRoute{}, false
	}
	if !methodPathOK && !prefixOK {
		return kotlinSpringRoute{}, false
	}
	return kotlinSpringRoute{
		method:  method,
		path:    kotlinJoinSpringRoutePath(prefix, methodPath),
		handler: name,
	}, true
}

// kotlinMicronautRouteFromFunction returns the Micronaut route evidence for
// one function_declaration node, or ok=false when the function carries no
// Micronaut HTTP-method annotation. Called from the combined
// kotlinFrameworkSemantics shared.WalkNamed pass instead of running its own
// full-tree walk.
func kotlinMicronautRouteFromFunction(node *tree_sitter.Node, source []byte) (kotlinSpringRoute, bool) {
	name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
	if name == "" {
		return kotlinSpringRoute{}, false
	}
	methodRoute, ok := kotlinMicronautMethodRoute(kotlinLeadingAnnotations(shared.NodeText(node, source)))
	if !ok {
		return kotlinSpringRoute{}, false
	}
	prefix, prefixOK := kotlinMicronautClassPrefix(node, source)
	if !prefixOK && methodRoute.path == "" {
		return kotlinSpringRoute{}, false
	}
	return kotlinSpringRoute{
		method:  methodRoute.method,
		path:    kotlinJoinSpringRoutePath(prefix, methodRoute.path),
		handler: name,
	}, true
}

// kotlinKtorRouteFromCall returns the Ktor route evidence for one
// call_expression node, or ok=false when the call is not a recognized Ktor
// route registration. Called from the combined kotlinFrameworkSemantics
// shared.WalkNamed pass instead of running its own full-tree walk.
func kotlinKtorRouteFromCall(node *tree_sitter.Node, source []byte) (kotlinSpringRoute, bool) {
	method, pathNode, ok := kotlinKtorRouteCall(node, source)
	if !ok {
		return kotlinSpringRoute{}, false
	}
	path, ok := kotlinKtorRoutePath(pathNode, source)
	if !ok {
		return kotlinSpringRoute{}, false
	}
	handler, ok := kotlinKtorLambdaHandler(node, source)
	if !ok {
		return kotlinSpringRoute{}, false
	}
	return kotlinSpringRoute{
		method:  method,
		path:    path,
		handler: handler,
	}, true
}

func kotlinKtorRouteCall(node *tree_sitter.Node, source []byte) (string, *tree_sitter.Node, bool) {
	callee := kotlinFirstNamedChild(node)
	if callee == nil {
		return "", nil, false
	}
	if callee.Kind() == "identifier" {
		method, ok := kotlinKtorHTTPMethod(strings.TrimSpace(shared.NodeText(callee, source)))
		return method, node, ok
	}
	if callee.Kind() != "call_expression" {
		return "", nil, false
	}
	inner := kotlinFirstNamedChild(callee)
	if inner == nil || inner.Kind() != "identifier" {
		return "", nil, false
	}
	method, ok := kotlinKtorHTTPMethod(strings.TrimSpace(shared.NodeText(inner, source)))
	if !ok {
		return "", nil, false
	}
	return method, callee, true
}

func kotlinJAXRSClassPrefix(node *tree_sitter.Node, source []byte) (string, bool, bool) {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "object_declaration":
			annotations := kotlinLeadingAnnotations(shared.NodeText(current, source))
			if !kotlinHasPathAnnotation(annotations) {
				return "", false, false
			}
			prefix, ok := kotlinRequiredPathAnnotation(annotations)
			return prefix, true, ok
		}
	}
	return "", false, false
}

func kotlinMicronautClassPrefix(node *tree_sitter.Node, source []byte) (string, bool) {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "object_declaration":
			for _, annotation := range kotlinLeadingAnnotations(shared.NodeText(current, source)) {
				if annotation.name != "Controller" {
					continue
				}
				return kotlinOptionalAnnotationPath(annotation)
			}
			return "", false
		}
	}
	return "", false
}

func kotlinJAXRSHTTPMethod(annotations []kotlinSpringAnnotation) (string, bool) {
	for _, annotation := range annotations {
		switch annotation.name {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
			return annotation.name, true
		}
	}
	return "", false
}

func kotlinMicronautMethodRoute(annotations []kotlinSpringAnnotation) (kotlinSpringRoute, bool) {
	for _, annotation := range annotations {
		method, ok := kotlinMicronautHTTPMethod(annotation.name)
		if !ok {
			continue
		}
		path, pathOK := kotlinOptionalAnnotationPath(annotation)
		if !pathOK {
			return kotlinSpringRoute{}, false
		}
		return kotlinSpringRoute{method: method, path: path}, true
	}
	return kotlinSpringRoute{}, false
}

func kotlinMicronautHTTPMethod(name string) (string, bool) {
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

func kotlinKtorHTTPMethod(name string) (string, bool) {
	switch name {
	case "get":
		return "GET", true
	case "post":
		return "POST", true
	case "put":
		return "PUT", true
	case "patch":
		return "PATCH", true
	case "delete":
		return "DELETE", true
	case "head":
		return "HEAD", true
	case "options":
		return "OPTIONS", true
	default:
		return "", false
	}
}

func kotlinHasPathAnnotation(annotations []kotlinSpringAnnotation) bool {
	for _, annotation := range annotations {
		if annotation.name == "Path" {
			return true
		}
	}
	return false
}

func kotlinRequiredPathAnnotation(annotations []kotlinSpringAnnotation) (string, bool) {
	for _, annotation := range annotations {
		if annotation.name != "Path" {
			continue
		}
		values := kotlinStringLiterals(annotation.body)
		if len(values) != 1 {
			return "", false
		}
		return kotlinNormalizeSpringRoutePath(values[0]), true
	}
	return "", false
}

func kotlinOptionalAnnotationPath(annotation kotlinSpringAnnotation) (string, bool) {
	if strings.TrimSpace(annotation.body) == "" {
		return "", true
	}
	values := kotlinStringLiterals(annotation.body)
	if len(values) != 1 {
		return "", false
	}
	return kotlinNormalizeSpringRoutePath(values[0]), true
}

func kotlinKtorRoutePath(node *tree_sitter.Node, source []byte) (string, bool) {
	values := kotlinStringLiterals(shared.NodeText(node, source))
	if len(values) != 1 {
		return "", false
	}
	return kotlinNormalizeSpringRoutePath(values[0]), true
}

func kotlinKtorLambdaHandler(node *tree_sitter.Node, source []byte) (string, bool) {
	lambda := kotlinFirstDescendantByKind(node, "lambda_literal")
	if lambda == nil {
		return "", false
	}

	handlers := make([]string, 0, 1)
	shared.WalkNamed(lambda, func(child *tree_sitter.Node) {
		if child.Kind() != "call_expression" {
			return
		}
		callee := kotlinFirstNamedChild(child)
		if callee == nil || callee.Kind() != "identifier" {
			return
		}
		name := strings.TrimSpace(shared.NodeText(callee, source))
		if name == "" {
			return
		}
		handlers = append(handlers, name)
	})
	if len(handlers) != 1 {
		return "", false
	}
	return handlers[0], true
}

func kotlinFirstDescendantByKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	var found *tree_sitter.Node
	shared.WalkNamed(node, func(child *tree_sitter.Node) {
		if found != nil || child.Kind() != kind {
			return
		}
		found = shared.CloneNode(child)
	})
	return found
}

func kotlinFirstNamedChild(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		return shared.CloneNode(&child)
	}
	return nil
}
