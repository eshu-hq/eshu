// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// buildHandlesRouteRowMap converts a handles_route intent payload into the flat
// UNWIND parameter map for the HANDLES_ROUTE upsert. It skips the row (ok=false)
// when any MATCH key — function_entity_id, repo_id, or path — is empty so an
// unresolvable edge is never written. Provenance fields are passed through from
// the reducer, which derives them from the resolution method.
func buildHandlesRouteRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	functionEntityID := sourcecypher.PayloadString(payload, "function_entity_id")
	repoID := sourcecypher.PayloadString(payload, "repo_id")
	path := sourcecypher.PayloadString(payload, "path")
	if functionEntityID == "" || repoID == "" || path == "" {
		return "", nil, false
	}
	resolutionMethod := sourcecypher.PayloadString(payload, "resolution_method")
	return sourcecypher.BatchCanonicalHandlesRouteEdgeUpsertCypher, map[string]any{
		"function_entity_id": functionEntityID,
		"repo_id":            repoID,
		"path":               path,
		"http_method":        sourcecypher.PayloadString(payload, "http_method"),
		"resolution_method":  resolutionMethod,
		"confidence":         sourcecypher.PayloadFloat(payload, "confidence"),
		"reason":             sourcecypher.PayloadString(payload, "reason"),
		"evidence_source":    evidenceSource,
	}, true
}

func buildDeployableUnitCorrelationRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	repoID := sourcecypher.PayloadString(payload, "repo_id")
	deploymentRepoID := sourcecypher.PayloadString(payload, "deployment_repo_id")
	unitKey := sourcecypher.PayloadString(payload, "deployable_unit_key")
	correlationKey := sourcecypher.PayloadString(payload, "correlation_key")
	if repoID == "" || deploymentRepoID == "" || unitKey == "" || correlationKey == "" {
		return "", nil, false
	}
	relationshipType := sourcecypher.PayloadString(payload, "relationship_type")
	if relationshipType == "" {
		relationshipType = string(edgetype.CorrelatesDeployableUnit)
	}
	return sourcecypher.BatchCanonicalDeployableUnitCorrelationUpsertCypher, map[string]any{
		"repo_id":             repoID,
		"deployment_repo_id":  deploymentRepoID,
		"deployable_unit_key": unitKey,
		"correlation_key":     correlationKey,
		"relationship_type":   relationshipType,
		"evidence_type":       sourcecypher.PayloadString(payload, "evidence_type"),
		"evidence_source":     evidenceSource,
		"resolution_source":   sourcecypher.PayloadString(payload, "resolution_source"),
		"resolved_id":         sourcecypher.PayloadString(payload, "resolved_id"),
		"generation_id":       sourcecypher.PayloadString(payload, "generation_id"),
		"admission_state":     sourcecypher.PayloadString(payload, "admission_state"),
		"confidence":          sourcecypher.RepoRelationshipConfidence(sourcecypher.PayloadFloat(payload, "confidence")),
		"evidence_count":      sourcecypher.PayloadInt(payload, "evidence_count"),
		"evidence_kinds":      sourcecypher.PayloadStringSlice(payload, "evidence_kinds"),
		"rule_pack":           sourcecypher.PayloadString(payload, "rule_pack"),
		"reason":              sourcecypher.PayloadString(payload, "reason"),
	}, true
}
