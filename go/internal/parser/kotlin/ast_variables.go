package kotlin

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// handlePropertyDeclaration records a variable row, infers its type into the
// owning scope, then recurses into the initializer so embedded calls emit.
func (w *astWalker) handlePropertyDeclaration(node *tree_sitter.Node, f frame) {
	variable := w.childByKind(node, "variable_declaration")
	if variable == nil {
		w.walkChildren(node, f)
		return
	}
	name := strings.TrimSpace(shared.NodeText(w.childByKind(variable, "identifier"), w.source))
	if name == "" {
		w.walkChildren(node, f)
		return
	}

	declaredType := w.declaredVariableType(variable)
	switch {
	case declaredType != "" && f.functionContext != "":
		w.setLocalVariableType(f.functionContext, name, declaredType)
	case declaredType != "" && f.classContext != "":
		w.setClassPropertyType(f.classContext, name, declaredType)
	case f.functionContext != "":
		w.inferLocalVariableType(node, name, f)
	}

	if _, ok := w.seenVariables[name]; !ok {
		w.seenVariables[name] = struct{}{}
		shared.AppendBucket(w.payload, "variables", map[string]any{
			"name":        name,
			"line_number": shared.NodeLine(node),
			"end_line":    shared.NodeLine(node),
			"lang":        "kotlin",
		})
	}

	w.walkChildren(node, f)
}

// declaredVariableType returns the explicit type annotation of a variable
// declaration (`val x: Type`), canonicalized, or "".
func (w *astWalker) declaredVariableType(variable *tree_sitter.Node) string {
	var typeNode *tree_sitter.Node
	cursor := variable.Walk()
	defer cursor.Close()
	for _, child := range variable.NamedChildren(cursor) {
		child := child
		switch child.Kind() {
		case "user_type", "nullable_type":
			typeNode = shared.CloneNode(&child)
		}
	}
	if typeNode == nil {
		return ""
	}
	return kotlinCanonicalTypeReference(shared.NodeText(typeNode, w.source))
}

// inferLocalVariableType infers and stores the type of an untyped local
// variable from its initializer (constructor call, function-return alias, cast,
// string literal, identifier alias, or lazy delegate).
func (w *astWalker) inferLocalVariableType(node *tree_sitter.Node, name string, f frame) {
	variables := w.effectiveVariableTypes(f)
	inferred, callKind := w.inferInitializerType(node, f, variables)
	if inferred == "" {
		return
	}
	w.setLocalVariableType(f.functionContext, name, inferred)
	if callKind != "" {
		w.setLocalVariableCallKind(f.functionContext, name, callKind)
	} else {
		delete(w.localVariableCallKinds[f.functionContext], name)
	}
}

// inferInitializerType returns the inferred type and optional call kind of a
// property initializer expression.
func (w *astWalker) inferInitializerType(node *tree_sitter.Node, f frame, variables map[string]string) (string, string) {
	if delegate := w.childByKind(node, "property_delegate"); delegate != nil {
		if expr := w.lazyDelegateExpression(delegate); expr != nil {
			typ := w.inferExpressionType(expr, f, variables)
			if typ != "" {
				return typ, "kotlin_lazy_delegated_property_receiver"
			}
		}
		return "", ""
	}

	value := w.assignmentValueNode(node)
	if value == nil {
		return "", ""
	}
	return w.inferExpressionType(value, f, variables), ""
}

// assignmentValueNode returns the initializer expression of a property
// declaration: the named node that follows the anonymous `=`.
func (w *astWalker) assignmentValueNode(node *tree_sitter.Node) *tree_sitter.Node {
	sawEquals := false
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if !child.IsNamed() {
			if child.Kind() == "=" {
				sawEquals = true
			}
			continue
		}
		if sawEquals {
			return shared.CloneNode(child)
		}
	}
	return nil
}

// lazyDelegateExpression returns the lambda body expression of a `by lazy { ...
// }` delegate, used to infer the delegated property type.
func (w *astWalker) lazyDelegateExpression(delegate *tree_sitter.Node) *tree_sitter.Node {
	call := w.childByKind(delegate, "call_expression")
	if call == nil {
		return nil
	}
	identifier := w.childByKind(call, "identifier")
	if identifier == nil || strings.TrimSpace(shared.NodeText(identifier, w.source)) != "lazy" {
		return nil
	}
	lambda := w.lambdaLiteral(call)
	if lambda == nil {
		return nil
	}
	return w.lastNamedStatement(lambda)
}

