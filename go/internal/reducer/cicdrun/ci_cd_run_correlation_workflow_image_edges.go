// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// CICDWorkflowImageBuiltFromEvidenceSource owns the narrow BUILT_FROM
// assertion made from an exact GitHub Actions workflow-image correlation. It
// is intentionally distinct from container_image_identity's assertion so the
// two evidence domains can independently support the same endpoint pair.
//
// Exported (rather than package-private) because the reducer root's shared
// provenance-edge-counter test exercises it directly (issue #6061); the root
// keeps a forwarding alias so that call site's spelling is unchanged.
const CICDWorkflowImageBuiltFromEvidenceSource = "reducer/ci-cd-run-correlation/workflow-image"

// cicdWorkflowImageBuiltFromRows returns one BUILT_FROM row for each exact
// workflow-image decision that resolved a produced image to its run
// repository. Artifact-only exact matches and all non-exact outcomes are
// excluded: they do not prove that the workflow declared this image.
func cicdWorkflowImageBuiltFromRows(decisions []CICDRunCorrelationDecision) []map[string]any {
	type builtFromKey struct{ digest, repositoryID string }
	rows := make([]map[string]any, 0, len(decisions))
	seen := make(map[builtFromKey]struct{}, len(decisions))
	for _, decision := range decisions {
		if strings.TrimSpace(decision.Provider) != cicdWorkflowImageProvider ||
			decision.Outcome != CICDRunCorrelationExact ||
			decision.CorrelationKind != "workflow_image" ||
			decision.CanonicalTarget != "container_image" {
			continue
		}
		digest := strings.TrimSpace(decision.ArtifactDigest)
		repositoryID := strings.TrimSpace(decision.RepositoryID)
		if digest == "" || repositoryID == "" {
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
	return rows
}

// ProjectCICDWorkflowImageBuiltFromEdges retracts this evidence source's
// prior generation and then writes the current exact workflow-image rows.
//
// Exported (rather than package-private) because the reducer root's shared
// provenance-edge-counter test exercises it directly (issue #6061).
func (h CICDRunCorrelationHandler) ProjectCICDWorkflowImageBuiltFromEdges(
	ctx context.Context,
	intent reducercontract.Intent,
	decisions []CICDRunCorrelationDecision,
) error {
	if h.ProvenanceEdgeWriter == nil {
		return nil
	}
	if err := h.ProvenanceEdgeWriter.RetractBuiltFromEdges(
		ctx,
		intent.ScopeID,
		intent.GenerationID,
		CICDWorkflowImageBuiltFromEvidenceSource,
	); err != nil {
		return fmt.Errorf("retract ci/cd workflow-image built_from provenance edges: %w", err)
	}

	rows := cicdWorkflowImageBuiltFromRows(decisions)
	if len(rows) == 0 {
		return nil
	}
	if err := h.ProvenanceEdgeWriter.WriteBuiltFromEdges(
		ctx,
		rows,
		intent.ScopeID,
		intent.GenerationID,
		CICDWorkflowImageBuiltFromEvidenceSource,
	); err != nil {
		return fmt.Errorf("write ci/cd workflow-image built_from provenance edges: %w", err)
	}
	h.emitWorkflowImageProvenanceEdgeCounter(ctx, len(rows))
	return nil
}

func (h CICDRunCorrelationHandler) emitWorkflowImageProvenanceEdgeCounter(ctx context.Context, count int) {
	if h.Instruments == nil || h.Instruments.ProvenanceEdges == nil || count <= 0 {
		return
	}
	h.Instruments.ProvenanceEdges.Add(
		ctx,
		int64(count),
		metric.WithAttributes(
			telemetry.AttrDomain(CICDWorkflowImageBuiltFromEvidenceSource),
			telemetry.AttrOutcome("submitted"),
		),
	)
}
