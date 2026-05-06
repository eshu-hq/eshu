package parser

import (
	"regexp"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	pythonFastAPIAppAssignRe    = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*FastAPI)?\s*=\s*FastAPI\(`)
	pythonFastAPIRouterAssignRe = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*APIRouter)?\s*=\s*APIRouter\(([^)]*)\)`)
	pythonFlaskAssignRe         = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(?:Flask|create_app)\(`)
	pythonFastAPIDecoratorRe    = regexp.MustCompile(`(?m)^@([A-Za-z_][A-Za-z0-9_]*)\.(get|post|put|patch|delete|options|head)\(\s*["']([^"']+)["']`)
	pythonFlaskRouteRe          = regexp.MustCompile(`(?m)^@([A-Za-z_][A-Za-z0-9_]*)\.route\(\s*["']([^"']+)["'](?:,\s*methods\s*=\s*\[([^\]]*)\])?`)
	pythonClassLineRe           = regexp.MustCompile(`^class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	pythonTablenameRe           = regexp.MustCompile(`^__tablename__\s*=\s*["']([^"']+)["']`)
	pythonDBTableRe             = regexp.MustCompile(`^db_table\s*=\s*["']([^"']+)["']`)
	pythonPrefixRe              = regexp.MustCompile(`\bprefix\s*=\s*["']([^"']+)["']`)
	pythonMethodTokenRe         = regexp.MustCompile(`["']([A-Za-z]+)["']`)
)

func buildPythonFrameworkSemantics(source string) map[string]any {
	fastAPI := detectPythonFastAPISemantics(source)
	flask := detectPythonFlaskSemantics(source)
	frameworks := make([]string, 0, 2)
	semantics := map[string]any{
		"frameworks": []string{},
	}
	if fastAPI != nil {
		frameworks = append(frameworks, "fastapi")
		semantics["fastapi"] = fastAPI
	}
	if flask != nil {
		frameworks = append(frameworks, "flask")
		semantics["flask"] = flask
	}
	semantics["frameworks"] = frameworks
	return semantics
}

func detectPythonFastAPISemantics(source string) map[string]any {
	serverSymbols := uniqueOrdered(pythonFastAPIAppAssignRe.FindAllStringSubmatch(source, -1), 1)
	routerMatches := pythonFastAPIRouterAssignRe.FindAllStringSubmatch(source, -1)
	routerPrefixes := make(map[string]string, len(routerMatches))
	for _, match := range routerMatches {
		routerPrefixes[match[1]] = pythonPrefix(match[2])
		serverSymbols = appendUniqueString(serverSymbols, match[1])
	}

	decorators := pythonFastAPIDecoratorRe.FindAllStringSubmatch(source, -1)
	if len(serverSymbols) == 0 || len(decorators) == 0 {
		return nil
	}

	methods := make([]string, 0, len(decorators))
	paths := make([]string, 0, len(decorators))
	entries := make([]map[string]string, 0, len(decorators))
	for _, match := range decorators {
		symbol := match[1]
		path := match[3]
		if prefix := routerPrefixes[symbol]; prefix != "" {
			path = prefix + path
		}
		method := strings.ToUpper(match[2])
		methods = appendUniqueString(methods, method)
		paths = appendUniqueString(paths, path)
		entries = append(entries, routeEntry(method, path))
	}

	return map[string]any{
		"route_methods":  methods,
		"route_paths":    paths,
		"route_entries":  entries,
		"server_symbols": serverSymbols,
	}
}

func detectPythonFlaskSemantics(source string) map[string]any {
	serverSymbols := uniqueOrdered(pythonFlaskAssignRe.FindAllStringSubmatch(source, -1), 1)
	if len(serverSymbols) == 0 {
		return nil
	}

	routes := pythonFlaskRouteRe.FindAllStringSubmatch(source, -1)
	if len(routes) == 0 {
		return nil
	}

	methods := make([]string, 0, len(routes))
	paths := make([]string, 0, len(routes))
	entries := make([]map[string]string, 0, len(routes))
	allowed := make(map[string]struct{}, len(serverSymbols))
	for _, symbol := range serverSymbols {
		allowed[symbol] = struct{}{}
	}

	for _, match := range routes {
		symbol := match[1]
		if _, ok := allowed[symbol]; !ok {
			continue
		}
		paths = appendUniqueString(paths, match[2])
		routeMethods := []string{"GET"}
		if strings.TrimSpace(match[3]) != "" {
			routeMethods = pythonRouteMethods(match[3])
		}
		for _, method := range routeMethods {
			methods = appendUniqueString(methods, method)
			entries = append(entries, routeEntry(method, match[2]))
		}
	}
	if len(paths) == 0 {
		return nil
	}

	return map[string]any{
		"route_methods":  methods,
		"route_paths":    paths,
		"route_entries":  entries,
		"server_symbols": serverSymbols,
	}
}

func buildPythonORMTableMappings(source string) []map[string]any {
	lines := strings.Split(source, "\n")
	mappings := make([]map[string]any, 0)

	currentClass := ""
	currentClassIndent := -1
	currentClassLine := 0
	inMetaClass := false
	metaIndent := -1
	for index, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		indent := leadingWhitespace(rawLine)
		if currentClass != "" && indent <= currentClassIndent && !strings.HasPrefix(line, "class ") {
			currentClass = ""
			currentClassIndent = -1
			currentClassLine = 0
			inMetaClass = false
			metaIndent = -1
		}
		if inMetaClass && indent <= metaIndent {
			inMetaClass = false
			metaIndent = -1
		}

		if matches := pythonClassLineRe.FindStringSubmatch(line); matches != nil {
			if line == "class Meta:" && currentClass != "" {
				inMetaClass = true
				metaIndent = indent
				continue
			}
			currentClass = matches[1]
			currentClassIndent = indent
			currentClassLine = index + 1
			inMetaClass = false
			metaIndent = -1
			continue
		}

		if currentClass == "" {
			continue
		}
		if matches := pythonTablenameRe.FindStringSubmatch(line); matches != nil {
			mappings = append(mappings, map[string]any{
				"class_name":        currentClass,
				"class_line_number": currentClassLine,
				"table_name":        matches[1],
				"framework":         "sqlalchemy",
				"line_number":       index + 1,
			})
			continue
		}
		if inMetaClass {
			if matches := pythonDBTableRe.FindStringSubmatch(line); matches != nil {
				mappings = append(mappings, map[string]any{
					"class_name":        currentClass,
					"class_line_number": currentClassLine,
					"table_name":        matches[1],
					"framework":         "django",
					"line_number":       index + 1,
				})
			}
		}
	}

	return mappings
}

