// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openIsolatedBootstrapSchema applies the real bootstrap migrations to a fresh
// schema of the database named by dsn and returns a pool whose every
// connection resolves unqualified names there. The schema is dropped when the
// test ends.
//
// A live proof that drives a queue Claim or an all-scopes recovery must use it
// (#7479). Both act on every matching row in the database, so on a shared,
// long-lived schema they pick up rows other tests or earlier runs left behind:
// the proof then claims, acks or reopens the wrong work item, and whether it
// passes depends on how much history the database holds.
//
// The isolated schema comes first on the search_path. public stays on it so
// the pg_trgm extension, installed there under the package's advisory lock,
// still resolves its operator classes for the bootstrap.
func openIsolatedBootstrapSchema(t *testing.T, dsn, prefix string) *sql.DB {
	t.Helper()
	return postgresproof.OpenIsolatedSchema(t, dsn, prefix, func(ctx context.Context, database *sql.DB) error {
		if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
			return fmt.Errorf("apply bootstrap schema: %w", err)
		}
		return nil
	})
}

// openIsolatedLiveDB is openIsolatedBootstrapSchema for the DSN in
// ESHU_POSTGRES_DSN, skipping with skipMessage when it is unset, plus the
// 90-second proof deadline the live helpers share.
func openIsolatedLiveDB(t *testing.T, prefix, skipMessage string) (*sql.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip(skipMessage)
	}
	database := openIsolatedBootstrapSchema(t, dsn, prefix)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return database, ctx
}
