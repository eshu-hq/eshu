// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// censusTable is one table the run can touch: how to read its status,
// which id column carries the harness id, and which id family that column
// holds (run, item, or scope; see seedPattern).
type censusTable struct {
	name     string
	status   string // SQL expression for the status key; constant for status-less tables
	prefixed string // column the family filter binds
	family   string // LIKE pattern of the harness family in that column
	key      string // "run", "item", "scope", or "" (never deleted)
}

// seedPattern is the delete filter for one table: the seed's own runs and
// items, and the harness scope family for scope-keyed tables. Tables with
// no key are never deleted from.
func (t censusTable) seedPattern(seed int64) string {
	switch t.key {
	case "run":
		return seedRunPattern(seed)
	case "item":
		return seedItemPattern(seed)
	case "scope":
		return scopeFamily
	}
	return ""
}

// censusTables is every satellite (and in-tree) table the run can touch,
// child tables first: DeletePrefixed walks it in this order. The SQL text
// below is built only from these constants, never from caller input.
var censusTables = []censusTable{
	{name: "workflow_claims", status: "status", prefixed: "work_item_id", family: itemFamily, key: "item"},
	{name: "component_quota_windows", status: "component", prefixed: "scope_id", family: scopeFamily, key: "scope"},
	{name: "workflow_claim_pacing", status: "signal", prefixed: "domain", family: Prefix + "%"},
	{name: "workflow_run_progress", status: "'row'", prefixed: "run_id", family: runFamily, key: "run"},
	{name: "workflow_run_completeness", status: "'row'", prefixed: "run_id", family: runFamily, key: "run"},
	{name: "workflow_work_items", status: "status", prefixed: "work_item_id", family: itemFamily, key: "item"},
	{name: "workflow_runs", status: "status", prefixed: "run_id", family: runFamily, key: "run"},
	{name: "collector_instances", status: "enabled::text", prefixed: "instance_id", family: instanceFamily},
}

// SQLObserver reads one leg's database. It holds its own small pool so the
// sampler never competes with the leg's pool for a connection.
type SQLObserver struct {
	db        *sql.DB
	tables    []censusTable
	pinColumn bool
}

// NewSQLObserver probes which census tables exist on db (the in-tree
// database lacks workflow_run_progress, the satellite lacks none) and
// whether workflow_claims carries the satellite commit-pin column.
func NewSQLObserver(ctx context.Context, db *sql.DB) (*SQLObserver, error) {
	o := &SQLObserver{db: db}
	for _, table := range censusTables {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+table.name).Scan(&exists); err != nil {
			return nil, fmt.Errorf("probe table %s: %w", table.name, err)
		}
		if exists {
			o.tables = append(o.tables, table)
		}
	}
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = 'workflow_claims' AND column_name = 'commit_pinned_until')`).Scan(&o.pinColumn); err != nil {
		return nil, fmt.Errorf("probe commit pin column: %w", err)
	}
	return o, nil
}

// Snapshot implements Observer.
func (o *SQLObserver) Snapshot(ctx context.Context) (Snapshot, error) {
	snap := Snapshot{ItemsByStatus: map[string]int{}}
	rows, err := o.db.QueryContext(ctx, `SELECT status, count(*) FROM workflow_work_items
WHERE work_item_id LIKE $1 GROUP BY status`, itemFamily)
	if err != nil {
		return snap, fmt.Errorf("snapshot items: %w", err)
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			_ = rows.Close()
			return snap, fmt.Errorf("snapshot items: %w", err)
		}
		snap.ItemsByStatus[status] = n
		snap.ItemTotal += n
	}
	if err := closeRows(rows); err != nil {
		return snap, fmt.Errorf("snapshot items: %w", err)
	}
	// The active-claim partial scan rides workflow_claims_active_expiry_idx
	// (status, lease_expires_at): it stays cheap at any backlog size.
	dual, err := o.db.QueryContext(ctx, `SELECT work_item_id, count(*) FROM workflow_claims
