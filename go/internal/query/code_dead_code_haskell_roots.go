package query

import (
	"slices"
	"strings"
)

var haskellDeadCodeMetadataRootKinds = []string{
	"haskell.main_function",
	"haskell.module_export",
	"haskell.exported_type",
	"haskell.typeclass_method",
	"haskell.instance_method",
}

func deadCodeIsHaskellRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "haskell" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range haskellDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
