// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunVulnScanRepoJSONReportPreservesScannerContractAndFindingsExit(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := vulnScanReportTestServer(t, repoPath, `{"data":{"findings":[{"finding_id":"finding-1","cve_id":"CVE-2026-0001","advisory_id":"GHSA-xxxx-yyyy-zzzz","package_id":"npm://registry.npmjs.org/ws","package_name":"ws","ecosystem":"npm","impact_status":"affected_exact","priority_bucket":"high","priority_score":91,"fixed_version":"8.17.1","evidence_fact_ids":["fact-package-1","fact-advisory-1"],"remediation":{"first_patched_version":"8.17.1","confidence":"exact","provider_payload_url":"https://private.example/alert/1"}}],"count":1,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_with_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":2,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":80,"freshness":"fresh"}],"unsupported_targets":[{"target_kind":"ecosystem","reason":"matcher_not_available","ecosystem":"swift","count":1}],"freshness":"fresh","counts":{"findings_returned":1,"evidence_facts_total":83}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`)
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newReportTestVulnScanRepoCommand(t, server.URL, out)

	err := runVulnScanRepo(cmd, []string{repoPath})
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("runVulnScanRepo() error = %T %v, want commandExitError for findings", err, err)
	}
	if got, want := exitErr.ExitCode(), 3; got != want {
		t.Fatalf("ExitCode() = %d, want %d for ready_with_findings", got, want)
	}

	payload := decodeVulnScanPayload(t, out)
	if payload["error"] != nil {
		t.Fatalf("payload[error] = %#v, want nil for scanner findings exit", payload["error"])
	}
	data := payload["data"].(map[string]any)
	report := data["report"].(map[string]any)
	if got, want := report["schema_version"], "eshu.vulnerability_report.v1"; got != want {
		t.Fatalf("report[schema_version] = %#v, want %#v", got, want)
	}
	summary := report["summary"].(map[string]any)
	if got, want := toInt(t, summary["exit_code"]), 3; got != want {
		t.Fatalf("summary[exit_code] = %d, want %d", got, want)
	}
	if got, want := summary["exit_reason"], "findings_present"; got != want {
		t.Fatalf("summary[exit_reason] = %#v, want %#v", got, want)
	}
	readiness := report["readiness"].(map[string]any)
	if got, want := readiness["state"], "ready_with_findings"; got != want {
		t.Fatalf("report readiness state = %#v, want %#v", got, want)
	}
	unsupported := readiness["unsupported_targets"].([]any)
	if len(unsupported) != 1 {
		t.Fatalf("report unsupported_targets = %#v, want one entry", unsupported)
	}
	findings := report["findings"].([]any)
	finding := findings[0].(map[string]any)
	handles := finding["evidence_handles"].([]any)
	if got, want := len(handles), 2; got != want {
		t.Fatalf("evidence_handles len = %d, want %d", got, want)
	}
	remediation := finding["remediation"].(map[string]any)
	if got, want := remediation["first_patched_version"], "8.17.1"; got != want {
		t.Fatalf("remediation[first_patched_version] = %#v, want %#v", got, want)
	}
	if _, ok := remediation["provider_payload_url"]; ok {
		t.Fatalf("remediation copied private provider payload field: %#v", remediation)
	}
}

