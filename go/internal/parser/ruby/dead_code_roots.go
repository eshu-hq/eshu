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
	"before_action": {},
	"after_action":  {},
	"around_action": {},
	"before_filter": {},
	"after_filter":  {},
	"around_filter": {},
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
// stripping the leading colon. Bare symbol arguments and symbols nested inside
// an array argument (for example `before_action [:a, :b]`) are both collected,
// because Rails callback registrations accept either form.
func (s *rubySyntax) symbolArguments(node *tree_sitter.Node) []string {
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	names := make([]string, 0)
	s.collectSymbolNames(args, &names)
	return names
}

// collectSymbolNames appends the colon-stripped name of every simple_symbol that
// is a direct child of node or an element of a direct child array node.
func (s *rubySyntax) collectSymbolNames(node *tree_sitter.Node, names *[]string) {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return
	}
	for {
		child := cursor.Node()
		if child.IsNamed() {
			switch child.Kind() {
			case "simple_symbol":
				if name := strings.TrimPrefix(s.text(child), ":"); name != "" {
					*names = append(*names, name)
				}
			case "array":
				// Rails callbacks accept an array literal, e.g.
				// `before_action [:a, :b]`, whose simple_symbol elements are
				// grandchildren of the argument node. Descend one level so the
				// array form is collected like bare symbol arguments.
				s.collectSymbolNames(child, names)
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
}

// rubyScriptEntrypointCallNames returns the method names made reachable by a
// `if __FILE__ == $0` (or `$PROGRAM_NAME`) script guard. The guard is detected
// on the AST: an if/unless node whose condition compares `__FILE__` against a
// `$0`/`$PROGRAM_NAME` global, in either order. Receiverless calls and bare
// identifier statements in the guard body name reachable methods.
func rubyScriptEntrypointCallNames(syntax *rubySyntax) map[string]struct{} {
	names := make(map[string]struct{})
	shared.WalkNamed(syntax.root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "if", "unless":
		default:
			return
		}
		if !rubyIsScriptGuardCondition(syntax, node.ChildByFieldName("condition")) {
			return
		}
		body := node.ChildByFieldName("consequence")
		if body == nil {
			return
		}
		rubyCollectScriptBodyNames(syntax, body, names)
	})
	return names
}

// rubyIsScriptGuardCondition reports whether a condition node is an equality
// (`==`) comparison of `__FILE__` against a `$0`/`$PROGRAM_NAME` global, in
// either order. A non-equality operator such as `!=` guards a non-entrypoint
// body and must not root the calls inside it.
func rubyIsScriptGuardCondition(syntax *rubySyntax, condition *tree_sitter.Node) bool {
	if condition == nil || condition.Kind() != "binary" {
		return false
	}
	operator := condition.ChildByFieldName("operator")
	if operator == nil || syntax.text(operator) != "==" {
		return false
	}
	left := condition.ChildByFieldName("left")
	right := condition.ChildByFieldName("right")
	if left == nil || right == nil {
		return false
	}
	return rubyScriptGuardOperands(syntax, left, right) ||
		rubyScriptGuardOperands(syntax, right, left)
}

// rubyScriptGuardOperands reports whether file is the `__FILE__` identifier and
// program is the `$0` or `$PROGRAM_NAME` global variable.
func rubyScriptGuardOperands(syntax *rubySyntax, file *tree_sitter.Node, program *tree_sitter.Node) bool {
	if file.Kind() != "identifier" || syntax.text(file) != "__FILE__" {
		return false
	}
	if program.Kind() != "global_variable" {
		return false
	}
	switch syntax.text(program) {
	case "$0", "$PROGRAM_NAME":
		return true
	default:
		return false
	}
}

// rubyCollectScriptBodyNames records the receiverless call names and bare
// identifier statement names found among the direct children of a guard body.
func rubyCollectScriptBodyNames(syntax *rubySyntax, body *tree_sitter.Node, names map[string]struct{}) {
	cursor := body.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return
	}
	for {
		child := cursor.Node()
		if child.IsNamed() {
			switch child.Kind() {
			case "identifier":
				if name := syntax.text(child); name != "" {
					names[name] = struct{}{}
				}
			case "call":
				if child.ChildByFieldName("receiver") == nil {
					if method := child.ChildByFieldName("method"); method != nil && method.Kind() == "identifier" {
						if name := syntax.text(method); name != "" {
							names[name] = struct{}{}
						}
					}
				}
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
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
