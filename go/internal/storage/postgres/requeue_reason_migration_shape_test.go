// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// migrationFile reads one shipped migration's SQL bytes.
func migrationFile(t *testing.T, name string) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(here), "migrations", name))
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(raw)
}

// triggerWhenClause extracts the WHEN (...) block of the named trigger
// creation from migration SQL, normalized for comparison.
func triggerWhenClause(t *testing.T, sql, trigger string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?is)CREATE TRIGGER ` + trigger + `\s+(.*?)\s*EXECUTE FUNCTION`)
	match := pattern.FindStringSubmatch(sql)
	if match == nil {
		t.Fatalf("trigger %s creation not found", trigger)
	}
	fields := strings.Fields(match[1])
	return strings.Join(fields, " ")
}

// TestCrossScopeAckTriggerMigrationConvergesReason pins the shape of the
// #11615 trigger migration (365): it replaces only the function body to
// stamp cross_scope_replay on the conversion the 093 trigger performs,
// keeps the trigger WHEN list byte-identical to 093, and carries no table
// DDL, backfill DML, or index. Shipped files 005/088/093 are never edited.
func TestCrossScopeAckTriggerMigrationConvergesReason(t *testing.T) {
	t.Parallel()

	original := migrationFile(t, "093_cross_scope_completion_queue.sql")
	converged := migrationFile(t, "365_cross_scope_ack_trigger_requeue_reason.sql")

	trigger := "fact_work_items_enforce_cross_scope_required_replay"
	if got, want := triggerWhenClause(t, converged, trigger), triggerWhenClause(t, original, trigger); got != want {
		t.Fatalf("365 trigger WHEN differs from 093:\n got: %s\nwant: %s", got, want)
	}
	for _, want := range []string{
		"CREATE OR REPLACE FUNCTION enforce_cross_scope_required_replay()",
		"NEW.last_requeue_reason := 'cross_scope_replay'",
		// The non-succeeded branch keeps the 093 behavior exactly: clear
		// the flag and return without touching the reason, because the
		// Fail/retry path owns the row's reason there.
		"IF NEW.status <> 'succeeded' THEN",
		"NEW.cross_scope_replay_required := FALSE;",
		"RETURN NEW;",
	} {
		if !strings.Contains(converged, want) {
			t.Fatalf("365 trigger migration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"ADD COLUMN",
		"CREATE INDEX",
		"CREATE TABLE",
		"INSERT INTO",
		"UPDATE fact_work_items",
		"DELETE FROM",
	} {
		if strings.Contains(strings.ToUpper(converged), forbidden) {
			t.Fatalf("365 trigger migration must be DDL-only (function + trigger guard), found %q", forbidden)
		}
	}
}

// TestRequeueReasonColumnMigrationIsCatalogOnly pins the shape of the #11615
// column migration (364): one ADD COLUMN statement, nullable TEXT, no
// default, CHECK, index, or backfill.
func TestRequeueReasonColumnMigrationIsCatalogOnly(t *testing.T) {
	t.Parallel()

	sql := migrationFile(t, "364_fact_work_items_last_requeue_reason.sql")
	// Strip line comments before shape checks: the header documents the
	// rejected alternatives (NOT NULL, backfill) by name.
	var code []string
	for _, line := range strings.Split(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		code = append(code, line)
	}
	upper := strings.ToUpper(strings.Join(code, "\n"))
	if got := strings.Count(upper, "ALTER TABLE"); got != 1 {
		t.Fatalf("364 has %d ALTER TABLE statements, want exactly 1", got)
	}
	for _, want := range []string{
		"ADD COLUMN IF NOT EXISTS last_requeue_reason TEXT NULL",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("364 column migration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"NOT NULL",
		"DEFAULT",
		"CHECK",
		"CREATE INDEX",
		"INSERT INTO",
		"UPDATE ",
		"BACKFILL",
	} {
		if strings.Contains(upper, forbidden) {
			t.Fatalf("364 column migration must be a bare nullable ADD COLUMN, found %q", forbidden)
		}
	}
}
