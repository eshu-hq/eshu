// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// queryErrDB fails every query with err.
type queryErrDB struct{ err error }

func (q queryErrDB) QueryContext(context.Context, string, ...any) (db.Rows, error) { return nil, q.err }

func (q queryErrDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, q.err
}

// TestBackfillCompleteTreatsMissingTableAsNotReady covers the deploy window
// where a new API binary runs before bootstrap applied migration 109: the
// marker table does not exist yet. That is "read model not installed", so
// readers must stay on the graph, not return 500s. Any other error still
// fails, so a broken database is never mistaken for "not ready".
func TestBackfillCompleteTreatsMissingTableAsNotReady(t *testing.T) {
	t.Parallel()

	missing := fmt.Errorf("query: %w", &pgconn.PgError{Code: "42P01", Message: `relation "infra_resource_entity_backfill_markers" does not exist`})
	complete, err := BackfillComplete(context.Background(), queryErrDB{err: missing})
	if err != nil || complete {
		t.Fatalf("BackfillComplete(missing table) = %v, %v; want false, nil", complete, err)
	}

	broken := &pgconn.PgError{Code: "08006", Message: "connection failure"}
	if _, err := BackfillComplete(context.Background(), queryErrDB{err: broken}); err == nil {
		t.Fatal("BackfillComplete(connection failure) error = nil, want it surfaced")
	}
}
