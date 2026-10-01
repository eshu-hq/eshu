// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundederr

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Operation names the driver call a bounded error came from. The set is closed
// so the log field stays low-cardinality.
const (
	opConnect  = "connect"
	opPrepare  = "prepare"
	opQuery    = "query"
	opExec     = "exec"
	opRows     = "rows"
	opBegin    = "begin"
	opCommit   = "commit"
	opRollback = "rollback"
	opPing     = "ping"
	opReset    = "reset_session"
	opClose    = "close"
)

const (
	// maxStatementHead bounds the statement text carried on the log record.
	maxStatementHead = 160
	// maxErrorText bounds the driver text carried on the log record.
	maxErrorText = 1024
)

// observer replaces a driver error with a bounded *Error and emits the operator
// record that keeps the detail.
type observer struct {
	logger *slog.Logger
}

// bound returns err unchanged when it is a database/sql sentinel, nil, or
// already bounded; otherwise it returns an *Error that wraps err and logs the
// detail once. statement is the SQL text when the call has one; Eshu's store
// statements use $N placeholders, never inline values.
func (o *observer) bound(ctx context.Context, operation, statement string, err error) error {
	if passthrough(err) {
		return err
	}
	bounded := o.newError(err)
	o.log(ctx, operation, statement, bounded)
	return bounded
}

// newError classifies err and wraps it without logging.
func (o *observer) newError(err error) *Error {
	return &Error{kind: classify(err), cause: err}
}

// log emits postgres.store.error. A caller-canceled request is not a fault and
// logs nothing. A timeout, and a data or integrity error a request can trigger
// (SQLSTATE class 22 or 23), log at WARN so a client cannot raise an ERROR
// stream; everything else is ERROR.
func (o *observer) log(ctx context.Context, operation, statement string, err *Error) {
	if err.kind == KindCanceled {
		return
	}
	level := slog.LevelError
	sqlState := ""
	var pgErr *pgconn.PgError
	if errors.As(err.cause, &pgErr) {
		sqlState = pgErr.Code
		if strings.HasPrefix(sqlState, "22") || strings.HasPrefix(sqlState, "23") {
			level = slog.LevelWarn
		}
	}
	if err.kind == KindTimeout {
		level = slog.LevelWarn
	}
	o.logger.LogAttrs(ctx, level, "postgres store call failed",
		telemetry.EventAttr(telemetry.EventPostgresStoreError),
		telemetry.FailureClassAttr(string(err.kind)),
		slog.String(telemetry.LogKeyPostgresStoreOperation, operation),
		slog.String(telemetry.LogKeyPostgresStoreSQLState, sqlState),
		slog.String(telemetry.LogKeyPostgresStoreStatementHead, statementHead(statement)),
		slog.String(telemetry.LogKeyPostgresStoreError, truncate(err.cause.Error(), maxErrorText)),
	)
}

// statementHead collapses whitespace and truncates the SQL text so a record
// names the statement shape without carrying a multi-page query.
func statementHead(statement string) string {
	return truncate(strings.Join(strings.Fields(statement), " "), maxStatementHead)
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !isRuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "...(truncated)"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
