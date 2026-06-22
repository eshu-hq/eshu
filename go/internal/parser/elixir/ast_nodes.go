package elixir

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// elixirHasImplDecorator reports whether any decorator is an @impl attribute,
// which marks a behaviour callback definition.
func elixirHasImplDecorator(decorators []string) bool {
	for _, decorator := range decorators {
		if elixirDecoratorIsImpl(strings.TrimSpace(decorator)) {
			return true
		}
	}
	return false
}

// elixirDecoratorIsImpl reports whether a decorator text is an @impl attribute,
// distinguishing it from same-prefix attributes such as @implementation.
func elixirDecoratorIsImpl(decorator string) bool {
	if decorator == "@impl" {
		return true
	}
	if !strings.HasPrefix(decorator, "@impl") {
		return false
	}
	switch decorator[len("@impl")] {
	case ' ', '\t', '(':
		return true
	default:
		return false
	}
}

// elixirCallHasParenArguments reports whether a call node has a parenthesized
// argument list. The prior extraction required a literal `(` after the call
// name, so control-flow forms such as `case value do` and `for x <- ...`, whose
// arguments node is not wrapped in parentheses, are not calls.
func elixirCallHasParenArguments(node *tree_sitter.Node, source []byte) bool {
	arguments := elixirFirstChildOfKind(node, "arguments")
	if arguments == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(shared.NodeText(arguments, source)), "(")
}

// elixirImportKeyword reports whether head introduces an import-style directive.
func elixirImportKeyword(head string) bool {
	switch head {
	case "use", "import", "alias", "require":
		return true
	default:
		return false
	}
}

// elixirNodeText returns the trimmed source text covered by a node.
func elixirNodeText(node *tree_sitter.Node, source []byte) string {
	return strings.TrimSpace(shared.NodeText(node, source))
}

// elixirNodeSource returns the source slice for a definition node extended back
// to the start of its first line so leading indentation is preserved. This
// matches the line-based source spans the former extractor produced for module
// and function rows.
func elixirNodeSource(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	start := int(node.StartByte())
	end := int(node.EndByte())
	if start < 0 || end > len(source) || start > end {
		return ""
	}
	lineStart := start
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}
	return string(source[lineStart:end])
}

// elixirCallEndLine returns the 1-based end line of a definition call node,
// equal to the node span end the AST reports for the whole definition.
func elixirCallEndLine(node *tree_sitter.Node) int {
	return shared.NodeEndLine(node)
}

// elixirSameNode reports whether two nodes cover the same byte range, used to
// skip the definition target while walking a function body.
func elixirSameNode(left *tree_sitter.Node, right *tree_sitter.Node) bool {
	if left == nil || right == nil {
		return false
	}
	return left.StartByte() == right.StartByte() && left.EndByte() == right.EndByte()
}

// elixirCallTarget returns the receiver alias and call name for a call node.
// A dotted head yields the alias receiver and trailing identifier; a bare
// identifier head yields an empty receiver and the identifier name.
func elixirCallTarget(node *tree_sitter.Node, source []byte) (string, string) {
	children := elixirNamedChildren(node)
	if len(children) == 0 {
		return "", ""
	}
	head := children[0]
	switch head.Kind() {
	case "identifier", "operator_identifier":
		return "", elixirNodeText(&head, source)
	case "dot":
		return elixirDotReceiverName(&head, source)
	default:
		return "", ""
	}
}

// elixirDotReceiverName splits a dotted call head into its receiver alias text
// and trailing member identifier. Only module-alias receivers (uppercase) yield
// a scoped call, matching the prior `Receiver.name(` extraction; lowercase
// variable receivers such as `state.value(...)` are not scoped calls.
func elixirDotReceiverName(dot *tree_sitter.Node, source []byte) (string, string) {
	children := elixirNamedChildren(dot)
	if len(children) < 2 {
		return "", ""
	}
	receiverNode := children[0]
	if receiverNode.Kind() != "alias" {
		return "", ""
	}
	member := children[len(children)-1]
	if member.Kind() == "alias" {
		// A trailing alias (Foo.Bar) is a module reference, not a call name.
		return "", ""
	}
	return elixirNodeText(&receiverNode, source), elixirNodeText(&member, source)
}

// elixirImportPaths returns the module paths introduced by an import directive,
// expanding alias brace groups such as `alias Demo.{Basic, Worker}`.
func elixirImportPaths(node *tree_sitter.Node, keyword string, source []byte) []string {
	arguments := elixirFirstChildOfKind(node, "arguments")
	if arguments == nil {
		return nil
	}
	first := elixirFirstArgument(arguments)
	if first == nil {
		return nil
	}
	if keyword == "alias" {
		return expandAliasPaths(elixirNodeText(first, source))
	}
	if text := elixirNodeText(first, source); text != "" {
		return []string{text}
	}
	return nil
}

