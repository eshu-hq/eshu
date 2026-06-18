package dart

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func appendDartCalls(payload map[string]any, seen map[string]struct{}, source []byte) {
	line := 1
	for index := 0; index < len(source); {
		if next, nextLine, ok := skipDartComment(source, index, line); ok {
			index = next
			line = nextLine
			continue
		}
		if next, nextLine, ok := skipDartStringLiteral(source, index, line); ok {
			index = next
			line = nextLine
			continue
		}
		character, width := utf8.DecodeRune(source[index:])
		if character == '\n' {
			line++
			index += width
			continue
		}
		if !isDartIdentifierStart(character) {
			index += width
			continue
		}
		start := index
		index += width
		for index < len(source) {
			next, nextWidth := utf8.DecodeRune(source[index:])
			if !isDartIdentifierPart(next) {
				break
			}
			index += nextWidth
		}
		name := string(source[start:index])
		after := skipDartWhitespace(source, index)
		if after >= len(source) || source[after] != '(' || dartCallKeyword(name) {
			continue
		}
		appendUniqueDartCall(payload, seen, name, line)
	}
}

func skipDartComment(source []byte, index int, line int) (int, int, bool) {
	if index+1 >= len(source) || source[index] != '/' {
		return index, line, false
	}
	switch source[index+1] {
	case '/':
		index += 2
		for index < len(source) {
			character, width := utf8.DecodeRune(source[index:])
			index += width
			if character == '\n' {
				return index, line + 1, true
			}
		}
		return index, line, true
	case '*':
		index += 2
		for index < len(source) {
			if index+1 < len(source) && source[index] == '*' && source[index+1] == '/' {
				return index + 2, line, true
			}
			character, width := utf8.DecodeRune(source[index:])
			index += width
			if character == '\n' {
				line++
			}
		}
		return index, line, true
	default:
		return index, line, false
	}
}

func skipDartStringLiteral(source []byte, index int, line int) (int, int, bool) {
	quote := source[index]
	if quote != '\'' && quote != '"' {
		return index, line, false
	}
	index++
	triple := false
	if index+1 < len(source) && source[index] == quote && source[index+1] == quote {
		triple = true
		index += 2
	}
	for index < len(source) {
		if !triple && source[index] == '\\' {
			index++
			if index < len(source) {
				_, width := utf8.DecodeRune(source[index:])
				index += width
			}
			continue
		}
		character, width := utf8.DecodeRune(source[index:])
		if character == '\n' {
			line++
		}
		if source[index] == quote {
			if !triple {
				return index + width, line, true
			}
			if index+2 < len(source) && source[index+1] == quote && source[index+2] == quote {
				return index + 3, line, true
			}
		}
		index += width
	}
	return index, line, true
}

func skipDartWhitespace(source []byte, index int) int {
	for index < len(source) {
		character, width := utf8.DecodeRune(source[index:])
		if !unicode.IsSpace(character) {
			return index
		}
		index += width
	}
	return index
}

func isDartIdentifierStart(character rune) bool {
	return character == '_' || unicode.IsLetter(character)
}

func isDartIdentifierPart(character rune) bool {
	return character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character)
}

func dartCallKeyword(name string) bool {
	switch name {
	case "assert", "catch", "for", "if", "switch", "while":
		return true
	default:
		return false
	}
}

func appendUniqueDartCall(payload map[string]any, seen map[string]struct{}, fullName string, lineNumber int) {
	if strings.TrimSpace(fullName) == "" {
		return
	}
	if _, ok := seen[fullName]; ok {
		return
	}
	seen[fullName] = struct{}{}
	shared.AppendBucket(payload, "function_calls", map[string]any{
		"name":        fullName,
		"full_name":   fullName,
		"line_number": lineNumber,
		"lang":        "dart",
	})
}
