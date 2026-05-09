package hcl

import (
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	localConfigFunctionStartPattern   = regexp.MustCompile(`(?i)\b(?:file|templatefile)\(`)
	localStringAssignmentPattern      = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_]+)\s*=\s*"([^"]+)"\s*$`)
	localAssignmentStartPattern       = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s*=\s*(.+?)\s*$`)
	pathRelativeToIncludeSplitPattern = regexp.MustCompile(`(?is)^split\(\s*"/"\s*,\s*path_relative_to_include\(\s*(?:"[^"]+")?\s*\)\s*\)$`)
	quotedStringPattern               = regexp.MustCompile(`"([^"]+)"`)
	localInterpolationPattern         = regexp.MustCompile(`\$\{local\.([A-Za-z0-9_]+)\}`)
)

func extractTerragruntLocalConfigAssetPaths(content string, localAssignments map[string]string) []string {
	matches := localConfigFunctionStartPattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		openParenIndex := match[1] - 1
		arguments, ok := extractDelimitedContent(content, openParenIndex, '(', ')')
		if !ok {
			continue
		}
		firstArgument := firstTopLevelArgument(arguments)
		if firstArgument == "" {
			continue
		}
		normalized := extractTerragruntConfigAssetPathFromExpression(firstArgument, localAssignments)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		paths = append(paths, normalized)
	}
	sort.Strings(paths)
	return paths
}

func collectTerragruntLocalAssignments(content string, relativePath string) map[string]string {
	assignments := make(map[string]string)
	lines := strings.Split(content, "\n")
	inLocals := false
	depth := 0
	currentName := ""
	currentExpression := strings.Builder{}
	currentParenDepth := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inLocals {
			if !strings.HasPrefix(trimmed, "locals") || !strings.Contains(trimmed, "{") {
				continue
			}
			inLocals = true
			depth = countBracesOutsideStrings(line)
			if depth <= 0 {
				inLocals = false
				depth = 0
			}
			continue
		}

		if currentName == "" {
			if assignment := localStringAssignmentPattern.FindStringSubmatch(line); len(assignment) >= 3 {
				name := strings.TrimSpace(assignment[1])
				value := strings.TrimSpace(assignment[2])
				if name != "" && value != "" {
					assignments[name] = value
				}
			} else if assignment := localAssignmentStartPattern.FindStringSubmatch(line); len(assignment) >= 3 {
				currentName = strings.TrimSpace(assignment[1])
				currentExpression.Reset()
				currentExpression.WriteString(strings.TrimSpace(assignment[2]))
				currentParenDepth = countParensOutsideStrings(assignment[2])
				if currentParenDepth <= 0 {
					storeTerragruntLocalAssignment(assignments, currentName, currentExpression.String(), relativePath)
					currentName = ""
				}
			}
		} else {
			if trimmed != "" {
				currentExpression.WriteString("\n")
				currentExpression.WriteString(trimmed)
			}
			currentParenDepth += countParensOutsideStrings(line)
			if currentParenDepth <= 0 {
				storeTerragruntLocalAssignment(assignments, currentName, currentExpression.String(), relativePath)
				currentName = ""
			}
		}

		depth += countBracesOutsideStrings(line)
		if depth <= 0 {
			if currentName != "" {
				storeTerragruntLocalAssignment(assignments, currentName, currentExpression.String(), relativePath)
				currentName = ""
			}
			inLocals = false
			depth = 0
		}
	}
	return assignments
}

func storeTerragruntLocalAssignment(assignments map[string]string, name, expression, relativePath string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if indexedAssignments := extractPathRelativeToIncludeIndexedAssignments(name, expression, relativePath); len(indexedAssignments) > 0 {
		for key, value := range indexedAssignments {
			assignments[key] = value
		}
		return
	}
	if value := normalizeTerragruntAssignmentValue(expression, assignments); value != "" {
		assignments[name] = value
	}
}

func extractPathRelativeToIncludeIndexedAssignments(name, expression, relativePath string) map[string]string {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" || !pathRelativeToIncludeSplitPattern.MatchString(stripHCLInlineComments(expression)) {
		return nil
	}

	directory := cleanRepositoryRelativePath(path.Dir(strings.TrimSpace(relativePath)))
	if directory == "" || directory == "." {
		return nil
	}

	parts := strings.Split(directory, "/")
	assignments := make(map[string]string, len(parts))
	for index, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		assignments[trimmedName+"["+strconv.Itoa(index)+"]"] = part
	}
	return assignments
}

func normalizeTerragruntAssignmentValue(expression string, localAssignments map[string]string) string {
	trimmed := strings.TrimSpace(stripHCLInlineComments(expression))
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "local.") {
		localName := strings.TrimSpace(strings.TrimPrefix(trimmed, "local."))
		if localName == "" {
			return ""
		}
		return normalizeTerragruntAssignmentValue(localAssignments[localName], localAssignments)
	}
	if quoted := exactQuotedString(trimmed); quoted != "" {
		if strings.Contains(quoted, "${") {
			if resolved := normalizeLocalConfigAssetPathValue(quoted, localAssignments); resolved != "" {
				return resolved
			}
		}
		return quoted
	}
	if value := extractTerragruntConfigAssetPathFromExpression(trimmed, localAssignments); value != "" {
		return value
	}
	if !strings.ContainsAny(trimmed, " \t\n\r()[]{}?,:=#") {
		return trimmed
	}
	return ""
}

