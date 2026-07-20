// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func buildDeliveryPaths(
	deploymentSources []map[string]any,
	cloudResources []map[string]any,
	k8sResources []map[string]any,
	imageRefs []string,
	k8sRelationships []map[string]any,
) []map[string]any {
	paths := make([]map[string]any, 0, len(deploymentSources)+len(cloudResources)+len(k8sResources)+len(imageRefs)+len(k8sRelationships))
	for _, source := range deploymentSources {
		paths = append(paths, map[string]any{
			"type":       "deployment_source",
			"target":     safeStr(source, "repo_name"),
			"target_id":  safeStr(source, "repo_id"),
			"confidence": floatVal(source, "confidence"),
		})
	}
	for _, resource := range cloudResources {
		paths = append(paths, map[string]any{
			"type":       "cloud_resource",
			"target":     safeStr(resource, "name"),
			"target_id":  safeStr(resource, "id"),
			"confidence": floatVal(resource, "confidence"),
		})
	}
	for _, resource := range k8sResources {
		paths = append(paths, map[string]any{
			"type":      "k8s_resource",
			"target":    safeStr(resource, "entity_name"),
			"target_id": safeStr(resource, "entity_id"),
			"kind":      safeStr(resource, "kind"),
		})
	}
	for _, imageRef := range imageRefs {
		paths = append(paths, map[string]any{
			"type":   "image_ref",
			"target": imageRef,
		})
	}
	for _, relationship := range k8sRelationships {
		paths = append(paths, map[string]any{
			"type":        "k8s_relationship",
			"target":      safeStr(relationship, "target_name"),
			"target_id":   safeStr(relationship, "target_id"),
			"source_name": safeStr(relationship, "source_name"),
			"reason":      safeStr(relationship, "reason"),
			"kind":        safeStr(relationship, "type"),
		})
	}
	return paths
}

func buildDeploymentDrilldowns(serviceName, workloadID string) map[string]any {
	return map[string]any{
		"service_context_path":  "/api/v0/services/" + serviceName + "/context",
		"service_story_path":    "/api/v0/services/" + serviceName + "/story",
		"workload_context_path": "/api/v0/workloads/" + workloadID + "/context",
	}
}
