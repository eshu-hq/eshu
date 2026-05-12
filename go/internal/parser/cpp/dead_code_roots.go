package cpp

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

const (
	cppMainFunctionRoot          = "cpp.main_function"
	cppPublicHeaderAPIRoot       = "cpp.public_header_api"
	cppVirtualMethodRoot         = "cpp.virtual_method"
	cppOverrideMethodRoot        = "cpp.override_method"
	cppCallbackArgumentTarget    = "cpp.callback_argument_target"
	cppFunctionPointerTargetRoot = "cpp.function_pointer_target"
	cppNodeAddonEntrypointRoot   = "cpp.node_addon_entrypoint"
)

var cppQualifiedFunctionPattern = regexp.MustCompile(
	`(?:^|[\s*&])([A-Za-z_]\w*)\s*::\s*(~?[A-Za-z_]\w*)\s*\(`,
)

var cppFunctionPointerAliasPattern = regexp.MustCompile(
	`(?s)\b(?:using\s+([A-Za-z_]\w*)\s*=\s*[^;]*\(\s*\*\s*\)|typedef\b[^;]*\(\s*\*\s*([A-Za-z_]\w*)\s*\))[^;]*;`,
)

var cppDirectInitializerTargetPattern = regexp.MustCompile(
	`=\s*&?\s*([A-Za-z_]\w*)\s*(?:[,;]|$)`,
)

var cppBraceInitializerPattern = regexp.MustCompile(`(?s)=\s*\{([^{}]*)\}`)

var cppNodeAddonRegistrationPattern = regexp.MustCompile(
	`\b(?:NAPI_MODULE|NODE_MODULE|NODE_MODULE_CONTEXT_AWARE)\s*\([^,]+,\s*([A-Za-z_]\w*)`,
)

type cppFunctionKey struct {
	class string
	name  string
}

func annotateCPPDeadCodeRoots(payload map[string]any, root *tree_sitter.Node, source []byte) {
	functionsByName := cppFunctionItemsByName(payload)
	if len(functionsByName) == 0 {
		return
	}
	functionPointerAliases := cppFunctionPointerAliasNames(string(source))

	for _, mainFunction := range functionsByName["main"] {
		appendCPPDeadCodeRootKind(mainFunction, cppMainFunctionRoot)
	}
	annotateCPPNodeAddonRoots(functionsByName, string(source))

	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_definition":
			annotateCPPMethodRuntimeRoots(payload, node, source)
		case "call_expression":
			for _, argument := range cppCallArguments(node, source) {
				for _, function := range functionsByName[argument] {
					appendCPPDeadCodeRootKind(function, cppCallbackArgumentTarget)
				}
			}
		case "declaration":
			annotateCPPFunctionPointerTargetRoot(functionsByName, functionPointerAliases, node, source)
		}
	})
}

func annotateCPPNodeAddonRoots(functionsByName map[string][]map[string]any, source string) {
	for _, name := range []string{"NAPI_MODULE_INIT", "NODE_MODULE_INIT"} {
		for _, function := range functionsByName[name] {
			appendCPPDeadCodeRootKind(function, cppNodeAddonEntrypointRoot)
		}
	}
	for _, match := range cppNodeAddonRegistrationPattern.FindAllStringSubmatch(source, -1) {
		if len(match) != 2 {
			continue
		}
		for _, function := range functionsByName[strings.TrimSpace(match[1])] {
			appendCPPDeadCodeRootKind(function, cppNodeAddonEntrypointRoot)
		}
	}
}

func annotateCPPMethodRuntimeRoots(payload map[string]any, node *tree_sitter.Node, source []byte) {
	name, classContext := cppFunctionNameAndClass(node, firstNamedDescendant(node, "identifier", "field_identifier", "destructor_name"), source)
	if name == "" {
		return
	}
	function := cppFunctionItem(payload, classContext, name)
	if function == nil {
		return
	}
	text := shared.NodeText(node, source)
	if cppTextHasWord(text, "virtual") {
		appendCPPDeadCodeRootKind(function, cppVirtualMethodRoot)
	}
	if cppTextHasWord(text, "override") {
		appendCPPDeadCodeRootKind(function, cppOverrideMethodRoot)
	}
}

