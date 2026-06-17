package javascript

import (
	"path/filepath"
	"regexp"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	javaScriptHTTPVerbExportRe = regexp.MustCompile(`(?m)export\s+(?:async\s+)?function\s+(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\b`)
	javaScriptMetadataConstRe  = regexp.MustCompile(`(?m)export\s+const\s+metadata\b`)
	javaScriptExpressRouteRe   = regexp.MustCompile(`(?m)\b([A-Za-z_$][A-Za-z0-9_$]*)\.(get|post|put|patch|delete|head|options)\(\s*["']([^"']+)["']`)
	// javaScriptExpressRouteHandlerRe binds an Express route to its handler only
	// when the callback is a single bare named reference closing the call
	// (e.g. app.get("/x", getX)). Inline callbacks and middleware chains do not
	// match, so they stay unbound rather than guess a handler symbol (#2721).
	javaScriptExpressRouteHandlerRe = regexp.MustCompile(`(?m)\b[A-Za-z_$][A-Za-z0-9_$]*\.(get|post|put|patch|delete|head|options)\(\s*["']([^"']+)["']\s*,\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\)`)
	javaScriptHapiMethodRe          = regexp.MustCompile(`(?m)\bmethod\s*:\s*["']([A-Za-z]+)["']`)
	javaScriptHapiPathRe            = regexp.MustCompile(`(?m)\bpath\s*:\s*["']([^"']+)["']`)
	javaScriptAWSImportRe           = regexp.MustCompile(`@aws-sdk/client-([a-z0-9-]+)`)
	javaScriptGCPImportRe           = regexp.MustCompile(`@google-cloud/([a-z0-9-]+)`)
	javaScriptClientSymbolRe        = regexp.MustCompile(`\b([A-Z][A-Za-z0-9]+Client)\b`)
	javaScriptHookCallRe            = regexp.MustCompile(`\b(use[A-Z][A-Za-z0-9_]*)\s*\(`)
	javaScriptDirectiveRe           = regexp.MustCompile(`(?m)^\s*["']use\s+(client|server)["'];?`)
	javaScriptJSXReturnRe           = regexp.MustCompile(`(?m)(return\s*<|=>\s*<)`)
)

func maybeAppendJavaScriptComponent(
	payload map[string]any,
	node *tree_sitter.Node,
	nameNode *tree_sitter.Node,
	source []byte,
	outputLanguage string,
	reactAliases map[string]string,
) {
	name := strings.TrimSpace(nodeText(nameNode, source))
	if !isPascalIdentifier(name) {
		return
	}
	if !javaScriptLooksLikeComponent(node, source, outputLanguage) {
		return
	}
	item := map[string]any{
		"name":        name,
		"line_number": nodeLine(nameNode),
		"end_line":    nodeEndLine(node),
		"lang":        outputLanguage,
	}
	if outputLanguage == "tsx" && javaScriptContainsJSXFragmentShorthand(node) {
		item["jsx_fragment_shorthand"] = true
	}
	if outputLanguage == "tsx" {
		if wrapperKind := javaScriptComponentWrapperKind(node, source, reactAliases); wrapperKind != "" {
			item["component_wrapper_kind"] = wrapperKind
		}
	}
	appendBucket(payload, "components", item)
}

func javaScriptComponentWrapperKind(node *tree_sitter.Node, source []byte, reactAliases map[string]string) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "parenthesized_expression":
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		for i := range children {
			if wrapper := javaScriptComponentWrapperKind(&children[i], source, reactAliases); wrapper != "" {
				return wrapper
			}
		}
	case "call_expression":
		functionNode := node.ChildByFieldName("function")
		name := javaScriptNormalizeReactAlias(strings.TrimSpace(javaScriptCallName(functionNode, source)), reactAliases)
		switch name {
		case "memo", "forwardRef", "lazy":
			return name
		}
	}
	return ""
}

func javaScriptComponentTypeAssertion(node *tree_sitter.Node, source []byte, reactAliases map[string]string) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "type_annotation":
		if typeNode := node.ChildByFieldName("type"); typeNode != nil {
			if typeName := javaScriptAssertionTypeName(typeNode, source); typeName != "" {
				return typeName
			}
		}
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		for i := range children {
			child := children[i]
			if typeName := javaScriptAssertionTypeName(&child, source); typeName != "" {
				return javaScriptNormalizeReactAlias(typeName, reactAliases)
			}
		}
	case "as_expression", "type_assertion":
		if typeName := javaScriptAssertionTypeName(node.ChildByFieldName("type"), source); typeName != "" {
			return javaScriptNormalizeReactAlias(typeName, reactAliases)
		}
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		if len(children) >= 2 {
			return javaScriptNormalizeReactAlias(javaScriptAssertionTypeName(&children[1], source), reactAliases)
		}
	case "parenthesized_expression":
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		for i := range children {
			child := children[i]
			if typeName := javaScriptComponentTypeAssertion(&child, source, reactAliases); typeName != "" {
				return typeName
			}
		}
	}
	return ""
}

