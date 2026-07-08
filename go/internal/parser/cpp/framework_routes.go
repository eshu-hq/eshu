// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cpp

import (
	"strconv"
	"strings"
	"unicode"
)

func appendCPPRouteFramework(semantics map[string]any, name string, routes []cppRoute) {
	if len(routes) == 0 {
		return
	}

	methods := make([]string, 0, len(routes))
	paths := make([]string, 0, len(routes))
	entries := make([]map[string]string, 0, len(routes))
	for _, route := range routes {
		methods = appendCPPUnique(methods, route.method)
		paths = appendCPPUnique(paths, route.path)
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

func cppCrowRoutes(text string) []cppRoute {
	if !cppHasCallMarker(text, "CROW_ROUTE") {
		return nil
	}
	routeArgs, ok := cppCallArgumentsAfter(text, "CROW_ROUTE")
	if !ok || len(routeArgs) < 2 {
		return nil
	}
	path, ok := cppSingleStringLiteral(routeArgs[1])
	if !ok || !cppExactRoutePath(path) {
		return nil
	}
	methodArgs, ok := cppCallArgumentsAfter(text, ".methods")
	if !ok || len(methodArgs) == 0 {
		return nil
	}
	methods := cppHTTPMethodsFromArgument(strings.Join(methodArgs, ","))
	if len(methods) == 0 {
		return nil
	}
	handler, ok := cppTrailingHandlerArgument(text)
	if !ok {
		return nil
	}
	return cppRoutesForMethods(methods, path, handler)
}

func cppDrogonRoutes(text string) []cppRoute {
	if !cppDrogonRouteCall(text) {
		return nil
	}
	args, ok := cppCallArgumentsAfter(text, "registerHandler")
	if !ok || len(args) < 3 {
		return nil
	}
	path, ok := cppSingleStringLiteral(args[0])
	if !ok || !cppExactRoutePath(path) {
		return nil
	}
	handler, ok := cppHandlerArgument(args[1])
	if !ok {
		return nil
	}
	methods := cppHTTPMethodsFromArgument(args[2])
	if len(methods) == 0 {
		return nil
	}
	return cppRoutesForMethods(methods, path, handler)
}

func cppPistacheRoutes(text string) []cppRoute {
	if !strings.Contains(text, "Pistache::") {
		return nil
	}
	method, ok := cppPistacheMethod(text)
	if !ok {
		return nil
	}
	args, ok := cppCallArgumentsAfter(text, "Routes::"+cppTitleHTTPMethod(method))
	if !ok || len(args) < 3 {
		return nil
	}
	path, ok := cppSingleStringLiteral(args[1])
	if !ok || !cppExactRoutePath(path) {
		return nil
	}
	handler, ok := cppHandlerArgument(args[2])
	if !ok {
		return nil
	}
	return []cppRoute{{method: method, path: path, handler: handler}}
}

func cppDrogonRouteCall(text string) bool {
	return strings.Contains(text, "drogon::app().registerHandler(") ||
		strings.Contains(text, "drogon::HttpAppFramework::instance().registerHandler(")
}

func cppPistacheMethod(text string) (string, bool) {
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		if strings.Contains(text, "Routes::"+cppTitleHTTPMethod(method)+"(") {
			return method, true
		}
	}
	return "", false
}

func cppRoutesForMethods(methods []string, path string, handler string) []cppRoute {
	routes := make([]cppRoute, 0, len(methods))
	for _, method := range methods {
		routes = append(routes, cppRoute{method: method, path: path, handler: handler})
	}
	return routes
}

func cppTrailingHandlerArgument(text string) (string, bool) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), ";"))
	if !strings.HasSuffix(trimmed, ")") {
		return "", false
	}
	start := cppMatchingOpenParen(trimmed, len(trimmed)-1)
	if start < 0 {
		return "", false
	}
	return cppHandlerArgument(trimmed[start+1 : len(trimmed)-1])
}

