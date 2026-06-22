package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// phpDeadCodeFacts accumulates the bounded, same-file evidence used to classify
// PHP dead-code root kinds: declared type kinds and bases, interface method
// signatures, literal route handler targets, Symfony route attribute lines, and
// WordPress hook callback names.
type phpDeadCodeFacts struct {
	typeKinds                 map[string]string
	typeBases                 map[string][]string
	interfaceMethods          map[string]map[phpMethodKey]struct{}
	routeMethodTargets        map[string]map[string]struct{}
	symfonyRouteAttributeLine map[int]struct{}
	wordpressFunctionTargets  map[string]struct{}
}

type phpMethodKey struct {
	name  string
	arity int
}

// phpDeadCodeFunctionFact stages a function payload row for dead-code root
// classification after every file-level fact has been observed.
type phpDeadCodeFunctionFact struct {
	item        map[string]any
	name        string
	contextName string
	contextKind string
	lineNumber  int
	parameters  []string
	isPublic    bool
}

func newPHPDeadCodeFacts() phpDeadCodeFacts {
	return phpDeadCodeFacts{
		typeKinds:                 map[string]string{},
		typeBases:                 map[string][]string{},
		interfaceMethods:          map[string]map[phpMethodKey]struct{}{},
		routeMethodTargets:        map[string]map[string]struct{}{},
		symfonyRouteAttributeLine: map[int]struct{}{},
		wordpressFunctionTargets:  map[string]struct{}{},
	}
}

func recordPHPDeadCodeType(facts phpDeadCodeFacts, kind string, name string, bases []string) {
	if name == "" {
		return
	}
	facts.typeKinds[name] = kind
	if len(bases) > 0 {
		facts.typeBases[name] = dedupePHPNonEmptyStrings(append(facts.typeBases[name], bases...))
	}
}

func recordPHPDeadCodeFunction(
	facts phpDeadCodeFacts,
	name string,
	contextName string,
	contextKind string,
	parameters []string,
) {
	if contextKind != "interface_declaration" {
		return
	}
	if facts.interfaceMethods[contextName] == nil {
		facts.interfaceMethods[contextName] = map[phpMethodKey]struct{}{}
	}
	facts.interfaceMethods[contextName][phpMethodKey{name: name, arity: len(parameters)}] = struct{}{}
}

// observePHPAttribute records the line of any method carrying a Symfony Route
// attribute so route-backed controller actions can be rooted.
func observePHPAttribute(state *phpParseState, node *tree_sitter.Node) {
	nameNode := phpAttributeNameNode(node)
	if nameNode == nil {
		return
	}
	if !phpIsSymfonyRouteAttribute(strings.TrimSpace(shared.NodeText(nameNode, state.source))) {
		return
	}
	method := phpAttributeOwningMethod(node)
	if method == nil {
		return
	}
	state.deadCodeFacts.symfonyRouteAttributeLine[shared.NodeLine(phpNameNode(method))] = struct{}{}
}

// observePHPWordPressHookCall records the WordPress hook callback name from an
// add_action/add_filter free-function call.
func observePHPWordPressHookCall(state *phpParseState, node *tree_sitter.Node) {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "name" {
		return
	}
	name := strings.TrimSpace(shared.NodeText(functionNode, state.source))
	if name != "add_action" && name != "add_filter" {
		return
	}
	collectPHPWordPressHookTarget(state, node)
}

// collectPHPLiteralRouteTarget records a `[Class::class, 'method']` array
// literal as a route handler target, regardless of the surrounding call kind.
func collectPHPLiteralRouteTarget(state *phpParseState, node *tree_sitter.Node) {
	className, methodName := phpClassMethodArray(node, state.source)
	if className == "" || methodName == "" {
		return
	}
	if state.deadCodeFacts.routeMethodTargets[className] == nil {
		state.deadCodeFacts.routeMethodTargets[className] = map[string]struct{}{}
	}
	state.deadCodeFacts.routeMethodTargets[className][methodName] = struct{}{}
}

// collectPHPWordPressHookTarget records the callback name string passed as the
// second argument of an add_action/add_filter call.
func collectPHPWordPressHookTarget(state *phpParseState, node *tree_sitter.Node) {
	args := phpArgumentsNode(node)
	if args == nil {
		return
	}
	cursor := args.Walk()
	defer cursor.Close()
	index := 0
	for _, child := range args.NamedChildren(cursor) {
		child := child
		if child.Kind() != "argument" {
			continue
		}
		if index == 1 {
			if name := phpStringArgumentLiteral(&child, state.source); name != "" {
				state.deadCodeFacts.wordpressFunctionTargets[name] = struct{}{}
			}
			return
		}
		index++
	}
}

// phpClassMethodArray returns the class and method names from a two-element
// `[Class::class, 'method']` array literal.
func phpClassMethodArray(node *tree_sitter.Node, source []byte) (string, string) {
	cursor := node.Walk()
	defer cursor.Close()
	elements := make([]*tree_sitter.Node, 0, 2)
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "array_element_initializer" {
			elements = append(elements, shared.CloneNode(&child))
		}
	}
	if len(elements) != 2 {
		return "", ""
	}
	className := phpClassConstantClassName(elements[0], source)
	methodName := phpStringLiteralValue(elements[1], source)
	return className, methodName
}

// phpClassConstantClassName returns the normalized class name from a
// `Class::class` constant access expression nested in an array element.
func phpClassConstantClassName(node *tree_sitter.Node, source []byte) string {
	var className string
	shared.WalkNamed(node, func(candidate *tree_sitter.Node) {
		if className != "" || candidate.Kind() != "class_constant_access_expression" {
			return
		}
		text := strings.TrimSpace(shared.NodeText(candidate, source))
		if !strings.HasSuffix(text, "::class") {
			return
		}
		className = normalizePHPTypeName(strings.TrimSuffix(text, "::class"))
	})
	return className
}

