// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type vulnScanFixtureMatrixCase struct {
	name                string
	fixture             string
	repositoryID        string
	readinessState      string
	freshness           string
	exitCode            int
	findings            []map[string]any
	evidenceSources     []map[string]any
	missingEvidence     []any
	unsupportedTargets  []map[string]any
	incompleteReasons   []any
	sourceSnapshots     []map[string]any
	wantMissingEvidence string
	wantTerminalNeedle  string
}

func TestRunVulnScanRepoFixtureMatrixProvesStandaloneReadinessStates(t *testing.T) {
	cases := []vulnScanFixtureMatrixCase{
		{
			name:           "vulnerable dependency",
			fixture:        "vulnerable-npm",
			repositoryID:   "repo-fixture-vulnerable-npm",
			readinessState: "ready_with_findings",
			freshness:      "fresh",
			exitCode:       3,
			findings: []map[string]any{
				{
					"finding_id":        "finding-synthetic-vulnerable-npm",
					"cve_id":            "CVE-2026-SYNTHETIC-NPM",
					"advisory_id":       "GHSA-synthetic-npm-0001",
					"package_id":        "npm:synthetic-vulnerable-npm",
					"package_name":      "synthetic-vulnerable-npm",
					"ecosystem":         "npm",
					"impact_status":     "affected_exact",
					"observed_version":  "1.0.0",
					"fixed_version":     "1.0.1",
					"repository_id":     "repo-fixture-vulnerable-npm",
					"evidence_fact_ids": []any{"fact-fixture-package-synthetic-npm", "fact-fixture-advisory-synthetic-npm"},
				},
			},
			evidenceSources: []map[string]any{
				{"family": "package.consumption", "fact_count": 2, "freshness": "fresh"},
				{"family": "package.registry", "fact_count": 1, "freshness": "fresh"},
				{"family": "vulnerability.advisory", "fact_count": 4, "freshness": "fresh"},
			},
			sourceSnapshots: []map[string]any{
				{"source": "osv", "ecosystem": "npm", "freshness": "fresh", "complete": true},
			},
			wantTerminalNeedle: "synthetic-vulnerable-npm affected_exact fixed=1.0.1",
		},
		{
			name:           "ready zero with collected evidence",
			fixture:        "ready-zero-npm",
			repositoryID:   "repo-fixture-ready-zero-npm",
			readinessState: "ready_zero_findings",
			freshness:      "fresh",
			exitCode:       0,
			evidenceSources: []map[string]any{
				{"family": "package.consumption", "fact_count": 1, "freshness": "fresh"},
				{"family": "package.registry", "fact_count": 1, "freshness": "fresh"},
				{"family": "vulnerability.advisory", "fact_count": 5, "freshness": "fresh"},
			},
			sourceSnapshots: []map[string]any{
				{"source": "osv", "ecosystem": "npm", "freshness": "fresh", "complete": true},
			},
		},
		{
			name:           "incomplete advisory evidence",
			fixture:        "incomplete-advisory-npm",
			repositoryID:   "repo-fixture-incomplete-advisory-npm",
			readinessState: "evidence_incomplete",
			freshness:      "fresh",
			exitCode:       4,
			evidenceSources: []map[string]any{
				{"family": "package.consumption", "fact_count": 1, "freshness": "fresh"},
				{"family": "package.registry", "fact_count": 1, "freshness": "fresh"},
			},
			missingEvidence:     []any{"advisory_sources"},
			wantMissingEvidence: "advisory_sources",
		},
		{
			name:           "incomplete package evidence",
			fixture:        "incomplete-package-npm",
			repositoryID:   "repo-fixture-incomplete-package-npm",
			readinessState: "ready_zero_findings",
			freshness:      "fresh",
			exitCode:       4,
			evidenceSources: []map[string]any{
				{"family": "package.consumption", "fact_count": 1, "freshness": "fresh"},
				{"family": "vulnerability.advisory", "fact_count": 4, "freshness": "fresh"},
			},
			wantMissingEvidence: "package_registry_metadata",
		},
		{
			name:           "unsupported ecosystem",
			fixture:        "unsupported-pub",
			repositoryID:   "repo-fixture-unsupported-pub",
			readinessState: "unsupported",
			freshness:      "fresh",
			exitCode:       5,
			evidenceSources: []map[string]any{
				{"family": "package.consumption", "fact_count": 1, "freshness": "fresh"},
			},
			missingEvidence: []any{"unsupported_targets"},
			unsupportedTargets: []map[string]any{
				{"target_kind": "ecosystem", "reason": "matcher_not_available", "ecosystem": "pub", "count": 1},
			},
			wantMissingEvidence: "unsupported_targets",
			wantTerminalNeedle:  "Unsupported targets: ecosystem/matcher_not_available count=1",
		},
		{
			name:           "stale advisory cache",
			fixture:        "stale-cache-npm",
			repositoryID:   "repo-fixture-stale-cache-npm",
			readinessState: "ready_zero_findings",
			freshness:      "stale",
			exitCode:       4,
			evidenceSources: []map[string]any{
				{"family": "package.consumption", "fact_count": 1, "freshness": "fresh"},
				{"family": "package.registry", "fact_count": 1, "freshness": "fresh"},
				{"family": "vulnerability.advisory", "fact_count": 4, "freshness": "stale"},
			},
			sourceSnapshots: []map[string]any{
				{"source": "osv", "ecosystem": "npm", "freshness": "stale", "complete": true, "warning_code": "stale_cache"},
			},
			wantMissingEvidence: "advisory_cache_stale",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/json", func(t *testing.T) {
			out, err := runVulnScanFixtureMatrixCase(t, tc, true)
			requireVulnScanExitCode(t, err, tc.exitCode)
			payload := decodeVulnScanPayload(t, out)
			data := payload["data"].(map[string]any)
			if got, want := data["repository_id"], tc.repositoryID; got != want {
				t.Fatalf("data[repository_id] = %#v, want %#v", got, want)
			}
			wantState := tc.readinessState
			if tc.wantMissingEvidence == "package_registry_metadata" || tc.wantMissingEvidence == "advisory_cache_stale" {
				wantState = "evidence_incomplete"
			}
			if got := data["readiness_state"]; got != wantState {
				t.Fatalf("data[readiness_state] = %#v, want %#v", got, wantState)
			}
			report := requireMapField(t, data, "report")
			summary := requireMapField(t, report, "summary")
			if got := toInt(t, summary["exit_code"]); got != tc.exitCode {
				t.Fatalf("summary[exit_code] = %d, want %d", got, tc.exitCode)
			}
			readiness := requireMapField(t, report, "readiness")
			if got := readiness["freshness"]; got != tc.freshness {
				t.Fatalf("report readiness freshness = %#v, want %#v", got, tc.freshness)
			}
			if tc.wantMissingEvidence != "" {
				if !sliceContainsString(requireSliceField(t, readiness, "missing_evidence"), tc.wantMissingEvidence) {
					t.Fatalf("report readiness missing_evidence = %#v, want %q", readiness["missing_evidence"], tc.wantMissingEvidence)
				}
			}
			perf := requireMapField(t, data, "scan_performance")
			if got := toInt(t, perf["wall_time_ms"]); got < 0 {
				t.Fatalf("scan_performance.wall_time_ms = %d, want non-negative timing", got)
			}
		})

		t.Run(tc.name+"/terminal", func(t *testing.T) {
			out, err := runVulnScanFixtureMatrixCase(t, tc, false)
			requireVulnScanExitCode(t, err, tc.exitCode)
			rendered := out.String()
			for _, want := range []string{
				"Readiness: state=",
				" freshness=" + tc.freshness,
				fmt.Sprintf("Exit: code=%d", tc.exitCode),
				"Performance: wall_time_ms=",
			} {
				if !strings.Contains(rendered, want) {
					t.Fatalf("terminal output missing %q; output:\n%s", want, rendered)
				}
			}
			if tc.wantMissingEvidence != "" && !strings.Contains(rendered, "Missing evidence: "+tc.wantMissingEvidence) {
				t.Fatalf("terminal output missing final missing evidence %q; output:\n%s", tc.wantMissingEvidence, rendered)
			}
			if tc.wantTerminalNeedle != "" && !strings.Contains(rendered, tc.wantTerminalNeedle) {
				t.Fatalf("terminal output missing %q; output:\n%s", tc.wantTerminalNeedle, rendered)
			}
		})
	}
}

