package ruby

import (
	"regexp"
	"strings"
)

const (
	rubyRailsControllerActionRoot = "ruby.rails_controller_action"
	rubyRailsCallbackMethodRoot   = "ruby.rails_callback_method"
	rubyDynamicDispatchHookRoot   = "ruby.dynamic_dispatch_hook"
	rubyMethodReferenceTargetRoot = "ruby.method_reference_target"
	rubyScriptEntrypointRoot      = "ruby.script_entrypoint"
)

var (
	rubyRailsCallbackPattern = regexp.MustCompile(
		`^\s*(?:before_action|after_action|around_action|before_filter|after_filter|around_filter)\b(.*)$`,
	)
	rubySymbolLiteralPattern          = regexp.MustCompile(`:([A-Za-z_]\w*[!?=]?)`)
	rubyLiteralMethodReferencePattern = regexp.MustCompile(
		`\b(?:method|public_send|send)\s*\(?\s*:([A-Za-z_]\w*[!?=]?)`,
	)
	rubyScriptGuardPattern = regexp.MustCompile(
		`^\s*if\s+(?:__FILE__\s*==\s*(?:\$PROGRAM_NAME|\$0)|(?:\$PROGRAM_NAME|\$0)\s*==\s*__FILE__)\s*$`,
	)
	rubyBareScriptCallPattern = regexp.MustCompile(`^\s*([A-Za-z_]\w*[!?=]?)\s*(?:\([^)]*\))?\s*$`)
)

func rubyFunctionDefinitionRootKinds(
	name string,
	functionType string,
	contextName string,
	contextType string,
	visibility string,
) []string {
	rootKinds := make([]string, 0, 2)
	if name == "method_missing" || name == "respond_to_missing?" {
		rootKinds = appendRubyRootKind(rootKinds, rubyDynamicDispatchHookRoot)
	}
	if contextType == "class" &&
		functionType == "instance" &&
		visibility == "public" &&
		rubyIsRailsController(contextName) &&
		rubyIsRailsControllerActionName(name) {
		rootKinds = appendRubyRootKind(rootKinds, rubyRailsControllerActionRoot)
	}
	return rootKinds
}

func annotateRubyDeadCodeRoots(payload map[string]any, lines []string) {
	functionsByName := rubyFunctionItemsByName(payload)
	for name := range rubyRailsCallbackMethodNames(lines) {
		for _, function := range functionsByName[name] {
			appendRubyFunctionRootKind(function, rubyRailsCallbackMethodRoot)
		}
	}
	for name := range rubyLiteralMethodReferenceNames(lines) {
		for _, function := range functionsByName[name] {
			appendRubyFunctionRootKind(function, rubyMethodReferenceTargetRoot)
		}
	}
	for name := range rubyScriptEntrypointCallNames(lines) {
		for _, function := range functionsByName[name] {
			appendRubyFunctionRootKind(function, rubyScriptEntrypointRoot)
		}
	}
}

func rubyFunctionItemsByName(payload map[string]any) map[string][]map[string]any {
	items, _ := payload["functions"].([]map[string]any)
	functions := make(map[string][]map[string]any, len(items))
	for _, item := range items {
		name, _ := item["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		functions[name] = append(functions[name], item)
	}
	return functions
}

func rubyRailsCallbackMethodNames(lines []string) map[string]struct{} {
	names := make(map[string]struct{})
	for _, line := range lines {
		matches := rubyRailsCallbackPattern.FindStringSubmatch(line)
		if len(matches) != 2 {
			continue
		}
		for _, symbol := range rubySymbolLiteralPattern.FindAllStringSubmatch(matches[1], -1) {
			if len(symbol) == 2 && symbol[1] != "" {
				names[symbol[1]] = struct{}{}
			}
		}
	}
	return names
}

func rubyLiteralMethodReferenceNames(lines []string) map[string]struct{} {
	names := make(map[string]struct{})
	for _, line := range lines {
		for _, match := range rubyLiteralMethodReferencePattern.FindAllStringSubmatch(line, -1) {
			if len(match) == 2 && match[1] != "" {
				names[match[1]] = struct{}{}
			}
		}
	}
	return names
}

func rubyScriptEntrypointCallNames(lines []string) map[string]struct{} {
	names := make(map[string]struct{})
	for index := 0; index < len(lines); index++ {
		if !rubyScriptGuardPattern.MatchString(strings.TrimSpace(lines[index])) {
			continue
		}
		for lineIndex := index + 1; lineIndex < len(lines); lineIndex++ {
			trimmed := strings.TrimSpace(lines[lineIndex])
			if trimmed == "end" {
				break
			}
			if matches := rubyBareScriptCallPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
				name := strings.TrimSpace(matches[1])
				if name != "" && !rubyKeywordLikeIdentifier(name) {
					names[name] = struct{}{}
				}
			}
		}
	}
	return names
}

func rubyIsRailsController(contextName string) bool {
	return strings.HasSuffix(strings.TrimSpace(contextName), "Controller")
}

func rubyIsRailsControllerActionName(name string) bool {
	switch strings.TrimSpace(name) {
	case "", "initialize", "method_missing", "respond_to_missing?":
		return false
	default:
		return !strings.HasSuffix(name, "=")
	}
}

func appendRubyFunctionRootKind(item map[string]any, rootKind string) {
	existing, _ := item["dead_code_root_kinds"].([]string)
	item["dead_code_root_kinds"] = appendRubyRootKind(existing, rootKind)
}

func appendRubyRootKind(rootKinds []string, rootKind string) []string {
	for _, value := range rootKinds {
		if value == rootKind {
			return rootKinds
		}
	}
	return append(rootKinds, rootKind)
}

func rubyKeywordLikeIdentifier(value string) bool {
	switch value {
	case "if", "unless", "case", "begin", "for", "while", "until", "return", "yield", "super":
		return true
	default:
		return false
	}
}
