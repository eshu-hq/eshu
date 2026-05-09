package python

import (
	"regexp"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var pythonMainGuardCallRe = regexp.MustCompile(`(?m)\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

var pythonFastAPIRouteDecoratorKinds = map[string]struct{}{
	".get(":     {},
	".post(":    {},
	".put(":     {},
	".patch(":   {},
	".delete(":  {},
	".options(": {},
	".head(":    {},
}

func pythonDeadCodeRootKinds(decorators []string) []string {
	rootKinds := make([]string, 0, 2)
	for _, decorator := range decorators {
		normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(decorator)), ""))
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
		normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(decorator)), ""))
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

func pythonScriptMainGuardRoots(source []byte) map[string]bool {
	text := string(source)
	guardIndex := strings.Index(text, `if __name__ == "__main__":`)
	if guardIndex < 0 {
		guardIndex = strings.Index(text, `if __name__ == '__main__':`)
	}
	if guardIndex < 0 {
		return nil
	}
	roots := make(map[string]bool)
	for _, match := range pythonMainGuardCallRe.FindAllStringSubmatch(text[guardIndex:], -1) {
		if len(match) == 2 && match[1] != "if" {
			roots[match[1]] = true
		}
	}
	return roots
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
	return normalized == "@property"
}

func pythonIsDataclassDecorator(normalized string) bool {
	return normalized == "@dataclass" ||
		normalized == "@dataclasses.dataclass" ||
		strings.HasPrefix(normalized, "@dataclass(") ||
		strings.HasPrefix(normalized, "@dataclasses.dataclass(")
}

func pythonIsDunderMethod(name string) bool {
	name = strings.TrimSpace(name)
	return name != "__post_init__" &&
		len(name) > 4 &&
		strings.HasPrefix(name, "__") &&
		strings.HasSuffix(name, "__")
}
