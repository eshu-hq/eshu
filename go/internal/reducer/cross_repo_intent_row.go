// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// buildResolvedEdgeIntentRows converts resolved relationships to shared
// projection intent rows while preserving typed relationship families.
func buildResolvedEdgeIntentRows(
	resolved []relationships.ResolvedRelationship,
	scopeID string,
	sourceRunID string,
	generationID string,
	createdAt time.Time,
) ([]SharedProjectionIntentRow, map[string]int) {
	rows := make([]SharedProjectionIntentRow, 0, len(resolved))
	routeCounts := make(map[string]int)

	for i, r := range resolved {
		row, routeType, ok := buildResolvedEdgeIntentRow(
			r,
			scopeID,
			sourceRunID,
			generationID,
			i,
			createdAt,
		)
		if !ok {
			continue
		}
		rows = append(rows, row)
		routeCounts[routeType]++
	}

	return rows, routeCounts
}

// buildResolvedEdgeIntentRow shapes one resolved cross-repo relationship into a
// shared projection intent row. The payload carries the edge's provenance
// (evidence_kinds, evidence_type, and the normalized source_tool token derived
// from the same primary evidence kind) plus the typed routing fields.
func buildResolvedEdgeIntentRow(
	r relationships.ResolvedRelationship,
	scopeID string,
	sourceRunID string,
	generationID string,
	ordinal int,
	createdAt time.Time,
) (SharedProjectionIntentRow, string, bool) {
	if r.SourceRepoID == "" {
		return SharedProjectionIntentRow{}, "", false
	}

	payload := map[string]any{
		"repo_id":           r.SourceRepoID,
		"evidence_source":   crossRepoEvidenceSource,
		"resolved_id":       relationships.ResolvedRelationshipID(generationID, r, ordinal),
		"generation_id":     generationID,
		"confidence":        r.Confidence,
		"evidence_count":    r.EvidenceCount,
		"evidence_kinds":    toStringSlice(r.Details["evidence_kinds"]),
		"rationale":         r.Rationale,
		"resolution_source": string(r.ResolutionSource),
	}
	// #5441: allowlist exactly three Details fields onto the edge itself.
	// These answer "which git revision / which namespace / which module
	// version is declared for env Y" directly from the graph edge, which is
	// otherwise unanswerable without a Postgres lookup at query time. Every
	// other Details key deliberately stays out of the payload; see
	// copyRepoRelationshipMetadata in edge_writer_retract.go for the second
	// half of this allowlist (Postgres payload -> graph row).
	payload["source_revision"] = toDetailsString(r.Details["source_revision"])
	payload["destination_namespace"] = toDetailsString(r.Details["destination_namespace"])
	payload["first_party_ref_version"] = extractTerraformRefPinFromDetails(r.Details)

	if evidenceType := resolvedRelationshipEvidenceType(r); evidenceType != "" {
		payload["evidence_type"] = evidenceType
	}
	if sourceTool := resolvedRelationshipSourceTool(r); sourceTool != "" {
		payload["source_tool"] = sourceTool
	}
	if artifacts := resolvedRelationshipEvidenceArtifacts(r); len(artifacts) > 0 {
		payload["evidence_artifacts"] = artifacts
	}

	partitionKey := ""
	routeType := string(r.RelationshipType)

	switch r.RelationshipType {
	case relationships.RelRunsOn:
		if r.TargetEntityID == "" {
			return SharedProjectionIntentRow{}, "", false
		}
		payload["platform_id"] = r.TargetEntityID
		payload["relationship_type"] = string(r.RelationshipType)
		partitionKey = fmt.Sprintf("runs_on:%s->%s", r.SourceRepoID, r.TargetEntityID)
	case relationships.RelDeploysFrom, relationships.RelDiscoversConfigIn, relationships.RelProvisionsDependencyFor:
		if r.TargetRepoID == "" {
			return SharedProjectionIntentRow{}, "", false
		}
		payload["target_repo_id"] = r.TargetRepoID
		payload["relationship_type"] = string(r.RelationshipType)
		partitionKey = fmt.Sprintf("repo:%s->%s|%s", r.SourceRepoID, r.TargetRepoID, r.RelationshipType)
	default:
		if r.TargetRepoID == "" {
			return SharedProjectionIntentRow{}, "", false
		}
		payload["target_repo_id"] = r.TargetRepoID
		payload["relationship_type"] = string(r.RelationshipType)
		partitionKey = fmt.Sprintf("repo:%s->%s|%s", r.SourceRepoID, r.TargetRepoID, r.RelationshipType)
	}

	return BuildSharedProjectionIntent(SharedProjectionIntentInput{
		ProjectionDomain: DomainRepoDependency,
		PartitionKey:     partitionKey,
		ScopeID:          scopeID,
		AcceptanceUnitID: r.SourceRepoID,
		RepositoryID:     r.SourceRepoID,
		SourceRunID:      strings.TrimSpace(sourceRunID),
		GenerationID:     generationID,
		Payload:          payload,
		CreatedAt:        createdAt,
	}), routeType, true
}

// toDetailsString reads a string-typed Details value, returning "" when the
// key is absent or not a string. Mirrors the cypher package's payloadString
// absent-value convention (empty string, never a fabricated placeholder) so
// the value round-trips unchanged into the graph row built by
// copyRepoRelationshipMetadata.
func toDetailsString(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return s
}

// extractTerraformRefPinFromDetails derives the first_party_ref_version edge
// property from the raw pinned Terraform/Terragrunt module source stashed at
// Details["source_ref"] (terraform_evidence.go). Only module-source evidence
// populates source_ref today, so this is naturally "" for every other
// evidence family (e.g. ArgoCD, which carries its own source_revision key
// instead) rather than colliding with it.
func extractTerraformRefPinFromDetails(details map[string]any) string {
	return relationships.ExtractTerraformRefPin(toDetailsString(details["source_ref"]))
}
