// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// refusingDatabase fails the test on any statement: gate G14 requires that
// the changed-since link domain issue no SQL while its switch is off.
type refusingDatabase struct{ t *testing.T }

func (d refusingDatabase) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	d.t.Fatalf("switch off, but the changed-since link domain queried: %s", query)
	return nil, nil
}

func (d refusingDatabase) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	d.t.Fatalf("switch off, but the changed-since link domain executed: %s", query)
	return nil, nil
}

func (d refusingDatabase) Begin(context.Context) (db.Transaction, error) {
	d.t.Fatal("switch off, but the changed-since link domain began a transaction")
	return nil, nil
}

func envMap(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestChangedSinceLinkRunnerIsOffByDefault(t *testing.T) {
	if runner := changedSinceLinkRunnerFor(envMap(nil), refusingDatabase{t}, nil, nil, nil); runner != nil {
		t.Fatalf("changed-since link runner built with no environment; the switch must default off")
	}
	off := envMap(map[string]string{changedSinceLinkEnabledEnv: "false"})
	if runner := changedSinceLinkRunnerFor(off, refusingDatabase{t}, nil, nil, nil); runner != nil {
		t.Fatalf("changed-since link runner built with the switch off")
	}
}

func TestChangedSinceLinkConfigReadsEveryKnob(t *testing.T) {
	cfg := loadChangedSinceLinkConfig(envMap(map[string]string{
		changedSinceLinkEnabledEnv:          "true",
		changedSinceLinkSlotsEnv:            "3",
		changedSinceLinkStatementTimeoutEnv: "45s",
		changedSinceLinkPollIntervalEnv:     "10s",
		changedSinceLinkWorkersEnv:          "6",
		changedSinceLinkBackfillScopesEnv:   "25",
	}))
	if !cfg.Enabled || cfg.Slots != 3 || cfg.StatementTimeout != 45*time.Second ||
		cfg.Runner.PollInterval != 10*time.Second || cfg.Runner.Workers != 6 || cfg.Runner.BackfillScopesPerCycle != 25 {
		t.Fatalf("config = %+v", cfg)
	}
	defaults := loadChangedSinceLinkConfig(envMap(nil))
	if defaults.Enabled || defaults.Slots != 2 || defaults.StatementTimeout != 120*time.Second {
		t.Fatalf("defaults = %+v, want off, 2 slots, 120s", defaults)
	}
}

func TestChangedSinceLinkRunnerBuiltWhenEnabled(t *testing.T) {
	on := envMap(map[string]string{changedSinceLinkEnabledEnv: "true"})
	runner := changedSinceLinkRunnerFor(on, refusingDatabase{t}, nil, nil, nil)
	if runner == nil || runner.Linker == nil || runner.Journal == nil {
		t.Fatalf("changed-since link runner = %+v, want a wired runner", runner)
	}
}
