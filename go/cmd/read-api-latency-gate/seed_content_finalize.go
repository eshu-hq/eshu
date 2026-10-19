// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// ensureContentSearchIndexesForGate is the seam the finalization tests stub:
// production runs the storage lifecycle finalizer, tests record the call
// without a live database.
var ensureContentSearchIndexesForGate = func(ctx context.Context, beginner db.Beginner) error {
	return pgstorage.EnsureContentSearchIndexes(ctx, beginner)
}

// finalizeContentSearchIndexes builds the deferred content trigram indexes
// after the gate's bulk content drain completes. The gate bootstrap defers
// them (ESHU_DEFER_CONTENT_SEARCH_INDEXES=true), which is the documented
// bulk-load contract: the loader calls EnsureContentSearchIndexes once the
// initial write-heavy drain is done. Without this call the readiness probe
// below waits on a fence nothing clears and the seed always times out.
// *sql.DB satisfies db.Beginner through database/sql.
func finalizeContentSearchIndexes(ctx context.Context, dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open postgres for content search index finalization: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := ensureContentSearchIndexesForGate(ctx, pgstorage.SQLDB{DB: sqlDB}); err != nil {
		return fmt.Errorf("finalize content search indexes: %w", err)
	}
	return nil
}
