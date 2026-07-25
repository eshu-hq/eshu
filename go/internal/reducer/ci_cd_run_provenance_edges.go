// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// cicdRunBuiltFromProvenanceEvidenceSource tags BUILT_FROM edges projected from
// ci_cd_run_correlation decisions (#5428). BUILT_FROM is a shared edge type with
// the container_image_identity domain
// (reducer/container-image-identity, #5457); the distinct evidence_source is
// what keeps each domain's retract-first pass from touching the other domain's
// edges, and it is the axis the golden gate's rc assertions isolate on --
// source_tool is "oci" for both domains, so it cannot distinguish them
// (docs/internal/design/5472-graph-projection-policy.md, and
// go/internal/storage/cypher/provenance_edge_writer.go's evidence_kinds map).
const cicdRunBuiltFromProvenanceEvidenceSource = "reducer/ci-cd-run-correlation"

// CICDRunProvenanceEdgeWriter persists and retracts canonical BUILT_FROM edges
// between the ContainerImage a CI run produced and the Repository that ran it.
// Implementations MUST be idempotent by (image digest, BUILT_FROM, repository
// id) so reducer retries and re-projected generations converge on one edge, and
// MUST NOT fabricate an endpoint node: a row whose image or repository node is
// absent is a no-op. This mirrors ContainerImageProvenanceEdgeWriter so both
// domains share one adapter (go/internal/storage/cypher).
type CICDRunProvenanceEdgeWriter interface {
	WriteBuiltFromEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractBuiltFromEdges(ctx context.Context, scopeID, generationID, evidenceSource string) error
}

// cicdRunBuiltFromRows builds BUILT_FROM edge rows from exact ci_cd_run
// correlation decisions. Exact-only promotion is the #5472 rule for this edge:
// a derived, ambiguous, unresolved, or rejected outcome has not proven which
// image the run produced, so it stays provenance-only in Postgres and never
// reaches the graph. A decision missing either endpoint (artifact digest or
// repository id) produces no row rather than an edge anchored on a fabricated
// node (#5463). Rows are deduplicated and stably ordered so a re-projected
// generation emits the same bounded write set.
func cicdRunBuiltFromRows(decisions []CICDRunCorrelationDecision) []map[string]any {
	type edge struct{ digest, repositoryID string }
	seen := make(map[edge]struct{}, len(decisions))
	unique := make([]edge, 0, len(decisions))
	for _, decision := range decisions {
		if decision.Outcome != CICDRunCorrelationExact {
			continue
		}
		digest := strings.TrimSpace(decision.ArtifactDigest)
		repositoryID := strings.TrimSpace(decision.RepositoryID)
		if digest == "" || repositoryID == "" {
			continue
		}
		key := edge{digest: digest, repositoryID: repositoryID}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, key)
	}
	sort.Slice(unique, func(i, j int) bool {
		if unique[i].digest != unique[j].digest {
			return unique[i].digest < unique[j].digest
		}
		return unique[i].repositoryID < unique[j].repositoryID
	})
	rows := make([]map[string]any, 0, len(unique))
	for _, key := range unique {
		rows = append(rows, map[string]any{
			"digest":        key.digest,
			"repository_id": key.repositoryID,
		})
	}
	return rows
}

// projectCICDRunBuiltFromEdges retracts this generation's prior BUILT_FROM
// edges owned by this evidence_source and re-projects the current exact
// decisions. It is a no-op when no ProvenanceEdgeWriter is wired, so the
// ci_cd_run_correlation profile stays Postgres-only until an adapter is
// configured. It never fails the correlation result for an empty projection;
// only a writer error propagates.
//
// Retract runs unconditionally, ahead of any row check, so a generation that
// drops a previously-exact decision still removes that decision's stale edge
// (#5472 retract-first-per-generation).
func (h CICDRunCorrelationHandler) projectCICDRunBuiltFromEdges(
	ctx context.Context,
	intent Intent,
	decisions []CICDRunCorrelationDecision,
) error {
	if h.ProvenanceEdgeWriter == nil {
		return nil
	}
	if err := h.ProvenanceEdgeWriter.RetractBuiltFromEdges(
		ctx, intent.ScopeID, intent.GenerationID, cicdRunBuiltFromProvenanceEvidenceSource,
	); err != nil {
		return err
	}
	rows := cicdRunBuiltFromRows(decisions)
	if len(rows) == 0 {
		return nil
	}
	if err := h.ProvenanceEdgeWriter.WriteBuiltFromEdges(
		ctx, rows, intent.ScopeID, intent.GenerationID, cicdRunBuiltFromProvenanceEvidenceSource,
	); err != nil {
		return err
	}
	h.emitProvenanceEdgeCounter(ctx, "materialized", len(rows))
	return nil
}

// emitProvenanceEdgeCounter records a ProvenanceEdges counter sample for the
// edges this domain materialized, labeled with this domain's evidence_source so
// an operator can tell #5428's BUILT_FROM writes apart from #5457's on the
// shared edge type -- the same axis evidence_kinds provides in the graph. A nil
// Instruments or a zero count records nothing rather than a misleading zero
// sample, matching the sibling projections.
func (h CICDRunCorrelationHandler) emitProvenanceEdgeCounter(ctx context.Context, outcome string, count int) {
	if h.Instruments == nil || h.Instruments.ProvenanceEdges == nil || count <= 0 {
		return
	}
	h.Instruments.ProvenanceEdges.Add(
		ctx,
		int64(count),
		metric.WithAttributes(
			telemetry.AttrDomain(cicdRunBuiltFromProvenanceEvidenceSource),
			telemetry.AttrOutcome(outcome),
		),
	)
}
