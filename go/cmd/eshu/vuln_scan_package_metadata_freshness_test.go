// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunVulnScanRepoFailsClosedOnMissingPackageMetadata(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":3,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":50,"freshness":"fresh"}],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":53}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
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

	err := runVulnScanRepo(cmd, []string{repoPath})
	if err == nil {
		t.Fatal("runVulnScanRepo() error = nil, want fail-closed for missing package metadata")
	}

	var payload map[string]any
	if uerr := json.Unmarshal(out.Bytes(), &payload); uerr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", uerr, out.String())
	}
	data := payload["data"].(map[string]any)
	if got, want := data["readiness_state"], "evidence_incomplete"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v", got, want)
	}
	plan := data["scope_plan"].(map[string]any)
	if got, want := plan["package_registry_facts"], float64(0); got != want {
		t.Fatalf("plan[package_registry_facts] = %#v, want %#v", got, want)
	}
	if got, want := plan["package_registry_freshness"], "missing"; got != want {
		t.Fatalf("plan[package_registry_freshness] = %#v, want %#v", got, want)
	}
	missing, ok := plan["missing_evidence"].([]any)
	if !ok || len(missing) == 0 || missing[0] != "package_registry_metadata" {
		t.Fatalf("plan[missing_evidence] = %#v, want package_registry_metadata", plan["missing_evidence"])
	}
	perf := data["scan_performance"].(map[string]any)
	if got, want := perf["package_registry_freshness"], "missing"; got != want {
		t.Fatalf("perf[package_registry_freshness] = %#v, want %#v", got, want)
	}
	if got, want := perf["package_registry_complete"], false; got != want {
		t.Fatalf("perf[package_registry_complete] = %#v, want %#v", got, want)
	}
}

func TestRunVulnScanRepoFailsClosedOnStalePackageMetadata(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":1,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"stale"},{"family":"vulnerability.advisory","fact_count":50,"freshness":"fresh"}],"freshness":"stale","counts":{"findings_returned":0,"evidence_facts_total":52}}},"truth":{"level":"exact","freshness":{"state":"stale"}},"error":null}`))
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

	err := runVulnScanRepo(cmd, []string{repoPath})
	if err == nil {
		t.Fatal("runVulnScanRepo() error = nil, want fail-closed for stale package metadata")
	}

	var payload map[string]any
	if uerr := json.Unmarshal(out.Bytes(), &payload); uerr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", uerr, out.String())
	}
	data := payload["data"].(map[string]any)
	if got, want := data["readiness_state"], "evidence_incomplete"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v", got, want)
	}
	plan := data["scope_plan"].(map[string]any)
	if got, want := plan["package_registry_freshness"], "stale"; got != want {
		t.Fatalf("plan[package_registry_freshness] = %#v, want %#v", got, want)
	}
	perf := data["scan_performance"].(map[string]any)
	if got, want := perf["package_registry_freshness"], "stale"; got != want {
		t.Fatalf("perf[package_registry_freshness] = %#v, want %#v", got, want)
	}
	if got, want := perf["package_registry_complete"], false; got != want {
		t.Fatalf("perf[package_registry_complete] = %#v, want %#v", got, want)
	}
}

func TestRunVulnScanRepoBroadModeFailsClosedOnStalePackageMetadata(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":1,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"stale"},{"family":"vulnerability.advisory","fact_count":50,"freshness":"fresh"}],"freshness":"stale","counts":{"findings_returned":0,"evidence_facts_total":52}}},"truth":{"level":"exact","freshness":{"state":"stale"}},"error":null}`))
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
	if err := cmd.Flags().Set("broad", "true"); err != nil {
		t.Fatalf("Set(broad) error = %v, want nil", err)
	}

	err := runVulnScanRepo(cmd, []string{repoPath})
	if err == nil {
		t.Fatal("runVulnScanRepo() error = nil, want broad mode to fail closed for stale package metadata")
	}

	var payload map[string]any
	if uerr := json.Unmarshal(out.Bytes(), &payload); uerr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", uerr, out.String())
	}
	data := payload["data"].(map[string]any)
	if got, want := data["readiness_state"], "evidence_incomplete"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v", got, want)
	}
	plan := data["scope_plan"].(map[string]any)
	missing, ok := plan["missing_evidence"].([]any)
	if !ok || len(missing) == 0 {
		t.Fatalf("plan[missing_evidence] = %#v, want package_registry_metadata", plan["missing_evidence"])
	}
	if got, want := missing[0], "package_registry_metadata"; got != want {
		t.Fatalf("plan[missing_evidence][0] = %#v, want %#v", got, want)
	}
}

func TestRunVulnScanRepoDoesNotRequirePackageMetadataWithoutDependencies(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"vulnerability.advisory","fact_count":50,"freshness":"fresh"}],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":50}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
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

	if err := runVulnScanRepo(cmd, []string{repoPath}); err != nil {
		t.Fatalf("runVulnScanRepo() error = %v, want nil when no dependency facts require package metadata", err)
	}

	var payload map[string]any
	if uerr := json.Unmarshal(out.Bytes(), &payload); uerr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", uerr, out.String())
	}
	data := payload["data"].(map[string]any)
	if got, want := data["readiness_state"], "ready_zero_findings"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v", got, want)
	}
	plan := data["scope_plan"].(map[string]any)
	if _, ok := plan["missing_evidence"]; ok {
		t.Fatalf("plan[missing_evidence] = %#v, want absent", plan["missing_evidence"])
	}
}
