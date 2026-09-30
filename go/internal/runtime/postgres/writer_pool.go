// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/boundederr"
)

// openWriterPool opens the writer pool from an already validated endpoint. The
// pool's driver errors are bounded at the database/sql boundary (#7253): handlers
// write err.Error() into 5xx bodies, and pgx formats a connection failure with
// the database user, the database, and the dialed address. The error text is one
// fixed string per failure class, the driver error stays behind Unwrap for
// errors.Is and errors.As, and the detail is logged once on logger as
// postgres.store.error. The reader pool does not need this: its errors already
// leave the package as privateError. A nil logger falls back to slog.Default.
func openWriterPool(cfg *pgx.ConnConfig, logger *slog.Logger) *sql.DB {
	return sql.OpenDB(boundederr.NewConnector(stdlib.GetConnector(*cfg), boundederr.WithLogger(logger)))
}
