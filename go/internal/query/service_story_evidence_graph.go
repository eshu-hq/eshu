// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"cmp"
	"slices"
	"strings"
)

type serviceEvidenceNodeFields struct {
	role         string
	canonicalKey string
	scopeKey     string
	repoID       string
}

func buildServiceEvidenceGraph(workloadContext map[string]any) map[string]any {
	nodes := map[string]map[string]any{}
	serviceID := safeStr(workloadContext, "id")
	serviceRepoID := safeStr(workloadContext, "repo_id")
	addEvidenceNode(nodes, serviceID, safeStr(workloadContext, "name"), "service", "service", serviceEvidenceNodeFields{
		role: "workload",
	})
	addEvidenceNode(nodes, serviceRepoID, safeStr(workloadContext, "repo_name"), "repository", "source", serviceEvidenceNodeFields{
		role: "source_repository",
	})
	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		addServiceRepositoryEvidenceNode(nodes, artifact, "source", serviceRepoID)
		addServiceRepositoryEvidenceNode(nodes, artifact, "target", serviceRepoID)
	}
	for _, instance := range mapSliceValue(workloadContext, "instances") {
		addEvidenceNode(
			nodes,
			StringVal(instance, "instance_id"),
			firstNonEmptyString(StringVal(instance, "platform_name"), StringVal(instance, "instance_id")),
			"runtime",
			"runtime",
			serviceEvidenceNodeFields{
				role:   "runtime_instance",
				repoID: serviceRepoID,
			},
		)
	}
	for _, consumer := range mapSliceValue(workloadContext, "consumer_repositories") {
		addEvidenceNode(nodes, StringVal(consumer, "repo_id"), StringVal(consumer, "repository"), "repository", "downstream", serviceEvidenceNodeFields{
			role: "downstream_consumer",
		})
	}
	for _, dependent := range mapSliceValue(workloadContext, "dependents") {
		addEvidenceNode(nodes, StringVal(dependent, "repo_id"), StringVal(dependent, "repository"), "repository", "downstream", serviceEvidenceNodeFields{
			role: "downstream_consumer",
		})
	}
	edges, edgeCount, edgeTruncated := serviceEvidenceGraphEdges(workloadContext)
	return map[string]any{
		"nodes":      sortedEvidenceNodes(nodes),
		"edges":      edges,
		"edge_count": edgeCount,
		"truncated":  edgeTruncated,
	}
}

func serviceEvidenceGraphEdges(workloadContext map[string]any) ([]map[string]any, int, bool) {
	edges := make([]map[string]any, 0)
	seenEdges := map[string]map[string]any{}
	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		resolvedID := StringVal(artifact, "resolved_id")
		if resolvedID == "" {
			continue
		}
		edge := map[string]any{
			"id":                resolvedID,
			"source":            StringVal(artifact, "source_repo_id"),
			"target":            StringVal(artifact, "target_repo_id"),
			"relationship_type": StringVal(artifact, "relationship_type"),
			"confidence":        relationshipFloatVal(artifact, "confidence"),
			"evidence_count":    firstPositiveInt(artifact, "evidence_count"),
			"rationale":         StringVal(artifact, "rationale"),
			"resolved_id":       resolvedID,
		}
		if existing := seenEdges[resolvedID]; existing != nil {
			mergeServiceRelationshipRow(existing, edge)
			continue
		}
		seenEdges[resolvedID] = edge
		edges = append(edges, edge)
	}
	serviceID := safeStr(workloadContext, "id")
	for _, instance := range mapSliceValue(workloadContext, "instances") {
		instanceID := StringVal(instance, "instance_id")
		if serviceID == "" || instanceID == "" {
			continue
		}
		edgeID := visualizationEdgeID(serviceID, instanceID, "RUNS_AS")
		if _, exists := seenEdges[edgeID]; exists {
			continue
		}
		edge := map[string]any{
			"id":                edgeID,
			"source":            serviceID,
			"target":            instanceID,
			"relationship_type": "RUNS_AS",
		}
		seenEdges[edgeID] = edge
		edges = append(edges, edge)
	}
	slices.SortFunc(edges, func(left, right map[string]any) int {
		if relationshipOrder := cmp.Compare(StringVal(left, "relationship_type"), StringVal(right, "relationship_type")); relationshipOrder != 0 {
			return relationshipOrder
		}
		for _, key := range []string{"resolved_id", "source", "target", "id"} {
			if fieldOrder := cmp.Compare(StringVal(left, key), StringVal(right, key)); fieldOrder != 0 {
				return fieldOrder
			}
		}
		return 0
	})
	capped, truncated := capMapRows(edges, serviceStoryItemLimit)
	return capped, len(edges), truncated
}

func addServiceRepositoryEvidenceNode(
	nodes map[string]map[string]any,
	artifact map[string]any,
	endpoint string,
	serviceRepoID string,
) {
	id := StringVal(artifact, endpoint+"_repo_id")
	role := "deployment_configuration"
	category := "deployment"
	if id != "" && id == serviceRepoID {
		role = "source_repository"
		category = "source"
	}
	addEvidenceNode(
		nodes,
		id,
		StringVal(artifact, endpoint+"_repo_name"),
		"repository",
		category,
		serviceEvidenceNodeFields{
			role:         role,
			canonicalKey: StringVal(artifact, endpoint+"_repo_canonical_id"),
			scopeKey:     StringVal(artifact, endpoint+"_repo_scope_key"),
		},
	)
}

func addEvidenceNode(
	nodes map[string]map[string]any,
	id string,
	label string,
	kind string,
	category string,
	fields serviceEvidenceNodeFields,
) {
	if id == "" {
		return
	}
	if label == "" {
		label = id
	}
	node := map[string]any{
		"id":       id,
		"label":    label,
		"kind":     kind,
		"category": category,
		"role":     fields.role,
	}
	if strings.TrimSpace(fields.canonicalKey) != "" {
		node["canonical_key"] = fields.canonicalKey
	}
	if strings.TrimSpace(fields.scopeKey) != "" {
		node["scope_key"] = fields.scopeKey
	}
	if strings.TrimSpace(fields.repoID) != "" {
		node["repo_id"] = fields.repoID
	}
	if existing := nodes[id]; existing != nil {
		if serviceEvidenceNodeRolePriority(StringVal(existing, "role")) > serviceEvidenceNodeRolePriority(StringVal(node, "role")) {
			for key, value := range node {
				if StringVal(existing, key) == "" {
					existing[key] = value
				}
			}
			nodes[id] = existing
			return
		}
		for key, value := range existing {
			if StringVal(node, key) == "" {
				node[key] = value
			}
		}
	}
	nodes[id] = node
}

func serviceEvidenceNodeRolePriority(role string) int {
	switch role {
	case "workload":
		return 5
	case "source_repository":
		return 4
	case "runtime_instance":
		return 3
	case "deployment_configuration":
		return 2
	case "downstream_consumer":
		return 1
	default:
		return 0
	}
}

func sortedEvidenceNodes(nodes map[string]map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, node)
	}
	slices.SortFunc(result, func(left, right map[string]any) int {
		return cmp.Compare(StringVal(left, "id"), StringVal(right, "id"))
	})
	return result
}
