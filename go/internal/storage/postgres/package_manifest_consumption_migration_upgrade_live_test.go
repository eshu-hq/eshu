// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func TestPackageManifestConsumptionMigrationsUpgradeAfterSecretLinesLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	definitions := BootstrapDefinitions()
	const secretLinesMigration = "138_content_file_secret_lines.sql"
	boundary := 0
	for index, definition := range definitions {
		if path.Base(definition.Path) == secretLinesMigration {
			boundary = index + 1
			break
		}
	}
	if boundary == 0 {
		t.Fatalf("bootstrap definitions omit %s", secretLinesMigration)
	}
	wantNext := []string{
		"139_package_manifest_consumption_keys.sql",
		"140_supply_chain_readiness_target_indexes.sql",
		"141_supply_chain_readiness_sbom_component_index.sql",
		"142_supply_chain_readiness_package_registry_index.sql",
		"143_package_registry_identity_keys.sql",
		"144_supply_chain_readiness_sbom_warning_document_index.sql",
	}
	if len(definitions) < boundary+len(wantNext) {
		t.Fatalf("bootstrap definitions after %s = %d, want at least %d", secretLinesMigration, len(definitions)-boundary, len(wantNext))
	}
	for offset, filename := range wantNext {
		if got := path.Base(definitions[boundary+offset].Path); got != filename {
			t.Fatalf("bootstrap definition after %s at offset %d = %q, want %q", secretLinesMigration, offset, got, filename)
		}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions[:boundary], logger, schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("apply shipped migrations through %s: %v", secretLinesMigration, err)
	}
	assertMigrationLedgerCount(t, ctx, database, boundary)
	for _, table := range []string{"package_manifest_consumption_keys", "package_registry_identity_keys"} {
		assertMigrationTablePresence(t, ctx, database, table, false)
	}
	if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions, logger, schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("upgrade through package-consumption migrations: %v", err)
	}
	assertMigrationLedgerCount(t, ctx, database, len(definitions))
	for _, table := range []string{"package_manifest_consumption_keys", "package_registry_identity_keys"} {
		assertMigrationTablePresence(t, ctx, database, table, true)
	}
	if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions, logger, schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("reapply package-consumption migrations: %v", err)
	}
	assertMigrationLedgerCount(t, ctx, database, len(definitions))
}

func assertMigrationTablePresence(t *testing.T, ctx context.Context, database *sql.DB, table string, want bool) {
	t.Helper()
	var got bool
	if err := database.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&got); err != nil {
		t.Fatalf("check migration table %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("migration table %s exists = %t, want %t", table, got, want)
	}
}

func assertMigrationLedgerCount(t *testing.T, ctx context.Context, database *sql.DB, want int) {
	t.Helper()
	var got int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM eshu_schema_migrations").Scan(&got); err != nil {
		t.Fatalf("read schema migration ledger count: %v", err)
	}
	if got != want {
		t.Fatalf("schema migration ledger count = %d, want %d", got, want)
	}
}
