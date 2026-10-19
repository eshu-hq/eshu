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
		case postgres.LockTimeoutEnv:
			return "30s"
		}
		return ""
	}, testLogger(t))
	if err != nil {
		t.Fatalf("applyPostgresSchema: %v", err)
	}
	// The fake is not a lock-capable SQLDB, so the bootstrap runs the plain
	// definition loop. A definition may take multiple ordered Exec calls.
	assertPostgresBootstrapSQLMatchesDefinitions(t, db.execs, postgres.BootstrapDefinitions())
}

func assertPostgresBootstrapSQLMatchesDefinitions(t *testing.T, executed []fakeBootstrapCall, definitions []postgres.Definition) {
	t.Helper()
	call := 0
	for _, definition := range definitions {
		var combined strings.Builder
		for combined.Len() < len(definition.SQL) {
			if call == len(executed) {
				t.Fatalf("missing SQL for definition %q after %d calls", definition.Name, call)
			}
			if executed[call].query == "" || len(executed[call].args) != 0 {
				t.Fatalf("invalid SQL call %d for definition %q", call, definition.Name)
			}
			combined.WriteString(executed[call].query)
			if !strings.HasPrefix(definition.SQL, combined.String()) {
				t.Fatalf("SQL at call %d differs from definition %q", call, definition.Name)
			}
			call++
		}
	}
	if call != len(executed) {
		t.Fatalf("%d extra SQL calls after the last definition", len(executed)-call)
	}
}
