package haskell

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// haskellCallTokenPattern matches qualified and bare lower-case identifiers in a
// right-hand-side expression. Call extraction is the documented evidence
// exception: it reports bounded lexical call tokens, not resolved Haskell name
// binding, so it stays a regex over AST-bounded source text.
var haskellCallTokenPattern = regexp.MustCompile(`(?:[A-Z][A-Za-z0-9_']*\.)+[a-z_][A-Za-z0-9_']*|[a-z_][A-Za-z0-9_']*`)

// haskellIsExplicitExport reports whether name appears in the module export list.
func haskellIsExplicitExport(exports map[string]struct{}, name string) bool {
	_, ok := exports[name]
	return ok
}

// haskellFunctionContextAndRoots resolves the class/instance context and
// dead-code root kinds for a top-level or method binding. Class and instance
// contexts yield their method roots; a top-level `main` and explicit module
// exports yield function roots.
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

// haskellFunctionKey builds the dedupe key for a function row from its context
// and name so the same name in different class/instance contexts stays distinct.
func haskellFunctionKey(context, name string) string {
	return context + "\x00" + name
}

// haskellLineRangeSource returns the joined source lines covering an inclusive
// 1-based line range, used to populate the function `source` field.
func haskellLineRangeSource(lines []string, startLine int, endLine int) string {
	if startLine <= 0 || endLine < startLine || startLine > len(lines) {
		return ""
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}

// haskellAppendFunctionCalls mines the right-hand side of a binding line for
// bounded call evidence, splitting on the first `=` so the bound name and
// parameters are not treated as calls.
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

// haskellAppendExpressionCalls records bounded call rows for the identifier
// tokens in an expression, skipping the enclosing function name, keywords, and
// bound parameters. Calls are deduplicated per context, function, full name, and
// line.
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

// haskellTokenIsEmbedded reports whether the [start,end) token is part of a
// larger identifier, in which case it is not a standalone call token.
func haskellTokenIsEmbedded(text string, start int, end int) bool {
	if start > 0 && haskellIdentifierByte(text[start-1]) {
		return true
	}
	return end < len(text) && haskellIdentifierByte(text[end])
}

// haskellIdentifierByte reports whether char can appear inside a Haskell
// identifier.
func haskellIdentifierByte(char byte) bool {
	return char == '_' || char == '\'' ||
		(char >= '0' && char <= '9') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= 'a' && char <= 'z')
}

// haskellStripStringsAndLineComment blanks string literals and trailing line
// comments so call-token scanning never matches identifiers inside them.
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

// haskellIsKeyword reports whether name is a Haskell reserved word, so keyword
// tokens never become functions, variables, or call targets.
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
