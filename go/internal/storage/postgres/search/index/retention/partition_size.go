// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"context"
	"fmt"
	"regexp"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// parityLeafPattern is the closed allowlist for TakePartitionParitySnapshot
// leaves: a terms leaf short name or its schema-qualified regclass text (the
// census emits ::regclass::text, which PostgreSQL schema-qualifies as the
// search_path needs). Anything else is rejected before its text reaches SQL.
var parityLeafPattern = regexp.MustCompile(`^(public\.)?eshu_search_index_terms_p[0-9]{2}$`)

// Partition size-vs-rowcount suspect thresholds (#10874, arbiter Decision 3:
// initial values, calibrated against the all-64-leaf distribution measured
// on the QA copy before locking constants). A leaf trips a verdict only on
// the AND of absolute size and bytes/row, so a tiny sparse leaf and a huge
// dense leaf both stay quiet.
const (
	// PartitionSizeWarnTotalBytes is the WARN absolute-size half: 4 GiB.
	PartitionSizeWarnTotalBytes = int64(4) << 30
	// PartitionSizeWarnBytesPerRow is the WARN ratio half: 8 KiB/row.
	PartitionSizeWarnBytesPerRow = float64((8) << 10)
	// PartitionSizePageTotalBytes is the PAGE absolute-size half: 16 GiB.
	PartitionSizePageTotalBytes = int64(16) << 30
	// PartitionSizePageBytesPerRow is the PAGE ratio half: 32 KiB/row.
	PartitionSizePageBytesPerRow = float64((32) << 10)
)

// PartitionSizeVerdict is one leaf's size-vs-rowcount verdict.
type PartitionSizeVerdict int

const (
	// PartitionSizeOK clears the leaf: small, dense, or rowless.
	PartitionSizeOK PartitionSizeVerdict = iota
	// PartitionSizeWarn marks a suspect leaf for the structured log.
	PartitionSizeWarn
	// PartitionSizePage marks an operator-action leaf: today's p29 and p07
	// breach it, and it points at the Decision-1 rebuild playbook.
	PartitionSizePage
)

// PartitionSize is one terms leaf's catalog-only size row.
type PartitionSize struct {
	Leaf string
	// TotalBytes is pg_total_relation_size: heap, indexes, toast, free space.
	TotalBytes int64
	// HeapBytes is pg_relation_size: the heap fork alone.
	HeapBytes int64
	// IndexBytes is total minus heap minus toast: on these leaves the
	// primary key is the only index, so this is the bloat carrier (85-95%
	// of p29/p07 live).
	IndexBytes int64
	// EstimatedRows prefers n_live_tup, else reltuples: a suspect-detector,
	// never a byte-exact claim.
	EstimatedRows int64
}

// BytesPerRow divides total bytes by estimated rows; rows <= 0 yields 0 so
// an empty leaf never alarms.
func (p PartitionSize) BytesPerRow() float64 {
	if p.EstimatedRows <= 0 || p.TotalBytes <= 0 {
		return 0
	}
	return float64(p.TotalBytes) / float64(p.EstimatedRows)
}

// ClassifyPartitionSize applies the WARN/PAGE thresholds (#10874).
func ClassifyPartitionSize(p PartitionSize) PartitionSizeVerdict {
	ratio := p.BytesPerRow()
	switch {
	case p.TotalBytes >= PartitionSizePageTotalBytes && ratio >= PartitionSizePageBytesPerRow:
		return PartitionSizePage
	case p.TotalBytes >= PartitionSizeWarnTotalBytes && ratio >= PartitionSizeWarnBytesPerRow:
		return PartitionSizeWarn
	default:
		return PartitionSizeOK
	}
}

// TakePartitionSizeCensus reads one catalog-only size pass over the terms
// leaves, in leaf-name order. It scans no heap and holds no row lock; the
// runner calls it after the prune transaction on the orphan-sweep cadence.
func TakePartitionSizeCensus(ctx context.Context, q db.Queryer) ([]PartitionSize, error) {
	rows, err := q.QueryContext(ctx, partitionSizeCensusQuery)
	if err != nil {
		return nil, fmt.Errorf("search index partition size census: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var census []PartitionSize
	for rows.Next() {
		var leaf PartitionSize
		if err := rows.Scan(&leaf.Leaf, &leaf.TotalBytes, &leaf.HeapBytes, &leaf.IndexBytes, &leaf.EstimatedRows); err != nil {
			return nil, fmt.Errorf("search index partition size census: scan: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("search index partition size census: %w", err)
		}
		census = append(census, leaf)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search index partition size census: %w", err)
	}
	return census, nil
}

// PartitionParity is one leaf's rebuild-equivalence snapshot: the exact
// live row count and a checksum over the full primary key.
type PartitionParity struct {
	Leaf     string
	Rows     int64
	Checksum int64
}

// TakePartitionParitySnapshot reads one leaf's exact count and PK checksum.
// Run it before and after a rebuild-equivalent operation on the copy;
// ComparePartitionParity is the search-parity proof.
func TakePartitionParitySnapshot(ctx context.Context, q db.Queryer, leaf string) (PartitionParity, error) {
	if !parityLeafPattern.MatchString(leaf) {
		return PartitionParity{}, fmt.Errorf("search index partition parity snapshot: unexpected leaf %q", leaf)
	}
	snapshot := PartitionParity{Leaf: leaf}
	rows, err := q.QueryContext(ctx, partitionParitySnapshotQuery(leaf))
	if err != nil {
		return PartitionParity{}, fmt.Errorf("search index partition parity snapshot of %s: %w", leaf, err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return PartitionParity{}, fmt.Errorf("search index partition parity snapshot of %s: no row", leaf)
	}
	if err := rows.Scan(&snapshot.Rows, &snapshot.Checksum); err != nil {
		return PartitionParity{}, fmt.Errorf("search index partition parity snapshot of %s: scan: %w", leaf, err)
	}
	if err := rows.Err(); err != nil {
		return PartitionParity{}, fmt.Errorf("search index partition parity snapshot of %s: %w", leaf, err)
	}
	return snapshot, nil
}

// ComparePartitionParity reports whether two snapshots of the same leaf
// describe identical live content.
func ComparePartitionParity(before, after PartitionParity) error {
	if before.Leaf != after.Leaf {
		return fmt.Errorf("search index partition parity: leaf %q vs %q", before.Leaf, after.Leaf)
	}
	if before.Rows != after.Rows || before.Checksum != after.Checksum {
		return fmt.Errorf("search index partition parity mismatch on %s: rows %d vs %d, checksum %d vs %d",
			before.Leaf, before.Rows, after.Rows, before.Checksum, after.Checksum)
	}
	return nil
}
