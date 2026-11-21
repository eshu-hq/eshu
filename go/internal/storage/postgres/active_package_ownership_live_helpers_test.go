// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func ownershipIndexDefinitions(t *testing.T) []Definition {
	t.Helper()
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == "fact_records_package_ownership_active_idx" {
			return []Definition{definition}
		}
	}
	t.Fatal("fact_records_package_ownership_active_idx definition not found")
	return nil
}

// ownershipTupleKey renders the row identity the differential compares.
func ownershipTupleKey(observedAt time.Time, factID, factKind, scopeID, generationID string) string {
	return observedAt.UTC().Format(time.RFC3339Nano) + "|" + factID + "|" + factKind + "|" + scopeID + "|" + generationID
}

// drainLegacyOwnership drains the frozen legacy statement with the same
// short-page-stop loop production used before the split.
func drainLegacyOwnership(t *testing.T, ctx context.Context, db *sql.DB) []string {
	t.Helper()
	var (
		rows     []string
		cursor   *string
		cursorTS string
	)
	for {
		var rs *sql.Rows
		var err error
		if cursor == nil {
			rs, err = db.QueryContext(ctx, legacyActivePackageOwnershipFactsQuery, nil, "", listFactsByKindPageSize)
		} else {
			rs, err = db.QueryContext(ctx, legacyActivePackageOwnershipFactsQuery, cursorTS, *cursor, listFactsByKindPageSize)
		}
		if err != nil {
			t.Fatalf("legacy drain query: %v", err)
		}
		var page int
		var lastTS time.Time
		var lastID string
		for rs.Next() {
			var (
				factID, scopeID, generationID, factKind, stableKey, schemaVer string
				collector, confidence, system, factKey, uri, recordID         string
				fencing                                                       int64
				observedAt                                                    time.Time
				tomb                                                          bool
				payload                                                       []byte
			)
			if err := rs.Scan(&factID, &scopeID, &generationID, &factKind, &stableKey,
				&schemaVer, &collector, &fencing, &confidence, &system, &factKey,
				&uri, &recordID, &observedAt, &tomb, &payload); err != nil {
				_ = rs.Close()
				t.Fatalf("legacy drain scan: %v", err)
			}
			rows = append(rows, ownershipTupleKey(observedAt, factID, factKind, scopeID, generationID))
			lastTS, lastID = observedAt, factID
			page++
		}
		if err := rs.Err(); err != nil {
			_ = rs.Close()
			t.Fatalf("legacy drain rows: %v", err)
		}
		_ = rs.Close()
		if page < listFactsByKindPageSize {
			break
		}
		cursorTS = lastTS.UTC().Format(time.RFC3339Nano)
		cursor = &lastID
	}
	return rows
}

// ownershipPageCursors returns the (observed_at, fact_id) cursors that open
// pages 2..N of the drained set.
func ownershipPageCursors(loaded []facts.Envelope) [][2]string {
	var cursors [][2]string
	for i := listFactsByKindPageSize - 1; i+1 < len(loaded); i += listFactsByKindPageSize {
		cursors = append(cursors, [2]string{
			loaded[i].ObservedAt.UTC().Format(time.RFC3339Nano),
			loaded[i].FactID,
		})
	}
	return cursors
}

