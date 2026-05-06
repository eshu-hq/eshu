package parser

import "strings"

func parseElixirModuleLine(trimmed string) (string, string, string, bool) {
	matches := elixirModulePattern.FindStringSubmatch(trimmed)
	if len(matches) != 3 {
		return "", "", "", false
	}
	keyword := matches[1]
	remainder := strings.TrimSpace(matches[2])
	if remainder == "" {
		return "", "", "", false
	}
	if index := strings.Index(remainder, " do"); index >= 0 {
		remainder = strings.TrimSpace(remainder[:index])
	}
	tail := ""
	if index := strings.Index(remainder, ","); index >= 0 {
		tail = strings.TrimSpace(remainder[index+1:])
		remainder = strings.TrimSpace(remainder[:index])
	}
	fields := strings.Fields(remainder)
	if len(fields) == 0 {
		return "", "", "", false
	}
	return keyword, fields[0], tail, true
}

func elixirModuleKind(keyword string) string {
	switch keyword {
	case "defprotocol":
		return "protocol"
	case "defimpl":
		return "protocol_implementation"
	default:
		return "module"
	}
}

func elixirFunctionSemanticKind(keyword string) string {
	switch keyword {
	case "defmacro", "defmacrop":
		return "macro"
	case "defdelegate":
		return "delegate"
	case "defguard", "defguardp":
		return "guard"
	default:
		return "function"
	}
}

func parseElixirDefImplTarget(tail string) string {
	trimmed := strings.TrimSpace(tail)
	if trimmed == "" {
		return ""
	}
	if index := strings.Index(trimmed, "for:"); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[index+len("for:"):])
	}
	if index := strings.Index(trimmed, " do"); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[:index])
	}
	if trimmed == "" {
		return ""
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSuffix(fields[0], ",")
}

func parseElixirFunctionLine(trimmed string) (string, string, []string, bool, bool) {
	matches := elixirFunctionPattern.FindStringSubmatch(trimmed)
	if len(matches) != 3 {
		return "", "", nil, false, false
	}
	keyword := matches[1]
	remainder := strings.TrimSpace(matches[2])
	if remainder == "" {
		return "", "", nil, false, false
	}

	name := remainder
	if index := strings.IndexAny(name, "(, \t"); index >= 0 {
		name = strings.TrimSpace(name[:index])
	}
	if name == "" {
		return "", "", nil, false, false
	}

	args := parseElixirArgs(remainder)
	openBlock := elixirLineOpensBlock(keyword, trimmed)
	return keyword, name, args, openBlock, true
}

func parseElixirImportLine(trimmed string) (string, []string, bool) {
	matches := elixirImportPattern.FindStringSubmatch(trimmed)
	if len(matches) != 3 {
		return "", nil, false
	}
	keyword := matches[1]
	remainder := strings.TrimSpace(matches[2])
	if remainder == "" {
		return "", nil, false
	}
	parts := splitElixirArgs(remainder)
	if len(parts) == 0 {
		return "", nil, false
	}
	remainder = strings.TrimSpace(parts[0])
	if keyword == "alias" {
		base := strings.TrimSpace(remainder)
		if base == "" {
			return "", nil, false
		}
		return keyword, expandElixirAliasPaths(base), true
	}
	fields := strings.Fields(remainder)
	if len(fields) == 0 {
		return "", nil, false
	}
	return keyword, []string{fields[0]}, true
}

