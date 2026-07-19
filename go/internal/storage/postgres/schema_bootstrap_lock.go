// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	schemaBootstrapAdvisoryLockClass = 5318
	schemaBootstrapAdvisoryLockID    = 0
)

type schemaConnectionExecutor struct {
	db   SQLDB
	conn *sql.Conn
}

func (executor schemaConnectionExecutor) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return executor.conn.ExecContext(ctx, query, args...)
}

func (executor schemaConnectionExecutor) execContextWithLockTimeout(
	ctx context.Context,
	query string,
	lockTimeout time.Duration,
) (sql.Result, error) {
	if lockTimeout <= 0 {
		return executor.ExecContext(ctx, query)
	}
	if _, err := executor.conn.ExecContext(
		ctx,
		"SELECT set_config('lock_timeout', $1, false)",
		lockTimeout.String(),
	); err != nil {
		return nil, fmt.Errorf("set schema lock timeout: %w", err)
	}
	if err := executor.db.dropInvalidConcurrentIndexes(
		ctx,
		executor.conn,
		concurrentIndexNamesForInvalidCleanup(query),
	); err != nil {
		return nil, errors.Join(err, resetSchemaLockTimeout(executor.conn))
	}
	result, execErr := executor.conn.ExecContext(ctx, query)
	return result, errors.Join(execErr, resetSchemaLockTimeout(executor.conn))
}

func (db SQLDB) withSchemaBootstrapLock(
	ctx context.Context,
	waitTimeout time.Duration,
	apply func(Executor) error,
) error {
	if db.DB == nil {
		return fmt.Errorf("postgres SQLDB requires a database handle")
	}
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open schema bootstrap connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	lockCtx := ctx
	cancel := func() {}
	if waitTimeout > 0 {
		lockCtx, cancel = context.WithTimeout(ctx, waitTimeout)
	}
	_, err = conn.ExecContext(
		lockCtx,
		"SELECT pg_advisory_lock($1, $2)",
		schemaBootstrapAdvisoryLockClass,
		schemaBootstrapAdvisoryLockID,
	)
	cancel()
	if err != nil {
		return fmt.Errorf("acquire schema bootstrap ownership: %w", err)
	}

	applyErr := apply(schemaConnectionExecutor{db: db, conn: conn})
	unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer unlockCancel()
	_, unlockErr := conn.ExecContext(
		unlockCtx,
		"SELECT pg_advisory_unlock($1, $2)",
		schemaBootstrapAdvisoryLockClass,
		schemaBootstrapAdvisoryLockID,
	)
	if unlockErr != nil {
		unlockErr = fmt.Errorf("release schema bootstrap ownership: %w", unlockErr)
	}
	return errors.Join(applyErr, unlockErr)
}
