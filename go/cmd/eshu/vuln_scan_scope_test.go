package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVulnScanRepoCommandRegistersBroadFlag(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"vuln-scan", "repo"})
	if err != nil {
		t.Fatalf("rootCmd.Find(vuln-scan repo) error = %v, want nil", err)
	}
	broad := cmd.Flags().Lookup("broad")
	if broad == nil {
		t.Fatal("vuln-scan repo missing --broad flag")
	}
	if broad.DefValue != "false" {
		t.Fatalf("vuln-scan repo --broad default = %q, want false (scoped is default)", broad.DefValue)
	}
}

func TestRunVulnScanRepoDefaultScopedModeAttachesScopePlanAndPerformance(t *testing.T) {
	// In scoped (default) mode the CLI must surface a scope_plan derived from
	// the readiness envelope and a scan_performance block so operators can
	// prove the local one-shot scan honored the observed-dependency contract.
	//
	// The readiness envelope's `evidence_sources[].fact_count` is a count of
	// source facts (not unique packages or advisory sources), so the scope
	// plan and performance fields use *_facts names. `package.registry` is
	// intentionally omitted from this stub because the server only counts
	// registry metadata when a `package_id` anchor is supplied; the assertion
	// confirms a repo-only scan reports `package_registry_facts == 0`.
	repoPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoPath, "package.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatalf("write package.json error = %v, want nil", err)
	}

	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[{"finding_id":"f-1","cve_id":"CVE-2026-0001","package_id":"npm://registry.npmjs.org/ws","impact_status":"affected_exact","repository_id":"repo-local"}],"count":1,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_with_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":4,"freshness":"fresh","latest_observed_at":"2026-05-25T10:00:00Z"},{"family":"vulnerability.advisory","fact_count":120,"freshness":"fresh","latest_observed_at":"2026-05-25T09:00:00Z"}],"source_snapshots":[{"source":"osv","ecosystem":"npm","freshness":"fresh","complete":true,"cache_artifact_version":"v1","snapshot_digest":"abc"}],"freshness":"fresh","counts":{"findings_returned":1,"evidence_facts_total":124}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
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
		t.Fatalf("runVulnScanRepo() error = %v, want nil", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	data := payload["data"].(map[string]any)
	if got, want := data["scope_mode"], "scoped"; got != want {
		t.Fatalf("data[scope_mode] = %#v, want %#v", got, want)
	}
	plan, ok := data["scope_plan"].(map[string]any)
	if !ok {
		t.Fatalf("data[scope_plan] = %T, want object", data["scope_plan"])
	}
	if got, want := plan["mode"], "scoped"; got != want {
		t.Fatalf("plan[mode] = %#v, want %#v", got, want)
	}
	if got := plan["observed_dependency_facts"]; got == nil || toInt(t, got) != 4 {
		t.Fatalf("plan[observed_dependency_facts] = %#v, want 4", got)
	}
	if got := plan["advisory_facts"]; got == nil || toInt(t, got) != 120 {
		t.Fatalf("plan[advisory_facts] = %#v, want 120", got)
	}
	if got := plan["package_registry_facts"]; got == nil || toInt(t, got) != 0 {
		t.Fatalf("plan[package_registry_facts] = %#v, want 0 (repo-only scope omits registry counts)", got)
	}
	if got, want := plan["freshness"], "fresh"; got != want {
		t.Fatalf("plan[freshness] = %#v, want %#v", got, want)
	}
	if got, want := plan["stop_threshold"], "ready_with_findings"; got != want {
		t.Fatalf("plan[stop_threshold] = %#v, want %#v", got, want)
	}
	snaps, ok := plan["source_snapshots"].([]any)
	if !ok || len(snaps) == 0 {
		t.Fatalf("plan[source_snapshots] = %#v, want at least one entry", plan["source_snapshots"])
	}
	perf, ok := data["scan_performance"].(map[string]any)
	if !ok {
		t.Fatalf("data[scan_performance] = %T, want object", data["scan_performance"])
	}
	for _, key := range []string{"started_at", "completed_at", "wall_time_ms", "scope_mode", "stop_threshold", "observed_dependency_facts", "advisory_facts", "package_registry_facts"} {
		if _, has := perf[key]; !has {
			t.Fatalf("scan_performance missing key %q; perf=%#v", key, perf)
		}
	}
	if got, want := perf["scope_mode"], "scoped"; got != want {
		t.Fatalf("perf[scope_mode] = %#v, want %#v", got, want)
	}
}

