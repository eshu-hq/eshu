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

// buildRubyFrameworkSemantics assembles the framework_semantics payload
// section from routesByFramework, the routes rubyCollectSemantics gathered
// during the single merged tree walk (see rubyCollectRouteCandidate and
// rubyResolveRouteContext), plus railsRouteAmbiguous, the #5494
// has_unmodeled_routes signal for the same file (see framework_routes_ambiguity.go).
func buildRubyFrameworkSemantics(routesByFramework map[string][]rubyRoute, railsRouteAmbiguous bool) map[string]any {
	semantics := map[string]any{"frameworks": []string{}}
	for _, framework := range []string{"rails", "sinatra"} {
		appendRubyRouteFramework(semantics, framework, routesByFramework[framework])
	}
	if railsRouteAmbiguous {
		appendRubyRailsRouteAmbiguity(semantics)
	}
	return semantics
}

// rubyCollectRouteCandidate resolves node (a "call" node visited by the
// merged rubyCollectSemantics walk) into a route, if it is an exact-path HTTP
// route call reachable under a Rails or Sinatra route-registration context.
// The cheap, node-local route shape (no receiver, HTTP-verb method name,
// literal exact path) is checked before resolving context, so the ancestor
// climb in rubyResolveRouteContext only runs for call nodes that already look
// like a route registration. The #5494 has_unmodeled_routes ambiguity signal
// is computed separately (framework_routes_ambiguity.go,
// rubyScanRailsDrawBlockForAmbiguity): it does not depend on this function's
// return shape, because a fail-safe design cannot afford to enumerate every
// call shape that ISN'T an exact route -- see that file's doc comment.
func (s *rubySyntax) rubyCollectRouteCandidate(node *tree_sitter.Node, topLevelSinatra bool) (framework string, route rubyRoute, ok bool) {
	if node.ChildByFieldName("receiver") != nil {
		return "", rubyRoute{}, false
	}
	methodNode := node.ChildByFieldName("method")
	method, isHTTPMethod := rubyHTTPRouteMethod(s.text(methodNode))
	if !isHTTPMethod {
		return "", rubyRoute{}, false
	}
	path := s.firstLiteralStringArgument(node)
	if !rubyExactRoutePath(path) {
		return "", rubyRoute{}, false
	}

	context := s.rubyResolveRouteContext(node, topLevelSinatra)
	switch context.framework {
	case "rails":
		handler, resolved := s.railsRouteHandler(node)
		if !resolved {
			return "", rubyRoute{}, false
		}
		return context.framework, rubyRoute{method: method, path: path, handler: handler}, true
	case "sinatra":
		handler, resolved := s.sinatraMethodHandler(node, context.className)
		if !resolved {
			return "", rubyRoute{}, false
		}
		return context.framework, rubyRoute{method: method, path: path, handler: handler}, true
	default:
		return "", rubyRoute{}, false
	}
}

// railsExactRouteEntry attempts to parse node as a fully-modeled, exact Rails
// route: a receiverless HTTP-verb call with a literal exact path and an
// explicit `to:` target that resolves into a clean, unqualified
// "controller#action" handler. It performs no context resolution -- callers
// (rubyCollectRouteCandidate, which has already climbed to confirm rails
// context, and rubyScanRailsDrawBlockForAmbiguity, which knows it by
// construction) must establish rails context themselves. Any call shape this
// function rejects is NOT necessarily routeless: it may be a route
// registration the parser cannot model exactly (see
// framework_routes_ambiguity.go), so callers must not treat ok=false as
// proof of absence on its own.
func (s *rubySyntax) railsExactRouteEntry(node *tree_sitter.Node) (rubyRoute, bool) {
	if node.ChildByFieldName("receiver") != nil {
		return rubyRoute{}, false
	}
	methodNode := node.ChildByFieldName("method")
	method, isHTTPMethod := rubyHTTPRouteMethod(s.text(methodNode))
	if !isHTTPMethod {
		return rubyRoute{}, false
	}
	path := s.firstLiteralStringArgument(node)
	if !rubyExactRoutePath(path) {
		return rubyRoute{}, false
	}
	handler, resolved := s.railsRouteHandler(node)
	if !resolved {
		return rubyRoute{}, false
	}
	return rubyRoute{method: method, path: path, handler: handler}, true
}

// rubyResolveRouteContext resolves the framework/class context a route
// candidate call node sees by climbing from node to its nearest
// context-changing ancestor: a class extending Sinatra::Base, or a
// Rails.application.routes.draw call. A non-matching class ancestor is
// transparent (the original top-down walk inherited the enclosing context
// through it unchanged), so the climb continues past it. This replaces the
// context parameter collectRubyRoutes used to thread down during its own
// dedicated recursive walk: the nearest context-changing ancestor found by
// climbing is exactly the context top-down threading would have assigned,
// because both mechanisms resolve to whichever enclosing scope is closest to
// node. Recovering context this way lets route candidates be resolved
// in-line during the single merged rubyCollectSemantics walk instead of
// requiring a second, context-threaded traversal.
func (s *rubySyntax) rubyResolveRouteContext(node *tree_sitter.Node, topLevelSinatra bool) rubyRouteContext {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class":
			if s.classExtendsSinatraBase(current) {
				return rubyRouteContext{
					framework: "sinatra",
					className: s.constantName(current.ChildByFieldName("name")),
				}
			}
		case "call":
			if s.isRailsRoutesDraw(current) {
				return rubyRouteContext{framework: "rails"}
			}
		}
	}
	if topLevelSinatra {
		return rubyRouteContext{framework: "sinatra"}
	}
	return rubyRouteContext{}
}

// isRailsRoutesDraw and rubyRailsRouteSetMethods live in
// framework_routes_ambiguity.go (shared by rubyResolveRouteContext here and
// the #5494/#5494-P1 ambiguity scan there).

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
