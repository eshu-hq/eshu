// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// PackageOwnershipProvenanceEvidenceSource and
// PackagePublicationProvenanceEvidenceSource tag PUBLISHES edges by the
// correlation domain that produced them (docs/internal/design/
// 5472-graph-projection-policy.md), so each domain's retract-first pass only
// ever removes its own edges. Exported because the multi-family provenance
// metrics test asserts per-source counters without going through the handler.
const (
	PackageOwnershipProvenanceEvidenceSource   = "reducer/package-ownership"
	PackagePublicationProvenanceEvidenceSource = "reducer/package-publication"
)

// PackageProvenanceEdgeWriter persists and retracts canonical PUBLISHES edges
// between a Repository and the Package or PackageVersion it owns or
// published. Implementations MUST be idempotent by (repository id, PUBLISHES,
// package/version uid, scope_id, evidence_source) so reducer retries and
// re-projected generations converge on one assertion, and MUST NOT fabricate
// an endpoint node: a row whose repository or package/version node is absent is
// a no-op.
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
//
// Rows are deduplicated by edge identity: a package-level and a
// version-scoped hint can resolve the same repository and target through
// separate decisions, but the writer MERGEs idempotently onto one edge, so
// the row set carries each identity once — the same population the live
// gate counts in the graph (#6228).
func packageOwnershipPublishesRows(decisions []PackageSourceDecision) []map[string]any {
	rows := make([]map[string]any, 0, len(decisions))
	seen := make(map[publishesRowKey]struct{}, len(decisions))
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
		key := publishesRowKey{repositoryID: repositoryID, targetID: anyToStringVersionOrPackage(row)}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		rows = append(rows, row)
	}
	return rows
}

// packagePublicationPublishesRows builds PUBLISHES edge rows from
// exact/derived package-publication correlation decisions with a resolved
// repository. Publication decisions always name a specific published version
// (BuildPackagePublicationDecisions requires a non-empty VersionID), so every
// row targets a PackageVersion.
//
// Rows are deduplicated by edge identity like the ownership rows: the
// package-level and the version-scoped hint pair with the same version
// through separate decisions, but both project onto one edge.
func packagePublicationPublishesRows(decisions []PackagePublicationDecision) []map[string]any {
	rows := make([]map[string]any, 0, len(decisions))
	seen := make(map[publishesRowKey]struct{}, len(decisions))
	for _, decision := range decisions {
		if !packageOwnerOutcomeAdmits(decision.Outcome) {
			continue
		}
		repositoryID := strings.TrimSpace(decision.RepositoryID)
		versionID := strings.TrimSpace(decision.VersionID)
		if repositoryID == "" || versionID == "" {
			continue
		}
		key := publishesRowKey{repositoryID: repositoryID, targetID: versionID}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		rows = append(rows, map[string]any{
			"repository_id": repositoryID,
			"version_id":    versionID,
		})
	}
	return rows
}

// publishesRowKey is the PUBLISHES edge identity a row writes: the source
// repository id plus the target Package uid or PackageVersion uid. It
// mirrors the writer's MERGE identity (repository id, PUBLISHES, target
// uid, scope_id, evidence_source); scope_id and evidence_source are stamped
// per writer call, not per row, so they cannot distinguish rows here.
type publishesRowKey struct {
	repositoryID string
	targetID     string
}

// anyToStringVersionOrPackage reads the row's edge target the same way the
// writer picks its Package or PackageVersion template: version_id wins when
// present, otherwise package_id.
func anyToStringVersionOrPackage(row map[string]any) string {
	if versionID, ok := row["version_id"].(string); ok && versionID != "" {
		return versionID
	}
	if packageID, ok := row["package_id"].(string); ok {
		return packageID
	}
	return ""
}

// ProjectPackageProvenanceEdgesForTest projects the current exact/derived
// ownership and publication decisions through the retract-first path with the
// handler's wired writer and instruments. It is the test seam the
// multi-family provenance metrics test drives directly, mirroring the
// containerimage/cicdrun Project...ForTest seams; production calls go through
// PackageSourceHandler.Handle.
func (h PackageSourceHandler) ProjectPackageProvenanceEdgesForTest(
	ctx context.Context,
	intent reducercontract.Intent,
	ownershipDecisions []PackageSourceDecision,
	publicationDecisions []PackagePublicationDecision,
) error {
	return h.projectPackageProvenanceEdges(ctx, intent, ownershipDecisions, publicationDecisions)
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
func (h PackageSourceHandler) projectPackageProvenanceEdges(
	ctx context.Context,
	intent reducercontract.Intent,
	ownershipDecisions []PackageSourceDecision,
	publicationDecisions []PackagePublicationDecision,
) error {
	if h.ProvenanceEdgeWriter == nil {
		return nil
	}

	if err := h.ProvenanceEdgeWriter.RetractPublishesEdges(
		ctx, intent.ScopeID, intent.GenerationID, PackageOwnershipProvenanceEvidenceSource,
	); err != nil {
		return fmt.Errorf("retract package ownership provenance edges: %w", err)
	}
	if err := h.ProvenanceEdgeWriter.RetractPublishesEdges(
		ctx, intent.ScopeID, intent.GenerationID, PackagePublicationProvenanceEvidenceSource,
	); err != nil {
		return fmt.Errorf("retract package publication provenance edges: %w", err)
	}

	ownershipRows := packageOwnershipPublishesRows(ownershipDecisions)
	if len(ownershipRows) > 0 {
		if err := h.ProvenanceEdgeWriter.WritePublishesEdges(
			ctx, ownershipRows, intent.ScopeID, intent.GenerationID, PackageOwnershipProvenanceEvidenceSource,
		); err != nil {
			return fmt.Errorf("write package ownership provenance edges: %w", err)
		}
		h.emitProvenanceEdgeCounter(ctx, PackageOwnershipProvenanceEvidenceSource, "submitted", len(ownershipRows))
	}

	publicationRows := packagePublicationPublishesRows(publicationDecisions)
	if len(publicationRows) > 0 {
		if err := h.ProvenanceEdgeWriter.WritePublishesEdges(
			ctx, publicationRows, intent.ScopeID, intent.GenerationID, PackagePublicationProvenanceEvidenceSource,
		); err != nil {
			return fmt.Errorf("write package publication provenance edges: %w", err)
		}
		h.emitProvenanceEdgeCounter(ctx, PackagePublicationProvenanceEvidenceSource, "submitted", len(publicationRows))
	}
	return nil
}

// emitProvenanceEdgeCounter records rows submitted by a successful writer call,
// labeled by the producing evidence_source domain. It is a no-op when no
// Instruments are wired or the count is zero, matching emitRepoEdgeCounter's
// shape.
func (h PackageSourceHandler) emitProvenanceEdgeCounter(ctx context.Context, evidenceSource, outcome string, count int) {
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
