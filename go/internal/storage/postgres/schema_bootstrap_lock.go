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

	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
	applyTrackedDefinitions(context.Context, []Definition, time.Duration, *slog.Logger) error
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
	recorded   bool
}

type schemaMigrationLedger struct {
	schema  string
	table   string
	applied map[schemaMigrationKey]string
}

const schemaMigrationsTableSQL = `CREATE TABLE IF NOT EXISTS %s (
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

func (executor schemaConnectionExecutor) invalidConcurrentIndexNames(
	ctx context.Context,
	definitions []Definition,
	schema string,
) (map[string]bool, error) {
	nameSet := make(map[string]struct{})
	for _, def := range definitions {
		for _, name := range concurrentIndexNamesForInvalidCleanup(def.SQL) {
			nameSet[name] = struct{}{}
		}
	}
	invalid := make(map[string]bool)
	if len(nameSet) == 0 {
		return invalid, nil
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	rows, err := executor.conn.QueryContext(ctx, `
SELECT DISTINCT c.relname::text
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relname::text = ANY($1::text[])
  AND n.nspname = $2
  AND i.indisvalid = FALSE
`, names, schema)
	if err != nil {
		return nil, fmt.Errorf("inspect invalid concurrent indexes: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan invalid concurrent index: %w", err)
		}
		invalid[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("read invalid concurrent indexes: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close invalid concurrent index rows: %w", err)
	}
	return invalid, nil
}

func (executor schemaConnectionExecutor) loadSchemaMigrationLedger(
	ctx context.Context,
	capacity int,
	lockTimeout time.Duration,
) (schemaMigrationLedger, error) {
	var schema sql.NullString
	if err := executor.conn.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		return schemaMigrationLedger{}, fmt.Errorf("read schema migration namespace: %w", err)
	}
	if !schema.Valid || schema.String == "" {
		return schemaMigrationLedger{}, fmt.Errorf("schema migration namespace is empty")
	}
	table := quoteSQLIdentifier(schema.String) + ".eshu_schema_migrations"
	var tableName sql.NullString
	if err := executor.conn.QueryRowContext(ctx, "SELECT to_regclass($1)::text", table).Scan(&tableName); err != nil {
		return schemaMigrationLedger{}, fmt.Errorf("inspect schema migration ledger: %w", err)
	}
	if !tableName.Valid {
		if _, err := executor.execContextWithLockTimeout(
			ctx, fmt.Sprintf(schemaMigrationsTableSQL, table), lockTimeout,
		); err != nil {
			return schemaMigrationLedger{}, fmt.Errorf("create schema migration ledger: %w", err)
		}
	}
	rows, err := executor.conn.QueryContext(ctx, "SELECT path, variant, checksum_sha256 FROM "+table)
	if err != nil {
		return schemaMigrationLedger{}, fmt.Errorf("read schema migration ledger: %w", err)
	}
	applied := make(map[schemaMigrationKey]string, capacity)
	for rows.Next() {
		var key schemaMigrationKey
		var checksum string
		if err := rows.Scan(&key.path, &key.variant, &checksum); err != nil {
			_ = rows.Close()
			return schemaMigrationLedger{}, fmt.Errorf("scan schema migration ledger: %w", err)
		}
		applied[key] = checksum
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return schemaMigrationLedger{}, fmt.Errorf("read schema migration ledger rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return schemaMigrationLedger{}, fmt.Errorf("close schema migration ledger rows: %w", err)
	}
	return schemaMigrationLedger{schema: schema.String, table: table, applied: applied}, nil
}

func (executor schemaConnectionExecutor) applyTrackedDefinitions(
	ctx context.Context,
	definitions []Definition,
	lockTimeout time.Duration,
	logger *slog.Logger,
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
	ledger, err := executor.loadSchemaMigrationLedger(ctx, len(definitions), lockTimeout)
	if err != nil {
		return err
	}
	invalidIndexes, err := executor.invalidConcurrentIndexNames(ctx, definitions, ledger.schema)
	if err != nil {
		return err
	}

	plans := make([]schemaMigrationPlan, 0, len(definitions))
	for _, def := range definitions {
		checksum := migrationChecksum(def.SQL)
		variant := migrationVariant(def)
		fullApplied := false
		if def.fullChecksum != "" {
			fullKey := schemaMigrationKey{path: def.Path, variant: "full"}
			if recorded, exists := ledger.applied[fullKey]; exists {
				if recorded != def.fullChecksum {
					return fmt.Errorf("schema migration %q full checksum changed: recorded %s, current %s",
						def.Path, recorded, def.fullChecksum)
				}
				fullApplied = true
			}
		}
		key := schemaMigrationKey{path: def.Path, variant: variant}
		if recorded, exists := ledger.applied[key]; exists {
			if recorded != checksum {
				return fmt.Errorf("schema migration %q (%s) checksum changed: recorded %s, current %s",
					def.Path, variant, recorded, checksum)
			}
			recoverIndex := false
			for _, name := range concurrentIndexNamesForInvalidCleanup(def.SQL) {
				if invalidIndexes[name] {
					recoverIndex = true
					break
				}
			}
			plans = append(plans, schemaMigrationPlan{
				definition: def, checksum: checksum, variant: variant,
				apply: recoverIndex, recorded: true,
			})
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
		logger.InfoContext(ctx, "postgres schema migration applying",
			telemetry.EventAttr("bootstrap.postgres.migration.applying"),
			"path", plan.definition.Path,
			"variant", plan.variant,
			"recovery", plan.recorded,
			"index", index+1,
			"total", len(plans),
		)
		if plan.recorded {
			// A concurrent index drop commits before its replacement build. Clear the
			// success receipt first so a crash or failed build remains retryable.
			result, err := executor.ExecContext(ctx,
				"DELETE FROM "+ledger.table+" WHERE path = $1 AND variant = $2 AND checksum_sha256 = $3",
				plan.definition.Path, plan.variant, plan.checksum,
			)
			if err != nil {
				return fmt.Errorf("clear schema migration recovery receipt %s: %w", plan.definition.Name, err)
			}
			deleted, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("count cleared schema migration recovery receipts %s: %w", plan.definition.Name, err)
			}
			if deleted != 1 {
				return fmt.Errorf("clear schema migration recovery receipt %s: deleted %d rows, want 1",
					plan.definition.Name, deleted)
			}
		}
		if _, err := executor.execContextWithLockTimeout(ctx, plan.definition.SQL, lockTimeout); err != nil {
			return fmt.Errorf("apply %s: %w", plan.definition.Name, err)
		}
		if _, err := executor.ExecContext(ctx,
			"INSERT INTO "+ledger.table+" (path, variant, checksum_sha256) VALUES ($1, $2, $3)",
			plan.definition.Path, plan.variant, plan.checksum,
		); err != nil {
			return fmt.Errorf("record schema migration %s: %w", plan.definition.Name, err)
		}
		appliedCount++
		logger.InfoContext(ctx, "postgres schema migration recorded",
			telemetry.EventAttr("bootstrap.postgres.migration.recorded"),
			"path", plan.definition.Path,
			"variant", plan.variant,
			"recovery", plan.recorded,
			"index", index+1,
			"total", len(plans),
			"duration_ms", time.Since(migrationStarted).Milliseconds(),
		)
	}
	logger.InfoContext(ctx, "postgres schema migrations complete",
		telemetry.EventAttr("bootstrap.postgres.migrations.complete"),
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
