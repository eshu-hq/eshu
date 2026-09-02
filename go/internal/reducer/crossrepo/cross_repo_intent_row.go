// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"fmt"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
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
) ([]sharedintent.Row, map[string]int) {
	rows := make([]sharedintent.Row, 0, len(resolved))
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
) (sharedintent.Row, string, bool) {
	if r.SourceRepoID == "" {
		return sharedintent.Row{}, "", false
	}

	payload := map[string]any{
		"repo_id":           r.SourceRepoID,
		"evidence_source":   CrossRepoEvidenceSource,
		"resolved_id":       relationships.ResolvedRelationshipID(generationID, r, ordinal),
		"generation_id":     generationID,
		"confidence":        r.Confidence,
		"evidence_count":    r.EvidenceCount,
		"evidence_kinds":    payloadcore.ToStringSlice(r.Details["evidence_kinds"]),
		"rationale":         r.Rationale,
		"resolution_source": string(r.ResolutionSource),
	}
	// #5441: allowlist exactly two fields onto the edge itself. These answer
	// "which git revision / which module version is declared for env Y"
	// directly from the graph edge, which is otherwise unanswerable without
	// a Postgres lookup at query time. Read as typed ResolvedRelationship
	// fields, NOT from the untyped r.Details map — r.Details (built by
	// relationships.aggregateCandidate) carries only
	// "evidence_kinds"/"evidence_preview" and never held these keys; reading
	// them from Details was the #5441 P0 (this data always resolved to "" in
	// production). See relationships.evidenceFactSourceRevision/
	// evidenceFactFirstPartyRefVersion
	// (go/internal/relationships/evidence_edge_fields.go) for where these
	// typed fields are actually populated from raw evidence facts, and
	// copyRepoRelationshipMetadata in edge_writer_row_metadata.go for the
	// second half of this allowlist (Postgres payload -> graph row).
	//
	// A third field, destination_namespace, was deliberately removed before
	// merge (#5441 review round 2, DECISION NEEDED item): it has no evidence
	// producer on any of the five widened relationship types (see the
	// Candidate doc comment in relationships/models.go), so it would have
	// shipped as a permanently-empty property with no producer, forever.
	payload["source_revision"] = r.SourceRevision
	payload["first_party_ref_version"] = r.FirstPartyRefVersion

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
			return sharedintent.Row{}, "", false
		}
		payload["platform_id"] = r.TargetEntityID
		payload["relationship_type"] = string(r.RelationshipType)
		partitionKey = fmt.Sprintf("runs_on:%s->%s", r.SourceRepoID, r.TargetEntityID)
	case relationships.RelDeploysFrom, relationships.RelDiscoversConfigIn, relationships.RelProvisionsDependencyFor:
		if r.TargetRepoID == "" {
			return sharedintent.Row{}, "", false
		}
		payload["target_repo_id"] = r.TargetRepoID
		payload["relationship_type"] = string(r.RelationshipType)
		partitionKey = fmt.Sprintf("repo:%s->%s|%s", r.SourceRepoID, r.TargetRepoID, r.RelationshipType)
	default:
		if r.TargetRepoID == "" {
			return sharedintent.Row{}, "", false
		}
		payload["target_repo_id"] = r.TargetRepoID
		payload["relationship_type"] = string(r.RelationshipType)
		partitionKey = fmt.Sprintf("repo:%s->%s|%s", r.SourceRepoID, r.TargetRepoID, r.RelationshipType)
	}

	return sharedintent.Build(sharedintent.Input{
		ProjectionDomain: reducercontract.DomainRepoDependency,
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

// ExtractRepoDependencyIntentRows converts resolved cross-repo relationships
// into repo_dependency shared-projection intent rows through the exact pure
// conversion buildResolvedEdgeIntentRow applies in production (Handle step 5,
// cross_repo_resolution.go). It performs no I/O and reads no other
// process-global state, so it is safe for Ifá's repo_dependency
// materialized-edge vacuity guard to call directly against a cataloged Odù's
// own resolved relationships -- go/internal/ifa/AGENTS.md forbids wall-clock
// time inside Ifá derivation, so createdAt must be an injected, deterministic
// value, never time.Now(), exactly as ExtractDeployableUnitCorrelationRows's
// now parameter requires for the deployable_unit_edges guard.
//
// The second return is the per-relationship-type route count
// buildResolvedEdgeIntentRows already tallies, exposed so a caller can prove
// which relationship types a resolved set actually produced without
// re-deriving that from the returned rows' Payload.
func ExtractRepoDependencyIntentRows(
	resolved []relationships.ResolvedRelationship,
	scopeID string,
	sourceRunID string,
	generationID string,
	createdAt time.Time,
) ([]sharedintent.Row, map[string]int) {
	return buildResolvedEdgeIntentRows(resolved, scopeID, sourceRunID, generationID, createdAt)
}
