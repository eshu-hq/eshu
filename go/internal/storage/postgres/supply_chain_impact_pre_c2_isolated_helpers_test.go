// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openPreC2IsolatedLiveDB is openIsolatedLiveDB for a proof that models a schema from before
// PR-C2 (#10880): it reads or plants finding facts in fact_records, which the write-stop
// trigger (migration 358) refuses, so the trigger is disabled for the whole test and re-armed
// (and checked) at cleanup. Named in the shrink-only allowlist in
// internal/testutil/postgresproof/finding_write_stop_allowlist_test.go.
func openPreC2IsolatedLiveDB(t *testing.T, prefix, skipMessage string) (*sql.DB, context.Context) {
	t.Helper()
	db, ctx := openIsolatedLiveDB(t, prefix, skipMessage)
	postgresproof.DisarmFindingWriteStopForTest(t, db)
	return db, ctx
}
