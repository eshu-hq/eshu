// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build issue7033_rollout

package postgres

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestIssue7033ScopedRolloutConfigRefusesMissingSafetyInputs(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		issue7033RolloutOptInEnv:                    issue7033RolloutOptInValue,
		issue7033RolloutDSNEnv:                      "postgres://example.invalid/eshu",
		issue7033RolloutExpectedSystemIdentifierEnv: "123",
		issue7033RolloutExpectedDatabaseEnv:         "eshu",
		issue7033RolloutExpectedSchemaEnv:           "public",
	}
	for _, missing := range []string{
		issue7033RolloutOptInEnv,
		issue7033RolloutDSNEnv,
		issue7033RolloutExpectedSystemIdentifierEnv,
		issue7033RolloutExpectedDatabaseEnv,
		issue7033RolloutExpectedSchemaEnv,
	} {
		t.Run(missing, func(t *testing.T) {
			values := make(map[string]string, len(base))
			for key, value := range base {
				values[key] = value
			}
			delete(values, missing)

			_, err := issue7033RolloutConfigFromEnv(func(key string) string { return values[key] })
			if err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("issue7033RolloutConfigFromEnv() error = %v, want missing %q", err, missing)
			}
		})
	}
	config, err := issue7033RolloutConfigFromEnv(func(key string) string { return base[key] })
	if err != nil {
		t.Fatalf("issue7033RolloutConfigFromEnv() valid config error = %v", err)
	}
	if config.database != "eshu" || config.schema != "public" || config.systemIdentifier != "123" {
		t.Fatalf("issue7033RolloutConfigFromEnv() config = %+v, want supplied target identity", config)
	}
}

func TestIssue7033ScopedRolloutConfigRefusesNonPublicSchema(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		issue7033RolloutOptInEnv:                    issue7033RolloutOptInValue,
		issue7033RolloutDSNEnv:                      "postgres://example.invalid/eshu",
		issue7033RolloutExpectedSystemIdentifierEnv: "123",
		issue7033RolloutExpectedDatabaseEnv:         "eshu",
		issue7033RolloutExpectedSchemaEnv:           "custom",
	}
	_, err := issue7033RolloutConfigFromEnv(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("non-public schema error = %v, want public-only refusal", err)
	}
}

func TestIssue7033ScopedRolloutSelectsOnlyExactMigrations(t *testing.T) {
	t.Parallel()

	definitions, err := issue7033ScopedRolloutDefinitions()
	if err != nil {
		t.Fatalf("issue7033ScopedRolloutDefinitions() error = %v", err)
	}
	if len(definitions) != 2 {
		t.Fatalf("selected definitions = %d, want 2", len(definitions))
	}
	for index, want := range []struct {
		name string
		path string
	}{
		{name: "content_files_relative_path_trgm_index", path: "go/internal/storage/postgres/migrations/130_content_files_relative_path_trgm_index.sql"},
		{name: "content_files_relative_path_trgm_index_lifecycle", path: "go/internal/storage/postgres/migrations/131_content_files_relative_path_trgm_index_lifecycle.sql"},
	} {
		if definitions[index].Name != want.name || definitions[index].Path != want.path {
			t.Fatalf("definition %d = %+v, want name=%q path=%q", index, definitions[index], want.name, want.path)
		}
		if definitions[index].SQL == "" || migrationChecksum(definitions[index].SQL) == "" {
			t.Fatalf("definition %q lacks exact embedded SQL/checksum", definitions[index].Name)
		}
	}
}

func TestIssue7033ScopedRolloutAppliesOnly130And131ThenRetriesLive(t *testing.T) {
	ctx, database, definitions := issue7033ScopedRolloutThrough124Live(t)
	config := issue7033RolloutTargetConfig(t, ctx, database)

	if err := runIssue7033ScopedRollout(ctx, database, config, issue7033TestLogger()); err != nil {
		t.Fatalf("runIssue7033ScopedRollout() error = %v", err)
	}
	for _, definition := range definitions {
		assertMigrationReceipt(t, ctx, database, definition, true)
	}
	assertContentFilesRelativePathIndexDefinition(t, ctx, database)
	assertContentSearchIndexState(t, database, "ready")

	if err := runIssue7033ScopedRollout(ctx, database, config, issue7033TestLogger()); err != nil {
		t.Fatalf("runIssue7033ScopedRollout() retry error = %v", err)
	}
	for _, definition := range definitions {
		assertMigrationReceipt(t, ctx, database, definition, true)
	}
}

