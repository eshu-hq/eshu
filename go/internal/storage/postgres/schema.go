// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// Definition describes one ordered bootstrap SQL payload.
type Definition struct {
	Name string
	Path string
	SQL  string
}

// Executor is the narrow adapter surface required to apply schema bootstrap
// statements against a SQL connection or transaction.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type schemaLockTimeoutExecutor interface {
	execContextWithLockTimeout(context.Context, string, time.Duration) (sql.Result, error)
}

type schemaBootstrapLocker interface {
	withSchemaBootstrapLock(context.Context, time.Duration, func(Executor) error) error
}

const defaultSchemaLockTimeout = 5 * time.Second

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// BootstrapDefinitions returns the ordered Wave 2 bootstrap layout, sourced
// from embed.FS so the migrations/ directory is the single source of truth.
func BootstrapDefinitions() []Definition {
	entries, err := embeddedMigrations.ReadDir("migrations")
	if err != nil {
		panic("postgres: read embedded migrations dir: " + err.Error())
	}
	defs := make([]Definition, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		data, err := embeddedMigrations.ReadFile(path.Join("migrations", name))
		if err != nil {
			panic("postgres: read embedded migration " + name + ": " + err.Error())
		}
		// Derive definition name: strip numeric prefix and .sql extension.
		// e.g. "001_ingestion_scopes.sql" → "ingestion_scopes".
		// Skip files without the expected NNN_ prefix (a human should rename them).
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue
		}
		defName := strings.TrimSuffix(parts[1], ".sql")

		// Path reflects the real embed location for callers that read files
		// from disk (e.g. the bootstrap mirror test).
		fspath := path.Join("go", "internal", "storage", "postgres", "migrations", name)

		defs = append(defs, Definition{
			Name: defName,
			Path: fspath,
			SQL:  string(data),
		})
	}
	sort.SliceStable(defs, func(i, j int) bool {
		return defs[i].Path < defs[j].Path
	})
	return defs
}

// BootstrapDefinitionsWithoutContentSearchIndexes returns the bootstrap layout
// without the expensive content trigram indexes. It is intended for
// bulk-load flows that call EnsureContentSearchIndexes after the initial
// write-heavy drain completes.
func BootstrapDefinitionsWithoutContentSearchIndexes() []Definition {
	defs := BootstrapDefinitions()
	for i := range defs {
		if defs[i].Name == "content_store" {
			defs[i].SQL = contentStoreSchemaWithoutSearchIndexesSQL
			break
		}
	}
	return defs
}

// BootstrapStatements returns the ordered SQL payloads that make up the
// bootstrap layout.
func BootstrapStatements() []string {
	defs := BootstrapDefinitions()
	statements := make([]string, 0, len(defs))
	for _, def := range defs {
		statements = append(statements, def.SQL)
	}

	return statements
}

// ValidateDefinitions checks that a schema layout is complete enough to apply.
func ValidateDefinitions(defs []Definition) error {
	seen := make(map[string]struct{}, len(defs))
	for i, def := range defs {
		if strings.TrimSpace(def.Name) == "" {
			return fmt.Errorf("definition %d has an empty name", i)
		}
		if strings.TrimSpace(def.Path) == "" {
			return fmt.Errorf("definition %q has an empty path", def.Name)
		}
		if strings.TrimSpace(def.SQL) == "" {
			return fmt.Errorf("definition %q has empty SQL", def.Name)
		}
		if _, ok := seen[def.Name]; ok {
			return fmt.Errorf("duplicate definition name %q", def.Name)
		}
		seen[def.Name] = struct{}{}
	}

	return nil
}

// ApplyDefinitions executes one ordered schema layout against the executor.
func ApplyDefinitions(ctx context.Context, exec Executor, defs []Definition) error {
	return ApplyDefinitionsWithLockTimeout(ctx, exec, defs, defaultSchemaLockTimeout)
}

// ApplyDefinitionsWithLockTimeout executes one ordered schema layout while
// bounding Postgres lock acquisition for lock-timeout-capable executors.
func ApplyDefinitionsWithLockTimeout(
	ctx context.Context,
	exec Executor,
	defs []Definition,
	lockTimeout time.Duration,
) error {
	if err := ValidateDefinitions(defs); err != nil {
		return err
	}
	if exec == nil {
		return fmt.Errorf("executor is required")
	}

	if lockTimeout > 0 {
		if lockExec, ok := exec.(schemaLockTimeoutExecutor); ok {
			for _, def := range defs {
				if _, err := lockExec.execContextWithLockTimeout(ctx, def.SQL, lockTimeout); err != nil {
					return fmt.Errorf("apply %s: %w", def.Name, err)
				}
			}
			return nil
		}
	}

	for _, def := range defs {
		if _, err := exec.ExecContext(ctx, def.SQL); err != nil {
			return fmt.Errorf("apply %s: %w", def.Name, err)
		}
	}

	return nil
}

// ApplyBootstrap applies the Wave 2 schema bootstrap layout.
func ApplyBootstrap(ctx context.Context, exec Executor) error {
	return applyBootstrapDefinitions(ctx, exec, BootstrapDefinitions())
}

