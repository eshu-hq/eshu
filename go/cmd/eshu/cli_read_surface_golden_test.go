// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

func TestCLIReadSurfaceGoldensMatchCommandTree(t *testing.T) {
	snap, err := goldengate.LoadSnapshot(filepath.Join("..", "..", "..", "testdata", "golden", "e2e-20repo-snapshot.json"))
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	for _, tc := range []struct {
		snapshotKey string
		commandPath string
	}{
		{snapshotKey: "eshu list", commandPath: "list"},
		{snapshotKey: "eshu index-status", commandPath: "index-status"},
		{snapshotKey: "eshu trace service --json", commandPath: "trace service"},
		{snapshotKey: "eshu playbooks list", commandPath: "playbooks list"},
		{snapshotKey: "eshu vuln-scan repo --json", commandPath: "vuln-scan repo"},
		{snapshotKey: "eshu component inventory --json", commandPath: "component inventory"},
		{snapshotKey: "eshu hosted-onboard --json", commandPath: "hosted-onboard"},
	} {
		shape, ok := snap.QueryShapes.CLI[tc.snapshotKey]
		if !ok {
			t.Fatalf("query_shapes.cli missing %s", tc.snapshotKey)
		}
		if !commandPathExists(tc.commandPath) {
			t.Fatalf("query_shapes.cli[%s] points at missing command path %s", tc.snapshotKey, tc.commandPath)
		}
		if err := commandArgvAccepted(shape.Command); err != nil {
			t.Fatalf("query_shapes.cli[%s] command %q is not runnable: %v", tc.snapshotKey, strings.Join(shape.Command, " "), err)
		}
	}
}

func commandArgvAccepted(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty argv")
	}
	cmd, remaining, err := rootCmd.Find(argv)
	if err != nil {
		return err
	}
	if cmd == nil || cmd == rootCmd {
		return fmt.Errorf("no concrete command resolved")
	}
	for _, token := range remaining {
		if !strings.HasPrefix(token, "--") {
			continue
		}
		name := strings.TrimPrefix(token, "--")
		if before, _, ok := strings.Cut(name, "="); ok {
			name = before
		}
		if cmd.Flag(name) == nil {
			return fmt.Errorf("unknown flag --%s on %s", name, cmd.CommandPath())
		}
	}
	return nil
}
