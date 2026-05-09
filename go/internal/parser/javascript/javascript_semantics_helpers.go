package javascript

import (
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func detectAWSSemantics(source string) (map[string]any, bool) {
	services := uniqueOrdered(javaScriptAWSImportRe.FindAllStringSubmatch(source, -1), 1)
	if len(services) == 0 {
		return nil, false
	}
	for index := range services {
		parts := strings.Split(services[index], "-")
		services[index] = parts[len(parts)-1]
	}
	return map[string]any{
		"services":       services,
		"client_symbols": uniqueOrdered(javaScriptClientSymbolRe.FindAllStringSubmatch(source, -1), 1),
	}, true
}

func detectGCPSemantics(source string) (map[string]any, bool) {
	services := uniqueOrdered(javaScriptGCPImportRe.FindAllStringSubmatch(source, -1), 1)
	if len(services) == 0 {
		return nil, false
	}
	return map[string]any{
		"services":       services,
		"client_symbols": uniqueOrdered(javaScriptClientSymbolRe.FindAllStringSubmatch(source, -1), 1),
	}, true
}

func detectReactSemantics(path string, source string, payload map[string]any) (map[string]any, bool) {
	componentExports := componentNames(payload)
	hooksUsed := uniqueOrdered(javaScriptHookCallRe.FindAllStringSubmatch(source, -1), 1)
	hasDirective := javaScriptDirectiveRe.MatchString(source)
	hasReactImport := strings.Contains(source, "from \"react\"") || strings.Contains(source, "from 'react'") ||
		strings.Contains(source, "require(\"react\")") || strings.Contains(source, "require('react')")
	if len(componentExports) == 0 && len(hooksUsed) == 0 && !hasDirective && !hasReactImport && !strings.HasSuffix(path, ".tsx") {
		return nil, false
	}

	boundary := "shared"
	if directive := javaScriptDirectiveRe.FindStringSubmatch(source); len(directive) == 2 {
		boundary = directive[1]
	}
	return map[string]any{
		"boundary":          boundary,
		"component_exports": componentExports,
		"hooks_used":        hooksUsed,
	}, true
}

func javaScriptContainsJSXFragmentShorthand(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind() == "jsx_element" {
		openTag := node.ChildByFieldName("open_tag")
		if openTag != nil && openTag.ChildByFieldName("name") == nil {
			return true
		}
	}
	cursor := node.Walk()
	children := node.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		child := children[i]
		if javaScriptContainsJSXFragmentShorthand(&child) {
			return true
		}
	}
	return false
}

func javaScriptAssertionTypeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "generic_type":
		if typeName := javaScriptAssertionTypeName(node.ChildByFieldName("name"), source); typeName != "" {
			return typeName
		}
	case "parenthesized_type", "union_type", "intersection_type":
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		for i := range children {
			child := children[i]
			if typeName := javaScriptAssertionTypeName(&child, source); typeName != "" {
				return typeName
			}
		}
	case "type_identifier", "identifier", "nested_type_identifier", "scoped_type_identifier", "member_expression":
		return strings.TrimSpace(nodeText(node, source))
	default:
		cursor := node.Walk()
		children := node.NamedChildren(cursor)
		cursor.Close()
		for i := range children {
			child := children[i]
			if typeName := javaScriptAssertionTypeName(&child, source); typeName != "" {
				return typeName
			}
		}
	}
	return ""
}

func componentNames(payload map[string]any) []string {
	items, _ := payload["components"].([]map[string]any)
	names := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		name, _ := item["name"].(string)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func nextJSRouteSegments(path string) []string {
	slashPath := filepath.ToSlash(path)
	appIndex := strings.Index(slashPath, "/app/")
	if appIndex < 0 {
		return []string{}
	}
	relative := slashPath[appIndex+len("/app/"):]
	dir := filepath.ToSlash(filepath.Dir(relative))
	if dir == "." || dir == "" {
		return []string{}
	}
	segments := strings.Split(dir, "/")
	filtered := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		filtered = append(filtered, segment)
	}
	return filtered
}

func nextJSRequestResponseAPIs(source string) []string {
	if !strings.Contains(source, "next/server") {
		return []string{}
	}
	apis := make([]string, 0, 2)
	for _, name := range []string{"NextRequest", "NextResponse"} {
		if strings.Contains(source, name) {
			apis = append(apis, name)
		}
	}
	return apis
}

func uniqueOrdered(matches [][]string, group int) []string {
	seen := make(map[string]struct{}, len(matches))
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) <= group {
			continue
		}
		value := strings.TrimSpace(match[group])
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func uniqueOrderedUpper(matches [][]string, group int) []string {
	values := uniqueOrdered(matches, group)
	for index := range values {
		values[index] = strings.ToUpper(values[index])
	}
	return values
}

func isPascalIdentifier(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	runes := []rune(name)
	return len(runes) > 0 && strings.ToUpper(string(runes[0])) == string(runes[0])
}
