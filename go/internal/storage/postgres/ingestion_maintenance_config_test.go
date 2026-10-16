// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func TestIngestionMaintenanceConstructorPreservesDefaultsAndOverrides(t *testing.T) {
	defaultStore := NewIngestionStore(nil)
	configuredDefault := NewIngestionStoreWithMaintenanceConfig(nil, 0, 0)
	if configuredDefault.maintenanceBatchSize != defaultStore.maintenanceBatchSize ||
		configuredDefault.maintenanceWorkers != defaultStore.maintenanceWorkers {
		t.Fatalf("zero settings changed defaults: batch=%d/%d workers=%d/%d",
			configuredDefault.maintenanceBatchSize, defaultStore.maintenanceBatchSize,
			configuredDefault.maintenanceWorkers, defaultStore.maintenanceWorkers)
	}
	if configuredDefault.catalogCache == nil || configuredDefault.idleMaintenanceLogGate == nil {
		t.Fatal("configured constructor omitted shared cache or idle log gate")
	}
	overridden := NewIngestionStoreWithMaintenanceConfig(nil, 1, 2)
	if overridden.maintenanceBatchSize != 1 || overridden.maintenanceWorkers != 2 {
		t.Fatalf("overrides = (%d,%d), want (1,2)", overridden.maintenanceBatchSize, overridden.maintenanceWorkers)
	}
	if overridden.backfillStore().BatchSize != 1 || overridden.backfillStore().Workers != 2 {
		t.Fatal("backfill engine did not receive both overrides")
	}
	if defaultStore.maintenanceWorkers != backfill.DeferredBackfillWorkerCount() {
		t.Fatal("default worker count changed")
	}
}

// targetedWiringProbe proves the public root constructor supplies both the
// query handle and transaction beginner to the child without a database.
type targetedWiringProbe struct{ queries, begins int }

func (p *targetedWiringProbe) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	p.queries++
	return nil, errors.New("wiring probe read")
}

func (*targetedWiringProbe) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected wiring probe exec")
}

func (p *targetedWiringProbe) Begin(context.Context) (db.Transaction, error) {
	p.begins++
	return nil, errors.New("unexpected wiring probe begin")
}

func TestIngestionTargetedStoreProductionWiringPassesValidation(t *testing.T) {
	probe := &targetedWiringProbe{}
	store := NewIngestionStore(probe)
	child := store.targetedStore()
	if child.Backfill.DB != probe || child.Backfill.Beginner != probe ||
		child.PhaseCommitted == nil || child.GateReopen == nil || child.ReopenSucceeded == nil ||
		len(child.CorrelationDomains) == 0 || child.Queries.DeploymentMapping == "" ||
		child.Queries.CodeImportRepoEdge == "" || child.Queries.Correlation == "" {
		t.Fatal("root targeted store omitted a required collaborator")
	}
	_, err := store.RunDeferredRelationshipMaintenanceForPartitions(context.Background(), nil, nil,
		[]OwedPartition{{ScopeID: "scope-a", GenerationID: "gen-a"}})
	if err == nil || !strings.Contains(err.Error(), `read active generation for owed scope "scope-a": wiring probe read`) {
		t.Fatalf("root entrypoint error = %v, want first database read error", err)
	}
	if probe.queries != 1 || probe.begins != 0 {
		t.Fatalf("root wiring queries=%d begins=%d, want one read and no begin", probe.queries, probe.begins)
	}
}