func runVulnScanFixtureMatrixCase(t *testing.T, tc vulnScanFixtureMatrixCase, jsonOutput bool) (*bytes.Buffer, error) {
	t.Helper()
	reset := stubScanRuntime(t)
	defer reset()
	repoPath := copyVulnScanFixtureRepository(t, tc.fixture)
	server := vulnScanFixtureMatrixServer(t, repoPath, tc)
	defer server.Close()

	out := &bytes.Buffer{}
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if jsonOutput {
		if err := cmd.Flags().Set("json", "true"); err != nil {
			t.Fatalf("Set(json) error = %v, want nil", err)
		}
	}
	return out, runVulnScanRepo(cmd, []string{repoPath})
}

func copyVulnScanFixtureRepository(t *testing.T, fixture string) string {
	t.Helper()
	src := filepath.Join("testdata", "vuln_scan_repo_fixtures", fixture)
	dst := filepath.Join(t.TempDir(), fixture)
	if err := filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	}); err != nil {
		t.Fatalf("copy fixture %q error = %v, want nil", fixture, err)
	}
	if err := os.Mkdir(filepath.Join(dst, ".git"), 0o755); err != nil {
		t.Fatalf("create fixture .git error = %v, want nil", err)
	}
	return dst
}

func vulnScanFixtureMatrixServer(t *testing.T, repoPath string, tc vulnScanFixtureMatrixCase) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			writeFixtureJSON(t, w, map[string]any{
				"count": 1,
				"repositories": []map[string]any{
					{"id": tc.repositoryID, "name": tc.fixture, "path": repoPath, "local_path": repoPath},
				},
			})
		case "/api/v0/supply-chain/impact/findings":
			writeFixtureJSON(t, w, vulnScanFixtureImpactEnvelope(tc))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
}

