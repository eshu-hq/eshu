package query

import (
	"slices"
	"strings"
)

var javaDeadCodeMetadataRootKinds = []string{
	"java.main_method",
	"java.constructor",
	"java.override_method",
	"java.ant_task_setter",
	"java.gradle_plugin_apply",
	"java.gradle_task_action",
	"java.gradle_task_property",
	"java.gradle_task_setter",
	"java.gradle_task_interface_method",
	"java.gradle_dsl_public_method",
	"java.method_reference_target",
	"java.spring_component_class",
	"java.spring_configuration_properties_class",
	"java.spring_request_mapping_method",
	"java.spring_bean_method",
	"java.spring_event_listener_method",
	"java.spring_scheduled_method",
	"java.lifecycle_callback_method",
	"java.junit_test_method",
	"java.junit_lifecycle_method",
	"java.jenkins_extension_class",
	"java.jenkins_symbol_class",
	"java.jenkins_symbol_method",
	"java.jenkins_initializer_method",
	"java.jenkins_databound_setter_method",
	"java.stapler_web_method",
	"java.serialization_hook_method",
	"java.externalizable_hook_method",
}

func deadCodeIsJavaRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "java" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range javaDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