func TestRunVulnScanRepoJSONReportPreservesTargetPackageImageAndVersionContext(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := vulnScanReportTestServer(t, repoPath, `{"data":{"findings":[{"finding_id":"finding-runtime-1","cve_id":"CVE-2026-0002","advisory_id":"GHSA-yyyy-zzzz-1111","package_id":"npm://registry.npmjs.org/synthetic-runtime-lib","package_name":"synthetic-runtime-lib","ecosystem":"npm","purl":"pkg:npm/synthetic-runtime-lib@2.3.4","impact_status":"possibly_affected","confidence":"partial","observed_version":"2.3.4","requested_range":"^2.3.0","vulnerable_range":"<2.3.5","fixed_version":"2.3.5","match_reason":"range_only_manifest","repository_id":"repo-local","subject_digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111","image_ref":"registry.example.test/team/api@sha256:1111111111111111111111111111111111111111111111111111111111111111","runtime_reachability":"image_sbom","workload_ids":["workload-synthetic"],"service_ids":["service-synthetic"],"environments":["dev"],"dependency_scope":"runtime","dependency_path":["synthetic-root","synthetic-runtime-lib"],"dependency_depth":1,"direct_dependency":true,"missing_evidence":["workload_evidence"],"evidence_fact_ids":["fact-package-1","fact-sbom-1"],"source_freshness":"fresh","remediation":{"current_version":"2.3.4","vulnerable_range":"<2.3.5","first_patched_version":"2.3.5","manifest_range":"^2.3.0","manifest_allows_fix":"allowed","confidence":"partial","reason":"direct_upgrade_allowed"}}],"count":1,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_with_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":2,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"sbom.component","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":80,"freshness":"fresh"}],"freshness":"fresh","counts":{"findings_returned":1,"evidence_facts_total":84}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`)
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newReportTestVulnScanRepoCommand(t, server.URL, out)

	requireVulnScanExitCode(t, runVulnScanRepo(cmd, []string{repoPath}), 3)

	payload := decodeVulnScanPayload(t, out)
	data := payload["data"].(map[string]any)
	report := data["report"].(map[string]any)
	findings := report["findings"].([]any)
	finding := findings[0].(map[string]any)

	target := requireMapField(t, finding, "target")
	if got, want := target["repository_id"], "repo-local"; got != want {
		t.Fatalf("target[repository_id] = %#v, want %#v", got, want)
	}
	if got, want := target["subject_digest"], "sha256:1111111111111111111111111111111111111111111111111111111111111111"; got != want {
		t.Fatalf("target[subject_digest] = %#v, want %#v", got, want)
	}
	if got, want := target["runtime_reachability"], "image_sbom"; got != want {
		t.Fatalf("target[runtime_reachability] = %#v, want %#v", got, want)
	}
	if got, want := target["image_ref"], "registry.example.test/team/api@sha256:1111111111111111111111111111111111111111111111111111111111111111"; got != want {
		t.Fatalf("target[image_ref] = %#v, want %#v", got, want)
	}
	workloadIDs := requireSliceField(t, target, "workload_ids")
	if got, want := workloadIDs[0], "workload-synthetic"; got != want {
		t.Fatalf("target[workload_ids][0] = %#v, want %#v", got, want)
	}

	pkg := requireMapField(t, finding, "package")
	if got, want := pkg["ecosystem"], "npm"; got != want {
		t.Fatalf("package[ecosystem] = %#v, want %#v", got, want)
	}
	if got, want := pkg["package_id"], "npm://registry.npmjs.org/synthetic-runtime-lib"; got != want {
		t.Fatalf("package[package_id] = %#v, want %#v", got, want)
	}
	if got, want := pkg["purl"], "pkg:npm/synthetic-runtime-lib@2.3.4"; got != want {
		t.Fatalf("package[purl] = %#v, want %#v", got, want)
	}
	dependencyPath := requireSliceField(t, pkg, "dependency_path")
	if got, want := dependencyPath[1], "synthetic-runtime-lib"; got != want {
		t.Fatalf("package[dependency_path][1] = %#v, want %#v", got, want)
	}

	affected := requireMapField(t, finding, "affected")
	if got, want := affected["status"], "possibly_affected"; got != want {
		t.Fatalf("affected[status] = %#v, want %#v", got, want)
	}
	if got, want := affected["observed_version"], "2.3.4"; got != want {
		t.Fatalf("affected[observed_version] = %#v, want %#v", got, want)
	}
	if got, want := affected["requested_range"], "^2.3.0"; got != want {
		t.Fatalf("affected[requested_range] = %#v, want %#v", got, want)
	}
	if got, want := affected["vulnerable_range"], "<2.3.5"; got != want {
		t.Fatalf("affected[vulnerable_range] = %#v, want %#v", got, want)
	}
	if got, want := affected["fixed_version"], "2.3.5"; got != want {
		t.Fatalf("affected[fixed_version] = %#v, want %#v", got, want)
	}
	missingEvidence := requireSliceField(t, finding, "missing_evidence")
	if got, want := missingEvidence[0], "workload_evidence"; got != want {
		t.Fatalf("missing_evidence[0] = %#v, want %#v", got, want)
	}
}

