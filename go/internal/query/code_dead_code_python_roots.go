package query

import (
	"slices"
	"strings"
)

func deadCodeIsPythonFrameworkRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "python" {
		return false
	}
	if primaryEntityLabel(result) != "Function" {
		return false
	}

	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	if slices.Contains(rootKinds, "python.fastapi_route_decorator") ||
		slices.Contains(rootKinds, "python.flask_route_decorator") ||
		slices.Contains(rootKinds, "python.celery_task_decorator") {
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	return false
}
