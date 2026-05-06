package parser

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
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
	rubyLocalAssignmentPattern = regexp.MustCompile(`^\s*([a-z_]\w*)\s*=\s*(.+)$`)
	rubyInstanceAssignPattern  = regexp.MustCompile(`^\s*(@\w+)\s*(?:\|\|)?=\s*(.+)$`)
	rubyChainedCallPattern     = regexp.MustCompile(`(?:^|[^A-Za-z0-9_:@])((?:[A-Za-z_]\w*|@[A-Za-z_]\w*|self|[A-Z][A-Za-z0-9_:]*)(?:\.[A-Za-z_]\w*[!?=]?)+)\(([^()]*)\)\.([A-Za-z_]\w*[!?=]?)(?:\s*\(([^)]*)\)|\s+([^#]+))?`)
	rubyScopedCallPattern      = regexp.MustCompile(`([A-Z][A-Za-z0-9_:]*\.[A-Za-z_]\w*[!?=]?)\(`)
	rubyQualifiedCallPattern   = regexp.MustCompile(`(?:^|[^A-Za-z0-9_:@])((?:[A-Za-z_]\w*|@[A-Za-z_]\w*|self|[A-Z][A-Za-z0-9_:]*)(?:\.[A-Za-z_]\w*[!?=]?)+)(?:\s*\(|\b)`)
	rubyBareCallPattern        = regexp.MustCompile(`(?:^|[^A-Za-z0-9_:@])((?:require_relative|require|load|include|extend|attr_accessor|attr_reader|attr_writer|define_method|define_singleton_method|instance_method|instance_eval|cache_method|puts|sleep|method|public_send|send|super|bind))(?:\s*\(([^)]*)\)|\s+([^#]+))`)
)

type rubyBlock struct {
	kind string
	name string
}

func (e *Engine) parseRuby(path string, isDependency bool, options Options) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}

	payload := basePayload(path, "ruby", isDependency)
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
				blocks = blocks[:len(blocks)-1]
			}
			continue
		}

		if matches := rubyModulePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			name := rubyLastSegment(matches[1])
			appendBucket(payload, "modules", map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "ruby",
			})
			blocks = append(blocks, rubyBlock{kind: "module", name: name})
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
			appendBucket(payload, "classes", item)
			blocks = append(blocks, rubyBlock{kind: "class", name: name})
			continue
		}

		if rubySingletonClassPattern.MatchString(trimmed) {
			className := rubyCurrentBlockName(blocks, "class")
			if className == "" {
				className = "self"
			}
			blocks = append(blocks, rubyBlock{kind: "singleton_class", name: className})
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
			}
			args := rubyParseArguments(matches[3])
			item["args"] = args
			if contextName, contextType := rubyCurrentContext(blocks, "class", "module"); contextName != "" {
				item["context"] = contextName
				item["context_type"] = contextType
				if contextType == "class" {
					item["class_context"] = contextName
				}
			}
			if options.IndexSource {
				item["source"] = rawLine
			}
			appendBucket(payload, "functions", item)
			blocks = append(blocks, rubyBlock{kind: "def", name: name})
			continue
		}

		if matches := rubyRequirePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendBucket(payload, "imports", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"lang":        "ruby",
			})
		}
		if matches := rubyRequireRelativePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendBucket(payload, "imports", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"lang":        "ruby",
			})
		}
		if matches := rubyLoadPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendBucket(payload, "imports", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"lang":        "ruby",
			})
		}

		if matches := rubyIncludePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			className := rubyCurrentBlockName(blocks, "class")
			if className != "" {
				appendBucket(payload, "module_inclusions", map[string]any{
					"class":  className,
					"module": rubyLastSegment(matches[1]),
				})
			}
		}

		if matches := rubyInstanceAssignPattern.FindStringSubmatch(trimmed); len(matches) >= 3 {
			variableName := matches[1]
			if _, ok := seenVariables[variableName]; !ok {
				seenVariables[variableName] = struct{}{}
				contextName, contextType := rubyCurrentContext(blocks, "class", "module", "def")
				item := map[string]any{
					"name":        variableName,
					"type":        rubyInferAssignmentType(matches[2]),
					"line_number": lineNumber,
					"end_line":    lineNumber,
					"lang":        "ruby",
				}
				if contextName != "" {
					item["context"] = contextName
					item["context_type"] = contextType
					if contextType == "class" {
						item["class_context"] = contextName
					}
				}
				appendBucket(payload, "variables", item)
			}
		}

		if matches := rubyLocalAssignmentPattern.FindStringSubmatch(trimmed); len(matches) >= 3 {
			variableName := matches[1]
			if _, ok := seenVariables[variableName]; !ok {
				seenVariables[variableName] = struct{}{}
				contextName, contextType := rubyCurrentContext(blocks, "class", "module", "def")
				item := map[string]any{
					"name":        variableName,
					"type":        rubyInferAssignmentType(trimmed),
					"line_number": lineNumber,
					"end_line":    lineNumber,
					"lang":        "ruby",
				}
				if contextName != "" {
					item["context"] = contextName
					item["context_type"] = contextType
					if contextType == "class" {
						item["class_context"] = contextName
					}
				}
				appendBucket(payload, "variables", item)
			}
		}

		for _, variable := range rubyInstanceVarPattern.FindAllString(rawLine, -1) {
			if _, ok := seenVariables[variable]; ok {
				continue
			}
			seenVariables[variable] = struct{}{}
			contextName, contextType := rubyCurrentContext(blocks, "class", "module", "def")
			item := map[string]any{
				"name":        variable,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "ruby",
			}
			if contextName != "" {
				item["context"] = contextName
				item["context_type"] = contextType
				if contextType == "class" {
					item["class_context"] = contextName
				}
			}
			appendBucket(payload, "variables", item)
		}

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
			appendBucket(payload, "function_calls", item)
		}
	}

	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "classes")
	sortNamedBucket(payload, "modules")
	sortNamedBucket(payload, "variables")
	sortNamedBucket(payload, "imports")
	sortNamedBucket(payload, "function_calls")

	return payload, nil
}

func (e *Engine) preScanRuby(path string) ([]string, error) {
	payload, err := e.parseRuby(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "classes", "modules")
	slices.Sort(names)
	return names, nil
}

func rubyCurrentBlockName(blocks []rubyBlock, kind string) string {
	for index := len(blocks) - 1; index >= 0; index-- {
		if blocks[index].kind == kind {
			return blocks[index].name
		}
	}
	return ""
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

func rubyLastSegment(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	segments := strings.Split(trimmed, "::")
	return segments[len(segments)-1]
}
