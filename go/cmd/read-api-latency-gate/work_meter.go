// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
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
	Calls int64
	Rows  int64
	Blks  int64
}

// WorkMeter measures Postgres work between a Reset and a Read. It is an
// interface so the hermetic RED/GREEN tests can inject a fake; the pgx
// implementation is the only production one. A meter error is always a gate
// error — the caller never skips the work assertion and continues on latency.
type WorkMeter interface {
	Reset(ctx context.Context) error
	Read(ctx context.Context) (WorkCounters, error)
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

type pgxWorkMeter struct {
	pool *pgxpool.Pool
}

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
