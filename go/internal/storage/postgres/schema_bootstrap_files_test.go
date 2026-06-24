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
