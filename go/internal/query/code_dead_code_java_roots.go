package query

import (
	"slices"
	"strings"
)

func deadCodeIsJavaRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "java" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	if slices.Contains(rootKinds, "java.main_method") ||
		slices.Contains(rootKinds, "java.constructor") ||
		slices.Contains(rootKinds, "java.override_method") {
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	return false
}
