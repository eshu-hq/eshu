// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
)

func buildServiceCodeToRuntimeTrace(workloadContext map[string]any) map[string]any {
	segments := []map[string]any{
		serviceTraceIdentitySegment(workloadContext),
		serviceTraceCodeEntrypointSegment(workloadContext),
		serviceTraceCICDSegment(workloadContext),
		serviceTraceImagePackageSegment(workloadContext),
		serviceTraceDeploymentConfigSegment(workloadContext),
		serviceTraceRuntimeSegment(workloadContext),
		serviceTraceCloudDependencySegment(workloadContext),
	}
	missing := make([]string, 0)
	for _, segment := range segments {
		if StringVal(segment, "status") == "missing_evidence" {
			missing = append(missing, StringVal(segment, "name"))
		}
	}
	status := "complete"
	if len(missing) > 0 {
		status = "partial"
	}
	return map[string]any{
		"status":           status,
		"segments":         segments,
		"missing_segments": missing,
		"limit":            serviceStoryItemLimit,
		"ordering":         "code_to_runtime",
	}
}

func serviceTraceIdentitySegment(workloadContext map[string]any) map[string]any {
	evidence := make([]map[string]any, 0, 1)
	if safeStr(workloadContext, "id") != "" || safeStr(workloadContext, "repo_id") != "" {
		evidence = append(evidence, map[string]any{
			"service_id":   safeStr(workloadContext, "id"),
			"service_name": safeStr(workloadContext, "name"),
			"repo_id":      safeStr(workloadContext, "repo_id"),
			"repo_name":    safeStr(workloadContext, "repo_name"),
		})
	}
	return serviceTraceSegment("service_identity", "workload_identity", "exact", evidence)
}

func serviceTraceCodeEntrypointSegment(workloadContext map[string]any) map[string]any {
	evidence := make([]map[string]any, 0)
	for _, endpoint := range mapSliceValue(mapValue(workloadContext, "api_surface"), "endpoints") {
		evidence = append(evidence, map[string]any{
			"type":          "api_endpoint",
			"path":          StringVal(endpoint, "path"),
			"methods":       StringSliceVal(endpoint, "methods"),
			"operation_ids": StringSliceVal(endpoint, "operation_ids"),
			"spec_path":     StringVal(endpoint, "spec_path"),
		})
	}
	for _, entrypoint := range mapSliceValue(workloadContext, "entrypoints") {
		evidence = append(evidence, map[string]any{
			"type":        firstNonEmptyString(StringVal(entrypoint, "type"), "service_entrypoint"),
			"target":      firstNonEmptyString(StringVal(entrypoint, "target"), StringVal(entrypoint, "hostname")),
			"environment": StringVal(entrypoint, "environment"),
			"visibility":  StringVal(entrypoint, "visibility"),
		})
	}
	return serviceTraceSegment("code_entrypoints", "api_surface_or_entrypoints", "derived", evidence)
}

func serviceTraceCICDSegment(workloadContext map[string]any) map[string]any {
	if ciCDEvidence := mapValue(workloadContext, "ci_cd_evidence"); len(ciCDEvidence) > 0 {
		segment := serviceTraceSegment(
			"ci_cd",
			"ci_cd_run_correlation_readback",
			"derived",
			[]map[string]any{{"evidence_summary": ciCDEvidence}},
		)
		segment["evidence_summary"] = ciCDEvidence
		return segment
	}
	return serviceTraceSegment(
		"ci_cd",
		"delivery_workflows",
		"derived",
		serviceTraceRowsFromDeploymentEvidence(workloadContext, "delivery_workflows"),
	)
}

func serviceTraceImagePackageSegment(workloadContext map[string]any) map[string]any {
	if supplyChain := serviceStorySupplyChainImagePackage(workloadContext); len(supplyChain) > 0 {
		evidence := mapSliceValue(supplyChain, "evidence")
		missing := StringSliceVal(supplyChain, "missing_evidence")
		segment := serviceTraceSegment("image_package", "container_image_identity_and_sbom_attachment", "exact", evidence)
		segment["candidate_image_ref_count"] = IntVal(supplyChain, "candidate_image_ref_count")
		segment["candidate_image_refs"] = StringSliceVal(supplyChain, "candidate_image_refs")
		segment["image_refs_truncated"] = BoolVal(supplyChain, "image_refs_truncated")
		segment["missing_evidence"] = missing
		if details := mapSliceValue(supplyChain, "missing_evidence_details"); len(details) > 0 {
			segment["missing_evidence_details"] = details
		}
		return segment
	}

	evidence := make([]map[string]any, 0)
	for _, key := range []string{"artifacts", "delivery_paths", "delivery_workflows"} {
		for _, row := range mapSliceValue(mapValue(workloadContext, "deployment_evidence"), key) {
			evidence = append(evidence, serviceTraceImagePackageRows(row)...)
		}
	}
	sort.Slice(evidence, func(i, j int) bool {
		left := firstNonEmptyString(StringVal(evidence[i], "image_ref"), StringVal(evidence[i], "package_ref"), StringVal(evidence[i], "path"))
		right := firstNonEmptyString(StringVal(evidence[j], "image_ref"), StringVal(evidence[j], "package_ref"), StringVal(evidence[j], "path"))
		return left < right
	})
	return serviceTraceSegment("image_package", "container_image_or_package_evidence", "derived", evidence)
}