func TestRunVulnScanRepoJSONReportPreservesMalformedVersionAndRangeEvidence(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := vulnScanReportTestServer(t, repoPath, `{"data":{"findings":[{"finding_id":"finding-malformed-version","cve_id":"CVE-2026-0101","advisory_id":"GHSA-malformed-version","package_id":"npm://registry.npmjs.org/bad-version","package_name":"bad-version","ecosystem":"npm","impact_status":"unknown_impact","confidence":"partial","observed_version":"not-semver","requested_range":"^1.0.0","vulnerable_range":"<1.2.0","match_reason":"malformed_installed_version","repository_id":"repo-local","missing_evidence":["installed_version_malformed"],"evidence_fact_ids":["fact-package-malformed"],"source_freshness":"fresh","remediation":{"current_version":"not-semver","vulnerable_range":"<1.2.0","manifest_range":"^1.0.0","confidence":"unknown","reason":"installed_version_malformed","missing_evidence":["installed_version_malformed"]}},{"finding_id":"finding-malformed-range","cve_id":"CVE-2026-0102","advisory_id":"GHSA-malformed-range","package_id":"npm://registry.npmjs.org/bad-range","package_name":"bad-range","ecosystem":"npm","impact_status":"unknown_impact","confidence":"partial","observed_version":"1.0.0","requested_range":"^1.0.0","vulnerable_range":"<<bad","match_reason":"malformed_advisory_range","repository_id":"repo-local","missing_evidence":["vulnerable_range"],"evidence_fact_ids":["fact-advisory-malformed"],"source_freshness":"fresh","remediation":{"current_version":"1.0.0","vulnerable_range":"<<bad","manifest_range":"^1.0.0","confidence":"unknown","reason":"manifest_range_malformed","missing_evidence":["vulnerable_range"]}}],"count":2,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_with_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":2,"freshness":"fresh"},{"family":"package.registry","fact_count":2,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":2,"freshness":"fresh"}],"freshness":"fresh","counts":{"findings_returned":2,"evidence_facts_total":6}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`)
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newReportTestVulnScanRepoCommand(t, server.URL, out)

	requireVulnScanExitCode(t, runVulnScanRepo(cmd, []string{repoPath}), 3)

	payload := decodeVulnScanPayload(t, out)
	data := payload["data"].(map[string]any)
	report := data["report"].(map[string]any)
	findings := report["findings"].([]any)
	if got, want := len(findings), 2; got != want {
		t.Fatalf("len(findings) = %d, want %d", got, want)
	}

	versionFinding := findings[0].(map[string]any)
	versionAffected := requireMapField(t, versionFinding, "affected")
	if got, want := versionAffected["observed_version"], "not-semver"; got != want {
		t.Fatalf("malformed version observed_version = %#v, want %#v", got, want)
	}
	if got, want := versionAffected["match_reason"], "malformed_installed_version"; got != want {
		t.Fatalf("malformed version match_reason = %#v, want %#v", got, want)
	}
	versionMissing := requireSliceField(t, versionFinding, "missing_evidence")
	if got, want := versionMissing[0], "installed_version_malformed"; got != want {
		t.Fatalf("malformed version missing_evidence[0] = %#v, want %#v", got, want)
	}
	versionRemediation := requireMapField(t, versionFinding, "remediation")
	if got, want := versionRemediation["reason"], "installed_version_malformed"; got != want {
		t.Fatalf("malformed version remediation.reason = %#v, want %#v", got, want)
	}

	rangeFinding := findings[1].(map[string]any)
	rangeAffected := requireMapField(t, rangeFinding, "affected")
	if got, want := rangeAffected["vulnerable_range"], "<<bad"; got != want {
		t.Fatalf("malformed advisory vulnerable_range = %#v, want %#v", got, want)
	}
	if got, want := rangeAffected["match_reason"], "malformed_advisory_range"; got != want {
		t.Fatalf("malformed advisory match_reason = %#v, want %#v", got, want)
	}
	rangeMissing := requireSliceField(t, rangeFinding, "missing_evidence")
	if got, want := rangeMissing[0], "vulnerable_range"; got != want {
		t.Fatalf("malformed advisory missing_evidence[0] = %#v, want %#v", got, want)
	}
	rangeRemediation := requireMapField(t, rangeFinding, "remediation")
	if got, want := rangeRemediation["reason"], "manifest_range_malformed"; got != want {
		t.Fatalf("malformed advisory remediation.reason = %#v, want %#v", got, want)
	}
}

