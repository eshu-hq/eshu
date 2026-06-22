package kotlin

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// handleCall emits function_call rows for a call_expression or infix_expression
// node. The walker recurses into argument subtrees separately, so this method
// only emits the call rooted at the given node.
func (w *astWalker) handleCall(node *tree_sitter.Node, f frame) {
	switch node.Kind() {
	case "infix_expression":
		w.handleInfixCall(node, f)
	case "call_expression":
		w.handleCallExpression(node, f)
	}
}

// handleInfixCall emits the row for a `receiver name arg` infix invocation.
func (w *astWalker) handleInfixCall(node *tree_sitter.Node, f frame) {
	receiverNode := w.firstNamedChild(node)
	if receiverNode == nil {
		return
	}
	nameNode := w.nthNamedChild(node, 1)
	if nameNode == nil || nameNode.Kind() != "identifier" {
		return
	}
	receiver := strings.TrimSpace(shared.NodeText(receiverNode, w.source))
	name := strings.TrimSpace(shared.NodeText(nameNode, w.source))
	if receiver == "" || !kotlinCallNameAllowed(name) || f.functionContext == "" {
		return
	}

	var inferredType, classContext string
	if receiver == "this" {
		if f.classContext != "" {
			classContext = f.classContext
			inferredType = f.classContext
		}
	} else {
		inferredType = w.inferReceiverType(receiver, w.effectiveVariableTypes(f), f.classContext)
	}
	if inferredType == "" {
		return
	}

	fullName := strings.TrimSpace(receiver + " " + name)
	item := map[string]any{
		"name":              name,
		"full_name":         fullName,
		"line_number":       shared.NodeLine(node),
		"lang":              "kotlin",
		"inferred_obj_type": kotlinBaseTypeName(inferredType),
	}
	if classContext != "" {
		item["class_context"] = classContext
	}
	shared.AppendBucket(w.payload, "function_calls", item)
}

// kotlinBareCallControlKeywords are keywords that take a parenthesized clause
// but are not function calls, so they must never be emitted as calls.
var kotlinBareCallControlKeywords = map[string]struct{}{
	"if": {}, "for": {}, "while": {}, "when": {}, "catch": {},
	"return": {}, "switch": {}, "super": {}, "fun": {}, "class": {},
	"interface": {}, "object": {}, "constructor": {}, "init": {},
}

// handleCallExpression emits the row for a `receiver.name(args)`,
// `Constructor(args)`, or bare `name(args)` invocation. Chained intermediate
// calls are emitted by the walker as it recurses into the receiver subtree.
func (w *astWalker) handleCallExpression(node *tree_sitter.Node, f frame) {
	callee := w.firstNamedChild(node)
	if callee == nil {
		return
	}

	switch callee.Kind() {
	case "identifier":
		w.emitIdentifierCall(callee, node, f)
	case "navigation_expression":
		w.emitNavigationCall(callee, node, f)
	}
}

// emitIdentifierCall emits the row for a receiver-less `name(args)` call. A
// known type with a type-shaped name is a constructor; any other allowed name is
// a bare function call (same-scope, top-level, or imported). Locally-declared
// types and chain-receiver positions are skipped so the constructor and
// qualified-call paths own those expressions.
func (w *astWalker) emitIdentifierCall(callee, node *tree_sitter.Node, f frame) {
	name := strings.TrimSpace(shared.NodeText(callee, w.source))
	if name == "" || !kotlinCallNameAllowed(name) {
		return
	}
	if _, isControl := kotlinBareCallControlKeywords[name]; isControl {
		return
	}

	if _, known := w.knownTypeNames[name]; known && kotlinLooksLikeTypeName(name) {
		w.appendCallRow(name, name, shared.NodeLine(node))
		return
	}

	// Bare function call. Imported names are NOT skipped (Kotlin imports do not
	// distinguish a function from a type); only file-local type declarations are
	// constructor targets. Chain receivers belong to the qualified-call path.
	if _, local := w.localTypeNames[name]; local {
		return
	}
	if w.callIsChainReceiver(node) {
		return
	}
	w.appendCallRow(name, name, shared.NodeLine(node))
}

// appendCallRow appends one minimal function_call row.
func (w *astWalker) appendCallRow(name, fullName string, line int) {
	shared.AppendBucket(w.payload, "function_calls", map[string]any{
		"name":        name,
		"full_name":   fullName,
		"line_number": line,
		"lang":        "kotlin",
	})
}