func TestRunVulnScanRepoScopedModeFailsClosedOnStaleAdvisoryCache(t *testing.T) {
	// When the readiness envelope's aggregate `freshness` is `stale` and the
	// server still classifies the scope as `ready_zero_findings`, scoped mode
	// must downgrade to `evidence_incomplete` and record
	// `advisory_cache_stale`. The CLI does not gate on per-source-snapshot
	// freshness because the server aggregates `source_snapshots[]` globally
	// rather than by the repository scope (see server readiness postgres
	// CTE `vulnerability_source_snapshot`).
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":3,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":50,"freshness":"stale"}],"freshness":"stale","counts":{"findings_returned":0,"evidence_facts_total":53}}},"truth":{"level":"exact","freshness":{"state":"stale"}},"error":null}`))
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
		t.Fatal("runVulnScanRepo() error = nil, want fail-closed for stale envelope freshness")
	}

	var payload map[string]any
	if uerr := json.Unmarshal(out.Bytes(), &payload); uerr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", uerr, out.String())
	}
	data := payload["data"].(map[string]any)
	if got, want := data["readiness_state"], "evidence_incomplete"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v (stale envelope freshness must fail closed in scoped mode)", got, want)
	}
	plan, ok := data["scope_plan"].(map[string]any)
	if !ok {
		t.Fatalf("data[scope_plan] = %T, want object", data["scope_plan"])
	}
	if got, want := plan["stop_threshold"], "evidence_incomplete"; got != want {
		t.Fatalf("plan[stop_threshold] = %#v, want %#v", got, want)
	}
	missing, ok := plan["missing_evidence"].([]any)
	if !ok || len(missing) == 0 {
		t.Fatalf("plan[missing_evidence] = %#v, want advisory_cache_stale", plan["missing_evidence"])
	}
	if got := missing[0]; got != "advisory_cache_stale" {
		t.Fatalf("plan[missing_evidence][0] = %#v, want %q", got, "advisory_cache_stale")
	}
}

func TestRunVulnScanRepoScopedModeIgnoresGlobalStaleSnapshotsWhenEnvelopeFresh(t *testing.T) {
	// `source_snapshots[]` is aggregated globally by the readiness store,
	// so a stale Python snapshot must not flip a repo-only npm scan whose
	// repo-anchored evidence (package.consumption + vulnerability.advisory)
	// is fresh and whose envelope-level freshness is `fresh`. This regression
	// guards against the CLI re-introducing a per-snapshot fail-closed gate.
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":3,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":50,"freshness":"fresh"}],"source_snapshots":[{"source":"osv","ecosystem":"npm","freshness":"fresh","complete":true},{"source":"osv","ecosystem":"pypi","freshness":"stale","complete":false,"warning_code":"stale_cache","warning_message":"unrelated python snapshot"}],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":53}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
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
		t.Fatalf("runVulnScanRepo() error = %v, want nil (unrelated global stale snapshot must not flip a fresh repo-scoped run)", err)
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
	if got := plan["missing_evidence"]; got != nil {
		t.Fatalf("plan[missing_evidence] = %#v, want nil (no scoped fail-closed)", got)
	}
}

func TestRunVulnScanRepoScopedModePassesThroughServerTargetIncomplete(t *testing.T) {
	// The server already classifies in-flight advisory ingestion as
	// `target_incomplete` via `vulnerability_source_snapshot.target_incomplete`.
	// Scoped mode passes that verdict through unchanged rather than adding a
	// duplicate CLI-side guard (the per-snapshot list is global and not safe
	// to gate on).
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"target_incomplete","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":2,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":10,"freshness":"fresh"}],"incomplete_reasons":["advisory snapshot still ingesting"],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":12}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
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
		t.Fatalf("runVulnScanRepo() error = %v, want nil (server target_incomplete passes through)", err)
	}

	var payload map[string]any
	if uerr := json.Unmarshal(out.Bytes(), &payload); uerr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", uerr, out.String())
	}
	data := payload["data"].(map[string]any)
	if got, want := data["readiness_state"], "target_incomplete"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v", got, want)
	}
	plan := data["scope_plan"].(map[string]any)
	if got, want := plan["stop_threshold"], "target_incomplete"; got != want {
		t.Fatalf("plan[stop_threshold] = %#v, want %#v", got, want)
	}
}

func TestRunVulnScanRepoBroadModeSkipsScopeGuards(t *testing.T) {
	// --broad must override the scoped fail-closed guards so operators can
	// explicitly accept wider coverage (or a stale cache) and still receive
	// the server-provided readiness verdict.
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":3,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":50,"freshness":"stale"}],"source_snapshots":[{"source":"osv","ecosystem":"npm","freshness":"stale","complete":true}],"freshness":"stale","counts":{"findings_returned":0,"evidence_facts_total":53}}},"truth":{"level":"exact","freshness":{"state":"stale"}},"error":null}`))
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

	if err := runVulnScanRepo(cmd, []string{repoPath}); err != nil {
		t.Fatalf("runVulnScanRepo() error = %v, want nil (broad mode tolerates stale cache)", err)
	}

	var payload map[string]any
	if uerr := json.Unmarshal(out.Bytes(), &payload); uerr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", uerr, out.String())
	}
	data := payload["data"].(map[string]any)
	if got, want := data["scope_mode"], "broad"; got != want {
		t.Fatalf("data[scope_mode] = %#v, want %#v", got, want)
	}
	plan := data["scope_plan"].(map[string]any)
	if got, want := plan["mode"], "broad"; got != want {
		t.Fatalf("plan[mode] = %#v, want %#v", got, want)
	}
	if got, want := data["readiness_state"], "ready_zero_findings"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v (server verdict preserved)", got, want)
	}
	warnings, _ := data["warnings"].([]any)
	hasBroadNote := false
	for _, value := range warnings {
		if msg, ok := value.(string); ok && strings.Contains(msg, "broad") {
			hasBroadNote = true
			break
		}
	}
	if !hasBroadNote {
		t.Fatalf("data[warnings] = %#v, want a warning noting broad mode skipped scope guards", warnings)
	}
}

