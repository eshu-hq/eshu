package query

import (
	"slices"
	"strings"
)

var groovyDeadCodeMetadataRootKinds = []string{
	"groovy.jenkins_pipeline_entrypoint",
	"groovy.shared_library_call",
}

func deadCodeIsGroovyRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "groovy" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range groovyDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
