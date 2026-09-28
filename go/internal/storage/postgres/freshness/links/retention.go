// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// PrunedGenerationRowCounts reports, per pruned generation and ledger table,
// the rows DeletePrunedGenerationRows would delete for the generations, so
// generation retention checks BatchRowLimit against the ledger too (#7127
// ruling 2.8). scopeIDs and generationIDs are parallel, one entry per
// generation. A link that names two of the generations is charged to the one
// later in generationIDs; the retention store passes its candidates oldest
// first, so that is the newer one. Every generation gets a row for every
// table, zero included.
//
// The counts are a pre-count in the retention transaction q: a link that
// commits between them and the delete is not in them. RowsPruned must come
// from DeletePrunedGenerationRows.
func PrunedGenerationRowCounts(ctx context.Context, q db.Queryer, scopeIDs, generationIDs []string) (map[string]map[string]int64, error) {
	counts := make(map[string]map[string]int64, len(generationIDs))
	if len(generationIDs) == 0 {
		return counts, nil
	}
	if len(scopeIDs) != len(generationIDs) {
		return nil, fmt.Errorf("changed-since ledger retention: count rows: %d scope ids for %d generations", len(scopeIDs), len(generationIDs))
	}
	rows, err := q.QueryContext(ctx, retentionRowCountsQuery, scopeIDs, generationIDs)
	if err != nil {
		return nil, fmt.Errorf("changed-since ledger retention: count rows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var generationID, table string
		var n int64
		if err := rows.Scan(&generationID, &table, &n); err != nil {
			return nil, fmt.Errorf("changed-since ledger retention: scan row count: %w", err)
		}
		if counts[generationID] == nil {
			counts[generationID] = make(map[string]int64, 4)
		}
		counts[generationID][table] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("changed-since ledger retention: count rows: %w", err)
	}
	return counts, nil
}

// DeletePrunedGenerationRows deletes, inside the retention transaction q and
// in one statement, every ledger row #7127 ruling 2.8 retires with the
// generations: the links whose generation or prior generation is one of
// them, with their link deltas and bucket counts, and the activation rows of
// the generations. scopeIDs and generationIDs are parallel, one entry per
// generation. It returns the rows deleted per ledger table, which is what
// RowsPruned reports. The state table and the cursor are left alone: they
// describe the active generation, which retention never prunes. It does not
// depend on the link domain's switch: rows written while it was on are
// pruned after it is turned off.
//
// It fails when the rows the statement deleted by ctid differ from the rows
// its own snapshot selected, which only a concurrent writer of the same
// ledger rows could cause; the caller rolls the batch back. The error names
// the batch's size and generation ids beside the counts.
func DeletePrunedGenerationRows(ctx context.Context, q db.Queryer, scopeIDs, generationIDs []string) (map[string]int64, error) {
	deleted := map[string]int64{TableLinks: 0, TableLinkDeltas: 0, TableLinkBucketCounts: 0, TableActivations: 0}
	if len(generationIDs) == 0 {
		return deleted, nil
	}
	if len(scopeIDs) != len(generationIDs) {
		return nil, fmt.Errorf("changed-since ledger retention: prune: %d scope ids for %d generations", len(scopeIDs), len(generationIDs))
	}
	var links, deltas, buckets, activations, wantLinks, wantDeltas, wantBuckets int64
	if err := queryOne(ctx, q, retentionPruneQuery, []any{scopeIDs, generationIDs},
		&links, &deltas, &buckets, &activations, &wantLinks, &wantDeltas, &wantBuckets); err != nil {
		return nil, fmt.Errorf("changed-since ledger retention: prune: %w", err)
	}
	if links != wantLinks || deltas != wantDeltas || buckets != wantBuckets {
		return nil, fmt.Errorf("changed-since ledger retention: prune %d generations %v: deleted %d of %d links, %d of %d deltas, %d of %d bucket counts",
			len(generationIDs), generationIDs, links, wantLinks, deltas, wantDeltas, buckets, wantBuckets)
	}
	deleted[TableLinks] = links
	deleted[TableLinkDeltas] = deltas
	deleted[TableLinkBucketCounts] = buckets
	deleted[TableActivations] = activations
	return deleted, nil
}