func TestIssue7033ScopedRolloutThenNormalBootstrapCatchesUp125Through129Live(t *testing.T) {
	ctx, database, selected := issue7033ScopedRolloutThrough124Live(t)
	config := issue7033RolloutTargetConfig(t, ctx, database)
	deferred := make([]Definition, 0, 5)
	for _, definition := range BootstrapDefinitions() {
		for _, number := range []string{"125", "126", "127", "128", "129"} {
			if strings.Contains(definition.Path, "/"+number+"_") {
				deferred = append(deferred, definition)
				break
			}
		}
	}
	if len(deferred) != 5 {
		t.Fatalf("deferred migrations = %d, want 5", len(deferred))
	}
	for _, definition := range deferred {
		assertMigrationReceipt(t, ctx, database, definition, false)
	}
	if err := runIssue7033ScopedRollout(ctx, database, config, issue7033TestLogger()); err != nil {
		t.Fatalf("scoped rollout before unrelated migrations: %v", err)
	}
	indexAppliedAt := assertMigrationReceipt(t, ctx, database, selected[0], true)
	lifecycleAppliedAt := assertMigrationReceipt(t, ctx, database, selected[1], true)
	assertContentFilesRelativePathIndexDefinition(t, ctx, database)
	assertContentSearchIndexState(t, database, "ready")
	for _, definition := range deferred {
		assertMigrationReceipt(t, ctx, database, definition, false)
	}
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("normal bootstrap catches up migrations 125-129: %v", err)
	}
	for _, definition := range deferred {
		assertMigrationReceipt(t, ctx, database, definition, true)
	}
	if got := assertMigrationReceipt(t, ctx, database, selected[0], true); !got.Equal(indexAppliedAt) {
		t.Fatalf("migration 130 reapplied: first=%s later=%s", indexAppliedAt, got)
	}
	if got := assertMigrationReceipt(t, ctx, database, selected[1], true); !got.Equal(lifecycleAppliedAt) {
		t.Fatalf("migration 131 reapplied: first=%s later=%s", lifecycleAppliedAt, got)
	}
	assertContentFilesRelativePathIndexDefinition(t, ctx, database)
	assertContentSearchIndexState(t, database, "ready")
	var indexDefinition string
	if err := database.QueryRowContext(ctx, `SELECT pg_get_indexdef('public.service_materialization_generations_active_service_idx'::regclass)`).Scan(&indexDefinition); err != nil {
		t.Fatalf("read rescoped service index: %v", err)
	}
	if !strings.Contains(indexDefinition, "(scope_id, service_id)") {
		t.Fatalf("service index was not rescoped: %s", indexDefinition)
	}
}

func TestIssue7033ScopedRolloutRejectsWrongTargetBeforeDDL(t *testing.T) {
	ctx, database, definitions := issue7033ScopedRolloutThrough124Live(t)
	config := issue7033RolloutTargetConfig(t, ctx, database)
	config.systemIdentifier = "0"

	err := runIssue7033ScopedRollout(ctx, database, config, issue7033TestLogger())
	if err == nil || !strings.Contains(err.Error(), "target identity") {
		t.Fatalf("runIssue7033ScopedRollout() error = %v, want target identity refusal", err)
	}
	assertMigrationReceipt(t, ctx, database, definitions[0], false)
	assertMigrationReceipt(t, ctx, database, definitions[1], false)
}

func TestIssue7033ScopedRolloutRejectsNonPublicSearchPathBeforeDDL(t *testing.T) {
	ctx, database, definitions := issue7033ScopedRolloutThrough124Live(t)
	database.SetMaxOpenConns(1)
	if _, err := database.ExecContext(ctx, "CREATE SCHEMA custom"); err != nil {
		t.Fatalf("create distractor schema: %v", err)
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE custom.content_files (relative_path text)"); err != nil {
		t.Fatalf("create distractor content_files: %v", err)
	}
	if _, err := database.ExecContext(ctx, "SET search_path = custom, public"); err != nil {
		t.Fatalf("set non-public search path: %v", err)
	}
	config := issue7033RolloutTargetConfig(t, ctx, database)
	if config.schema != "custom" {
		t.Fatalf("current_schema = %q, want custom", config.schema)
	}

	err := runIssue7033ScopedRollout(ctx, database, config, issue7033TestLogger())
	if err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("runIssue7033ScopedRollout() error = %v, want public-only refusal", err)
	}
	var customIndexExists bool
	if err := database.QueryRowContext(ctx,
		"SELECT to_regclass('custom.content_files_relative_path_trgm_idx') IS NOT NULL",
	).Scan(&customIndexExists); err != nil {
		t.Fatalf("read distractor index: %v", err)
	}
	if customIndexExists {
		t.Fatal("non-public search path created an index before refusing target")
	}
	assertMigrationReceipt(t, ctx, database, definitions[0], false)
	assertMigrationReceipt(t, ctx, database, definitions[1], false)
}

