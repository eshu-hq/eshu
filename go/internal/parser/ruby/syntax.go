// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// rubyScopeKind names the lexical Ruby block kinds the parser tracks for
// context resolution: modules, classes, singleton classes, and method
// definitions.
type rubyScopeKind string

const (
	rubyScopeModule         rubyScopeKind = "module"
	rubyScopeClass          rubyScopeKind = "class"
	rubyScopeSingletonClass rubyScopeKind = "singleton_class"
	rubyScopeDef            rubyScopeKind = "def"
)

// rubyScope is one lexical block discovered in the AST. It carries the kind and
// resolved name pushed onto the active scope stack so nested definitions and
// calls resolve their enclosing context.
type rubyScope struct {
	kind rubyScopeKind
	name string
}

// rubySyntax is the ordered, fully-resolved view of one Ruby source tree. It
// captures the structural facts (definitions, imports, inclusions, variables,
// calls) discovered while walking the AST.
type rubySyntax struct {
	source        []byte
	lines         []string
	functions     []map[string]any
	classes       []map[string]any
	modules       []map[string]any
	variables     []map[string]any
	imports       []map[string]any
	inclusions    []map[string]any
	calls         []map[string]any
	seenCalls     map[string]struct{}
	classRegistry rubyClassRegistry
	root          *tree_sitter.Node
}

// rubyClassRegistry is the same-file, top-down transitive superclass view
// built incrementally in visitClass as classes are discovered. Ruby source is
// parsed top-down, so by the time a method's dead-code-root kind is computed,
// the registry already holds every class defined earlier in the file,
// including the method's own enclosing class. It backs the Rails controller
// superclass-chain walk in dead_code_roots.go and intentionally does not
// require a second AST walk.
//
// Known limitation — same-file short-name collisions: the maps are keyed by
// the class's simple (last-segment) name, because that is the only class
// identity readily available here. constantName already collapses a declared
// name to its last "::" segment (a pre-existing convention that also names the
// scope stack and the class_context field), and the method-side lookup in
// dead_code_roots.go likewise only has the enclosing class's collapsed name.
// So two classes defined in the SAME file whose short names collide across
// different namespaces (for example `Admin::BaseController` and
// `Api::BaseController`, both keyed as "BaseController"), or a reopened class,
// overwrite each other's superclass entry: the last one registered in source
// order wins, and a later class extending the bare short name resolves against
// whichever superclass was registered last. Keying by the fully-qualified
// module path is not done here because the qualified name is not available at
// both registration and lookup without threading it through the whole
// scope/context-resolution machinery (a change that would also move the
// pre-existing collapsed class_context semantics). Correct repo-wide,
// namespace- and reopening-aware resolution is the reducer follow-up #5376;
// this registry stays deliberately same-file and short-name-keyed.
type rubyClassRegistry struct {
	// superclass maps a class's simple (last-segment) name to its declared
	// superclass's full, possibly module-qualified name. A class with no
	// declared superclass has no entry. On a same-file short-name collision
	// the last registration in source order wins (see the type doc).
	superclass map[string]string
	// known holds every class simple name defined in the file so far,
	// regardless of whether it declares a superclass.
	known map[string]struct{}
}

// rubyBuildSyntax parses path with parser and returns the resolved syntax view.
func rubyBuildSyntax(source []byte, tree *tree_sitter.Tree, options shared.Options) *rubySyntax {
	syntax := &rubySyntax{
		source:    source,
		lines:     strings.Split(string(source), "\n"),
		seenCalls: make(map[string]struct{}),
		classRegistry: rubyClassRegistry{
			superclass: make(map[string]string),
			known:      make(map[string]struct{}),
		},
		root: tree.RootNode(),
	}
	seenVariables := make(map[string]struct{})
	syntax.walk(syntax.root, nil, "public", seenVariables, options)
	return syntax
}

func rubyScopeKindMatches(kind rubyScopeKind, kinds []rubyScopeKind) bool {
	for _, candidate := range kinds {
		if kind == candidate {
			return true
		}
	}
	return false
}

