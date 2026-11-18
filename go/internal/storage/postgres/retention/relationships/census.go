// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Census is one orphan census of the relationship tables: the distinct
// generation ids whose rows no scope_generations row, active generation row or
// scope active pointer keeps alive (Rule A).
type Census struct {
	// Generations is the orphan generations the census listed: the union of the
	// three tables' lists, de-duplicated and sorted, at most CensusListLimit.
	// The sweep walks it in this order.
	Generations []string
	// ByTable is the distinct orphan generations found per table, each count
	// capped at CensusListLimit. All three tables always have an entry, so a
	// clean census reads 0.
	ByTable map[string]int64
	// Saturated reports that the census found more orphan generations than it
	// lists: a table's count reached CensusListLimit, or the union was cut to
	// it. ByTable counts then read "at least". A later pass lists the rest.
	Saturated bool
	// Duration is the census wall time.
	Duration time.Duration
}

// censusTable binds one census statement to the table label it reports.
type censusTable struct {
	table string
	query string
}

var censusTables = []censusTable{
	{EvidenceTable, censusEvidenceQuery},
	{CandidatesTable, censusCandidatesQuery},
	{GenerationsTable, censusGenerationsQuery},
}

// TakeCensus lists the orphan generations of the relationship tables, one
// read-only statement per table, in one transaction under a local
// statement_timeout so a slow scan ends instead of holding a connection. It
// writes nothing and takes no lock beyond the reads, so it is safe to run with
// deletes disabled. A failed or timed-out statement returns an error and no
// partial census, so the caller never reports a zero it did not measure.
func TakeCensus(ctx context.Context, database db.Beginner) (Census, error) {
	started := time.Now()
	census := Census{ByTable: map[string]int64{EvidenceTable: 0, CandidatesTable: 0, GenerationsTable: 0}}
	tx, err := database.Begin(ctx)
	if err != nil {
		return Census{}, fmt.Errorf("relationship retention census: begin: %w", err)
	}
	// The census only reads; ending the transaction rolls it back.
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, setStatementTimeoutQuery, censusStatementTimeoutSetting); err != nil {
		return Census{}, fmt.Errorf("relationship retention census: set statement_timeout: %w", err)
	}
	union := make(map[string]struct{})
	for _, source := range censusTables {
		if err := ctx.Err(); err != nil {
			return Census{}, fmt.Errorf("relationship retention census: %w", err)
		}
		listed, err := listOrphans(ctx, tx, source)
		if err != nil {
			return Census{}, err
		}
		if len(listed) > CensusListLimit {
			census.Saturated = true
			listed = listed[:CensusListLimit]
		}
		census.ByTable[source.table] = int64(len(listed))
		for _, generationID := range listed {
			union[generationID] = struct{}{}
		}
	}
	census.Generations = make([]string, 0, len(union))
	for generationID := range union {
		census.Generations = append(census.Generations, generationID)
	}
	sort.Strings(census.Generations)
	if len(census.Generations) > CensusListLimit {
		// Each table's list is its lowest ids in the database's collation order
		// (the census statements ORDER BY the id), while sort.Strings above orders
		// bytewise. The two orders agree for ASCII ids under the C collation and
		// can differ otherwise, so the CensusListLimit ids kept here are not
		// guaranteed to be the lowest orphans overall. That only changes which
		// orphans a pass lists: the ones left out are listed by later passes
		// (Saturated stays set) and the gauge reads "at least". It never changes
		// eligibility: every batch re-checks Rule A for each id it is given, so a
		// generation is never swept because of where it sorts.
		census.Generations = census.Generations[:CensusListLimit]
		census.Saturated = true
	}
	census.Duration = time.Since(started)
	return census, nil
}

// listOrphans runs one table's census statement. It asks for one row past the
// list limit so the caller can tell a full list from a truncated one.
func listOrphans(ctx context.Context, q db.Queryer, source censusTable) ([]string, error) {
	rows, err := q.QueryContext(ctx, source.query, CensusListLimit+1)
	if err != nil {
		return nil, fmt.Errorf("relationship retention census: %s: %w", source.table, err)
	}
	defer func() { _ = rows.Close() }()
	var listed []string
	for rows.Next() {
		var generationID string
		if err := rows.Scan(&generationID); err != nil {
			return nil, fmt.Errorf("relationship retention census: %s: scan: %w", source.table, err)
		}
		listed = append(listed, generationID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("relationship retention census: %s: %w", source.table, err)
	}
	return listed, nil
}
