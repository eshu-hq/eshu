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
// projected from container_image_identity decisions. container_image_identity
// is the sole writer of BUILT_FROM today. A #5428 ci_cd_run_correlation writer
// sharing this edge type was implemented and then rescinded before shipping
// (docs/internal/evidence/5428-built-from-projection-rescinded.md): the
// canonical MERGE identity matches on (start, end, type) only, ignoring
// evidence_source, so a second writer on the same (digest, repository) pair
// would collapse onto this domain's edge instead of being isolated from it
// by evidence_source (#5827). A second BUILT_FROM writer MUST NOT land until
// #5827 is fixed (docs/internal/design/5472-graph-projection-policy.md).
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
// container-image-identity decisions with at least one build-provenance
// repository (#5472 exact-only tiering for this edge -- stricter than
// PUBLISHES, which also admits derived). A decision naming more than one
// build-provenance repository fans out to one row per distinct repository id,
// since BUILT_FROM has no cardinality limit on the Repository side. Non-exact
// outcomes and decisions with no build-provenance repository never produce a
// row.
//
// This gates on BuildProvenanceRepositoryIDs, not the broader
// SourceRepositoryIDs (#5796). SourceRepositoryIDs also collects the
// repository whose Kubernetes manifest merely REFERENCES a digest-pinned
// third-party image (containerImageSourceRepositoryIDs anchors on the
// deploying repository's own envelope scope), so gating on it let BUILT_FROM
// claim a repository built an image it only deploys. BuildProvenanceRepositoryIDs
// is populated only from genuine build evidence -- an OCI config source label
// the image itself carries, or a CI run that reported producing this digest
// (extractOCIConfigBuildProvenanceRefs, addCICDArtifactImageReference) -- the
// same field and semantics #5460/PR #5793 (merged) uses to gate the
// DERIVED_FROM child side, so both provenance edges share one signal. The
// broader SourceRepositoryIDs has no other legitimate BUILT_FROM consumer:
// nothing else reads it off this decision type for this edge, so narrowing
// loses no intended coverage.
//
// Rows are de-duplicated across decisions, not only within one. One intent
// routinely holds several decisions for the SAME digest -- a ci.artifact's
// bare-digest ref and a deploying repository's explicit image reference both
// resolve to one image, and since #5426 both carry the same build-provenance
// repository -- which would otherwise UNWIND the identical (digest, repository)
// pair once per decision. The graph outcome is unchanged either way because the
// canonical writer MERGEs on (start, end, type), so this is a payload and
// counter fix, not a correctness one: it keeps the write batch proportional to
// distinct submitted rows and keeps the "materialized" ProvenanceEdges sample
// counting those rather than one per (decision x build-provenance repository)
// pair. Not "edges" -- the sample is len(rows) before the write, and a row whose
// endpoint node is absent still counts (#5828).
func containerImageBuiltFromRows(decisions []ContainerImageIdentityDecision) []map[string]any {
	rows := make([]map[string]any, 0, len(decisions))
	// A comparable two-string struct rather than a concatenated key. At 32 bytes
	// it stays far under the 128-byte limit above which Go boxes a map key, and
	// it removed the concatenation's +5,017 allocs/op on the N=5000 cost-budget
	// benchmark.
	//
	// Be precise about what that bought: the concatenation cost ~7% of wall
	// time, not the whole regression. Deduping at all costs ~29% over the
	// no-dedup baseline (970k -> 1,250k ns/op median), and ALL of that ships --
	// the map is roughly three quarters of the concatenated variant's larger
	// ~38% regression, and removing the concatenation left the map behind. That is a
	// deliberate trade -- a duplicate row still costs a MERGE round in the
	// graph writer -- and it is recorded as a measured regression in
	// docs/internal/evidence/5426-corroborated-vs-declared-environment.md
	// rather than left implied.
	type builtFromKey struct{ digest, repositoryID string }
	seen := make(map[builtFromKey]struct{}, len(decisions))
	for _, decision := range decisions {
		if decision.Outcome != ContainerImageIdentityExactDigest {
			continue
		}
		digest := strings.TrimSpace(decision.Digest)
		if digest == "" {
			continue
		}
		for _, repositoryID := range uniqueSortedStrings(decision.BuildProvenanceRepositoryIDs) {
			if repositoryID == "" {
				continue
			}
			key := builtFromKey{digest: digest, repositoryID: repositoryID}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
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
// container-image-identity BUILT_FROM projection, labeled by outcome (currently
// always "materialized"; the outcome label is retained for a future skipped
// series). It is a no-op when no Instruments are wired or the count is zero.
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
