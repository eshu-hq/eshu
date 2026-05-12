package ruby

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	rubyModulePattern          = regexp.MustCompile(`^\s*module\s+([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`)
	rubyClassPattern           = regexp.MustCompile(`^\s*class\s+([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)(?:\s*<\s*([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*))?`)
	rubySingletonClassPattern  = regexp.MustCompile(`^\s*class\s*<<\s*self\b`)
	rubyFunctionPattern        = regexp.MustCompile(`^\s*def\s+(self\.)?([A-Za-z_]\w*[!?=]?)\s*(?:\((.*?)\))?`)
	rubyRequirePattern         = regexp.MustCompile(`^\s*require\s+['"]([^'"]+)['"]`)
	rubyRequireRelativePattern = regexp.MustCompile(`^\s*require_relative\s+['"]([^'"]+)['"]`)
	rubyLoadPattern            = regexp.MustCompile(`^\s*load\s+['"]([^'"]+)['"]`)
	rubyIncludePattern         = regexp.MustCompile(`^\s*include\s+([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`)
	rubyInstanceVarPattern     = regexp.MustCompile(`@\w+`)
	rubyConstantAssignPattern  = regexp.MustCompile(`^\s*([A-Z]\w*(?:::[A-Z]\w*)*)\s*=\s*(.+)$`)
	rubyLocalAssignmentPattern = regexp.MustCompile(`^\s*([a-z_]\w*)\s*=\s*(.+)$`)
	rubyInstanceAssignPattern  = regexp.MustCompile(`^\s*(@\w+)\s*(?:\|\|)?=\s*(.+)$`)
	rubyOpaqueBlockPattern     = regexp.MustCompile(`^(?:if|unless|case|begin|for|while|until)\b|\bdo\b`)
)

type rubyBlock struct {
	kind       string
	name       string
	visibility string
	item       map[string]any
}

// Parse reads path and returns the legacy Ruby parser payload.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "ruby", isDependency)
	payload["modules"] = []map[string]any{}
	payload["module_inclusions"] = []map[string]any{}
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}

	lines := strings.Split(string(source), "\n")
	blocks := make([]rubyBlock, 0)
	seenVariables := make(map[string]struct{})
	seenCalls := make(map[string]struct{})

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if trimmed == "end" {
			if len(blocks) > 0 {
				rubyCloseBlock(blocks[len(blocks)-1], lineNumber)
				blocks = blocks[:len(blocks)-1]
			}
			continue
		}
		if rubyVisibilityKeyword(trimmed) != "" {
			rubySetCurrentClassVisibility(blocks, trimmed)
			continue
		}

		if matches := rubyModulePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			name := rubyLastSegment(matches[1])
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "ruby",
			}
			shared.AppendBucket(payload, "modules", item)
			blocks = append(blocks, rubyBlock{kind: "module", name: name, visibility: "public", item: item})
			continue
		}

		if matches := rubyClassPattern.FindStringSubmatch(trimmed); len(matches) >= 2 {
			name := rubyLastSegment(matches[1])
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "ruby",
				"type":        "class",
			}
			if len(matches) >= 3 && strings.TrimSpace(matches[2]) != "" {
				item["bases"] = []string{rubyLastSegment(matches[2])}
			}
			shared.AppendBucket(payload, "classes", item)
			blocks = append(blocks, rubyBlock{kind: "class", name: name, visibility: "public", item: item})
			continue
		}

		if rubySingletonClassPattern.MatchString(trimmed) {
			className := rubyCurrentBlockName(blocks, "class")
			if className == "" {
				className = "self"
			}
			blocks = append(blocks, rubyBlock{kind: "singleton_class", name: className, visibility: "public"})
			continue
		}

		if matches := rubyFunctionPattern.FindStringSubmatch(trimmed); len(matches) >= 4 {
			name := matches[2]
			functionType := "instance"
			switch {
			case matches[1] != "" || rubyCurrentBlockName(blocks, "singleton_class") != "":
				functionType = "singleton"
			case name == "method_missing" || name == "respond_to_missing?":
				functionType = "dynamic_dispatch"
			}
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "ruby",
				"decorators":  []string{},
				"type":        functionType,
				"args":        rubyParseArguments(matches[3]),
			}
			contextName, contextType := rubyCurrentContext(blocks, "class", "module")
			visibility := "public"
			if contextType == "class" {
				visibility = rubyCurrentClassVisibility(blocks)
			}
			if contextName != "" {
				item["context"] = contextName
				item["context_type"] = contextType
				if contextType == "class" {
					item["class_context"] = contextName
				}
			}
			if rootKinds := rubyFunctionDefinitionRootKinds(name, functionType, contextName, contextType, visibility); len(rootKinds) > 0 {
				item["dead_code_root_kinds"] = rootKinds
			}
			if options.IndexSource {
				item["source"] = rawLine
			}
			shared.AppendBucket(payload, "functions", item)
			blocks = append(blocks, rubyBlock{kind: "def", name: name, item: item})
			continue
		}

		appendRubyImports(payload, trimmed, lineNumber)
		appendRubyModuleInclusion(payload, blocks, trimmed)
		appendRubyVariables(payload, blocks, seenVariables, rawLine, trimmed, lineNumber)
		appendRubyCalls(payload, blocks, seenCalls, trimmed, lineNumber)
		if rubyStartsOpaqueBlock(trimmed) {
			blocks = append(blocks, rubyBlock{kind: "block"})
		}
	}
	annotateRubyDeadCodeRoots(payload, lines)

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "modules")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")
	return payload, nil
}

