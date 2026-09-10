// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"context"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// This file exports the package-private production symbols the reducer
// root's own test files still reach directly (issue #6061):
// provenance_edges_bench_test.go and provenance_replay_tombstone_live_test.go
// exercise this family's PUBLISHES row-building logic directly rather than
// only through the handler. None of these are called by this package's own
// production code paths under their exported name; they wrap the real
// unexported logic other files in this package call directly.

// PackageOwnershipPublishesRows forwards to the package-private
// packageOwnershipPublishesRows.
func PackageOwnershipPublishesRows(
	decisions []PackageSourceDecision,
) []map[string]any {
	return packageOwnershipPublishesRows(decisions)
}

// PackagePublicationPublishesRows forwards to the package-private
// packagePublicationPublishesRows.
func PackagePublicationPublishesRows(
	decisions []PackagePublicationDecision,
) []map[string]any {
	return packagePublicationPublishesRows(decisions)
}

// ProjectPackageProvenanceEdgesForReplayTest drives the package-private
// retract-first projection through the real writer supplied by the replay test.
func ProjectPackageProvenanceEdgesForReplayTest(
	ctx context.Context,
	writer PackageProvenanceEdgeWriter,
	scopeID string,
	generationID string,
	ownershipDecisions []PackageSourceDecision,
	publicationDecisions []PackagePublicationDecision,
) error {
	handler := PackageSourceHandler{ProvenanceEdgeWriter: writer}
	return handler.projectPackageProvenanceEdges(
		ctx,
		reducercontract.Intent{ScopeID: scopeID, GenerationID: generationID},
		ownershipDecisions,
		publicationDecisions,
	)
}
