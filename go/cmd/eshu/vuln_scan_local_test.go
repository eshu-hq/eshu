// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

func TestRunVulnScanRepoStartsLocalRuntimeWhenServiceURLUnconfigured(t *testing.T) {
	t.Setenv("ESHU_HOME", t.TempDir())
	t.Setenv("ESHU_SERVICE_URL", "")

	repoPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}
	absRepoPath, err := filepath.Abs(repoPath)
	if err != nil {
		t.Fatalf("Abs(repoPath) error = %v, want nil", err)
	}
	if realPath, err := filepath.EvalSymlinks(absRepoPath); err == nil {
		absRepoPath = realPath
	}

	reset := stubScanRuntime(t)
	defer reset()

	var bootstrapCalled atomic.Bool
	scanRunBootstrap = func(_ context.Context, _ string, _ []string, env []string, _ io.Writer, _ io.Writer) error {
		bootstrapCalled.Store(true)
		if got, want := envValue(env, "ESHU_POSTGRES_DSN"), "owner-dsn"; got != want {
			t.Fatalf("ESHU_POSTGRES_DSN = %q, want %q", got, want)
		}
		if got := envValue(env, "ESHU_FILESYSTEM_ROOT"); got != absRepoPath {
			t.Fatalf("ESHU_FILESYSTEM_ROOT = %q, want %q", got, absRepoPath)
		}
		return nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + absRepoPath + `","local_path":"` + absRepoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":1,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":20,"freshness":"fresh"}],"source_snapshots":[{"source":"osv","ecosystem":"npm","freshness":"fresh","complete":true}],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":22}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	originalPrepareLocalRuntime := vulnScanPrepareLocalRuntime
	defer func() { vulnScanPrepareLocalRuntime = originalPrepareLocalRuntime }()

	var prepareCalled atomic.Bool
	var closeCalled atomic.Bool
	vulnScanPrepareLocalRuntime = func(_ context.Context, root string, _ io.Writer) (vulnScanLocalRuntime, error) {
		prepareCalled.Store(true)
		if root != absRepoPath {
			t.Fatalf("local runtime root = %q, want %q", root, absRepoPath)
		}
		return vulnScanLocalRuntime{
			Client:       NewAPIClient(server.URL, "", ""),
			BootstrapEnv: []string{"ESHU_POSTGRES_DSN=owner-dsn"},
			Close: func() error {
				closeCalled.Store(true)
				return nil
			},
		}, nil
	}

	out := &bytes.Buffer{}
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}

	if err := runVulnScanRepo(cmd, []string{repoPath}); err != nil {
		t.Fatalf("runVulnScanRepo() error = %v, want nil", err)
	}
	if !prepareCalled.Load() {
		t.Fatal("local runtime was not prepared")
	}
	if !bootstrapCalled.Load() {
		t.Fatal("bootstrap was not called")
	}
	if !closeCalled.Load() {
		t.Fatal("local runtime close was not called")
	}
}