// PreScan returns Ruby function, class, and module names used by repository pre-scan.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes", "modules")
	slices.Sort(names)
	return names, nil
}

func appendRubyImports(payload map[string]any, trimmed string, lineNumber int) {
	if matches := rubyRequirePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		shared.AppendBucket(payload, "imports", map[string]any{
			"name":        matches[1],
			"line_number": lineNumber,
			"lang":        "ruby",
		})
	}
	if matches := rubyRequireRelativePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		shared.AppendBucket(payload, "imports", map[string]any{
			"name":        matches[1],
			"line_number": lineNumber,
			"lang":        "ruby",
		})
	}
	if matches := rubyLoadPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		shared.AppendBucket(payload, "imports", map[string]any{
			"name":        matches[1],
			"line_number": lineNumber,
			"lang":        "ruby",
		})
	}
}

func appendRubyModuleInclusion(payload map[string]any, blocks []rubyBlock, trimmed string) {
	if matches := rubyIncludePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		className := rubyCurrentBlockName(blocks, "class")
		if className != "" {
			shared.AppendBucket(payload, "module_inclusions", map[string]any{
				"class":  className,
				"module": rubyLastSegment(matches[1]),
			})
		}
	}
}

func appendRubyVariables(
	payload map[string]any,
	blocks []rubyBlock,
	seenVariables map[string]struct{},
	rawLine string,
	trimmed string,
	lineNumber int,
) {
	if matches := rubyConstantAssignPattern.FindStringSubmatch(trimmed); len(matches) >= 3 {
		appendRubyVariable(payload, blocks, seenVariables, rubyLastSegment(matches[1]), rubyInferAssignmentType(matches[2]), lineNumber)
	}
	if matches := rubyInstanceAssignPattern.FindStringSubmatch(trimmed); len(matches) >= 3 {
		appendRubyVariable(payload, blocks, seenVariables, matches[1], rubyInferAssignmentType(matches[2]), lineNumber)
	}
	if matches := rubyLocalAssignmentPattern.FindStringSubmatch(trimmed); len(matches) >= 3 {
		appendRubyVariable(payload, blocks, seenVariables, matches[1], rubyInferAssignmentType(trimmed), lineNumber)
	}
	for _, variable := range rubyInstanceVarPattern.FindAllString(rawLine, -1) {
		appendRubyVariable(payload, blocks, seenVariables, variable, "", lineNumber)
	}
}

