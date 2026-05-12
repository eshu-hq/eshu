package query

import (
	"slices"
	"strings"
)

var dartDeadCodeMetadataRootKinds = []string{
	"dart.main_function",
	"dart.constructor",
	"dart.override_method",
	"dart.flutter_widget_build",
	"dart.flutter_create_state",
	"dart.public_library_api",
}

func deadCodeIsDartRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "dart" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range dartDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