WHERE status = 'active' AND work_item_id LIKE $1 GROUP BY work_item_id`, itemFamily)
	if err != nil {
		return snap, fmt.Errorf("snapshot claims: %w", err)
	}
	for dual.Next() {
		var item string
		var n int
		if err := dual.Scan(&item, &n); err != nil {
			_ = dual.Close()
			return snap, fmt.Errorf("snapshot claims: %w", err)
		}
		snap.ActiveClaims += n
		if n > 1 {
			snap.DualLive = append(snap.DualLive, item)
		}
	}
	return snap, closeRows(dual)
}

// FencingViolations implements Observer. A grant increments the item's
// current token and inserts a claim at that token, so a claim token above
// the item's current token, or two claims at one token, is a regression.
func (o *SQLObserver) FencingViolations(ctx context.Context) ([]string, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT c.work_item_id FROM (
    SELECT work_item_id, max(fencing_token) AS max_token, count(*) AS n,
        count(DISTINCT fencing_token) AS distinct_tokens
    FROM workflow_claims WHERE work_item_id LIKE $1 GROUP BY work_item_id
) AS c JOIN workflow_work_items AS i ON i.work_item_id = c.work_item_id
WHERE i.current_fencing_token < c.max_token OR c.n <> c.distinct_tokens
ORDER BY c.work_item_id LIMIT 20`, itemFamily)
	if err != nil {
		return nil, fmt.Errorf("fencing check: %w", err)
	}
	return scanStrings(rows)
}

// DoubleTerminal implements Observer.
func (o *SQLObserver) DoubleTerminal(ctx context.Context) ([]string, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT work_item_id FROM workflow_claims
WHERE work_item_id LIKE $1 AND status IN ('completed', 'failed_terminal')
GROUP BY work_item_id HAVING count(*) > 1 ORDER BY work_item_id LIMIT 20`, itemFamily)
	if err != nil {
		return nil, fmt.Errorf("double terminal check: %w", err)
	}
	return scanStrings(rows)
}

// Census implements Observer.
func (o *SQLObserver) Census(ctx context.Context) (Census, error) {
	c := Census{Prefixed: map[string]int{}, Other: map[string]int{}}
	for _, table := range o.tables {
		// #nosec G201 -- identifiers come only from the compile-time censusTables constants; the family value is bound as $1.
		query := fmt.Sprintf(`SELECT %s, %s LIKE $1, count(*) FROM %s GROUP BY 1, 2`,
			table.status, table.prefixed, table.name)
		rows, err := o.db.QueryContext(ctx, query, table.family)
		if err != nil {
			return c, fmt.Errorf("census %s: %w", table.name, err)
		}
		for rows.Next() {
			var status sql.NullString
			var prefixed bool
			var n int
			if err := rows.Scan(&status, &prefixed, &n); err != nil {
				_ = rows.Close()
				return c, fmt.Errorf("census %s: %w", table.name, err)
			}
			key := table.name + "|" + status.String
			if prefixed {
				c.Prefixed[key] += n
			} else {
				c.Other[key] += n
			}
		}
		if err := closeRows(rows); err != nil {
			return c, fmt.Errorf("census %s: %w", table.name, err)
		}
	}
	return c, nil
}

// ClaimRows implements Observer.
func (o *SQLObserver) ClaimRows(ctx context.Context, claimIDs []string) (map[string]ClaimRow, error) {
	out := make(map[string]ClaimRow, len(claimIDs))
	if len(claimIDs) == 0 {
		return out, nil
	}
	pin := "NULL::timestamptz"
	if o.pinColumn {
		pin = "commit_pinned_until"
	}
	rows, err := o.db.QueryContext(ctx, fmt.Sprintf(`SELECT claim_id, work_item_id, fencing_token, status,
    lease_expires_at, finished_at, %s FROM workflow_claims
WHERE claim_id = ANY($1) AND claim_id LIKE $2`, pin), claimIDs, claimFamily)
	if err != nil {
		return nil, fmt.Errorf("claim rows: %w", err)
	}
	for rows.Next() {
		var r ClaimRow
		var finished, pinned sql.NullTime
		if err := rows.Scan(&r.ClaimID, &r.WorkItemID, &r.FencingToken, &r.Status,
			&r.LeaseExpiresAt, &finished, &pinned); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("claim rows: %w", err)
		}
		r.FinishedAt, r.PinnedUntil = finished.Time, pinned.Time
		out[r.ClaimID] = r
	}
	return out, closeRows(rows)
}

// QuotaBuckets implements Observer.
func (o *SQLObserver) QuotaBuckets(ctx context.Context) ([]QuotaBucket, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT component, scope_id, window_start, grants
FROM component_quota_windows WHERE scope_id LIKE $1 ORDER BY scope_id, window_start`, scopeFamily)
	if err != nil {
		return nil, fmt.Errorf("quota buckets: %w", err)
	}
	var out []QuotaBucket
	for rows.Next() {
		var b QuotaBucket
		if err := rows.Scan(&b.Component, &b.ScopeID, &b.WindowStart, &b.Grants); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("quota buckets: %w", err)
		}
		out = append(out, b)
	}
	return out, closeRows(rows)
}

