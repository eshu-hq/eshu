// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build issue7033_rollout

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	issue7033RolloutOptInEnv                    = "ESHU_ISSUE7033_ROLLOUT"
	issue7033RolloutOptInValue                  = "apply"
	issue7033RolloutDSNEnv                      = "ESHU_ISSUE7033_ROLLOUT_DSN"
	issue7033RolloutExpectedSystemIdentifierEnv = "ESHU_ISSUE7033_ROLLOUT_EXPECTED_SYSTEM_IDENTIFIER"
	issue7033RolloutExpectedDatabaseEnv         = "ESHU_ISSUE7033_ROLLOUT_EXPECTED_DATABASE"
	issue7033RolloutExpectedSchemaEnv           = "ESHU_ISSUE7033_ROLLOUT_EXPECTED_SCHEMA"

	issue7033RolloutTimeout       = 15 * time.Minute
	issue7033RolloutOwnershipWait = 30 * time.Second
	issue7033RolloutLockRetry     = 30 * time.Second

	issue7033IndexMigrationName     = "content_files_relative_path_trgm_index"
	issue7033IndexMigrationPath     = "go/internal/storage/postgres/migrations/126_content_files_relative_path_trgm_index.sql"
	issue7033IndexMigrationChecksum = "ef395de0a2c1ad86fcbc1f82abcda08c83e689508a1197d6e2848695978a8e41"
	issue7033LifecycleMigrationName = "content_files_relative_path_trgm_index_lifecycle"
	issue7033LifecycleMigrationPath = "go/internal/storage/postgres/migrations/127_content_files_relative_path_trgm_index_lifecycle.sql"
	issue7033LifecycleMigrationSum  = "c264034334e8251d3169462a141701d9f87d6737f9329a1750092a26b76f840e"
	issue7033Prerequisite124Name    = "fact_records_documentation_semantic_target_refs_idx"
	issue7033Prerequisite125Name    = "shared_projection_acceptance_generation_key"
	issue7033RelativePathIndexName  = "content_files_relative_path_trgm_idx"
	issue7033ContentIndexStateTable = "content_substring_index_state"
	issue7033SchemaMigrationsTable  = "eshu_schema_migrations"
)

type issue7033RolloutConfig struct {
	dsn              string
	systemIdentifier string
	database         string
	schema           string
}

type issue7033ExpectedIndex struct {
	name   string
	table  string
	column string
}

var issue7033ExistingIndexes = []issue7033ExpectedIndex{
	{name: "content_files_content_trgm_idx", table: "content_files", column: "content"},
	{name: "content_entities_source_trgm_idx", table: "content_entities", column: "source_cache"},
	{name: "content_entities_name_trgm_idx", table: "content_entities", column: "entity_name"},
}