func pythonPrefix(argumentText string) string {
	matches := pythonPrefixRe.FindStringSubmatch(argumentText)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}

func pythonRouteMethods(methodList string) []string {
	matches := pythonMethodTokenRe.FindAllStringSubmatch(methodList, -1)
	if len(matches) == 0 {
		return []string{"GET"}
	}
	methods := make([]string, 0, len(matches))
	for _, match := range matches {
		methods = appendUniqueString(methods, strings.ToUpper(match[1]))
	}
	return methods
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func pythonDocstring(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	body := node.ChildByFieldName("body")
	if body == nil {
		if node.Kind() == "module" {
			body = node
		} else {
			return ""
		}
	}
	if body == nil {
		return ""
	}

	cursor := body.Walk()
	defer cursor.Close()

	children := body.NamedChildren(cursor)
	if len(children) == 0 {
		return ""
	}

	child := children[0]
	if child.Kind() != "expression_statement" {
		return ""
	}

	stringNode := firstNamedDescendant(&child, "string", "concatenated_string")
	if stringNode == nil {
		return ""
	}
	return cleanPythonDocstringLiteral(nodeText(stringNode, source))
}

func cleanPythonDocstringLiteral(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	for len(trimmed) > 0 {
		switch trimmed[0] {
		case 'r', 'R', 'u', 'U', 'b', 'B', 'f', 'F':
			trimmed = trimmed[1:]
		default:
			goto prefixDone
		}
	}
prefixDone:
	switch {
	case strings.HasPrefix(trimmed, `"""`) && strings.HasSuffix(trimmed, `"""`) && len(trimmed) >= 6:
		trimmed = trimmed[3 : len(trimmed)-3]
	case strings.HasPrefix(trimmed, `'''`) && strings.HasSuffix(trimmed, `'''`) && len(trimmed) >= 6:
		trimmed = trimmed[3 : len(trimmed)-3]
	case strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) && len(trimmed) >= 2:
		trimmed = trimmed[1 : len(trimmed)-1]
	case strings.HasPrefix(trimmed, `'`) && strings.HasSuffix(trimmed, `'`) && len(trimmed) >= 2:
		trimmed = trimmed[1 : len(trimmed)-1]
	}

	return strings.TrimSpace(trimmed)
}

func cyclomaticComplexity(node *tree_sitter.Node) int {
	if node == nil {
		return 0
	}

	complexity := 1
	var walk func(*tree_sitter.Node)
	walk = func(current *tree_sitter.Node) {
		if current == nil {
			return
		}
		if current != node && isNestedDefinition(current.Kind()) {
			return
		}
		if isCyclomaticBranchKind(current.Kind()) {
			complexity++
		}

		cursor := current.Walk()
		defer cursor.Close()
		for _, child := range current.NamedChildren(cursor) {
			child := child
			walk(&child)
		}
	}

	walk(node)
	return complexity
}

func isNestedDefinition(kind string) bool {
	switch kind {
	case "class_definition", "function_definition", "lambda":
		return true
	default:
		return false
	}
}

func isCyclomaticBranchKind(kind string) bool {
	switch kind {
	case "if_statement",
		"elif_clause",
		"for_statement",
		"while_statement",
		"except_clause",
		"case_clause",
		"switch_statement",
		"type_switch_statement",
		"select_statement",
		"conditional_expression":
		return true
	default:
		return false
	}
}

func leadingWhitespace(raw string) int {
	count := 0
	for _, char := range raw {
		if char != ' ' && char != '\t' {
			break
		}
		count++
	}
	return count
}
