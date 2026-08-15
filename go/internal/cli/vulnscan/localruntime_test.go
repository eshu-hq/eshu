// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/localsupervisor"
	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

func TestPrepareVulnScanLocalRuntimeAttachesExistingAuthoritativeOwner(t *testing.T) {
	repoPath := t.TempDir()
	restoreOwner := stubLocalOwner(t, repoPath, eshulocal.OwnerRecord{
		PID:                1234,
		WorkspaceID:        "workspace-123",
		PostgresPort:       15439,
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
		Profile:            "local_authoritative",
		GraphBackend:       "nornicdb",
		GraphAddress:       "127.0.0.1",
		GraphBoltPort:      17687,
		GraphUsername:      "admin",
		GraphPassword:      "workspace-secret",
	})
	defer restoreOwner()

	originalReservePort := reserveLocalAPIPortFn
	originalStartAPI := startLocalAPIFn
	originalWaitAPI := waitLocalAPIFn
	originalStopProcess := stopLocalProcessFn
	defer func() {
		reserveLocalAPIPortFn = originalReservePort
		startLocalAPIFn = originalStartAPI
		waitLocalAPIFn = originalWaitAPI
		stopLocalProcessFn = originalStopProcess
	}()

	reserveLocalAPIPortFn = func() (int, error) { return 19090, nil }
	var gotAPIEnv []string
	startLocalAPIFn = func(env []string) (*exec.Cmd, error) {
		gotAPIEnv = append([]string(nil), env...)
		return nil, nil
	}
	waitLocalAPIFn = func(_ context.Context, baseURL string, _ time.Duration) error {
		if baseURL != "http://127.0.0.1:19090" {
			t.Fatalf("baseURL = %q, want local API URL", baseURL)
		}
		return nil
	}
	stopLocalProcessFn = func(*exec.Cmd, time.Duration) error { return nil }

	runtime, err := prepareLocalRuntime(context.Background(), repoPath, io.Discard)
	if err != nil {
		t.Fatalf("prepareLocalRuntime() error = %v, want nil", err)
	}
	if runtime.BaseURL != "http://127.0.0.1:19090" {
		t.Fatalf("runtime.BaseURL = %q, want local API address", runtime.BaseURL)
	}
	wantDSN := "host=127.0.0.1 port=15439 user=eshu password=change-me dbname=postgres sslmode=disable"
	if got := envValue(runtime.BootstrapEnv, "ESHU_POSTGRES_DSN"); got != wantDSN {
		t.Fatalf("bootstrap ESHU_POSTGRES_DSN = %q, want %q", got, wantDSN)
	}
	if got, want := envValue(runtime.BootstrapEnv, "ESHU_NEO4J_URI"), "bolt://127.0.0.1:17687"; got != want {
		t.Fatalf("bootstrap ESHU_NEO4J_URI = %q, want %q", got, want)
	}
	if got, want := envValue(gotAPIEnv, "ESHU_API_ADDR"), "127.0.0.1:19090"; got != want {
		t.Fatalf("API ESHU_API_ADDR = %q, want %q", got, want)
	}
	if got, want := envValue(gotAPIEnv, "ESHU_GRAPH_BACKEND"), "nornicdb"; got != want {
		t.Fatalf("API ESHU_GRAPH_BACKEND = %q, want %q", got, want)
	}
}

