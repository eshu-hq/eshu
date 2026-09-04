// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

// The #5363 ContentStore additions on the shared fakePortContentStore double
// forward from here, split out of ports_test.go to keep that file under the
// repo's 500-line package-file cap. Both read from the promoted double's
// Entities so a single fixture set drives the name-anchored fetch, the narrow
// candidate scan, and the by-ID hydration consistently.

func (f fakePortContentStore) ListRepoEntitiesByIDs(
	ctx context.Context,
	repoID string,
	entityIDs []string,
	limit int,
) ([]EntityContent, error) {
	return f.promoted().ListRepoEntitiesByIDs(ctx, repoID, entityIDs, limit)
}

func (f fakePortContentStore) ListRepoK8sSelectCandidates(
	ctx context.Context,
	repoID string,
	limit int,
) ([]K8sSelectCandidate, error) {
	return f.promoted().ListRepoK8sSelectCandidates(ctx, repoID, limit)
}
