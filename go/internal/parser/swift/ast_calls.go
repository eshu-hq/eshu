package swift

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// swiftIgnoredCallNames are control-flow and declaration heads the grammar never
// models as call_expression nodes but which the prior plain-call scanner had to
// exclude by name. Keeping the set documents the intent even though the AST walk
// only reaches genuine call targets.
var swiftIgnoredCallNames = map[string]struct{}{
	"func":   {},
	"init":   {},
	"if":     {},
	"switch": {},
	"return": {},
}

// handleCall records a function-call row for a call_expression node. Swift models
// a plain call as `simple_identifier call_suffix` and a receiver call as
// `navigation_expression call_suffix`, where the navigation expression supplies a
// receiver identifier and a navigation_suffix method name. Calls are deduplicated
// by full name and line so the same call text on one line is recorded once.
func (e *swiftExtractor) handleCall(node *tree_sitter.Node) {
	receiver, name := swiftCallTarget(node, e.source)
	if name == "" {
		return
	}
	if _, ignored := swiftIgnoredCallNames[name]; ignored && receiver == "" {
		return
	}

	fullName := name
	if receiver != "" {
		fullName = receiver + "." + name
	}
	callKey := fullName + ":" + strconv.Itoa(shared.NodeLine(node))
	if _, ok := e.seenCalls[callKey]; ok {
		return
	}
	e.seenCalls[callKey] = struct{}{}

	item := map[string]any{
		"name":          name,
		"full_name":     fullName,
		"line_number":   shared.NodeLine(node),
		"args":          swiftCallArguments(node, e.source),
		"lang":          "swift",
		"is_dependency": e.isDependency,
	}
	if receiver != "" {
		item["inferred_obj_type"] = e.variableTypes[receiver]
	}
	shared.AppendBucket(e.payload, "function_calls", item)
}

// swiftCallTarget returns the receiver and method name for a call_expression. A
// plain call has no receiver; a receiver call resolves the receiver from the
// navigation expression's leading simple_identifier and the method from the
// navigation suffix. Calls whose callee is itself a call or a non-identifier
// expression yield an empty name and are skipped.
func swiftCallTarget(node *tree_sitter.Node, source []byte) (string, string) {
	for _, child := range swiftNamedChildren(node) {
		child := child
		switch child.Kind() {
		case "simple_identifier":
			return "", strings.TrimSpace(shared.NodeText(&child, source))
		case "navigation_expression":
			return swiftNavigationTarget(&child, source)
		}
	}
	return "", ""
}

// swiftNavigationTarget resolves the receiver identifier and trailing method name
// from a navigation_expression (`receiver.method`). A receiver that is not a bare
// identifier (for example a chained call) yields an empty receiver so the call is
// still recorded by its method name without a misleading inferred type.
func swiftNavigationTarget(node *tree_sitter.Node, source []byte) (string, string) {
	var receiver string
	var name string
	for _, child := range swiftNamedChildren(node) {
		child := child
		switch child.Kind() {
		case "simple_identifier", "super_expression", "self_expression":
			receiver = strings.TrimSpace(shared.NodeText(&child, source))
		case "navigation_suffix":
			identifier := swiftFirstChildOfKind(&child, "simple_identifier")
			if identifier != nil {
				name = strings.TrimSpace(shared.NodeText(identifier, source))
			}
		}
	}
	return receiver, name
}

// swiftCallArguments returns the trimmed source text of each value_argument in a
// call's value_arguments suffix. Argument text is preserved verbatim (labels,
// interpolations, and expressions) to match the call-graph argument contract.
func swiftCallArguments(node *tree_sitter.Node, source []byte) []string {
	suffix := swiftFirstChildOfKind(node, "call_suffix")
	if suffix == nil {
		return nil
	}
	arguments := swiftFirstChildOfKind(suffix, "value_arguments")
	if arguments == nil {
		return nil
	}
	args := make([]string, 0, 2)
	for _, child := range swiftNamedChildren(arguments) {
		child := child
		if child.Kind() != "value_argument" {
			continue
		}
		text := strings.TrimSpace(shared.NodeText(&child, source))
		if text != "" {
			args = append(args, text)
		}
	}
	return args
}
