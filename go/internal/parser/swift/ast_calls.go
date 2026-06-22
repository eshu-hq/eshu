package swift

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// emitCall appends one function-call row for a call_expression node. A call
// whose callee is a navigation_expression (`receiver.method(...)`) emits a
// receiver row carrying full_name `receiver.method` and inferred_obj_type from
// the recorded variable type. A call whose callee is a bare simple_identifier
// emits a plain row with full_name equal to the name and no inferred_obj_type
// key. Calls are deduplicated by full_name and source line so repeated text on
// the same line collapses to one row, matching the prior extractor.
func (b *swiftPayloadBuilder) emitCall(node *tree_sitter.Node, source []byte) {
	callee := swiftCallCallee(node)
	if callee == nil {
		return
	}
	lineText := swiftCallLineText(node, source)
	switch callee.Kind() {
	case "navigation_expression":
		b.emitReceiverCall(node, callee, source, lineText)
	case "simple_identifier":
		b.emitPlainCall(node, callee, source, lineText)
	}
}

func (b *swiftPayloadBuilder) emitReceiverCall(node *tree_sitter.Node, callee *tree_sitter.Node, source []byte, lineText string) {
	target := callee.ChildByFieldName("target")
	suffix := callee.ChildByFieldName("suffix")
	if target == nil || suffix == nil {
		return
	}
	receiver := swiftReceiverName(target, source)
	if receiver == "" {
		return
	}
	method := suffix.ChildByFieldName("suffix")
	if method == nil || method.Kind() != "simple_identifier" {
		return
	}
	name := swiftTrimText(method, source)
	if name == "" {
		return
	}
	fullName := receiver + "." + name
	if !b.markCall(fullName, lineText) {
		return
	}
	shared.AppendBucket(b.payload, "function_calls", map[string]any{
		"name":              name,
		"full_name":         fullName,
		"line_number":       shared.NodeLine(node),
		"args":              swiftCallArguments(node, source),
		"inferred_obj_type": b.variableTypes[receiver],
		"lang":              "swift",
		"is_dependency":     b.isDependency,
	})
}

func (b *swiftPayloadBuilder) emitPlainCall(node *tree_sitter.Node, callee *tree_sitter.Node, source []byte, lineText string) {
	name := swiftTrimText(callee, source)
	if name == "" {
		return
	}
	if !b.markCall(name, lineText) {
		return
	}
	shared.AppendBucket(b.payload, "function_calls", map[string]any{
		"name":          name,
		"full_name":     name,
		"line_number":   shared.NodeLine(node),
		"args":          swiftCallArguments(node, source),
		"lang":          "swift",
		"is_dependency": b.isDependency,
	})
}

// markCall records the call dedup key and reports whether the row is new.
func (b *swiftPayloadBuilder) markCall(fullName string, lineText string) bool {
	key := fullName + ":" + lineText
	if _, seen := b.seenCalls[key]; seen {
		return false
	}
	b.seenCalls[key] = struct{}{}
	return true
}

// swiftReceiverName returns the receiver token for a navigation target. A
// simple_identifier receiver (a variable or type name) returns its text; the
// `super` and `self` keyword receivers return "super"/"self" so genuine
// `super.method(...)` and `self.method(...)` calls are preserved. Other target
// shapes (chained navigation, subscript, literals) are not modeled as a single
// receiver and return "".
func swiftReceiverName(target *tree_sitter.Node, source []byte) string {
	switch target.Kind() {
	case "simple_identifier":
		return swiftTrimText(target, source)
	case "super_expression":
		return "super"
	case "self_expression":
		return "self"
	default:
		return ""
	}
}

// swiftCallCallee returns the first named child of a call_expression, which is
// the callee expression (a simple_identifier for plain calls or a
// navigation_expression for receiver calls).
func swiftCallCallee(node *tree_sitter.Node) *tree_sitter.Node {
	for _, child := range swiftNamedChildren(node) {
		child := child
		return shared.CloneNode(&child)
	}
	return nil
}

// swiftCallArguments returns the trimmed source text of each value_argument in
// the call's argument list. Labeled arguments keep their `label: value` text and
// string literals keep their quotes, reproducing the argument strings callers
// rely on. An empty argument list yields an empty (non-nil) slice; a call with
// no argument list yields nil.
func swiftCallArguments(node *tree_sitter.Node, source []byte) []string {
	suffix := swiftCallSuffix(node)
	if suffix == nil {
		return nil
	}
	valueArguments := swiftChildByKind(suffix, "value_arguments")
	if valueArguments == nil {
		return nil
	}
	args := []string{}
	for _, child := range swiftNamedChildren(valueArguments) {
		child := child
		if child.Kind() != "value_argument" {
			continue
		}
		if text := strings.TrimSpace(shared.NodeText(&child, source)); text != "" {
			args = append(args, text)
		}
	}
	return args
}

// swiftCallValueArguments returns the value_argument nodes for a call_expression
// so callers can inspect labels and values directly.
func swiftCallValueArguments(node *tree_sitter.Node) []tree_sitter.Node {
	suffix := swiftCallSuffix(node)
	if suffix == nil {
		return nil
	}
	valueArguments := swiftChildByKind(suffix, "value_arguments")
	if valueArguments == nil {
		return nil
	}
	arguments := make([]tree_sitter.Node, 0, 2)
	for _, child := range swiftNamedChildren(valueArguments) {
		if child.Kind() == "value_argument" {
			arguments = append(arguments, child)
		}
	}
	return arguments
}

// swiftCallSuffix returns the call_suffix child holding the argument list.
func swiftCallSuffix(node *tree_sitter.Node) *tree_sitter.Node {
	return swiftChildByKind(node, "call_suffix")
}

func swiftChildByKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	for _, child := range swiftNamedChildren(node) {
		child := child
		if child.Kind() == kind {
			return shared.CloneNode(&child)
		}
	}
	return nil
}

// swiftCallLineText returns the trimmed source line where the call starts, used
// as the dedup discriminator so two distinct calls on different lines never
// collapse while repeated identical text on one line does.
func swiftCallLineText(node *tree_sitter.Node, source []byte) string {
	startLine := shared.NodeLine(node)
	lines := swiftSourceLines(source)
	if startLine >= 1 && startLine <= len(lines) {
		return strings.TrimSpace(lines[startLine-1])
	}
	return ""
}
