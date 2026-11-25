// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strings"
	"testing"
)

// The winners dirty-capture functions that read finding facts read the supports
// table instead (#10880 PR-C1 e). Migration 352 replaces the scope-delete
// capture; migration 353 replaces the scanner pair (public-only, like 311). Each
// new function body must be the previous migration's body with the row source
// swapped and nothing else changed: the tests derive the expected text from the
// previous migration file and compare byte for byte. Migration 355 reunites the
// scope-delete capture over both sources (a scope retired before backfill still
// stages): its body must be 352's with the single-source read replaced by the
// UNION of the fact arm (310's predicates) and the supports arm, derived the
// same way.

func readMigrationFile(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(raw)
}

// functionBody returns the text of one CREATE OR REPLACE FUNCTION statement,
// from its CREATE line through the closing $function$; delimiter.
func functionBody(t *testing.T, migration, function string) string {
	t.Helper()
	marker := "CREATE OR REPLACE FUNCTION " + function + "()"
	start := strings.Index(migration, marker)
	if start < 0 {
		t.Fatalf("function %s not found", function)
	}
	end := strings.Index(migration[start:], "$function$;\n")
	if end < 0 {
		t.Fatalf("function %s has no closing delimiter", function)
	}
	return migration[start : start+end+len("$function$;\n")]
}

const (
	scopeDeleteFunction     = "supply_chain_impact_winners_capture_scope_delete"
	scannerFunction         = "supply_chain_impact_winners_capture_scanner"
	scannerDeleteFunction   = "supply_chain_impact_winners_capture_scanner_delete"
	activationFunction      = "supply_chain_impact_winners_capture_activation"
	previousScopeDeleteFile = "310_supply_chain_impact_winners_staging_capture.sql"
	previousScannerFile     = "311_supply_chain_impact_winners_scanner_staging_capture.sql"
	supportsScopeDeleteFile = "352_supply_chain_impact_winners_supports_scope_capture.sql"
	supportsScannerFile     = "353_supply_chain_impact_winners_supports_scanner_capture.sql"
	reunitedScopeDeleteFile = "355_supply_chain_impact_winners_scope_capture_facts_and_supports.sql"
)

// TestSupportsScopeDeleteCaptureIsTheFactCaptureOverSupports proves the
// replacement scope-delete capture is migration 310's body with the row source
// swapped (the table, and the kind and tombstone predicates a support makes
// implicit) and nothing else, and that it reads no fact row.
func TestSupportsScopeDeleteCaptureIsTheFactCaptureOverSupports(t *testing.T) {
	t.Parallel()

	previous := functionBody(t, readMigrationFile(t, previousScopeDeleteFile), scopeDeleteFunction)
	want := strings.Replace(previous,
		"    FROM fact_records AS fact\n    WHERE fact.scope_id = OLD.scope_id\n      AND fact.fact_kind = 'reducer_supply_chain_impact_finding'\n      AND fact.is_tombstone = FALSE;",
		"    FROM supply_chain_impact_finding_supports AS fact\n    WHERE fact.scope_id = OLD.scope_id;", 1)
	if want == previous {
		t.Fatal("the swap did not apply to migration 310's scope-delete capture: the fragment changed")
	}
	got := functionBody(t, readMigrationFile(t, supportsScopeDeleteFile), scopeDeleteFunction)
	if got != want {
		t.Fatalf("migration 352's scope-delete capture is not migration 310's with the row source swapped:\n--- got\n%s\n--- want\n%s", got, want)
	}
	if strings.Contains(got, "fact_records") {
		t.Fatal("the replacement scope-delete capture still reads fact_records")
	}
}

// TestReunitedScopeDeleteCaptureReadsFactsAndSupports proves migration 355's
// scope-delete capture is migration 352's body with the single-source read
// replaced by the UNION of the fact arm (migration 310's kind and tombstone
// predicates) and the supports arm, and nothing else: a scope retired before
// its facts are backfilled stages its keys, and the dual-written overlap
// collapses to one mark per key.
func TestReunitedScopeDeleteCaptureReadsFactsAndSupports(t *testing.T) {
	t.Parallel()

	previous := functionBody(t, readMigrationFile(t, supportsScopeDeleteFile), scopeDeleteFunction)
	supportsRead := "    FROM supply_chain_impact_finding_supports AS fact\n    WHERE fact.scope_id = OLD.scope_id;"
	factArm := "    FROM fact_records AS fact\n" +
		"    WHERE fact.scope_id = OLD.scope_id\n" +
		"      AND fact.fact_kind = 'reducer_supply_chain_impact_finding'\n" +
		"      AND fact.is_tombstone = FALSE\n" +
		"    UNION\n" +
		"    SELECT DISTINCT 'fact', v_source,\n" +
		"           COALESCE(fact.payload->>'package_id', ''),\n" +
		"           COALESCE(fact.payload->>'repository_id', ''),\n" +
		"           COALESCE(fact.payload->>'subject_digest', ''),\n" +
		"           COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),\n" +
		"           COALESCE(fact.payload->>'advisory_id', '')\n" +
		"    FROM supply_chain_impact_finding_supports AS fact\n" +
		"    WHERE fact.scope_id = OLD.scope_id;"
	want := strings.Replace(previous, supportsRead, factArm, 1)
	if want == previous {
		t.Fatal("the reunion did not apply to migration 352's scope-delete capture: the read fragment changed")
	}
	got := functionBody(t, readMigrationFile(t, reunitedScopeDeleteFile), scopeDeleteFunction)
	if got != want {
		t.Fatalf("migration 355's scope-delete capture is not migration 352's reading both sources:\n--- got\n%s\n--- want\n%s", got, want)
	}
	if !strings.Contains(got, "fact_records") || !strings.Contains(got, "supply_chain_impact_finding_supports") {
		t.Fatal("the reunited scope-delete capture must read both fact_records and the supports table")
	}
}

