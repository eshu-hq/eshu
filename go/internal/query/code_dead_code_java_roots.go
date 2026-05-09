package query

import (
	"slices"
	"strings"
)

func deadCodeIsJavaRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "java" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	if slices.Contains(rootKinds, "java.main_method") ||
		slices.Contains(rootKinds, "java.constructor") ||
		slices.Contains(rootKinds, "java.override_method") ||
		slices.Contains(rootKinds, "java.ant_task_setter") ||
		slices.Contains(rootKinds, "java.gradle_plugin_apply") ||
		slices.Contains(rootKinds, "java.gradle_task_action") ||
		slices.Contains(rootKinds, "java.gradle_task_property") ||
		slices.Contains(rootKinds, "java.gradle_task_setter") ||
		slices.Contains(rootKinds, "java.gradle_task_interface_method") ||
		slices.Contains(rootKinds, "java.gradle_dsl_public_method") ||
		slices.Contains(rootKinds, "java.method_reference_target") {
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	return false
}