// walk descends node, recording scopes, definitions, imports, inclusions, and
// variable assignments in source order. visibility tracks the active
// public/private/protected state for the nearest enclosing class.
func (s *rubySyntax) walk(
	node *tree_sitter.Node,
	scopeStack []rubyScope,
	visibility string,
	seenVariables map[string]struct{},
	options shared.Options,
) {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return
	}
	for {
		child := cursor.Node()
		if child.IsNamed() {
			visibility = s.visit(child, scopeStack, visibility, seenVariables, options)
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
}

// visit handles one named node and returns the possibly-updated class
// visibility. Children of structural nodes are descended with an extended scope
// stack so nested context resolves correctly.
func (s *rubySyntax) visit(
	node *tree_sitter.Node,
	scopeStack []rubyScope,
	visibility string,
	seenVariables map[string]struct{},
	options shared.Options,
) string {
	switch node.Kind() {
	case "module":
		s.visitModule(node, scopeStack, seenVariables, options)
	case "class":
		s.visitClass(node, scopeStack, seenVariables, options)
	case "singleton_class":
		s.visitSingletonClass(node, scopeStack, seenVariables, options)
	case "method", "singleton_method":
		s.visitMethod(node, scopeStack, visibility, seenVariables, options)
	case "call":
		s.visitCall(node, scopeStack)
		s.recordCall(node, scopeStack)
		s.walk(node, scopeStack, visibility, seenVariables, options)
	case "assignment", "operator_assignment":
		s.visitAssignment(node, scopeStack, seenVariables)
		s.recordAssignmentCall(node, scopeStack)
		s.walk(node, scopeStack, visibility, seenVariables, options)
	case "identifier":
		if name := s.text(node); rubyVisibilityKeyword(name) != "" {
			return rubyVisibilityKeyword(name)
		}
	default:
		s.collectInstanceVariableRefs(node, scopeStack, seenVariables)
		s.walk(node, scopeStack, visibility, seenVariables, options)
	}
	return visibility
}

func (s *rubySyntax) visitModule(
	node *tree_sitter.Node,
	scopeStack []rubyScope,
	seenVariables map[string]struct{},
	options shared.Options,
) {
	name := s.constantName(node.ChildByFieldName("name"))
	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeEndLine(node),
		"lang":        "ruby",
	}
	s.modules = append(s.modules, item)
	scope := rubyScope{kind: rubyScopeModule, name: name}
	body := node.ChildByFieldName("body")
	if body != nil {
		s.walk(body, append(scopeStack, scope), "public", seenVariables, options)
	}
}

func (s *rubySyntax) visitClass(
	node *tree_sitter.Node,
	scopeStack []rubyScope,
	seenVariables map[string]struct{},
	options shared.Options,
) {
	name := s.constantName(node.ChildByFieldName("name"))
	item := map[string]any{
		"name":        name,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeEndLine(node),
		"lang":        "ruby",
		"type":        "class",
	}
	// qualified_name is the class's namespace-qualified name (#5376 F3), built
	// from the enclosing module/class scope prefix plus the raw name-node text
	// (which itself carries any compact "Admin::Base" spelling); a leading "::"
	// marks an absolute name and ignores the enclosing path. The reducer's
	// repo-wide controller registry keys on it so a base reference like
	// "Admin::Base" resolves to the right class instead of a same-last-segment
	// impostor. Emitted additively next to name; the last-segment name is kept
	// for existing consumers.
	if qualified := s.qualifiedClassName(node, scopeStack); qualified != "" {
		item["qualified_name"] = qualified
	}
	s.classRegistry.known[name] = struct{}{}
	if superclass := node.ChildByFieldName("superclass"); superclass != nil {
		if base := s.superclassName(superclass); base != "" {
			item["bases"] = []string{base}
		}
		if qualified := s.superclassQualifiedName(superclass); qualified != "" {
			s.classRegistry.superclass[name] = qualified
			// qualified_bases is the full, possibly module-qualified base
			// (e.g. "ActionController::Base"), emitted additively next to the
			// last-segment "bases" fact. The reducer's repo-wide #5376
			// code-root verdict builder needs the qualification: the persisted
			// "bases" fact collapses "ActionController::Base" to "Base", which
			// would make a reducer walk conflate a real controller base with an
			// unrelated class sharing the same last segment. Kept in-memory-only
			// before #5376; now persisted so cross-file resolution is possible.
			item["qualified_bases"] = []string{qualified}
		}
	}
	s.classes = append(s.classes, item)
	scope := rubyScope{kind: rubyScopeClass, name: name}
	body := node.ChildByFieldName("body")
	if body != nil {
		s.walk(body, append(scopeStack, scope), "public", seenVariables, options)
	}
}

func (s *rubySyntax) visitSingletonClass(
	node *tree_sitter.Node,
	scopeStack []rubyScope,
	seenVariables map[string]struct{},
	options shared.Options,
) {
	className := rubyEnclosingClassName(scopeStack)
	if className == "" {
		className = "self"
	}
	scope := rubyScope{kind: rubyScopeSingletonClass, name: className}
	body := node.ChildByFieldName("body")
	if body != nil {
		s.walk(body, append(scopeStack, scope), "public", seenVariables, options)
	}
}

