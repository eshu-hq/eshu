package query

import (
	"slices"
	"strings"
)

var cDeadCodeMetadataRootKinds = []string{
	"c.main_function",
	"c.public_header_api",
	"c.signal_handler",
	"c.callback_argument_target",
	"c.function_pointer_target",
}

func deadCodeIsCRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "c" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range cDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
