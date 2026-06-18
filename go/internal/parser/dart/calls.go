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
