// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestCICDRunWatermarkSchemaMatchesBootstrapMigration proves the Go DDL
// constant (CICDRunWatermarkSchemaSQL, used by CICDRunWatermarkStore's
// idempotent EnsureSchema convenience) and the embedded bootstrap migration
// (migrations/078_cicd_run_watermarks.sql, the actual production bootstrap
// path via BootstrapDefinitions) stay in lockstep. AWS pagination
// checkpoints keep an equivalent pair maintained manually with no
// cross-check; this test exists so a future edit to one side does not
// silently drift from the other.
func TestCICDRunWatermarkSchemaMatchesBootstrapMigration(t *testing.T) {
	t.Parallel()

	var migrationSQL string
	for _, def := range BootstrapDefinitions() {
		if def.Name == "cicd_run_watermarks" {
			migrationSQL = def.SQL
			break
		}
	}
	if migrationSQL == "" {
		t.Fatal("BootstrapDefinitions() has no cicd_run_watermarks entry; check migrations/078_cicd_run_watermarks.sql exists")
	}
	if got, want := strings.TrimSpace(migrationSQL), strings.TrimSpace(CICDRunWatermarkSchemaSQL()); got != want {
		t.Fatalf("migration SQL and CICDRunWatermarkSchemaSQL() drifted:\nmigration:\n%s\n\nconstant:\n%s", got, want)
	}
}
