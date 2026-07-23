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

// packageOwnershipProvenanceEvidenceSource and
// packagePublicationProvenanceEvidenceSource tag PUBLISHES edges by the
// correlation domain that produced them (docs/internal/design/
// 5472-graph-projection-policy.md), so each domain's retract-first pass only
// ever removes its own edges.
const (
	packageOwnershipProvenanceEvidenceSource   = "reducer/package-ownership"
	packagePublicationProvenanceEvidenceSource = "reducer/package-publication"
)

// PackageProvenanceEdgeWriter persists and retracts canonical PUBLISHES edges
// between a Repository and the Package or PackageVersion it owns or
// published. Implementations MUST be idempotent by (repository id, PUBLISHES,
// package/version uid) so reducer retries and re-projected generations
// converge on one edge, and MUST NOT fabricate an endpoint node: a row whose
// repository or package/version node is absent is a no-op.
type PackageProvenanceEdgeWriter interface {
	WritePublishesEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractPublishesEdges(ctx context.Context, scopeID, generationID, evidenceSource string) error
}

// packageOwnershipPublishesRows builds PUBLISHES edge rows from exact/derived
// package-ownership correlation decisions with a resolved repository. A
// decision whose source hint named a specific package version (VersionID
// non-empty) targets that PackageVersion; otherwise it targets the Package.
// Ambiguous/unresolved/stale/rejected outcomes and decisions with no resolved
// repository never produce a row (#5472 exact/derived-only tiering).
func packageOwnershipPublishesRows(decisions []PackageSourceCorrelationDecision) []map[string]any {
	rows := make([]map[string]any, 0, len(decisions))
	for _, decision := range decisions {
		if !packageOwnerOutcomeAdmits(decision.Outcome) {
			continue
		}
		repositoryID := strings.TrimSpace(decision.RepositoryID)
		packageID := strings.TrimSpace(decision.PackageID)
		if repositoryID == "" || packageID == "" {
			continue
		}
		row := map[string]any{"repository_id": repositoryID}
		if versionID := strings.TrimSpace(decision.VersionID); versionID != "" {
			row["version_id"] = versionID
		} else {
			row["package_id"] = packageID
		}
		rows = append(rows, row)
	}
	return rows
}

// packagePublicationPublishesRows builds PUBLISHES edge rows from
// exact/derived package-publication correlation decisions with a resolved
// repository. Publication decisions always name a specific published version
// (BuildPackagePublicationDecisions requires a non-empty VersionID), so every
// row targets a PackageVersion.
func packagePublicationPublishesRows(decisions []PackagePublicationDecision) []map[string]any {
	rows := make([]map[string]any, 0, len(decisions))
	for _, decision := range decisions {
		if !packageOwnerOutcomeAdmits(decision.Outcome) {
			continue
		}
		repositoryID := strings.TrimSpace(decision.RepositoryID)
		versionID := strings.TrimSpace(decision.VersionID)
		if repositoryID == "" || versionID == "" {
			continue
		}
		rows = append(rows, map[string]any{
			"repository_id": repositoryID,
			"version_id":    versionID,
		})
	}
	return rows
}

// projectPackageProvenanceEdges retracts this generation's prior PUBLISHES
// edges and re-projects the current exact/derived ownership and publication
// decisions. It is a no-op when no ProvenanceEdgeWriter is wired, so the
// package-source-correlation profile stays Postgres-only until an adapter is
// configured. It never fails the correlation result for an empty projection;
// only a writer error propagates -- mirroring
// projectConsumptionRepoDependencyEdges (issue #3579).
//
// Retract runs unconditionally (ahead of any row check) so a generation that
// drops a previously-admitted decision still removes that decision's stale
// edge (#5472 retract-first-per-generation).
func (h PackageSourceCorrelationHandler) projectPackageProvenanceEdges(
	ctx context.Context,
	intent Intent,
	ownershipDecisions []PackageSourceCorrelationDecision,
	publicationDecisions []PackagePublicationDecision,
) error {
	if h.ProvenanceEdgeWriter == nil {
		return nil
	}

	if err := h.ProvenanceEdgeWriter.RetractPublishesEdges(
		ctx, intent.ScopeID, intent.GenerationID, packageOwnershipProvenanceEvidenceSource,
	); err != nil {
		return fmt.Errorf("retract package ownership provenance edges: %w", err)
	}
	if err := h.ProvenanceEdgeWriter.RetractPublishesEdges(
		ctx, intent.ScopeID, intent.GenerationID, packagePublicationProvenanceEvidenceSource,
	); err != nil {
		return fmt.Errorf("retract package publication provenance edges: %w", err)
	}

	ownershipRows := packageOwnershipPublishesRows(ownershipDecisions)
	h.emitProvenanceEdgeCounter(ctx, packageOwnershipProvenanceEvidenceSource, "materialized", len(ownershipRows))
	if len(ownershipRows) > 0 {
		if err := h.ProvenanceEdgeWriter.WritePublishesEdges(
			ctx, ownershipRows, intent.ScopeID, intent.GenerationID, packageOwnershipProvenanceEvidenceSource,
		); err != nil {
			return fmt.Errorf("write package ownership provenance edges: %w", err)
		}
	}

	publicationRows := packagePublicationPublishesRows(publicationDecisions)
	h.emitProvenanceEdgeCounter(ctx, packagePublicationProvenanceEvidenceSource, "materialized", len(publicationRows))
	if len(publicationRows) > 0 {
		if err := h.ProvenanceEdgeWriter.WritePublishesEdges(
			ctx, publicationRows, intent.ScopeID, intent.GenerationID, packagePublicationProvenanceEvidenceSource,
		); err != nil {
			return fmt.Errorf("write package publication provenance edges: %w", err)
		}
	}
	return nil
}

// emitProvenanceEdgeCounter records a ProvenanceEdges counter sample labeled
// by the producing evidence_source domain and outcome (currently always
// "materialized"; the outcome label is retained for a future skipped series).
// It is a no-op when no Instruments are wired or the count is zero, matching
// emitRepoEdgeCounter's shape.
func (h PackageSourceCorrelationHandler) emitProvenanceEdgeCounter(ctx context.Context, evidenceSource, outcome string, count int) {
	if h.Instruments == nil || h.Instruments.ProvenanceEdges == nil || count <= 0 {
		return
	}
	h.Instruments.ProvenanceEdges.Add(
		ctx,
		int64(count),
		metric.WithAttributes(
			telemetry.AttrDomain(evidenceSource),
			telemetry.AttrOutcome(outcome),
		),
	)
}