func javaScriptNormalizeReactAlias(name string, reactAliases map[string]string) string {
	name = strings.TrimSpace(name)
	if name == "" || len(reactAliases) == 0 {
		return name
	}
	if normalized, ok := reactAliases[name]; ok && normalized != "" {
		return normalized
	}
	return name
}

func javaScriptReactAliases(root *tree_sitter.Node, source []byte, outputLanguage string) map[string]string {
	if root == nil || outputLanguage != "tsx" {
		return nil
	}

	reactAliases := map[string]string{}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "import_statement" {
			return
		}
		for _, item := range javaScriptImportEntries(node, source, outputLanguage) {
			sourceName, _ := item["source"].(string)
			if sourceName != "react" {
				continue
			}
			alias, _ := item["alias"].(string)
			if alias == "" {
				continue
			}
			name, _ := item["name"].(string)
			if name == "" || name == "*" || name == "default" {
				continue
			}
			switch name {
			case "ComponentType", "FC", "FunctionComponent", "memo", "forwardRef", "lazy":
				reactAliases[alias] = name
			}
		}
	})
	if len(reactAliases) == 0 {
		return nil
	}
	return reactAliases
}

func javaScriptLooksLikeComponent(node *tree_sitter.Node, source []byte, outputLanguage string) bool {
	if outputLanguage == "tsx" {
		return true
	}
	text := nodeText(node, source)
	return strings.Contains(text, "React.Component") ||
		strings.Contains(text, "React.PureComponent") ||
		strings.Contains(text, "useState(") ||
		strings.Contains(text, "useEffect(") ||
		strings.Contains(text, "useMemo(") ||
		javaScriptJSXReturnRe.MatchString(text)
}

func buildJavaScriptFrameworkSemantics(path string, source []byte, payload map[string]any) map[string]any {
	text := string(source)
	semantics := map[string]any{
		"frameworks": []string{},
	}
	frameworks := make([]string, 0, 6)

	if nextjs, ok := detectNextJSSemantics(path, text); ok {
		frameworks = append(frameworks, "nextjs")
		semantics["nextjs"] = nextjs
	}
	if express, ok := detectExpressSemantics(text); ok {
		frameworks = append(frameworks, "express")
		semantics["express"] = express
	}
	if aws, ok := detectAWSSemantics(text); ok {
		frameworks = append(frameworks, "aws")
		semantics["aws"] = aws
	}
	if gcp, ok := detectGCPSemantics(text); ok {
		frameworks = append(frameworks, "gcp")
		semantics["gcp"] = gcp
	}
	if react, ok := detectReactSemantics(path, text, payload); ok {
		frameworks = append(frameworks, "react")
		semantics["react"] = react
	}
	if hapi, ok := detectHapiSemantics(text); ok {
		frameworks = append(frameworks, "hapi")
		semantics["hapi"] = hapi
	}

	semantics["frameworks"] = frameworks
	return semantics
}

func detectNextJSSemantics(path string, source string) (map[string]any, bool) {
	moduleKind := ""
	switch filepath.Base(path) {
	case "route.ts", "route.tsx", "route.js", "route.jsx":
		moduleKind = "route"
	case "page.tsx", "page.jsx", "page.ts", "page.js":
		moduleKind = "page"
	case "layout.tsx", "layout.jsx", "layout.ts", "layout.js":
		moduleKind = "layout"
	}
	if moduleKind == "" {
		return nil, false
	}

	routeSegments := nextJSRouteSegments(path)
	metadataExports := "none"
	if strings.Contains(source, "generateMetadata") {
		metadataExports = "dynamic"
	} else if javaScriptMetadataConstRe.MatchString(source) {
		metadataExports = "static"
	}

	runtimeBoundary := "server"
	if directive := javaScriptDirectiveRe.FindStringSubmatch(source); len(directive) == 2 {
		runtimeBoundary = directive[1]
	}

	nextjs := map[string]any{
		"module_kind":      moduleKind,
		"metadata_exports": metadataExports,
		"route_segments":   routeSegments,
		"runtime_boundary": runtimeBoundary,
	}
	if moduleKind == "route" {
		nextjs["route_verbs"] = uniqueOrderedUpper(javaScriptHTTPVerbExportRe.FindAllStringSubmatch(source, -1), 1)
		nextjs["request_response_apis"] = nextJSRequestResponseAPIs(source)
	}
	return nextjs, true
}

func javaScriptHasExpressImport(source string) bool {
	return strings.Contains(source, `require("express")`) ||
		strings.Contains(source, `require('express')`) ||
		strings.Contains(source, `from "express"`) ||
		strings.Contains(source, `from 'express'`)
}

