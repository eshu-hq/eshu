// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// containerImageBuiltFromProvenanceEvidenceSource tags BUILT_FROM edges
// projected from container_image_identity decisions. BUILT_FROM is a shared
// edge type with the #5428 ci_cd_run_correlation domain
// (reducer/ci-cd-run-correlation); the distinct evidence_source is what keeps
// this domain's retract-first pass from ever touching that domain's edges
// (docs/internal/design/5472-graph-projection-policy.md).
const containerImageBuiltFromProvenanceEvidenceSource = "reducer/container-image-identity"

// ContainerImageProvenanceEdgeWriter persists and retracts canonical
// BUILT_FROM edges between a ContainerImage and the Repository its identity
// decision resolved as build source. Implementations MUST be idempotent by
// (image digest, BUILT_FROM, repository id) so reducer retries and
// re-projected generations converge on one edge, and MUST NOT fabricate an
// endpoint node: a row whose image or repository node is absent is a no-op.
type ContainerImageProvenanceEdgeWriter interface {
	WriteBuiltFromEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractBuiltFromEdges(ctx context.Context, scopeID, generationID, evidenceSource string) error
}

// containerImageBuiltFromRows builds BUILT_FROM edge rows from exact_digest
// container-image-identity decisions with at least one resolved source
// repository (#5472 exact-only tiering for this edge -- stricter than
// PUBLISHES, which also admits derived). A decision naming more than one
// source repository fans out to one row per distinct repository id, since
// BUILT_FROM has no cardinality limit on the Repository side. Non-exact
// outcomes and decisions with no resolved source repository never produce a
// row.
func containerImageBuiltFromRows(decisions []ContainerImageIdentityDecision) []map[string]any {
	rows := make([]map[string]any, 0, len(decisions))
	for _, decision := range decisions {
		if decision.Outcome != ContainerImageIdentityExactDigest {
			continue
		}
		digest := strings.TrimSpace(decision.Digest)
		if digest == "" {
			continue
		}
		for _, repositoryID := range uniqueSortedStrings(decision.SourceRepositoryIDs) {
			if repositoryID == "" {
				continue
			}
			rows = append(rows, map[string]any{
				"digest":        digest,
				"repository_id": repositoryID,
			})
		}
	}
	return rows
}

// projectContainerImageBuiltFromEdges retracts this generation's prior
// BUILT_FROM edges owned by this evidence_source and re-projects the current
// exact_digest decisions. It is a no-op when no ProvenanceEdgeWriter is
// wired, so the container-image-identity profile stays Postgres-only until an
// adapter is configured. It never fails the identity result for an empty
// projection; only a writer error propagates.
//
// Retract runs unconditionally (ahead of any row check) so a generation that
// drops a previously-exact decision still removes that decision's stale edge
// (#5472 retract-first-per-generation).
func (h ContainerImageIdentityHandler) projectContainerImageBuiltFromEdges(
	ctx context.Context,
	intent Intent,
	decisions []ContainerImageIdentityDecision,
) error {
	if h.ProvenanceEdgeWriter == nil {
		return nil
	}

	if err := h.ProvenanceEdgeWriter.RetractBuiltFromEdges(
		ctx, intent.ScopeID, intent.GenerationID, containerImageBuiltFromProvenanceEvidenceSource,
	); err != nil {
		return fmt.Errorf("retract container image built_from provenance edges: %w", err)
	}

	rows := containerImageBuiltFromRows(decisions)
	h.emitProvenanceEdgeCounter(ctx, "materialized", len(rows))
	if len(rows) == 0 {
		return nil
	}
	if err := h.ProvenanceEdgeWriter.WriteBuiltFromEdges(
		ctx, rows, intent.ScopeID, intent.GenerationID, containerImageBuiltFromProvenanceEvidenceSource,
	); err != nil {
		return fmt.Errorf("write container image built_from provenance edges: %w", err)
	}
	return nil
}

// emitProvenanceEdgeCounter records a ProvenanceEdges counter sample for the
// container-image-identity BUILT_FROM projection, labeled by outcome
// (materialized/skipped). It is a no-op when no Instruments are wired or the
// count is zero.
func (h ContainerImageIdentityHandler) emitProvenanceEdgeCounter(ctx context.Context, outcome string, count int) {
	if h.Instruments == nil || h.Instruments.ProvenanceEdges == nil || count <= 0 {
		return
	}
	h.Instruments.ProvenanceEdges.Add(
		ctx,
		int64(count),
		metric.WithAttributes(
			telemetry.AttrDomain(containerImageBuiltFromProvenanceEvidenceSource),
			telemetry.AttrOutcome(outcome),
		),
	)
}