func TestRunVulnScanRepoReportsLocalRuntimeCloseErrorAsWarning(t *testing.T) {
	t.Setenv("ESHU_HOME", t.TempDir())
	t.Setenv("ESHU_SERVICE_URL", "")

	repoPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}
	absRepoPath, err := filepath.Abs(repoPath)
	if err != nil {
		t.Fatalf("Abs(repoPath) error = %v, want nil", err)
	}
	if realPath, err := filepath.EvalSymlinks(absRepoPath); err == nil {
		absRepoPath = realPath
	}

	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + absRepoPath + `","local_path":"` + absRepoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":1,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":20,"freshness":"fresh"}],"source_snapshots":[{"source":"osv","ecosystem":"npm","freshness":"fresh","complete":true}],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":22}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	originalPrepareLocalRuntime := vulnScanPrepareLocalRuntime
	defer func() { vulnScanPrepareLocalRuntime = originalPrepareLocalRuntime }()
	vulnScanPrepareLocalRuntime = func(context.Context, string, io.Writer) (vulnScanLocalRuntime, error) {
		return vulnScanLocalRuntime{
			Client:       NewAPIClient(server.URL, "", ""),
			BootstrapEnv: []string{"ESHU_POSTGRES_DSN=owner-dsn"},
			Close: func() error {
				return errors.New("cleanup boom")
			},
		}, nil
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}

	if err := runVulnScanRepo(cmd, []string{repoPath}); err != nil {
		t.Fatalf("runVulnScanRepo() error = %v, want nil cleanup warning", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	if payload["error"] != nil {
		t.Fatalf("payload[error] = %#v, want nil", payload["error"])
	}
	data := payload["data"].(map[string]any)
	warnings, ok := data["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("data[warnings] = %#v, want cleanup warning", data["warnings"])
	}
	if got := warnings[len(warnings)-1].(string); got != "local runtime cleanup failed: cleanup boom" {
		t.Fatalf("last warning = %q, want cleanup warning", got)
	}
	if got := errOut.String(); !strings.Contains(got, "Warning: local runtime cleanup failed: cleanup boom") {
		t.Fatalf("stderr = %q, want cleanup warning", got)
	}
}

func TestRunVulnScanRepoUsesConfiguredServiceURLWithoutLocalRuntime(t *testing.T) {
	repoPath := t.TempDir()

	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":1,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":20,"freshness":"fresh"}],"source_snapshots":[{"source":"osv","ecosystem":"npm","freshness":"fresh","complete":true}],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":22}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	originalPrepareLocalRuntime := vulnScanPrepareLocalRuntime
	defer func() { vulnScanPrepareLocalRuntime = originalPrepareLocalRuntime }()
	vulnScanPrepareLocalRuntime = func(context.Context, string, io.Writer) (vulnScanLocalRuntime, error) {
		t.Fatal("local runtime should not start when --service-url is configured")
		return vulnScanLocalRuntime{}, nil
	}

	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}

	if err := runVulnScanRepo(cmd, []string{repoPath}); err != nil {
		t.Fatalf("runVulnScanRepo() error = %v, want nil", err)
	}
}

func TestVulnScanHasConfiguredServiceURLChecksEnvAndConfig(t *testing.T) {
	t.Setenv("ESHU_HOME", t.TempDir())
	t.Setenv("ESHU_SERVICE_URL", "")

	if vulnScanHasConfiguredServiceURL(newTestVulnScanRepoCommand(t)) {
		t.Fatal("vulnScanHasConfiguredServiceURL() = true with no flag, config, or env")
	}

	t.Setenv("ESHU_SERVICE_URL", "http://env.example.test")
	if !vulnScanHasConfiguredServiceURL(newTestVulnScanRepoCommand(t)) {
		t.Fatal("vulnScanHasConfiguredServiceURL() = false with ESHU_SERVICE_URL")
	}

	t.Setenv("ESHU_SERVICE_URL", "")
	if err := setConfigValue("ESHU_SERVICE_URL", "http://config.example.test"); err != nil {
		t.Fatalf("setConfigValue() error = %v, want nil", err)
	}
	if !vulnScanHasConfiguredServiceURL(newTestVulnScanRepoCommand(t)) {
		t.Fatal("vulnScanHasConfiguredServiceURL() = false with persisted service URL")
	}
}

