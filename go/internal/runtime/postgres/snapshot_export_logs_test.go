// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestSnapshotExportDeclarationsWarnOnBareLabels proves a bare release label
// is accepted but warned once at startup, while a suffixed build is silent.
func TestSnapshotExportDeclarationsWarnOnBareLabels(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	logSnapshotExportDeclarations(logger, []SnapshotExportBuild{
		{ServerVersion: "18.6", Evidence: "unpatched CI mirror"},
		{ServerVersion: "18", Evidence: "major only"},
		{ServerVersion: "18.6 (eshu-pg r2)", Evidence: "sha256:abc"},
		{ServerVersion: "18.6.1", Evidence: "three parts is not a bare label"},
	})
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("warnings = %d, want 2 bare labels: %s", len(lines), logs.String())
	}
	for i, want := range []string{`"server_version":"18.6"`, `"server_version":"18"`} {
		if !strings.Contains(lines[i], `"level":"WARN"`) || !strings.Contains(lines[i], `"event_name":"postgres.reader.snapshot_export_declaration"`) || !strings.Contains(lines[i], want) {
			t.Fatalf("warning %d = %s, want %s", i, lines[i], want)
		}
	}
}
