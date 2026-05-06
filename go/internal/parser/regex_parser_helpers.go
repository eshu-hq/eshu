package parser

import "strings"

type scopedContext struct {
	kind       string
	name       string
	braceDepth int
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

func lastPathSegment(name string, separator string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	segments := strings.Split(trimmed, separator)
	return strings.TrimSpace(segments[len(segments)-1])
}
