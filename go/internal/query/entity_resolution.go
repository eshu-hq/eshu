// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file held exact graph-entity resolution. It moved to
// internal/query/querycontract with lane B2 of #6060, which the impact
// handler family reads; the wrappers below keep root callers unchanged.

// resolveExactGraphEntityCandidate resolves one entity by exact name. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func resolveExactGraphEntityCandidate(
	ctx context.Context,
	reader ContentStore,
	repoID string,
	name string,
) (*EntityContent, error) {
	return querycontract.ResolveExactGraphEntityCandidate(ctx, reader, repoID, name)
}

// resolveExactGraphEntityCandidates lists exact-name entity candidates. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func resolveExactGraphEntityCandidates(
	ctx context.Context,
	reader ContentStore,
	repoID string,
	name string,
) ([]EntityContent, error) {
	return querycontract.ResolveExactGraphEntityCandidates(ctx, reader, repoID, name)
}