// AnalyzedAt implements Observer.
func (o *SQLObserver) AnalyzedAt(ctx context.Context) (time.Time, error) {
	var at sql.NullTime
	err := o.db.QueryRowContext(ctx, `SELECT GREATEST(last_analyze, last_autoanalyze)
FROM pg_stat_user_tables WHERE schemaname = 'public' AND relname = 'workflow_work_items'`).Scan(&at)
	if err != nil {
		return time.Time{}, fmt.Errorf("read work item analyze time: %w", err)
	}
	return at.Time, nil
}

// NonTerminalItems implements Observer.
func (o *SQLObserver) NonTerminalItems(ctx context.Context, limit int) ([]string, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT work_item_id || ':' || status FROM workflow_work_items
WHERE work_item_id LIKE $1 AND status NOT IN ('completed', 'failed_terminal')
ORDER BY work_item_id LIMIT $2`, itemFamily, limit)
	if err != nil {
		return nil, fmt.Errorf("non-terminal items: %w", err)
	}
	return scanStrings(rows)
}

// DeletePrefixed implements Observer: child tables first, each in batches
// of at most batch rows chosen by ctid, until a batch deletes nothing. It
// deletes only this seed's runs, items, and claims and the harness scope
// family; tables without a key (pacing, collector_instances) are never
// touched.
func (o *SQLObserver) DeletePrefixed(ctx context.Context, seed int64, batch int) (map[string]int, error) {
	deleted := map[string]int{}
	for _, table := range o.tables {
		pattern := table.seedPattern(seed)
		if pattern == "" {
			continue
		}
		// #nosec G201 -- identifiers come only from the compile-time censusTables constants; pattern and batch are bound as $1/$2.
		query := fmt.Sprintf(`DELETE FROM %[1]s WHERE ctid IN (
    SELECT ctid FROM %[1]s WHERE %[2]s LIKE $1 LIMIT $2)`, table.name, table.prefixed)
		for {
			res, err := o.db.ExecContext(ctx, query, pattern, batch)
			if err != nil {
				return deleted, fmt.Errorf("delete prefixed %s: %w", table.name, err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return deleted, fmt.Errorf("delete prefixed %s: %w", table.name, err)
			}
			deleted[table.name] += int(n)
			if n == 0 {
				break
			}
		}
	}
	return deleted, nil
}

// CurrentDatabase returns the connected database name, recorded as
// intree_database_name and checked against the satellite expectation.
func CurrentDatabase(ctx context.Context, db *sql.DB) (string, error) {
	var name string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&name); err != nil {
		return "", fmt.Errorf("read current database: %w", err)
	}
	return name, nil
}

func scanStrings(rows *sql.Rows) ([]string, error) {
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan row: %w", err)
		}
		out = append(out, s)
	}
	return out, closeRows(rows)
}

func closeRows(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close rows: %w", err)
	}
	return nil
}

// sampleTimeout bounds one sampler query round so a stuck database turns
// into a recorded sampler error instead of a hung harness.
const sampleTimeout = 10 * time.Second
