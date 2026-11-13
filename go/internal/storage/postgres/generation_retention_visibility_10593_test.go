// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestGuardedTablesVisible10593 is the seeded RED for the #10593
// visibility precondition: a guarded table counts as vacuum-settled only
// when pg_class reports relallvisible=relpages for every guarded table.
func TestGuardedTablesVisible10593(t *testing.T) {
	t.Parallel()
	tables := []string{"t1", "t2"}
	for _, tc := range []struct {
		name       string
		visibility map[string]guardedTableVisibility10512
		tables     []string
		want       bool
	}{
		{
			name: "all settled",
			visibility: map[string]guardedTableVisibility10512{
				"t1": {reltuples: 100, relpages: 2, relallvisible: 2},
				"t2": {reltuples: 50, relpages: 1, relallvisible: 1},
			},
			tables: tables,
			want:   true,
		},
		{
			name: "one table pinned by a horizon",
			visibility: map[string]guardedTableVisibility10512{
				"t1": {reltuples: 51200, relpages: 1064, relallvisible: 0},
				"t2": {reltuples: 50, relpages: 1, relallvisible: 1},
			},
			tables: tables,
			want:   false,
		},
		{
			name: "partially set map stays unsettled",
			visibility: map[string]guardedTableVisibility10512{
				"t1": {reltuples: 51200, relpages: 1064, relallvisible: 532},
				"t2": {reltuples: 50, relpages: 1, relallvisible: 1},
			},
			tables: tables,
			want:   false,
		},
		{
			name: "missing table fails closed",
			visibility: map[string]guardedTableVisibility10512{
				"t1": {reltuples: 100, relpages: 2, relallvisible: 2},
			},
			tables: tables,
			want:   false,
		},
		{
			name: "empty table is vacuously settled",
			visibility: map[string]guardedTableVisibility10512{
				"t1": {reltuples: 0, relpages: 0, relallvisible: 0},
				"t2": {reltuples: 0, relpages: 0, relallvisible: 0},
			},
			tables: tables,
			want:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := guardedTablesVisible10512(tc.visibility, tc.tables); got != tc.want {
				t.Fatalf("guardedTablesVisible10512() = %v, want %v (visibility %+v)", got, tc.want, tc.visibility)
			}
		})
	}
}

// TestRetentionGuardedPlanFailureIncludesVisibility10593 is the seeded RED
// for the #10593 self-identifying guard message: the failure text must
// carry the pg_class values so the next CI horizon names itself.
func TestRetentionGuardedPlanFailureIncludesVisibility10593(t *testing.T) {
	t.Parallel()
	visibility := map[string]guardedTableVisibility10512{
		"shared_projection_intents": {reltuples: 51200, relpages: 1064, relallvisible: 0},
	}
	message := retentionGuardedPlanFailure10512("cold custom", "row_counts",
		[]string{"Seq Scan on shared_projection_intents"}, visibility,
		[]string{"shared_projection_intents"})
	for _, want := range []string{
		"cold custom",
		"row_counts",
		"Seq Scan on shared_projection_intents",
		"shared_projection_intents",
		"reltuples=51200",
		"relpages=1064",
		"relallvisible=0",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("guard failure message misses %q:\n%s", want, message)
		}
	}
}

