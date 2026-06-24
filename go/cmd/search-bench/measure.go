// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// latency summarizes a set of measured query durations.
type latency struct {
	Queries int
	Results int
	P50     time.Duration
	P95     time.Duration
	Max     time.Duration
}

// measure runs probe once per query across the given rounds, recording the
// duration and result count of each call.
func measure(queries []string, rounds int, probe func(string) (int, error)) (latency, error) {
	durations := make([]time.Duration, 0, len(queries)*rounds)
	totalResults := 0
	for round := 0; round < rounds; round++ {
		for _, query := range queries {
			start := time.Now()
			results, err := probe(query)
			elapsed := time.Since(start)
			if err != nil {
				return latency{}, err
			}
			durations = append(durations, elapsed)
			if round == 0 {
				totalResults += results
			}
		}
	}
	return latency{
		Queries: len(queries),
		Results: totalResults,
		P50:     percentile(durations, 50),
		P95:     percentile(durations, 95),
		Max:     percentile(durations, 100),
	}, nil
}

// pgKeywordSearch runs the Postgres content-search baseline for one term: the
// same source_cache ILIKE pattern the content store uses, scoped to the repo and
// capped at limit. It returns the row count.
func pgKeywordSearch(ctx context.Context, pool *pgxpool.Pool, term string, repoID string, limit int) (int, error) {
	rows, err := pool.Query(ctx, `
		SELECT entity_id FROM content_entities
		WHERE source_cache ILIKE $1 AND repo_id = $2
		LIMIT $3`, "%"+term+"%", repoID, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	return count, rows.Err()
}

// percentile returns the nearest-rank percentile of durations for p in [0,100].
func percentile(durations []time.Duration, p int) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := int(math.Ceil(float64(p) / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}
