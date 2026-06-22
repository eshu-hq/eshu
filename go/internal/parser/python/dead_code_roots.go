package python

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var pythonFastAPIRouteDecoratorKinds = map[string]struct{}{
	".get(":     {},
	".post(":    {},
	".put(":     {},
	".patch(":   {},
	".delete(":  {},
	".options(": {},
	".head(":    {},
}

var pythonClassProtocolMethodNames = map[string]struct{}{
	"__abs__":           {},
	"__add__":           {},
	"__aenter__":        {},
	"__aexit__":         {},
	"__aiter__":         {},
	"__and__":           {},
	"__anext__":         {},
	"__await__":         {},
	"__bool__":          {},
	"__bytes__":         {},
	"__call__":          {},
	"__complex__":       {},
	"__contains__":      {},
	"__copy__":          {},
	"__deepcopy__":      {},
	"__del__":           {},
	"__delattr__":       {},
	"__delete__":        {},
	"__delitem__":       {},
	"__dir__":           {},
	"__divmod__":        {},
	"__enter__":         {},
	"__eq__":            {},
	"__exit__":          {},
	"__float__":         {},
	"__floordiv__":      {},
	"__format__":        {},
	"__ge__":            {},
	"__get__":           {},
	"__getattr__":       {},
	"__getattribute__":  {},
	"__getitem__":       {},
	"__getnewargs__":    {},
	"__getnewargs_ex__": {},
	"__getstate__":      {},
	"__gt__":            {},
	"__hash__":          {},
	"__iadd__":          {},
	"__iand__":          {},
	"__ifloordiv__":     {},
	"__ilshift__":       {},
	"__imatmul__":       {},
	"__imod__":          {},
	"__imul__":          {},
	"__index__":         {},
	"__init__":          {},
	"__init_subclass__": {},
	"__int__":           {},
	"__invert__":        {},
	"__ior__":           {},
	"__ipow__":          {},
	"__irshift__":       {},
	"__isub__":          {},
	"__iter__":          {},
	"__itruediv__":      {},
	"__ixor__":          {},
	"__le__":            {},
	"__len__":           {},
	"__length_hint__":   {},
	"__lshift__":        {},
	"__lt__":            {},
	"__matmul__":        {},
	"__missing__":       {},
	"__mod__":           {},
	"__mul__":           {},
	"__ne__":            {},
	"__neg__":           {},
	"__new__":           {},
	"__next__":          {},
	"__or__":            {},
	"__pos__":           {},
	"__pow__":           {},
	"__radd__":          {},
	"__rand__":          {},
	"__rdivmod__":       {},
	"__reduce__":        {},
	"__reduce_ex__":     {},
	"__repr__":          {},
	"__reversed__":      {},
	"__rfloordiv__":     {},
	"__rlshift__":       {},
	"__rmatmul__":       {},
	"__rmod__":          {},
	"__rmul__":          {},
	"__ror__":           {},
	"__round__":         {},
	"__rpow__":          {},
	"__rrshift__":       {},
	"__rshift__":        {},
	"__rsub__":          {},
	"__rtruediv__":      {},
	"__rxor__":          {},
	"__set__":           {},
	"__set_name__":      {},
	"__setattr__":       {},
	"__setitem__":       {},
	"__setstate__":      {},
	"__sizeof__":        {},
	"__str__":           {},
	"__sub__":           {},
	"__truediv__":       {},
	"__trunc__":         {},
	"__xor__":           {},
}

var pythonModuleProtocolFunctionNames = map[string]struct{}{
	"__dir__":     {},
	"__getattr__": {},
}

