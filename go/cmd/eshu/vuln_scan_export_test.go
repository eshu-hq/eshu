package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunVulnScanRepoSARIFExportPreservesScannerReportContract(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()
	resetVulnScanClock := stubVulnScanClock(t)
	defer resetVulnScanClock()

	server := vulnScanReportTestServer(t, repoPath, `{"data":{"findings":[{"finding_id":"finding-sarif-1","cve_id":"CVE-2026-SARIF-0001","advisory_id":"GHSA-sarif-0001","package_id":"npm://registry.npmjs.org/synthetic-runtime-lib","package_name":"synthetic-runtime-lib","ecosystem":"npm","purl":"pkg:npm/synthetic-runtime-lib@2.3.4","impact_status":"possibly_affected","confidence":"partial","observed_version":"2.3.4","requested_range":"^2.3.0","vulnerable_range":"<2.3.5","fixed_version":"2.3.5","match_reason":"range_only_manifest","summary":"synthetic-runtime-lib vulnerable through 2.3.4","cvss_score":8.8,"cvss_vector":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H","epss_probability":"0.234","known_exploited":true,"priority_bucket":"high","repository_id":"repo-local","subject_digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111","image_ref":"registry.example.test/team/api@sha256:1111111111111111111111111111111111111111111111111111111111111111","runtime_reachability":"image_sbom","workload_ids":["workload-synthetic"],"service_ids":["service-synthetic"],"environments":["prod"],"dependency_scope":"runtime","dependency_path":["synthetic-root","synthetic-runtime-lib"],"dependency_depth":1,"direct_dependency":true,"missing_evidence":["workload_evidence"],"evidence_fact_ids":["fact-package-1","fact-sbom-1"],"source_freshness":"fresh","manifest_path":"services/api/package.json","start_line":12,"end_line":12,"provenance":{"selected_severity_label":"high","selected_severity_score":8.8,"selected_severity_vector":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H","advisory_sources":[{"source":"ghsa","advisory_id":"GHSA-sarif-0001","url":"https://github.com/advisories/GHSA-sarif-0001"},{"source":"nvd","advisory_id":"CVE-2026-SARIF-0001"}]},"remediation":{"current_version":"2.3.4","vulnerable_range":"<2.3.5","first_patched_version":"2.3.5","manifest_range":"^2.3.0","manifest_allows_fix":"allowed","confidence":"partial","reason":"direct_upgrade_allowed","direct":true,"missing_evidence":["workload_evidence"],"provider_payload_url":"https://private.example/alert/1"}}],"count":1,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_with_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":2,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":80,"freshness":"fresh"}],"unsupported_targets":[{"target_kind":"ecosystem","reason":"matcher_not_available","ecosystem":"swift","count":1}],"freshness":"fresh","counts":{"findings_returned":1,"evidence_facts_total":83}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`)
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("export", "sarif"); err != nil {
		t.Fatalf("Set(export) error = %v, want nil", err)
	}

	requireVulnScanExitCode(t, runVulnScanRepo(cmd, []string{repoPath}), 3)
	assertVulnScanGolden(t, out.Bytes(), "ready_with_findings.sarif.json")
	if bytes.Contains(out.Bytes(), []byte("provider_payload_url")) ||
		bytes.Contains(out.Bytes(), []byte("private.example")) {
		t.Fatalf("SARIF output leaked private provider payload data:\n%s", out.String())
	}
}

func TestRunVulnScanRepoSARIFExportPreservesEvidenceIncompleteWithoutLocation(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()
	resetVulnScanClock := stubVulnScanClock(t)
	defer resetVulnScanClock()

	server := vulnScanReportTestServer(t, repoPath, `{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"evidence_incomplete","target_scope":{"repository_id":"repo-local"},"missing_evidence":["owned_packages"],"incomplete_reasons":["no owned package facts reached reducer"],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":20}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`)
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("export", "sarif"); err != nil {
		t.Fatalf("Set(export) error = %v, want nil", err)
	}

	requireVulnScanExitCode(t, runVulnScanRepo(cmd, []string{repoPath}), 4)

	var sarif map[string]any
	if err := json.Unmarshal(out.Bytes(), &sarif); err != nil {
		t.Fatalf("json.Unmarshal(SARIF) error = %v, want nil; output=%s", err, out.String())
	}
	run := requireSARIFRun(t, sarif)
	results := requireSliceField(t, run, "results")
	if got, want := len(results), 1; got != want {
		t.Fatalf("SARIF results len = %d, want %d so evidence_incomplete is not exported as clean", got, want)
	}
	result := results[0].(map[string]any)
	if got, want := result["ruleId"], "ESHU-SCAN-EVIDENCE-INCOMPLETE"; got != want {
		t.Fatalf("status result ruleId = %#v, want %#v", got, want)
	}
	if _, ok := result["locations"]; ok {
		t.Fatalf("evidence-incomplete SARIF result invented a source location: %#v", result["locations"])
	}
	props := requireMapField(t, run, "properties")
	missing := requireSliceField(t, props, "eshu.missingEvidence")
	if got, want := missing[0], "owned_packages"; got != want {
		t.Fatalf("run missing evidence[0] = %#v, want %#v", got, want)
	}
}

func TestRunVulnScanRepoRejectsJSONAndSARIFExportTogether(t *testing.T) {
	cmd := newTestVulnScanRepoCommand(t)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("export", "sarif"); err != nil {
		t.Fatalf("Set(export) error = %v, want nil", err)
	}

	_, err := vulnScanRepoOptionsFromCommand(cmd, []string{t.TempDir()})
	if err == nil {
		t.Fatalf("vulnScanRepoOptionsFromCommand() error = nil, want json/export conflict")
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("vulnScanRepoOptionsFromCommand() error = %T %v, want commandExitError", err, err)
	}
	if got, want := exitErr.ExitCode(), 2; got != want {
		t.Fatalf("ExitCode() = %d, want %d", got, want)
	}
}

func assertVulnScanGolden(t *testing.T, got []byte, fixture string) {
	t.Helper()
	goldenPath := filepath.Join("testdata", "vuln_scan_sarif", fixture)
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if bytes.Equal(want, got) {
		return
	}
	var wantValue, gotValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("golden does not parse as JSON: %v\n%s", err, want)
	}
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("SARIF output does not parse as JSON: %v\n%s", err, got)
	}
	t.Fatalf("SARIF output diverges from golden:\nwant (%d bytes):\n%s\ngot (%d bytes):\n%s",
		len(want), want, len(got), got)
}

func requireSARIFRun(t *testing.T, sarif map[string]any) map[string]any {
	t.Helper()
	runs := requireSliceField(t, sarif, "runs")
	if got, want := len(runs), 1; got != want {
		t.Fatalf("runs len = %d, want %d", got, want)
	}
	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatalf("runs[0] = %#v, want object", runs[0])
	}
	return run
}

func stubVulnScanClock(t *testing.T) func() {
	t.Helper()
	original := vulnScanNow
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	vulnScanNow = func() time.Time {
		current := now
		now = now.Add(time.Second)
		return current
	}
	return func() {
		vulnScanNow = original
	}
}