func TestPrepareVulnScanLocalRuntimeStartsOwnerWhenMissing(t *testing.T) {
	repoPath := t.TempDir()
	workspaceRoot := mustEvalSymlinks(t, repoPath)
	layout := eshulocal.Layout{
		WorkspaceRoot:   workspaceRoot,
		WorkspaceID:     "workspace-123",
		OwnerRecordPath: filepath.Join(t.TempDir(), "owner.json"),
		LogsDir:         filepath.Join(t.TempDir(), "logs"),
	}
	record := eshulocal.OwnerRecord{
		PID:                1234,
		WorkspaceID:        layout.WorkspaceID,
		PostgresPort:       15439,
		PostgresSocketPath: "/tmp/.s.PGSQL.15439",
		Profile:            "local_authoritative",
		GraphBackend:       "nornicdb",
		GraphAddress:       "127.0.0.1",
		GraphBoltPort:      17687,
		GraphHTTPPort:      17474,
		GraphUsername:      "admin",
		GraphPassword:      "workspace-secret",
	}

	originalBuildLayout := localsupervisor.BuildLayout
	originalReadOwnerRecord := localsupervisor.ReadOwnerRecord
	originalProcessAlive := localsupervisor.ProcessAlive
	originalSocketHealthy := localsupervisor.SocketHealthy
	originalGraphHealthy := localsupervisor.GraphHealthy
	originalStartOwner := startLocalOwnerFn
	originalReservePort := reserveLocalAPIPortFn
	originalStartAPI := startLocalAPIFn
	originalWaitAPI := waitLocalAPIFn
	originalStopProcess := stopLocalProcessFn
	defer func() {
		localsupervisor.BuildLayout = originalBuildLayout
		localsupervisor.ReadOwnerRecord = originalReadOwnerRecord
		localsupervisor.ProcessAlive = originalProcessAlive
		localsupervisor.SocketHealthy = originalSocketHealthy
		localsupervisor.GraphHealthy = originalGraphHealthy
		startLocalOwnerFn = originalStartOwner
		reserveLocalAPIPortFn = originalReservePort
		startLocalAPIFn = originalStartAPI
		waitLocalAPIFn = originalWaitAPI
		stopLocalProcessFn = originalStopProcess
	}()

	var readCount atomic.Int64
	localsupervisor.BuildLayout = func(root string) (eshulocal.Layout, error) {
		if mustEvalSymlinks(t, root) != workspaceRoot {
			t.Fatalf("BuildLayout(%q), want %q", root, workspaceRoot)
		}
		return layout, nil
	}
	localsupervisor.ReadOwnerRecord = func(string) (eshulocal.OwnerRecord, error) {
		if readCount.Add(1) == 1 {
			return eshulocal.OwnerRecord{}, os.ErrNotExist
		}
		return record, nil
	}
	localsupervisor.ProcessAlive = func(pid int) bool { return pid == record.PID }
	localsupervisor.SocketHealthy = func(path string) bool { return path == record.PostgresSocketPath }
	localsupervisor.GraphHealthy = func(eshulocal.OwnerRecord) bool { return true }

	ownerCmd := &exec.Cmd{}
	apiCmd := &exec.Cmd{}
	var ownerStarted atomic.Bool
	var ownerStopped atomic.Bool
	startLocalOwnerFn = func(_ context.Context, gotLayout eshulocal.Layout) (*exec.Cmd, error) {
		ownerStarted.Store(true)
		if gotLayout.WorkspaceRoot != workspaceRoot {
			t.Fatalf("owner workspace = %q, want %q", gotLayout.WorkspaceRoot, workspaceRoot)
		}
		return ownerCmd, nil
	}
	reserveLocalAPIPortFn = func() (int, error) { return 19090, nil }
	startLocalAPIFn = func([]string) (*exec.Cmd, error) { return apiCmd, nil }
	waitLocalAPIFn = func(context.Context, string, time.Duration) error { return nil }
	stopLocalProcessFn = func(cmd *exec.Cmd, _ time.Duration) error {
		if cmd == ownerCmd {
			ownerStopped.Store(true)
		}
		return nil
	}

	runtime, err := prepareLocalRuntime(context.Background(), repoPath, io.Discard)
	if err != nil {
		t.Fatalf("prepareLocalRuntime() error = %v, want nil", err)
	}
	if !ownerStarted.Load() {
		t.Fatal("local owner was not started")
	}
	if runtime.Close == nil {
		t.Fatal("runtime.Close = nil, want cleanup hook")
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("runtime.Close() error = %v, want nil", err)
	}
	if !ownerStopped.Load() {
		t.Fatal("owned local service was not stopped")
	}
}