// callIsChainReceiver reports whether a call_expression is the receiver of an
// enclosing member-access call (the `x()` in `x().y()`), which the qualified-
// call path already represents. Receiver-preserving scope functions
// (`.also`/`.apply`) are transparent: the walk steps through them, so
// `createService().apply { }` alone is not a chain receiver (its underlying call
// still emits), but `createService().apply { }.info()` is, because the value
// ultimately flows into the `.info()` member call.
func (w *astWalker) callIsChainReceiver(node *tree_sitter.Node) bool {
	current := node
	for {
		parent := current.Parent()
		if parent == nil || parent.Kind() != "navigation_expression" {
			return false
		}
		receiver := w.firstNamedChild(parent)
		if receiver == nil || receiver.StartByte() != current.StartByte() || receiver.EndByte() != current.EndByte() {
			return false
		}
		if !isScopeFunctionName(w.navigationMemberName(parent)) {
			// A real member access consumes this value as its receiver.
			return true
		}
		// Step through the scope-function call to its enclosing expression.
		scopeCall := parent.Parent()
		if scopeCall == nil || scopeCall.Kind() != "call_expression" {
			return false
		}
		current = scopeCall
	}
}

// isScopeFunctionName reports whether a member name is a receiver-preserving
// scope function whose call row is suppressed so the underlying receiver call
// surfaces instead.
func isScopeFunctionName(name string) bool {
	return name == "also" || name == "apply"
}

// emitNavigationCall emits the row for a `receiver.name(args)` call. It records
// the inferred receiver type, this-context, call kind, and reconstructs the
// textual full_name with safe-call operators normalized to plain dots.
func (w *astWalker) emitNavigationCall(navigation, node *tree_sitter.Node, f frame) {
	name := w.navigationMemberName(navigation)
	if name == "" || !kotlinCallNameAllowed(name) {
		return
	}
	// Receiver-preserving scope functions are transparent: the underlying
	// receiver call emits instead, so do not emit a row for `.also`/`.apply`.
	if isScopeFunctionName(name) {
		return
	}
	receiverNode := w.firstNamedChild(navigation)
	if receiverNode == nil {
		return
	}
	receiverText := w.receiverChainText(receiverNode)
	fullName := kotlinNormalizeParenthesizedReceivers(
		kotlinStripReceiverPreservingScopeFunctions(
			w.normalizeChainText(strings.TrimSpace(shared.NodeText(navigation, w.source))),
		),
	)

	item := map[string]any{
		"name":        name,
		"full_name":   fullName,
		"line_number": shared.NodeLine(node),
		"lang":        "kotlin",
	}

	if receiverText == "this" {
		if f.classContext != "" {
			item["class_context"] = f.classContext
		}
	} else if receiverText != "" && f.functionContext != "" {
		inferredType := w.inferReceiverNodeType(receiverNode, receiverText, f)
		if inferredType != "" {
			item["inferred_obj_type"] = kotlinBaseTypeName(inferredType)
		}
		if callKind := w.receiverCallKind(receiverNode, f.functionContext); callKind != "" {
			item["call_kind"] = callKind
		}
	}
	shared.AppendBucket(w.payload, "function_calls", item)
}

// navigationMemberName returns the member identifier selected by a navigation
// expression (`receiver.member`).
func (w *astWalker) navigationMemberName(navigation *tree_sitter.Node) string {
	var member string
	for i := uint(0); i < navigation.ChildCount(); i++ {
		child := navigation.Child(i)
		if child == nil {
			continue
		}
		if child.IsNamed() && child.Kind() == "identifier" {
			member = strings.TrimSpace(shared.NodeText(child, w.source))
		}
	}
	return member
}

// receiverChainText returns the receiver text used for type inference: the
// navigation receiver normalized (safe calls to dots, scope functions
// stripped). It mirrors the previous expression normalization.
func (w *astWalker) receiverChainText(node *tree_sitter.Node) string {
	text := kotlinNormalizeParenthesizedReceivers(
		w.normalizeChainText(strings.TrimSpace(shared.NodeText(node, w.source))),
	)
	text = kotlinStripWrappingParentheses(text)
	return kotlinStripReceiverPreservingScopeFunctions(text)
}

// normalizeChainText converts safe-call operators to plain dots so receiver
// text matches the dotted form used in inference and full_name parity.
func (w *astWalker) normalizeChainText(text string) string {
	return strings.ReplaceAll(text, "?.", ".")
}

// receiverCallKind returns the lazy-delegate call kind when the receiver root
// is a lazy-delegated local variable.
func (w *astWalker) receiverCallKind(receiverNode *tree_sitter.Node, functionContext string) string {
	root := w.receiverRootIdentifier(receiverNode)
	if root == "" {
		return ""
	}
	return strings.TrimSpace(w.localVariableCallKinds[functionContext][root])
}