// TestSupportsScannerCapturesAreTheFactCapturesOverSupports does the same for
// the scanner insert and delete captures against migration 311.
func TestSupportsScannerCapturesAreTheFactCapturesOverSupports(t *testing.T) {
	t.Parallel()

	previous := readMigrationFile(t, previousScannerFile)
	current := readMigrationFile(t, supportsScannerFile)
	for function, transition := range map[string]string{scannerFunction: "new_rows", scannerDeleteFunction: "old_rows"} {
		prev := functionBody(t, previous, function)
		want := strings.Replace(prev,
			"    JOIN public.fact_records AS fact ON fact.fact_id = "+transition+".finding_fact_id\n    WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding';",
			"    JOIN public.supply_chain_impact_finding_supports AS fact ON fact.fact_id = "+transition+".finding_fact_id;", 1)
		if want == prev {
			t.Fatalf("the swap did not apply to migration 311's %s: the fragment changed", function)
		}
		got := functionBody(t, current, function)
		if got != want {
			t.Errorf("migration 353's %s is not migration 311's with the row source swapped:\n--- got\n%s\n--- want\n%s", function, got, want)
		}
		if strings.Contains(got, "fact_records") {
			t.Errorf("the replacement %s still reads fact_records", function)
		}
	}
}

// TestSupportsCaptureMigrationsRedefineOnlyTheFunctionsThatReadFacts proves the
// activation capture, which reads no fact row, is not redefined, and that each
// new migration keeps the security shape of the one it replaces: SECURITY
// DEFINER, a pinned search_path, and a PUBLIC revoke.
func TestSupportsCaptureMigrationsRedefineOnlyTheFunctionsThatReadFacts(t *testing.T) {
	t.Parallel()

	scope := readMigrationFile(t, supportsScopeDeleteFile)
	scanner := readMigrationFile(t, supportsScannerFile)
	reunited := readMigrationFile(t, reunitedScopeDeleteFile)
	if strings.Contains(scope, activationFunction+"()\nRETURNS") || strings.Contains(scanner, activationFunction+"()\nRETURNS") || strings.Contains(reunited, activationFunction+"()\nRETURNS") {
		t.Error("a supports capture migration redefines the activation capture, which reads no fact row")
	}
	if got := strings.Count(scope, "CREATE OR REPLACE FUNCTION"); got != 1 {
		t.Errorf("migration 352 redefines %d functions, want 1 (the scope-delete capture)", got)
	}
	if got := strings.Count(reunited, "CREATE OR REPLACE FUNCTION"); got != 1 {
		t.Errorf("migration 355 redefines %d functions, want 1 (the scope-delete capture)", got)
	}
	if got := strings.Count(scanner, "CREATE OR REPLACE FUNCTION"); got != 2 {
		t.Errorf("migration 353 redefines %d functions, want 2 (the scanner pair)", got)
	}
	for name, text := range map[string]string{"352": scope, "353": scanner, "355": reunited} {
		if !strings.Contains(text, "SECURITY DEFINER") {
			t.Errorf("migration %s lost SECURITY DEFINER", name)
		}
		if !strings.Contains(text, "FROM PUBLIC;") {
			t.Errorf("migration %s lost the PUBLIC revoke", name)
		}
		if strings.Contains(text, "ON CONFLICT") || strings.Contains(text, "supply_chain_impact_winners_dirty_log") {
			t.Errorf("migration %s writes the versioned dirty log or arbitrates a conflict: capture must stage", name)
		}
	}
	if !strings.Contains(scope, "ALTER FUNCTION "+scopeDeleteFunction+"() SET search_path FROM CURRENT;") {
		t.Error("migration 352 does not pin the scope-delete capture's search_path as 310 does")
	}
	if !strings.Contains(scanner, "SET search_path = pg_catalog, public, pg_temp") {
		t.Error("migration 353 does not carry 311's pinned public search_path")
	}
}