func isElixirDefinitionLine(trimmed string) bool {
	for _, prefix := range []string{
		"defmodule ",
		"defprotocol ",
		"defimpl ",
		"def ",
		"defp ",
		"defmacro ",
		"defmacrop ",
		"defdelegate ",
		"defguard ",
		"defguardp ",
		"use ",
		"import ",
		"alias ",
		"require ",
	} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func elixirLineOpensBlock(keyword string, trimmed string) bool {
	switch keyword {
	case "defmodule", "defprotocol", "defimpl", "def", "defp", "defmacro", "defmacrop":
		return strings.Contains(trimmed, " do") && !strings.Contains(trimmed, ", do:")
	default:
		return false
	}
}

func parseElixirArgs(remainder string) []string {
	start := strings.Index(remainder, "(")
	if start < 0 {
		return []string{}
	}
	end := findMatchingParen(remainder, start)
	if end <= start {
		return []string{}
	}
	return splitElixirArgs(remainder[start+1 : end])
}

func elixirCallArgs(trimmed string, openParenIndex int) []string {
	if openParenIndex < 0 || openParenIndex >= len(trimmed) {
		return []string{}
	}
	end := findMatchingParen(trimmed, openParenIndex)
	if end <= openParenIndex {
		return []string{}
	}
	return splitElixirArgs(trimmed[openParenIndex+1 : end])
}

func findMatchingParen(text string, openParenIndex int) int {
	depth := 0
	inSingle := false
	inDouble := false
	inBacktick := false
	for index := openParenIndex; index < len(text); index++ {
		char := text[index]
		switch char {
		case '\\':
			if index+1 < len(text) {
				index++
			}
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		case '(':
			if !inSingle && !inDouble && !inBacktick {
				depth++
			}
		case ')':
			if !inSingle && !inDouble && !inBacktick {
				depth--
				if depth == 0 {
					return index
				}
			}
		}
	}
	return -1
}

func splitElixirArgs(body string) []string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return []string{}
	}

	args := make([]string, 0)
	current := strings.Builder{}
	depth := 0
	inSingle := false
	inDouble := false
	inBacktick := false

	flush := func() {
		value := strings.TrimSpace(current.String())
		if value != "" {
			args = append(args, value)
		}
		current.Reset()
	}

	for index := 0; index < len(trimmed); index++ {
		char := trimmed[index]
		switch char {
		case '\\':
			current.WriteByte(char)
			if index+1 < len(trimmed) {
				index++
				current.WriteByte(trimmed[index])
			}
			continue
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		case '(', '[', '{':
			if !inSingle && !inDouble && !inBacktick {
				depth++
			}
		case ')', ']', '}':
			if !inSingle && !inDouble && !inBacktick && depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 && !inSingle && !inDouble && !inBacktick {
				flush()
				continue
			}
		}
		current.WriteByte(char)
	}
	flush()
	return args
}

func elixirDocstringFromPreviousLine(previous string) string {
	trimmed := strings.TrimSpace(previous)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "@doc") || strings.HasPrefix(trimmed, "@moduledoc") || strings.HasPrefix(trimmed, "#") {
		return trimmed
	}
	return ""
}

func currentElixirModule(scopes []elixirScope) (string, int) {
	for index := len(scopes) - 1; index >= 0; index-- {
		if scopes[index].kind == "module" {
			return scopes[index].name, scopes[index].lineNumber
		}
	}
	return "", 0
}

func currentElixirContext(scopes []elixirScope) (string, string, int) {
	for index := len(scopes) - 1; index >= 0; index-- {
		scope := scopes[index]
		if scope.kind == "function" {
			return scope.name, "function", scope.lineNumber
		}
		if scope.kind == "module" {
			return scope.name, "module", scope.lineNumber
		}
	}
	return "", "", 0
}

func popElixirScope(scopes []elixirScope) ([]elixirScope, elixirScope) {
	if len(scopes) == 0 {
		return scopes, elixirScope{}
	}
	popped := scopes[len(scopes)-1]
	return scopes[:len(scopes)-1], popped
}

func lastAliasSegment(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, ".")
	return parts[len(parts)-1]
}

func expandElixirAliasPaths(base string) []string {
	trimmed := strings.TrimSpace(base)
	if trimmed == "" {
		return nil
	}
	openIndex := strings.Index(trimmed, "{")
	closeIndex := strings.Index(trimmed, "}")
	if openIndex < 0 || closeIndex < 0 || closeIndex <= openIndex {
		return []string{trimmed}
	}

	prefix := strings.TrimSpace(trimmed[:openIndex])
	suffix := strings.TrimSpace(trimmed[closeIndex+1:])
	options := splitElixirArgs(trimmed[openIndex+1 : closeIndex])
	expanded := make([]string, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option)
		if value == "" {
			continue
		}
		name := strings.TrimSpace(prefix + value + suffix)
		name = strings.TrimSuffix(name, ".")
		if name != "" {
			expanded = append(expanded, name)
		}
	}
	if len(expanded) == 0 {
		return []string{trimmed}
	}
	return expanded
}

func appendUniqueElixirCall(
	payload map[string]any,
	seen map[string]struct{},
	name string,
	fullName string,
	inferredObjType string,
	args []string,
	lineNumber int,
	contextName string,
	contextType string,
	contextLine int,
	classContext string,
	isDependency bool,
) {
	if strings.TrimSpace(fullName) == "" {
		return
	}
	if _, ok := seen[fullName]; ok {
		return
	}
	seen[fullName] = struct{}{}

	item := map[string]any{
		"name":          name,
		"full_name":     fullName,
		"line_number":   lineNumber,
		"args":          args,
		"lang":          "elixir",
		"is_dependency": isDependency,
	}
	if inferredObjType != "" {
		item["inferred_obj_type"] = inferredObjType
	} else {
		item["inferred_obj_type"] = nil
	}
	if contextName != "" && contextType != "" {
		item["context"] = []any{contextName, contextType, contextLine}
	}
	if classContext != "" {
		item["class_context"] = classContext
	}
	appendBucket(payload, "function_calls", item)
}