func serviceTraceImagePackageRows(row map[string]any) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, key := range []string{"image_ref", "container_image", "package_ref", "package_id", "package_name", "purl"} {
		if value := StringVal(row, key); value != "" {
			rows = append(rows, serviceTraceImagePackageRow(row, key, value))
		}
	}
	for _, key := range []string{"image_refs", "container_images", "packages", "package_refs", "purls"} {
		for _, value := range StringSliceVal(row, key) {
			rows = append(rows, serviceTraceImagePackageRow(row, key, value))
		}
	}
	if value := serviceStoryMatchedImageRef(row); value != "" {
		rows = append(rows, serviceTraceImagePackageRow(row, "matched_value", value))
	}
	return rows
}

func serviceTraceImagePackageRow(row map[string]any, key string, value string) map[string]any {
	out := map[string]any{
		"path":        StringVal(row, "path"),
		"tool_family": StringVal(row, "tool_family"),
		"source_key":  key,
	}
	if strings.Contains(key, "image") || key == "matched_value" {
		out["image_ref"] = value
		return out
	}
	out["package_ref"] = value
	return out
}

func serviceTraceDeploymentConfigSegment(workloadContext map[string]any) map[string]any {
	evidence := make([]map[string]any, 0)
	evidence = append(evidence, serviceTraceRowsFromDeploymentEvidence(workloadContext, "artifacts")...)
	evidence = append(evidence, serviceTraceRowsFromDeploymentEvidence(workloadContext, "delivery_paths")...)
	evidence = append(evidence, serviceTraceRowsFromDeploymentEvidence(workloadContext, "shared_config_paths")...)
	return serviceTraceSegment("deployment_config", "deployment_evidence", "derived", evidence)
}

func serviceTraceRuntimeSegment(workloadContext map[string]any) map[string]any {
	return serviceTraceSegment("runtime", "materialized_workload_instances", "exact", mapSliceValue(workloadContext, "instances"))
}

func serviceTraceCloudDependencySegment(workloadContext map[string]any) map[string]any {
	if resources := mapSliceValue(workloadContext, "cloud_resources"); len(resources) > 0 {
		segment := serviceTraceSegment("cloud_dependencies", "cloud_resource_evidence", "derived", resources)
		segment["promoted_count"] = len(resources)
		segment["missing_evidence"] = []string{}
		return segment
	}
	candidates := mapSliceValue(workloadContext, "uncorrelated_cloud_resources")
	if len(candidates) == 0 {
		return serviceTraceSegment("cloud_dependencies", "cloud_resource_evidence", "missing_evidence", nil)
	}
	segment := serviceTraceSegment("cloud_dependencies", "uncorrelated_cloud_resource_candidates", "missing_evidence", candidates)
	segment["candidate_count"] = len(candidates)
	segment["missing_relationship"] = "workload_cloud_relationship"
	segment["promoted_count"] = 0
	segment["missing_evidence"] = serviceTraceCloudDependencyMissingEvidence(candidates)
	return segment
}

func serviceTraceCloudDependencyMissingEvidence(candidates []map[string]any) []string {
	if len(candidates) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	missing := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		reason := serviceTraceCloudDependencyCandidateMissingEvidence(candidate)
		if reason == "" {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		missing = append(missing, reason)
	}
	sort.Strings(missing)
	return missing
}

func serviceTraceCloudDependencyCandidateMissingEvidence(candidate map[string]any) string {
	if reason := StringVal(candidate, "service_anchor_reason"); reason != "" {
		return reason
	}
	switch StringVal(candidate, "candidate_status") {
	case "ambiguous_anchor":
		return "ambiguous_cloud_resource_anchor"
	case "stale_anchor":
		return "stale_deployment_evidence"
	case "weak_anchor":
		return "weak_cloud_resource_anchor"
	default:
		return "workload_cloud_relationship_missing"
	}
}

func serviceTraceRowsFromDeploymentEvidence(workloadContext map[string]any, key string) []map[string]any {
	rows := mapSliceValue(mapValue(workloadContext, "deployment_evidence"), key)
	if len(rows) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, copyMap(row))
	}
	return out
}

func serviceTraceSegment(name string, basis string, presentStatus string, evidence []map[string]any) map[string]any {
	if evidence == nil {
		evidence = []map[string]any{}
	}
	capped, truncated := capMapRows(evidence, serviceStoryItemLimit)
	status := presentStatus
	if len(evidence) == 0 {
		status = "missing_evidence"
	}
	return map[string]any{
		"name":           name,
		"status":         status,
		"basis":          basis,
		"evidence_count": len(evidence),
		"evidence":       capped,
		"truncated":      truncated,
	}
}