func detectExpressSemantics(source string) (map[string]any, bool) {
	if !javaScriptHasExpressImport(source) {
		return nil, false
	}
	matches := javaScriptExpressRouteRe.FindAllStringSubmatch(source, -1)
	if len(matches) == 0 {
		return nil, false
	}

	handlersByRoute := expressRouteHandlers(source)
	routeRegistrations := expressRouteRegistrationCounts(matches)
	methods := make([]string, 0, len(matches))
	paths := make([]string, 0, len(matches))
	entries := make([]map[string]string, 0, len(matches))
	serverSymbols := make([]string, 0, len(matches))
	seenMethods := make(map[string]struct{})
	seenPaths := make(map[string]struct{})
	seenSymbols := make(map[string]struct{})
	for _, match := range matches {
		symbol := match[1]
		method := strings.ToUpper(match[2])
		path := match[3]
		key := expressRouteKey(method, path)
		handler := ""
		// A route registered exactly once has an unambiguous handler. A route
		// registered more than once (e.g. an inline and a named callback, or two
		// routers) is ambiguous about which handler serves it, so it stays
		// unbound rather than attach a handler to the wrong entry (#2721).
		if routeRegistrations[key] == 1 {
			handler = handlersByRoute[key]
		}
		entries = append(entries, routeEntry(method, path, handler))
		if _, ok := seenMethods[method]; !ok {
			seenMethods[method] = struct{}{}
			methods = append(methods, method)
		}
		if _, ok := seenPaths[path]; !ok {
			seenPaths[path] = struct{}{}
			paths = append(paths, path)
		}
		if _, ok := seenSymbols[symbol]; !ok {
			seenSymbols[symbol] = struct{}{}
			serverSymbols = append(serverSymbols, symbol)
		}
	}

	return map[string]any{
		"route_methods":  methods,
		"route_paths":    paths,
		"route_entries":  entries,
		"server_symbols": serverSymbols,
	}, true
}

func detectHapiSemantics(source string) (map[string]any, bool) {
	if strings.Contains(source, "server.inject(") {
		return nil, false
	}
	if !javaScriptHasHapiRouteSignal(source) {
		return nil, false
	}
	methods := uniqueOrderedUpper(javaScriptHapiMethodRe.FindAllStringSubmatch(source, -1), 1)
	paths := uniqueOrdered(javaScriptHapiPathRe.FindAllStringSubmatch(source, -1), 1)
	if len(methods) == 0 || len(paths) == 0 {
		return nil, false
	}
	return map[string]any{
		"route_methods":  methods,
		"route_paths":    paths,
		"route_entries":  javaScriptHapiRouteEntries(source),
		"server_symbols": []string{},
	}, true
}

// javaScriptHasHapiRouteSignal keeps generic config objects with method/path
// fields from being classified as Hapi routes unless the file shows Hapi usage.
func javaScriptHasHapiRouteSignal(source string) bool {
	return strings.Contains(source, "server.route(") ||
		strings.Contains(source, `require("@hapi/hapi")`) ||
		strings.Contains(source, `require('@hapi/hapi')`) ||
		strings.Contains(source, `require("hapi")`) ||
		strings.Contains(source, `require('hapi')`) ||
		strings.Contains(source, `from "@hapi/hapi"`) ||
		strings.Contains(source, `from '@hapi/hapi'`)
}

// routeEntry is the parser-owned wire shape consumed by query read models. The
// handler symbol is included only when an exact route↔handler binding was
// observed; an empty handler is omitted so consumers never read a fabricated
// binding for an inline or middleware-wrapped route (#2721).
func routeEntry(method string, path string, handler string) map[string]string {
	entry := map[string]string{
		"method": strings.ToUpper(strings.TrimSpace(method)),
		"path":   strings.TrimSpace(path),
	}
	if handler = strings.TrimSpace(handler); handler != "" {
		entry["handler"] = handler
	}
	return entry
}

// expressRouteKey identifies an Express route by its normalized method and path
// so a separately-scanned handler binding can be matched back to its entry.
func expressRouteKey(method string, path string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + strings.TrimSpace(path)
}

// expressRouteHandlers maps an Express route to its handler function symbol for
// routes whose callback is a single bare named reference. Ambiguity from a route
// registered more than once is resolved by the registration-count guard in the
// caller, so this only records the observed named handler per route key (#2721).
func expressRouteHandlers(source string) map[string]string {
	matches := javaScriptExpressRouteHandlerRe.FindAllStringSubmatch(source, -1)
	if len(matches) == 0 {
		return nil
	}
	handlers := make(map[string]string, len(matches))
	for _, match := range matches {
		handler := strings.TrimSpace(match[3])
		if handler == "" {
			continue
		}
		handlers[expressRouteKey(match[1], match[2])] = handler
	}
	return handlers
}

// expressRouteRegistrationCounts counts how many times each route key (method
// and path) is registered across all Express route calls, so a route declared
// more than once can be treated as an ambiguous handler binding (#2721).
func expressRouteRegistrationCounts(matches [][]string) map[string]int {
	counts := make(map[string]int, len(matches))
	for _, match := range matches {
		counts[expressRouteKey(match[2], match[3])]++
	}
	return counts
}
