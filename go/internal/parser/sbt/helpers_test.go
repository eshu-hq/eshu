// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// parseSource writes body to a build.sbt in a temp dir and parses it.
func parseSource(t *testing.T, body string) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "build.sbt")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	payload, err := Parse(filepath.Dir(path), path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return payload
}

func dependencyRows(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %#v, want []map[string]any", payload["variables"])
	}
	return rows
}

func requireRow(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()
	for _, row := range dependencyRows(t, payload) {
		if row["name"] == name {
			return row
		}
	}
	t.Fatalf("no dependency row named %q in %#v", name, payload["variables"])
	return nil
}

func stateOf(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	state, ok := payload["sbt_state"].(map[string]any)
	if !ok {
		t.Fatalf("sbt_state = %#v, want map[string]any", payload["sbt_state"])
	}
	return state
}

func requireField(t *testing.T, row map[string]any, key string, want any) {
	t.Helper()
	if got := row[key]; got != want {
		t.Fatalf("%v %s = %#v, want %#v", row["name"], key, got, want)
	}
}
