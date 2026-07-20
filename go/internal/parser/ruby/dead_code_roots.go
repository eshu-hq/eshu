// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/rubycontroller"
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
	registry rubyClassRegistry,
) []string {
	rootKinds := make([]string, 0, 2)
	if name == "method_missing" || name == "respond_to_missing?" {
		rootKinds = appendRubyRootKind(rootKinds, rubyDynamicDispatchHookRoot)
	}
	if contextType == "class" &&
		functionType == "instance" &&
		visibility == "public" &&
		rubyIsRailsController(contextName, registry) &&
		rubyIsRailsControllerActionName(name) {
		rootKinds = appendRubyRootKind(rootKinds, rubyRailsControllerActionRoot)
	}
	return rootKinds
}

// rubyDeadCodeNames holds the three independent name sets a single merged
// tree walk collects for annotateRubyDeadCodeRoots: Rails callback
// registrations, literal method references, and script-entrypoint call
// names. Populating all three from one shared.WalkNamed pass (see
// rubyCollectSemantics) replaces what were three separate full-tree walks.
type rubyDeadCodeNames struct {
	railsCallback    map[string]struct{}
	methodReference  map[string]struct{}
	scriptEntrypoint map[string]struct{}
}

// annotateRubyDeadCodeRoots tags functions reachable through Rails callbacks,
// literal method references, and script entrypoints discovered in the AST.
// names is collected once per file by rubyCollectSemantics; the three
// per-kind loops below preserve the original append order (rails callback,
// then method reference, then script entrypoint) so a function matched by
// more than one kind still accumulates its dead_code_root_kinds in the same
// order the three separate walks always produced.
func annotateRubyDeadCodeRoots(payload map[string]any, names rubyDeadCodeNames) {
	functionsByName := rubyFunctionItemsByName(payload)
	for name := range names.railsCallback {
		for _, function := range functionsByName[name] {
			appendRubyFunctionRootKind(function, rubyRailsCallbackMethodRoot)
		}
	}
	for name := range names.methodReference {
		for _, function := range functionsByName[name] {
			appendRubyFunctionRootKind(function, rubyMethodReferenceTargetRoot)
		}
	}
	for name := range names.scriptEntrypoint {
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

// rubyCollectRailsCallbackNames records the symbol names registered by a
// single Rails callback registration call node into names. It is invoked once
// per "call" node from the merged rubyCollectSemantics walk instead of
// running its own dedicated shared.WalkNamed pass.
func rubyCollectRailsCallbackNames(syntax *rubySyntax, node *tree_sitter.Node, names map[string]struct{}) {
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
}

// rubyCollectMethodReferenceNames records the symbol names passed to a single
// reflection call node (method/send/public_send) into names. It is invoked
// once per "call" node from the merged rubyCollectSemantics walk instead of
// running its own dedicated shared.WalkNamed pass.
func rubyCollectMethodReferenceNames(syntax *rubySyntax, node *tree_sitter.Node, names map[string]struct{}) {
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

// rubyCollectScriptEntrypointNames records the method names made reachable by
// a single `if __FILE__ == $0` (or `$PROGRAM_NAME`) script guard node into
// names. The guard is detected on the AST: an if/unless node whose condition
// compares `__FILE__` against a `$0`/`$PROGRAM_NAME` global, in either order.
// Receiverless calls and bare identifier statements in the guard body name
// reachable methods. It is invoked once per "if"/"unless" node from the
// merged rubyCollectSemantics walk instead of running its own dedicated
// shared.WalkNamed pass.
func rubyCollectScriptEntrypointNames(syntax *rubySyntax, node *tree_sitter.Node, names map[string]struct{}) {
	if !rubyIsScriptGuardCondition(syntax, node.ChildByFieldName("condition")) {
		return
	}
	body := node.ChildByFieldName("consequence")
	if body == nil {
		return
	}
	rubyCollectScriptBodyNames(syntax, body, names)
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

// rubyIsRailsController reports whether contextName's declared superclass
// chain, walked transitively through registry (classes defined earlier in
// the same file — Ruby source is top-down, so the registry is always
// complete for the current position), terminates in an accepted Rails
// controller base.
//
// The decision itself — accepted-base set, suffix fallbacks, and the
// keep-biased asymmetries — lives in the shared rubycontroller package so the
// reducer's repo-wide code-root verdict builder (#5376) re-runs the identical
// logic and can never drift from the parser. This function only adapts the
// same-file rubyClassRegistry to the shared rubycontroller.Registry contract;
// see rubycontroller.Decide for the documented outcomes.
//
// contextName and every registry key are simple (last-segment) class names, so
// two same-file classes whose short names collide across namespaces share one
// registry entry and the last one registered wins; see rubyClassRegistry's doc
// for why the qualified name is not keyed here and why repo-wide resolution is
// deferred to #5376.
func rubyIsRailsController(contextName string, registry rubyClassRegistry) bool {
	return rubycontroller.IsRailsController(contextName, rubySameFileControllerRegistry{registry: registry})
}

// rubySameFileControllerRegistry adapts the parser's single-valued, same-file
// rubyClassRegistry to the shared rubycontroller.Registry contract. Each class
// has at most one declared superclass in the same-file registry, so
// DeclaredBases returns a singleton slice — behavior-identical to the original
// same-file walk.
type rubySameFileControllerRegistry struct {
	registry rubyClassRegistry
}

func (r rubySameFileControllerRegistry) DeclaredBases(className string) ([]string, bool) {
	base, declared := r.registry.superclass[className]
	if !declared {
		return nil, false
	}
	return []string{base}, true
}

func (r rubySameFileControllerRegistry) IsKnownClass(className string) bool {
	_, ok := r.registry.known[className]
	return ok
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
