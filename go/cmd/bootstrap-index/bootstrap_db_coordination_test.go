// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// countingBootstrapDB is the smallest bootstrapDB that records statements;
// it is not a lock-capable SQLDB, so the bootstrap runs the plain
// definition loop through it.
type countingBootstrapDB struct {
	execs []string
}

func (c *countingBootstrapDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	c.execs = append(c.execs, query)
	return nil, nil
}

func (c *countingBootstrapDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, nil
}

func (c *countingBootstrapDB) Close() error { return nil }

// bootstrap-index honors the same #6956 knobs as db-migrate: a refused
// value stops before any statement, an accepted one reaches the migrator.
func TestApplySchemaFromEnvRefusesABadKnobBeforeAnyStatement(t *testing.T) {
	t.Parallel()
	database := &countingBootstrapDB{}
	err := applySchemaFromEnv(func(key string) string {
		if key == postgres.OwnershipWaitEnv {
			return "0s"
		}
		return ""
	})(context.Background(), database, slog.Default())
	if err == nil || !strings.Contains(err.Error(), postgres.OwnershipWaitEnv) {
		t.Fatalf("err = %v, want a refusal naming %s", err, postgres.OwnershipWaitEnv)
	}
	if len(database.execs) != 0 {
		t.Fatalf("migrator ran %d statements after a refused knob, want 0", len(database.execs))
	}
}

func TestApplySchemaFromEnvDefersContentSearchIndexes(t *testing.T) {
	t.Parallel()
	database := &countingBootstrapDB{}
	if err := applySchemaFromEnv(func(string) string { return "" })(context.Background(), database, slog.Default()); err != nil {
		t.Fatalf("applySchemaFromEnv: %v", err)
	}
	assertBootstrapSQLMatchesDefinitions(t, database.execs, postgres.BootstrapDefinitionsWithoutContentSearchIndexes())
}

// Each definition may use multiple Exec calls, but their ordered SQL must
// reconstruct the selected definition byte for byte.
func assertBootstrapSQLMatchesDefinitions(t *testing.T, executed []string, definitions []postgres.Definition) {
	t.Helper()
	call := 0
	for _, definition := range definitions {
		var combined strings.Builder
		for combined.Len() < len(definition.SQL) {
			if call == len(executed) {
				t.Fatalf("missing SQL for definition %q after %d calls", definition.Name, call)
			}
			if executed[call] == "" {
				t.Fatalf("empty SQL at call %d for definition %q", call, definition.Name)
			}
			combined.WriteString(executed[call])
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

// Non-default knobs and the runtime logger reach the migrator's options.
func TestSchemaOptionsFromEnvThreadsKnobsAndLogger(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	options, err := schemaOptionsFromEnv(func(key string) string {
		switch key {
		case postgres.OwnershipWaitEnv:
			return "2m"
		case postgres.LockRetryBudgetEnv:
			return "45s"
		case postgres.LockTimeoutEnv:
			return "30s"
		}
		return ""
	}, logger)
	if err != nil {
		t.Fatalf("schemaOptionsFromEnv: %v", err)
	}
	if options.OwnershipWait != 2*time.Minute || options.LockRetryBudget != 45*time.Second || options.LockTimeout != 30*time.Second || !options.DeferContentSearchIndexes || options.Logger != logger {
		t.Fatalf("options = %+v, want 2m / 45s / 30s / deferred indexes / the given logger", options)
	}
}
