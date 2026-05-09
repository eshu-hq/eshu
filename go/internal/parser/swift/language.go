package swift

import (
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	importPattern       = regexp.MustCompile(`^\s*import\s+([A-Za-z0-9_\.]+)`)
	classPattern        = regexp.MustCompile(`^\s*(?:final\s+)?class\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	actorPattern        = regexp.MustCompile(`^\s*actor\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	structPattern       = regexp.MustCompile(`^\s*struct\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	enumPattern         = regexp.MustCompile(`^\s*enum\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	protocolPattern     = regexp.MustCompile(`^\s*protocol\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	functionPattern     = regexp.MustCompile(`\bfunc\s+([A-Za-z_]\w*)(?:<[^>]+>)?\s*\(`)
	variablePattern     = regexp.MustCompile(`^\s*(?:let|var)\s+([A-Za-z_]\w*)(?:\s*:\s*([^=<{]+(?:<[^>]+>)?))?`)
	receiverCallPattern = regexp.MustCompile(`\b([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*\(`)
	callPattern         = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`)
)

type scopedContext struct {
	kind       string
	name       string
	braceDepth int
}

// Parse extracts Swift imports, types, functions, variables, and calls.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "swift", isDependency)
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

		appendImport(payload, trimmed, lineNumber, isDependency)
		stack = appendTypes(payload, stack, trimmed, rawLine, lineNumber, braceDepth)
		appendFunctions(payload, stack, trimmed, rawLine, lineNumber, options)
		appendVariable(payload, stack, trimmed, lineNumber, seenVariables, variableTypes)
		appendCalls(payload, trimmed, lineNumber, variableTypes, seenCalls, isDependency)

		braceDepth += braceDelta(rawLine)
		stack = popCompletedScopes(stack, braceDepth)
	}

	for _, bucket := range []string{"functions", "classes", "structs", "enums", "protocols", "variables", "imports", "function_calls"} {
		shared.SortNamedBucket(payload, bucket)
	}

	return payload, nil
}

// PreScan returns Swift names used by the collector import-map pre-scan.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes", "structs", "enums", "protocols")
	slices.Sort(names)
	return names, nil
}

func appendImport(payload map[string]any, trimmed string, lineNumber int, isDependency bool) {
	if matches := importPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		shared.AppendBucket(payload, "imports", map[string]any{
			"name":             matches[1],
			"full_import_name": matches[1],
			"alias":            nil,
			"context":          nil,
			"is_dependency":    isDependency,
			"line_number":      lineNumber,
			"lang":             "swift",
		})
	}
}

func appendTypes(
	payload map[string]any,
	stack []scopedContext,
	trimmed string,
	rawLine string,
	lineNumber int,
	braceDepth int,
) []scopedContext {
	typePatterns := []struct {
		pattern *regexp.Regexp
		bucket  string
		kind    string
	}{
		{pattern: classPattern, bucket: "classes", kind: "class"},
		{pattern: actorPattern, bucket: "classes", kind: "class"},
		{pattern: structPattern, bucket: "structs", kind: "struct"},
		{pattern: enumPattern, bucket: "enums", kind: "enum"},
		{pattern: protocolPattern, bucket: "protocols", kind: "protocol"},
	}
	for _, typed := range typePatterns {
		if matches := typed.pattern.FindStringSubmatch(trimmed); len(matches) >= 2 {
			name := matches[1]
			shared.AppendBucket(payload, typed.bucket, map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"bases":       parseInheritanceClause(matches, 2),
				"lang":        "swift",
			})
			return append(stack, scopedContext{
				kind:       typed.kind,
				name:       name,
				braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")),
			})
		}
	}
	return stack
}

func appendFunctions(payload map[string]any, stack []scopedContext, trimmed string, rawLine string, lineNumber int, options shared.Options) {
	if matches := functionPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		appendFunction(payload, matches[1], rawLine, lineNumber, options, currentScopedName(stack, "class", "struct", "enum"))
	}
	if strings.HasPrefix(trimmed, "init(") || strings.Contains(trimmed, " init(") {
		appendFunction(payload, "init", rawLine, lineNumber, options, currentScopedName(stack, "class", "struct"))
	}
}

func appendFunction(payload map[string]any, name string, source string, lineNumber int, options shared.Options, classContext string) {
	args := extractParameters(source)
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
	shared.AppendBucket(payload, "functions", item)
}

func appendVariable(
	payload map[string]any,
	stack []scopedContext,
	trimmed string,
	lineNumber int,
	seenVariables map[string]struct{},
	variableTypes map[string]string,
) {
	matches := variablePattern.FindStringSubmatch(trimmed)
	if len(matches) < 2 {
		return
	}
	name := matches[1]
	if _, ok := seenVariables[name]; ok {
		return
	}
	seenVariables[name] = struct{}{}
	contextName := currentScopedName(stack, "class", "struct", "enum")
	varType := ""
	if len(matches) >= 3 {
		varType = strings.TrimSpace(matches[2])
	}
	shared.AppendBucket(payload, "variables", map[string]any{
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

func appendCalls(payload map[string]any, trimmed string, lineNumber int, variableTypes map[string]string, seenCalls map[string]struct{}, isDependency bool) {
	for _, match := range receiverCallPattern.FindAllStringSubmatch(trimmed, -1) {
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
		shared.AppendBucket(payload, "function_calls", map[string]any{
			"name":              name,
			"full_name":         fullName,
			"line_number":       lineNumber,
			"args":              extractCallArguments(trimmed, fullName),
			"inferred_obj_type": variableTypes[receiver],
			"lang":              "swift",
			"is_dependency":     isDependency,
		})
	}
	appendPlainCalls(payload, trimmed, lineNumber, seenCalls, isDependency)
}

func appendPlainCalls(payload map[string]any, trimmed string, lineNumber int, seenCalls map[string]struct{}, isDependency bool) {
	for _, match := range callPattern.FindAllStringSubmatchIndex(trimmed, -1) {
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
		shared.AppendBucket(payload, "function_calls", map[string]any{
			"name":          name,
			"full_name":     name,
			"line_number":   lineNumber,
			"args":          extractCallArguments(trimmed, name),
			"lang":          "swift",
			"is_dependency": isDependency,
		})
	}
}