func cppHandlerArgument(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "[]") || strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	if strings.Contains(trimmed, "Routes::bind(") {
		args, ok := cppCallArgumentsAfter(trimmed, "Routes::bind")
		if !ok || len(args) == 0 {
			return "", false
		}
		return cppHandlerArgument(args[0])
	}
	trimmed = strings.TrimPrefix(trimmed, "&")
	trimmed = strings.TrimSpace(trimmed)
	if strings.Contains(trimmed, "::") {
		parts := strings.Split(trimmed, "::")
		if len(parts) != 2 || !cppIdentifier(parts[0]) || !cppIdentifier(parts[1]) {
			return "", false
		}
		return parts[0] + "." + parts[1], true
	}
	if !cppIdentifier(trimmed) {
		return "", false
	}
	return trimmed, true
}

func cppCallArgumentsAfter(text string, marker string) ([]string, bool) {
	index := cppCallMarkerIndex(text, marker)
	if index < 0 {
		return nil, false
	}
	open := strings.Index(text[index+len(marker):], "(")
	if open < 0 {
		return nil, false
	}
	open += index + len(marker)
	close := cppMatchingCloseParen(text, open)
	if close < 0 {
		return nil, false
	}
	return cppSplitTopLevelArguments(text[open+1 : close]), true
}

func cppSplitTopLevelArguments(raw string) []string {
	var args []string
	start := 0
	parenDepth, braceDepth, bracketDepth := 0, 0, 0
	inString := false
	escaped := false
	for i, r := range raw {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case ',':
			if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
				args = append(args, strings.TrimSpace(raw[start:i]))
				start = i + 1
			}
		}
	}
	last := strings.TrimSpace(raw[start:])
	if last != "" {
		args = append(args, last)
	}
	return args
}

func cppMatchingCloseParen(text string, open int) int {
	depth := 0
	inString := false
	escaped := false
	for i := open; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func cppMatchingOpenParen(text string, close int) int {
	depth := 0
	inString := false
	escaped := false
	for i := close; i >= 0; i-- {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func cppSingleStringLiteral(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "\"") {
		return "", false
	}
	end := 1
	escaped := false
	for ; end < len(trimmed); end++ {
		ch := trimmed[end]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			break
		}
	}
	if end >= len(trimmed) || strings.TrimSpace(trimmed[end+1:]) != "" {
		return "", false
	}
	value, err := strconv.Unquote(trimmed[:end+1])
	if err != nil {
		return "", false
	}
	return value, true
}

func cppHTTPMethodsFromArgument(raw string) []string {
	methods := make([]string, 0, 2)
	for _, token := range cppIdentifierAndStringTokens(raw) {
		method, ok := cppHTTPMethodToken(token)
		if !ok {
			continue
		}
		methods = appendCPPUnique(methods, method)
	}
	return methods
}

func cppIdentifierAndStringTokens(raw string) []string {
	var tokens []string
	inString := false
	escaped := false
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}
	for _, r := range raw {
		if inString {
			if escaped {
				current.WriteRune(r)
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				flush()
				inString = false
				continue
			}
			current.WriteRune(r)
			continue
		}
		if r == '"' {
			flush()
			inString = true
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func cppHTTPMethodToken(token string) (string, bool) {
	method := strings.ToUpper(strings.TrimSuffix(token, "_method"))
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return method, true
	default:
		return "", false
	}
}

func cppTitleHTTPMethod(method string) string {
	return strings.ToUpper(method[:1]) + strings.ToLower(method[1:])
}

func cppExactRoutePath(path string) bool { return strings.HasPrefix(path, "/") }

func cppIdentifier(raw string) bool {
	if raw == "" {
		return false
	}
	for i, r := range raw {
		if i == 0 && !unicode.IsLetter(r) && r != '_' {
			return false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

func appendCPPUnique(items []string, item string) []string {
	if item == "" {
		return items
	}
	for _, existing := range items {
		if existing == item {
			return items
		}
	}
	return append(items, item)
}
