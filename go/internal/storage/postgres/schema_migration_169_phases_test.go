// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

const migration169Checksum = "28871f0d7b0bc493b3d22949ac475b98129bc3668fe93e1d1409543714a3878b"

func migration169Definition(t *testing.T) Definition {
	t.Helper()
	for _, def := range BootstrapDefinitions() {
		if def.Name == "content_files_generation_id" {
			if got := migrationChecksum(def.SQL); got != migration169Checksum {
				t.Fatalf("migration 169 checksum = %s, want %s", got, migration169Checksum)
			}
			return def
		}
	}
	t.Fatal("migration 169 not found")
	return Definition{}
}

func TestMigration169UntrackedApplyCommitsThreeExactPhases(t *testing.T) {
	t.Parallel()
	def := migration169Definition(t)
	update := strings.Index(def.SQL, "\n\nUPDATE content_files AS f\n")
	analyze := strings.Index(def.SQL, "\n\nANALYZE content_files;")
	if update < 0 || analyze <= update {
		t.Fatal("migration 169 phase boundaries changed")
	}
	want := []string{def.SQL[:update], def.SQL[update:analyze], def.SQL[analyze:]}
	for _, tc := range []struct {
		name string
		exec func([]Definition) ([]string, error)
	}{
		{"plain", func(defs []Definition) ([]string, error) {
			exec := &recordingExecutor{}
			err := ApplyDefinitions(context.Background(), exec, defs)
			return exec.statements, err
		}},
		{"lock-timeout", func(defs []Definition) ([]string, error) {
			exec := &recordingLockTimeoutExecutor{}
			err := ApplyDefinitionsWithLockTimeout(context.Background(), exec, defs, time.Second)
			if err == nil {
				for i, timeout := range exec.lockTimeouts {
					if timeout != time.Second {
						t.Errorf("phase %d lock timeout = %s, want 1s", i, timeout)
					}
				}
			}
			return exec.statements, err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.exec([]Definition{def})
			if err != nil {
				t.Fatalf("apply 169: %v", err)
			}
			if len(got) != len(want) {
				t.Fatalf("executed %d phases, want %d", len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("phase %d differs from exact migration slice", i)
				}
			}
		})
	}
}

func TestMigration169RejectsChangedPayloadOrVariantBeforeExecution(t *testing.T) {
	t.Parallel()
	def := migration169Definition(t)
	for _, changed := range []Definition{
		func() Definition { copy := def; copy.SQL += "\n-- drift"; return copy }(),
		func() Definition {
			copy := def
			copy.Variant = "deferred"
			copy.FullChecksum = migration169Checksum
			return copy
		}(),
	} {
		exec := &recordingExecutor{}
		if err := ApplyDefinitions(context.Background(), exec, []Definition{changed}); err == nil {
			t.Fatal("changed migration 169 accepted")
		}
		if len(exec.statements) != 0 {
			t.Fatalf("changed migration 169 executed %d statements", len(exec.statements))
		}
	}
}

func TestOtherMigrationsRetainSingleStatementCall(t *testing.T) {
	t.Parallel()
	def := Definition{Name: "example", Path: "169_other.sql", SQL: "CREATE TABLE example(id int); INSERT INTO example VALUES (1);"}
	exec := &recordingExecutor{}
	if err := ApplyDefinitions(context.Background(), exec, []Definition{def}); err != nil {
		t.Fatalf("apply unrelated migration: %v", err)
	}
	if len(exec.statements) != 1 || exec.statements[0] != def.SQL {
		t.Fatalf("unrelated migration statements = %v, want one byte-identical call", exec.statements)
	}
}
