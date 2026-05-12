package query

import (
	"slices"
	"strings"
)

var cppDeadCodeMetadataRootKinds = []string{
	"cpp.main_function",
	"cpp.public_header_api",
	"cpp.virtual_method",
	"cpp.override_method",
	"cpp.callback_argument_target",
	"cpp.function_pointer_target",
	"cpp.node_addon_entrypoint",
}

func deadCodeIsCPPRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "cpp" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range cppDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
