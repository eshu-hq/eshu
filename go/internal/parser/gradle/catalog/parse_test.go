// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func writeCatalog(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gradle", "libs.versions.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseReturnsPayloadWithRowsAndStateEnvelope(t *testing.T) {
	t.Parallel()
	path := writeCatalog(t, positiveCatalog)
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if payload["lang"] != "gradle" || payload["path"] != path {
		t.Fatalf("payload lang/path = %v/%v", payload["lang"], payload["path"])
	}
	rows, _ := payload["variables"].([]map[string]any)
	if len(rows) != 10 {
		t.Fatalf("variables = %d rows, want 10", len(rows))
	}
	state, _ := payload[StateKey].(map[string]any)
	if state["state"] != StateParsed || state["library_count"] != 7 || state["bundle_count"] != 1 {
		t.Fatalf("%s = %#v", StateKey, state)
	}
	if _, has := payload["source"]; has {
		t.Fatal("payload carries source without IndexSource")
	}
}

func TestParseMalformedFileKeepsEmptyBucketsAndReportsWhy(t *testing.T) {
	t.Parallel()
	payload, err := Parse(writeCatalog(t, "[libraries\n"), false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil for malformed input", err)
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok || len(rows) != 0 {
		t.Fatalf("variables = %#v, want an empty typed bucket", payload["variables"])
	}
	state, _ := payload[StateKey].(map[string]any)
	if state["state"] != StateMalformed || state["parse_error"] == "" {
		t.Fatalf("%s = %#v, want malformed with a reason", StateKey, state)
	}
}

func TestParseMissingFileReturnsError(t *testing.T) {
	t.Parallel()
	if _, err := Parse(filepath.Join(t.TempDir(), "libs.versions.toml"), false, shared.Options{}); err == nil {
		t.Fatal("Parse() error = nil for a missing file")
	}
}
