package query

import (
	"slices"
	"strings"
)

var rubyDeadCodeMetadataRootKinds = []string{
	"ruby.rails_controller_action",
	"ruby.rails_callback_method",
	"ruby.dynamic_dispatch_hook",
	"ruby.method_reference_target",
	"ruby.script_entrypoint",
}

func deadCodeIsRubyRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "ruby" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range rubyDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
