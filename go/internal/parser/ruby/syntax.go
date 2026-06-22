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

// rubyScope is one lexical block discovered in the AST. startLine and endLine
// are 1-based and inclusive; endLine is the line of the matching `end`.
type rubyScope struct {
	kind      rubyScopeKind
	name      string
	startLine int
	endLine   int
	item      map[string]any
}

// rubySyntax is the ordered, fully-resolved view of one Ruby source tree. It
// captures the structural facts (scopes, definitions, imports, inclusions,
// variables) the line-oriented passes need to attach context without rescanning
// the AST.
type rubySyntax struct {
	source         []byte
	lines          []string
	scopes         []rubyScope
	functions      []map[string]any
	classes        []map[string]any
	modules        []map[string]any
	variables      []map[string]any
	imports        []map[string]any
	inclusions     []map[string]any
	root           *tree_sitter.Node
}

// rubyBuildSyntax parses path with parser and returns the resolved syntax view.
func rubyBuildSyntax(source []byte, tree *tree_sitter.Tree, options shared.Options) *rubySyntax {
	syntax := &rubySyntax{
		source: source,
		lines:  strings.Split(string(source), "\n"),
		root:   tree.RootNode(),
	}
	seenVariables := make(map[string]struct{})
	syntax.walk(syntax.root, nil, "public", seenVariables, options)
	return syntax
}

// contextAt returns the name and type of the nearest enclosing scope at line
// from the given kinds, mirroring the legacy stack-based context resolution.
func (s *rubySyntax) contextAt(line int, kinds ...rubyScopeKind) (string, rubyScopeKind) {
	best := -1
	for index := range s.scopes {
		scope := &s.scopes[index]
		if line < scope.startLine || line > scope.endLine {
			continue
		}
		if !rubyScopeKindMatches(scope.kind, kinds) {
			continue
		}
		if best == -1 || scope.startLine >= s.scopes[best].startLine {
			best = index
		}
	}
	if best == -1 {
		return "", ""
	}
	return s.scopes[best].name, s.scopes[best].kind
}

// structuralStartLines returns the set of 1-based line numbers that open a
// module, class, singleton class, or method definition. The legacy parser
// consumed those lines structurally and never scanned them for calls, so the
// call pass skips them to keep parity.
func (s *rubySyntax) structuralStartLines() map[int]struct{} {
	lines := make(map[int]struct{}, len(s.scopes))
	for index := range s.scopes {
		lines[s.scopes[index].startLine] = struct{}{}
	}
	return lines
}

// classNameAt returns the nearest enclosing class name at line, or empty.
func (s *rubySyntax) classNameAt(line int) string {
	name, _ := s.contextAt(line, rubyScopeClass)
	return name
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
		s.walk(node, scopeStack, visibility, seenVariables, options)
	case "assignment", "operator_assignment":
		s.visitAssignment(node, scopeStack, seenVariables)
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
	scope := rubyScope{kind: rubyScopeModule, name: name, startLine: shared.NodeLine(node), endLine: shared.NodeEndLine(node), item: item}
	s.scopes = append(s.scopes, scope)
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
	if superclass := node.ChildByFieldName("superclass"); superclass != nil {
		if base := s.superclassName(superclass); base != "" {
			item["bases"] = []string{base}
		}
	}
	s.classes = append(s.classes, item)
	scope := rubyScope{kind: rubyScopeClass, name: name, startLine: shared.NodeLine(node), endLine: shared.NodeEndLine(node), item: item}
	s.scopes = append(s.scopes, scope)
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
	scope := rubyScope{kind: rubyScopeSingletonClass, name: className, startLine: shared.NodeLine(node), endLine: shared.NodeEndLine(node)}
	s.scopes = append(s.scopes, scope)
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
		"name":        name,
		"line_number": shared.NodeLine(node),
		"end_line":    shared.NodeEndLine(node),
		"lang":        "ruby",
		"decorators":  []string{},
		"type":        functionType,
		"args":        s.methodArguments(node.ChildByFieldName("parameters")),
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
	if rootKinds := rubyFunctionDefinitionRootKinds(name, functionType, contextName, string(contextType), resolvedVisibility); len(rootKinds) > 0 {
		item["dead_code_root_kinds"] = rootKinds
	}
	if options.IndexSource {
		item["source"] = s.rawLine(shared.NodeLine(node))
	}
	s.functions = append(s.functions, item)
	scope := rubyScope{kind: rubyScopeDef, name: name, startLine: shared.NodeLine(node), endLine: shared.NodeEndLine(node), item: item}
	s.scopes = append(s.scopes, scope)
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

func (s *rubySyntax) text(node *tree_sitter.Node) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(shared.NodeText(node, s.source))
}

func (s *rubySyntax) rawLine(line int) string {
	if line <= 0 || line > len(s.lines) {
		return ""
	}
	return s.lines[line-1]
}

// constantName returns the last `::` segment of a constant or scope_resolution
// node, matching the legacy last-segment behavior.
func (s *rubySyntax) constantName(node *tree_sitter.Node) string {
	if node == nil {
		return ""
	}
	return shared.LastPathSegment(s.text(node), "::")
}

// superclassName returns the base constant name from a (superclass) node.
func (s *rubySyntax) superclassName(node *tree_sitter.Node) string {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return ""
	}
	for {
		child := cursor.Node()
		if child.IsNamed() {
			switch child.Kind() {
			case "constant", "scope_resolution":
				return shared.LastPathSegment(s.text(child), "::")
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return ""
}

// methodArguments returns normalized parameter names from a (method_parameters)
// node, matching legacy argument normalization.
func (s *rubySyntax) methodArguments(node *tree_sitter.Node) []string {
	if node == nil {
		return []string{}
	}
	args := make([]string, 0)
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return args
	}
	for {
		child := cursor.Node()
		if child.IsNamed() {
			if name := s.parameterName(child); name != "" {
				args = append(args, name)
			}
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return args
}

func (s *rubySyntax) parameterName(node *tree_sitter.Node) string {
	switch node.Kind() {
	case "identifier":
		return s.text(node)
	case "optional_parameter", "keyword_parameter", "splat_parameter",
		"hash_splat_parameter", "block_parameter", "splat_argument":
		if name := node.ChildByFieldName("name"); name != nil {
			return s.text(name)
		}
		return rubyNormalizeArgument(s.text(node))
	default:
		return rubyNormalizeArgument(s.text(node))
	}
}

func (s *rubySyntax) firstStringArgument(node *tree_sitter.Node) string {
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	cursor := args.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return ""
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && child.Kind() == "string" {
			return s.stringContent(child)
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return ""
}

func (s *rubySyntax) firstConstantArgument(node *tree_sitter.Node) string {
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	cursor := args.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return ""
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && (child.Kind() == "constant" || child.Kind() == "scope_resolution") {
			return shared.LastPathSegment(s.text(child), "::")
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return ""
}

// stringContent returns the (string_content) child text of a (string) node.
func (s *rubySyntax) stringContent(node *tree_sitter.Node) string {
	cursor := node.Walk()
	defer cursor.Close()
	if !cursor.GotoFirstChild() {
		return ""
	}
	for {
		child := cursor.Node()
		if child.IsNamed() && child.Kind() == "string_content" {
			return s.text(child)
		}
		if !cursor.GotoNextSibling() {
			break
		}
	}
	return ""
}