// receiverRootIdentifier returns the leftmost identifier of a receiver chain.
func (w *astWalker) receiverRootIdentifier(node *tree_sitter.Node) string {
	current := node
	for current != nil {
		switch current.Kind() {
		case "identifier":
			return strings.TrimSpace(shared.NodeText(current, w.source))
		case "navigation_expression", "call_expression", "parenthesized_expression":
			current = w.firstNamedChild(current)
		default:
			return ""
		}
	}
	return ""
}

// inferReceiverNodeType resolves a navigation receiver's static type. It first
// handles AST-structural cases the textual inference cannot: a parenthesized
// `(value as Type)` cast resolves directly to the cast type. Otherwise it falls
// back to the shared textual chain inference.
func (w *astWalker) inferReceiverNodeType(receiverNode *tree_sitter.Node, receiverText string, f frame) string {
	if cast := w.parenthesizedCastType(receiverNode); cast != "" {
		return cast
	}
	return w.inferReceiverType(receiverText, w.effectiveVariableTypes(f), f.classContext)
}

// parenthesizedCastType returns the cast target type when the node is a
// parenthesized `as` expression such as `(any as Service)`, or "".
func (w *astWalker) parenthesizedCastType(node *tree_sitter.Node) string {
	if node == nil || node.Kind() != "parenthesized_expression" {
		return ""
	}
	inner := w.firstNamedChild(node)
	if inner == nil || inner.Kind() != "as_expression" {
		return ""
	}
	return kotlinCanonicalTypeReference(shared.NodeText(inner.ChildByFieldName("right"), w.source))
}

// inferReceiverType resolves the static type of a textual receiver chain using
// the shared inference helpers.
func (w *astWalker) inferReceiverType(receiver string, variables map[string]string, classContext string) string {
	return kotlinInferReceiverType(
		receiver,
		variables,
		w.classPropertyTypes,
		classContext,
		w.packageName,
		w.functionReturnTypes,
		w.classTypeParameters,
	)
}

// inferCallExpressionType resolves the return type of a call_expression used as
// an initializer (constructor call or function-return alias).
func (w *astWalker) inferCallExpressionType(node *tree_sitter.Node, f frame, variables map[string]string) string {
	callee := w.firstNamedChild(node)
	if callee == nil {
		return ""
	}
	if callee.Kind() == "identifier" {
		name := strings.TrimSpace(shared.NodeText(callee, w.source))
		if kotlinLooksLikeTypeName(name) {
			return kotlinCanonicalTypeReference(name)
		}
		return kotlinLookupFunctionReturnType(w.functionReturnTypes, w.packageName, f.classContext, name)
	}
	// Navigation or chained call: reconstruct the textual call and reuse the
	// shared method-return inference.
	expr := w.normalizeChainText(strings.TrimSpace(shared.NodeText(node, w.source)))
	expr = kotlinStripReceiverPreservingScopeFunctions(expr)
	return kotlinInferMethodCallReturnType(
		expr,
		variables,
		w.classPropertyTypes,
		f.classContext,
		w.packageName,
		w.functionReturnTypes,
		w.classTypeParameters,
	)
}

// unwrapScopeFunctions descends through trailing `.also {}` / `.apply {}` scope
// functions and parentheses so initializer inference sees the receiver value.
func (w *astWalker) unwrapScopeFunctions(node *tree_sitter.Node) *tree_sitter.Node {
	for node != nil {
		if node.Kind() == "call_expression" {
			if navigation := w.firstNamedChild(node); navigation != nil && navigation.Kind() == "navigation_expression" {
				if member := w.navigationMemberName(navigation); member == "also" || member == "apply" {
					node = w.firstNamedChild(navigation)
					continue
				}
			}
		}
		return node
	}
	return node
}

// lambdaLiteral returns the lambda_literal inside an annotated_lambda child of
// a call expression.
func (w *astWalker) lambdaLiteral(call *tree_sitter.Node) *tree_sitter.Node {
	annotated := w.childByKind(call, "annotated_lambda")
	if annotated == nil {
		return nil
	}
	return w.childByKind(annotated, "lambda_literal")
}

// lastNamedStatement returns the last named child statement of a lambda body.
func (w *astWalker) lastNamedStatement(lambda *tree_sitter.Node) *tree_sitter.Node {
	var last *tree_sitter.Node
	cursor := lambda.Walk()
	defer cursor.Close()
	for _, child := range lambda.NamedChildren(cursor) {
		child := child
		last = shared.CloneNode(&child)
	}
	return last
}

func (w *astWalker) firstNamedChild(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		return shared.CloneNode(&child)
	}
	return nil
}

func (w *astWalker) nthNamedChild(node *tree_sitter.Node, n int) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	index := 0
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if index == n {
			return shared.CloneNode(&child)
		}
		index++
	}
	return nil
}
