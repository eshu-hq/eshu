// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Definition describes one ordered bootstrap SQL payload. It is an alias for
// migrations.Definition, which owns the //go:embed (#6693 decision D2).
type Definition = migrations.Definition

// BootstrapOptions controls deferred content indexes, migration progress
// logs, and how the migrator waits on other sessions (#6956).
type BootstrapOptions struct {
	DeferContentSearchIndexes bool
	Logger                    *slog.Logger
	// OwnershipWait bounds how long this bootstrapper waits for another
	// bootstrapper that owns the schema advisory lock before failing. Zero
	// means defaultSchemaOwnershipWait.
	OwnershipWait time.Duration
	// LockRetryBudget bounds the total backoff one migration statement may
	// spend retrying after lock_timeout (SQLSTATE 55P03) before the
	// migrator fails. Zero means defaultSchemaLockRetryBudget.
	LockRetryBudget time.Duration
}

type schemaLockTimeoutExecutor interface {
	execContextWithLockTimeout(context.Context, string, time.Duration) (sql.Result, error)
}

type schemaBootstrapLocker interface {
	withSchemaBootstrapLock(context.Context, *slog.Logger, time.Duration, func(db.Executor) error) error
}

const defaultSchemaLockTimeout = 5 * time.Second

// BootstrapDefinitions returns the ordered Wave 2 bootstrap layout, sourced
// from the migrations package's embed.FS so that directory stays the single
// source of truth (#6693 decision D2).
func BootstrapDefinitions() []Definition {
	return migrations.BootstrapDefinitions()
}

// BootstrapDefinitionsWithoutContentSearchIndexes returns the bootstrap layout
// without the expensive content trigram indexes. It is intended for
// bulk-load flows that call EnsureContentSearchIndexes after the initial
// write-heavy drain completes.
func BootstrapDefinitionsWithoutContentSearchIndexes() []Definition {
	defs := BootstrapDefinitions()
	for i := range defs {
		// These lifecycle files make conditional decisions while indexes are deferred;
		// the full pass must revisit them after content_store builds the indexes.
		switch defs[i].Name {
		case "content_store", "content_substring_index_state", "content_entity_name_trgm_index", "content_files_relative_path_trgm_index":
			defs[i].FullChecksum = migrationChecksum(defs[i].SQL)
			defs[i].Variant = "deferred"
			if defs[i].Name == "content_store" {
				defs[i].SQL = contentStoreSchemaWithoutSearchIndexesSQL
			}
			if defs[i].Name == "content_files_relative_path_trgm_index" {
				defs[i].SQL = "SELECT 1;"
			}
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
func ApplyDefinitions(ctx context.Context, exec db.Executor, defs []Definition) error {
	return ApplyDefinitionsWithLockTimeout(ctx, exec, defs, defaultSchemaLockTimeout)
}

// ApplyDefinitionsWithLockTimeout executes one ordered schema layout while
// bounding Postgres lock acquisition for lock-timeout-capable executors.
func ApplyDefinitionsWithLockTimeout(
	ctx context.Context,
	exec db.Executor,
	defs []Definition,
	lockTimeout time.Duration,
) error {
	if err := ValidateDefinitions(defs); err != nil {
		return err
	}
	if exec == nil {
		return fmt.Errorf("executor is required")
	}
	phases := make([][]migrationPhase, len(defs))
	for i, def := range defs {
		var err error
		phases[i], err = migrationPhases(def)
		if err != nil {
			return err
		}
	}

	if lockTimeout > 0 {
		if lockExec, ok := exec.(schemaLockTimeoutExecutor); ok {
			for i, def := range defs {
				for _, phase := range phases[i] {
					if _, err := lockExec.execContextWithLockTimeout(ctx, phase.sql, lockTimeout); err != nil {
						if len(phases[i]) > 1 {
							return fmt.Errorf("apply %s phase %s: %w", def.Name, phase.name, err)
						}
						return fmt.Errorf("apply %s: %w", def.Name, err)
					}
				}
			}
			return nil
		}
	}

	for i, def := range defs {
		for _, phase := range phases[i] {
			if _, err := exec.ExecContext(ctx, phase.sql); err != nil {
				if len(phases[i]) > 1 {
					return fmt.Errorf("apply %s phase %s: %w", def.Name, phase.name, err)
				}
				return fmt.Errorf("apply %s: %w", def.Name, err)
			}
		}
	}

	return nil
}

// Environment variables BootstrapOptionsFromEnv reads (#6956).
const (
	// OwnershipWaitEnv bounds how long a bootstrapper waits for another one
	// that owns the schema advisory lock.
	OwnershipWaitEnv = "ESHU_SCHEMA_BOOTSTRAP_OWNERSHIP_WAIT" // #nosec G101 -- environment variable name, not a credential value
	// LockRetryBudgetEnv bounds the total backoff one migration statement may
	// spend retrying after lock_timeout.
	LockRetryBudgetEnv = "ESHU_SCHEMA_LOCK_RETRY_BUDGET" // #nosec G101 -- environment variable name, not a credential value
)

// BootstrapOptionsFromEnv returns options whose OwnershipWait and
// LockRetryBudget come from the environment. An unset variable leaves the
// package default in force; a value that does not parse as a duration or is
// not strictly positive is refused by name, so a typo cannot disable a
// bound.
func BootstrapOptionsFromEnv(getenv func(string) string) (BootstrapOptions, error) {
	ownershipWait, err := optionalPositiveDuration(getenv, OwnershipWaitEnv)
	if err != nil {
		return BootstrapOptions{}, err
	}
	lockRetryBudget, err := optionalPositiveDuration(getenv, LockRetryBudgetEnv)
	if err != nil {
		return BootstrapOptions{}, err
	}
	return BootstrapOptions{OwnershipWait: ownershipWait, LockRetryBudget: lockRetryBudget}, nil
}

func optionalPositiveDuration(getenv func(string) string, env string) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(env))
	if raw == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", env, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", env)
	}
	return value, nil
}

