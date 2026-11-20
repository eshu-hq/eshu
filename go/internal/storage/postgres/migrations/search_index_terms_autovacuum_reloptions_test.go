// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import (
	"regexp"
	"strings"
	"testing"
)

// searchIndexTermsAutovacuumDefinition returns the shipped definition,
// failing the test when the migration is missing.
func searchIndexTermsAutovacuumDefinition(t *testing.T) Definition {
	t.Helper()
	for _, def := range BootstrapDefinitions() {
		if def.Name == "search_index_terms_autovacuum_reloptions" {
			return def
		}
	}
	t.Fatalf("BootstrapDefinitions() has no %q migration", "search_index_terms_autovacuum_reloptions")
	return Definition{}
}

// TestSearchIndexTermsAutovacuumReloptionsMigrationShape pins the #10874
// ruling: one catalog-only DO block that sets exactly the two scale factors
// on every terms leaf plus the documents table. A cost knob, a toast.*
// parameter, an analyze knob, a fillfactor change, a VACUUM call, or a
// second statement would each be an unmeasured theory
// (Prove-The-Theory-First) or would hold a stronger lock longer than the
// per-leaf SHARE UPDATE EXCLUSIVE ALTERs.
func TestSearchIndexTermsAutovacuumReloptionsMigrationShape(t *testing.T) {
	def := searchIndexTermsAutovacuumDefinition(t)
	const wantPath = "go/internal/storage/postgres/migrations/313_search_index_terms_autovacuum_reloptions.sql"
	if def.Path != wantPath {
		t.Fatalf("migration path = %q, want %q", def.Path, wantPath)
	}

	code := strings.TrimSpace(sqlLineCommentPattern.ReplaceAllString(def.SQL, ""))
	code = sqlWhitespacePattern.ReplaceAllString(code, " ")
	code = strings.ReplaceAll(code, "( ", "(")
	code = strings.ReplaceAll(code, " )", ")")
	// One statement: a single DO block, so semicolons inside the block are
	// expected. The shape pin is the single DO wrapper plus exactly the two
	// ALTER TABLE arms (one per-leaf EXECUTE, one direct for documents).
	if !strings.HasPrefix(code, "DO $$") || !strings.HasSuffix(code, "END $$;") {
		t.Fatalf("migration must be a single DO block, got prefix/suffix:\n%.60q\n%.60q", code, code[max(0, len(code)-60):])
	}
	if got := strings.Count(code, "ALTER TABLE"); got != 2 {
		t.Fatalf("migration ALTER TABLE arms = %d, want 2 (per-leaf EXECUTE plus documents)", got)
	}

	// The header comment may name what it deliberately omits, so only the
	// statement text is checked.
	lowered := strings.ToLower(code)
	for _, banned := range []string{
		"cost_delay", "cost_limit", "toast.", "analyze", "reset", "vacuum full", "fillfactor", "cluster",
	} {
		if strings.Contains(lowered, banned) {
			t.Errorf("migration statement must not carry %q", banned)
		}
	}
	// A bare VACUUM (not inside "autovacuum_") would rewrite heap files on
	// the live primary; the migration must never issue one.
	if matched, _ := regexp.MatchString(`(?i)(^|[^_a-z])vacuum( |;|$)`, lowered); matched {
		t.Error("migration statement must not issue VACUUM")
	}

	for _, want := range []string{
		"autovacuum_vacuum_scale_factor = 0.02",
		"autovacuum_vacuum_insert_scale_factor = 0.05",
		"pg_inherits",
		"eshu_search_index_terms",
		"eshu_search_index_documents",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("migration statement must carry %q", want)
		}
	}
}
