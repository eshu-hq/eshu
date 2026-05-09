package ruby

import (
	"regexp"
	"strings"
)

var (
	rubyChainedCallPattern   = regexp.MustCompile(`(?:^|[^A-Za-z0-9_:@])((?:[A-Za-z_]\w*|@[A-Za-z_]\w*|self|[A-Z][A-Za-z0-9_:]*)(?:\.[A-Za-z_]\w*[!?=]?)+)\(([^()]*)\)\.([A-Za-z_]\w*[!?=]?)(?:\s*\(([^)]*)\)|\s+([^#]+))?`)
	rubyScopedCallPattern    = regexp.MustCompile(`([A-Z][A-Za-z0-9_:]*\.[A-Za-z_]\w*[!?=]?)\(`)
	rubyQualifiedCallPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9_:@])((?:[A-Za-z_]\w*|@[A-Za-z_]\w*|self|[A-Z][A-Za-z0-9_:]*)(?:\.[A-Za-z_]\w*)+[!?=]?)(?:\s*\(|\b|[\s;])`)
	rubyBareCallPattern      = regexp.MustCompile(`(?:^|[^A-Za-z0-9_:@])((?:require_relative|require|load|include|extend|attr_accessor|attr_reader|attr_writer|define_method|define_singleton_method|instance_method|instance_eval|cache_method|puts|sleep|method|public_send|send|super|bind))(?:\s*\(([^)]*)\)|\s+([^#]+))`)
)

type rubyCallMatch struct {
	name     string
	fullName string
	args     string
}

func rubyParseCalls(line string) []rubyCallMatch {
	calls := make([]rubyCallMatch, 0)
	seen := make(map[string]struct{})

	for _, matches := range rubyChainedCallPattern.FindAllStringSubmatch(line, -1) {
		if len(matches) < 4 {
			continue
		}
		receiver := strings.TrimSpace(matches[1])
		methodName := strings.TrimSpace(matches[3])
		if receiver == "" || methodName == "" {
			continue
		}
		fullName := receiver + "." + methodName
		if _, ok := seen[fullName]; ok {
			continue
		}
		argsText := ""
		switch {
		case len(matches) >= 5 && strings.TrimSpace(matches[4]) != "":
			argsText = matches[4]
		case len(matches) >= 6 && strings.TrimSpace(matches[5]) != "":
			argsText = matches[5]
		}
		seen[fullName] = struct{}{}
		calls = append(calls, rubyCallMatch{
			name:     rubyCallName(fullName),
			fullName: fullName,
			args:     argsText,
		})
	}

	for _, matches := range rubyScopedCallPattern.FindAllStringSubmatch(line, -1) {
		if len(matches) != 2 {
			continue
		}
		fullName := strings.TrimSpace(matches[1])
		if fullName == "" {
			continue
		}
		fullName = rubyRestoreCallPunctuation(line, fullName)
		if _, ok := seen[fullName]; ok {
			continue
		}
		seen[fullName] = struct{}{}
		calls = append(calls, rubyCallMatch{
			name:     rubyCallName(fullName),
			fullName: fullName,
		})
	}

	for _, matches := range rubyQualifiedCallPattern.FindAllStringSubmatch(line, -1) {
		if len(matches) != 2 {
			continue
		}
		fullName := strings.TrimSpace(matches[1])
		if fullName == "" {
			continue
		}
		fullName = rubyRestoreCallPunctuation(line, fullName)
		if _, ok := seen[fullName]; ok {
			continue
		}
		seen[fullName] = struct{}{}
		calls = append(calls, rubyCallMatch{
			name:     rubyCallName(fullName),
			fullName: fullName,
		})
	}

	for _, matches := range rubyBareCallPattern.FindAllStringSubmatch(line, -1) {
		if len(matches) < 2 {
			continue
		}
		fullName := strings.TrimSpace(matches[1])
		if fullName == "" {
			continue
		}
		if _, ok := seen[fullName]; ok {
			continue
		}
		argsText := ""
		switch {
		case len(matches) >= 3 && strings.TrimSpace(matches[2]) != "":
			argsText = matches[2]
		case len(matches) >= 4 && strings.TrimSpace(matches[3]) != "":
			argsText = matches[3]
		}
		seen[fullName] = struct{}{}
		calls = append(calls, rubyCallMatch{
			name:     rubyCallName(fullName),
			fullName: fullName,
			args:     argsText,
		})
	}

	return calls
}

func rubyCallName(fullName string) string {
	trimmed := strings.TrimSpace(fullName)
	if trimmed == "" {
		return ""
	}
	if index := strings.LastIndex(trimmed, "."); index >= 0 {
		return trimmed[index+1:]
	}
	if index := strings.LastIndex(trimmed, "::"); index >= 0 {
		return trimmed[index+2:]
	}
	return trimmed
}

// rubyRestoreCallPunctuation preserves Ruby predicate, bang, and writer method
// suffixes when the line-oriented call pattern also matched the bare name.
func rubyRestoreCallPunctuation(line string, fullName string) string {
	if strings.HasSuffix(fullName, "?") || strings.HasSuffix(fullName, "!") || strings.HasSuffix(fullName, "=") {
		return fullName
	}
	for _, suffix := range []string{"?", "!", "="} {
		if strings.Contains(line, fullName+suffix) {
			return fullName + suffix
		}
	}
	return fullName
}

func rubyInferAssignmentType(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "Unknown"
	}
	if index := strings.Index(trimmed, "="); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[index+1:])
	}
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "new "))
	if trimmed == "" {
		return "Unknown"
	}
	return trimmed
}

func rubyParseArguments(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}
	}
	segments := strings.Split(trimmed, ",")
	args := make([]string, 0, len(segments))
	for _, segment := range segments {
		arg := rubyNormalizeArgument(segment)
		if arg == "" {
			continue
		}
		args = append(args, arg)
	}
	return args
}

func rubyNormalizeArgument(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "&")
	trimmed = strings.TrimPrefix(trimmed, "*")
	trimmed = strings.TrimPrefix(trimmed, ":")
	if index := strings.Index(trimmed, "="); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[:index])
	}
	if index := strings.Index(trimmed, ":"); index >= 0 && !strings.Contains(trimmed, "://") {
		if strings.Count(trimmed, ":") == 1 {
			trimmed = strings.TrimSpace(trimmed[:index])
		}
	}
	if len(trimmed) >= 2 {
		if (trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') || (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') {
			trimmed = trimmed[1 : len(trimmed)-1]
		}
	}
	return trimmed
}