func appendRubyVariable(
	payload map[string]any,
	blocks []rubyBlock,
	seenVariables map[string]struct{},
	name string,
	variableType string,
	lineNumber int,
) {
	if _, ok := seenVariables[name]; ok {
		return
	}
	seenVariables[name] = struct{}{}
	contextName, contextType := rubyCurrentContext(blocks, "class", "module", "def")
	item := map[string]any{
		"name":        name,
		"line_number": lineNumber,
		"end_line":    lineNumber,
		"lang":        "ruby",
	}
	if variableType != "" {
		item["type"] = variableType
	}
	if contextName != "" {
		item["context"] = contextName
		item["context_type"] = contextType
		if contextType == "class" {
			item["class_context"] = contextName
		}
	}
	shared.AppendBucket(payload, "variables", item)
}

func appendRubyCalls(
	payload map[string]any,
	blocks []rubyBlock,
	seenCalls map[string]struct{},
	trimmed string,
	lineNumber int,
) {
	for _, call := range rubyParseCalls(trimmed) {
		fullName := call.fullName
		callKey := fullName + ":" + strconv.Itoa(lineNumber)
		if _, ok := seenCalls[callKey]; ok {
			continue
		}
		seenCalls[callKey] = struct{}{}
		contextName, contextType := rubyCurrentContext(blocks, "class", "module", "def")
		item := map[string]any{
			"name":              rubyCallName(fullName),
			"full_name":         fullName,
			"line_number":       lineNumber,
			"args":              rubyParseArguments(call.args),
			"inferred_obj_type": nil,
			"lang":              "ruby",
			"is_dependency":     false,
		}
		if contextName != "" {
			item["context"] = contextName
			item["context_type"] = contextType
			if contextType == "class" {
				item["class_context"] = contextName
			}
		}
		if className := rubyCurrentBlockName(blocks, "class"); className != "" {
			item["class_context"] = className
		}
		shared.AppendBucket(payload, "function_calls", item)
	}
}

func rubyCurrentBlockName(blocks []rubyBlock, kind string) string {
	for index := len(blocks) - 1; index >= 0; index-- {
		if blocks[index].kind == kind {
			return blocks[index].name
		}
	}
	return ""
}

func rubyCloseBlock(block rubyBlock, lineNumber int) {
	switch block.kind {
	case "class", "def", "module":
	default:
		return
	}
	if block.item == nil || lineNumber <= 0 {
		return
	}
	startLine := rubyLineNumberValue(block.item["line_number"])
	if startLine > 0 && lineNumber < startLine {
		return
	}
	block.item["end_line"] = lineNumber
}

func rubyLineNumberValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func rubyCurrentClassVisibility(blocks []rubyBlock) string {
	for index := len(blocks) - 1; index >= 0; index-- {
		if blocks[index].kind == "class" {
			if blocks[index].visibility != "" {
				return blocks[index].visibility
			}
			return "public"
		}
	}
	return "public"
}

func rubySetCurrentClassVisibility(blocks []rubyBlock, visibility string) {
	visibility = rubyVisibilityKeyword(visibility)
	if visibility == "" {
		return
	}
	for index := len(blocks) - 1; index >= 0; index-- {
		if blocks[index].kind == "class" {
			blocks[index].visibility = visibility
			return
		}
	}
}

func rubyVisibilityKeyword(value string) string {
	switch strings.TrimSpace(value) {
	case "public", "private", "protected":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func rubyCurrentContext(blocks []rubyBlock, kinds ...string) (string, string) {
	for index := len(blocks) - 1; index >= 0; index-- {
		for _, kind := range kinds {
			if blocks[index].kind == kind {
				return blocks[index].name, blocks[index].kind
			}
		}
	}
	return "", ""
}

func rubyLastSegment(name string) string {
	return shared.LastPathSegment(name, "::")
}

// rubyStartsOpaqueBlock tracks Ruby control and DSL blocks so their end tokens
// do not pop the surrounding class, module, or method context.
func rubyStartsOpaqueBlock(trimmed string) bool {
	if !rubyOpaqueBlockPattern.MatchString(trimmed) {
		return false
	}
	return !strings.Contains(trimmed, " end")
}
