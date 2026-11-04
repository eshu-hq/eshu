// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import (
	"path"
	"regexp"
	"strings"
	"testing"
)

const (
	factRecordsAutovacuumName = "fact_records_autovacuum_reloptions"
	factRecordsAutovacuumPath = "go/internal/storage/postgres/migrations/278_fact_records_autovacuum_reloptions.sql"
	// factRecordsAutovacuumStatement is the single statement #9975 ruled for
	// this table: two storage parameters, no cost knobs, no toast.* values,
	// no analyze knobs.
	factRecordsAutovacuumStatement = "ALTER TABLE fact_records SET (autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_insert_scale_factor = 0.05);"
)

var (
	sqlLineCommentPattern = regexp.MustCompile(`(?m)--[^\n]*$`)
	sqlWhitespacePattern  = regexp.MustCompile(`\s+`)
)

// factRecordsAutovacuumDefinition returns the shipped definition, failing the
// test when the migration is missing.
func factRecordsAutovacuumDefinition(t *testing.T) Definition {
	t.Helper()
	for _, def := range BootstrapDefinitions() {
		if def.Name == factRecordsAutovacuumName {
			return def
		}
	}
	t.Fatalf("BootstrapDefinitions() has no %q migration", factRecordsAutovacuumName)
	return Definition{}
}

// TestFactRecordsAutovacuumReloptionsMigrationShape pins the #9975 ruling:
// one catalog-only ALTER that sets exactly the two scale factors on
// fact_records. A cost knob, a toast.* parameter, an analyze knob, or a second
// statement would each be an unmeasured theory (Prove-The-Theory-First) or
// would hold the SHARE UPDATE EXCLUSIVE lock longer than the single ALTER.
func TestFactRecordsAutovacuumReloptionsMigrationShape(t *testing.T) {
	def := factRecordsAutovacuumDefinition(t)
	if def.Path != factRecordsAutovacuumPath {
		t.Fatalf("migration path = %q, want %q", def.Path, factRecordsAutovacuumPath)
	}
	if got := path.Base(def.Path); !strings.HasPrefix(got, "278_") {
		t.Fatalf("migration file %q must take sequence 278 (277 is the highest on main and 276 is held by another lane)", got)
	}

	code := strings.TrimSpace(sqlLineCommentPattern.ReplaceAllString(def.SQL, ""))
	code = sqlWhitespacePattern.ReplaceAllString(code, " ")
	code = strings.ReplaceAll(code, "( ", "(")
	code = strings.ReplaceAll(code, " )", ")")
	if code != factRecordsAutovacuumStatement {
		t.Fatalf("migration statement =\n%q\nwant exactly\n%q", code, factRecordsAutovacuumStatement)
	}
	if strings.Count(code, ";") != 1 {
		t.Fatalf("migration must be a single statement, found %d semicolons", strings.Count(code, ";"))
	}

	// The header comment may name what it deliberately omits, so only the
	// statement text is checked.
	for _, banned := range []string{
		"cost_delay", "cost_limit", "toast.", "analyze", "reset", "vacuum full", "fillfactor",
	} {
		if strings.Contains(strings.ToLower(code), banned) {
			t.Errorf("migration statement must not carry %q", banned)
		}
	}
}
