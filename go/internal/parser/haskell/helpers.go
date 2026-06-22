package haskell

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

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

// haskellAppendRHSCalls records call evidence from the right-hand side of a
// definition or binding line: it slices after the first `=` so the name being
// bound (the binder LHS, including local where/let bindings such as
// `helper y = ...`) is never reported as a call. The package contract is that
// function-call rows are bounded lexical evidence from definition right-hand
// sides, not the bound names themselves. Lines without a defining `=` (bare
// continuation expressions) are scanned whole.
func haskellAppendRHSCalls(
	payload map[string]any,
	line string,
	lineNumber int,
	functionName string,
	context string,
	params map[string]struct{},
	seenCalls map[string]struct{},
) {
	expression := line
	if equalIndex := strings.Index(line, "="); equalIndex != -1 {
		if equalIndex == len(line)-1 {
			return
		}
		expression = line[equalIndex+1:]
	}
	haskellAppendExpressionCalls(payload, expression, lineNumber, functionName, context, params, seenCalls)
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
