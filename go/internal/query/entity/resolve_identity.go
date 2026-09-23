// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/query/selector"
)

// hydrateResolvedEntityRepoIdentity forwards to
// selector.HydrateResolvedEntityRepoIdentity. The implementation moved
// to selector for #6060 (querycontract's AGENTS.md names a query-owning
// leaf, not the dependency-neutral contract package, as the home for a
// complete query; selector already owns two complete MATCH statements
// and already consumes RepositoryAccessFilter) so a handler-family
// subpackage can hydrate the same repo identity without importing root.
// This wrapper keeps root callers (handler.go, content_types.go)
// unchanged.
func hydrateResolvedEntityRepoIdentity(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	entities []map[string]any,
) (bool, error) {
	return selector.HydrateResolvedEntityRepoIdentity(ctx, graph, content, entities)
}
