// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// SessionSetting is the per-session Postgres setting that migration 131's
// content_files triggers consult in their WHEN clause.
const SessionSetting = "eshu.secret_lines_derive"

// deferredValue is the setting value that makes the triggers skip a write.
const deferredValue = "deferred"

// DeferredSessionSQL marks a connection as a bulk-load writer: migration 131's
// content_files triggers skip that connection's writes, and the caller owns
// rebuilding content_file_secret_lines afterwards (BeginDeferral before the
// first write, Finalize after the last). Only bootstrap-index runs it, and not
// through a transaction-mode pooler. A pooler affects the setting in two
// directions. One that drops or never forwards session state leaves that
// session deriving, which is correct and only slower. One that does not reset
// session settings between clients can leave the setting on a server connection
// later handed to another binary, whose writes then skip derivation while the
// state is ready: findings are silently missing.
const DeferredSessionSQL = "SET " + SessionSetting + " = '" + deferredValue + "'"

// Bounded lifecycle states of content_file_secret_lines_state.
const (
	// StateNotBuilt means a deferred bulk load began and the side table may be
	// missing findings.
	StateNotBuilt = "not_built"
	// StateBuilding means a finalizer holds the current epoch.
	StateBuilding = "building"
	// StateReady means every content_files row's findings are in the side
	// table; readers use it.
	StateReady = "ready"
	// StateFailed means the finalizer for the current epoch failed; readers stay
	// on the legacy scan until a later bulk load or finalizer publishes ready.
	StateFailed = "failed"
)

const readySQL = `
SELECT EXISTS (
    SELECT 1 FROM content_file_secret_lines_state
    WHERE singleton = TRUE AND state = 'ready'
)`

// undefinedTableSQLState is Postgres' undefined_table error code.
const undefinedTableSQLState = "42P01"

// RowQueryer is the single-row read Ready needs; *sql.DB and *sql.Tx satisfy it.
type RowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Ready reports whether content_file_secret_lines is published as complete.
// The investigation read serves from the side table only when it is true and
// otherwise runs the legacy content scan, so a bulk load never yields a silent
// partial answer. The statement is one primary-key row lookup. When migration
// 131 has not created the readiness table yet, it reports false without an
// error so a new API or MCP binary uses the legacy scan until migration runs.
func Ready(ctx context.Context, queryer RowQueryer) (bool, error) {
	var ready bool
	if err := queryer.QueryRowContext(ctx, readySQL).Scan(&ready); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == undefinedTableSQLState {
			return false, nil
		}
		return false, fmt.Errorf("read secret lines readiness: %w", err)
	}
	return ready, nil
}
