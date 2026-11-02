// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// censusTable is one table the run can touch: how to read its status and
// which id column carries the synthetic prefix.
type censusTable struct {
	name     string
	status   string // SQL expression for the status key; constant for status-less tables
	prefixed string // column the prefix filter binds
}

// censusTables is every satellite (and in-tree) table the run can touch,
// child tables first: DeletePrefixed walks it in this order. The SQL text
// below is built only from these constants, never from caller input.
var censusTables = []censusTable{
	{name: "workflow_claims", status: "status", prefixed: "work_item_id"},
	{name: "component_quota_windows", status: "component", prefixed: "scope_id"},
	{name: "workflow_claim_pacing", status: "signal", prefixed: "domain"},
	{name: "workflow_run_progress", status: "'row'", prefixed: "run_id"},
	{name: "workflow_run_completeness", status: "'row'", prefixed: "run_id"},
	{name: "workflow_work_items", status: "status", prefixed: "work_item_id"},
	{name: "workflow_runs", status: "status", prefixed: "run_id"},
	{name: "collector_instances", status: "enabled::text", prefixed: "instance_id"},
}

// SQLObserver reads one leg's database. It holds its own small pool so the
// sampler never competes with the leg's pool for a connection.
type SQLObserver struct {
	db         *sql.DB
	tables     []censusTable
	pinColumn  bool
	deletables map[string]bool
}

// NewSQLObserver probes which census tables exist on db (the in-tree
// database lacks workflow_run_progress, the satellite lacks none) and
// whether workflow_claims carries the satellite commit-pin column.
func NewSQLObserver(ctx context.Context, db *sql.DB) (*SQLObserver, error) {
	o := &SQLObserver{db: db, deletables: map[string]bool{}}
	for _, table := range censusTables {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+table.name).Scan(&exists); err != nil {
			return nil, fmt.Errorf("probe table %s: %w", table.name, err)
		}
		if exists {
			o.tables = append(o.tables, table)
			// collector_instances is never written by the harness, so
			// it is censused but never deleted from.
			o.deletables[table.name] = table.name != "collector_instances"
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
WHERE work_item_id LIKE $1 GROUP BY status`, likePattern)
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
WHERE status = 'active' AND work_item_id LIKE $1 GROUP BY work_item_id`, likePattern)
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
ORDER BY c.work_item_id LIMIT 20`, likePattern)
	if err != nil {
		return nil, fmt.Errorf("fencing check: %w", err)
	}
	return scanStrings(rows)
}

// DoubleTerminal implements Observer.
func (o *SQLObserver) DoubleTerminal(ctx context.Context) ([]string, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT work_item_id FROM workflow_claims
WHERE work_item_id LIKE $1 AND status IN ('completed', 'failed_terminal')
GROUP BY work_item_id HAVING count(*) > 1 ORDER BY work_item_id LIMIT 20`, likePattern)
	if err != nil {
		return nil, fmt.Errorf("double terminal check: %w", err)
	}
	return scanStrings(rows)
}

// Census implements Observer.
func (o *SQLObserver) Census(ctx context.Context) (Census, error) {
	c := Census{Prefixed: map[string]int{}, Other: map[string]int{}}
	for _, table := range o.tables {
		query := fmt.Sprintf(`SELECT %s, %s LIKE $1, count(*) FROM %s GROUP BY 1, 2`,
			table.status, table.prefixed, table.name)
		rows, err := o.db.QueryContext(ctx, query, likePattern)
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
WHERE claim_id = ANY($1) AND claim_id LIKE $2`, pin), claimIDs, likePattern)
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
FROM component_quota_windows WHERE scope_id LIKE $1 ORDER BY scope_id, window_start`, likePattern)
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

// NonTerminalItems implements Observer.
func (o *SQLObserver) NonTerminalItems(ctx context.Context, limit int) ([]string, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT work_item_id || ':' || status FROM workflow_work_items
WHERE work_item_id LIKE $1 AND status NOT IN ('completed', 'failed_terminal')
ORDER BY work_item_id LIMIT $2`, likePattern, limit)
	if err != nil {
		return nil, fmt.Errorf("non-terminal items: %w", err)
	}
	return scanStrings(rows)
}

// DeletePrefixed implements Observer: child tables first, each in batches
// of at most batch rows chosen by ctid, until a batch deletes nothing.
func (o *SQLObserver) DeletePrefixed(ctx context.Context, batch int) (map[string]int, error) {
	deleted := map[string]int{}
	for _, table := range o.tables {
		if !o.deletables[table.name] {
			continue
		}
		query := fmt.Sprintf(`DELETE FROM %[1]s WHERE ctid IN (
    SELECT ctid FROM %[1]s WHERE %[2]s LIKE $1 LIMIT $2)`, table.name, table.prefixed)
		for {
			res, err := o.db.ExecContext(ctx, query, likePattern, batch)
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