// ApplyBootstrapWithoutContentSearchIndexes applies the bootstrap layout while
// deferring content trigram indexes for a later bulk index build.
func ApplyBootstrapWithoutContentSearchIndexes(ctx context.Context, exec Executor) error {
	return applyBootstrapDefinitions(ctx, exec, BootstrapDefinitionsWithoutContentSearchIndexes())
}

func applyBootstrapDefinitions(ctx context.Context, exec Executor, definitions []Definition) error {
	if locker, ok := exec.(schemaBootstrapLocker); ok {
		return locker.withSchemaBootstrapLock(ctx, defaultSchemaLockTimeout, func(locked Executor) error {
			return ApplyDefinitions(ctx, locked, definitions)
		})
	}
	return ApplyDefinitions(ctx, exec, definitions)
}

// EnsureContentSearchIndexes creates and validates the trigram indexes that
// accelerate content file and entity source search. A transaction-scoped
// advisory lock serializes the complete finalization lifecycle, so concurrent
// finalizers wait and then recheck durable readiness instead of racing DDL.
func EnsureContentSearchIndexes(ctx context.Context, db Beginner) error {
	if db == nil {
		return fmt.Errorf("executor is required")
	}
	claimed, err := claimContentSearchIndexBuild(ctx, db)
	if err != nil {
		return errors.Join(err, markContentSearchIndexBuildFailed(db))
	}
	if !claimed {
		return nil
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return errors.Join(
			fmt.Errorf("begin content search index finalization: %w", err),
			markContentSearchIndexBuildFailed(db),
		)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, contentSearchIndexFinalizerLockSQL); err != nil {
		_ = tx.Rollback()
		return errors.Join(
			fmt.Errorf("lock content search index finalization: %w", err),
			markContentSearchIndexBuildFailed(db),
		)
	}
	if err := ensureContentSearchIndexesInTransaction(ctx, tx); err != nil {
		_ = tx.Rollback()
		return errors.Join(err, markContentSearchIndexBuildFailed(db))
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return errors.Join(
			fmt.Errorf("commit content search index finalization: %w", err),
			markContentSearchIndexBuildFailed(db),
		)
	}
	return nil
}

func claimContentSearchIndexBuild(ctx context.Context, db Beginner) (bool, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin content search index build claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, contentSearchIndexFinalizerLockSQL); err != nil {
		return false, fmt.Errorf("lock content search index build claim: %w", err)
	}
	claim, err := tx.ExecContext(ctx, contentSearchIndexClaimBuildSQL)
	if err != nil {
		return false, fmt.Errorf("claim content search index build: %w", err)
	}
	claimed, err := claim.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read content search index build claim: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit content search index build claim: %w", err)
	}
	return claimed == 1, nil
}

func ensureContentSearchIndexesInTransaction(ctx context.Context, exec Executor) error {
	claim, err := exec.ExecContext(ctx, contentSearchIndexClaimBuildSQL)
	if err != nil {
		return fmt.Errorf("claim content search index build: %w", err)
	}
	claimed, err := claim.RowsAffected()
	if err != nil {
		return fmt.Errorf("read content search index build claim: %w", err)
	}
	if claimed == 0 {
		return nil
	}

	buildSteps := []struct {
		name string
		sql  string
	}{
		{name: "content_files", sql: contentFilesSearchIndexSchemaSQL},
		{name: "content_entities", sql: contentEntitiesSearchIndexSchemaSQL},
		{name: "content_entity_names", sql: contentEntityNamesSearchIndexSchemaSQL},
		{name: "analyze", sql: "ANALYZE content_files; ANALYZE content_entities;"},
	}
	for _, step := range buildSteps {
		if _, err := exec.ExecContext(ctx, step.sql); err != nil {
			return fmt.Errorf("build content search index %s: %w", step.name, err)
		}
	}

	ready, err := exec.ExecContext(ctx, contentSearchIndexPublishReadySQL)
	if err != nil {
		return fmt.Errorf("publish content search indexes ready: %w", err)
	}
	published, err := ready.RowsAffected()
	if err != nil {
		return fmt.Errorf("read content search index ready publication: %w", err)
	}
	if published != 1 {
		return fmt.Errorf("publish content search indexes ready: exact valid indexes not found")
	}
	return nil
}

func markContentSearchIndexBuildFailed(db Beginner) error {
	failedCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := db.Begin(failedCtx)
	if err != nil {
		return fmt.Errorf("begin content search index failure publication: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(failedCtx, contentSearchIndexFinalizerLockSQL); err != nil {
		return fmt.Errorf("lock content search index failure publication: %w", err)
	}
	if _, err := tx.ExecContext(failedCtx, contentSearchIndexPublishFailedSQL); err != nil {
		return fmt.Errorf("publish content search indexes failed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit content search index failure publication: %w", err)
	}
	return nil
}

// MigrationSQL returns the SQL payload for the named migration definition.
// It panics if the name is not found in the embedded migrations.
func MigrationSQL(defName string) string {
	for _, def := range BootstrapDefinitions() {
		if def.Name == defName {
			return def.SQL
		}
	}
	panic("postgres: migration not found: " + defName)
}
