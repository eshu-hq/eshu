package query

import (
	"slices"
	"strings"
)

func deadCodeIsJavaScriptFrameworkRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	switch strings.ToLower(deadCodeEntityLanguage(result, entity)) {
	case "javascript", "jsx", "typescript", "tsx":
	default:
		return false
	}
	if primaryEntityLabel(result) != "Function" {
		return false
	}

	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	if slices.Contains(rootKinds, "javascript.nextjs_route_export") ||
		slices.Contains(rootKinds, "javascript.express_route_registration") {
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	return false
}
