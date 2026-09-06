// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// DeploymentEvidenceDeliveryPaths shapes deployment evidence into delivery
// path rows, and NormalizedDeliveryPathKey keys a delivery path row for
// dedupe; both stay exported because package query forwards to them for the
// repository-story deployment-evidence reader. See #6060.
//
// This file holds the deployment delivery-path builders for the
// trace_deployment_chain response surface. They moved here from the query
// root (deployment_trace_delivery_paths.go) with lane B2 of #6060: the trace
// response shaper in this package is their primary reader, and an
// impacttrace file cannot name root-defined helpers. deploymentEvidence-
// DeliveryPaths and normalizedDeliveryPathKey keep forwarding wrappers in
// package query for the repository-story deployment-evidence reader. None of
// these builders issue a graph Run or RunSingle call, so none are tracked by
// the query-source-coverage gate.

func BuildNormalizedDeliveryPaths(
	deploymentSources []map[string]any,
	cloudResources []map[string]any,
	k8sResources []map[string]any,
	imageRefs []string,
	k8sRelationships []map[string]any,
	deploymentEvidence map[string]any,
) []map[string]any {
	canonical := buildDeliveryPaths(deploymentSources, cloudResources, k8sResources, imageRefs, k8sRelationships)
	evidencePaths := DeploymentEvidenceDeliveryPaths(deploymentEvidence)
	rows := make([]map[string]any, 0, len(canonical)+len(evidencePaths))
	rows = append(rows, canonical...)
	rows = append(rows, evidencePaths...)

	seen := make(map[string]struct{}, len(rows))
	normalized := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry, ok := normalizeDeliveryPathRow(row)
		if !ok {
			continue
		}
		key := NormalizedDeliveryPathKey(entry)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, entry)
	}
	return normalized
}

func DeploymentEvidenceDeliveryPaths(deploymentEvidence map[string]any) []map[string]any {
	deliveryPaths := querycontract.MapSliceValue(deploymentEvidence, "delivery_paths")
	artifacts := querycontract.MapSliceValue(deploymentEvidence, "artifacts")
	rows := make([]map[string]any, 0, len(deliveryPaths)+len(artifacts))
	for _, row := range deliveryPaths {
		entry := querycontract.CloneAnyMap(row)
		if querycontract.StringVal(entry, "type") == "" {
			entry["type"] = "repository_delivery_artifact"
		}
		rows = append(rows, entry)
	}
	for _, artifact := range artifacts {
		entry := querycontract.CloneAnyMap(artifact)
		entry["type"] = "deployment_evidence"
		rows = append(rows, entry)
	}
	return rows
}

func normalizeDeliveryPathRow(row map[string]any) (map[string]any, bool) {
	entry := querycontract.CloneAnyMap(row)
	pathType := strings.TrimSpace(querycontract.StringVal(entry, "type"))
	if pathType == "" {
		return nil, false
	}
	entry["type"] = pathType

	switch pathType {
	case "deployment_source":
		if querycontract.StringVal(entry, "target") == "" && querycontract.StringVal(entry, "target_id") == "" {
			return nil, false
		}
	case "cloud_resource":
		if querycontract.StringVal(entry, "target") == "" && querycontract.StringVal(entry, "target_id") == "" {
			return nil, false
		}
	case "k8s_resource":
		if querycontract.StringVal(entry, "target") == "" && querycontract.StringVal(entry, "target_id") == "" && querycontract.StringVal(entry, "kind") == "" {
			return nil, false
		}
	case "image_ref":
		if querycontract.StringVal(entry, "target") == "" {
			return nil, false
		}
	case "k8s_relationship":
		if querycontract.StringVal(entry, "target") == "" && querycontract.StringVal(entry, "source_name") == "" && querycontract.StringVal(entry, "kind") == "" {
			return nil, false
		}
	case "repository_delivery_artifact":
		if !repositoryDeliveryArtifactHasIdentity(entry) {
			return nil, false
		}
	case "deployment_evidence":
		if !deploymentEvidenceDeliveryPathHasIdentity(entry) {
			return nil, false
		}
	default:
		if !genericDeliveryPathHasIdentity(entry) {
			return nil, false
		}
	}

	return entry, true
}

