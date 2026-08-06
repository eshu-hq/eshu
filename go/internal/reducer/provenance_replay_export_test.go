// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "context"

// PackageOwnershipPublishesRowsForReplayTest exposes the package-private row
// mapper only to the external replay test compiled with this package's tests.
func PackageOwnershipPublishesRowsForReplayTest(
	decisions []PackageSourceCorrelationDecision,
) []map[string]any {
	return packageOwnershipPublishesRows(decisions)
}

// ContainerImageBuiltFromRowsForReplayTest exposes the package-private row
// mapper only to the external replay test compiled with this package's tests.
func ContainerImageBuiltFromRowsForReplayTest(
	decisions []ContainerImageIdentityDecision,
) []map[string]any {
	return containerImageBuiltFromRows(decisions)
}

// ProjectPackageProvenanceEdgesForReplayTest drives the package-private
// retract-first projection through the real writer supplied by the replay test.
func ProjectPackageProvenanceEdgesForReplayTest(
	ctx context.Context,
	writer PackageProvenanceEdgeWriter,
	scopeID string,
	generationID string,
	decisions []PackageSourceCorrelationDecision,
) error {
	handler := PackageSourceCorrelationHandler{ProvenanceEdgeWriter: writer}
	return handler.projectPackageProvenanceEdges(
		ctx,
		Intent{ScopeID: scopeID, GenerationID: generationID},
		decisions,
		nil,
	)
}

// ProjectContainerImageBuiltFromEdgesForReplayTest drives the package-private
// retract-first projection through the real writer supplied by the replay test.
func ProjectContainerImageBuiltFromEdgesForReplayTest(
	ctx context.Context,
	writer ContainerImageProvenanceEdgeWriter,
	scopeID string,
	generationID string,
	decisions []ContainerImageIdentityDecision,
) error {
	handler := ContainerImageIdentityHandler{ProvenanceEdgeWriter: writer}
	return handler.projectContainerImageBuiltFromEdges(
		ctx,
		Intent{ScopeID: scopeID, GenerationID: generationID},
		decisions,
	)
}