func (s *rubySyntax) visitMethod(
	node *tree_sitter.Node,
	scopeStack []rubyScope,
	visibility string,
	seenVariables map[string]struct{},
	options shared.Options,
) {
	name := s.text(node.ChildByFieldName("name"))
	functionType := "instance"
	switch {
	case node.Kind() == "singleton_method" || rubyEnclosingSingletonClass(scopeStack):
		functionType = "singleton"
	case name == "method_missing" || name == "respond_to_missing?":
		functionType = "dynamic_dispatch"
	}
	item := map[string]any{
		"name":                  name,
		"line_number":           shared.NodeLine(node),
		"end_line":              shared.NodeEndLine(node),
		"lang":                  "ruby",
		"decorators":            []string{},
		"type":                  functionType,
		"args":                  s.methodArguments(node.ChildByFieldName("parameters")),
		"cyclomatic_complexity": rubyCyclomaticComplexity(node, s.source),
	}
	contextName, contextType := rubyEnclosingContext(scopeStack, rubyScopeClass, rubyScopeModule)
	resolvedVisibility := "public"
	if contextType == rubyScopeClass {
		resolvedVisibility = visibility
	}
	if contextName != "" {
		item["context"] = contextName
		item["context_type"] = string(contextType)
		if contextType == rubyScopeClass {
			item["class_context"] = contextName
		}
	}
	if rootKinds := rubyFunctionDefinitionRootKinds(name, functionType, contextName, string(contextType), resolvedVisibility, s.classRegistry); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	if options.IndexSource {
		item["source"] = s.rawLine(shared.NodeLine(node))
	}
	s.functions = append(s.functions, item)
	scope := rubyScope{kind: rubyScopeDef, name: name}
	body := node.ChildByFieldName("body")
	if body != nil {
		s.walk(body, append(scopeStack, scope), visibility, seenVariables, options)
	}
}

// visitCall records imports, module inclusions, and instance-variable
// references reachable from a call expression, then descends into arguments and
// receivers so nested calls are still observed by later passes.
func (s *rubySyntax) visitCall(node *tree_sitter.Node, scopeStack []rubyScope) {
	method := node.ChildByFieldName("method")
	receiver := node.ChildByFieldName("receiver")
	if receiver == nil && method != nil && method.Kind() == "identifier" {
		name := s.text(method)
		switch name {
		case "require", "require_relative", "load":
			if value := s.firstStringArgument(node); value != "" {
				s.imports = append(s.imports, map[string]any{
					"name":        value,
					"line_number": shared.NodeLine(node),
					"lang":        "ruby",
				})
			}
		case "include":
			if className := rubyEnclosingClassName(scopeStack); className != "" {
				if module := s.firstConstantArgument(node); module != "" {
					s.inclusions = append(s.inclusions, map[string]any{
						"class":  className,
						"module": module,
					})
				}
			}
		}
	}
}

func (s *rubySyntax) visitAssignment(node *tree_sitter.Node, scopeStack []rubyScope, seenVariables map[string]struct{}) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if left == nil {
		return
	}
	line := shared.NodeLine(left)
	switch left.Kind() {
	case "constant":
		s.appendVariable(scopeStack, seenVariables, s.constantName(left), rubyInferAssignmentType(s.text(right)), line)
	case "identifier":
		s.appendVariable(scopeStack, seenVariables, s.text(left), rubyInferAssignmentType(s.text(right)), line)
	case "instance_variable":
		s.appendVariable(scopeStack, seenVariables, s.text(left), rubyInferAssignmentType(s.text(right)), line)
	}
}

// collectInstanceVariableRefs records bare @ivar references (reads) so they are
// captured even when not assigned, matching the legacy line scan.
func (s *rubySyntax) collectInstanceVariableRefs(node *tree_sitter.Node, scopeStack []rubyScope, seenVariables map[string]struct{}) {
	if node.Kind() != "instance_variable" {
		return
	}
	s.appendVariable(scopeStack, seenVariables, s.text(node), "", shared.NodeLine(node))
}

func (s *rubySyntax) appendVariable(
	scopeStack []rubyScope,
	seenVariables map[string]struct{},
	name string,
	variableType string,
	line int,
) {
	if name == "" {
		return
	}
	if _, ok := seenVariables[name]; ok {
		return
	}
	seenVariables[name] = struct{}{}
	contextName, contextType := rubyEnclosingContext(scopeStack, rubyScopeClass, rubyScopeModule, rubyScopeDef)
	item := map[string]any{
		"name":        name,
		"line_number": line,
		"end_line":    line,
		"lang":        "ruby",
	}
	if variableType != "" {
		item["type"] = variableType
	}
	if contextName != "" {
		item["context"] = contextName
		item["context_type"] = string(contextType)
		if contextType == rubyScopeClass {
			item["class_context"] = contextName
		}
	}
	s.variables = append(s.variables, item)
}

func rubyEnclosingClassName(scopeStack []rubyScope) string {
	for index := len(scopeStack) - 1; index >= 0; index-- {
		if scopeStack[index].kind == rubyScopeClass {
			return scopeStack[index].name
		}
	}
	return ""
}

func rubyEnclosingSingletonClass(scopeStack []rubyScope) bool {
	for index := len(scopeStack) - 1; index >= 0; index-- {
		if scopeStack[index].kind == rubyScopeSingletonClass {
			return true
		}
	}
	return false
}

func rubyEnclosingContext(scopeStack []rubyScope, kinds ...rubyScopeKind) (string, rubyScopeKind) {
	for index := len(scopeStack) - 1; index >= 0; index-- {
		if rubyScopeKindMatches(scopeStack[index].kind, kinds) {
			return scopeStack[index].name, scopeStack[index].kind
		}
	}
	return "", ""
}
