// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	infraInventoryReconcileEnabledEnv    = "ESHU_INFRA_INVENTORY_RECONCILE_ENABLED"
	infraInventoryReconcileIntervalEnv   = "ESHU_INFRA_INVENTORY_RECONCILE_INTERVAL"
	infraInventoryReconcileRepoBudgetEnv = "ESHU_INFRA_INVENTORY_RECONCILE_REPO_BUDGET"

	defaultInfraInventoryReconcileInterval   = 5 * time.Minute
	defaultInfraInventoryReconcileRepoBudget = 500
)

type infraInventoryReconcileConfig struct {
	Enabled bool
	Runner  maintenance.InfraInventoryReconcileRunnerConfig
}

// loadInfraInventoryReconcileConfig reads the reconcile knobs. An unparsable
// or non-positive value uses the default, as the other reducer maintenance
// knobs do.
func loadInfraInventoryReconcileConfig(getenv func(string) string) infraInventoryReconcileConfig {
	return infraInventoryReconcileConfig{
		Enabled: loadBoolOrDefault(getenv, infraInventoryReconcileEnabledEnv, true),
		Runner: maintenance.InfraInventoryReconcileRunnerConfig{
			PollInterval: loadDurationOrDefault(getenv, infraInventoryReconcileIntervalEnv, defaultInfraInventoryReconcileInterval),
			RepoBudget:   loadPositiveIntOrDefault(getenv, infraInventoryReconcileRepoBudgetEnv, defaultInfraInventoryReconcileRepoBudget),
		},
	}
}

// infraInventoryReconcileRunnerFor builds the infra read model reconcile
// runner (#6793), or nil when ESHU_INFRA_INVENTORY_RECONCILE_ENABLED=false.
func infraInventoryReconcileRunnerFor(
	getenv func(string) string,
	database db.ExecQueryer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) *maintenance.InfraInventoryReconcileRunner {
	cfg := loadInfraInventoryReconcileConfig(getenv)
	if !cfg.Enabled {
		return nil
	}
	return &maintenance.InfraInventoryReconcileRunner{
		Reconciler:  postgresInfraInventoryReconciler{database: database},
		Config:      cfg.Runner,
		Tracer:      tracer,
		Instruments: instruments,
		Logger:      logger,
	}
}

// postgresInfraInventoryReconciler adapts inventory.ReconcileCycle to the
// maintenance runner's contract.
type postgresInfraInventoryReconciler struct {
	database db.ExecQueryer
}

func (r postgresInfraInventoryReconciler) ReconcileInfraInventory(
	ctx context.Context, req maintenance.InfraInventoryReconcileRequest,
) (maintenance.InfraInventoryReconcileBatch, error) {
	batch, err := inventory.ReconcileCycle(ctx, r.database, inventory.ReconcileRequest{
		Cursor:   req.Cursor,
		Budget:   req.Budget,
		Suspects: req.Suspects,
		Persist:  req.Persist,
	})
	if err != nil {
		return maintenance.InfraInventoryReconcileBatch{}, err
	}
	out := maintenance.InfraInventoryReconcileBatch{
		Ready:          batch.Ready,
		NextCursor:     batch.NextCursor,
		DirtyRepos:     batch.DirtyRepos,
		DirtyOldestAge: batch.DirtyOldestAge,
		Repos:          make([]maintenance.InfraInventoryReconcileRepo, 0, len(batch.Repos)),
	}
	for _, repo := range batch.Repos {
		out.Repos = append(out.Repos, maintenance.InfraInventoryReconcileRepo{
			RepoID:      repo.RepoID,
			Outcome:     repo.Outcome,
			ContentRows: repo.ContentRows,
			TableRows:   repo.TableRows,
			Duration:    repo.Duration,
			Err:         repo.Err,
		})
	}
	return out, nil
}