func pythonDeadCodeRootKinds(decorators []string) []string {
	rootKinds := make([]string, 0, 2)
	for _, decorator := range decorators {
		normalized := pythonNormalizeDecorator(decorator)
		if normalized == "" {
			continue
		}
		switch {
		case pythonIsFastAPIRouteDecorator(normalized):
			rootKinds = appendUniqueString(rootKinds, "python.fastapi_route_decorator")
		case pythonIsFlaskRouteDecorator(normalized):
			rootKinds = appendUniqueString(rootKinds, "python.flask_route_decorator")
		case pythonIsCeleryTaskDecorator(normalized):
			rootKinds = appendUniqueString(rootKinds, "python.celery_task_decorator")
		case pythonIsClickCommandDecorator(normalized):
			rootKinds = appendUniqueString(rootKinds, "python.click_command_decorator")
		case pythonIsTyperCallbackDecorator(normalized):
			rootKinds = appendUniqueString(rootKinds, "python.typer_callback_decorator")
		case pythonIsTyperCommandDecorator(normalized):
			rootKinds = appendUniqueString(rootKinds, "python.typer_command_decorator")
		case pythonIsPropertyDecorator(normalized):
			rootKinds = appendUniqueString(rootKinds, "python.property_decorator")
		}
	}
	return rootKinds
}

func pythonClassDeadCodeRootKinds(decorators []string) []string {
	rootKinds := make([]string, 0, 1)
	for _, decorator := range decorators {
		normalized := pythonNormalizeDecorator(decorator)
		if pythonIsDataclassDecorator(normalized) {
			rootKinds = appendUniqueString(rootKinds, "python.dataclass_model")
		}
	}
	return rootKinds
}

func pythonDataclassClassNames(root *tree_sitter.Node, source []byte) map[string]bool {
	names := make(map[string]bool)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "class_definition" {
			return
		}
		if len(pythonClassDeadCodeRootKinds(pythonDecorators(node, source))) == 0 {
			return
		}
		name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if name != "" {
			names[name] = true
		}
	})
	return names
}

func pythonScriptMainGuardRoots(root *tree_sitter.Node, source []byte) map[string]bool {
	roots := make(map[string]bool)
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "if_statement" || !pythonIsScriptMainGuard(node, source) {
			return
		}
		pythonCollectScriptMainGuardCalls(node.ChildByFieldName("consequence"), source, roots)
	})
	if len(roots) == 0 {
		return nil
	}
	return roots
}

func pythonCollectScriptMainGuardCalls(node *tree_sitter.Node, source []byte, roots map[string]bool) {
	if node == nil {
		return
	}
	if node.Kind() != "call" && pythonNodeStartsNestedDefinition(node) {
		return
	}
	if node.Kind() == "call" {
		name := pythonCallName(node.ChildByFieldName("function"), source)
		if name != "" {
			roots[name] = true
		}
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		pythonCollectScriptMainGuardCalls(&child, source, roots)
	}
}

func pythonNodeStartsNestedDefinition(node *tree_sitter.Node) bool {
	switch node.Kind() {
	case "class_definition", "function_definition":
		return true
	default:
		return false
	}
}

func pythonDunderFunctionAssignedInEnclosingScope(node *tree_sitter.Node, name string, source []byte) bool {
	if !pythonIsDunderName(name) {
		return false
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "function_definition", "module":
			return pythonScopeAssignsDunderFunction(current, name, source)
		case "class_definition":
			return false
		}
	}
	return false
}

// pythonScopeAssignsDunderFunction reports whether the direct body statements of
// a module or function scope install the dunder protocol method onto an
// attribute via `<expr>.<name> = <name>` (for example
// `type(missing).__reduce__ = __reduce__`). It inspects the scope's direct body
// statements rather than nested blocks so a guard inside an inner block does not
// count as a scope-level install, matching the prior line-scan contract.
func pythonScopeAssignsDunderFunction(scope *tree_sitter.Node, name string, source []byte) bool {
	for _, statement := range pythonScopeBodyStatements(scope) {
		if pythonStatementInstallsDunder(statement, name, source) {
			return true
		}
	}
	return false
}

// pythonScopeBodyStatements returns the direct statement nodes of a scope: the
// module's direct children, or a function definition body block's direct
// children.
func pythonScopeBodyStatements(scope *tree_sitter.Node) []*tree_sitter.Node {
	if scope.Kind() == "module" {
		return pythonNamedChildren(scope)
	}
	body := scope.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	return pythonNamedChildren(body)
}