func cppFunctionNameAndClass(node *tree_sitter.Node, nameNode *tree_sitter.Node, source []byte) (string, string) {
	text := shared.NodeText(node, source)
	if match := cppQualifiedFunctionPattern.FindStringSubmatch(text); len(match) == 3 {
		return strings.TrimSpace(match[2]), strings.TrimSpace(match[1])
	}
	return strings.TrimSpace(shared.NodeText(nameNode, source)),
		strings.TrimSpace(nearestNamedAncestor(node, source, "class_specifier", "struct_specifier"))
}

func appendCPPImportMetadata(payload map[string]any, node *tree_sitter.Node, source []byte) {
	nameNode := firstNamedDescendant(node, "system_lib_string", "string_literal")
	if nameNode == nil {
		return
	}
	name := strings.Trim(shared.NodeText(nameNode, source), `<>"`)
	if name == "" {
		return
	}

	includeKind := "local"
	if nameNode.Kind() == "system_lib_string" {
		includeKind = "system"
	}

	shared.AppendBucket(payload, "imports", map[string]any{
		"name":             name,
		"source":           name,
		"full_import_name": strings.TrimSpace(shared.NodeText(node, source)),
		"include_kind":     includeKind,
		"line_number":      shared.NodeLine(node),
		"lang":             "cpp",
	})
}

func cppFunctionItemsByName(payload map[string]any) map[string][]map[string]any {
	items, _ := payload["functions"].([]map[string]any)
	functions := make(map[string][]map[string]any, len(items))
	for _, item := range items {
		name := strings.TrimSpace(cppStringVal(item, "name"))
		if name == "" {
			continue
		}
		functions[name] = append(functions[name], item)
	}
	return functions
}

func cppFunctionItemsByKey(payload map[string]any) map[cppFunctionKey][]map[string]any {
	items, _ := payload["functions"].([]map[string]any)
	functions := make(map[cppFunctionKey][]map[string]any, len(items))
	for _, item := range items {
		name := strings.TrimSpace(cppStringVal(item, "name"))
		if name == "" {
			continue
		}
		key := cppFunctionKey{class: strings.TrimSpace(cppStringVal(item, "class_context")), name: name}
		functions[key] = append(functions[key], item)
	}
	return functions
}

func cppFunctionItem(payload map[string]any, classContext string, name string) map[string]any {
	items := cppFunctionItemsByKey(payload)[cppFunctionKey{class: strings.TrimSpace(classContext), name: strings.TrimSpace(name)}]
	if len(items) == 0 && strings.TrimSpace(classContext) == "" {
		items = cppFunctionItemsByName(payload)[strings.TrimSpace(name)]
	}
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func cppFunctionPointerAliasNames(source string) map[string]struct{} {
	matches := cppFunctionPointerAliasPattern.FindAllStringSubmatch(source, -1)
	names := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		for _, group := range match[1:] {
			name := strings.TrimSpace(group)
			if name != "" {
				names[name] = struct{}{}
			}
		}
	}
	return names
}

func annotateCPPFunctionPointerTargetRoot(
	functions map[string][]map[string]any,
	functionPointerAliases map[string]struct{},
	node *tree_sitter.Node,
	source []byte,
) {
	text := strings.TrimSpace(shared.NodeText(node, source))
	if !strings.Contains(text, "=") {
		return
	}
	left := text[:strings.LastIndex(text, "=")]
	if !cppDeclarationHasFunctionPointerTarget(left, functionPointerAliases) {
		return
	}
	for _, target := range cppDirectFunctionPointerInitializerTargets(text) {
		for _, function := range functions[target] {
			appendCPPDeadCodeRootKind(function, cppFunctionPointerTargetRoot)
		}
	}
}

