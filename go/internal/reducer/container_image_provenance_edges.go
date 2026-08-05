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
// projected from container_image_identity decisions. The #5428
// ci_cd_run_correlation writer was rescinded before shipping, but independent
// domains can now safely assert the same endpoint pair because canonical
// relationship identity includes scope_id and evidence_source (#5827). Each
// domain must use its own stable evidence source and retract only that identity
// (docs/internal/design/5472-graph-projection-policy.md).
const containerImageBuiltFromProvenanceEvidenceSource = "reducer/container-image-identity"

// ContainerImageProvenanceEdgeWriter persists and retracts canonical
// BUILT_FROM edges between a ContainerImage and the Repository its identity
// decision resolved as build source. Implementations MUST be idempotent by
// (image digest, BUILT_FROM, repository id, scope_id, evidence_source) so
// reducer retries and re-projected generations converge on one assertion, and
// MUST NOT fabricate an endpoint node: a row whose image or repository node is
// absent is a no-op.
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
// counter fix, not a correctness one: it keeps the write batch and the
// submitted-row counter proportional to distinct (digest, repository) pairs.
// A submitted row whose endpoint node is absent remains a writer no-op, so the
// counter does not claim that a durable edge exists (#5828).
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

func containerImageBuiltFromSupportRows(
	supports []containerImageIdentitySupport,
) []map[string]any {
	type builtFromKey struct{ digest, repositoryID string }
	rows := make([]map[string]any, 0, len(supports))
	seen := make(map[builtFromKey]struct{}, len(supports))
	for _, support := range supports {
		digest := exactContainerImageSupportDigest(support)
		if digest == "" {
			continue
		}
		for _, repositoryID := range support.BuildProvenanceRepositoryIDs {
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
	return h.projectContainerImageBuiltFromRows(ctx, intent, containerImageBuiltFromRows(decisions))
}

func (h ContainerImageIdentityHandler) projectContainerImageBuiltFromSupportEdges(
	ctx context.Context,
	intent Intent,
	supports []containerImageIdentitySupport,
) error {
	if h.ProvenanceEdgeWriter == nil {
		return nil
	}
	return h.projectContainerImageBuiltFromRows(ctx, intent, containerImageBuiltFromSupportRows(supports))
}

func (h ContainerImageIdentityHandler) projectContainerImageBuiltFromRows(
	ctx context.Context,
	intent Intent,
	rows []map[string]any,
) error {
	if err := h.ProvenanceEdgeWriter.RetractBuiltFromEdges(
		ctx, intent.ScopeID, intent.GenerationID, containerImageBuiltFromProvenanceEvidenceSource,
	); err != nil {
		return fmt.Errorf("retract container image built_from provenance edges: %w", err)
	}

	if len(rows) == 0 {
		return nil
	}
	if err := h.ProvenanceEdgeWriter.WriteBuiltFromEdges(
		ctx, rows, intent.ScopeID, intent.GenerationID, containerImageBuiltFromProvenanceEvidenceSource,
	); err != nil {
		return fmt.Errorf("write container image built_from provenance edges: %w", err)
	}
	h.emitProvenanceEdgeCounter(ctx, "submitted", len(rows))
	return nil
}

// emitProvenanceEdgeCounter records BUILT_FROM rows submitted by a successful
// writer call. It is a no-op when no Instruments are wired or the count is
// zero.
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