// pythonStatementInstallsDunder reports whether a single statement assigns the
// dunder name onto an attribute target, in either the plain `assignment` form
// (`obj.__call__ = __call__`) or the `type_alias_statement` form that tree-sitter
// uses when the receiver is a `type(...)` call (`type(x).__reduce__ = __reduce__`).
func pythonStatementInstallsDunder(statement *tree_sitter.Node, name string, source []byte) bool {
	switch statement.Kind() {
	case "expression_statement":
		assignment := pythonClassBodyAssignment(statement)
		if assignment == nil {
			return false
		}
		return pythonAssignmentInstallsDunder(
			assignment.ChildByFieldName("left"),
			assignment.ChildByFieldName("right"),
			name,
			source,
		)
	case "type_alias_statement":
		return pythonAssignmentInstallsDunder(
			pythonTypeAliasInner(statement.ChildByFieldName("left")),
			pythonTypeAliasInner(statement.ChildByFieldName("right")),
			name,
			source,
		)
	default:
		return false
	}
}

// pythonTypeAliasInner returns the single named child of a `type` wrapper node
// used by type_alias_statement operands, or the node unchanged otherwise.
func pythonTypeAliasInner(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil || node.Kind() != "type" {
		return node
	}
	if inner := pythonFirstNamedChild(node); inner != nil {
		return inner
	}
	return node
}

// pythonAssignmentInstallsDunder reports whether an assignment's left attribute
// target and right identifier both name the dunder being installed.
func pythonAssignmentInstallsDunder(left *tree_sitter.Node, right *tree_sitter.Node, name string, source []byte) bool {
	if left == nil || left.Kind() != "attribute" {
		return false
	}
	if strings.TrimSpace(nodeText(left.ChildByFieldName("attribute"), source)) != name {
		return false
	}
	return right != nil && right.Kind() == "identifier" &&
		strings.TrimSpace(nodeText(right, source)) == name
}

// pythonNamedChildren returns stable pointer copies of a node's direct named
// children so callers can retain them after the cursor advances.
func pythonNamedChildren(node *tree_sitter.Node) []*tree_sitter.Node {
	if node == nil {
		return nil
	}
	cursor := node.Walk()
	defer cursor.Close()
	children := make([]*tree_sitter.Node, 0)
	for _, child := range node.NamedChildren(cursor) {
		child := child
		children = append(children, shared.CloneNode(&child))
	}
	return children
}

// pythonIsScriptMainGuard reports whether an if_statement is the script entry
// guard `if __name__ == "__main__":`. It inspects the if condition AST node,
// unwrapping a parenthesized_expression, and accepts the comparison in either
// operand order: `__name__ == "__main__"` and `"__main__" == __name__`.
func pythonIsScriptMainGuard(node *tree_sitter.Node, source []byte) bool {
	condition := pythonUnwrapParenthesized(node.ChildByFieldName("condition"))
	if condition == nil || condition.Kind() != "comparison_operator" {
		return false
	}
	if !pythonComparisonUsesEquality(condition, source) {
		return false
	}
	operands := pythonComparisonOperands(condition)
	if len(operands) != 2 {
		return false
	}
	return pythonOperandsAreMainGuard(operands[0], operands[1], source)
}

// pythonUnwrapParenthesized returns the inner expression of a
// parenthesized_expression, or the node unchanged when it is not parenthesized.
func pythonUnwrapParenthesized(node *tree_sitter.Node) *tree_sitter.Node {
	for node != nil && node.Kind() == "parenthesized_expression" {
		inner := pythonFirstNamedChild(node)
		if inner == nil {
			return node
		}
		node = inner
	}
	return node
}

// pythonComparisonUsesEquality reports whether a comparison_operator uses only
// the `==` operator, so inequality guards are not treated as the main guard.
func pythonComparisonUsesEquality(comparison *tree_sitter.Node, source []byte) bool {
	cursor := comparison.Walk()
	defer cursor.Close()
	sawOperator := false
	for i := uint(0); i < comparison.ChildCount(); i++ {
		child := comparison.Child(i)
		if comparison.FieldNameForChild(uint32(i)) != "operators" {
			continue
		}
		sawOperator = true
		if strings.TrimSpace(nodeText(child, source)) != "==" {
			return false
		}
	}
	return sawOperator
}

