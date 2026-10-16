// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
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
