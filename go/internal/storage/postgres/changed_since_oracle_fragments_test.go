// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	changedSinceOracleFragmentsUpdateEnv = "ESHU_UPDATE_CHANGED_SINCE_ORACLE_FRAGMENTS"
	changedSinceOracleFragmentsRelPath   = "scripts/lib/golden-corpus-changed-since-sql-fragments.sh"
)

// changedSinceOracleFragments maps each shell variable the golden-corpus
// changed-since oracle reads to the Go constant that owns its SQL text. The
// oracle in scripts/lib/golden-corpus-changed-since.sh compares its own count of
// the diff against the live get_changed_since response, so both must classify
// with exactly the fragments the API statement uses (#7127).
func changedSinceOracleFragments() [][2]string {
	return [][2]string{
		{"golden_changed_since_digest_input", changedSincePayloadDigestInput},
		{"golden_changed_since_exclude_reducer_kinds", changedSinceExcludeReducerDerivedKinds},
	}
}

// renderChangedSinceOracleFragments renders the generated shell file. Values are
// double-quoted with backslash, double quote, dollar and backtick escaped so the
// shell evaluates each one back to the exact Go constant bytes.
func renderChangedSinceOracleFragments() string {
	escape := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`")
	var out strings.Builder
	out.WriteString("#!/usr/bin/env bash\n")
	out.WriteString("# SPDX-License-Identifier: MIT\n")
	out.WriteString("# Copyright (c) 2025-2026 eshu-hq\n")
	out.WriteString("#\n")
	out.WriteString("# GENERATED FILE - DO NOT EDIT. Regenerate with:\n")
	out.WriteString("#   " + changedSinceOracleFragmentsUpdateEnv + "=1 go test ./internal/storage/postgres -run ChangedSinceOracleFragments\n")
	out.WriteString("# The SQL fragments the golden-corpus changed-since oracle shares with the\n")
	out.WriteString("# get_changed_since statement (go/internal/storage/postgres/changed_since_sql.go).\n")
	out.WriteString("# shellcheck disable=SC2034\n")
	for _, fragment := range changedSinceOracleFragments() {
		out.WriteString(fragment[0] + `="` + escape.Replace(fragment[1]) + `"` + "\n")
	}
	return out.String()
}

// TestChangedSinceOracleFragmentsMatchGoConstants keeps the golden-corpus oracle
// derived from the API statement. It fails when the committed fragments file no
// longer equals the Go constants, and it proves the file by evaluating it in a
// shell rather than by comparing text alone.
func TestChangedSinceOracleFragmentsMatchGoConstants(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", filepath.FromSlash(changedSinceOracleFragmentsRelPath))
	want := renderChangedSinceOracleFragments()
	if os.Getenv(changedSinceOracleFragmentsUpdateEnv) == "1" {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with %s=1)", path, err, changedSinceOracleFragmentsUpdateEnv)
	}
	if string(got) != want {
		t.Fatalf("%s is stale against changed_since_sql.go; regenerate with %s=1 go test ./internal/storage/postgres -run ChangedSinceOracleFragments", changedSinceOracleFragmentsRelPath, changedSinceOracleFragmentsUpdateEnv)
	}
	for _, fragment := range changedSinceOracleFragments() {
		out, err := exec.Command("bash", "-c", `. "$1"; printf %s "${!2}"`, "bash", path, fragment[0]).Output()
		if err != nil {
			t.Fatalf("evaluate %s: %v", fragment[0], err)
		}
		if string(out) != fragment[1] {
			t.Fatalf("%s evaluates to %q, want %q", fragment[0], out, fragment[1])
		}
	}
}
