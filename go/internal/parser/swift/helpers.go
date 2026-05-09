package swift

import "strings"

func parseInheritanceClause(matches []string, index int) []string {
	if len(matches) <= index {
		return nil
	}
	raw := strings.TrimSpace(matches[index])
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	bases := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		bases = append(bases, trimmed)
	}
	if len(bases) == 0 {
		return nil
	}
	return bases
}

func extractParameters(source string) []string {
	start := strings.Index(source, "(")
	end := strings.LastIndex(source, ")")
	if start == -1 || end == -1 || end <= start+1 {
		return nil
	}
	signature := source[start+1 : end]
	rawParams := strings.Split(signature, ",")
	args := make([]string, 0, len(rawParams))
	for _, rawParam := range rawParams {
		param := strings.TrimSpace(rawParam)
		if param == "" {
			continue
		}
		beforeType := strings.SplitN(param, ":", 2)[0]
		tokens := strings.Fields(beforeType)
		if len(tokens) == 0 {
			continue
		}
		name := tokens[len(tokens)-1]
		if name == "_" && len(tokens) >= 2 {
			name = tokens[len(tokens)-2]
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "_" {
			continue
		}
		args = append(args, name)
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

func extractCallArguments(source string, callName string) []string {
	index := strings.Index(source, callName)
	if index < 0 {
		return nil
	}
	open := strings.Index(source[index+len(callName):], "(")
	if open < 0 {
		return nil
	}
	open += index + len(callName)
	close := strings.LastIndex(source, ")")
	if close <= open {
		return nil
	}
	inside := strings.TrimSpace(source[open+1 : close])
	if inside == "" {
		return []string{}
	}
	parts := strings.Split(inside, ",")
	args := make([]string, 0, len(parts))
	for _, part := range parts {
		arg := strings.TrimSpace(part)
		if arg != "" {
			args = append(args, arg)
		}
	}
	return args
}

func braceDelta(line string) int {
	return strings.Count(line, "{") - strings.Count(line, "}")
}

func currentScopedName(stack []scopedContext, kinds ...string) string {
	for index := len(stack) - 1; index >= 0; index-- {
		for _, kind := range kinds {
			if stack[index].kind == kind {
				return stack[index].name
			}
		}
	}
	return ""
}

func popCompletedScopes(stack []scopedContext, braceDepth int) []scopedContext {
	for len(stack) > 0 && braceDepth < stack[len(stack)-1].braceDepth {
		stack = stack[:len(stack)-1]
	}
	return stack
}