func vulnScanFixtureImpactEnvelope(tc vulnScanFixtureMatrixCase) map[string]any {
	readiness := map[string]any{
		"readiness_state":  tc.readinessState,
		"target_scope":     map[string]any{"repository_id": tc.repositoryID},
		"evidence_sources": tc.evidenceSources,
		"freshness":        tc.freshness,
		"counts": map[string]any{
			"findings_returned":    len(tc.findings),
			"evidence_facts_total": fixtureEvidenceFactTotal(tc.evidenceSources),
		},
	}
	if len(tc.missingEvidence) > 0 {
		readiness["missing_evidence"] = tc.missingEvidence
	}
	if len(tc.unsupportedTargets) > 0 {
		readiness["unsupported_targets"] = tc.unsupportedTargets
	}
	if len(tc.incompleteReasons) > 0 {
		readiness["incomplete_reasons"] = tc.incompleteReasons
	}
	if len(tc.sourceSnapshots) > 0 {
		readiness["source_snapshots"] = tc.sourceSnapshots
	}
	return map[string]any{
		"data": map[string]any{
			"findings":  tc.findings,
			"count":     len(tc.findings),
			"limit":     50,
			"truncated": false,
			"readiness": readiness,
		},
		"truth": map[string]any{"level": "exact", "freshness": map[string]any{"state": tc.freshness}},
		"error": nil,
	}
}

func fixtureEvidenceFactTotal(sources []map[string]any) int {
	total := 0
	for _, source := range sources {
		if count, ok := source["fact_count"].(int); ok {
			total += count
		}
	}
	return total
}

func writeFixtureJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("json.NewEncoder().Encode() error = %v, want nil", err)
	}
}

func sliceContainsString(values []any, want string) bool {
	for _, value := range values {
		if got, ok := value.(string); ok && got == want {
			return true
		}
	}
	return false
}
