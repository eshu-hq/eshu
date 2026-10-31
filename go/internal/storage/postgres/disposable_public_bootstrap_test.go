// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openDisposablePublicBootstrapDB creates a fresh disposable database,
// applies apply on its public schema, and returns the open handle. The
// database is dropped when the test ends.
//
// Proofs whose definition sets must install the lane's database-global
// authority migrations (241a/241b carry a public-schema-only SQL guard)
// cannot bootstrap an isolated schema: the guard fails closed there, while
// the isolated-schema skip in ApplyBootstrapWithOptions drops the claim
// columns later migrations and lane queries require. For those proofs the
// isolation unit is a database, not a schema.
//
// An empty dsn skips with skipMessage. An opt-in other than "1" skips as
// well: creating databases stays behind the explicit disposable opt-in, and
// a proof that cannot isolate itself must not run half-isolated.
func openDisposablePublicBootstrapDB(
	t *testing.T,
	dsn, optIn, skipMessage string,
	apply func(context.Context, *sql.DB) error,
) *sql.DB {
	t.Helper()
	if dsn == "" {
		t.Skip(skipMessage)
	}
	if optIn != "1" {
		t.Skip("set ESHU_POSTGRES_DISPOSABLE=1 to run this proof on a disposable PostgreSQL database (absent opt-in skips, never fails)")
	}
	_, database := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, optIn, 5*time.Minute, apply)
	return database
}
