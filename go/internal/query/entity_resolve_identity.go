// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// hydrateResolvedEntityRepoIdentity forwards to
// querycontract.HydrateResolvedEntityRepoIdentity, which this implementation
// moved to (#6060) so a handler-family subpackage can call the exact same
// logic -- including the #6408 projection-placeholder scrubber -- without
// importing this package, which it cannot do without an import cycle through
// root's compatibility aliases.
func hydrateResolvedEntityRepoIdentity(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	entities []map[string]any,
) (bool, error) {
	return querycontract.HydrateResolvedEntityRepoIdentity(ctx, graph, content, entities)
}

// clearResolvedEntityRepoProjectionPlaceholders forwards to
// querycontract.ClearResolvedEntityRepoProjectionPlaceholders; see that
// function's doc comment for the #6408 defect it works around.
func clearResolvedEntityRepoProjectionPlaceholders(entity map[string]any) {
	querycontract.ClearResolvedEntityRepoProjectionPlaceholders(entity)
}
