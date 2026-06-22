package python

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pythonDecoratorCall returns the call node a decorator wraps, or nil when the
// decorator is a bare name (`@auth_required`).
func pythonDecoratorCall(decorator *tree_sitter.Node) *tree_sitter.Node {
	cursor := decorator.Walk()
	defer cursor.Close()
	for _, child := range decorator.NamedChildren(cursor) {
		child := child
		if child.Kind() == "call" {
			return shared.CloneNode(&child)
		}
	}
	return nil
}

// pythonDecoratorHandlerName returns the def name bound to a route decorator.
// The handler is the function_definition sibling inside the same
// decorated_definition. An orphaned decorator (parked under an ERROR node when
// no def follows) has no decorated_definition parent, so it stays unbound and
// "" is returned, matching the correlation-truth contract (#2788).
func pythonDecoratorHandlerName(decorator *tree_sitter.Node, source []byte) string {
	parent := decorator.Parent()
	if parent == nil || parent.Kind() != "decorated_definition" {
		return ""
	}
	definition := parent.ChildByFieldName("definition")
	if definition == nil || definition.Kind() != "function_definition" {
		return ""
	}
	return strings.TrimSpace(nodeText(definition.ChildByFieldName("name"), source))
}

// pythonCallSimpleName returns the bare callee name for a call function node:
// the identifier for `Flask`, or the trailing attribute for `module.create_app`.
func pythonCallSimpleName(function *tree_sitter.Node, source []byte) string {
	if function == nil {
		return ""
	}
	switch function.Kind() {
	case "identifier":
		return strings.TrimSpace(nodeText(function, source))
	case "attribute":
		return strings.TrimSpace(nodeText(function.ChildByFieldName("attribute"), source))
	default:
		return ""
	}
}

// pythonFirstPositionalString returns the first positional string argument of a
// call's argument_list (the route path), or "" when the first argument is not a
// string literal.
func pythonFirstPositionalString(call *tree_sitter.Node, source []byte) string {
	arguments := call.ChildByFieldName("arguments")
	if arguments == nil {
		return ""
	}
	first := pythonFirstNamedChild(arguments)
	if first == nil || first.Kind() != "string" {
		return ""
	}
	return pythonStringLiteralValue(first, source)
}

// pythonKeywordArgumentString returns the string value of a keyword argument in
// a call (e.g. `prefix="/api"`), or "" when absent or non-string.
func pythonKeywordArgumentString(call *tree_sitter.Node, name string, source []byte) string {
	value := pythonKeywordArgumentValue(call, name, source)
	if value == nil || value.Kind() != "string" {
		return ""
	}
	return pythonStringLiteralValue(value, source)
}

// pythonKeywordArgumentStringList returns the uppercased string entries of a
// keyword argument whose value is a list (e.g. `methods=["GET", "POST"]`).
func pythonKeywordArgumentStringList(call *tree_sitter.Node, name string, source []byte) []string {
	value := pythonKeywordArgumentValue(call, name, source)
	if value == nil || value.Kind() != "list" {
		return nil
	}
	methods := make([]string, 0)
	cursor := value.Walk()
	defer cursor.Close()
	for _, child := range value.NamedChildren(cursor) {
		child := child
		if child.Kind() != "string" {
			continue
		}
		literal := pythonStringLiteralValue(&child, source)
		if literal == "" {
			continue
		}
		methods = appendUniqueString(methods, strings.ToUpper(literal))
	}
	return methods
}

// pythonKeywordArgumentValue returns the value node for a named keyword argument
// in a call's argument_list, or nil when the keyword is absent.
func pythonKeywordArgumentValue(call *tree_sitter.Node, name string, source []byte) *tree_sitter.Node {
	arguments := call.ChildByFieldName("arguments")
	if arguments == nil {
		return nil
	}
	cursor := arguments.Walk()
	defer cursor.Close()
	for _, child := range arguments.NamedChildren(cursor) {
		child := child
		if child.Kind() != "keyword_argument" {
			continue
		}
		keyName := strings.TrimSpace(nodeText(child.ChildByFieldName("name"), source))
		if keyName != name {
			continue
		}
		return child.ChildByFieldName("value")
	}
	return nil
}

// pythonStringLiteralValue returns the inner text of a Python string node by
// joining its string_content children, so quotes and prefixes are excluded.
func pythonStringLiteralValue(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() == "string_content" {
			builder.WriteString(nodeText(&child, source))
		}
	}
	return builder.String()
}
