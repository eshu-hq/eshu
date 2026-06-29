// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"strings"
	"unicode"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type rubyRoute struct {
	method  string
	path    string
	handler string
}

type rubyRouteContext struct {
	framework string
	className string
}

func buildRubyFrameworkSemantics(syntax *rubySyntax) map[string]any {
	semantics := map[string]any{"frameworks": []string{}}
	if syntax == nil || syntax.root == nil {
		return semantics
	}

	routesByFramework := make(map[string][]rubyRoute)
	context := rubyRouteContext{}
	if rubyImportsSinatra(syntax.imports) {
		context.framework = "sinatra"
	}
	syntax.collectRubyRoutes(syntax.root, context, routesByFramework)

	for _, framework := range []string{"rails", "sinatra"} {
		appendRubyRouteFramework(semantics, framework, routesByFramework[framework])
	}
	return semantics
}

func (s *rubySyntax) collectRubyRoutes(
	node *tree_sitter.Node,
	context rubyRouteContext,
	routesByFramework map[string][]rubyRoute,
) {
	if node == nil {
		return
	}

	if node.Kind() == "class" {
		nextContext := context
		if s.classExtendsSinatraBase(node) {
			nextContext = rubyRouteContext{
				framework: "sinatra",
				className: s.constantName(node.ChildByFieldName("name")),
			}
		}
		s.collectRubyRouteChildren(node, nextContext, routesByFramework)
		return
	}

	if node.Kind() == "call" {
		if s.isRailsRoutesDraw(node) {
			s.collectRubyRouteChildren(node, rubyRouteContext{framework: "rails"}, routesByFramework)
			return
		}
		if route, ok := s.exactRubyRoute(node, context); ok {
			routesByFramework[context.framework] = append(routesByFramework[context.framework], route)
		}
	}

	s.collectRubyRouteChildren(node, context, routesByFramework)
}