func TestRunVulnScanRepoScopedModeSurfacesEvidenceIncompleteWhenNoOwnedDeps(t *testing.T) {
	// When the server already classifies the scope as evidence_incomplete
	// (e.g. zero observed dependencies), the scoped CLI passes the verdict
	// through unmodified. The scope plan still records that
	// observed_dependency_facts is zero so operators can see why the server
	// fell back to evidence_incomplete without a follow-up call.
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-empty","name":"empty","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(`{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"evidence_incomplete","target_scope":{"repository_id":"repo-empty"},"evidence_sources":[{"family":"vulnerability.advisory","fact_count":50,"freshness":"fresh"}],"missing_evidence":["owned_packages"],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":50}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`))
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
		t.Fatalf("runVulnScanRepo() error = %v, want nil (server already classifies non-ready)", err)
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
	if got := plan["observed_dependency_facts"]; toInt(t, got) != 0 {
		t.Fatalf("plan[observed_dependency_facts] = %#v, want 0", got)
	}
	if got, want := plan["stop_threshold"], "evidence_incomplete"; got != want {
		t.Fatalf("plan[stop_threshold] = %#v, want %#v", got, want)
	}
}

func toInt(t *testing.T, value any) int {
	t.Helper()
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case json.Number:
		n, err := typed.Int64()
		if err != nil {
			t.Fatalf("toInt(json.Number) error = %v, want nil", err)
		}
		return int(n)
	default:
		t.Fatalf("toInt(%T) = unsupported value %#v", value, value)
		return 0
	}
}
