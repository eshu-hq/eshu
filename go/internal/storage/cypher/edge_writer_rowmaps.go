// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "github.com/eshu-hq/eshu/go/internal/graph/edgetype"

// buildHandlesRouteRowMap converts a handles_route intent payload into the flat
// UNWIND parameter map for the HANDLES_ROUTE upsert. It skips the row (ok=false)
// when any MATCH key — function_entity_id, repo_id, or path — is empty so an
// unresolvable edge is never written. Provenance fields are passed through from
// the reducer, which derives them from the resolution method.
func buildHandlesRouteRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	functionEntityID := payloadString(payload, "function_entity_id")
	repoID := payloadString(payload, "repo_id")
	path := payloadString(payload, "path")
	if functionEntityID == "" || repoID == "" || path == "" {
		return "", nil, false
	}
	resolutionMethod := payloadString(payload, "resolution_method")
	return batchCanonicalHandlesRouteEdgeUpsertCypher, map[string]any{
		"function_entity_id": functionEntityID,
		"repo_id":            repoID,
		"path":               path,
		"http_method":        payloadString(payload, "http_method"),
		"resolution_method":  resolutionMethod,
		"confidence":         payloadFloat(payload, "confidence"),
		"reason":             payloadString(payload, "reason"),
		"evidence_source":    evidenceSource,
	}, true
}

func buildDeployableUnitCorrelationRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	repoID := payloadString(payload, "repo_id")
	deploymentRepoID := payloadString(payload, "deployment_repo_id")
	unitKey := payloadString(payload, "deployable_unit_key")
	correlationKey := payloadString(payload, "correlation_key")
	if repoID == "" || deploymentRepoID == "" || unitKey == "" || correlationKey == "" {
		return "", nil, false
	}
	relationshipType := payloadString(payload, "relationship_type")
	if relationshipType == "" {
		relationshipType = string(edgetype.CorrelatesDeployableUnit)
	}
	return batchCanonicalDeployableUnitCorrelationUpsertCypher, map[string]any{
		"repo_id":             repoID,
		"deployment_repo_id":  deploymentRepoID,
		"deployable_unit_key": unitKey,
		"correlation_key":     correlationKey,
		"relationship_type":   relationshipType,
		"evidence_type":       payloadString(payload, "evidence_type"),
		"evidence_source":     evidenceSource,
		"resolution_source":   payloadString(payload, "resolution_source"),
		"resolved_id":         payloadString(payload, "resolved_id"),
		"generation_id":       payloadString(payload, "generation_id"),
		"admission_state":     payloadString(payload, "admission_state"),
		"confidence":          repoRelationshipConfidence(payloadFloat(payload, "confidence")),
		"evidence_count":      payloadInt(payload, "evidence_count"),
		"evidence_kinds":      payloadStringSlice(payload, "evidence_kinds"),
		"rule_pack":           payloadString(payload, "rule_pack"),
		"reason":              payloadString(payload, "reason"),
	}, true
}
