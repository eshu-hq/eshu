// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// countingBootstrapDB is the smallest bootstrapDB that counts statements;
// it is not a lock-capable SQLDB, so the bootstrap runs the plain
// definition loop through it.
type countingBootstrapDB struct {
	execs int
}

func (c *countingBootstrapDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	c.execs++
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
	})(context.Background(), database)
	if err == nil || !strings.Contains(err.Error(), postgres.OwnershipWaitEnv) {
		t.Fatalf("err = %v, want a refusal naming %s", err, postgres.OwnershipWaitEnv)
	}
	if database.execs != 0 {
		t.Fatalf("migrator ran %d statements after a refused knob, want 0", database.execs)
	}
}

func TestApplySchemaFromEnvDefersContentSearchIndexes(t *testing.T) {
	t.Parallel()
	database := &countingBootstrapDB{}
	if err := applySchemaFromEnv(func(string) string { return "" })(context.Background(), database); err != nil {
		t.Fatalf("applySchemaFromEnv: %v", err)
	}
	if want := len(postgres.BootstrapDefinitionsWithoutContentSearchIndexes()); database.execs != want {
		t.Fatalf("migrator executed %d statements, want %d (content search indexes deferred)", database.execs, want)
	}
}
