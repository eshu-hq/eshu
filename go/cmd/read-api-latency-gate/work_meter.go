// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// maxBackgroundCallsPerSecond is the statement rate the idle stack may show
// before the run is refused. Anything above it means something in eshu-api (a
// new poller, a refresh worker) is querying Postgres on its own, and that noise
// would be charged to whichever route happens to be metered.
const maxBackgroundCallsPerSecond = 2.0

// WorkCounters are raw Postgres work totals since the last Reset.
type WorkCounters struct {
	Calls int64 `json:"calls"`
	Rows  int64 `json:"rows"`
	Blks  int64 `json:"blks"`
}

// WorkMeter measures Postgres work between a Reset and a Read. It is an
// interface so the hermetic RED/GREEN tests can inject a fake; the pgx
// implementation is the only production one. A meter error is always a gate
// error — the caller never skips the work assertion and continues on latency.
type WorkMeter interface {
	Reset(ctx context.Context) error
	Read(ctx context.Context) (WorkCounters, error)
}

type pgxWorkMeter struct {
	pool *pgxpool.Pool
}

// StatementWork is one numeric pg_stat_statements row from a database interval.
// QueryID is a decimal string so JSON consumers do not lose int64 precision.
type StatementWork struct {
	DBID                int64  `json:"dbid"`
	UserID              int64  `json:"userid"`
	QueryID             string `json:"queryid"`
	TopLevel            bool   `json:"toplevel"`
	StatsSinceUnixMicro int64  `json:"stats_since_unix_micro"`
	Calls               int64  `json:"calls"`
	Rows                int64  `json:"rows"`
	SharedBlksHit       int64  `json:"shared_blks_hit"`
	SharedBlksRead      int64  `json:"shared_blks_read"`
	LocalBlksHit        int64  `json:"local_blks_hit"`
	LocalBlksRead       int64  `json:"local_blks_read"`
	TempBlksRead        int64  `json:"temp_blks_read"`
	TempBlksWritten     int64  `json:"temp_blks_written"`
}

// snapshotWireRow uses pointers so absent and null counters cannot become
// plausible zero-valued measurements during JSON decoding.
type snapshotWireRow struct {
	DBID                *int64  `json:"dbid"`
	UserID              *int64  `json:"userid"`
	QueryID             *string `json:"queryid"`
	TopLevel            *bool   `json:"toplevel"`
	StatsSinceUnixMicro *int64  `json:"stats_since_unix_micro"`
	Calls               *int64  `json:"calls"`
	Rows                *int64  `json:"rows"`
	SharedBlksHit       *int64  `json:"shared_blks_hit"`
	SharedBlksRead      *int64  `json:"shared_blks_read"`
	LocalBlksHit        *int64  `json:"local_blks_hit"`
	LocalBlksRead       *int64  `json:"local_blks_read"`
	TempBlksRead        *int64  `json:"temp_blks_read"`
	TempBlksWritten     *int64  `json:"temp_blks_written"`
}

// WorkSnapshot holds numeric evidence and official totals from the same
// captured row set. Its scope is the database interval, not one HTTP request.
type WorkSnapshot struct {
	Scope          string          `json:"scope"`
	ResetUnixMicro int64           `json:"reset_unix_micro"`
	Counters       WorkCounters    `json:"counters"`
	Statements     []StatementWork `json:"statements"`
}

// WorkSnapshotMeter captures detailed database-interval evidence for the one
// serial MCP search whose gate variance is under investigation. The reset
// token stays local to one sweep, never in shared meter state.
type WorkSnapshotMeter interface {
	// BeginSnapshot resets statistics and returns the route-local reset token.
	BeginSnapshot(context.Context) (time.Time, error)
	// EndSnapshot captures one bounded row set and checks its reset epoch.
	EndSnapshot(context.Context, time.Time) (WorkSnapshot, error)
}

const maxSnapshotStatements = 10_000

// workSnapshotSQL retains at most one overflow row. The filter and six-block
// formula match workReadSQL; Go folds official totals over this exact row set.
const workSnapshotSQL = `WITH s AS MATERIALIZED (
  SELECT dbid::bigint AS dbid, userid::bigint AS userid, queryid::text AS queryid,
         toplevel, (extract(epoch FROM stats_since)*1000000)::bigint AS stats_since_unix_micro,
         calls::bigint AS calls, rows::bigint AS rows,
         shared_blks_hit::bigint AS shared_blks_hit, shared_blks_read::bigint AS shared_blks_read,
         local_blks_hit::bigint AS local_blks_hit, local_blks_read::bigint AS local_blks_read,
         temp_blks_read::bigint AS temp_blks_read, temp_blks_written::bigint AS temp_blks_written
  FROM pg_stat_statements
  WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
    AND query NOT LIKE '%pg_stat_statements%'
  LIMIT 10001
)
SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY s.dbid, s.userid, s.queryid, s.toplevel), '[]'::jsonb) FROM s`

