// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// RepositoryScopedResolvedRelationshipLoader returns active resolved
// relationships touching one or more repositories, regardless of which
// repository generation produced the relationship evidence.
//
// Declared locally rather than imported from the reducer root: the root's own
// RepositoryScopedResolvedRelationshipLoader
// (workload_materialization_handler.go) is genuine root-owned logic shared by
// several families that have not moved out of root yet (issue #6061), so
// importing it would violate the rule that a family subpackage never imports
// the reducer root. Go interfaces are satisfied structurally, so the same
// concrete implementation root wires into other families' loaders also
// satisfies this local declaration without any code duplication. codetaint's
// GraphQueryRunner and CodeValueFlowBackfillStateMarker resolve the same
// problem the same way.
type RepositoryScopedResolvedRelationshipLoader interface {
	GetResolvedRelationshipsForRepos(
		ctx context.Context,
		repoIDs []string,
	) ([]relationships.ResolvedRelationship, error)
}
