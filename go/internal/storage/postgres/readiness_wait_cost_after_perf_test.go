// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf6785_wait

package postgres

import (
	"database/sql"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/readinesswait"
)

// configureWaitHandler wires the commit-first ledger for the "after" run of
// the R2-F3 cost harness. The "before" run, at the pre-ledger commit, uses a
// copy that leaves the handler unchanged and returns "before".
func configureWaitHandler(handler *iamcan.IAMCanPerformMaterializationHandler, sqlDB *sql.DB, now func() time.Time) string {
	handler.ReadinessWaits = readinesswait.Store{DB: SQLDB{DB: sqlDB}}
	handler.Now = now
	return "after"
}