// elixirFirstArgument returns the first named child of an arguments node.
func elixirFirstArgument(arguments *tree_sitter.Node) *tree_sitter.Node {
	children := elixirNamedChildren(arguments)
	if len(children) == 0 {
		return nil
	}
	first := children[0]
	return &first
}

// elixirFirstChildOfKind returns the first named child matching kind.
func elixirFirstChildOfKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	for _, child := range elixirNamedChildren(node) {
		child := child
		if child.Kind() == kind {
			return &child
		}
	}
	return nil
}

// elixirAttribute returns the @name and value text for a module-attribute
// unary_operator node and whether the node is a module attribute.
func elixirAttribute(node *tree_sitter.Node, source []byte) (string, string, bool) {
	if node.Kind() != "unary_operator" {
		return "", "", false
	}
	if !strings.HasPrefix(elixirNodeText(node, source), "@") {
		return "", "", false
	}
	inner := elixirFirstChildOfKind(node, "call")
	if inner == nil {
		return elixirBareAttribute(node, source)
	}
	name := elixirCallHead(inner, source)
	if name == "" {
		return "", "", false
	}
	arguments := elixirFirstChildOfKind(inner, "arguments")
	value := ""
	if arguments != nil {
		value = elixirNodeText(arguments, source)
	}
	return "@" + name, value, true
}

// elixirBareAttribute handles attribute references with no value, returning the
// @name when the unary operator wraps a plain identifier.
func elixirBareAttribute(node *tree_sitter.Node, source []byte) (string, string, bool) {
	identifier := elixirFirstChildOfKind(node, "identifier")
	if identifier == nil {
		return "", "", false
	}
	return "@" + elixirNodeText(identifier, source), "", true
}

// elixirDefImplTarget returns the module named by the `for:` option of a defimpl
// call, e.g. `defimpl P, for: Demo.User` yields `Demo.User`.
func elixirDefImplTarget(node *tree_sitter.Node, source []byte) string {
	arguments := elixirFirstChildOfKind(node, "arguments")
	if arguments == nil {
		return ""
	}
	keywords := elixirFirstChildOfKind(arguments, "keywords")
	if keywords == nil {
		return ""
	}
	for _, pair := range elixirNamedChildren(keywords) {
		pair := pair
		if pair.Kind() != "pair" {
			continue
		}
		children := elixirNamedChildren(&pair)
		if len(children) < 2 {
			continue
		}
		if strings.TrimSpace(strings.TrimSuffix(elixirNodeText(&children[0], source), ":")) != "for" {
			continue
		}
		return elixirNodeText(&children[1], source)
	}
	return ""
}

// elixirGuardExpression returns the guard predicate subtree of a definition when
// the target carries a `when` clause, otherwise nil.
func elixirGuardExpression(node *tree_sitter.Node, target *tree_sitter.Node) *tree_sitter.Node {
	arguments := elixirFirstChildOfKind(node, "arguments")
	if arguments == nil {
		return nil
	}
	for _, child := range elixirNamedChildren(arguments) {
		child := child
		if child.Kind() != "binary_operator" {
			continue
		}
		inner := elixirNamedChildren(&child)
		if len(inner) < 2 {
			continue
		}
		if target != nil && !elixirSameNode(&inner[0], target) {
			continue
		}
		guard := inner[len(inner)-1]
		return &guard
	}
	return nil
}

// decoratorsBefore returns the @decorator attribute texts immediately preceding
// a definition node among its parent's named children.
func (e *elixirExtractor) decoratorsBefore(node *tree_sitter.Node) []string {
	siblings := e.precedingSiblings(node)
	decorators := make([]string, 0)
	for index := len(siblings) - 1; index >= 0; index-- {
		sibling := siblings[index]
		text := elixirNodeText(&sibling, e.source)
		if sibling.Kind() != "unary_operator" || !strings.HasPrefix(text, "@") {
			break
		}
		decorators = append(decorators, text)
	}
	slices.Reverse(decorators)
	return decorators
}

// docstringBefore returns the @doc, @moduledoc, or comment text immediately
// preceding a definition node, matching the former previous-line docstring rule.
func (e *elixirExtractor) docstringBefore(node *tree_sitter.Node) string {
	siblings := e.precedingSiblings(node)
	if len(siblings) == 0 {
		return ""
	}
	previous := siblings[len(siblings)-1]
	text := elixirNodeText(&previous, e.source)
	if strings.HasPrefix(text, "@doc") || strings.HasPrefix(text, "@moduledoc") || strings.HasPrefix(text, "#") {
		return text
	}
	return ""
}

// precedingSiblings returns the named siblings that appear before node within
// its parent, in source order.
func (e *elixirExtractor) precedingSiblings(node *tree_sitter.Node) []tree_sitter.Node {
	parent := node.Parent()
	if parent == nil {
		return nil
	}
	siblings := make([]tree_sitter.Node, 0)
	for _, child := range elixirNamedChildren(parent) {
		child := child
		if elixirSameNode(&child, node) {
			break
		}
		siblings = append(siblings, child)
	}
	return siblings
}
