package ruby

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

const (
	rubyRailsControllerActionRoot = "ruby.rails_controller_action"
	rubyRailsCallbackMethodRoot   = "ruby.rails_callback_method"
	rubyDynamicDispatchHookRoot   = "ruby.dynamic_dispatch_hook"
	rubyMethodReferenceTargetRoot = "ruby.method_reference_target"
	rubyScriptEntrypointRoot      = "ruby.script_entrypoint"
)

// rubyRailsCallbackMethods is the set of Rails callback registration methods
// whose symbol arguments name reachable instance methods.
var rubyRailsCallbackMethods = map[string]struct{}{
	"before_action":  {},
	"after_action":   {},
	"around_action":  {},
	"before_filter":  {},
	"after_filter":   {},
	"around_filter":  {},
}

// rubyMethodReferenceMethods is the set of reflection methods whose symbol
// argument names a method kept reachable as a reference target.
var rubyMethodReferenceMethods = map[string]struct{}{
	"method":      {},
	"send":        {},
	"public_send": {},
}

// rubyFunctionDefinitionRootKinds returns the dead-code root kinds implied by a
// method definition's own shape: dynamic-dispatch hooks and Rails controller
// actions.
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

// annotateRubyDeadCodeRoots tags functions reachable through Rails callbacks,
// literal method references, and script entrypoints discovered in the AST.
func annotateRubyDeadCodeRoots(payload map[string]any, syntax *rubySyntax) {
	functionsByName := rubyFunctionItemsByName(payload)
	for name := range rubyRailsCallbackMethodNames(syntax) {
		for _, function := range functionsByName[name] {
			appendRubyFunctionRootKind(function, rubyRailsCallbackMethodRoot)
		}
	}
	for name := range rubyLiteralMethodReferenceNames(syntax) {
		for _, function := range functionsByName[name] {
			appendRubyFunctionRootKind(function, rubyMethodReferenceTargetRoot)
		}
	}
	for name := range rubyScriptEntrypointCallNames(syntax) {
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

// rubyRailsCallbackMethodNames returns the symbol names registered through Rails
// callback calls anywhere in the tree.
func rubyRailsCallbackMethodNames(syntax *rubySyntax) map[string]struct{} {
	names := make(map[string]struct{})
	shared.WalkNamed(syntax.root, func(node *tree_sitter.Node) {
		if node.Kind() != "call" {
			return
		}
		method := node.ChildByFieldName("method")
		if method == nil || method.Kind() != "identifier" {
			return
		}
		if _, ok := rubyRailsCallbackMethods[syntax.text(method)]; !ok {
			return
		}
		for _, symbol := range syntax.symbolArguments(node) {
			names[symbol] = struct{}{}
		}
	})
	return names
}

// rubyLiteralMethodReferenceNames returns the symbol names passed to reflection
// methods like method/send/public_send.
func rubyLiteralMethodReferenceNames(syntax *rubySyntax) map[string]struct{} {
	names := make(map[string]struct{})
	shared.WalkNamed(syntax.root, func(node *tree_sitter.Node) {
		if node.Kind() != "call" {
			return
		}
		method := node.ChildByFieldName("method")
		if method == nil || method.Kind() != "identifier" {
			return
		}
		if _, ok := rubyMethodReferenceMethods[syntax.text(method)]; !ok {
			return
		}
		for _, symbol := range syntax.symbolArguments(node) {
			names[symbol] = struct{}{}
		}
	})
	return names
}

// symbolArguments returns the names of simple symbol arguments of a call node,
// stripping the leading colon.
func (s *rubySyntax) symbolArguments(node *tree_sitter.Node) []string {
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	names := make([]string, 0)
	cursor := args.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return names
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && child.Kind() == "simple_symbol" {
			name := strings.TrimPrefix(s.text(child), ":")
			if name != "" {
				names = append(names, name)
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return names
}

// rubyScriptEntrypointCallNames returns receiverless call names that appear
// inside a `if __FILE__ == $0` (or `$PROGRAM_NAME`) script guard. The guard and
// its bare statement calls are recovered from the source lines because the
// grammar produces an error region for the modifier-less guard form.
func rubyScriptEntrypointCallNames(syntax *rubySyntax) map[string]struct{} {
	names := make(map[string]struct{})
	lines := syntax.lines
	for index := 0; index < len(lines); index++ {
		if !rubyIsScriptGuardLine(strings.TrimSpace(lines[index])) {
			continue
		}
		for lineIndex := index + 1; lineIndex < len(lines); lineIndex++ {
			trimmed := strings.TrimSpace(lines[lineIndex])
			if trimmed == "end" {
				break
			}
			if name := rubyBareScriptCallName(trimmed); name != "" && !rubyKeywordLikeIdentifier(name) {
				names[name] = struct{}{}
			}
		}
	}
	return names
}

// rubyIsScriptGuardLine reports whether a trimmed line is a bare script guard:
// `if __FILE__ == $0` or `if $0 == __FILE__` (also accepting `$PROGRAM_NAME`).
func rubyIsScriptGuardLine(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "if ") {
		return false
	}
	condition := strings.TrimSpace(trimmed[len("if "):])
	parts := strings.SplitN(condition, "==", 2)
	if len(parts) != 2 {
		return false
	}
	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	return rubyScriptGuardPair(left, right) || rubyScriptGuardPair(right, left)
}

func rubyScriptGuardPair(file string, program string) bool {
	return file == "__FILE__" && (program == "$0" || program == "$PROGRAM_NAME")
}

// rubyBareScriptCallName returns the bare call name from a `name` or `name(...)`
// statement line, or empty when the line is not a bare call.
func rubyBareScriptCallName(trimmed string) string {
	end := rubyScanBareScriptName(trimmed)
	if end == 0 {
		return ""
	}
	name := trimmed[:end]
	rest := strings.TrimSpace(trimmed[end:])
	if rest == "" {
		return name
	}
	if !strings.HasPrefix(rest, "(") {
		return ""
	}
	if !strings.HasSuffix(rest, ")") {
		return ""
	}
	if strings.Count(rest, "(") != 1 || strings.Count(rest, ")") != 1 {
		return ""
	}
	return name
}

// rubyScanBareScriptName returns the length of a leading `[A-Za-z_]\w*[!?=]?`
// identifier, or zero.
func rubyScanBareScriptName(trimmed string) int {
	if trimmed == "" || !rubyIsIdentStartByte(trimmed[0]) {
		return 0
	}
	cursor := 1
	for cursor < len(trimmed) && rubyIsIdentByte(trimmed[cursor]) {
		cursor++
	}
	if cursor < len(trimmed) {
		switch trimmed[cursor] {
		case '!', '?', '=':
			cursor++
		}
	}
	return cursor
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
