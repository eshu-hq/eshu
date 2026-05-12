package query

import (
	"slices"
	"strings"
)

var perlDeadCodeMetadataRootKinds = []string{
	"perl.script_entrypoint",
	"perl.package_namespace",
	"perl.exported_subroutine",
	"perl.constructor",
	"perl.special_block",
	"perl.autoload_subroutine",
	"perl.destroy_subroutine",
}

func deadCodeIsPerlRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "perl" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range perlDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