// TestWaitForGuardedTablesVisible10593 is the seeded RED pair for the
// #10593 bounded poll: it returns once a re-VACUUM settles the map, it
// vacuums only the still-unsettled tables, and it fails closed (ok=false)
// when the deadline passes with the map unset.
func TestWaitForGuardedTablesVisible10593(t *testing.T) {
	t.Parallel()
	tables := []string{"t1", "t2"}
	settled := map[string]guardedTableVisibility10512{
		"t1": {reltuples: 100, relpages: 2, relallvisible: 2},
		"t2": {reltuples: 50, relpages: 1, relallvisible: 1},
	}
	pinned := map[string]guardedTableVisibility10512{
		"t1": {reltuples: 100, relpages: 2, relallvisible: 0},
		"t2": {reltuples: 50, relpages: 1, relallvisible: 1},
	}
	t.Run("settles after one vacuum round", func(t *testing.T) {
		t.Parallel()
		var vacuumed []string
		rounds := 0
		visibility, ok, err := waitForGuardedTablesVisible10512(tables,
			func(table string) error {
				vacuumed = append(vacuumed, table)
				return nil
			},
			func() (map[string]guardedTableVisibility10512, error) {
				rounds++
				if rounds < 2 {
					return pinned, nil
				}
				return settled, nil
			}, 5*time.Second, time.Millisecond)
		if err != nil {
			t.Fatalf("wait returned error: %v", err)
		}
		if !ok {
			t.Fatalf("wait gave up on a map that settles after one vacuum round")
		}
		if len(vacuumed) != 1 || vacuumed[0] != "t1" {
			t.Fatalf("wait vacuumed %v, want exactly the unsettled table [t1]", vacuumed)
		}
		if !guardedTablesVisible10512(visibility, tables) {
			t.Fatalf("wait returned an unsettled map: %+v", visibility)
		}
	})
	t.Run("already settled vacuums nothing", func(t *testing.T) {
		t.Parallel()
		vacuumed := 0
		_, ok, err := waitForGuardedTablesVisible10512(tables,
			func(string) error {
				vacuumed++
				return nil
			},
			func() (map[string]guardedTableVisibility10512, error) { return settled, nil },
			5*time.Second, time.Millisecond)
		if err != nil || !ok {
			t.Fatalf("wait on a settled map: ok=%v err=%v", ok, err)
		}
		if vacuumed != 0 {
			t.Fatalf("wait vacuumed %d tables on an already settled map", vacuumed)
		}
	})
	t.Run("pinned horizon fails closed", func(t *testing.T) {
		t.Parallel()
		vacuumed := 0
		visibility, ok, err := waitForGuardedTablesVisible10512(tables,
			func(string) error {
				vacuumed++
				return nil
			},
			func() (map[string]guardedTableVisibility10512, error) { return pinned, nil },
			50*time.Millisecond, 10*time.Millisecond)
		if err != nil {
			t.Fatalf("wait returned error: %v", err)
		}
		if ok {
			t.Fatalf("wait reported success on a pinned visibility map")
		}
		if vacuumed == 0 {
			t.Fatalf("wait gave up without re-running VACUUM once")
		}
		if guardedTablesVisible10512(visibility, tables) {
			t.Fatalf("wait returned a settled map with ok=false: %+v", visibility)
		}
	})
	t.Run("query error aborts the poll", func(t *testing.T) {
		t.Parallel()
		_, _, err := waitForGuardedTablesVisible10512(tables,
			func(string) error { return nil },
			func() (map[string]guardedTableVisibility10512, error) {
				return nil, errWaitForSentinel10593
			}, 5*time.Second, time.Millisecond)
		if err == nil {
			t.Fatalf("wait swallowed the visibility query error")
		}
	})
}

// guardedTableVisibility10512 is one pg_class row for a guarded retention
// table: the planner prices the intents arm's index-only probe from the
// visibility-map fraction relallvisible/relpages, so the cold-plan guard is
// only meaningful once VACUUM has settled every guarded table.
type guardedTableVisibility10512 struct {
	reltuples     float64
	relpages      int64
	relallvisible int64
}

// retentionGuardedVisibilityTables10512 are the tables whose visibility
// map must read relallvisible=relpages before the cold plan checks run
// (#10593): the three tables the cold-custom guard requires index probes
// on. Plain VACUUM already runs before the checks; this list only names
// the tables the bounded poll re-vacuums and asserts on.
var retentionGuardedVisibilityTables10512 = []string{
	"shared_projection_intents",
	"package_manifest_consumption_keys",
	"package_registry_identity_keys",
}

// errWaitForSentinel10593 is the test-only visibility query failure the
// "query error aborts the poll" leg injects.
var errWaitForSentinel10593 = errors.New("sentinel visibility query error")

// guardedTablesVisible10512 reports whether pg_class shows a settled
// visibility map for every table: relallvisible=relpages. A missing table
// fails closed.
func guardedTablesVisible10512(visibility map[string]guardedTableVisibility10512, tables []string) bool {
	for _, table := range tables {
		settled, ok := visibility[table]
		if !ok || settled.relallvisible != settled.relpages {
			return false
		}
	}
	return true
}

// formatGuardedTableVisibility10512 renders one pg_class line per table
// for guard failure messages, so the next CI horizon self-identifies.
func formatGuardedTableVisibility10512(visibility map[string]guardedTableVisibility10512, tables []string) string {
	var lines strings.Builder
	for _, table := range tables {
		settled := visibility[table]
		fmt.Fprintf(&lines, "%s reltuples=%.0f relpages=%d relallvisible=%d\n",
			table, settled.reltuples, settled.relpages, settled.relallvisible)
	}
	return strings.TrimSuffix(lines.String(), "\n")
}

// retentionGuardedPlanFailure10512 builds the plan-guard failure message
// with the pg_class visibility snapshot attached (#10593).
func retentionGuardedPlanFailure10512(condition, statement string, violations []string, visibility map[string]guardedTableVisibility10512, tables []string) string {
	return fmt.Sprintf("%s %s plan leaves its generation index: %s\npg_class visibility at failure:\n%s",
		condition, statement, strings.Join(violations, "; "), formatGuardedTableVisibility10512(visibility, tables))
}

