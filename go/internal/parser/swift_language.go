package parser

import (
	"regexp"
	"slices"
	"strings"
)

var (
	swiftImportPattern       = regexp.MustCompile(`^\s*import\s+([A-Za-z0-9_\.]+)`)
	swiftClassPattern        = regexp.MustCompile(`^\s*(?:final\s+)?class\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	swiftActorPattern        = regexp.MustCompile(`^\s*actor\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	swiftStructPattern       = regexp.MustCompile(`^\s*struct\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	swiftEnumPattern         = regexp.MustCompile(`^\s*enum\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	swiftProtocolPattern     = regexp.MustCompile(`^\s*protocol\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	swiftFunctionPattern     = regexp.MustCompile(`\bfunc\s+([A-Za-z_]\w*)(?:<[^>]+>)?\s*\(`)
	swiftVariablePattern     = regexp.MustCompile(`^\s*(?:let|var)\s+([A-Za-z_]\w*)(?:\s*:\s*([^=<{]+(?:<[^>]+>)?))?`)
	swiftReceiverCallPattern = regexp.MustCompile(`\b([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*\(`)
	swiftCallPattern         = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`)
)

func (e *Engine) parseSwift(path string, isDependency bool, options Options) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}

	payload := basePayload(path, "swift", isDependency)
	payload["structs"] = []map[string]any{}
	payload["enums"] = []map[string]any{}
	payload["protocols"] = []map[string]any{}

	lines := strings.Split(string(source), "\n")
	braceDepth := 0
	stack := make([]scopedContext, 0)
	seenVariables := make(map[string]struct{})
	variableTypes := make(map[string]string)
	seenCalls := make(map[string]struct{})

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			braceDepth += braceDelta(rawLine)
			stack = popCompletedScopes(stack, braceDepth)
			continue
		}

		if matches := swiftImportPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendBucket(payload, "imports", map[string]any{
				"name":             matches[1],
				"full_import_name": matches[1],
				"alias":            nil,
				"context":          nil,
				"is_dependency":    isDependency,
				"line_number":      lineNumber,
				"lang":             "swift",
			})
		}
		if matches := swiftClassPattern.FindStringSubmatch(trimmed); len(matches) >= 2 {
			name := matches[1]
			appendBucket(payload, "classes", map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"bases":       parseSwiftInheritanceClause(matches, 2),
				"lang":        "swift",
			})
			stack = append(stack, scopedContext{kind: "class", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := swiftActorPattern.FindStringSubmatch(trimmed); len(matches) >= 2 {
			name := matches[1]
			appendBucket(payload, "classes", map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"bases":       parseSwiftInheritanceClause(matches, 2),
				"lang":        "swift",
			})
			stack = append(stack, scopedContext{kind: "class", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := swiftStructPattern.FindStringSubmatch(trimmed); len(matches) >= 2 {
			name := matches[1]
			appendBucket(payload, "structs", map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"bases":       parseSwiftInheritanceClause(matches, 2),
				"lang":        "swift",
			})
			stack = append(stack, scopedContext{kind: "struct", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := swiftEnumPattern.FindStringSubmatch(trimmed); len(matches) >= 2 {
			name := matches[1]
			appendBucket(payload, "enums", map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"bases":       parseSwiftInheritanceClause(matches, 2),
				"lang":        "swift",
			})
			stack = append(stack, scopedContext{kind: "enum", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := swiftProtocolPattern.FindStringSubmatch(trimmed); len(matches) >= 2 {
			name := matches[1]
			appendBucket(payload, "protocols", map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"bases":       parseSwiftInheritanceClause(matches, 2),
				"lang":        "swift",
			})
			stack = append(stack, scopedContext{kind: "protocol", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}

		if matches := swiftFunctionPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendSwiftFunction(payload, matches[1], rawLine, lineNumber, options, currentScopedName(stack, "class", "struct", "enum"))
		}
		if strings.HasPrefix(trimmed, "init(") || strings.Contains(trimmed, " init(") {
			appendSwiftFunction(payload, "init", rawLine, lineNumber, options, currentScopedName(stack, "class", "struct"))
		}

		if matches := swiftVariablePattern.FindStringSubmatch(trimmed); len(matches) >= 2 {
			name := matches[1]
			if _, ok := seenVariables[name]; !ok {
				seenVariables[name] = struct{}{}
				contextName := currentScopedName(stack, "class", "struct", "enum")
				varType := ""
				if len(matches) >= 3 {
					varType = strings.TrimSpace(matches[2])
				}
				appendBucket(payload, "variables", map[string]any{
					"name":          name,
					"type":          varType,
					"context":       contextName,
					"class_context": contextName,
					"line_number":   lineNumber,
					"end_line":      lineNumber,
					"lang":          "swift",
				})
				variableTypes[name] = varType
			}
		}

		for _, match := range swiftReceiverCallPattern.FindAllStringSubmatch(trimmed, -1) {
			if len(match) != 3 {
				continue
			}
			receiver := match[1]
			name := match[2]
			fullName := receiver + "." + name
			callKey := fullName + ":" + trimmed
			if _, ok := seenCalls[callKey]; ok {
				continue
			}
			seenCalls[callKey] = struct{}{}
			appendBucket(payload, "function_calls", map[string]any{
				"name":              name,
				"full_name":         fullName,
				"line_number":       lineNumber,
				"args":              extractSwiftCallArguments(trimmed, fullName),
				"inferred_obj_type": variableTypes[receiver],
				"lang":              "swift",
				"is_dependency":     isDependency,
			})
		}
		for _, match := range swiftCallPattern.FindAllStringSubmatchIndex(trimmed, -1) {
			if len(match) != 4 {
				continue
			}
			if match[0] > 0 && trimmed[match[0]-1] == '.' {
				continue
			}
			if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "init(") {
				continue
			}
			name := trimmed[match[2]:match[3]]
			switch name {
			case "func", "init", "if", "switch", "return":
				continue
			}
			callKey := name + ":" + trimmed
			if _, ok := seenCalls[callKey]; ok {
				continue
			}
			seenCalls[callKey] = struct{}{}
			appendBucket(payload, "function_calls", map[string]any{
				"name":          name,
				"full_name":     name,
				"line_number":   lineNumber,
				"args":          extractSwiftCallArguments(trimmed, name),
				"lang":          "swift",
				"is_dependency": isDependency,
			})
		}

		braceDepth += braceDelta(rawLine)
		stack = popCompletedScopes(stack, braceDepth)
	}

	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "classes")
	sortNamedBucket(payload, "structs")
	sortNamedBucket(payload, "enums")
	sortNamedBucket(payload, "protocols")
	sortNamedBucket(payload, "variables")
	sortNamedBucket(payload, "imports")
	sortNamedBucket(payload, "function_calls")

	return payload, nil
}

func (e *Engine) preScanSwift(path string) ([]string, error) {
	payload, err := e.parseSwift(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "classes", "structs", "enums", "protocols")
	slices.Sort(names)
	return names, nil
}

func appendSwiftFunction(
	payload map[string]any,
	name string,
	source string,
	lineNumber int,
	options Options,
	classContext string,
) {
	args := extractSwiftParameters(source)
	item := map[string]any{
		"name":        name,
		"args":        args,
		"context":     classContext,
		"line_number": lineNumber,
		"end_line":    lineNumber,
		"lang":        "swift",
		"decorators":  []string{},
	}
	if classContext != "" {
		item["class_context"] = classContext
	}
	if options.IndexSource {
		item["source"] = source
	}
	appendBucket(payload, "functions", item)
}

func parseSwiftInheritanceClause(matches []string, index int) []string {
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

func extractSwiftParameters(source string) []string {
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

func extractSwiftCallArguments(source string, callName string) []string {
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
