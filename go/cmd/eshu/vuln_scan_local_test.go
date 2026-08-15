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
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/vulnscan"
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
	scanStub.RunBootstrap = func(_ context.Context, _ string, _ []string, env []string, _ io.Writer, _ io.Writer) error {
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
	vulnScanPrepareLocalRuntime = func(_ context.Context, root string, _ io.Writer) (vulnscan.LocalRuntime, error) {
		prepareCalled.Store(true)
		if root != absRepoPath {
			t.Fatalf("local runtime root = %q, want %q", root, absRepoPath)
		}
		return vulnscan.LocalRuntime{
			BaseURL:      server.URL,
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
	vulnScanPrepareLocalRuntime = func(context.Context, string, io.Writer) (vulnscan.LocalRuntime, error) {
		return vulnscan.LocalRuntime{
			BaseURL:      server.URL,
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
	vulnScanPrepareLocalRuntime = func(context.Context, string, io.Writer) (vulnscan.LocalRuntime, error) {
		t.Fatal("local runtime should not start when --service-url is configured")
		return vulnscan.LocalRuntime{}, nil
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
