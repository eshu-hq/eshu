package query

import (
	"slices"
	"strings"
)

var phpDeadCodeMetadataRootKinds = []string{
	"php.script_entrypoint",
	"php.constructor",
	"php.magic_method",
	"php.interface_method",
	"php.interface_implementation_method",
	"php.trait_method",
	"php.framework_controller_action",
	"php.route_handler",
	"php.symfony_route_attribute",
	"php.wordpress_hook_callback",
}

func deadCodeIsPHPRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "php" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range phpDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
