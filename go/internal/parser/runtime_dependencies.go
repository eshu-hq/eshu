package parser

import "strings"

func extractRuntimeServiceDependencies(source string, workloadName string) []string {
	matches := make([]string, 0)
	seen := make(map[string]struct{})
	remaining := source

	for {
		index := strings.Index(remaining, "services")
		if index < 0 {
			break
		}
		remaining = remaining[index+len("services"):]
		colonIndex := strings.Index(remaining, ":")
		if colonIndex < 0 {
			continue
		}
		remaining = remaining[colonIndex+1:]
		openIndex := strings.Index(remaining, "[")
		if openIndex < 0 {
			continue
		}
		remaining = remaining[openIndex+1:]
		closeIndex := strings.Index(remaining, "]")
		if closeIndex < 0 {
			break
		}

		body := remaining[:closeIndex]
		remaining = remaining[closeIndex+1:]
		for _, value := range extractQuotedStrings(body) {
			dependencyName := normalizeRuntimeDependencyName(value, workloadName)
			if dependencyName == "" {
				continue
			}
			if _, ok := seen[dependencyName]; ok {
				continue
			}
			seen[dependencyName] = struct{}{}
			matches = append(matches, dependencyName)
		}
	}

	return matches
}

func extractQuotedStrings(body string) []string {
	values := make([]string, 0)
	var (
		quote  rune
		buffer strings.Builder
	)

	for _, r := range body {
		switch {
		case quote == 0 && (r == '\'' || r == '"' || r == '`'):
			quote = r
			buffer.Reset()
		case quote != 0 && r == quote:
			values = append(values, buffer.String())
			quote = 0
		case quote != 0:
			if r == '\\' {
				continue
			}
			buffer.WriteRune(r)
		}
	}

	return values
}

func normalizeRuntimeDependencyName(value string, workloadName string) string {
	candidate := strings.TrimSpace(value)
	if candidate == "" || strings.Contains(candidate, "${") {
		return ""
	}
	candidate = strings.TrimPrefix(candidate, "/api/")
	if candidate == workloadName {
		return ""
	}
	if strings.Contains(candidate, "/") {
		return ""
	}
	switch candidate {
	case "aws", "elastic", "elasticache":
		return ""
	}
	if !isRuntimeServiceName(candidate) {
		return ""
	}
	return candidate
}

func isRuntimeServiceName(value string) bool {
	if value == "" {
		return false
	}
	if value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, r := range value[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}
