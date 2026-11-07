// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/localsupervisor"
	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestRunGraphStartEnvUsesCoreContract proves `eshu graph start` launches the
// local-host supervisor with the authoritative profile and the nornicdb graph
// backend taken from the core-owned querycontract ports.
func TestRunGraphStartEnvUsesCoreContract(t *testing.T) {
	originalExecutable := procexec.Executable
	originalExec := procexec.Exec
	originalEnviron := procexec.Environ
	t.Cleanup(func() {
		procexec.Executable = originalExecutable
		procexec.Exec = originalExec
		procexec.Environ = originalEnviron
	})

	workspaceRoot := t.TempDir()
	stubGraphLayout(t, eshulocal.Layout{
		WorkspaceRoot: workspaceRoot,
		WorkspaceID:   "workspace-id",
		LogsDir:       filepath.Join(workspaceRoot, ".eshu-logs"),
	}, nil)
	procexec.Executable = func() (string, error) {
		return "/usr/local/bin/eshu", nil
	}
	procexec.Environ = func() []string {
		return []string{"PATH=/tmp"}
	}
	wantErr := errors.New("exec sentinel")
	var gotEnv []string
	procexec.Exec = func(binary string, args []string, env []string) error {
		gotEnv = append([]string(nil), env...)
		return wantErr
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")
	cmd.Flags().String("progress", "auto", "")
	cmd.Flags().String("logs", "file", "")
	cmd.Flags().Bool("verbose", false, "")

	captureStderr(t, func() {
		if err := runGraphStart(cmd, nil); !errors.Is(err, wantErr) {
			t.Fatalf("runGraphStart() error = %v, want %v", err, wantErr)
		}
	})

	if got := envValue(gotEnv, "ESHU_QUERY_PROFILE"); got != string(querycontract.ProfileLocalAuthoritative) {
		t.Fatalf("ESHU_QUERY_PROFILE = %q, want %q", got, string(querycontract.ProfileLocalAuthoritative))
	}
	if got := envValue(gotEnv, "ESHU_GRAPH_BACKEND"); got != string(querycontract.GraphBackendNornicDB) {
		t.Fatalf("ESHU_GRAPH_BACKEND = %q, want %q", got, string(querycontract.GraphBackendNornicDB))
	}
	if !strings.Contains(strings.Join(gotEnv, "\x00"), localsupervisor.ProgressModeEnv+"=") {
		t.Fatalf("exec env is missing %s", localsupervisor.ProgressModeEnv)
	}
}
