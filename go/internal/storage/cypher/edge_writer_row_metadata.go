// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"crypto/sha1" // #nosec G505 -- non-cryptographic stable artifact ID digest, not a security primitive
	"encoding/hex"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// copyRepoRelationshipMetadata preserves durable evidence pointers on graph
// edge writes while keeping the full evidence payload in Postgres.
//
// #5441 widens this narrow allowlist by exactly two fields — source_revision
// and first_party_ref_version, answering "which git revision / which module
// version is declared for env Y" directly from the edge instead of a
// Postgres round trip at query time. Every other Details key still stays out
// of the graph. A third field, destination_namespace, was deliberately
// removed before merge (#5441 review round 2): it has no evidence producer
// on any of the five widened relationship types — see the Candidate doc
// comment in go/internal/relationships/models.go. Absent values copy as ""
// (never omitted, never Cypher null), matching every other optional field
// here (resolution_source, rationale): the pinned NornicDB Go module
// (github.com/orneryd/nornicdb v1.0.45, go/go.mod) per-property SET path
// stores a nil RHS as a literal nil-valued property instead of removing it
// (empirically verified in-process against that exact module -- see
// docs/internal/evidence/5441-edge-node-properties.md), diverging from
// Cypher's remove-on-null semantics — "" is the only value this shared
// writer treats uniformly across both backends.
func copyRepoRelationshipMetadata(rowMap map[string]any, payload map[string]any, rowGenerationID string) {
	rowMap["resolved_id"] = payloadString(payload, "resolved_id")
	generationID := payloadString(payload, "generation_id")
	if generationID == "" {
		generationID = rowGenerationID
	}
	rowMap["generation_id"] = generationID
	rowMap["evidence_count"] = payloadInt(payload, "evidence_count")
	rowMap["evidence_kinds"] = payloadStringSlice(payload, "evidence_kinds")
	rowMap["resolution_source"] = payloadString(payload, "resolution_source")
	rowMap["confidence"] = repoRelationshipConfidence(payloadFloat(payload, "confidence"))
	rowMap["rationale"] = payloadString(payload, "rationale")
	rowMap["source_revision"] = payloadString(payload, "source_revision")
	rowMap["first_party_ref_version"] = payloadString(payload, "first_party_ref_version")
}

// repoEvidenceArtifactRowsFromIntent builds bounded graph nodes from reducer
// evidence summaries while preserving raw detail ownership in Postgres.
// ref_value/ref_pinned (issue #5372) are carried through unchanged when the
// reducer computed them (GitHub Actions evidence only); this function is not
// the place that classifies a ref as pinned.
func repoEvidenceArtifactRowsFromIntent(
	row reducer.SharedProjectionIntentRow,
	evidenceSource string,
) []map[string]any {
	payload := row.Payload
	repoID := payloadString(payload, "repo_id")
	targetRepoID := payloadString(payload, "target_repo_id")
	if repoID == "" || targetRepoID == "" {
		return nil
	}
	artifacts := payloadMapSlice(payload, "evidence_artifacts")
	if len(artifacts) == 0 {
		return nil
	}

	relationshipType := payloadString(payload, "relationship_type")
	resolvedID := payloadString(payload, "resolved_id")
	generationID := payloadString(payload, "generation_id")
	if generationID == "" {
		generationID = row.GenerationID
	}
	rows := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		evidenceKind := payloadString(artifact, "evidence_kind")
		path := payloadString(artifact, "path")
		matchedValue := payloadString(artifact, "matched_value")
		fluxName := payloadString(artifact, "flux_git_repository_name")
		fluxNamespace := payloadString(artifact, "flux_git_repository_namespace")
		name := path
		if name == "" {
			name = evidenceKind
		}
		identitySuffix := []string(nil)
		if evidenceKind == "FLUX_GIT_REPOSITORY_SOURCE" {
			identitySuffix = []string{fluxNamespace, fluxName}
		}
		artifactID := repoEvidenceArtifactID(resolvedID, evidenceKind, path, matchedValue, identitySuffix...)
		row := map[string]any{
			"artifact_id":                   artifactID,
			"name":                          name,
			"repo_id":                       repoID,
			"target_repo_id":                targetRepoID,
			"relationship_type":             relationshipType,
			"resolved_id":                   resolvedID,
			"generation_id":                 generationID,
			"evidence_kind":                 evidenceKind,
			"artifact_family":               payloadString(artifact, "artifact_family"),
			"path":                          path,
			"extractor":                     payloadString(artifact, "extractor"),
			"environment":                   payloadString(artifact, "environment"),
			"runtime_platform_kind":         payloadString(artifact, "runtime_platform_kind"),
			"matched_alias":                 payloadString(artifact, "matched_alias"),
			"matched_value":                 matchedValue,
			"flux_git_repository_name":      fluxName,
			"flux_git_repository_namespace": fluxNamespace,
			"confidence":                    payloadFloat(artifact, "confidence"),
			"evidence_source":               evidenceSource,
		}
		// Propagate byte-level citation fields when the artifact carries them so
		// the EvidenceArtifact graph node exposes start_line/end_line/commit_sha
		// to the query surface. Absent fields are omitted to avoid zero-noise.
		if sl := payloadInt(artifact, "start_line"); sl > 0 {
			row["start_line"] = sl
		}
		if el := payloadInt(artifact, "end_line"); el > 0 {
			row["end_line"] = el
		}
		if sha := payloadString(artifact, "commit_sha"); sha != "" {
			row["commit_sha"] = sha
		}
		// GitHub Actions @ref pin signal (issue #5372). The reducer
		// (resolvedRelationshipEvidenceArtifacts) is the sole place
		// ref_value/ref_pinned are computed, scoped there to
		// GITHUB_ACTIONS_* evidence kinds; this builder only carries the
		// two fields through together, it never recomputes Pinned() or
		// widens the scope. Omitted together when the artifact carries no
		// ref_value (local ./ workflow, docker action, or a non-GitHub-
		// Actions evidence kind).
		if refValue := payloadString(artifact, "ref_value"); refValue != "" {
			row["ref_value"] = refValue
			row["ref_pinned"] = payloadBool(artifact, "ref_pinned")
		}
		rows = append(rows, row)
	}
	return rows
}

func repoEvidenceArtifactID(resolvedID string, evidenceKind string, path string, matchedValue string, identitySuffix ...string) string {
	identity := append([]string{resolvedID, evidenceKind, path, matchedValue}, identitySuffix...)
	hash := sha1.Sum([]byte(strings.Join(identity, "\x00"))) // #nosec G401 -- non-cryptographic stable evidence artifact ID, not a security primitive
	return "evidence-artifact:" + hex.EncodeToString(hash[:8])
}