// inferExpressionType infers the static type of an initializer expression node.
func (w *astWalker) inferExpressionType(node *tree_sitter.Node, f frame, variables map[string]string) string {
	node = w.unwrapScopeFunctions(node)
	switch node.Kind() {
	case "call_expression":
		return w.inferCallExpressionType(node, f, variables)
	case "as_expression":
		right := node.ChildByFieldName("right")
		return kotlinCanonicalTypeReference(shared.NodeText(right, w.source))
	case "string_literal":
		return "String"
	case "navigation_expression":
		return w.inferReceiverType(w.receiverChainText(node), variables, f.classContext)
	case "identifier":
		return w.inferReceiverType(strings.TrimSpace(shared.NodeText(node, w.source)), variables, f.classContext)
	case "parenthesized_expression":
		if inner := w.firstNamedChild(node); inner != nil {
			return w.inferExpressionType(inner, f, variables)
		}
	}
	return ""
}

// handleIfExpression narrows smart-cast types from an `if (x is T)` condition
// into the guarded block's frame, then recurses.
func (w *astWalker) handleIfExpression(node *tree_sitter.Node, f frame) {
	narrowed := w.smartCastFromCondition(node.ChildByFieldName("condition"))
	guarded := f.withSmartCasts(narrowed)

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		// The condition itself is evaluated without the narrowing it
		// introduces, matching prior behavior where the cast applies to the
		// consequent block.
		if child.Kind() == "block" {
			w.walkNode(&child, guarded)
			continue
		}
		w.walkNode(&child, f)
	}
}

// handleWhenExpression narrows smart-cast types for each `is T ->` entry of a
// `when (subject)` expression.
func (w *astWalker) handleWhenExpression(node *tree_sitter.Node, f frame) {
	subject := w.whenSubjectName(node)

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if child.Kind() != "when_entry" {
			w.walkNode(&child, f)
			continue
		}
		entryFrame := f
		if subject != "" {
			if narrowed := w.whenEntryNarrowing(&child, subject); len(narrowed) > 0 {
				entryFrame = f.withSmartCasts(narrowed)
			}
		}
		w.walkChildren(&child, entryFrame)
	}
}

// smartCastFromCondition returns the variable narrowing implied by an
// `x is T` condition (is_expression), or nil.
func (w *astWalker) smartCastFromCondition(condition *tree_sitter.Node) map[string]string {
	if condition == nil || condition.Kind() != "is_expression" {
		return nil
	}
	left := strings.TrimSpace(shared.NodeText(condition.ChildByFieldName("left"), w.source))
	right := kotlinCanonicalTypeReference(shared.NodeText(condition.ChildByFieldName("right"), w.source))
	if left == "" || right == "" {
		return nil
	}
	return map[string]string{left: right}
}

// whenSubjectName returns the bound subject identifier of a `when (subject)`
// expression, or "".
func (w *astWalker) whenSubjectName(node *tree_sitter.Node) string {
	subject := w.childByKind(node, "when_subject")
	if subject == nil {
		return ""
	}
	identifier := w.childByKind(subject, "identifier")
	if identifier == nil {
		return ""
	}
	return strings.TrimSpace(shared.NodeText(identifier, w.source))
}

// whenEntryNarrowing returns the narrowing implied by an `is T ->` when entry
// for the given subject.
func (w *astWalker) whenEntryNarrowing(entry *tree_sitter.Node, subject string) map[string]string {
	condition := entry.ChildByFieldName("condition")
	if condition == nil || condition.Kind() != "type_test" {
		return nil
	}
	userType := w.childByKind(condition, "user_type")
	if userType == nil {
		return nil
	}
	typ := kotlinCanonicalTypeReference(shared.NodeText(userType, w.source))
	if typ == "" {
		return nil
	}
	return map[string]string{subject: typ}
}

func (w *astWalker) setLocalVariableType(functionContext, name, typ string) {
	if functionContext == "" || name == "" || typ == "" {
		return
	}
	if _, ok := w.localVariableTypes[functionContext]; !ok {
		w.localVariableTypes[functionContext] = make(map[string]string)
	}
	w.localVariableTypes[functionContext][name] = typ
}

func (w *astWalker) setLocalVariableCallKind(functionContext, name, callKind string) {
	if functionContext == "" || name == "" || callKind == "" {
		return
	}
	if _, ok := w.localVariableCallKinds[functionContext]; !ok {
		w.localVariableCallKinds[functionContext] = make(map[string]string)
	}
	w.localVariableCallKinds[functionContext][name] = callKind
}

func (w *astWalker) setClassPropertyType(classContext, name, typ string) {
	if classContext == "" || name == "" || typ == "" {
		return
	}
	if _, ok := w.classPropertyTypes[classContext]; !ok {
		w.classPropertyTypes[classContext] = make(map[string]string)
	}
	w.classPropertyTypes[classContext][name] = typ
}