func (s *rubySyntax) collectRubyRouteChildren(
	node *tree_sitter.Node,
	context rubyRouteContext,
	routesByFramework map[string][]rubyRoute,
) {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return
	}
	for {
		child := cursor.Node()
		if child.IsNamed() {
			s.collectRubyRoutes(child, context, routesByFramework)
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
}

func (s *rubySyntax) exactRubyRoute(node *tree_sitter.Node, context rubyRouteContext) (rubyRoute, bool) {
	if context.framework == "" || node.ChildByFieldName("receiver") != nil {
		return rubyRoute{}, false
	}
	methodNode := node.ChildByFieldName("method")
	method, ok := rubyHTTPRouteMethod(s.text(methodNode))
	if !ok {
		return rubyRoute{}, false
	}
	path := s.firstLiteralStringArgument(node)
	if !rubyExactRoutePath(path) {
		return rubyRoute{}, false
	}

	switch context.framework {
	case "rails":
		handler, ok := s.railsRouteHandler(node)
		if !ok {
			return rubyRoute{}, false
		}
		return rubyRoute{method: method, path: path, handler: handler}, true
	case "sinatra":
		handler, ok := s.sinatraMethodHandler(node, context.className)
		if !ok {
			return rubyRoute{}, false
		}
		return rubyRoute{method: method, path: path, handler: handler}, true
	default:
		return rubyRoute{}, false
	}
}

func (s *rubySyntax) isRailsRoutesDraw(node *tree_sitter.Node) bool {
	method := node.ChildByFieldName("method")
	if s.text(method) != "draw" {
		return false
	}
	receiver := node.ChildByFieldName("receiver")
	return s.receiverName(receiver) == "Rails.application.routes"
}

func (s *rubySyntax) classExtendsSinatraBase(node *tree_sitter.Node) bool {
	superclass := node.ChildByFieldName("superclass")
	if superclass == nil {
		return false
	}
	return strings.Contains(s.text(superclass), "Sinatra::Base")
}

func (s *rubySyntax) firstLiteralStringArgument(node *tree_sitter.Node) string {
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	cursor := args.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return ""
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && child.Kind() == "string" {
			return s.literalStringContent(child)
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return ""
}

func (s *rubySyntax) literalStringContent(node *tree_sitter.Node) string {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return ""
	}
	var value string
	for {
		child := cursor.Node()
		if child.IsNamed() {
			if child.Kind() != "string_content" || value != "" {
				return ""
			}
			value = s.text(child)
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return value
}

func (s *rubySyntax) railsRouteHandler(node *tree_sitter.Node) (string, bool) {
	raw, ok := s.routePairStringValue(node, "to")
	if !ok {
		return "", false
	}
	parts := strings.Split(raw, "#")
	if len(parts) != 2 {
		return "", false
	}
	controller, action := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if strings.ContainsAny(controller, "/:") || !isRubyMethodName(action) {
		return "", false
	}
	className, ok := rubyControllerClassName(controller)
	if !ok {
		return "", false
	}
	return className + "." + action, true
}

func (s *rubySyntax) routePairStringValue(node *tree_sitter.Node, key string) (string, bool) {
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	cursor := args.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return "", false
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && child.Kind() == "pair" {
			if value, ok := s.pairStringValue(child, key); ok {
				return value, true
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return "", false
}

func (s *rubySyntax) pairStringValue(node *tree_sitter.Node, key string) (string, bool) {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return "", false
	}
	matchedKey := false
	for {
		child := cursor.Node()
		if child.IsNamed() {
			switch child.Kind() {
			case "hash_key_symbol":
				matchedKey = s.text(child) == key
			case "string":
				if matchedKey {
					value := s.literalStringContent(child)
					return value, value != ""
				}
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return "", false
}

func (s *rubySyntax) sinatraMethodHandler(node *tree_sitter.Node, className string) (string, bool) {
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	cursor := args.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return "", false
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && child.Kind() == "block_argument" {
			if handler, ok := s.methodBlockArgument(child); ok {
				if className != "" {
					return className + "." + handler, true
				}
				return handler, true
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return "", false
}

func (s *rubySyntax) methodBlockArgument(node *tree_sitter.Node) (string, bool) {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return "", false
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && child.Kind() == "call" && s.text(child.ChildByFieldName("method")) == "method" {
			return s.firstSimpleSymbolArgument(child)
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return "", false
}

func (s *rubySyntax) firstSimpleSymbolArgument(node *tree_sitter.Node) (string, bool) {
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	cursor := args.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return "", false
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && child.Kind() == "simple_symbol" {
			name := strings.TrimPrefix(s.text(child), ":")
			if isRubyMethodName(name) {
				return name, true
			}
			return "", false
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return "", false
}

func appendRubyRouteFramework(semantics map[string]any, name string, routes []rubyRoute) {
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
		methods = appendUniqueRubyRouteValue(methods, route.method)
		paths = appendUniqueRubyRouteValue(paths, route.path)
		entries = append(entries, map[string]string{
			"method":  route.method,
			"path":    route.path,
			"handler": route.handler,
		})
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

func appendUniqueRubyRouteValue(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func rubyImportsSinatra(imports []map[string]any) bool {
	for _, imported := range imports {
		switch strings.TrimSpace(anyRubyString(imported["name"])) {
		case "sinatra":
			return true
		}
	}
	return false
}

func rubyHTTPRouteMethod(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
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

func rubyExactRoutePath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/") && !strings.Contains(path, "#{")
}

func rubyControllerClassName(controller string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(controller), "_")
	if len(parts) == 0 {
		return "", false
	}
	var builder strings.Builder
	for _, part := range parts {
		if part == "" || !isRubyIdentifierStart([]rune(part)[0]) || !isRubyIdentifierPart(part) {
			return "", false
		}
		runes := []rune(part)
		builder.WriteRune(unicode.ToUpper(runes[0]))
		builder.WriteString(string(runes[1:]))
	}
	builder.WriteString("Controller")
	return builder.String(), true
}

func isRubyIdentifierPart(value string) bool {
	for _, ch := range value {
		if ch != '_' && !unicode.IsLetter(ch) && !unicode.IsDigit(ch) {
			return false
		}
	}
	return true
}

func isRubyIdentifierStart(ch rune) bool {
	return ch == '_' || unicode.IsLetter(ch)
}

func isRubyMethodName(name string) bool {
	if name == "" {
		return false
	}
	runes := []rune(name)
	if !isRubyIdentifierStart(runes[0]) {
		return false
	}
	for index, ch := range runes[1:] {
		last := index == len(runes)-2
		if ch == '_' || unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			continue
		}
		if last && (ch == '?' || ch == '!' || ch == '=') {
			continue
		}
		return false
	}
	return true
}

func anyRubyString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