// BeginSnapshot resets pg_stat_statements and returns its reset timestamp.
func (m pgxWorkMeter) BeginSnapshot(ctx context.Context) (time.Time, error) {
	var token time.Time
	if err := m.pool.QueryRow(ctx, "SELECT pg_stat_statements_reset()").Scan(&token); err != nil {
		return time.Time{}, fmt.Errorf("reset statement snapshot: %w", err)
	}
	if token.IsZero() {
		return time.Time{}, fmt.Errorf("reset statement snapshot: zero reset timestamp")
	}
	return token, nil
}

// EndSnapshot reads numeric work rows between two global epoch checks.
func (m pgxWorkMeter) EndSnapshot(ctx context.Context, token time.Time) (WorkSnapshot, error) {
	if token.IsZero() {
		return WorkSnapshot{}, fmt.Errorf("read statement snapshot: zero reset token")
	}
	pre, preDealloc, err := m.snapshotInfo(ctx)
	if err != nil {
		return WorkSnapshot{}, err
	}
	if !pre.Equal(token) || preDealloc != 0 {
		return WorkSnapshot{}, fmt.Errorf("read statement snapshot: reset epoch or deallocation changed before capture")
	}
	var raw []byte
	if err := m.pool.QueryRow(ctx, workSnapshotSQL).Scan(&raw); err != nil {
		return WorkSnapshot{}, fmt.Errorf("capture statement snapshot: %w", err)
	}
	post, postDealloc, err := m.snapshotInfo(ctx)
	if err != nil {
		return WorkSnapshot{}, err
	}
	if !post.Equal(token) || postDealloc != 0 {
		return WorkSnapshot{}, fmt.Errorf("read statement snapshot: reset epoch or deallocation changed during capture")
	}
	return decodeWorkSnapshot(token, raw)
}

func decodeWorkSnapshot(token time.Time, raw []byte) (WorkSnapshot, error) {
	if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
		return WorkSnapshot{}, fmt.Errorf("capture statement snapshot: expected complete JSON array")
	}
	var wireRows []snapshotWireRow
	if err := json.Unmarshal(raw, &wireRows); err != nil {
		return WorkSnapshot{}, fmt.Errorf("decode statement snapshot: %w", err)
	}
	statements := make([]StatementWork, 0, len(wireRows))
	for i, row := range wireRows {
		if row.DBID == nil || row.UserID == nil || row.QueryID == nil || row.TopLevel == nil || row.StatsSinceUnixMicro == nil ||
			row.Calls == nil || row.Rows == nil || row.SharedBlksHit == nil || row.SharedBlksRead == nil ||
			row.LocalBlksHit == nil || row.LocalBlksRead == nil || row.TempBlksRead == nil || row.TempBlksWritten == nil {
			return WorkSnapshot{}, fmt.Errorf("decode statement snapshot: row %d is incomplete", i)
		}
		statements = append(statements, StatementWork{
			DBID: *row.DBID, UserID: *row.UserID, QueryID: *row.QueryID, TopLevel: *row.TopLevel,
			StatsSinceUnixMicro: *row.StatsSinceUnixMicro, Calls: *row.Calls, Rows: *row.Rows,
			SharedBlksHit: *row.SharedBlksHit, SharedBlksRead: *row.SharedBlksRead,
			LocalBlksHit: *row.LocalBlksHit, LocalBlksRead: *row.LocalBlksRead,
			TempBlksRead: *row.TempBlksRead, TempBlksWritten: *row.TempBlksWritten,
		})
	}
	return validateWorkSnapshot(token, statements)
}

func (m pgxWorkMeter) snapshotInfo(ctx context.Context) (time.Time, int64, error) {
	var epoch time.Time
	var dealloc int64
	if err := m.pool.QueryRow(ctx, "SELECT stats_reset, dealloc FROM pg_stat_statements_info").Scan(&epoch, &dealloc); err != nil {
		return time.Time{}, 0, fmt.Errorf("read pg_stat_statements_info: %w", err)
	}
	return epoch, dealloc, nil
}

