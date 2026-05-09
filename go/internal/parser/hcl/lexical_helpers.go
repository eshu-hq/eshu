package hcl

import (
	"path"
	"strings"
)

func stripHCLInlineComments(expression string) string {
	lines := strings.Split(expression, "\n")
	for index, line := range lines {
		lines[index] = stripHCLLineComment(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func stripHCLLineComment(line string) string {
	inString := false
	escaped := false
	for index := 0; index < len(line); index++ {
		switch {
		case escaped:
			escaped = false
		case line[index] == '\\' && inString:
			escaped = true
		case line[index] == '"':
			inString = !inString
		case !inString && line[index] == '#':
			return strings.TrimSpace(line[:index])
		case !inString && line[index] == '/' && index+1 < len(line) && line[index+1] == '/':
			return strings.TrimSpace(line[:index])
		}
	}
	return strings.TrimSpace(line)
}

func countBracesOutsideStrings(line string) int {
	depth := 0
	inString := false
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			escaped = false
			continue
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case !inString && r == '{':
			depth++
		case !inString && r == '}':
			depth--
		}
	}
	return depth
}

func countParensOutsideStrings(line string) int {
	depth := 0
	inString := false
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case !inString && r == '(':
			depth++
		case !inString && r == ')':
			depth--
		}
	}
	return depth
}

func cleanRepositoryRelativePath(relativePath string) string {
	relativePath = path.Clean(strings.TrimSpace(relativePath))
	switch relativePath {
	case "", ".", "/":
		return ""
	default:
		return strings.TrimPrefix(relativePath, "./")
	}
}
