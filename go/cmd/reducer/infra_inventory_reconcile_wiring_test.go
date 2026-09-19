// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

func TestLoadInfraInventoryReconcileConfigDefaults(t *testing.T) {
	cfg := loadInfraInventoryReconcileConfig(func(string) string { return "" })
	if !cfg.Enabled {
		t.Fatal("Enabled = false, want the reconcile on by default")
	}
	if cfg.Runner.PollInterval != 5*time.Minute || cfg.Runner.RepoBudget != 500 {
		t.Fatalf("defaults = %+v, want 5m interval and 500 repositories", cfg.Runner)
	}
}

func TestLoadInfraInventoryReconcileConfigOverridesAndInvalidValues(t *testing.T) {
	env := map[string]string{
		infraInventoryReconcileEnabledEnv:    "false",
		infraInventoryReconcileIntervalEnv:   "90s",
		infraInventoryReconcileRepoBudgetEnv: "25",
	}
	cfg := loadInfraInventoryReconcileConfig(func(key string) string { return env[key] })
	if cfg.Enabled || cfg.Runner.PollInterval != 90*time.Second || cfg.Runner.RepoBudget != 25 {
		t.Fatalf("overrides = %+v, want disabled, 90s, 25", cfg)
	}

	env[infraInventoryReconcileIntervalEnv] = "-1s"
	env[infraInventoryReconcileRepoBudgetEnv] = "zero"
	cfg = loadInfraInventoryReconcileConfig(func(key string) string { return env[key] })
	if cfg.Runner.PollInterval != 5*time.Minute || cfg.Runner.RepoBudget != 500 {
		t.Fatalf("invalid values = %+v, want the defaults", cfg.Runner)
	}
}

func TestInfraInventoryReconcileRunnerFor(t *testing.T) {
	disabled := infraInventoryReconcileRunnerFor(func(key string) string {
		if key == infraInventoryReconcileEnabledEnv {
			return "false"
		}
		return ""
	}, &fakeReducerDB{}, nil, nil, nil)
	if disabled != nil {
		t.Fatal("runner = non-nil, want nil when disabled")
	}

	enabled := infraInventoryReconcileRunnerFor(func(string) string { return "" }, &fakeReducerDB{}, nil, nil, nil)
	if enabled == nil || enabled.Reconciler == nil {
		t.Fatal("runner or reconciler = nil, want the Postgres reconciler wired by default")
	}
	if enabled.Config.RepoBudget != 500 {
		t.Fatalf("RepoBudget = %d, want 500", enabled.Config.RepoBudget)
	}
}

// TestPostgresInfraInventoryReconcilerCarriesTheWalkWrap drives the reducer
// adapter over a ready read model whose walk claims an empty page, so
// inventory.ReconcileCycle reports a wrapped walk. The adapter must hand
// Wrapped and the fence fields to the maintenance runner unchanged.
func TestPostgresInfraInventoryReconcilerCarriesTheWalkWrap(t *testing.T) {
	t.Parallel()

	reconciler := postgresInfraInventoryReconciler{database: &readyEmptyInventoryDB{dirtyRepos: 3}}
	batch, err := reconciler.ReconcileInfraInventory(context.Background(), maintenance.InfraInventoryReconcileRequest{
		Budget:  10,
		Persist: true,
	})
	if err != nil {
		t.Fatalf("ReconcileInfraInventory() error = %v", err)
	}
	if !batch.Ready || !batch.Wrapped || batch.NextCursor != "" || batch.DirtyRepos != 3 {
		t.Fatalf("batch = %+v, want ready, wrapped, empty cursor, 3 dirty repositories", batch)
	}
}

// readyEmptyInventoryDB answers the fence-state read with the marker present
// and dirtyRepos fence marks, and every other read with no rows: no dirty
// repository to list, an empty cursor, and an empty walk page.
type readyEmptyInventoryDB struct{ dirtyRepos int64 }

func (f *readyEmptyInventoryDB) QueryContext(_ context.Context, _ string, args ...any) (db.Rows, error) {
	if len(args) == 2 && args[0] == inventory.BackfillMarker {
		return &fenceStateRows{dirtyRepos: f.dirtyRepos}, nil
	}
	return &fenceStateRows{done: true}, nil
}

func (f *readyEmptyInventoryDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeReducerResult{}, nil
}

func (f *readyEmptyInventoryDB) Begin(context.Context) (db.Transaction, error) {
	return readyEmptyInventoryTx{f}, nil
}

type readyEmptyInventoryTx struct{ *readyEmptyInventoryDB }

func (readyEmptyInventoryTx) Commit() error   { return nil }
func (readyEmptyInventoryTx) Rollback() error { return nil }

// fenceStateRows yields one fence-state row (marker present, dirtyRepos
// marks, zero age) unless done is set, then nothing.
type fenceStateRows struct {
	dirtyRepos int64
	done       bool
}

func (r *fenceStateRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *fenceStateRows) Scan(dest ...any) error {
	*dest[0].(*bool) = true
	*dest[1].(*int64) = r.dirtyRepos
	*dest[2].(*float64) = 0
	return nil
}

func (*fenceStateRows) Err() error   { return nil }
func (*fenceStateRows) Close() error { return nil }
