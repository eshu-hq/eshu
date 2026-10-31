// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

func TestRunMCPStartStdioExecsLocalHostWithResolvedWorkspaceRoot(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	startPath := filepath.Join(repoRoot, "pkg")
	if err := os.MkdirAll(startPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}

	restore, calls := stubServiceRuntime()
	defer restore()

	wantExecErr := errors.New("exec sentinel")
	calls.executable = func() (string, error) { return "/tmp/eshu", nil }
	calls.getwd = func() (string, error) { return startPath, nil }
	calls.exec = func(binary string, args []string, env []string) error {
		calls.binary = binary
		calls.args = append([]string(nil), args...)
		calls.env = append([]string(nil), env...)
		return wantExecErr
	}

	cmd := newMCPStartTestCommand()
	err := runMCPStart(cmd, nil)
	if !errors.Is(err, wantExecErr) {
		t.Fatalf("runMCPStart() error = %v, want %v", err, wantExecErr)
	}

	if got, want := calls.binary, "/tmp/eshu"; got != want {
		t.Fatalf("exec binary = %q, want %q", got, want)
	}
	wantWorkspaceRoot := mustEvalSymlinks(t, repoRoot)
	wantArgs := []string{"eshu", "local-host", "mcp-stdio", wantWorkspaceRoot}
	if !reflect.DeepEqual(calls.args, wantArgs) {
		t.Fatalf("exec args = %#v, want %#v", calls.args, wantArgs)
	}
}

func TestRunMCPStartStdioDefaultsLeaveProfileToOwner(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}

	restore, calls := stubServiceRuntime()
	defer restore()

	wantExecErr := errors.New("exec sentinel")
	calls.executable = func() (string, error) { return "/tmp/eshu", nil }
	calls.getwd = func() (string, error) { return repoRoot, nil }
	calls.exec = func(binary string, args []string, env []string) error {
		calls.env = append([]string(nil), env...)
		return wantExecErr
	}

	cmd := newMCPStartTestCommand()
	if err := runMCPStart(cmd, nil); !errors.Is(err, wantExecErr) {
		t.Fatalf("runMCPStart() error = %v, want %v", err, wantExecErr)
	}

	// With no --profile flag the parent must not pin a profile; the mcp-stdio
	// owner picks the authoritative default itself, which keeps attach-to-any
	// existing owner working.
	if got := envValue(calls.env, "ESHU_QUERY_PROFILE"); got != "" {
		t.Fatalf("ESHU_QUERY_PROFILE = %q, want empty (owner decides default)", got)
	}
	if got := envValue(calls.env, "ESHU_GRAPH_BACKEND"); got != "" {
		t.Fatalf("ESHU_GRAPH_BACKEND = %q, want empty", got)
	}
}

func TestRunMCPStartStdioProfileFlagInjectsLightweight(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}

	restore, calls := stubServiceRuntime()
	defer restore()

	// An inherited ESHU_GRAPH_BACKEND (e.g. from a Compose/local shell) must be
	// cleared by --profile local_lightweight; otherwise the child resolves
	// lightweight + a non-empty backend and fails.
	originalEnviron := procexec.Environ
	procexec.Environ = func() []string { return []string{"PATH=/tmp", "ESHU_GRAPH_BACKEND=nornicdb"} }
	defer func() { procexec.Environ = originalEnviron }()

	wantExecErr := errors.New("exec sentinel")
	calls.executable = func() (string, error) { return "/tmp/eshu", nil }
	calls.getwd = func() (string, error) { return repoRoot, nil }
	calls.exec = func(binary string, args []string, env []string) error {
		calls.env = append([]string(nil), env...)
		return wantExecErr
	}

	cmd := newMCPStartTestCommand()
	if err := cmd.Flags().Set("profile", "local_lightweight"); err != nil {
		t.Fatalf("Set(profile) error = %v, want nil", err)
	}
	if err := runMCPStart(cmd, nil); !errors.Is(err, wantExecErr) {
		t.Fatalf("runMCPStart() error = %v, want %v", err, wantExecErr)
	}

	assertEnvValue(t, calls.env, "ESHU_QUERY_PROFILE", "local_lightweight")
	if got := envValue(calls.env, "ESHU_GRAPH_BACKEND"); got != "" {
		t.Fatalf("ESHU_GRAPH_BACKEND = %q, want empty (cleared) for lightweight", got)
	}
}

func TestRunMCPStartStdioProfileFlagInjectsAuthoritative(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}

	restore, calls := stubServiceRuntime()
	defer restore()

	wantExecErr := errors.New("exec sentinel")
	calls.executable = func() (string, error) { return "/tmp/eshu", nil }
	calls.getwd = func() (string, error) { return repoRoot, nil }
	calls.exec = func(binary string, args []string, env []string) error {
		calls.env = append([]string(nil), env...)
		return wantExecErr
	}

	cmd := newMCPStartTestCommand()
	if err := cmd.Flags().Set("profile", "local_authoritative"); err != nil {
		t.Fatalf("Set(profile) error = %v, want nil", err)
	}
	if err := runMCPStart(cmd, nil); !errors.Is(err, wantExecErr) {
		t.Fatalf("runMCPStart() error = %v, want %v", err, wantExecErr)
	}

	assertEnvValue(t, calls.env, "ESHU_QUERY_PROFILE", "local_authoritative")
	assertEnvValue(t, calls.env, "ESHU_GRAPH_BACKEND", "nornicdb")
}