func cppDeclarationHasFunctionPointerTarget(left string, functionPointerAliases map[string]struct{}) bool {
	left = strings.TrimSpace(left)
	if strings.Contains(left, "(*") || strings.Contains(left, "std::function") {
		return true
	}
	fields := strings.FieldsFunc(left, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	for _, field := range fields {
		if _, ok := functionPointerAliases[field]; ok {
			return true
		}
	}
	return false
}

func cppDirectFunctionPointerInitializerTargets(text string) []string {
	matches := cppDirectInitializerTargetPattern.FindAllStringSubmatch(text, -1)
	braceInitializers := cppBraceInitializerPattern.FindAllStringSubmatch(text, -1)
	targets := make([]string, 0, len(matches)+len(braceInitializers))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		targets = appendCPPFunctionPointerTarget(targets, seen, match[1])
	}
	for _, match := range braceInitializers {
		if len(match) != 2 {
			continue
		}
		for _, target := range cppBraceInitializerTargets(match[1]) {
			targets = appendCPPFunctionPointerTarget(targets, seen, target)
		}
	}
	return targets
}

func appendCPPFunctionPointerTarget(targets []string, seen map[string]struct{}, target string) []string {
	target = strings.TrimSpace(target)
	if target == "" || !cppIdentifierLike(target) {
		return targets
	}
	if _, ok := seen[target]; ok {
		return targets
	}
	seen[target] = struct{}{}
	return append(targets, target)
}

func cppBraceInitializerTargets(initializer string) []string {
	parts := strings.Split(initializer, ",")
	targets := make([]string, 0, len(parts))
	for _, part := range parts {
		target := part
		if index := strings.LastIndex(target, "="); index >= 0 {
			target = target[index+1:]
		}
		target = cppDirectIdentifierExpression(target)
		if target == "" {
			continue
		}
		targets = append(targets, target)
	}
	return targets
}

func cppCallArguments(node *tree_sitter.Node, source []byte) []string {
	argumentsNode := node.ChildByFieldName("arguments")
	if argumentsNode == nil {
		return nil
	}
	var arguments []string
	cursor := argumentsNode.Walk()
	defer cursor.Close()
	for _, child := range argumentsNode.NamedChildren(cursor) {
		value := cppDirectIdentifierExpression(shared.NodeText(&child, source))
		if value == "" {
			continue
		}
		arguments = append(arguments, value)
	}
	return arguments
}

func cppDirectIdentifierExpression(expression string) string {
	value := strings.TrimSpace(expression)
	value = strings.TrimPrefix(value, "&")
	value = strings.TrimSpace(value)
	for strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "("), ")"))
		value = strings.TrimPrefix(value, "&")
		value = strings.TrimSpace(value)
	}
	if !cppIdentifierLike(value) {
		return ""
	}
	return value
}

func appendCPPDeadCodeRootKind(item map[string]any, rootKind string) {
	existing, _ := item["dead_code_root_kinds"].([]string)
	for _, value := range existing {
		if value == rootKind {
			return
		}
	}
	item["dead_code_root_kinds"] = append(existing, rootKind)
}

func cppTextHasWord(text string, word string) bool {
	for _, field := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}) {
		if field == word {
			return true
		}
	}
	return false
}

func cppStringVal(item map[string]any, key string) string {
	value, _ := item[key].(string)
	return value
}

func cppIdentifierLike(value string) bool {
	for index, r := range value {
		switch {
		case r == '_':
			continue
		case index == 0 && unicode.IsDigit(r):
			return false
		case unicode.IsLetter(r), unicode.IsDigit(r):
			continue
		default:
			return false
		}
	}
	return value != ""
}

func cppKeywordLikeIdentifier(value string) bool {
	switch value {
	case "if", "for", "while", "switch", "return", "sizeof", "noexcept":
		return true
	default:
		return false
	}
}