func repositoryDeliveryArtifactHasIdentity(entry map[string]any) bool {
	return querycontract.StringVal(entry, "path") != "" ||
		querycontract.StringVal(entry, "relative_path") != "" ||
		querycontract.StringVal(entry, "kind") != "" ||
		querycontract.StringVal(entry, "artifact_type") != "" ||
		querycontract.StringVal(entry, "evidence_kind") != "" ||
		querycontract.StringVal(entry, "source_repo") != "" ||
		querycontract.StringVal(entry, "service_name") != "" ||
		querycontract.StringVal(entry, "workflow_name") != ""
}

func deploymentEvidenceDeliveryPathHasIdentity(entry map[string]any) bool {
	return querycontract.StringVal(entry, "resolved_id") != "" ||
		querycontract.StringVal(entry, "path") != "" ||
		querycontract.StringVal(entry, "relative_path") != "" ||
		querycontract.StringVal(entry, "evidence_kind") != "" ||
		querycontract.StringVal(entry, "artifact_family") != "" ||
		querycontract.StringVal(entry, "source_repo_id") != "" ||
		querycontract.StringVal(entry, "source_repo_name") != "" ||
		querycontract.StringVal(entry, "target_repo_id") != "" ||
		querycontract.StringVal(entry, "target_repo_name") != ""
}

func genericDeliveryPathHasIdentity(entry map[string]any) bool {
	return querycontract.StringVal(entry, "target") != "" ||
		querycontract.StringVal(entry, "target_id") != "" ||
		querycontract.StringVal(entry, "path") != "" ||
		querycontract.StringVal(entry, "relative_path") != "" ||
		querycontract.StringVal(entry, "kind") != "" ||
		querycontract.StringVal(entry, "artifact_type") != "" ||
		querycontract.StringVal(entry, "evidence_kind") != ""
}

func NormalizedDeliveryPathKey(entry map[string]any) string {
	pathType := querycontract.StringVal(entry, "type")
	switch pathType {
	case "deployment_source", "cloud_resource":
		targetIdentity := querycontract.StringVal(entry, "target")
		if targetIdentity == "" {
			targetIdentity = querycontract.StringVal(entry, "target_id")
		}
		return pathType + "|" + targetIdentity
	case "k8s_resource":
		targetID := querycontract.StringVal(entry, "target_id")
		if targetID == "" {
			targetID = querycontract.StringVal(entry, "target")
		}
		return pathType + "|" + targetID + "|" + querycontract.StringVal(entry, "kind")
	case "image_ref":
		return pathType + "|" + querycontract.StringVal(entry, "target")
	case "k8s_relationship":
		return strings.Join([]string{
			pathType,
			querycontract.StringVal(entry, "source_name"),
			querycontract.StringVal(entry, "target"),
			querycontract.StringVal(entry, "kind"),
		}, "|")
	case "repository_delivery_artifact":
		return strings.Join([]string{
			pathType,
			querycontract.StringVal(entry, "path"),
			querycontract.StringVal(entry, "relative_path"),
			querycontract.StringVal(entry, "kind"),
			querycontract.StringVal(entry, "artifact_type"),
			querycontract.StringVal(entry, "evidence_kind"),
			querycontract.StringVal(entry, "source_repo"),
			querycontract.StringVal(entry, "service_name"),
		}, "|")
	case "deployment_evidence":
		return strings.Join([]string{
			pathType,
			querycontract.StringVal(entry, "resolved_id"),
			querycontract.StringVal(entry, "relationship_type"),
			querycontract.StringVal(entry, "source_repo_id"),
			querycontract.StringVal(entry, "source_repo_name"),
			querycontract.StringVal(entry, "target_repo_id"),
			querycontract.StringVal(entry, "target_repo_name"),
			querycontract.StringVal(entry, "path"),
			querycontract.StringVal(entry, "relative_path"),
			querycontract.StringVal(entry, "artifact_family"),
			querycontract.StringVal(entry, "evidence_kind"),
		}, "|")
	}

	path := querycontract.StringVal(entry, "path")
	relativePath := querycontract.StringVal(entry, "relative_path")
	workflowName := querycontract.StringVal(entry, "workflow_name")
	if path != "" || relativePath != "" {
		workflowName = ""
	}
	return strings.Join([]string{
		querycontract.StringVal(entry, "type"),
		querycontract.StringVal(entry, "target"),
		querycontract.StringVal(entry, "target_id"),
		path,
		relativePath,
		querycontract.StringVal(entry, "kind"),
		querycontract.StringVal(entry, "artifact_type"),
		querycontract.StringVal(entry, "evidence_kind"),
		querycontract.StringVal(entry, "source_repo"),
		querycontract.StringVal(entry, "service_name"),
		workflowName,
	}, "|")
}
