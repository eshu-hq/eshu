package query

import "strings"

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

func extractDelimitedContent(content string, openIndex int, openRune, closeRune rune) (string, bool) {
	if openIndex < 0 || openIndex >= len(content) || rune(content[openIndex]) != openRune {
		return "", false
	}
	depth := 1
	inString := false
	escaped := false
	for index := openIndex + 1; index < len(content); index++ {
		r := rune(content[index])
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case !inString && r == openRune:
			depth++
		case !inString && r == closeRune:
			depth--
			if depth == 0 {
				return content[openIndex+1 : index], true
			}
		}
	}
	return "", false
}

func firstTopLevelArgument(arguments string) string {
	inString := false
	escaped := false
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for index, r := range arguments {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case inString:
			continue
		case r == '(':
			parenDepth++
		case r == ')':
			parenDepth--
		case r == '[':
			bracketDepth++
		case r == ']':
			bracketDepth--
		case r == '{':
			braceDepth++
		case r == '}':
			braceDepth--
		case r == ',' && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0:
			return strings.TrimSpace(arguments[:index])
		}
	}
	return strings.TrimSpace(arguments)
}

func splitTopLevelCommaSeparated(expression string) []string {
	items := make([]string, 0)
	inString := false
	escaped := false
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	start := 0
	for index, r := range expression {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case inString:
			continue
		case r == '(':
			parenDepth++
		case r == ')':
			parenDepth--
		case r == '[':
			bracketDepth++
		case r == ']':
			bracketDepth--
		case r == '{':
			braceDepth++
		case r == '}':
			braceDepth--
		case r == ',' && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0:
			items = append(items, strings.TrimSpace(expression[start:index]))
			start = index + 1
		}
	}
	if start <= len(expression) {
		items = append(items, strings.TrimSpace(expression[start:]))
	}
	return items
}

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
