// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// This file exports the package-private production symbols the reducer
// root's own test files still reach unqualified through
// container_image_identity_compat.go's forwarders (issue #6061):
// provenance_edges_bench_test.go, provenance_edge_submission_metrics_test.go,
// defaults_cicd_test.go, and container_image_identity_ci_run_provenance_test.go
// exercise this family's BUILT_FROM/DERIVED_FROM row-building and payload
// logic directly rather than only through the handler. None of these are
// called by this package's own production code paths under their exported
// name; they wrap the real unexported logic other files in this package call
// directly.

// ContainerImageBuiltFromRows forwards to the package-private
// containerImageBuiltFromRows.
func ContainerImageBuiltFromRows(decisions []ContainerImageIdentityDecision) []map[string]any {
	return containerImageBuiltFromRows(decisions)
}

// ContainerImageDerivedFromRows forwards to the package-private
// containerImageDerivedFromRows.
func ContainerImageDerivedFromRows(decisions []ContainerImageIdentityDecision, owningRepositoryID string) []map[string]any {
	return containerImageDerivedFromRows(decisions, owningRepositoryID)
}

// ContainerImageBuiltFromProvenanceEvidenceSource forwards to the
// package-private containerImageBuiltFromProvenanceEvidenceSource.
const ContainerImageBuiltFromProvenanceEvidenceSource = containerImageBuiltFromProvenanceEvidenceSource

// ContainerImageDerivedFromProvenanceEvidenceSource forwards to the
// package-private containerImageDerivedFromProvenanceEvidenceSource.
const ContainerImageDerivedFromProvenanceEvidenceSource = containerImageDerivedFromProvenanceEvidenceSource

// ContainerImageIdentityPayload forwards to the package-private
// containerImageIdentityPayload.
func ContainerImageIdentityPayload(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
	canonicalID string,
) map[string]any {
	return containerImageIdentityPayload(write, decision, canonicalID)
}

// ProjectContainerImageBuiltFromRowsForTest exposes the package-private
// projectContainerImageBuiltFromRows to the reducer root's cross-family
// provenance-edge-counter test
// (provenance_edge_submission_metrics_test.go), which exercises this
// family's counter emission together with the still-in-root
// package-source-correlation family's own private projection method in one
// shared-instrument test.
func (h ContainerImageIdentityHandler) ProjectContainerImageBuiltFromRowsForTest(
	ctx context.Context,
	intent reducercontract.Intent,
	rows []map[string]any,
) error {
	return h.projectContainerImageBuiltFromRows(ctx, intent, rows)
}

// ProjectContainerImageDerivedFromRowsForTest exposes the package-private
// projectContainerImageDerivedFromRows for the same cross-family test reason
// as [ContainerImageIdentityHandler.ProjectContainerImageBuiltFromRowsForTest].
func (h ContainerImageIdentityHandler) ProjectContainerImageDerivedFromRowsForTest(
	ctx context.Context,
	intent reducercontract.Intent,
	rows []map[string]any,
) error {
	return h.projectContainerImageDerivedFromRows(ctx, intent, rows)
}

// ProjectContainerImageBuiltFromEdgesForTest exposes the package-private
// projectContainerImageBuiltFromEdges for the same cross-family test reason
// as [ContainerImageIdentityHandler.ProjectContainerImageBuiltFromRowsForTest].
func (h ContainerImageIdentityHandler) ProjectContainerImageBuiltFromEdgesForTest(
	ctx context.Context,
	intent reducercontract.Intent,
	decisions []ContainerImageIdentityDecision,
) error {
	return h.projectContainerImageBuiltFromEdges(ctx, intent, decisions)
}