// TestIssue7033ScopedRolloutLive is intentionally build-tagged and refuses to
// run without the explicit, fully identified production target. It applies only
// migrations 126 and 127 for the #7033 relative-path index rollout.
func TestIssue7033ScopedRolloutLive(t *testing.T) {
	config, err := issue7033RolloutConfigFromEnv(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("pgx", config.dsn)
	if err != nil {
		t.Fatalf("open scoped rollout target: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), issue7033RolloutTimeout)
	defer cancel()
	if err := runIssue7033ScopedRollout(ctx, database, config, slog.Default()); err != nil {
		t.Fatalf("run #7033 scoped rollout: %v", err)
	}
}

func issue7033RolloutConfigFromEnv(getenv func(string) string) (issue7033RolloutConfig, error) {
	config := issue7033RolloutConfig{
		dsn:              strings.TrimSpace(getenv(issue7033RolloutDSNEnv)),
		systemIdentifier: strings.TrimSpace(getenv(issue7033RolloutExpectedSystemIdentifierEnv)),
		database:         strings.TrimSpace(getenv(issue7033RolloutExpectedDatabaseEnv)),
		schema:           strings.TrimSpace(getenv(issue7033RolloutExpectedSchemaEnv)),
	}
	if strings.TrimSpace(getenv(issue7033RolloutOptInEnv)) != issue7033RolloutOptInValue {
		return issue7033RolloutConfig{}, fmt.Errorf("%s=%q is required", issue7033RolloutOptInEnv, issue7033RolloutOptInValue)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: issue7033RolloutDSNEnv, value: config.dsn},
		{name: issue7033RolloutExpectedSystemIdentifierEnv, value: config.systemIdentifier},
		{name: issue7033RolloutExpectedDatabaseEnv, value: config.database},
		{name: issue7033RolloutExpectedSchemaEnv, value: config.schema},
	} {
		if field.value == "" {
			return issue7033RolloutConfig{}, fmt.Errorf("%s is required", field.name)
		}
	}
	if !issue7033DecimalIdentifier(config.systemIdentifier) {
		return issue7033RolloutConfig{}, fmt.Errorf("%s must be a decimal PostgreSQL system identifier", issue7033RolloutExpectedSystemIdentifierEnv)
	}
	return config, nil
}

func issue7033DecimalIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func runIssue7033ScopedRollout(
	ctx context.Context,
	database *sql.DB,
	config issue7033RolloutConfig,
	logger *slog.Logger,
) error {
	if database == nil {
		return errors.New("scoped rollout database is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	definitions, err := issue7033ScopedRolloutDefinitions()
	if err != nil {
		return err
	}
	if err := issue7033ScopedRolloutPreflight(ctx, database, config, definitions); err != nil {
		return fmt.Errorf("preflight: %w", err)
	}

	exec := SQLDB{DB: database}
	coordination := schemaBootstrapCoordination{
		ownershipWait:   issue7033RolloutOwnershipWait,
		lockRetryBudget: issue7033RolloutLockRetry,
	}
	logger.InfoContext(ctx, "#7033 scoped migration rollout applying", "migrations", len(definitions))
	if err := applyBootstrapDefinitionsWith(ctx, exec, definitions[:1], logger, coordination); err != nil {
		return fmt.Errorf("apply migration 126: %w", err)
	}
	if err := issue7033ExactTrigramIndex(ctx, database, issue7033ExpectedIndex{
		name: issue7033RelativePathIndexName, table: "content_files", column: "relative_path",
	}); err != nil {
		return fmt.Errorf("validate migration 126 index: %w", err)
	}
	if err := applyBootstrapDefinitionsWith(ctx, exec, definitions[1:], logger, coordination); err != nil {
		return fmt.Errorf("apply migration 127: %w", err)
	}
	if err := issue7033ScopedRolloutPostflight(ctx, database, definitions); err != nil {
		return fmt.Errorf("postflight: %w", err)
	}
	logger.InfoContext(ctx, "#7033 scoped migration rollout complete", "migrations", len(definitions))
	return nil
}

func issue7033ScopedRolloutDefinitions() ([]Definition, error) {
	definitions := BootstrapDefinitions()
	selected := make([]Definition, 0, 2)
	for _, want := range []struct {
		name     string
		path     string
		checksum string
	}{
		{name: issue7033IndexMigrationName, path: issue7033IndexMigrationPath, checksum: issue7033IndexMigrationChecksum},
		{name: issue7033LifecycleMigrationName, path: issue7033LifecycleMigrationPath, checksum: issue7033LifecycleMigrationSum},
	} {
		var found Definition
		for _, definition := range definitions {
			if definition.Name == want.name && definition.Path == want.path {
				found = definition
				break
			}
		}
		if found.Name == "" {
			return nil, fmt.Errorf("required migration %s at %s is absent", want.name, want.path)
		}
		if checksum := migrationChecksum(found.SQL); checksum != want.checksum {
			return nil, fmt.Errorf("migration %s checksum = %s, want %s", want.name, checksum, want.checksum)
		}
		selected = append(selected, found)
	}
	return selected, nil
}

func issue7033ScopedRolloutPreflight(
	ctx context.Context,
	database *sql.DB,
	config issue7033RolloutConfig,
	definitions []Definition,
) error {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return fmt.Errorf("begin read-only preflight: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := issue7033ValidateTarget(ctx, tx, config); err != nil {
		return err
	}
	if err := issue7033ValidatePrerequisites(ctx, tx, definitions); err != nil {
		return err
	}
	return nil
}

func issue7033ValidateTarget(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, config issue7033RolloutConfig,
) error {
	var systemIdentifier, database, schema string
	var primary bool
	err := queryer.QueryRowContext(ctx, `
SELECT (pg_control_system()).system_identifier::text,
       current_database(),
       current_schema(),
       NOT pg_is_in_recovery()`).Scan(&systemIdentifier, &database, &schema, &primary)
	if err != nil {
		return fmt.Errorf("read target identity: %w", err)
	}
	if systemIdentifier != config.systemIdentifier || database != config.database || schema != config.schema || !primary {
		return errors.New("target identity, schema, or primary role does not match explicit rollout inputs")
	}
	return nil
}

func issue7033ValidatePrerequisites(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	definitions []Definition,
) error {
	var pgTrgm bool
	if err := queryer.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm')`).Scan(&pgTrgm); err != nil {
		return fmt.Errorf("read pg_trgm extension: %w", err)
	}
	if !pgTrgm {
		return errors.New("pg_trgm extension is absent")
	}
	for _, table := range []string{"content_files", "content_entities", issue7033ContentIndexStateTable, issue7033SchemaMigrationsTable} {
		var exists bool
		if err := queryer.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&exists); err != nil {
			return fmt.Errorf("read required table %s: %w", table, err)
		}
		if !exists {
			return fmt.Errorf("required table public.%s is absent", table)
		}
	}
	for _, index := range issue7033ExistingIndexes {
		if err := issue7033ExactTrigramIndex(ctx, queryer, index); err != nil {
			return fmt.Errorf("validate prerequisite index %s: %w", index.name, err)
		}
	}
	var state string
	var ready bool
	if err := queryer.QueryRowContext(ctx, `
SELECT state, eshu_content_substring_indexes_valid()
FROM content_substring_index_state
WHERE singleton = TRUE`).Scan(&state, &ready); err != nil {
		return fmt.Errorf("read content index lifecycle: %w", err)
	}
	if state != "ready" || !ready {
		return fmt.Errorf("content index lifecycle = state %q ready %t, want ready true", state, ready)
	}

	prerequisite124, err := issue7033DefinitionByName(issue7033Prerequisite124Name)
	if err != nil {
		return err
	}
	prerequisite125, err := issue7033DefinitionByName(issue7033Prerequisite125Name)
	if err != nil {
		return err
	}
	if _, err := issue7033Receipt(ctx, queryer, prerequisite124, true); err != nil {
		return fmt.Errorf("validate migration 124 receipt: %w", err)
	}
	for _, definition := range append([]Definition{prerequisite125}, definitions...) {
		if _, err := issue7033Receipt(ctx, queryer, definition, false); err != nil {
			return fmt.Errorf("validate optional migration receipt %s: %w", definition.Name, err)
		}
	}
	if err := issue7033ValidatePathIndexAndReceipts(ctx, queryer, definitions); err != nil {
		return err
	}
	var buildActive bool
	if err := queryer.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM pg_stat_progress_create_index
  WHERE relid = 'public.content_files'::regclass
)`).Scan(&buildActive); err != nil {
		return fmt.Errorf("inspect active content_files index build: %w", err)
	}
	if buildActive {
		return errors.New("content_files has an active index build")
	}
	return nil
}

func issue7033ValidatePathIndexAndReceipts(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	definitions []Definition,
) error {
	indexApplied, err := issue7033Receipt(ctx, queryer, definitions[0], false)
	if err != nil {
		return err
	}
	lifecycleApplied, err := issue7033Receipt(ctx, queryer, definitions[1], false)
	if err != nil {
		return err
	}
	if lifecycleApplied && !indexApplied {
		return errors.New("migration 127 receipt exists without migration 126 receipt")
	}
	if !indexApplied {
		var exists bool
		if err := queryer.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+issue7033RelativePathIndexName).Scan(&exists); err != nil {
			return fmt.Errorf("read existing relative-path index: %w", err)
		}
		if exists {
			return errors.New("untracked relative-path index collides with migration 126")
		}
		return nil
	}
	return issue7033ExactTrigramIndex(ctx, queryer, issue7033ExpectedIndex{
		name: issue7033RelativePathIndexName, table: "content_files", column: "relative_path",
	})
}

func issue7033DefinitionByName(name string) (Definition, error) {
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == name {
			return definition, nil
		}
	}
	return Definition{}, fmt.Errorf("required migration %q is absent", name)
}

func issue7033Receipt(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	definition Definition,
	required bool,
) (bool, error) {
	var checksum string
	err := queryer.QueryRowContext(ctx, `
SELECT checksum_sha256
FROM eshu_schema_migrations
WHERE path = $1 AND variant = 'full'`, definition.Path).Scan(&checksum)
	if errors.Is(err, sql.ErrNoRows) {
		if required {
			return false, fmt.Errorf("migration %s receipt is absent", definition.Name)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read migration %s receipt: %w", definition.Name, err)
	}
	if checksum != migrationChecksum(definition.SQL) {
		return false, fmt.Errorf("migration %s checksum does not match embedded SQL", definition.Name)
	}
	return true, nil
}

func issue7033ExactTrigramIndex(
	ctx context.Context,
	queryer interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	index issue7033ExpectedIndex,
) error {
	var exact bool
	err := queryer.QueryRowContext(ctx, `
SELECT i.indisvalid AND i.indisready
  AND NOT i.indisunique
  AND i.indnkeyatts = 1 AND i.indnatts = 1
  AND i.indpred IS NULL AND i.indexprs IS NULL
  AND access_method.amname = 'gin'
  AND indexed_attribute.attname = $3
  AND operator_class.opcname = 'gin_trgm_ops'
FROM pg_index AS i
JOIN pg_class AS index_relation ON index_relation.oid = i.indexrelid
JOIN pg_namespace AS index_schema ON index_schema.oid = index_relation.relnamespace
JOIN pg_class AS table_relation ON table_relation.oid = i.indrelid
JOIN pg_namespace AS table_schema ON table_schema.oid = table_relation.relnamespace
JOIN pg_am AS access_method ON access_method.oid = index_relation.relam
JOIN pg_attribute AS indexed_attribute
  ON indexed_attribute.attrelid = table_relation.oid
 AND indexed_attribute.attnum = i.indkey[0]
JOIN pg_opclass AS operator_class ON operator_class.oid = i.indclass[0]
WHERE index_schema.nspname = 'public'
  AND index_relation.relname = $1
  AND table_schema.nspname = 'public'
  AND table_relation.relname = $2`, index.name, index.table, index.column).Scan(&exact)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("index public.%s is absent", index.name)
	}
	if err != nil {
		return fmt.Errorf("read index public.%s: %w", index.name, err)
	}
	if !exact {
		return fmt.Errorf("index public.%s is not the exact valid ready gin_trgm_ops definition", index.name)
	}
	return nil
}

func issue7033ScopedRolloutPostflight(ctx context.Context, database *sql.DB, definitions []Definition) error {
	for _, definition := range definitions {
		if _, err := issue7033Receipt(ctx, database, definition, true); err != nil {
			return err
		}
	}
	if err := issue7033ExactTrigramIndex(ctx, database, issue7033ExpectedIndex{
		name: issue7033RelativePathIndexName, table: "content_files", column: "relative_path",
	}); err != nil {
		return err
	}
	var state string
	var indexesValid, readiness bool
	if err := database.QueryRowContext(ctx, `
SELECT state,
       eshu_content_substring_indexes_valid(),
       eshu_require_content_substring_indexes_ready()
FROM content_substring_index_state
WHERE singleton = TRUE`).Scan(&state, &indexesValid, &readiness); err != nil {
		return fmt.Errorf("read postflight readiness: %w", err)
	}
	if state != "ready" || !indexesValid || !readiness {
		return fmt.Errorf("postflight readiness = state %q indexes_valid %t readiness %t, want ready true true", state, indexesValid, readiness)
	}
	return nil
}