// ApplyBootstrap applies the Wave 2 schema bootstrap layout.
func ApplyBootstrap(ctx context.Context, exec db.Executor) error {
	return ApplyBootstrapWithOptions(ctx, exec, BootstrapOptions{})
}

// ApplyBootstrapWithoutContentSearchIndexes applies the bootstrap layout while
// deferring content trigram indexes for a later bulk index build.
func ApplyBootstrapWithoutContentSearchIndexes(ctx context.Context, exec db.Executor) error {
	return ApplyBootstrapWithOptions(ctx, exec, BootstrapOptions{DeferContentSearchIndexes: true})
}

// ApplyBootstrapWithOptions applies pending Postgres migrations and logs progress
// through the supplied logger, or the default logger when Logger is nil.
func ApplyBootstrapWithOptions(ctx context.Context, exec db.Executor, options BootstrapOptions) error {
	definitions := BootstrapDefinitions()
	if options.DeferContentSearchIndexes {
		definitions = BootstrapDefinitionsWithoutContentSearchIndexes()
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return applyBootstrapDefinitionsWith(ctx, exec, definitions, logger, schemaBootstrapCoordination{
		ownershipWait:   options.OwnershipWait,
		lockRetryBudget: options.LockRetryBudget,
	})
}

// applyBootstrapDefinitionsWith applies definitions under the schema
// ownership lock with the given coordination bounds (zero fields take the
// package defaults).
func applyBootstrapDefinitionsWith(
	ctx context.Context,
	exec db.Executor,
	definitions []Definition,
	logger *slog.Logger,
	coordination schemaBootstrapCoordination,
) error {
	coordination = coordination.withDefaults()
	if locker, ok := exec.(schemaBootstrapLocker); ok {
		return locker.withSchemaBootstrapLock(ctx, logger, coordination.ownershipWait, func(locked db.Executor) error {
			if tracker, ok := locked.(schemaMigrationTracker); ok {
				return tracker.applyTrackedDefinitions(ctx, definitions, schemaStatementBounds{
					lockTimeout:     defaultSchemaLockTimeout,
					lockRetryBudget: coordination.lockRetryBudget,
				}, logger)
			}
			return ApplyDefinitions(ctx, locked, definitions)
		})
	}
	return ApplyDefinitions(ctx, exec, definitions)
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

const (
	contentGenerationMigrationPath = "go/internal/storage/postgres/migrations/169_content_files_generation_id.sql"
	contentGenerationMigrationSHA  = "28871f0d7b0bc493b3d22949ac475b98129bc3668fe93e1d1409543714a3878b"
	contentGenerationUpdateStart   = "\n\nUPDATE content_files AS f\n"
	contentGenerationAnalyzeStart  = "\n\nANALYZE content_files;"
)

type migrationPhase struct {
	name string
	sql  string
}

// migrationPhases preserves ordinary migration execution and splits only the
// shipped full variant of 169 at its immutable byte boundaries. A changed 169
// fails closed rather than reverting to the read-blocking one-message path.
func migrationPhases(def Definition) ([]migrationPhase, error) {
	if def.Path != contentGenerationMigrationPath {
		return []migrationPhase{{name: "whole", sql: def.SQL}}, nil
	}
	if variant := migrationVariant(def); variant != "full" || migrationChecksum(def.SQL) != contentGenerationMigrationSHA {
		return nil, fmt.Errorf("migration %q requires the shipped full variant and checksum %s", def.Path, contentGenerationMigrationSHA)
	}
	update := strings.Index(def.SQL, contentGenerationUpdateStart)
	analyze := strings.Index(def.SQL, contentGenerationAnalyzeStart)
	// The pinned SQL has both markers in order; guard later marker-constant edits.
	if update <= 0 || analyze <= update {
		return nil, fmt.Errorf("migration %q immutable phase boundaries not found", def.Path)
	}
	return []migrationPhase{
		{name: "add_column", sql: def.SQL[:update]},
		{name: "backfill", sql: def.SQL[update:analyze]},
		{name: "analyze", sql: def.SQL[analyze:]},
	}, nil
}

// applyTrackedMigration owns the per-migration receipt. Each phase is a
// separate autocommit statement on the advisory-lock-owning connection, so
// the catalog-only ALTER releases ACCESS EXCLUSIVE before the long UPDATE.
// A crash after any phase leaves no receipt and safely replays from the ALTER.
func (executor schemaConnectionExecutor) applyTrackedMigration(
	ctx context.Context,
	plan schemaMigrationPlan,
	ledger schemaMigrationLedger,
	bounds schemaStatementBounds,
	retryAllowance *coordination.LockRetryAllowance,
	logger *slog.Logger,
	index, total int,
) error {
	migrationStarted := time.Now()
	logger.InfoContext(ctx, "postgres schema migration applying",
		telemetry.EventAttr("bootstrap.postgres.migration.applying"),
		"path", plan.definition.Path,
		"variant", plan.variant,
		"recovery", plan.recorded,
		"index", index+1,
		"total", total,
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
	phases, err := migrationPhases(plan.definition)
	if err != nil {
		return err
	}
	for _, phase := range phases {
		phaseStarted := time.Now()
		if len(phases) > 1 {
			logger.InfoContext(ctx, "postgres schema migration phase applying",
				telemetry.EventAttr("bootstrap.postgres.migration.phase_applying"),
				"path", plan.definition.Path, "phase", phase.name,
			)
		}
		retryPolicy := coordination.LockRetryPolicy{
			Allowance:      retryAllowance,
			InitialBackoff: max(bounds.lockTimeout, time.Second),
			MaxBackoff:     schemaLockRetryMaxBackoff,
		}
		if err := coordination.RetryOnLockTimeout(ctx, logger, plan.definition.Path, retryPolicy, coordination.SleepContext, time.Now, func() error {
			_, err := executor.execContextWithLockTimeout(ctx, phase.sql, bounds.lockTimeout)
			return err
		}); err != nil {
			if len(phases) > 1 {
				return fmt.Errorf("apply %s phase %s: %w", plan.definition.Name, phase.name, err)
			}
			return fmt.Errorf("apply %s: %w", plan.definition.Name, err)
		}
		if len(phases) > 1 {
			logger.InfoContext(ctx, "postgres schema migration phase applied",
				telemetry.EventAttr("bootstrap.postgres.migration.phase_applied"),
				"path", plan.definition.Path, "phase", phase.name,
				"duration_ms", time.Since(phaseStarted).Milliseconds(),
			)
		}
	}
	if _, err := executor.ExecContext(ctx,
		"INSERT INTO "+ledger.table+" (path, variant, checksum_sha256) VALUES ($1, $2, $3)",
		plan.definition.Path, plan.variant, plan.checksum,
	); err != nil {
		return fmt.Errorf("record schema migration %s: %w", plan.definition.Name, err)
	}
	logger.InfoContext(ctx, "postgres schema migration recorded",
		telemetry.EventAttr("bootstrap.postgres.migration.recorded"),
		"path", plan.definition.Path,
		"variant", plan.variant,
		"recovery", plan.recorded,
		"index", index+1,
		"total", total,
		"duration_ms", time.Since(migrationStarted).Milliseconds(),
	)
	return nil
}