func TestRunMCPStartStdioRejectsUnknownProfile(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}

	restore, calls := stubServiceRuntime()
	defer restore()
	calls.executable = func() (string, error) { return "/tmp/eshu", nil }
	calls.getwd = func() (string, error) { return repoRoot, nil }
	calls.exec = func(string, []string, []string) error {
		t.Fatalf("exec called, want profile validation error")
		return nil
	}

	cmd := newMCPStartTestCommand()
	if err := cmd.Flags().Set("profile", "production"); err != nil {
		t.Fatalf("Set(profile) error = %v, want nil", err)
	}
	err := runMCPStart(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "local_authoritative") {
		t.Fatalf("runMCPStart() error = %v, want profile validation error", err)
	}
}

func TestRunMCPStartSSEAttachesRunningWorkspaceOwner(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}

	restore, calls := stubServiceRuntime()
	defer restore()
	calls.getwd = func() (string, error) { return repoRoot, nil }
	calls.exec = func(string, []string, []string) error {
		t.Fatal("exec called, want RunAttachedMCPHTTP delegation instead")
		return nil
	}

	originalDelegate := runAttachedMCPHTTP
	defer func() { runAttachedMCPHTTP = originalDelegate }()
	var gotLayout eshulocal.Layout
	var gotHost string
	var gotPort int
	runAttachedMCPHTTP = func(_ context.Context, layout eshulocal.Layout, host string, port int) error {
		gotLayout = layout
		gotHost = host
		gotPort = port
		return nil
	}

	cmd := newMCPStartTestCommand()
	if err := cmd.Flags().Set("transport", "sse"); err != nil {
		t.Fatalf("Set(transport) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("host", "127.0.0.1"); err != nil {
		t.Fatalf("Set(host) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("port", "18191"); err != nil {
		t.Fatalf("Set(port) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("workspace-root", repoRoot); err != nil {
		t.Fatalf("Set(workspace-root) error = %v, want nil", err)
	}

	if err := runMCPStart(cmd, nil); err != nil {
		t.Fatalf("runMCPStart() error = %v, want nil", err)
	}

	wantRoot := mustEvalSymlinks(t, repoRoot)
	if got := mustEvalSymlinks(t, gotLayout.WorkspaceRoot); got != wantRoot {
		t.Fatalf("delegate workspace root = %q, want %q", got, wantRoot)
	}
	if gotHost != "127.0.0.1" {
		t.Fatalf("delegate host = %q, want 127.0.0.1", gotHost)
	}
	if gotPort != 18191 {
		t.Fatalf("delegate port = %d, want 18191", gotPort)
	}
}

// TestRunMCPStartHTTPWithWorkspaceRootPropagatesDelegateError proves a
// missing or unhealthy owner surfaces from the delegate instead of falling
// back to an Exec.
func TestRunMCPStartHTTPWithWorkspaceRootPropagatesDelegateError(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}

	restore, calls := stubServiceRuntime()
	defer restore()
	calls.getwd = func() (string, error) { return repoRoot, nil }
	calls.exec = func(string, []string, []string) error {
		t.Fatal("exec called, want RunAttachedMCPHTTP delegation instead")
		return nil
	}

	originalDelegate := runAttachedMCPHTTP
	defer func() { runAttachedMCPHTTP = originalDelegate }()
	wantErr := errors.New("no running local Eshu service owner for workspace")
	runAttachedMCPHTTP = func(context.Context, eshulocal.Layout, string, int) error {
		return wantErr
	}

	cmd := newMCPStartTestCommand()
	if err := cmd.Flags().Set("transport", "http"); err != nil {
		t.Fatalf("Set(transport) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("workspace-root", repoRoot); err != nil {
		t.Fatalf("Set(workspace-root) error = %v, want nil", err)
	}

	if err := runMCPStart(cmd, nil); !errors.Is(err, wantErr) {
		t.Fatalf("runMCPStart() error = %v, want %v", err, wantErr)
	}
}

type serviceRuntimeCalls struct {
	executable func() (string, error)
	getwd      func() (string, error)
	lookPath   func(string) (string, error)
	exec       func(string, []string, []string) error
	binary     string
	args       []string
	env        []string
}

func stubServiceRuntime() (func(), *serviceRuntimeCalls) {
	calls := &serviceRuntimeCalls{}

	originalExecutable := procexec.Executable
	originalGetwd := procexec.Getwd
	originalLookPath := procexec.LookPath
	originalExec := procexec.Exec
	originalEnviron := procexec.Environ

	procexec.Executable = func() (string, error) {
		if calls.executable == nil {
			return "", errors.New("procexec.Executable not stubbed")
		}
		return calls.executable()
	}
	procexec.Getwd = func() (string, error) {
		if calls.getwd == nil {
			return "", errors.New("procexec.Getwd not stubbed")
		}
		return calls.getwd()
	}
	procexec.LookPath = func(binary string) (string, error) {
		if calls.lookPath == nil {
			return "", errors.New("procexec.LookPath not stubbed")
		}
		return calls.lookPath(binary)
	}
	procexec.Exec = func(binary string, args []string, env []string) error {
		if calls.exec == nil {
			return errors.New("procexec.Exec not stubbed")
		}
		return calls.exec(binary, args, env)
	}
	procexec.Environ = func() []string {
		return []string{"PATH=/tmp"}
	}

	return func() {
		procexec.Executable = originalExecutable
		procexec.Getwd = originalGetwd
		procexec.LookPath = originalLookPath
		procexec.Exec = originalExec
		procexec.Environ = originalEnviron
	}, calls
}

func assertEnvValue(t *testing.T, env []string, key, want string) {
	t.Helper()
	if got := envValue(env, key); got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

func newMCPStartTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("transport", "stdio", "")
	cmd.Flags().String("host", "127.0.0.1", "")
	cmd.Flags().Int("port", 0, "")
	cmd.Flags().String("workspace-root", "", "")
	cmd.Flags().String("core-base-url", "", "")
	cmd.Flags().String("profile", "", "")
	return cmd
}