func extractTerragruntConfigAssetPathFromExpression(expression string, localAssignments map[string]string) string {
	trimmed := strings.TrimSpace(stripHCLInlineComments(expression))
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "find_in_parent_folders(") || strings.Contains(trimmed, "read_terragrunt_config(") {
		return ""
	}
	if value := extractTerragruntJoinedConfigAssetPath(trimmed, localAssignments); value != "" {
		return value
	}
	if strings.HasPrefix(trimmed, "local.") {
		localName := strings.TrimSpace(strings.TrimPrefix(trimmed, "local."))
		if localName == "" {
			return ""
		}
		return normalizeTerragruntAssignmentValue(localAssignments[localName], localAssignments)
	}
	for _, match := range quotedStringPattern.FindAllStringSubmatch(trimmed, -1) {
		if len(match) < 2 {
			continue
		}
		if value := normalizeTerragruntConfigAssetLiteral(match[1], localAssignments); value != "" {
			return value
		}
	}
	return ""
}

func extractTerragruntJoinedConfigAssetPath(expression string, localAssignments map[string]string) string {
	trimmed := strings.TrimSpace(expression)
	if !strings.HasPrefix(strings.ToLower(trimmed), "join(") {
		return ""
	}

	openParenIndex := strings.Index(trimmed, "(")
	arguments, ok := extractDelimitedContent(trimmed, openParenIndex, '(', ')')
	if !ok {
		return ""
	}

	parts := splitTopLevelCommaSeparated(arguments)
	if len(parts) != 2 {
		return ""
	}

	arrayExpression := strings.TrimSpace(parts[1])
	if arrayExpression == "" || !strings.HasPrefix(arrayExpression, "[") {
		return ""
	}
	openBracketIndex := strings.Index(arrayExpression, "[")
	listContent, ok := extractDelimitedContent(arrayExpression, openBracketIndex, '[', ']')
	if !ok {
		return ""
	}

	segments := make([]string, 0)
	for _, item := range splitTopLevelCommaSeparated(listContent) {
		resolved := extractTerragruntConfigAssetPathFromExpression(item, localAssignments)
		if resolved == "" {
			continue
		}
		segments = append(segments, resolved)
	}
	if len(segments) == 0 {
		return ""
	}

	separator := strings.TrimSpace(stripHCLInlineComments(parts[0]))
	separator = strings.Trim(separator, "\"")
	return normalizeLocalConfigAssetPathValue(strings.Join(segments, separator), localAssignments)
}

func normalizeTerragruntConfigAssetLiteral(value string, localAssignments map[string]string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if !strings.Contains(trimmed, "/") &&
		!strings.HasSuffix(lower, ".yaml") &&
		!strings.HasSuffix(lower, ".yml") &&
		!strings.HasSuffix(lower, ".json") &&
		!strings.HasSuffix(lower, ".tpl") &&
		!strings.HasSuffix(lower, ".tmpl") &&
		!strings.HasSuffix(lower, ".hcl") &&
		!strings.HasSuffix(lower, ".tf") {
		return ""
	}
	return normalizeLocalConfigAssetPathValue(trimmed, localAssignments)
}

func exactQuotedString(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 || trimmed[0] != '"' || trimmed[len(trimmed)-1] != '"' {
		return ""
	}
	return trimmed[1 : len(trimmed)-1]
}

func normalizeLocalConfigAssetPathValue(value string, localAssignments map[string]string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") ||
		strings.HasPrefix(strings.ToLower(value), "git::") ||
		strings.HasPrefix(strings.ToLower(value), "tfr:///") {
		return ""
	}
	value = resolveLocalInterpolations(value, localAssignments)
	replacer := strings.NewReplacer(
		"${path.module}/", "",
		"${path_relative_to_include()}/", "",
		"${path_relative_to_include()}", "",
		"${get_repo_root()}/", "",
		"${get_parent_terragrunt_dir()}/", "",
		"${get_terragrunt_dir()}/", "",
	)
	value = replacer.Replace(value)
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	value = strings.TrimSpace(value)
	if value == "" || value == "." {
		return ""
	}
	return cleanRepositoryRelativePath(value)
}

func resolveLocalInterpolations(value string, localAssignments map[string]string) string {
	if len(localAssignments) == 0 {
		return value
	}
	resolved := value
	for range 5 {
		changed := false
		resolved = localInterpolationPattern.ReplaceAllStringFunc(resolved, func(match string) string {
			submatch := localInterpolationPattern.FindStringSubmatch(match)
			if len(submatch) < 2 {
				return match
			}
			replacement := strings.TrimSpace(localAssignments[submatch[1]])
			if replacement == "" {
				return match
			}
			changed = true
			return replacement
		})
		if !changed {
			break
		}
	}
	return resolved
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