func TestRunVulnScanRepoExitCodesPreserveReadinessClasses(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		wantCode  int
		wantError bool
	}{
		{
			name:     "ready zero",
			response: `{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":1,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":20,"freshness":"fresh"}],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":22}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`,
			wantCode: 0,
		},
		{
			name:      "evidence incomplete",
			response:  `{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"evidence_incomplete","target_scope":{"repository_id":"repo-local"},"missing_evidence":["owned_packages"],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":20}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`,
			wantCode:  4,
			wantError: true,
		},
		{
			name:      "unsupported",
			response:  `{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"unsupported","target_scope":{"repository_id":"repo-local"},"missing_evidence":["unsupported_targets"],"unsupported_targets":[{"target_kind":"ecosystem","reason":"matcher_not_available","ecosystem":"swift","count":2}],"freshness":"fresh","counts":{"findings_returned":0,"evidence_facts_total":20}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`,
			wantCode:  5,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoPath := t.TempDir()
			reset := stubScanRuntime(t)
			defer reset()

			server := vulnScanReportTestServer(t, repoPath, tt.response)
			defer server.Close()

			out := &bytes.Buffer{}
			cmd := newReportTestVulnScanRepoCommand(t, server.URL, out)

			err := runVulnScanRepo(cmd, []string{repoPath})
			if tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("runVulnScanRepo() error = %v, want nil", err)
				}
			} else {
				var exitErr commandExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("runVulnScanRepo() error = %T %v, want commandExitError", err, err)
				}
				if got := exitErr.ExitCode(); got != tt.wantCode {
					t.Fatalf("ExitCode() = %d, want %d", got, tt.wantCode)
				}
			}

			payload := decodeVulnScanPayload(t, out)
			data := payload["data"].(map[string]any)
			report := data["report"].(map[string]any)
			summary := report["summary"].(map[string]any)
			if got := toInt(t, summary["exit_code"]); got != tt.wantCode {
				t.Fatalf("summary[exit_code] = %d, want %d", got, tt.wantCode)
			}
			if hasError := payload["error"] != nil; hasError != tt.wantError {
				t.Fatalf("payload[error] present = %v, want %v", hasError, tt.wantError)
			}
		})
	}
}

func TestRunVulnScanRepoScopedModeFailsClosedOnUnknownFreshness(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := vulnScanReportTestServer(t, repoPath, `{"data":{"findings":[],"count":0,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_zero_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":2,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":80,"freshness":"unknown"}],"freshness":"unknown","counts":{"findings_returned":0,"evidence_facts_total":83}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`)
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newReportTestVulnScanRepoCommand(t, server.URL, out)

	requireVulnScanExitCode(t, runVulnScanRepo(cmd, []string{repoPath}), 4)

	payload := decodeVulnScanPayload(t, out)
	if payload["error"] == nil {
		t.Fatalf("payload[error] = nil, want fail-closed error for unknown freshness")
	}
	data := payload["data"].(map[string]any)
	if got, want := data["readiness_state"], "evidence_incomplete"; got != want {
		t.Fatalf("data[readiness_state] = %#v, want %#v", got, want)
	}
	scopePlan := requireMapField(t, data, "scope_plan")
	missing := requireSliceField(t, scopePlan, "missing_evidence")
	if got, want := missing[0], "advisory_cache_freshness_unknown"; got != want {
		t.Fatalf("scope_plan missing_evidence[0] = %#v, want %#v", got, want)
	}
	report := requireMapField(t, data, "report")
	summary := requireMapField(t, report, "summary")
	if got, want := toInt(t, summary["exit_code"]), 4; got != want {
		t.Fatalf("summary[exit_code] = %d, want %d", got, want)
	}
}

