package parser

import "strings"

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
		}
	}
	return rootKinds
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
