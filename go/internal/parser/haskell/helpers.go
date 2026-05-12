package haskell

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func haskellCollectModuleHeader(lines []string, start int) (string, int) {
	parts := []string{strings.TrimSpace(lines[start])}
	if strings.Contains(parts[0], " where") || strings.HasSuffix(parts[0], "where") {
		return parts[0], start
	}
	for index := start + 1; index < len(lines); index++ {
		parts = append(parts, strings.TrimSpace(lines[index]))
		header := strings.Join(parts, " ")
		if strings.Contains(header, " where") || strings.HasSuffix(header, "where") {
			return header, index
		}
	}
	return strings.Join(parts, " "), start
}

func haskellParseModuleExports(header string) map[string]struct{} {
	exports := make(map[string]struct{})
	whereIndex := strings.LastIndex(header, " where")
	if whereIndex == -1 {
		whereIndex = len(header)
	}
	beforeWhere := header[:whereIndex]
	open := strings.Index(beforeWhere, "(")
	close := strings.LastIndex(beforeWhere, ")")
	if open == -1 || close == -1 || close <= open {
		return exports
	}
	for _, part := range strings.Split(beforeWhere[open+1:close], ",") {
		name := strings.TrimSpace(part)
		name = strings.TrimPrefix(name, "pattern ")
		if paren := strings.Index(name, "("); paren >= 0 {
			name = strings.TrimSpace(name[:paren])
		}
		if fields := strings.Fields(name); len(fields) > 0 {
			name = fields[0]
		}
		if name != "" {
			exports[name] = struct{}{}
		}
	}
	return exports
}

func haskellParseImport(trimmed string) (string, string, bool) {
	if !strings.HasPrefix(trimmed, "import ") {
		return "", "", false
	}
	text := strings.TrimSpace(strings.TrimPrefix(trimmed, "import "))
	text = strings.TrimSpace(strings.TrimPrefix(text, "{-# SOURCE #-}"))
	for {
		switch {
		case strings.HasPrefix(text, "safe "):
			text = strings.TrimSpace(strings.TrimPrefix(text, "safe "))
		case strings.HasPrefix(text, "qualified "):
			text = strings.TrimSpace(strings.TrimPrefix(text, "qualified "))
		default:
			goto moduleName
		}
	}

moduleName:
	if strings.HasPrefix(text, "\"") {
		closeIndex := strings.Index(text[1:], "\"")
		if closeIndex == -1 {
			return "", "", false
		}
		text = strings.TrimSpace(text[closeIndex+2:])
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", "", false
	}
	name := fields[0]
	alias := ""
	for index, field := range fields {
		if field == "as" && index+1 < len(fields) {
			alias = strings.Trim(fields[index+1], "()[],")
			break
		}
	}
	if name == "" || !strings.ContainsAny(name[:1], "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return "", "", false
	}
	return name, alias, true
}

func haskellIsExplicitExport(exports map[string]struct{}, name string) bool {
	_, ok := exports[name]
	return ok
}

func haskellFunctionContextAndRoots(name, classContext, instanceContext string, exports map[string]struct{}) (string, []string) {
	switch {
	case classContext != "":
		return classContext, []string{"haskell.typeclass_method"}
	case instanceContext != "":
		return instanceContext, []string{"haskell.instance_method"}
	}
	rootKinds := make([]string, 0, 2)
	if name == "main" {
		rootKinds = append(rootKinds, "haskell.main_function")
	}
	if haskellIsExplicitExport(exports, name) {
		rootKinds = append(rootKinds, "haskell.module_export")
	}
	return "", rootKinds
}

func haskellFunctionKey(context, name string) string {
	return context + "\x00" + name
}

func haskellAppendFunctionCalls(
	payload map[string]any,
	line string,
	lineNumber int,
	functionName string,
	context string,
	params map[string]struct{},
	seenCalls map[string]struct{},
) {
	equalIndex := strings.Index(line, "=")
	if equalIndex == -1 || equalIndex == len(line)-1 {
		return
	}
	rhs := haskellStripStringsAndLineComment(line[equalIndex+1:])
	haskellAppendExpressionCalls(payload, rhs, lineNumber, functionName, context, params, seenCalls)
}

func haskellAppendExpressionCalls(
	payload map[string]any,
	expression string,
	lineNumber int,
	functionName string,
	context string,
	params map[string]struct{},
	seenCalls map[string]struct{},
) {
	rhs := haskellStripStringsAndLineComment(expression)
	for _, match := range haskellCallTokenPattern.FindAllStringIndex(rhs, -1) {
		if haskellTokenIsEmbedded(rhs, match[0], match[1]) {
			continue
		}
		fullName := rhs[match[0]:match[1]]
		name := fullName[strings.LastIndex(fullName, ".")+1:]
		if name == functionName || haskellIsKeyword(name) {
			continue
		}
		if _, ok := params[name]; ok {
			continue
		}
		key := context + "\x00" + functionName + "\x00" + fullName + "\x00" + strconv.Itoa(lineNumber)
		if _, ok := seenCalls[key]; ok {
			continue
		}
		seenCalls[key] = struct{}{}
		item := map[string]any{
			"name":        name,
			"full_name":   fullName,
			"line_number": lineNumber,
			"lang":        "haskell",
			"call_kind":   "haskell.function_call",
			"context":     functionName,
		}
		if context != "" {
			item["class_context"] = context
		}
		shared.AppendBucket(payload, "function_calls", item)
	}
}

func haskellTokenIsEmbedded(text string, start int, end int) bool {
	if start > 0 && haskellIdentifierByte(text[start-1]) {
		return true
	}
	return end < len(text) && haskellIdentifierByte(text[end])
}

func haskellIdentifierByte(char byte) bool {
	return char == '_' || char == '\'' ||
		(char >= '0' && char <= '9') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= 'a' && char <= 'z')
}

func haskellFunctionParameters(lhs string) map[string]struct{} {
	params := make(map[string]struct{})
	fields := strings.Fields(lhs)
	for _, field := range fields[1:] {
		name := strings.Trim(field, "()[],")
		if name == "" || !strings.ContainsAny(name[:1], "abcdefghijklmnopqrstuvwxyz_") {
			continue
		}
		params[name] = struct{}{}
	}
	return params
}

func haskellStripStringsAndLineComment(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	inString := false
	escaped := false
	for index := 0; index < len(text); index++ {
		char := text[index]
		if inString {
			if escaped {
				escaped = false
				builder.WriteByte(' ')
				continue
			}
			if char == '\\' {
				escaped = true
				builder.WriteByte(' ')
				continue
			}
			if char == '"' {
				inString = false
			}
			builder.WriteByte(' ')
			continue
		}
		if char == '"' {
			inString = true
			builder.WriteByte(' ')
			continue
		}
		if char == '-' && index+1 < len(text) && text[index+1] == '-' {
			break
		}
		builder.WriteByte(char)
	}
	return builder.String()
}

func haskellIsKeyword(name string) bool {
	switch name {
	case "case", "class", "data", "default", "deriving", "do", "else", "foreign", "if", "import",
		"in", "infix", "infixl", "infixr", "instance", "let", "module", "newtype", "of", "then",
		"type", "where":
		return true
	default:
		return false
	}
}

func haskellLeadingIndent(line string) int {
	indent := 0
	for _, char := range line {
		switch char {
		case ' ':
			indent++
		case '\t':
			indent += 8
		default:
			return indent
		}
	}
	return indent
}
