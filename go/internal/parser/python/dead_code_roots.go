package python

import (
	"regexp"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var pythonMainGuardHeaderRe = regexp.MustCompile(
	`^\s*if\s*\(?\s*(?:__name__\s*==\s*["']__main__["']|["']__main__["']\s*==\s*__name__)\s*\)?\s*:`,
)

var pythonDunderAssignmentRe = regexp.MustCompile(`\.\s*(__[A-Za-z0-9_]+__)\s*=\s*(__[A-Za-z0-9_]+__)`)

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

func pythonScopeAssignsDunderFunction(scope *tree_sitter.Node, name string, source []byte) bool {
	scopeText := nodeText(scope, source)
	directIndent, ok := pythonDirectBodyIndent(scope.Kind(), scopeText)
	if !ok {
		return false
	}
	for _, line := range strings.Split(scopeText, "\n") {
		if strings.TrimSpace(line) == "" || leadingWhitespace(line) != directIndent {
			continue
		}
		directLine := line[directIndent:]
		for _, match := range pythonDunderAssignmentRe.FindAllStringSubmatch(directLine, -1) {
			if len(match) == 3 && match[1] == name && match[2] == name {
				return true
			}
		}
	}
	return false
}

func pythonDirectBodyIndent(kind string, text string) (int, bool) {
	if kind == "module" {
		return 0, true
	}
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return 0, false
	}
	headerIndent := leadingWhitespace(lines[0])
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := leadingWhitespace(line)
		if indent > headerIndent {
			return indent, true
		}
	}
	return 0, false
}

func pythonIsScriptMainGuard(node *tree_sitter.Node, source []byte) bool {
	text := strings.TrimSpace(nodeText(node, source))
	header, _, ok := strings.Cut(text, "\n")
	if !ok {
		header = text
	}
	return pythonMainGuardHeaderRe.MatchString(header)
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
