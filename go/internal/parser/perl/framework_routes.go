// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perl

import (
	"slices"
	"strconv"
	"strings"
)

type perlRoute struct {
	method  string
	path    string
	handler string
}

func buildPerlFrameworkSemantics(imports []map[string]any, calls []perlRouteCall) map[string]any {
	semantics := map[string]any{"frameworks": []string{}}
	framework, ok := perlRouteFramework(imports)
	if !ok {
		return semantics
	}

	routes := make([]perlRoute, 0, len(calls))
	seen := map[string]struct{}{}
	for _, call := range calls {
		route, ok := perlExactRouteCall(call.text)
		if !ok {
			continue
		}
		key := route.method + "\x00" + route.path + "\x00" + route.handler
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		routes = append(routes, route)
	}
	appendPerlRouteFramework(semantics, framework, routes)
	return semantics
}

func perlRouteFramework(imports []map[string]any) (string, bool) {
	mojolicious := false
	dancer := false
	for _, imported := range imports {
		switch strings.TrimSpace(anyPerlString(imported["name"])) {
		case "Mojolicious::Lite":
			mojolicious = true
		case "Dancer", "Dancer2":
			dancer = true
		}
	}
	switch {
	case mojolicious && !dancer:
		return "mojolicious", true
	case dancer && !mojolicious:
		return "dancer", true
	default:
		return "", false
	}
}

func appendPerlRouteFramework(semantics map[string]any, name string, routes []perlRoute) {
	if name == "" || len(routes) == 0 {
		return
	}

	methods := make([]string, 0, len(routes))
	paths := make([]string, 0, len(routes))
	entries := make([]map[string]string, 0, len(routes))
	for _, route := range routes {
		methods = perlUnique(methods, route.method)
		paths = perlUnique(paths, route.path)
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

func perlExactRouteCall(text string) (perlRoute, bool) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), ";"))
	method, rest, ok := perlRouteMethod(trimmed)
	if !ok {
		return perlRoute{}, false
	}
	rest, ok = perlRouteArguments(rest)
	if !ok {
		return perlRoute{}, false
	}
	path, rest, ok := perlStringLiteralPrefix(rest)
	if !ok || !perlExactRoutePath(path) {
		return perlRoute{}, false
	}
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "=>") {
		return perlRoute{}, false
	}
	handler, ok := perlCodeRefHandler(strings.TrimSpace(rest[2:]))
	if !ok {
		return perlRoute{}, false
	}
	return perlRoute{method: method, path: path, handler: handler}, true
}

func perlRouteMethod(text string) (string, string, bool) {
	for _, method := range []struct {
		name string
		http string
	}{
		{name: "get", http: "GET"},
		{name: "post", http: "POST"},
		{name: "put", http: "PUT"},
		{name: "patch", http: "PATCH"},
		{name: "del", http: "DELETE"},
		{name: "delete", http: "DELETE"},
		{name: "options", http: "OPTIONS"},
	} {
		if strings.HasPrefix(text, method.name) &&
			len(text) > len(method.name) &&
			isPerlRouteSeparator(rune(text[len(method.name)])) {
			return method.http, strings.TrimSpace(text[len(method.name):]), true
		}
	}
	return "", "", false
}

func perlRouteCallName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "get", "post", "put", "patch", "del", "delete", "options", "any":
		return true
	default:
		return false
	}
}

func perlStringLiteralPrefix(text string) (string, string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", false
	}
	quote := text[0]
	if quote != '"' && quote != '\'' {
		return "", "", false
	}
	escaped := false
	for i := 1; i < len(text); i++ {
		ch := text[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch != quote {
			continue
		}
		raw := text[:i+1]
		if quote == '\'' {
			raw = `"` + strings.ReplaceAll(strings.Trim(raw, "'"), `"`, `\"`) + `"`
		}
		value, err := strconv.Unquote(raw)
		if err != nil {
			return "", "", false
		}
		return value, text[i+1:], true
	}
	return "", "", false
}

func perlCodeRefHandler(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, `\&`) {
		return "", false
	}
	name := strings.TrimSpace(text[2:])
	if !isPerlQualifiedIdentifier(name) {
		return "", false
	}
	return name, true
}

func perlRouteArguments(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "(") {
		return text, true
	}
	return perlParenthesizedRouteArguments(text)
}

func perlParenthesizedRouteArguments(text string) (string, bool) {
	depth := 0
	escaped := false
	var quote byte
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == quote:
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				if strings.TrimSpace(text[i+1:]) != "" {
					return "", false
				}
				return strings.TrimSpace(text[1:i]), true
			}
			if depth < 0 {
				return "", false
			}
		}
	}
	return "", false
}

func perlExactRoutePath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/") && !strings.ContainsAny(path, "$@%")
}

func isPerlQualifiedIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for _, part := range strings.Split(name, "::") {
		if !isPerlIdentifier(part) {
			return false
		}
	}
	return true
}

func isPerlRouteSeparator(char rune) bool {
	return char == ' ' || char == '\t' || char == '\r' || char == '\n' || char == '(' || char == '"' || char == '\''
}

func perlUnique(values []string, value string) []string {
	if value == "" || slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func anyPerlString(raw any) string {
	value, _ := raw.(string)
	return value
}
