// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	statussummary "github.com/eshu-hq/eshu/go/internal/reducer/status/summary"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	snapshots "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// TestStatusSummaryWriterIsOffByDefault proves the writer is not built, so no
// goroutine starts and no SQL runs, unless the switch is on.
func TestStatusSummaryWriterIsOffByDefault(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"unset":    nil,
		"false":    {statusSummaryWriterEnabledEnv: "false"},
		"garbage":  {statusSummaryWriterEnabledEnv: "maybe"},
		"interval": {statusSummaryWriterIntervalEnv: "10s"},
	} {
		t.Run(name, func(t *testing.T) {
			runner, err := statusSummaryWriterFor(envMap(env), refusingDatabase{t}, nil, nil, nil)
			if err != nil || runner != nil {
				t.Fatalf("statusSummaryWriterFor() = %v, %v; want nil, nil with the switch off", runner, err)
			}
		})
	}
}

// TestStatusSummaryWriterBuiltWhenEnabled proves the enabled writer runs the
// storage package's active-work statement and digest at the configured
// interval.
func TestStatusSummaryWriterBuiltWhenEnabled(t *testing.T) {
	on := envMap(map[string]string{
		statusSummaryWriterEnabledEnv:  "true",
		statusSummaryWriterIntervalEnv: "10s",
	})
	runner, err := statusSummaryWriterFor(on, refusingDatabase{t}, nil, nil, nil)
	if err != nil {
		t.Fatalf("statusSummaryWriterFor() error = %v", err)
	}
	if runner == nil || runner.DB == nil || runner.Statement.Compute == nil {
		t.Fatalf("statusSummaryWriterFor() = %+v, want a wired runner", runner)
	}
	if runner.Interval != 10*time.Second {
		t.Fatalf("interval = %s, want 10s", runner.Interval)
	}
	if runner.Statement.ModelKey != snapshots.ModelActiveWorkSummary {
		t.Fatalf("model key = %q, want %q", runner.Statement.ModelKey, snapshots.ModelActiveWorkSummary)
	}
	if runner.Statement.SourceSHA256 != postgres.ActiveWorkSummarySourceSHA256() {
		t.Fatalf("source digest = %q, want the storage statement's digest", runner.Statement.SourceSHA256)
	}
}

// TestStatusSummaryWriterIntervalDefaultsAndFloor proves the 10 s default and
// that a faster or unparsable interval fails reducer startup instead of
// falling back silently.
func TestStatusSummaryWriterIntervalDefaultsAndFloor(t *testing.T) {
	cfg, err := loadStatusSummaryWriterConfig(envMap(map[string]string{statusSummaryWriterEnabledEnv: "true"}))
	if err != nil || !cfg.Enabled || cfg.Interval != 10*time.Second || statussummary.DefaultInterval != 10*time.Second {
		t.Fatalf("defaults = %+v, %v; want enabled at %s", cfg, err, statussummary.DefaultInterval)
	}
	for _, raw := range []string{"2s", "4999ms", "0s", "-5s", "five seconds"} {
		_, err := loadStatusSummaryWriterConfig(envMap(map[string]string{
			statusSummaryWriterEnabledEnv:  "true",
			statusSummaryWriterIntervalEnv: raw,
		}))
		if err == nil || !strings.Contains(err.Error(), statusSummaryWriterIntervalEnv) {
			t.Fatalf("interval %q: error = %v, want a startup error naming %s", raw, err, statusSummaryWriterIntervalEnv)
		}
	}
	cfg, err = loadStatusSummaryWriterConfig(envMap(map[string]string{statusSummaryWriterIntervalEnv: "5s"}))
	if err != nil || cfg.Interval != 5*time.Second {
		t.Fatalf("interval 5s = %+v, %v; want accepted", cfg, err)
	}
}

// nonTransactionalDatabase is an ExecQueryer that cannot open transactions.
type nonTransactionalDatabase struct{}

func (nonTransactionalDatabase) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, nil
}

func (nonTransactionalDatabase) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

// TestStatusSummaryWriterNeedsTransactions proves an enabled writer refuses a
// database that cannot hold the lock and the upsert in one transaction.
func TestStatusSummaryWriterNeedsTransactions(t *testing.T) {
	on := envMap(map[string]string{statusSummaryWriterEnabledEnv: "true"})
	if _, err := statusSummaryWriterFor(on, nonTransactionalDatabase{}, nil, nil, nil); err == nil {
		t.Fatal("statusSummaryWriterFor() accepted a database without transactions")
	}
}

// TestWithStatusSummaryWriterSetsOnlyTheWriter proves the helper that
// buildObservedReducerService calls leaves the writer nil by default, sets it
// when enabled without touching the rest of the service, and turns a bad
// interval into the startup error.
func TestWithStatusSummaryWriterSetsOnlyTheWriter(t *testing.T) {
	base := reducer.Service{PollInterval: 3 * time.Second, Workers: 7}

	off, err := withStatusSummaryWriter(base, envMap(nil), refusingDatabase{t}, nil, nil, nil)
	if err != nil || off.StatusSummaryWriter != nil {
		t.Fatalf("default = writer %v, err %v; want no writer and no error", off.StatusSummaryWriter, err)
	}
	on, err := withStatusSummaryWriter(base, envMap(map[string]string{statusSummaryWriterEnabledEnv: "true"}),
		refusingDatabase{t}, nil, nil, nil)
	if err != nil || on.StatusSummaryWriter == nil {
		t.Fatalf("enabled = writer %v, err %v; want a writer", on.StatusSummaryWriter, err)
	}
	if on.PollInterval != base.PollInterval || on.Workers != base.Workers {
		t.Fatalf("enabled service = %+v; the helper changed fields other than the writer", on)
	}
	if _, err := withStatusSummaryWriter(base, envMap(map[string]string{statusSummaryWriterIntervalEnv: "1s"}),
		refusingDatabase{t}, nil, nil, nil); err == nil {
		t.Fatal("a 1s interval did not fail startup")
	}
}