func TestStartVulnScanLocalOwnerDoesNotCreateWorkspaceLogsBeforeOwnerStartup(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Fatalf("LookPath(true) error = %v, want nil", err)
	}
	root := t.TempDir()
	layout := eshulocal.Layout{
		WorkspaceRoot: root,
		RootDir:       filepath.Join(t.TempDir(), "workspace-root"),
		LogsDir:       filepath.Join(t.TempDir(), "workspace-root", "logs"),
	}

	originalExecutable := procexec.Executable
	originalEnviron := procexec.Environ
	defer func() {
		procexec.Executable = originalExecutable
		procexec.Environ = originalEnviron
	}()
	procexec.Executable = func() (string, error) { return truePath, nil }
	procexec.Environ = func() []string { return []string{"PATH=/tmp"} }

	cmd, err := startLocalOwner(context.Background(), layout)
	if err != nil {
		t.Fatalf("startLocalOwner() error = %v, want nil", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("owner command Wait() error = %v, want nil", err)
	}
	if pathExists(layout.LogsDir) {
		t.Fatalf("startLocalOwner() created %q before owner startup", layout.LogsDir)
	}
}

// stubLocalOwner replaces the localsupervisor lookups so a test can present a
// healthy owner record without a running service. It mirrors the cmd/eshu
// helper of the same shape; the package cannot import that one because
// go/cmd/eshu is package main.
func stubLocalOwner(t *testing.T, repoRoot string, record eshulocal.OwnerRecord) func() {
	t.Helper()

	originalBuildLayout := localsupervisor.BuildLayout
	originalReadOwnerRecord := localsupervisor.ReadOwnerRecord
	originalProcessAlive := localsupervisor.ProcessAlive
	originalSocketHealthy := localsupervisor.SocketHealthy
	originalGraphHealthy := localsupervisor.GraphHealthy

	workspaceRoot := mustEvalSymlinks(t, repoRoot)
	localsupervisor.BuildLayout = func(root string) (eshulocal.Layout, error) {
		if got := mustEvalSymlinks(t, root); got != workspaceRoot {
			t.Fatalf("BuildLayout(%q) resolved to %q, want %q", root, got, workspaceRoot)
		}
		return eshulocal.Layout{
			WorkspaceRoot:   workspaceRoot,
			WorkspaceID:     "workspace-123",
			OwnerRecordPath: filepath.Join(t.TempDir(), "owner.json"),
		}, nil
	}
	localsupervisor.ReadOwnerRecord = func(string) (eshulocal.OwnerRecord, error) {
		if record.PID == 0 {
			return eshulocal.OwnerRecord{}, os.ErrNotExist
		}
		return record, nil
	}
	localsupervisor.ProcessAlive = func(int) bool { return record.PID != 0 }
	localsupervisor.SocketHealthy = func(string) bool { return record.PID != 0 }
	localsupervisor.GraphHealthy = func(eshulocal.OwnerRecord) bool { return record.PID != 0 }

	return func() {
		localsupervisor.BuildLayout = originalBuildLayout
		localsupervisor.ReadOwnerRecord = originalReadOwnerRecord
		localsupervisor.ProcessAlive = originalProcessAlive
		localsupervisor.SocketHealthy = originalSocketHealthy
		localsupervisor.GraphHealthy = originalGraphHealthy
	}
}

// mustEvalSymlinks resolves path the same way BuildLayout does, so a macOS
// /var -> /private/var temp directory compares equal.
func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v, want nil", path, err)
	}
	return resolved
}

// envValue reads one KEY=value entry out of a composed child environment.
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

// pathExists reports whether path is present, used to prove the owner start
// path does not create the log directory before the owner does.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