// pythonComparisonOperands returns the named operand nodes of a
// comparison_operator (the non-operator children).
func pythonComparisonOperands(comparison *tree_sitter.Node) []*tree_sitter.Node {
	operands := make([]*tree_sitter.Node, 0, 2)
	for i := uint(0); i < comparison.ChildCount(); i++ {
		if comparison.FieldNameForChild(uint32(i)) == "operators" {
			continue
		}
		child := comparison.Child(i)
		if child.IsNamed() {
			operands = append(operands, child)
		}
	}
	return operands
}

// pythonOperandsAreMainGuard reports whether the two comparison operands are the
// `__name__` identifier and the `"__main__"` string literal, in either order.
func pythonOperandsAreMainGuard(left *tree_sitter.Node, right *tree_sitter.Node, source []byte) bool {
	return (pythonIsNameIdentifier(left, source) && pythonIsMainStringLiteral(right, source)) ||
		(pythonIsMainStringLiteral(left, source) && pythonIsNameIdentifier(right, source))
}

func pythonIsNameIdentifier(node *tree_sitter.Node, source []byte) bool {
	return node != nil && node.Kind() == "identifier" &&
		strings.TrimSpace(nodeText(node, source)) == "__name__"
}

func pythonIsMainStringLiteral(node *tree_sitter.Node, source []byte) bool {
	return node != nil && node.Kind() == "string" &&
		pythonStringLiteralValue(node, source) == "__main__"
}

func pythonFirstNamedChild(node *tree_sitter.Node) *tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		return shared.CloneNode(&child)
	}
	return nil
}

func pythonNormalizeDecorator(decorator string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(decorator)), ""))
	normalized, _, _ = strings.Cut(normalized, "#")
	return normalized
}

func pythonIsFastAPIRouteDecorator(normalized string) bool {
	if !strings.HasPrefix(normalized, "@") {
		return false
	}
	for suffix := range pythonFastAPIRouteDecoratorKinds {
		if strings.Contains(normalized, suffix) {
			return true
		}
	}
	return false
}

func pythonIsFlaskRouteDecorator(normalized string) bool {
	return strings.HasPrefix(normalized, "@") && strings.Contains(normalized, ".route(")
}

func pythonIsCeleryTaskDecorator(normalized string) bool {
	if normalized == "@shared_task" || strings.HasPrefix(normalized, "@shared_task(") {
		return true
	}
	return strings.HasPrefix(normalized, "@") && strings.Contains(normalized, ".task(")
}

func pythonIsClickCommandDecorator(normalized string) bool {
	if normalized == "@click.command" || strings.HasPrefix(normalized, "@click.command(") {
		return true
	}
	return strings.HasPrefix(normalized, "@cli.command(")
}

func pythonIsTyperCommandDecorator(normalized string) bool {
	if strings.HasPrefix(normalized, "@typer.") && strings.Contains(normalized, ".command(") {
		return true
	}
	return strings.HasPrefix(normalized, "@app.command(")
}

func pythonIsTyperCallbackDecorator(normalized string) bool {
	if strings.HasPrefix(normalized, "@typer.") && strings.Contains(normalized, ".callback(") {
		return true
	}
	return strings.HasPrefix(normalized, "@app.callback(")
}

func pythonIsPropertyDecorator(normalized string) bool {
	if normalized == "@property" ||
		normalized == "@cached_property" ||
		normalized == "@functools.cached_property" {
		return true
	}
	return strings.HasPrefix(normalized, "@cached_property(") ||
		strings.HasPrefix(normalized, "@functools.cached_property(") ||
		strings.HasSuffix(normalized, ".cached_property")
}

func pythonIsDataclassDecorator(normalized string) bool {
	return normalized == "@dataclass" ||
		normalized == "@dataclasses.dataclass" ||
		strings.HasPrefix(normalized, "@dataclass(") ||
		strings.HasPrefix(normalized, "@dataclasses.dataclass(")
}

func pythonIsDunderName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" &&
		len(name) > 4 &&
		strings.HasPrefix(name, "__") &&
		strings.HasSuffix(name, "__")
}

func pythonIsClassProtocolMethod(name string) bool {
	_, ok := pythonClassProtocolMethodNames[strings.TrimSpace(name)]
	return ok
}

func pythonIsModuleProtocolFunction(name string) bool {
	_, ok := pythonModuleProtocolFunctionNames[strings.TrimSpace(name)]
	return ok
}
