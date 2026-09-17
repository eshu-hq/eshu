// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
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

type schemaMigrationTracker interface {
	applyTrackedDefinitions(context.Context, []Definition, time.Duration) error
}

type schemaMigrationKey struct {
	path    string
	variant string
}

type schemaMigrationPlan struct {
	definition Definition
	checksum   string
	variant    string
	apply      bool
}

const schemaMigrationsTableSQL = `CREATE TABLE IF NOT EXISTS eshu_schema_migrations (
    path TEXT NOT NULL,
    variant TEXT NOT NULL,
    checksum_sha256 TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (path, variant)
)`

func migrationChecksum(statement string) string {
	sum := sha256.Sum256([]byte(statement))
	return hex.EncodeToString(sum[:])
}

func migrationVariant(def Definition) string {
	if def.variant == "" {
		return "full"
	}
	return def.variant
}

func (executor schemaConnectionExecutor) applyTrackedDefinitions(
	ctx context.Context,
	definitions []Definition,
	lockTimeout time.Duration,
) error {
	if err := ValidateDefinitions(definitions); err != nil {
		return err
	}
	seenPaths := make(map[string]struct{}, len(definitions))
	for _, def := range definitions {
		if _, exists := seenPaths[def.Path]; exists {
			return fmt.Errorf("duplicate migration path %q", def.Path)
		}
		seenPaths[def.Path] = struct{}{}
		if def.variant != "" && def.fullChecksum == "" {
			return fmt.Errorf("migration %q variant %q lacks a full checksum", def.Path, def.variant)
		}
	}

	started := time.Now()
	var tableName sql.NullString
	if err := executor.conn.QueryRowContext(
		ctx, "SELECT to_regclass('eshu_schema_migrations')::text",
	).Scan(&tableName); err != nil {
		return fmt.Errorf("inspect schema migration ledger: %w", err)
	}
	if !tableName.Valid {
		if _, err := executor.execContextWithLockTimeout(ctx, schemaMigrationsTableSQL, lockTimeout); err != nil {
			return fmt.Errorf("create schema migration ledger: %w", err)
		}
	}

	rows, err := executor.conn.QueryContext(
		ctx, "SELECT path, variant, checksum_sha256 FROM eshu_schema_migrations",
	)
	if err != nil {
		return fmt.Errorf("read schema migration ledger: %w", err)
	}
	applied := make(map[schemaMigrationKey]string, len(definitions))
	for rows.Next() {
		var key schemaMigrationKey
		var checksum string
		if err := rows.Scan(&key.path, &key.variant, &checksum); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan schema migration ledger: %w", err)
		}
		applied[key] = checksum
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read schema migration ledger rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close schema migration ledger rows: %w", err)
	}

	plans := make([]schemaMigrationPlan, 0, len(definitions))
	for _, def := range definitions {
		checksum := migrationChecksum(def.SQL)
		variant := migrationVariant(def)
		fullApplied := false
		if def.fullChecksum != "" {
			fullKey := schemaMigrationKey{path: def.Path, variant: "full"}
			if recorded, exists := applied[fullKey]; exists {
				if recorded != def.fullChecksum {
					return fmt.Errorf("schema migration %q full checksum changed: recorded %s, current %s",
						def.Path, recorded, def.fullChecksum)
				}
				fullApplied = true
			}
		}
		key := schemaMigrationKey{path: def.Path, variant: variant}
		if recorded, exists := applied[key]; exists {
			if recorded != checksum {
				return fmt.Errorf("schema migration %q (%s) checksum changed: recorded %s, current %s",
					def.Path, variant, recorded, checksum)
			}
			plans = append(plans, schemaMigrationPlan{definition: def, checksum: checksum, variant: variant})
			continue
		}
		if fullApplied {
			plans = append(plans, schemaMigrationPlan{definition: def, checksum: checksum, variant: variant})
			continue
		}
		plans = append(plans, schemaMigrationPlan{definition: def, checksum: checksum, variant: variant, apply: true})
	}

	appliedCount := 0
	for index, plan := range plans {
		if !plan.apply {
			continue
		}
		migrationStarted := time.Now()
		slog.InfoContext(ctx, "postgres schema migration applying",
			"path", plan.definition.Path,
			"variant", plan.variant,
			"index", index+1,
			"total", len(plans),
		)
		if _, err := executor.execContextWithLockTimeout(ctx, plan.definition.SQL, lockTimeout); err != nil {
			return fmt.Errorf("apply %s: %w", plan.definition.Name, err)
		}
		if _, err := executor.ExecContext(ctx,
			"INSERT INTO eshu_schema_migrations (path, variant, checksum_sha256) VALUES ($1, $2, $3)",
			plan.definition.Path, plan.variant, plan.checksum,
		); err != nil {
			return fmt.Errorf("record schema migration %s: %w", plan.definition.Name, err)
		}
		appliedCount++
		slog.InfoContext(ctx, "postgres schema migration recorded",
			"path", plan.definition.Path,
			"variant", plan.variant,
			"index", index+1,
			"total", len(plans),
			"duration_ms", time.Since(migrationStarted).Milliseconds(),
		)
	}
	slog.InfoContext(ctx, "postgres schema migrations complete",
		"total", len(definitions),
		"applied", appliedCount,
		"skipped", len(definitions)-appliedCount,
		"duration_ms", time.Since(started).Milliseconds(),
	)
	return nil
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
