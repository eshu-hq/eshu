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
	"rust.public_api_item",
	"rust.trait_impl_method",
	"rust.benchmark_function",
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

func deadCodeIsRustCargoAuxiliaryTarget(result map[string]any, entity *EntityContent) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "rust" {
		return false
	}
	path := strings.ToLower(deadCodeEntityPath(result, entity))
	return strings.HasPrefix(path, "benches/") ||
		strings.Contains(path, "/benches/") ||
		strings.HasPrefix(path, "examples/") ||
		strings.Contains(path, "/examples/")
}
