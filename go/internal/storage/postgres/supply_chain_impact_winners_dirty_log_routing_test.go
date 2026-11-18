// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// winnersDirtyLogInsertPattern matches a direct write of the versioned dirty
// log with or without a schema qualifier: migrations 272a and 308 spell it
// `INSERT INTO public.supply_chain_impact_winners_dirty_log`, which a plain
// literal match would miss. Capture owners stage into
// supply_chain_impact_winners_dirty_staging instead (#10837 direction C), so
// no production statement outside the collapse and the convert may carry it.
var winnersDirtyLogInsertPattern = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+(?:public\.)?supply_chain_impact_winners_dirty_log\b`)

// winnersDirtyLogWriterAllowlist names the only non-test Go files that may
// write the versioned dirty log directly: the two collapse arms (fact and its
// scope-kind twin) and the scope-remainder convert. A future capture owner
// that adds a direct log write anywhere else fails this gate.
var winnersDirtyLogWriterAllowlist = map[string]bool{
	"supply_chain_impact_winners_staging_collapse.go": true,
	"supply_chain_impact_winners_keyed_settle_sql.go": true,
}

// winnersDirtyLogMigrationAllowlist names the trigger migrations that wrote
// the versioned dirty log directly before direction C retargeted capture
// triggers at staging: 272/307 (unqualified) and 272a/308 (schema-qualified),
// all superseded by the 310/311 staging triggers. Migrations are append-only
// history; a new migration carrying a direct log write is a new capture
// owner that bypasses the staging queue and fails this gate.
var winnersDirtyLogMigrationAllowlist = map[string]bool{
	"272_supply_chain_impact_winners_dirty_log.sql":              true,
	"272a_scanner_winners_dirty_capture.sql":                     true,
	"307_supply_chain_impact_winners_dirty_capture_upsert.sql":   true,
	"308_supply_chain_impact_winners_scanner_capture_upsert.sql": true,
}

// TestWinnersDirtyLogWritesStayInCollapseAndConvert is the #10851 routing
// guard: it scans every non-test Go file of this package for a direct write
// of the versioned dirty log and fails on any file outside the collapse and
// the convert. Test fixtures seed the log directly (they are not capture
// owners), so _test.go files are exempt. It also pins the scope-kind twin:
// both the fact and the scope ON CONFLICT tails must stay in the collapse,
// and the convert must stay a fact-kind versioned upsert.
func TestWinnersDirtyLogWritesStayInCollapseAndConvert(t *testing.T) {
	t.Parallel()

	var offenders []string
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Strip full-line comments before matching so prose mentioning
		// the statement does not count, then match the whole file at
		// once: a statement split across lines must not evade the guard.
		var code []string
		for _, line := range strings.Split(string(body), "\n") {
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "//") {
				continue
			}
			code = append(code, line)
		}
		if winnersDirtyLogInsertPattern.MatchString(strings.Join(code, "\n")) && !winnersDirtyLogWriterAllowlist[name] {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk package directory: %v", err)
	}
	if len(offenders) != 0 {
		t.Errorf("direct dirty-log writers outside collapse/convert: %v (route new capture owners through the staging queue)", offenders)
	}

	if !strings.Contains(collapseWinnersStagingFactSQL, "ON CONFLICT (package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'") {
		t.Error("collapse lost its fact arm: the scope-kind twin needs both")
	}
	if !strings.Contains(collapseWinnersStagingScopeSQL, "ON CONFLICT (scope_id, generation_id) WHERE kind = 'scope'") {
		t.Error("collapse lost its scope arm: the fact arm alone strands scope marks")
	}
	if !winnersDirtyLogInsertPattern.MatchString(convertWinnersKeyedScopeRemainderSQL) ||
		!strings.Contains(convertWinnersKeyedScopeRemainderSQL, "ON CONFLICT (package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'") {
		t.Error("scope-remainder convert is not the pinned fact-kind versioned upsert")
	}
}

// TestWinnersDirtyLogTriggerMigrationsStayAllowlisted fails on any new
// trigger migration that writes the versioned dirty log directly: under
// direction C capture triggers stage, and the tick collapse owns the only
// log writes a tick makes before admission.
func TestWinnersDirtyLogTriggerMigrationsStayAllowlisted(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations directory: %v", err)
	}
	var offenders []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if winnersDirtyLogInsertPattern.MatchString(string(body)) && !winnersDirtyLogMigrationAllowlist[name] {
			offenders = append(offenders, name)
		}
	}
	if len(offenders) != 0 {
		t.Errorf("new migrations writing the dirty log directly: %v (capture triggers must stage)", offenders)
	}
}

// TestWinnersWriteConflictClassMapsPostgresCodes pins the #10851 failure
// classification the writer and tick conflict counter uses: a deadlock
// (40P01) counts as deadlock, a serialization failure (40001) as
// serialization_failure, and anything else — including a nil error — counts
// nothing. The counter ships with no retry: the classification only names
// the victim.
func TestWinnersWriteConflictClassMapsPostgresCodes(t *testing.T) {
	t.Parallel()

	for code, want := range map[string]string{
		"40P01": "deadlock",
		"40001": "serialization_failure",
		"23505": "",
		"57014": "",
	} {
		if got := winnersWriteConflictClass(&pgconn.PgError{Code: code}); got != want {
			t.Errorf("winnersWriteConflictClass(%q) = %q, want %q", code, got, want)
		}
	}
	if got := winnersWriteConflictClass(nil); got != "" {
		t.Errorf("winnersWriteConflictClass(nil) = %q, want no class", got)
	}
	if got := winnersWriteConflictClass(os.ErrClosed); got != "" {
		t.Errorf("winnersWriteConflictClass(non-pg error) = %q, want no class", got)
	}
}
