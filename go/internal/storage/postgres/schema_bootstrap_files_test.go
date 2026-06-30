// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplyBootstrapExecutesDefinitionsInOrder(t *testing.T) {
	t.Parallel()

	exec := &recordingExecutor{}
	if err := ApplyBootstrap(context.Background(), exec); err != nil {
		t.Fatalf("ApplyBootstrap() error = %v, want nil", err)
	}

	got := exec.statements
	defs := BootstrapDefinitions()
	want := make([]string, 0, len(defs))
	for _, def := range defs {
		want = append(want, def.SQL)
	}
	if len(got) != len(want) {
		t.Fatalf("ApplyBootstrap() executed %d statements, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("statement[%d] mismatch\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
}

func TestBootstrapSQLFilesMirrorDefinitions(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	for _, def := range BootstrapDefinitions() {
		path := filepath.Join(repoRoot, def.Path)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if strings.TrimSpace(string(got)) != strings.TrimSpace(def.SQL) {
			t.Fatalf("file %q does not match bootstrap definition %q", path, def.Name)
		}
	}
}

func TestValidateDefinitionsRejectsBlankValues(t *testing.T) {
	t.Parallel()

	err := ValidateDefinitions([]Definition{{Name: " ", Path: "x.sql", SQL: "SELECT 1;"}})
	if err == nil {
		t.Fatal("ValidateDefinitions() error = nil, want non-nil")
	}
}

func TestApplyDefinitionsUsesSessionLockTimeoutWhenSupported(t *testing.T) {
	t.Parallel()

	exec := &recordingLockTimeoutExecutor{}
	defs := []Definition{
		{Name: "alpha", Path: "001_alpha.sql", SQL: "CREATE TABLE IF NOT EXISTS alpha(id TEXT);"},
		{Name: "beta", Path: "002_beta.sql", SQL: "CREATE TABLE IF NOT EXISTS beta(id TEXT);"},
	}
	if err := ApplyDefinitionsWithLockTimeout(context.Background(), exec, defs, 750*time.Millisecond); err != nil {
		t.Fatalf("ApplyDefinitionsWithLockTimeout() error = %v, want nil", err)
	}
	if len(exec.statements) != len(defs) {
		t.Fatalf("statements = %d, want %d", len(exec.statements), len(defs))
	}
	for i, statement := range exec.statements {
		if statement != defs[i].SQL {
			t.Fatalf("statement %d = %q, want %q", i, statement, defs[i].SQL)
		}
		if exec.lockTimeouts[i] != 750*time.Millisecond {
			t.Fatalf("lock timeout %d = %s, want 750ms", i, exec.lockTimeouts[i])
		}
	}
}

func TestConcurrentIndexNamesForInvalidCleanup(t *testing.T) {
	t.Parallel()

	sql := `
CREATE TABLE IF NOT EXISTS example(id INTEGER);
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS example_value_idx
ON example (id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS "example_id_idx"
ON example (id);
`
	got := concurrentIndexNamesForInvalidCleanup(sql)
	want := []string{"example_value_idx", "example_id_idx"}
	if len(got) != len(want) {
		t.Fatalf("index names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index name %d = %q, want %q", i, got[i], want[i])
		}
	}
}

type recordingExecutor struct {
	statements []string
}

func (e *recordingExecutor) ExecContext(_ context.Context, statement string, _ ...any) (sql.Result, error) {
	e.statements = append(e.statements, statement)
	return result{}, nil
}

type result struct{}

func (result) LastInsertId() (int64, error) { return 0, nil }

func (result) RowsAffected() (int64, error) { return 0, nil }

type recordingLockTimeoutExecutor struct {
	statements   []string
	lockTimeouts []time.Duration
}

func (e *recordingLockTimeoutExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	panic("ApplyDefinitionsWithLockTimeout should use lock-timeout execution when available")
}

func (e *recordingLockTimeoutExecutor) execContextWithLockTimeout(
	_ context.Context,
	statement string,
	lockTimeout time.Duration,
) (sql.Result, error) {
	e.statements = append(e.statements, statement)
	e.lockTimeouts = append(e.lockTimeouts, lockTimeout)
	return result{}, nil
}
