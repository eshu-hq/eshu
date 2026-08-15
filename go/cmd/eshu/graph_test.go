// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/localsupervisor"
	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// stubGraphLayout points the graph subcommands at a fixed layout so a wrapper
// test exercises flag reading and output without touching a real workspace.
func stubGraphLayout(t *testing.T, layout eshulocal.Layout, err error) {
	t.Helper()
	original := localsupervisor.LayoutForWorkspaceRoot
	t.Cleanup(func() {
		localsupervisor.LayoutForWorkspaceRoot = original
	})
	localsupervisor.LayoutForWorkspaceRoot = func(string) (eshulocal.Layout, error) {
		return layout, err
	}
}

func TestRunGraphStatusPrintsJSON(t *testing.T) {
	stubGraphLayout(t, eshulocal.Layout{
		WorkspaceRoot:   t.TempDir(),
		WorkspaceID:     "workspace-id",
		OwnerRecordPath: filepath.Join(t.TempDir(), "absent-owner.json"),
	}, nil)

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")

	output := captureStdout(t, func() {
		if err := runGraphStatus(cmd, nil); err != nil {
			t.Fatalf("runGraphStatus() error = %v, want nil", err)
		}
	})

	var got localsupervisor.StatusOutput
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("json.Unmarshal(output) error = %v, output=%q", err, output)
	}
	if got.WorkspaceID != "workspace-id" {
		t.Fatalf("WorkspaceID = %q, want %q", got.WorkspaceID, "workspace-id")
	}
	if got.OwnerPresent {
		t.Fatal("OwnerPresent = true, want false for an absent owner record")
	}
}

func TestRunGraphLogsPrintsWorkspaceGraphLog(t *testing.T) {
	logsDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(logsDir) error = %v, want nil", err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "graph-nornicdb.log"), []byte("graph ready\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(graph log) error = %v, want nil", err)
	}
	stubGraphLayout(t, eshulocal.Layout{
		WorkspaceRoot: t.TempDir(),
		WorkspaceID:   "workspace-id",
		LogsDir:       logsDir,
	}, nil)

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")

	output := captureStdout(t, func() {
		if err := runGraphLogs(cmd, nil); err != nil {
			t.Fatalf("runGraphLogs() error = %v, want nil", err)
		}
	})
	if output != "graph ready\n" {
		t.Fatalf("runGraphLogs() output = %q, want graph log content", output)
	}
}

func TestRunGraphStartExecsAuthoritativeLocalHost(t *testing.T) {
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
		return []string{"ESHU_QUERY_PROFILE=local_lightweight"}
	}
	wantErr := errors.New("exec sentinel")
	var gotBinary string
	var gotArgs []string
	var gotEnv []string
	procexec.Exec = func(binary string, args []string, env []string) error {
		gotBinary = binary
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return wantErr
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")
	cmd.Flags().String("progress", "auto", "")
	cmd.Flags().String("logs", "file", "")
	cmd.Flags().Bool("verbose", false, "")

	stderr := captureStderr(t, func() {
		err := runGraphStart(cmd, nil)
		if !errors.Is(err, wantErr) {
			t.Fatalf("runGraphStart() error = %v, want %v", err, wantErr)
		}
	})
	if !strings.Contains(stderr, "Starting local Eshu service") {
		t.Fatalf("runGraphStart() stderr = %q, want local Eshu service message", stderr)
	}
	if gotBinary != "/usr/local/bin/eshu" {
		t.Fatalf("exec binary = %q, want eshu path", gotBinary)
	}
	wantArgs := []string{"eshu", "local-host", "watch", workspaceRoot}
	if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("exec args = %#v, want %#v", gotArgs, wantArgs)
	}
	if envValue(gotEnv, "ESHU_QUERY_PROFILE") != string(query.ProfileLocalAuthoritative) {
		t.Fatalf("ESHU_QUERY_PROFILE = %q, want local_authoritative", envValue(gotEnv, "ESHU_QUERY_PROFILE"))
	}
	if envValue(gotEnv, "ESHU_GRAPH_BACKEND") != string(query.GraphBackendNornicDB) {
		t.Fatalf("ESHU_GRAPH_BACKEND = %q, want nornicdb", envValue(gotEnv, "ESHU_GRAPH_BACKEND"))
	}
	if envValue(gotEnv, "ESHU_LOCAL_PROGRESS_MODE") != "auto" {
		t.Fatalf("ESHU_LOCAL_PROGRESS_MODE = %q, want auto", envValue(gotEnv, "ESHU_LOCAL_PROGRESS_MODE"))
	}
	if envValue(gotEnv, "ESHU_LOCAL_LOG_MODE") != "file" {
		t.Fatalf("ESHU_LOCAL_LOG_MODE = %q, want file", envValue(gotEnv, "ESHU_LOCAL_LOG_MODE"))
	}
	if envValue(gotEnv, "ESHU_LOCAL_LOG_DIR") != filepath.Join(workspaceRoot, ".eshu-logs") {
		t.Fatalf("ESHU_LOCAL_LOG_DIR = %q, want workspace log dir", envValue(gotEnv, "ESHU_LOCAL_LOG_DIR"))
	}
}

func TestRunGraphStatusReturnsBuildLayoutError(t *testing.T) {
	stubGraphLayout(t, eshulocal.Layout{}, errors.New("layout failed"))

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")

	err := runGraphStatus(cmd, nil)
	if err == nil || err.Error() != "layout failed" {
		t.Fatalf("runGraphStatus() error = %v, want %q", err, "layout failed")
	}
}

// TestRunGraphUpgradeForwardsSourceFlags proves the cobra wrapper hands
// --from/--sha256 to the installer instead of dropping them: the source path
// does not exist, so the install error must name it.
func TestRunGraphUpgradeForwardsSourceFlags(t *testing.T) {
	workspaceRoot := t.TempDir()
	missingSource := filepath.Join(t.TempDir(), "nornicdb-headless")
	stubGraphLayout(t, eshulocal.Layout{
		WorkspaceRoot:   workspaceRoot,
		WorkspaceID:     "workspace-id",
		OwnerRecordPath: filepath.Join(t.TempDir(), "absent-owner.json"),
	}, nil)

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")
	cmd.Flags().String("from", missingSource, "")
	cmd.Flags().String("sha256", "", "")

	err := runGraphUpgrade(cmd, nil)
	if err == nil {
		t.Fatal("runGraphUpgrade() error = nil, want missing-source error")
	}
	if !strings.Contains(err.Error(), missingSource) {
		t.Fatalf("runGraphUpgrade() error = %q, want the --from path %q", err.Error(), missingSource)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v, want nil", err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = originalStdout
	})

	done := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		_, _ = io.Copy(&buffer, reader)
		done <- buffer.String()
	}()

	fn()

	_ = writer.Close()
	got := <-done
	return got
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	originalStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v, want nil", err)
	}
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = originalStderr
	})

	done := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		_, _ = io.Copy(&buffer, reader)
		done <- buffer.String()
	}()

	fn()

	_ = writer.Close()
	got := <-done
	return got
}

// envValue reads one KEY=VALUE entry out of a child-process environment slice.
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