func TestRenderVulnScanRepoSummaryIncludesReadinessEvidenceAndRemediation(t *testing.T) {
	result := vulnScanRepoResult{
		ReadinessState: "unsupported",
		ScopeMode:      vulnScanScopeModeScoped,
		RepositoryID:   "repo-local",
		Count:          1,
		Readiness: map[string]any{
			"freshness":        "fresh",
			"missing_evidence": []any{"unsupported_targets"},
			"unsupported_targets": []any{
				map[string]any{"target_kind": "ecosystem", "reason": "matcher_not_available", "ecosystem": "swift", "count": float64(1)},
			},
			"counts": map[string]any{"evidence_facts_total": float64(82)},
		},
		Findings: []map[string]any{
			{
				"finding_id":        "finding-1",
				"cve_id":            "CVE-2026-0001",
				"package_name":      "ws",
				"impact_status":     "affected_exact",
				"fixed_version":     "8.17.1",
				"evidence_fact_ids": []any{"fact-package-1"},
			},
		},
	}
	out := &bytes.Buffer{}

	if err := renderVulnScanRepoSummary(out, result); err != nil {
		t.Fatalf("renderVulnScanRepoSummary() error = %v, want nil", err)
	}

	rendered := out.String()
	for _, want := range []string{
		"Readiness: state=unsupported freshness=fresh",
		"Missing evidence: unsupported_targets",
		"Unsupported targets: ecosystem/matcher_not_available count=1",
		"Evidence facts: 82",
		"finding-1 CVE-2026-0001 ws affected_exact fixed=8.17.1 evidence=fact-package-1",
	} {
		if !bytes.Contains([]byte(rendered), []byte(want)) {
			t.Fatalf("summary missing %q; output:\n%s", want, rendered)
		}
	}
}

func TestRunVulnScanRepoTextSummaryRendersBeforeFindingsExit(t *testing.T) {
	repoPath := t.TempDir()
	reset := stubScanRuntime(t)
	defer reset()

	server := vulnScanReportTestServer(t, repoPath, `{"data":{"findings":[{"finding_id":"finding-1","cve_id":"CVE-2026-0001","package_name":"ws","impact_status":"affected_exact","fixed_version":"8.17.1","evidence_fact_ids":["fact-package-1"]}],"count":1,"limit":50,"truncated":false,"readiness":{"readiness_state":"ready_with_findings","target_scope":{"repository_id":"repo-local"},"evidence_sources":[{"family":"package.consumption","fact_count":2,"freshness":"fresh"},{"family":"package.registry","fact_count":1,"freshness":"fresh"},{"family":"vulnerability.advisory","fact_count":80,"freshness":"fresh"}],"freshness":"fresh","counts":{"findings_returned":1,"evidence_facts_total":83}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}`)
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}

	requireVulnScanExitCode(t, runVulnScanRepo(cmd, []string{repoPath}), 3)
	rendered := out.String()
	for _, want := range []string{
		"Vulnerability scan (scoped): ready_with_findings",
		"Readiness: state=ready_with_findings freshness=fresh",
		"Evidence facts: 83",
		"finding-1 CVE-2026-0001 ws affected_exact fixed=8.17.1 evidence=fact-package-1",
	} {
		if !bytes.Contains([]byte(rendered), []byte(want)) {
			t.Fatalf("text summary missing %q; output:\n%s", want, rendered)
		}
	}
}

func vulnScanReportTestServer(t *testing.T, repoPath string, impactResponse string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-local","name":"local","path":"` + repoPath + `","local_path":"` + repoPath + `"}]}`))
		case "/api/v0/supply-chain/impact/findings":
			_, _ = w.Write([]byte(impactResponse))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
}

func newReportTestVulnScanRepoCommand(t *testing.T, serviceURL string, out io.Writer) *cobra.Command {
	t.Helper()
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("service-url", serviceURL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Set(json) error = %v, want nil", err)
	}
	return cmd
}

func decodeVulnScanPayload(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	return payload
}

func requireMapField(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", key, parent[key])
	}
	return value
}

func requireSliceField(t *testing.T, parent map[string]any, key string) []any {
	t.Helper()
	value, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("%s = %#v, want array", key, parent[key])
	}
	return value
}

func requireVulnScanExitCode(t *testing.T, err error, want int) {
	t.Helper()
	if want == 0 {
		if err != nil {
			t.Fatalf("runVulnScanRepo() error = %v, want nil", err)
		}
		return
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("runVulnScanRepo() error = %T %v, want commandExitError", err, err)
	}
	if got := exitErr.ExitCode(); got != want {
		t.Fatalf("ExitCode() = %d, want %d", got, want)
	}
}