// phpStringLiteralValue returns the unquoted content of the first string literal
// under a node.
func phpStringLiteralValue(node *tree_sitter.Node, source []byte) string {
	var value string
	shared.WalkNamed(node, func(candidate *tree_sitter.Node) {
		if value != "" {
			return
		}
		switch candidate.Kind() {
		case "string", "encapsed_string":
			value = phpUnquoteString(shared.NodeText(candidate, source))
		}
	})
	return value
}

// phpStringArgumentLiteral returns the unquoted content of a single string
// literal argument, or the empty string when the argument is not a string.
func phpStringArgumentLiteral(node *tree_sitter.Node, source []byte) string {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "string", "encapsed_string":
			return phpUnquoteString(shared.NodeText(&child, source))
		}
	}
	return ""
}

func phpUnquoteString(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) >= 2 {
		first := trimmed[0]
		last := trimmed[len(trimmed)-1]
		if (first == '\'' || first == '"') && first == last {
			return trimmed[1 : len(trimmed)-1]
		}
	}
	return trimmed
}

func phpAttributeNameNode(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "name", "qualified_name":
			return shared.CloneNode(&child)
		}
	}
	return nil
}

// phpAttributeOwningMethod returns the method or function declaration that the
// attribute group annotates.
func phpAttributeOwningMethod(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "method_declaration", "function_definition":
			return current
		}
	}
	return nil
}

// phpIsSymfonyRouteAttribute reports whether an attribute name resolves to a
// Symfony routing Route attribute.
func phpIsSymfonyRouteAttribute(name string) bool {
	normalized := strings.TrimPrefix(strings.TrimSpace(name), `\`)
	switch normalized {
	case "Route",
		"Symfony\\Component\\Routing\\Annotation\\Route",
		"Symfony\\Component\\Routing\\Attribute\\Route":
		return true
	default:
		return false
	}
}

// phpDeadCodeRootKinds returns the bounded dead-code root kinds for a function
// or method given the observed file-level facts.
func phpDeadCodeRootKinds(
	name string,
	contextName string,
	contextKind string,
	lineNumber int,
	parameters []string,
	isPublic bool,
	facts phpDeadCodeFacts,
) []string {
	var rootKinds []string
	methodKey := phpMethodKey{name: name, arity: len(parameters)}
	if contextKind == "" && name == "main" {
		rootKinds = append(rootKinds, "php.script_entrypoint")
	}
	if _, ok := facts.wordpressFunctionTargets[name]; ok && contextKind == "" {
		rootKinds = append(rootKinds, "php.wordpress_hook_callback")
	}
	if contextKind == "interface_declaration" {
		rootKinds = append(rootKinds, "php.interface_method")
	}
	if contextKind == "trait_declaration" {
		rootKinds = append(rootKinds, "php.trait_method")
	}
	if contextKind == "class_declaration" {
		_, routeBacked := facts.routeMethodTargets[contextName][name]
		_, attributeBacked := facts.symfonyRouteAttributeLine[lineNumber]
		if name == "__construct" {
			rootKinds = append(rootKinds, "php.constructor")
		}
		if phpIsMagicMethod(name) {
			rootKinds = append(rootKinds, "php.magic_method")
		}
		if phpClassImplementsInterfaceMethod(contextName, methodKey, facts) {
			rootKinds = append(rootKinds, "php.interface_implementation_method")
		}
		if phpIsControllerAction(contextName, name, isPublic, routeBacked || attributeBacked) {
			rootKinds = append(rootKinds, "php.framework_controller_action")
		}
		if routeBacked {
			rootKinds = append(rootKinds, "php.route_handler")
		}
		if attributeBacked {
			rootKinds = append(rootKinds, "php.symfony_route_attribute")
		}
	}
	return dedupePHPNonEmptyStrings(rootKinds)
}

func phpClassImplementsInterfaceMethod(className string, methodKey phpMethodKey, facts phpDeadCodeFacts) bool {
	for _, base := range facts.typeBases[className] {
		if facts.typeKinds[base] != "interface_declaration" {
			continue
		}
		if phpInterfaceHasMethod(base, methodKey, facts, map[string]struct{}{}) {
			return true
		}
	}
	return false
}

func phpInterfaceHasMethod(interfaceName string, methodKey phpMethodKey, facts phpDeadCodeFacts, seen map[string]struct{}) bool {
	if _, ok := seen[interfaceName]; ok {
		return false
	}
	seen[interfaceName] = struct{}{}
	if _, ok := facts.interfaceMethods[interfaceName][methodKey]; ok {
		return true
	}
	for _, base := range facts.typeBases[interfaceName] {
		if facts.typeKinds[base] == "interface_declaration" && phpInterfaceHasMethod(base, methodKey, facts, seen) {
			return true
		}
	}
	return false
}

func phpIsControllerAction(contextName string, name string, isPublic bool, routeBacked bool) bool {
	if !strings.HasSuffix(contextName, "Controller") || strings.HasPrefix(name, "__") {
		return false
	}
	if !routeBacked {
		return false
	}
	return isPublic
}

func phpIsMagicMethod(name string) bool {
	switch strings.ToLower(name) {
	case "__construct", "__destruct", "__call", "__callstatic", "__get", "__set",
		"__isset", "__unset", "__sleep", "__wakeup", "__serialize", "__unserialize",
		"__tostring", "__invoke", "__set_state", "__clone", "__debuginfo":
		return true
	default:
		return false
	}
}
