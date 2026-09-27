// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// CorpusFencedResolvedRelationshipLoader returns the active resolved
// relationships touching one or more repositories together with the
// corpus-completeness fence verdict, both evaluated in ONE statement snapshot
// (#6740). complete is false while any active scope's current relationship
// generation is retired-or-pending; the rows are then empty and must not be
// consumed, because the by-repos read silently omits that scope's rows and a
// service generation built from them would supersede the prior one with
// spurious removed deployment and dependency evidence (#7258).
//
// The service catalog handler requires this fenced read rather than the plain
// by-repos read so production cannot wire an unfenced loader. It is declared
// locally rather than imported from the reducer root, whose own
// CorpusFencedResolvedRelationshipLoader is structurally identical: a family
// subpackage never imports the reducer root (issue #6061), and Go interfaces
// are satisfied structurally, so *postgres.RelationshipStore satisfies both
// (TestRelationshipStoreSatisfiesCorpusFencedResolvedRelationshipLoader in
// go/internal/storage/postgres asserts it).
type CorpusFencedResolvedRelationshipLoader interface {
	GetResolvedRelationshipsForReposWithCorpusFence(
		ctx context.Context,
		repoIDs []string,
	) (rows []relationships.ResolvedRelationship, complete bool, err error)
}
