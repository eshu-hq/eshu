// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// The openers of the proofs that model a schema from before PR-C2 (#10880): the old-writer
// fence, the backfill, drain, verification and key fill interleavings, and the
// differentials against the frozen fact statements. They read or plant finding facts in
// fact_records on purpose, which the write-stop trigger (migration 358) refuses, so the
// trigger is disabled for the whole test and re-armed (and checked) at cleanup. A proof of
// C2 code seeds with postgresproof.SeedFindingFacts instead and runs armed.
// Each helper is named in the shrink-only allowlist in
// internal/testutil/postgresproof/finding_write_stop_allowlist_test.go.

// openPreC2ReplaceSetLiveDB is openReplaceSetLiveDB for a pre-C2 proof.
func openPreC2ReplaceSetLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, db := openReplaceSetLiveDB(t)
	postgresproof.DisarmFindingWriteStopForTest(t, db)
	return ctx, db
}
