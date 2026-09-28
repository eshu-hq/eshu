// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

// SearchCodeCandidates delegates the root compatibility fake to the shared
// content fake, preserving the repository-scoped filter-before-limit contract.
func (f fakePortContentStore) SearchCodeCandidates(
	ctx context.Context,
	repoID, pattern, language string,
	limit int,
	exact bool,
) (nameMatches, sourceMatches []EntityContent, err error) {
	return f.promoted().SearchCodeCandidates(ctx, repoID, pattern, language, limit, exact)
}
