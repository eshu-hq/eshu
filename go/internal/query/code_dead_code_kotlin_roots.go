package query

import (
	"slices"
	"strings"
)

var kotlinDeadCodeMetadataRootKinds = []string{
	"kotlin.main_function",
	"kotlin.constructor",
	"kotlin.interface_type",
	"kotlin.interface_method",
	"kotlin.interface_implementation_method",
	"kotlin.override_method",
	"kotlin.gradle_plugin_apply",
	"kotlin.gradle_task_action",
	"kotlin.gradle_task_property",
	"kotlin.gradle_task_setter",
	"kotlin.spring_component_class",
	"kotlin.spring_configuration_properties_class",
	"kotlin.spring_request_mapping_method",
	"kotlin.spring_bean_method",
	"kotlin.spring_event_listener_method",
	"kotlin.spring_scheduled_method",
	"kotlin.lifecycle_callback_method",
	"kotlin.junit_test_method",
	"kotlin.junit_lifecycle_method",
}

func deadCodeIsKotlinRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "kotlin" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range kotlinDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
