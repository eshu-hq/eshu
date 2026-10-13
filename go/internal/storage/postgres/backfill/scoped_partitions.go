// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// This file enumerates the (scope_id, generation_id) partitions the
// deferred-backfill fact load fans out over: the partition type, its listing
// query, and its loader. The per-partition load itself lives in scoped_load.go.
// ScopeGenerationPartition identifies one (scope_id, generation_id) the deferred
// relationship backfill's fact load fans out over (issue #3710). Each partition is
// one scope's latest generation; the pair is the partition key, so two scopes that
// would collapse to the same derived repo_id stay distinct.
type ScopeGenerationPartition struct {
	ScopeID      string
	GenerationID string
}

// LoadActiveScopeGenerationPartitions returns the (scope_id, generation_id) pairs
// for every scope's latest generation, sorted by (scope_id, generation_id) for a
// deterministic fan-out order. It is the partition source for the deferred
// relationship backfill's per-scope fact load (issue #3710): unlike
// LoadActiveRepositoryGenerations it does not filter to fact_kind = 'repository',
// so cloud scopes that carry gcp_cloud_relationship facts but no repository fact
// are still partitioned, and it keys on the (scope_id, generation_id) pair so no
// two scopes collapse. The set is exactly the latest_generations the deferred
// query joins against, so every content/file/gcp_cloud_relationship fact the prior
// single corpus scan covered is reachable through some partition.
//
// The partition set is a start-of-pass snapshot (a single read, not a serializable
// transaction). A generation that activates after the snapshot but before the
// per-scope queries run is picked up on the next deferred pass, since readiness is
// republished each pass; this matches the visibility window of the prior single
// corpus scan and is not a regression.
// ActiveScopeGenerationPartitionsQuery lists every (scope_id, generation_id)
// the shared LatestGenerationCTE resolves as the latest generation for its
// scope. The deferred relationship backfill's per-scope fact load (issue
// #3710) partitions on this set rather than on
// ActiveRepositoryGenerationsQuery: the latter filters fact_kind =
// 'repository', so it covers only git scopes and silently omits cloud scopes
// (for example gcp:project:...:relationship:global) that carry
// gcp_cloud_relationship facts but no repository fact. Partitioning on those
// repository rows would drop the gcp_cloud_relationship arm of the deferred
// query entirely. This query instead covers EVERY scope with a latest
// generation — the exact set the deferred query already joins to through
// latest_generations — so no content/file/gcp_cloud_relationship fact's scope
// is missing from the partition map. Keying on the (scope_id, generation_id)
// PAIR also avoids the repo_id collapse: ActiveRepositoryGenerationsQuery keys
// DISTINCT ON (repo_id) over a COALESCE(repo_id, graph_id, name) value, so two
// distinct scopes whose COALESCE collides would share one map entry and one
// scope's facts would never load.
const ActiveScopeGenerationPartitionsQuery = LatestGenerationCTE + `
SELECT scope_id, generation_id
FROM latest_generations
WHERE generation_id IS NOT NULL
ORDER BY scope_id, generation_id
`

func LoadActiveScopeGenerationPartitions(
	ctx context.Context,
	queryer db.Queryer,
) ([]ScopeGenerationPartition, error) {
	if queryer == nil {
		return nil, nil
	}

	rows, err := queryer.QueryContext(ctx, ActiveScopeGenerationPartitionsQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var partitions []ScopeGenerationPartition
	for rows.Next() {
		var partition ScopeGenerationPartition
		if err := rows.Scan(&partition.ScopeID, &partition.GenerationID); err != nil {
			return nil, err
		}
		if strings.TrimSpace(partition.ScopeID) == "" || strings.TrimSpace(partition.GenerationID) == "" {
			continue
		}
		partitions = append(partitions, partition)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return partitions, nil
}
