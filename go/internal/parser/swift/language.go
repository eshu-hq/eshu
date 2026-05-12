package swift

import (
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	importPattern       = regexp.MustCompile(`^\s*import\s+([A-Za-z0-9_\.]+)`)
	classPattern        = regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|open|final)\s+)*class\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	actorPattern        = regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|open|final)\s+)*actor\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	structPattern       = regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|open|final)\s+)*struct\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	enumPattern         = regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|open|final)\s+)*enum\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	protocolPattern     = regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|open|final)\s+)*protocol\s+([A-Za-z_]\w*)(?:\s*:\s*([^{]+))?`)
	functionPattern     = regexp.MustCompile(`\bfunc\s+([A-Za-z_]\w*)(?:<[^>]+>)?\s*\(`)
	variablePattern     = regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|open|static|class|final|lazy|weak|unowned|private\(set\)|fileprivate\(set\)|internal\(set\))\s+)*(?:let|var)\s+([A-Za-z_]\w*)(?:\s*:\s*([^=<{]+(?:<[^>]+>)?))?`)
	vaporRoutePattern   = regexp.MustCompile(`\buse:\s*([A-Za-z_]\w*)`)
	receiverCallPattern = regexp.MustCompile(`\b([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*\(`)
	callPattern         = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`)
)

type swiftSemanticFacts struct {
	protocolMethods    map[string]map[string]struct{}
	typeConformances   map[string]map[string]struct{}
	vaporRouteHandlers map[string]struct{}
}

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
	facts := collectSwiftSemanticFacts(lines)
	braceDepth := 0
	stack := make([]scopedContext, 0)
	seenVariables := make(map[string]struct{})
	variableTypes := make(map[string]string)
	seenCalls := make(map[string]struct{})
	pendingAttributes := make([]string, 0)

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			braceDepth += braceDelta(rawLine)
			stack = popCompletedScopes(stack, braceDepth)
			continue
		}

		codeLine, attributes, onlyAttributes := swiftCodeLineAndAttributes(trimmed)
		pendingAttributes = append(pendingAttributes, attributes...)
		if onlyAttributes {
			braceDepth += braceDelta(rawLine)
			stack = popCompletedScopes(stack, braceDepth)
			continue
		}

		appendImport(payload, codeLine, lineNumber, isDependency)
		matchedDeclaration := false
		stack, matchedDeclaration = appendTypes(payload, stack, codeLine, rawLine, lineNumber, braceDepth, pendingAttributes)
		if appendFunctions(payload, stack, codeLine, codeLine, lineNumber, options, pendingAttributes, facts) {
			matchedDeclaration = true
		}
		if appendVariable(payload, stack, codeLine, lineNumber, seenVariables, variableTypes, facts) {
			matchedDeclaration = true
		}
		appendCalls(payload, codeLine, lineNumber, variableTypes, seenCalls, isDependency)
		if matchedDeclaration {
			pendingAttributes = pendingAttributes[:0]
		}

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
	attributes []string,
) ([]scopedContext, bool) {
	for _, typed := range swiftTypePatterns() {
		if matches := typed.pattern.FindStringSubmatch(trimmed); len(matches) >= 2 {
			name := matches[1]
			bases := parseInheritanceClause(matches, 2)
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"bases":       bases,
				"lang":        "swift",
			}
			if rootKinds := swiftTypeDeadCodeRootKinds(typed.kind, bases, attributes); len(rootKinds) > 0 {
				item["dead_code_root_kinds"] = rootKinds
			}
			shared.AppendBucket(payload, typed.bucket, item)
			return append(stack, scopedContext{
				kind:       typed.kind,
				name:       name,
				braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")),
			}), true
		}
	}
	return stack, false
}

func appendFunctions(
	payload map[string]any,
	stack []scopedContext,
	trimmed string,
	rawLine string,
	lineNumber int,
	options shared.Options,
	attributes []string,
	facts swiftSemanticFacts,
) bool {
	matched := false
	classContext := currentScopedName(stack, "class", "struct", "enum", "protocol")
	scopeKind := currentScopedKind(stack, "class", "struct", "enum", "protocol")
	if matches := functionPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		appendFunction(payload, matches[1], rawLine, lineNumber, options, classContext, scopeKind, attributes, facts)
		matched = true
	}
	if strings.HasPrefix(trimmed, "init(") || strings.Contains(trimmed, " init(") {
		appendFunction(payload, "init", rawLine, lineNumber, options, classContext, scopeKind, attributes, facts)
		matched = true
	}
	return matched
}

func appendFunction(
	payload map[string]any,
	name string,
	source string,
	lineNumber int,
	options shared.Options,
	classContext string,
	scopeKind string,
	attributes []string,
	facts swiftSemanticFacts,
) {
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
	if rootKinds := swiftFunctionDeadCodeRootKinds(name, source, classContext, scopeKind, attributes, facts); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
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
	facts swiftSemanticFacts,
) bool {
	matches := variablePattern.FindStringSubmatch(trimmed)
	if len(matches) < 2 {
		return false
	}
	name := matches[1]
	if _, ok := seenVariables[name]; ok {
		return false
	}
	seenVariables[name] = struct{}{}
	contextName := currentScopedName(stack, "class", "struct", "enum")
	varType := ""
	if len(matches) >= 3 {
		varType = strings.TrimSpace(matches[2])
	}
	item := map[string]any{
		"name":          name,
		"type":          varType,
		"context":       contextName,
		"class_context": contextName,
		"line_number":   lineNumber,
		"end_line":      lineNumber,
		"lang":          "swift",
	}
	if rootKinds := swiftVariableDeadCodeRootKinds(name, varType, contextName, facts); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	shared.AppendBucket(payload, "variables", item)
	variableTypes[name] = varType
	return true
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