// waitForGuardedTablesVisible10512 polls queryVisibility until every table
// reads relallvisible=relpages, re-running vacuum on the still-unsettled
// tables between polls. It returns ok=false (with the last snapshot) when
// timeout passes with the map unset, so the caller can fail closed with
// the holder attached; vacuum or query failures abort with err.
func waitForGuardedTablesVisible10512(tables []string, vacuum func(string) error, queryVisibility func() (map[string]guardedTableVisibility10512, error), timeout, interval time.Duration) (map[string]guardedTableVisibility10512, bool, error) {
	start := time.Now()
	for {
		visibility, err := queryVisibility()
		if err != nil {
			return visibility, false, err
		}
		if guardedTablesVisible10512(visibility, tables) {
			return visibility, true, nil
		}
		if time.Since(start) >= timeout {
			return visibility, false, nil
		}
		for _, table := range tables {
			if settled, ok := visibility[table]; !ok || settled.relallvisible != settled.relpages {
				if err := vacuum(table); err != nil {
					return visibility, false, err
				}
			}
		}
		if remaining := time.Until(start.Add(timeout)); remaining < interval {
			// Sleep only to the deadline so the last snapshot is read at
			// the horizon instead of overshooting it.
			if remaining > 0 {
				time.Sleep(remaining)
			}
		} else {
			time.Sleep(interval)
		}
	}
}

// queryGuardedTableVisibility10512 reads reltuples/relpages/relallvisible
// for the guarded tables from pg_class in the test database.
func queryGuardedTableVisibility10512(ctx context.Context, database *sql.DB, tables []string) (map[string]guardedTableVisibility10512, error) {
	placeholders := make([]string, len(tables))
	args := make([]any, len(tables))
	for i, table := range tables {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = table
	}
	rows, err := database.QueryContext(ctx,
		`SELECT relname, reltuples, relpages, relallvisible FROM pg_class WHERE relname IN (`+strings.Join(placeholders, ", ")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	visibility := make(map[string]guardedTableVisibility10512, len(tables))
	for rows.Next() {
		var name string
		var settled guardedTableVisibility10512
		if err := rows.Scan(&name, &settled.reltuples, &settled.relpages, &settled.relallvisible); err != nil {
			return nil, err
		}
		visibility[name] = settled
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return visibility, nil
}

// queryVisibilityHorizonHolders10512 lists the same-database sessions
// holding backend_xmin, the snapshots that pin relallvisible below
// relpages across VACUUM (#10593). It renders text for a failure message
// and never fails: a holder-query error is reported in place of the list.
func queryVisibilityHorizonHolders10512(ctx context.Context, database *sql.DB) string {
	rows, err := database.QueryContext(ctx, `SELECT pid, usename, COALESCE(application_name, ''),
  backend_xmin::text, COALESCE(state, ''), query_start, left(query, 160)
FROM pg_stat_activity
WHERE datname = current_database() AND backend_xmin IS NOT NULL AND pid <> pg_backend_pid()
ORDER BY query_start NULLS LAST`)
	if err != nil {
		return fmt.Sprintf("pg_stat_activity query failed: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var holders strings.Builder
	for rows.Next() {
		var pid int32
		var user, app, xmin, state string
		var started sql.NullTime
		var query sql.NullString
		if err := rows.Scan(&pid, &user, &app, &xmin, &state, &started, &query); err != nil {
			return fmt.Sprintf("pg_stat_activity scan failed: %v", err)
		}
		when := "NULL"
		if started.Valid {
			when = started.Time.UTC().Format(time.RFC3339)
		}
		text := "NULL"
		if query.Valid {
			text = query.String
		}
		fmt.Fprintf(&holders, "pid=%d user=%s app=%s backend_xmin=%s state=%s query_start=%s query=%s\n",
			pid, user, app, xmin, state, when, text)
	}
	if err := rows.Err(); err != nil {
		return fmt.Sprintf("pg_stat_activity rows failed: %v", err)
	}
	if holders.Len() == 0 {
		return "no same-database backend_xmin holders observed"
	}
	return strings.TrimSuffix(holders.String(), "\n")
}

// requireGuardedTablesVisible10512 is the #10593 fail-closed gate: after
// the existing plain VACUUM, it re-runs VACUUM on the still-unsettled
// guarded tables until pg_class reads relallvisible=relpages (about a 10s
// cap). On timeout it fails with the pg_class values plus the
// same-database backend_xmin holders, so the next CI failure names the
// horizon holder instead of showing only a plan dump.
func requireGuardedTablesVisible10512(t *testing.T, ctx context.Context, database *sql.DB, tables []string) {
	t.Helper()
	const timeout = 10 * time.Second
	const interval = 500 * time.Millisecond
	visibility, ok, err := waitForGuardedTablesVisible10512(tables,
		func(table string) error {
			_, err := database.ExecContext(ctx, "VACUUM "+table)
			return err
		},
		func() (map[string]guardedTableVisibility10512, error) {
			return queryGuardedTableVisibility10512(ctx, database, tables)
		}, timeout, interval)
	if err != nil {
		t.Fatalf("visibility poll for %s: %v", strings.Join(tables, ", "), err)
	}
	if !ok {
		t.Fatalf("guarded tables never reached relallvisible=relpages within %s: the visibility map is still unset, so a same-database snapshot held across VACUUM is pinning it and cold plans may flip to sequential scans\npg_class visibility at timeout:\n%s\nsame-database backend_xmin holders:\n%s",
			timeout, formatGuardedTableVisibility10512(visibility, tables), queryVisibilityHorizonHolders10512(ctx, database))
	}
}