func TestIssue7033ScopedRolloutRejectsIncompleteIndexStateBeforeDDL(t *testing.T) {
	ctx, database, definitions := issue7033ScopedRolloutThrough124Live(t)
	if _, err := database.ExecContext(ctx, "DROP INDEX content_files_content_trgm_idx"); err != nil {
		t.Fatalf("drop prerequisite index: %v", err)
	}

	err := runIssue7033ScopedRollout(ctx, database, issue7033RolloutTargetConfig(t, ctx, database), issue7033TestLogger())
	if err == nil || !strings.Contains(err.Error(), "prerequisite index") {
		t.Fatalf("runIssue7033ScopedRollout() error = %v, want prerequisite-index refusal", err)
	}
	assertMigrationReceipt(t, ctx, database, definitions[0], false)
	assertMigrationReceipt(t, ctx, database, definitions[1], false)
}

func TestIssue7033ScopedRolloutRejectsPrerequisiteLedgerMismatchBeforeDDL(t *testing.T) {
	ctx, database, definitions := issue7033ScopedRolloutThrough124Live(t)
	prerequisite, err := issue7033DefinitionByName(issue7033Prerequisite124Name)
	if err != nil {
		t.Fatalf("issue7033DefinitionByName(124) error = %v", err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE eshu_schema_migrations
SET checksum_sha256 = repeat('0', 64)
WHERE path = $1 AND variant = 'full'`, prerequisite.Path); err != nil {
		t.Fatalf("plant migration 124 checksum mismatch: %v", err)
	}

	err = runIssue7033ScopedRollout(ctx, database, issue7033RolloutTargetConfig(t, ctx, database), issue7033TestLogger())
	if err == nil || !strings.Contains(err.Error(), "migration 124 receipt") {
		t.Fatalf("runIssue7033ScopedRollout() error = %v, want migration receipt refusal", err)
	}
	assertMigrationReceipt(t, ctx, database, definitions[0], false)
	assertMigrationReceipt(t, ctx, database, definitions[1], false)
}

func issue7033ScopedRolloutThrough124Live(t *testing.T) (context.Context, *sql.DB, []Definition) {
	t.Helper()
	ctx, database := openContentSearchIndexLiveDB(t)
	index, lifecycle, preceding := contentFilesRelativePathIndexMigrations(t)
	preIndex := make([]Definition, 0, len(preceding))
	for _, definition := range preceding {
		deferred := false
		for _, number := range []string{"125", "126", "127", "128", "129"} {
			if strings.Contains(definition.Path, "/"+number+"_") {
				deferred = true
				break
			}
		}
		if !deferred {
			preIndex = append(preIndex, definition)
		}
	}
	if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, preIndex, issue7033TestLogger(), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("apply schema through migration 124: %v", err)
	}
	assertContentSearchIndexState(t, database, "ready")
	return ctx, database, []Definition{index, lifecycle}
}

func issue7033TestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func issue7033RolloutTargetConfig(t *testing.T, ctx context.Context, database *sql.DB) issue7033RolloutConfig {
	t.Helper()
	var systemIdentifier, databaseName, schema string
	if err := database.QueryRowContext(ctx, `
SELECT (pg_control_system()).system_identifier::text, current_database(), current_schema()`).Scan(
		&systemIdentifier, &databaseName, &schema,
	); err != nil {
		t.Fatalf("read disposable target identity: %v", err)
	}
	return issue7033RolloutConfig{
		dsn:              "postgres://disposable.invalid/eshu",
		systemIdentifier: systemIdentifier,
		database:         databaseName,
		schema:           schema,
	}
}
