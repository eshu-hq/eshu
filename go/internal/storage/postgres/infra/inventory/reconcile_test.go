// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestReconcileDigestHashesExactlyTheDerivedValues pins the digest to the
// derive: the content side must hash each dimension with the same
// normalization the insert applies, and the table side must hash the columns
// that insert writes. A dimension added to one and not the other would make
// every repository look drifted, or hide real drift in that column.
func TestReconcileDigestHashesExactlyTheDerivedValues(t *testing.T) {
	for _, dim := range reconcileDimensions {
		expr := "COALESCE(btrim(ce.metadata->>'" + dim + "'), '')"
		if !strings.Contains(insertSelectColumns, expr) {
			t.Fatalf("derive insert does not normalize %q as %s", dim, expr)
		}
		if !strings.Contains(reconcileDigestSQL, expr) {
			t.Fatalf("content digest does not hash %q as %s", dim, expr)
		}
		if !strings.Contains(insertColumns, dim) {
			t.Fatalf("table has no %q column", dim)
		}
	}
	if got, want := strings.Count(insertSelectColumns, "COALESCE(btrim(ce.metadata->>"), len(reconcileDimensions); got != want {
		t.Fatalf("derive normalizes %d dimensions, digest covers %d", got, want)
	}
	for _, column := range []string{"ce.entity_id", "ce.relative_path", "ce.entity_type", "ce.entity_name"} {
		if !strings.Contains(reconcileDigestSQL, column) {
			t.Fatalf("content digest does not hash %s", column)
		}
	}
}

// TestReconcileCycleIsNotReadyWithoutTheReadModel covers a reducer that runs
// before migration 109 or before the backfill marker: nothing reads the table
// yet, so the cycle reports not ready and touches nothing, instead of failing
// every cycle on undefined_table.
func TestReconcileCycleIsNotReadyWithoutTheReadModel(t *testing.T) {
	t.Parallel()

	missing := fmt.Errorf("query: %w", &pgconn.PgError{Code: "42P01", Message: "relation does not exist"})
	batch, err := ReconcileCycle(context.Background(), queryErrDB{err: missing}, ReconcileRequest{Budget: 10})
	if err != nil {
		t.Fatalf("ReconcileCycle(not installed) error = %v, want nil", err)
	}
	if batch.Ready || len(batch.Repos) != 0 {
		t.Fatalf("ReconcileCycle(not installed) = %+v, want not ready and empty", batch)
	}
}