// assertOwnershipPlanSeeks fails unless the named shipped page plans as a
// seek over the partial index under a forced generic plan, with the row
// comparison as an Index Cond on cursor pages: the plan shape the #6154
// regression destroyed was a healthy-looking index scan with the comparison
// left in Filter, so checking the index name alone is not enough.
func assertOwnershipPlanSeeks(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	indexName string,
	which string,
	cursorTS string,
	cursorID string,
) {
	t.Helper()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection for %s plan: %v", which, err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("set force_generic_plan for %s plan: %v", which, err)
	}
	var prepare, execute string
	if which == "first" {
		prepare = "ownership_first_probe AS " + listActivePackageOwnershipFactsFirstQuery
		execute = fmt.Sprintf("EXECUTE ownership_first_probe(%d)", listFactsByKindPageSize)
	} else {
		prepare = "ownership_cursor_probe AS " + listActivePackageOwnershipFactsCursorQuery
		execute = fmt.Sprintf("EXECUTE ownership_cursor_probe('%s'::timestamptz, '%s', %d)",
			cursorTS, cursorID, listFactsByKindPageSize)
	}
	if _, err := conn.ExecContext(ctx, "PREPARE "+prepare); err != nil {
		t.Fatalf("prepare %s probe: %v", which, err)
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, "DEALLOCATE ownership_"+which+"_probe")
	}()

	// Six executions so a generic plan is actually in play by the time the
	// plan is captured; Postgres only considers one from the sixth onward.
	for i := 0; i < 6; i++ {
		if _, err := conn.ExecContext(ctx, execute); err != nil {
			t.Fatalf("execute %s probe: %v", which, err)
		}
	}

	rows, err := conn.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+execute)
	if err != nil {
		t.Fatalf("explain %s probe: %v", which, err)
	}
	defer func() { _ = rows.Close() }()

	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan %s plan line: %v", which, err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read %s plan: %v", which, err)
	}

	planText := plan.String()
	if strings.Contains(planText, "Seq Scan on fact_records") {
		t.Fatalf("%s page seq-scans fact_records under a generic plan:\n%s", which, planText)
	}
	if !strings.Contains(planText, indexName) {
		t.Fatalf("%s page does not use %s under a generic plan:\n%s", which, indexName, planText)
	}
	if which == "cursor" {
		condStart := strings.Index(planText, "Index Cond")
		if condStart < 0 {
			t.Fatalf("cursor page has no index condition under a generic plan:\n%s", planText)
		}
		condLine := planText[condStart:]
		if end := strings.Index(condLine, "\n"); end >= 0 {
			condLine = condLine[:end]
		}
		if !strings.Contains(condLine, "observed_at") || !strings.Contains(condLine, "fact_id") {
			t.Fatalf("row comparison is not in the index condition, so the page cannot seek:\n%s", planText)
		}
		// The row comparison must do the seeking: a Filter that discards a
		// page or more per page is the corpus-scaled rescan this change
		// removes. Small bounded removals stay allowed -- the 60-row
		// scope_generations status check and the planner's redundant
		// re-check of the already-ranged CTE output remove nothing that
		// scales with the fact corpus.
		for _, line := range strings.Split(planText, "\n") {
			line = strings.TrimSpace(line)
			const prefix = "Rows Removed by Filter: "
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			var removed int64
			if _, err := fmt.Sscanf(strings.TrimPrefix(line, prefix), "%d", &removed); err != nil {
				t.Fatalf("unparsable filter removal %q: %v", line, err)
			}
			if removed >= int64(listFactsByKindPageSize) {
				t.Fatalf("cursor page filters %d rows (a page or more) instead of seeking:\n%s", removed, planText)
			}
		}
	}
}

// explainOwnershipPageBuffers returns the plan-total shared buffers for one
// shipped page under a forced generic plan.
func explainOwnershipPageBuffers(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	cursorPage bool,
	cursorTS string,
	cursorID string,
) int64 {
	t.Helper()

	// EXPLAIN through a dedicated connection with a forced generic plan, so
	// the measured shape is the one the services actually run.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection for buffers probe: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("set force_generic_plan for buffers probe: %v", err)
	}
	// Six prepared executions first: with plan_cache_mode forced the very
	// first execution is already generic; the warmups only steady the cache.
	probeName := "ownership_buffers_first"
	probeSQL := listActivePackageOwnershipFactsFirstQuery
	probeArgs := fmt.Sprintf("(%d)", listFactsByKindPageSize)
	if cursorPage {
		probeName = "ownership_buffers_cursor"
		probeSQL = listActivePackageOwnershipFactsCursorQuery
		probeArgs = fmt.Sprintf("('%s'::timestamptz, '%s', %d)", cursorTS, cursorID, listFactsByKindPageSize)
	}
	if _, err := conn.ExecContext(ctx, "PREPARE "+probeName+" AS "+probeSQL); err != nil {
		t.Fatalf("prepare buffers probe: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, "DEALLOCATE "+probeName) }()
	for i := 0; i < 6; i++ {
		if _, err := conn.ExecContext(ctx, "EXECUTE "+probeName+probeArgs); err != nil {
			t.Fatalf("warmup buffers probe: %v", err)
		}
	}
	rows, err := conn.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) EXECUTE "+probeName+probeArgs)
	if err != nil {
		t.Fatalf("explain buffers probe: %v", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan buffers plan line: %v", err)
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Buffers:") {
			continue
		}
		var hit, read int64
		for _, field := range strings.Fields(line) {
			if v, ok := strings.CutPrefix(field, "hit="); ok {
				_, _ = fmt.Sscanf(v, "%d", &hit)
			}
			if v, ok := strings.CutPrefix(field, "read="); ok {
				_, _ = fmt.Sscanf(v, "%d", &read)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read buffers plan: %v", err)
		}
		return hit + read
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read buffers plan: %v", err)
	}
	t.Fatal("no Buffers line in page plan")
	return 0
}
