package query

import (
	"fmt"
	"strings"
)

func buildDeploymentProvenanceStory(controllerEntities, deploymentSources []map[string]any) string {
	parts := make([]string, 0, 2)
	if len(controllerEntities) > 0 {
		summaries := make([]string, 0, len(controllerEntities))
		for _, controller := range controllerEntities {
			summary := StringVal(controller, "entity_name")
			if summary == "" {
				summary = StringVal(controller, "entity_id")
			}
			if controllerKind := StringVal(controller, "controller_kind"); controllerKind != "" {
				summary += " (" + controllerKind + ")"
			}
			if sourceRepo := StringVal(controller, "source_repo"); sourceRepo != "" {
				summary += " from " + sourceRepo
			}
			summaries = append(summaries, summary)
		}
		parts = append(parts, "Controller provenance: "+joinSentenceFragments(summaries)+".")
	}
	if len(deploymentSources) > 0 {
		summaries := make([]string, 0, len(deploymentSources))
		for _, source := range deploymentSources {
			summary := StringVal(source, "repo_name")
			if summary == "" {
				summary = StringVal(source, "repo_id")
			}
			if reason := StringVal(source, "reason"); reason != "" {
				summary += " via " + reason
			}
			summaries = append(summaries, summary)
		}
		parts = append(parts, "Deployment sources: "+joinSentenceFragments(summaries)+".")
	}
	return strings.Join(parts, " ")
}

func appendDeploymentTraceStory(base string, addition string) string {
	base = strings.TrimSpace(base)
	addition = strings.TrimSpace(addition)
	if addition == "" {
		return base
	}
	if base == "" {
		return addition
	}
	return base + " " + addition
}

func deploymentMappingMode(platformKinds []string, deploymentSources []map[string]any) string {
	for _, kind := range platformKinds {
		if kind == "argocd_application" || kind == "argocd_applicationset" {
			return "controller"
		}
	}
	if len(deploymentSources) > 0 {
		return "deployment_source"
	}
	if len(platformKinds) > 0 {
		return "evidence_only"
	}
	return "none"
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return fmt.Sprintf("%v", values)
}
