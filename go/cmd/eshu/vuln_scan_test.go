// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
)

func TestVulnScanRepoCommandIsRegisteredWithBoundedFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"vuln-scan", "repo"})
	if err != nil {
		t.Fatalf("rootCmd.Find(vuln-scan repo) error = %v, want nil", err)
	}
	if cmd == nil || cmd.Name() != "repo" {
		t.Fatalf("root command = %#v, want vuln-scan repo command", cmd)
	}
	for _, name := range []string{
		"json",
		"wait",
		"timeout",
		"poll-interval",
		"allow-partial",
		"limit",
		"impact-status",
		"repo-id",
		"workspace-root",
	} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("vuln-scan repo flag %q missing", name)
		}
	}
}

func TestRunVulnScanRepoIndexesResolvesRepoAndListsImpactFindings(t *testing.T) {
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
	scanRunBootstrap = func(_ context.Context, _ string, args []string, env []string, stdout io.Writer, _ io.Writer) error {
		bootstrapCalled.Store(true)
		if got, want := strings.Join(args, " "), "eshu-bootstrap-index --path "+absRepoPath; got != want {
			t.Fatalf("bootstrap args = %q, want %q", got, want)
		}
		if got := envValue(env, "ESHU_FILESYSTEM_ROOT"); got != absRepoPath {
			t.Fatalf("ESHU_FILESYSTEM_ROOT = %q, want %q", got, absRepoPath)
		}
		_, _ = fmt.Fprintln(stdout, "bootstrap log line")
		return nil
	}
	var gotImpactQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + absRepoPath + `","local_path":"` + absRepoPath + `","repo_slug":"","has_remote":false}]}`))
		case "/api/v0/supply-chain/impact/findings":
			gotImpactQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"data":{"findings":[{"finding_id":"finding-1","cve_id":"CVE-2026-0001","package_id":"npm://registry.npmjs.org/ws","impact_status":"affected_exact","repository_id":"repo-local"}],"count":1,"limit":25,"truncated":false,"readiness":{"readiness_state":"ready_with_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":2,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":80,"freshness":"fresh"}],"source_snapshots":[{"source":"osv","ecosystem":"npm","freshness":"fresh","complete":true}],"freshness":"fresh","counts":{"findings_returned":1,"evidence_facts_total":83}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("limit", "25"); err != nil {
		t.Fatalf("Set(limit) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("impact-status", "affected_exact"); err != nil {
		t.Fatalf("Set(impact-status) error = %v, want nil", err)
	}

	requireVulnScanExitCode(t, runVulnScanRepo(cmd, []string{repoPath}), 3)
	if !bootstrapCalled.Load() {
		t.Fatal("bootstrap was not called")
	}
	if !strings.Contains(gotImpactQuery, "repository_id=repo-local") {
		t.Fatalf("impact query = %q, want repository_id=repo-local", gotImpactQuery)
	}
	if !strings.Contains(gotImpactQuery, "limit=25") {
		t.Fatalf("impact query = %q, want limit=25", gotImpactQuery)
	}
	if !strings.Contains(gotImpactQuery, "impact_status=affected_exact") {
		t.Fatalf("impact query = %q, want impact_status=affected_exact", gotImpactQuery)
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	if payload["error"] != nil {
		t.Fatalf("payload[error] = %#v, want nil", payload["error"])
	}
	data := payload["data"].(map[string]any)
	if got, want := data["command"], "vuln-scan repo"; got != want {
		t.Fatalf("data[command] = %#v, want %#v", got, want)
	}
	if got, want := data["readiness_state"], "ready_with_findings"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v", got, want)
	}
	findings := data["findings"].([]any)
	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d", got, want)
	}
}

func TestRunVulnScanRepoReportsReadyZeroFindings(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-empty","name":"empty","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-empty"},"evidence_sources":[{"family":"package.consumption","fact_count":1,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":20,"freshness":"fresh"}],"source_snapshots":[{"source":"osv","ecosystem":"npm","freshness":"fresh","complete":true}],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":22}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}

	requireVulnScanExitCode(t, runVulnScanRepo(cmd, []string{repoPath}), 0)

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	data := payload["data"].(map[string]any)
	if got, want := data["readiness_state"], "ready_zero_findings"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v", got, want)
	}
}

func TestRunVulnScanRepoSurfacesServerNotConfiguredReadiness(t *testing.T) {
	// Regression for the CLI ignoring the server's richer readiness states
	// and reporting `ready_zero_findings` when the server says
	// `not_configured`. The CLI must surface the server verdict so a
	// developer with no advisory ingestion is not told their repo is clean.
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-empty","name":"empty","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"not_configured","target_scope":{"repository_id":"repo-empty"},"missing_evidence":["advisory_sources","owned_packages"]}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}

	requireVulnScanExitCode(t, runVulnScanRepo(cmd, []string{repoPath}), 4)

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	data := payload["data"].(map[string]any)
	if got, want := data["readiness_state"], "not_configured"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v (server verdict must win over count heuristic)", got, want)
	}
	readiness, ok := data["readiness"].(map[string]any)
	if !ok {
		t.Fatalf("data[readiness] = %T, want server-side envelope passed through", data["readiness"])
	}
	if got, want := readiness["readiness_state"], "not_configured"; got != want {
		t.Fatalf("data[readiness][readiness_state] = %#v, want %#v", got, want)
	}
}

func TestRunVulnScanRepoFailsClosedWhenScanIsNotReady(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	var impactCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/supply-chain/impact/findings":
			impactCalled.Store(true)
			t.Fatalf("impact findings endpoint called before scan readiness: %s", r.URL.RawQuery)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("wait", "false"); err != nil {
		t.Fatalf("Set(wait) error = %v, want nil", err)
	}

	err := runVulnScanRepo(cmd, []string{repoPath})
	if err == nil {
		t.Fatal("runVulnScanRepo() error = nil, want readiness failure")
	}
	if impactCalled.Load() {
		t.Fatal("impact findings endpoint was called before scan readiness")
	}

	var payload map[string]any
	if unmarshalErr := json.Unmarshal(out.Bytes(), &payload); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", unmarshalErr, out.String())
	}
	data := payload["data"].(map[string]any)
	if got, want := data["readiness_state"], "target_incomplete"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v", got, want)
	}
	if payload["error"] == nil {
		t.Fatalf("payload[error] = nil, want readiness error")
	}
}

func newTestVulnScanRepoCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	addVulnScanRepoFlags(cmd)
	addRemoteFlags(cmd)
	return cmd
}

// Scoped/broad/perf tests live in vuln_scan_scope_test.go so each file stays
// under the 500-line cap mandated by AGENTS.md.
