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
		{name: "content_files_relative_path_trgm_index", path: "go/internal/storage/postgres/migrations/126_content_files_relative_path_trgm_index.sql"},
		{name: "content_files_relative_path_trgm_index_lifecycle", path: "go/internal/storage/postgres/migrations/127_content_files_relative_path_trgm_index_lifecycle.sql"},
	} {
		if definitions[index].Name != want.name || definitions[index].Path != want.path {
			t.Fatalf("definition %d = %+v, want name=%q path=%q", index, definitions[index], want.name, want.path)
		}
		if definitions[index].SQL == "" || migrationChecksum(definitions[index].SQL) == "" {
			t.Fatalf("definition %q lacks exact embedded SQL/checksum", definitions[index].Name)
		}
	}
}

func TestIssue7033ScopedRolloutAppliesOnly126And127ThenRetriesLive(t *testing.T) {
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
		if definition.Name != issue7033Prerequisite125Name {
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
