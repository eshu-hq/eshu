// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwrite

import (
	"context"
	"database/sql"
)

// Execer is the minimal database surface the batch writers need: a single
// ExecContext. It is an interface rather than a concrete handle so a caller can
// pass a pool, a connection, or a transaction, and so tests can substitute a
// recorder without a live database.
type Execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}