func TestPrepareVulnScanLocalRuntimeAttachesExistingAuthoritativeOwner(t *testing.T) {
	repoPath := t.TempDir()
	restoreOwner := stubLocalOwnerForMCP(t, repoPath, eshulocal.OwnerRecord{
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

	originalReservePort := vulnScanReserveLocalAPIPort
	originalStartAPI := vulnScanStartLocalAPI
	originalWaitAPI := vulnScanWaitLocalAPI
	originalStopProcess := vulnScanStopLocalProcess
	defer func() {
		vulnScanReserveLocalAPIPort = originalReservePort
		vulnScanStartLocalAPI = originalStartAPI
		vulnScanWaitLocalAPI = originalWaitAPI
		vulnScanStopLocalProcess = originalStopProcess
	}()

	vulnScanReserveLocalAPIPort = func() (int, error) { return 19090, nil }
	var gotAPIEnv []string
	vulnScanStartLocalAPI = func(env []string) (*exec.Cmd, error) {
		gotAPIEnv = append([]string(nil), env...)
		return nil, nil
	}
	vulnScanWaitLocalAPI = func(_ context.Context, baseURL string, _ time.Duration) error {
		if baseURL != "http://127.0.0.1:19090" {
			t.Fatalf("baseURL = %q, want local API URL", baseURL)
		}
		return nil
	}
	vulnScanStopLocalProcess = func(*exec.Cmd, time.Duration) error { return nil }

	runtime, err := prepareVulnScanLocalRuntime(context.Background(), repoPath, io.Discard)
	if err != nil {
		t.Fatalf("prepareVulnScanLocalRuntime() error = %v, want nil", err)
	}
	if runtime.Client == nil || runtime.Client.BaseURL != "http://127.0.0.1:19090" {
		t.Fatalf("runtime.Client.BaseURL = %#v, want local API client", runtime.Client)
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

	originalBuildLayout := localHostBuildLayout
	originalReadOwnerRecord := localHostReadOwnerRecord
	originalProcessAlive := localHostProcessAlive
	originalSocketHealthy := localHostSocketHealthy
	originalGraphHealthy := localHostGraphHealthy
	originalStartOwner := vulnScanStartLocalOwner
	originalReservePort := vulnScanReserveLocalAPIPort
	originalStartAPI := vulnScanStartLocalAPI
	originalWaitAPI := vulnScanWaitLocalAPI
	originalStopProcess := vulnScanStopLocalProcess
	defer func() {
		localHostBuildLayout = originalBuildLayout
		localHostReadOwnerRecord = originalReadOwnerRecord
		localHostProcessAlive = originalProcessAlive
		localHostSocketHealthy = originalSocketHealthy
		localHostGraphHealthy = originalGraphHealthy
		vulnScanStartLocalOwner = originalStartOwner
		vulnScanReserveLocalAPIPort = originalReservePort
		vulnScanStartLocalAPI = originalStartAPI
		vulnScanWaitLocalAPI = originalWaitAPI
		vulnScanStopLocalProcess = originalStopProcess
	}()

	var readCount atomic.Int64
	localHostBuildLayout = func(root string) (eshulocal.Layout, error) {
		if mustEvalSymlinks(t, root) != workspaceRoot {
			t.Fatalf("BuildLayout(%q), want %q", root, workspaceRoot)
		}
		return layout, nil
	}
	localHostReadOwnerRecord = func(string) (eshulocal.OwnerRecord, error) {
		if readCount.Add(1) == 1 {
			return eshulocal.OwnerRecord{}, os.ErrNotExist
		}
		return record, nil
	}
	localHostProcessAlive = func(pid int) bool { return pid == record.PID }
	localHostSocketHealthy = func(path string) bool { return path == record.PostgresSocketPath }
	localHostGraphHealthy = func(eshulocal.OwnerRecord) bool { return true }

	ownerCmd := &exec.Cmd{}
	apiCmd := &exec.Cmd{}
	var ownerStarted atomic.Bool
	var ownerStopped atomic.Bool
	vulnScanStartLocalOwner = func(_ context.Context, gotLayout eshulocal.Layout) (*exec.Cmd, error) {
		ownerStarted.Store(true)
		if gotLayout.WorkspaceRoot != workspaceRoot {
			t.Fatalf("owner workspace = %q, want %q", gotLayout.WorkspaceRoot, workspaceRoot)
		}
		return ownerCmd, nil
	}
	vulnScanReserveLocalAPIPort = func() (int, error) { return 19090, nil }
	vulnScanStartLocalAPI = func([]string) (*exec.Cmd, error) { return apiCmd, nil }
	vulnScanWaitLocalAPI = func(context.Context, string, time.Duration) error { return nil }
	vulnScanStopLocalProcess = func(cmd *exec.Cmd, _ time.Duration) error {
		if cmd == ownerCmd {
			ownerStopped.Store(true)
		}
		return nil
	}

	runtime, err := prepareVulnScanLocalRuntime(context.Background(), repoPath, io.Discard)
	if err != nil {
		t.Fatalf("prepareVulnScanLocalRuntime() error = %v, want nil", err)
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

	originalExecutable := eshuExecutable
	originalEnviron := eshuEnviron
	defer func() {
		eshuExecutable = originalExecutable
		eshuEnviron = originalEnviron
	}()
	eshuExecutable = func() (string, error) { return truePath, nil }
	eshuEnviron = func() []string { return []string{"PATH=/tmp"} }

	cmd, err := startVulnScanLocalOwner(context.Background(), layout)
	if err != nil {
		t.Fatalf("startVulnScanLocalOwner() error = %v, want nil", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("owner command Wait() error = %v, want nil", err)
	}
	if pathExists(layout.LogsDir) {
		t.Fatalf("startVulnScanLocalOwner() created %q before owner startup", layout.LogsDir)
	}
}
