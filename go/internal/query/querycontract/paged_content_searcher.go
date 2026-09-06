// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "context"

// PagedContentSearcher is implemented by content stores that can push offset
// and multi-repo scope into SQL instead of paginating in handler memory.
//
// It lives in querycontract (rather than beside its callers) so the
// contentread handler family (#6060 lane B1) can assert it from another
// package: Go qualifies an unexported method name by its declaring package
// for interface satisfaction, and the request type the old root-local
// interface took is likewise unexported, so a root-local interface could
// never be satisfied across the package boundary -- only silently missed,
// falling back to the slower per-request search loop. Decomposed primitive
// parameters keep every signature element exported, so root's ContentReader
// still satisfies this interface while living in a different package than
// the handlers that assert it.
//
// Exactly one of repoID / repoIDs selects the scope: a non-empty repoID
// searches one repository, a non-empty repoIDs searches that explicit set,
// and both empty searches all indexed repositories. limit carries the
// caller's probe row (request limit + 1); offset is the zero-based SQL
// offset.
type PagedContentSearcher interface {
	SearchFiles(ctx context.Context, repoID string, repoIDs []string, pattern string, limit, offset int) ([]FileContent, error)
	SearchEntities(ctx context.Context, repoID string, repoIDs []string, pattern string, limit, offset int) ([]EntityContent, error)
}
