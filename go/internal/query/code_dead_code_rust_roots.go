package query

import (
	"slices"
	"strings"
)

var rustDeadCodeMetadataRootKinds = []string{
	"rust.main_function",
	"rust.test_function",
	"rust.tokio_main",
	"rust.tokio_test",
}

func deadCodeIsRustRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "rust" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range rustDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
