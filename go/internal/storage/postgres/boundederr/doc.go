// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package boundederr wraps a Postgres driver.Connector so the pool built from it
// returns driver errors that carry only a fixed, bounded message.
//
// The pgx driver formats a connection failure as "failed to connect to
// `user=<user> database=<db>`: <host:port>: <dial error>" and a server error as
// "<SEVERITY>: <message> (SQLSTATE <code>)", where the message can name a
// relation, a column, a constraint, or echo a bound value. Query handlers write
// err.Error() into 5xx response bodies, so an unbounded driver error reaches the
// client (#7253). A pool built from NewConnector replaces that value at the
// driver seam with an *Error: the text is one fixed string per failure class, Unwrap keeps
// the original cause so errors.Is and errors.As (pgconn.PgError,
// pgconn.ConnectError, context deadlines, driver.ErrBadConn) classify exactly as
// before, and one operator log record per failure carries the driver detail
// the client no longer sees.
//
// The wrapper sits at database/sql's driver boundary, below every store that
// takes a *sql.DB, so a store needs no per-site change. It forwards every
// optional driver interface pgx implements, and passes through the sentinels
// database/sql branches on (io.EOF, driver.ErrBadConn, driver.ErrSkip,
// driver.ErrRemoveArgument) unchanged.
package boundederr
