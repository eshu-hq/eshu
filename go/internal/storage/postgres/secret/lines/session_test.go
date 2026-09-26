// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/testutil/contentreader"
)

// TestReadyTreatsMissingStateTableAsNotReady covers an API or MCP binary that
// starts before migration 131 has created the readiness table. The side table
// cannot be trusted yet, so callers must use the legacy scan; unrelated
// database failures must still surface.
func TestReadyTreatsMissingStateTableAsNotReady(t *testing.T) {
	t.Parallel()

	missing := fmt.Errorf("readiness query: %w", &pgconn.PgError{
		Code:    "42P01",
		Message: `relation "content_file_secret_lines_state" does not exist`,
	})
	db := contentreader.OpenReaderTestDB(t, []contentreader.ReaderQueryResult{{
		Err:           missing,
		QueryContains: []string{"FROM content_file_secret_lines_state"},
	}})

	ready, err := Ready(context.Background(), db)
	if err != nil || ready {
		t.Fatalf("Ready(migration absent) = %v, %v; want false, nil", ready, err)
	}

	brokenDB := contentreader.OpenReaderTestDB(t, []contentreader.ReaderQueryResult{{
		Err: &pgconn.PgError{Code: "08006", Message: "connection failure"},
	}})
	if _, err := Ready(context.Background(), brokenDB); err == nil {
		t.Fatal("Ready(connection failure) error = nil, want it surfaced")
	}
}
