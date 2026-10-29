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

// openIsolatedBootstrapSchema applies the real bootstrap migrations to a
// fresh disposable database (#9299: the lane's public-qualified SECURITY
// DEFINER bodies cannot install into an isolated schema, so the isolation
// unit is a database, not a schema) and returns its pool. The database is
// dropped when the test ends. The dsn names the administrative postgres
// database and optIn carries that family's explicit disposable opt-in: both
// are passed through to postgresproof.OpenDisposableBootstrapDatabase, which
// skips on an absent DSN. Callers resolve both from their own family
// variables; nothing here derives them.
//
// A live proof that drives a queue Claim or an all-scopes recovery must use it
// (#7479). Both act on every matching row in the database, so on a shared,
// long-lived database they pick up rows other tests or earlier runs left behind:
// the proof then claims, acks or reopens the wrong work item, and whether it
// passes depends on how much history the database holds.
//
// The name is kept for familiarity; call sites pass their family's
// explicit opt-in alongside the DSN they already passed.
func openIsolatedBootstrapSchema(t *testing.T, dsn, optIn, prefix string) *sql.DB {
	t.Helper()
	_, database := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, optIn, 5*time.Minute, func(ctx context.Context, database *sql.DB) error {
		if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
			return fmt.Errorf("apply bootstrap schema: %w", err)
		}
		return nil
	})
	return database
}

// openIsolatedLiveDB is openIsolatedBootstrapSchema for the DSN in
// ESHU_POSTGRES_DSN with the opt-in in ESHU_POSTGRES_DISPOSABLE, skipping
// with skipMessage when the DSN is unset and skipping (never failing) when
// the opt-in is absent, plus the 90-second proof deadline the live helpers
// share.
func openIsolatedLiveDB(t *testing.T, prefix, skipMessage string) (*sql.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip(skipMessage)
	}
	optIn := os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	if optIn != "1" {
		t.Skip("set ESHU_POSTGRES_DISPOSABLE=1 to run this proof on a disposable PostgreSQL database (absent opt-in skips, never fails)")
	}
	database := openIsolatedBootstrapSchema(t, dsn, optIn, prefix)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return database, ctx
}
