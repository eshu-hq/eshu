// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// DefaultWinnersAuditRangeBudget is the default fact-row budget of one audit
// range (#11131). Measured on the QA replica, a 15,000-row range read cold in
// 20-21 s against the 25 s statement guard; 10,000 keeps about 14 s.
const DefaultWinnersAuditRangeBudget = 10000

// WinnersAuditRangeIdent is one identifier in keyed index order: the sweep
// cursor, or a range's new cursor. The fields compare in SQL only, never in
// Go, so the database collation decides the order on both sides.
type WinnersAuditRangeIdent struct {
	PackageID     string
	RepositoryID  string
	SubjectDigest string
	C0            string
	AdvisoryID    string
}

func (i WinnersAuditRangeIdent) args() []any {
	return []any{i.PackageID, i.RepositoryID, i.SubjectDigest, i.C0, i.AdvisoryID}
}

// WinnersAuditRange is what one range statement read: the bounds it covered,
// the facts it recomputed, the '|' guard, and the identifiers whose winners
// differ from the stored rows in the same key interval.
type WinnersAuditRange struct {
	// Lo is the cursor the range started after; nil opens the sweep.
	Lo *WinnersAuditRangeIdent
	// Hi is the range's last identifier, the next cursor. It is nil on the
	// last range, which is open-ended on the fact and the stored side.
	Hi *WinnersAuditRangeIdent
	// Last reports the range that reached the end of the index.
	Last bool
	// Oversize reports a range that is one identifier holding at least the
	// budget in facts; it ran alone and whole.
	Oversize bool
	// RawRows is the index rows the budgeted read took (at most the budget).
	RawRows int64
	// Facts is the fact rows the range recomputed.
	Facts int64
	// Pipe reports a range fact with '|' inside an identifier field. A '|'
	// can make two identifiers share a canonical key, so the range's diff
	// is not trusted and the sweep fails closed.
	Pipe bool
	// Budget is the row budget the read ran with.
	Budget int
	// Candidates are the differing identifiers.
	Candidates []WinnersAuditIdent
}

// ReadWinnersAuditRange runs one range statement on reader: the budget rows
// after lo (from the start when lo is nil), trimmed to whole identifiers. It
// takes no lock and writes nothing.
func ReadWinnersAuditRange(ctx context.Context, reader db.Queryer, lo *WinnersAuditRangeIdent, budget int) (WinnersAuditRange, error) {
	return readWinnersAuditRange(ctx, reader, lo, budget, selectWinnersAuditFirstRangeSQL, selectWinnersAuditNextRangeSQL)
}

// readWinnersAuditRange runs the range statement pair first/next; tests pass
// a seeded mutation of the shipped pair.
func readWinnersAuditRange(ctx context.Context, reader db.Queryer, lo *WinnersAuditRangeIdent, budget int, first, next string) (WinnersAuditRange, error) {
	var none WinnersAuditRange
	if reader == nil {
		return none, fmt.Errorf("winners audit range database is required")
	}
	if budget < 1 {
		return none, fmt.Errorf("winners audit range budget must be at least 1, got %d", budget)
	}
	query, args := first, []any{budget}
	if lo != nil {
		query = next
		args = append(args, lo.args()...)
	}
	rows, err := reader.QueryContext(ctx, query, args...)
	if err != nil {
		return none, fmt.Errorf("read winners audit range: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := WinnersAuditRange{Lo: lo, Budget: budget}
	sawBounds := false
	for rows.Next() {
		var (
			kind                            string
			n, facts                        sql.NullInt64
			isLast, oversize, pipe          sql.NullBool
			pkg, repo, digest, c0, advisory sql.NullString
		)
		if err := rows.Scan(&kind, &n, &isLast, &oversize, &pkg, &repo, &digest, &c0, &advisory, &facts, &pipe); err != nil {
			return none, fmt.Errorf("scan winners audit range: %w", err)
		}
		if kind == "b" {
			sawBounds = true
			out.RawRows, out.Facts = n.Int64, facts.Int64
			out.Last, out.Oversize, out.Pipe = isLast.Bool, oversize.Bool, pipe.Bool
			if !out.Last && pkg.Valid {
				out.Hi = &WinnersAuditRangeIdent{pkg.String, repo.String, digest.String, c0.String, advisory.String}
			}
			continue
		}
		out.Candidates = append(out.Candidates, WinnersAuditIdent{
			PackageID: pkg, RepositoryID: repo, SubjectDigest: digest, C0: c0, AdvisoryID: advisory,
		})
	}
	if err := rows.Err(); err != nil {
		return none, fmt.Errorf("read winners audit range: %w", err)
	}
	if !sawBounds {
		return none, fmt.Errorf("read winners audit range: statement returned no bounds row")
	}
	if !out.Last && out.Hi == nil {
		return none, fmt.Errorf("read winners audit range: a non-final range returned no upper bound")
	}
	return out, nil
}

// WinnersAuditNullKeys is the NULL probe's reading: which identifier fields
// some live finding fact leaves NULL.
type WinnersAuditNullKeys struct {
	PackageID     bool
	RepositoryID  bool
	SubjectDigest bool
}

// Any reports a NULL in any probed field.
func (k WinnersAuditNullKeys) Any() bool {
	return k.PackageID || k.RepositoryID || k.SubjectDigest
}

// ProbeWinnersAuditNullKeys runs the sweep's NULL probe on reader.
func ProbeWinnersAuditNullKeys(ctx context.Context, reader db.Queryer) (WinnersAuditNullKeys, error) {
	var none WinnersAuditNullKeys
	if reader == nil {
		return none, fmt.Errorf("winners audit null probe database is required")
	}
	rows, err := reader.QueryContext(ctx, probeWinnersAuditNullKeysSQL)
	if err != nil {
		return none, fmt.Errorf("probe winners audit null keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return none, fmt.Errorf("probe winners audit null keys: %w", err)
		}
		return none, fmt.Errorf("probe winners audit null keys: statement returned no row")
	}
	var out WinnersAuditNullKeys
	if err := rows.Scan(&out.PackageID, &out.RepositoryID, &out.SubjectDigest); err != nil {
		return none, fmt.Errorf("scan winners audit null keys: %w", err)
	}
	return out, rows.Err()
}
