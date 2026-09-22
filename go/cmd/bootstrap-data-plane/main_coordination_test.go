// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// applyPostgresSchema is the production hop from the environment to
// postgres.ApplyBootstrapWithOptions (#6956). A refused knob must stop the
// run before any statement; an accepted one must reach the migrator.
func TestApplyPostgresSchemaRefusesABadCoordinationKnobBeforeAnyStatement(t *testing.T) {
	t.Parallel()
	db := &fakeBootstrapDB{}
	err := applyPostgresSchema(context.Background(), db, func(key string) string {
		if key == postgres.LockRetryBudgetEnv {
			return "soon"
		}
		return ""
	}, testLogger(t))
	if err == nil || !strings.Contains(err.Error(), postgres.LockRetryBudgetEnv) {
		t.Fatalf("err = %v, want a refusal naming %s", err, postgres.LockRetryBudgetEnv)
	}
	if db.execCalls != 0 {
		t.Fatalf("migrator ran %d statements after a refused knob, want 0", db.execCalls)
	}
}

func TestApplyPostgresSchemaReachesTheMigratorWithAcceptedKnobs(t *testing.T) {
	t.Parallel()
	db := &fakeBootstrapDB{}
	err := applyPostgresSchema(context.Background(), db, func(key string) string {
		switch key {
		case postgres.OwnershipWaitEnv:
			return "2m"
		case postgres.LockRetryBudgetEnv:
			return "45s"
		}
		return ""
	}, testLogger(t))
	if err != nil {
		t.Fatalf("applyPostgresSchema: %v", err)
	}
	// The fake is not a lock-capable SQLDB, so the bootstrap runs the plain
	// definition loop; every bootstrap definition must have been executed.
	if want := len(postgres.BootstrapDefinitions()); db.execCalls != want {
		t.Fatalf("migrator executed %d statements, want %d bootstrap definitions", db.execCalls, want)
	}
}