func validateWorkSnapshot(token time.Time, rows []StatementWork) (WorkSnapshot, error) {
	if len(rows) > maxSnapshotStatements {
		return WorkSnapshot{}, fmt.Errorf("statement snapshot exceeds %d rows", maxSnapshotStatements)
	}
	if token.IsZero() {
		return WorkSnapshot{}, fmt.Errorf("statement snapshot has zero reset token")
	}
	if rows == nil {
		rows = []StatementWork{}
	}
	seen := make(map[string]bool, len(rows))
	var total WorkCounters
	for _, row := range rows {
		queryID, err := strconv.ParseInt(row.QueryID, 10, 64)
		if err != nil || strconv.FormatInt(queryID, 10) != row.QueryID {
			return WorkSnapshot{}, fmt.Errorf("statement snapshot has invalid queryid")
		}
		if row.DBID <= 0 || row.UserID <= 0 || row.StatsSinceUnixMicro < token.UnixMicro() {
			return WorkSnapshot{}, fmt.Errorf("statement snapshot has invalid identity or epoch")
		}
		key := fmt.Sprintf("%d/%d/%s/%t", row.DBID, row.UserID, row.QueryID, row.TopLevel)
		if seen[key] {
			return WorkSnapshot{}, fmt.Errorf("statement snapshot has duplicate identity")
		}
		seen[key] = true
		parts := []int64{row.Calls, row.Rows, row.SharedBlksHit, row.SharedBlksRead, row.LocalBlksHit, row.LocalBlksRead, row.TempBlksRead, row.TempBlksWritten}
		for _, value := range parts {
			if value < 0 {
				return WorkSnapshot{}, fmt.Errorf("statement snapshot has negative counter")
			}
		}
		if total.Calls > math.MaxInt64-row.Calls || total.Rows > math.MaxInt64-row.Rows {
			return WorkSnapshot{}, fmt.Errorf("statement snapshot counter overflow")
		}
		total.Calls += row.Calls
		total.Rows += row.Rows
		for _, value := range parts[2:] {
			if total.Blks > math.MaxInt64-value {
				return WorkSnapshot{}, fmt.Errorf("statement snapshot block counter overflow")
			}
			total.Blks += value
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.DBID != b.DBID {
			return a.DBID < b.DBID
		}
		if a.UserID != b.UserID {
			return a.UserID < b.UserID
		}
		if a.QueryID != b.QueryID {
			return a.QueryID < b.QueryID
		}
		return !a.TopLevel && b.TopLevel
	})
	return WorkSnapshot{Scope: "database_interval", ResetUnixMicro: token.UnixMicro(), Counters: total, Statements: rows}, nil
}

// workReadSQL sums the counters over the current database, excluding the
// meter's own statements. Buffers are shared, local and temp, read and hit and
// written: index probes are buffer hits, so hit counts are the signal a
// nested-loop plan multiplies. No pg_stat_statements timing column is read: that
// would be latency again.
const workReadSQL = `SELECT coalesce(sum(calls), 0)::bigint,
       coalesce(sum(rows), 0)::bigint,
       coalesce(sum(shared_blks_hit + shared_blks_read + local_blks_hit + local_blks_read + temp_blks_read + temp_blks_written), 0)::bigint
FROM pg_stat_statements
WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND query NOT LIKE '%pg_stat_statements%'`

// NewPgxWorkMeter returns the production WorkMeter, reading pg_stat_statements
// through pool.
func NewPgxWorkMeter(pool *pgxpool.Pool) WorkMeter {
	return pgxWorkMeter{pool: pool}
}

func (m pgxWorkMeter) Reset(ctx context.Context) error {
	if _, err := m.pool.Exec(ctx, "SELECT pg_stat_statements_reset()"); err != nil {
		return fmt.Errorf("reset pg_stat_statements: %w", err)
	}
	return nil
}

func (m pgxWorkMeter) Read(ctx context.Context) (WorkCounters, error) {
	var c WorkCounters
	if err := m.pool.QueryRow(ctx, workReadSQL).Scan(&c.Calls, &c.Rows, &c.Blks); err != nil {
		return WorkCounters{}, fmt.Errorf("read pg_stat_statements: %w", err)
	}
	return c, nil
}

// EnsureWorkMeterExtension creates pg_stat_statements if needed and proves it
// is actually collecting. CREATE EXTENSION alone succeeds without
// shared_preload_libraries, and the view then errors on first read, so the
// read here turns a missing docker-compose.read-api-latency-gate.yaml override
// into an immediate, named failure instead of a mid-sweep one.
func EnsureWorkMeterExtension(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements"); err != nil {
		return fmt.Errorf("create pg_stat_statements: %w", err)
	}
	if _, err := NewPgxWorkMeter(pool).Read(ctx); err != nil {
		return fmt.Errorf("pg_stat_statements is not collecting (is docker-compose.read-api-latency-gate.yaml applied and Postgres restarted with shared_preload_libraries?): %w", err)
	}
	return nil
}

// MeasureBackgroundCallRate resets the meter, waits idle with no requests
// issued, and returns the statements per second the stack ran on its own.
func MeasureBackgroundCallRate(ctx context.Context, meter WorkMeter, idle time.Duration) (float64, error) {
	if err := meter.Reset(ctx); err != nil {
		return 0, err
	}
	timer := time.NewTimer(idle)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
	}
	c, err := meter.Read(ctx)
	if err != nil {
		return 0, err
	}
	return float64(c.Calls) / idle.Seconds(), nil
}

// CheckMeterQuiet fails when the idle background statement rate is above
// maxBackgroundCallsPerSecond. A new poller in the API has to be understood, not
// absorbed into the budgets.
func CheckMeterQuiet(callsPerSecond float64) error {
	if callsPerSecond > maxBackgroundCallsPerSecond {
		return fmt.Errorf("meter is not quiet: %.1f background statements/s while idle, want at most %.1f", callsPerSecond, maxBackgroundCallsPerSecond)
	}
	return nil
}
