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
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	if slices.Contains(rootKinds, "javascript.node_package_export") {
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	if primaryEntityLabel(result) != "Function" {
		return false
	}
	if slices.Contains(rootKinds, "javascript.nextjs_route_export") ||
		slices.Contains(rootKinds, "javascript.express_route_registration") ||
		slices.Contains(rootKinds, "javascript.node_package_entrypoint") ||
		slices.Contains(rootKinds, "javascript.node_package_bin") ||
		slices.Contains(rootKinds, "javascript.hapi_handler_export") {
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	return false
}
