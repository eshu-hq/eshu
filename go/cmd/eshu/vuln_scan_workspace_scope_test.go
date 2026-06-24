// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestRunVulnScanRepoWorkspaceScopeProof(t *testing.T) {
	cases := []struct {
		name                 string
		readinessState       string
		exitCode             int
		findings             []map[string]any
		missingEvidence      []any
		wantReportSourcePath string
		wantSubjectDigest    string
		wantServiceIDs       []any
		wantEnvironments     []any
	}{
		{
			name:           "npm workspace package finding stays on package manifest",
			readinessState: "ready_with_findings",
			exitCode:       3,
			findings: []map[string]any{
				workspaceScopeFinding("npm", "apps/npm-api/package-lock.json", ""),
			},
			wantReportSourcePath: "apps/npm-api/package-lock.json",
		},
		{
			name:                 "yarn workspace package can be ready zero",
			readinessState:       "ready_zero_findings",
			exitCode:             0,
			wantReportSourcePath: "",
		},
		{
			name:           "pnpm workspace package finding stays on importer lockfile",
			readinessState: "ready_with_findings",
			exitCode:       3,
			findings: []map[string]any{
				workspaceScopeFinding("npm", "apps/pnpm-admin/pnpm-lock.yaml", ""),
			},
			wantReportSourcePath: "apps/pnpm-admin/pnpm-lock.yaml",
		},
		{
			name:                 "nested go module can be ready zero without sibling pollution",
			readinessState:       "ready_zero_findings",
			exitCode:             0,
			wantReportSourcePath: "",
		},
		{
			name:                 "maven child module can be ready zero without parent pollution",
			readinessState:       "ready_zero_findings",
			exitCode:             0,
			wantReportSourcePath: "",
		},
		{
			name:                 "gradle subproject can be ready zero without root pollution",
			readinessState:       "ready_zero_findings",
			exitCode:             0,
			wantReportSourcePath: "",
		},
		{
			name:           "cargo renamed package finding stays on nested crate",
			readinessState: "ready_with_findings",
			exitCode:       3,
			findings: []map[string]any{
				workspaceScopeFinding("cargo", "crates/cargo-api/Cargo.toml", ""),
			},
			wantReportSourcePath: "crates/cargo-api/Cargo.toml",
		},
		{
			name:           "nuget project lockfile finding stays on project",
			readinessState: "ready_with_findings",
			exitCode:       3,
			findings: []map[string]any{
				workspaceScopeFinding("nuget", "dotnet/SyntheticWorker/packages.lock.json", ""),
			},
			wantReportSourcePath: "dotnet/SyntheticWorker/packages.lock.json",
		},
		{
			name:                 "nested python requirements can be ready zero",
			readinessState:       "ready_zero_findings",
			exitCode:             0,
			wantReportSourcePath: "",
		},
		{
			name:                 "nested python pyproject can be ready zero",
			readinessState:       "ready_zero_findings",
			exitCode:             0,
			wantReportSourcePath: "",
		},
		{
			name:                 "nested python pipfile can be ready zero",
			readinessState:       "ready_zero_findings",
			exitCode:             0,
			wantReportSourcePath: "",
		},
		{
			name:                 "nested python poetry project can be ready zero",
			readinessState:       "ready_zero_findings",
			exitCode:             0,
			wantReportSourcePath: "",
		},
		{
			name:           "composer subproject finding stays on composer lockfile",
			readinessState: "ready_with_findings",
			exitCode:       3,
			findings: []map[string]any{
				workspaceScopeFinding("composer", "php/api/composer.lock", ""),
			},
			wantReportSourcePath: "php/api/composer.lock",
		},
		{
			name:           "bundler subproject finding stays on gem lockfile",
			readinessState: "ready_with_findings",
			exitCode:       3,
			findings: []map[string]any{
				workspaceScopeFinding("rubygems", "ruby/api/Gemfile.lock", ""),
			},
			wantReportSourcePath: "ruby/api/Gemfile.lock",
		},
		{
			name:           "image finding attaches to one workload with explicit hop",
			readinessState: "ready_with_findings",
			exitCode:       3,
			findings: []map[string]any{
				workspaceScopeFinding(
					"apk",
					"",
					"sha256:2222222222222222222222222222222222222222222222222222222222222222",
				),
			},
			wantSubjectDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			wantServiceIDs:    []any{"service-synthetic-api"},
			wantEnvironments:  []any{"staging"},
		},
		{
			name:            "ambiguous multi-root evidence stays incomplete",
			readinessState:  "evidence_incomplete",
			exitCode:        4,
			missingEvidence: []any{"ambiguous_multi_root_evidence"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			matrixCase := vulnScanFixtureMatrixCase{
				name:            tc.name,
				fixture:         "monorepo-workspace-scope",
				repositoryID:    "repo-fixture-monorepo-workspace-scope",
				readinessState:  tc.readinessState,
				freshness:       "fresh",
				exitCode:        tc.exitCode,
				findings:        tc.findings,
				missingEvidence: tc.missingEvidence,
				evidenceSources: []map[string]any{
					{"family": "package.consumption", "fact_count": 18, "freshness": "fresh"},
					{"family": "package.registry", "fact_count": 12, "freshness": "fresh"},
					{"family": "vulnerability.advisory", "fact_count": 24, "freshness": "fresh"},
				},
				sourceSnapshots: []map[string]any{
					{"source": "osv", "ecosystem": "npm", "freshness": "fresh", "complete": true},
				},
			}
			if len(tc.missingEvidence) > 0 {
				matrixCase.evidenceSources = []map[string]any{
					{"family": "package.consumption", "fact_count": 18, "freshness": "fresh"},
					{"family": "vulnerability.advisory", "fact_count": 24, "freshness": "fresh"},
				}
				matrixCase.wantMissingEvidence = "ambiguous_multi_root_evidence"
			}

			out, err := runVulnScanFixtureMatrixCase(t, matrixCase, true)
			requireVulnScanExitCode(t, err, tc.exitCode)
			payload := decodeVulnScanPayload(t, out)
			data := payload["data"].(map[string]any)
			if got := data["readiness_state"]; got != tc.readinessState {
				t.Fatalf("data[readiness_state] = %#v, want %#v", got, tc.readinessState)
			}
			report := requireMapField(t, data, "report")
			findings := requireSliceField(t, report, "findings")
			if tc.exitCode == 4 {
				readiness := requireMapField(t, report, "readiness")
				if !sliceContainsString(requireSliceField(t, readiness, "missing_evidence"), "ambiguous_multi_root_evidence") {
					t.Fatalf("report readiness missing_evidence = %#v, want ambiguous_multi_root_evidence", readiness["missing_evidence"])
				}
				if len(findings) != 0 {
					t.Fatalf("report findings = %#v, want none for ambiguous multi-root evidence", findings)
				}
				return
			}
			if tc.exitCode == 0 {
				if len(findings) != 0 {
					t.Fatalf("report findings length = %d, want 0 for ready-zero workspace scope", len(findings))
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("report findings length = %d, want 1", len(findings))
			}
			target := requireMapField(t, findings[0].(map[string]any), "target")
			if tc.wantReportSourcePath != "" {
				if got := target["source_path"]; got != tc.wantReportSourcePath {
					t.Fatalf("report finding target source_path = %#v, want %#v", got, tc.wantReportSourcePath)
				}
				if got := target["service_ids"]; got != nil {
					t.Fatalf("manifest finding service_ids = %#v, want nil without deployment evidence", got)
				}
				if got := target["environments"]; got != nil {
					t.Fatalf("manifest finding environments = %#v, want nil without deployment evidence", got)
				}
			}
			if tc.wantSubjectDigest != "" {
				if got := target["subject_digest"]; got != tc.wantSubjectDigest {
					t.Fatalf("report finding subject_digest = %#v, want %#v", got, tc.wantSubjectDigest)
				}
				if !sameStringSlice(target["service_ids"], tc.wantServiceIDs) {
					t.Fatalf("report finding service_ids = %#v, want %#v", target["service_ids"], tc.wantServiceIDs)
				}
				if !sameStringSlice(target["environments"], tc.wantEnvironments) {
					t.Fatalf("report finding environments = %#v, want %#v", target["environments"], tc.wantEnvironments)
				}
			}
		})
	}
}

func TestVulnScanRepoWorkspaceScopeFixtureHasParserBackedDependencyEvidence(t *testing.T) {
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	registry := parser.DefaultRegistry()
	root := filepath.Join("testdata", "vuln_scan_repo_fixtures", "monorepo-workspace-scope")

	for _, rel := range workspaceScopeFixtureFiles() {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			path := filepath.Join(root, rel)
			if _, ok := registry.LookupByPath(path); !ok {
				t.Fatalf("fixture path %q is not registered with parser registry", rel)
			}
			payload, err := engine.ParsePath(root, path, false, parser.Options{})
			if err != nil {
				t.Fatalf("ParsePath(%s) error = %v, want nil", rel, err)
			}
			for _, row := range parserVariableRows(payload) {
				if row["config_kind"] == "dependency" {
					return
				}
			}
			t.Fatalf("ParsePath(%s) emitted no dependency rows; payload=%#v", rel, payload)
		})
	}
}

func workspaceScopeFinding(ecosystem, sourcePath, subjectDigest string) map[string]any {
	packageName := "synthetic-" + ecosystem + "-workspace-vulnerable"
	finding := map[string]any{
		"finding_id":        "finding-workspace-" + ecosystem,
		"cve_id":            "CVE-2026-SYNTHETIC-WORKSPACE-" + ecosystem,
		"advisory_id":       "GHSA-synthetic-workspace-" + ecosystem,
		"package_id":        ecosystem + ":" + packageName,
		"package_name":      packageName,
		"ecosystem":         ecosystem,
		"impact_status":     "affected_exact",
		"observed_version":  "1.0.0",
		"fixed_version":     "1.0.1",
		"repository_id":     "repo-fixture-monorepo-workspace-scope",
		"dependency_scope":  "runtime",
		"direct_dependency": true,
		"dependency_depth":  1,
		"evidence_fact_ids": []any{"fact-workspace-" + ecosystem + "-package", "fact-workspace-" + ecosystem + "-advisory"},
	}
	if sourcePath != "" {
		finding["manifest_path"] = sourcePath
		finding["start_line"] = 2
		finding["end_line"] = 2
	}
	if subjectDigest != "" {
		finding["subject_digest"] = subjectDigest
		finding["image_ref"] = "registry.synthetic.test/team/api@" + subjectDigest
		finding["runtime_reachability"] = "image_sbom"
		finding["workload_ids"] = []any{"workload-synthetic-api"}
		finding["service_ids"] = []any{"service-synthetic-api"}
		finding["environments"] = []any{"staging"}
	}
	return finding
}

func workspaceScopeFixtureFiles() []string {
	return []string{
		"apps/npm-api/package.json",
		"apps/npm-api/package-lock.json",
		"apps/yarn-worker/package.json",
		"apps/yarn-worker/yarn.lock",
		"apps/pnpm-admin/package.json",
		"apps/pnpm-admin/pnpm-lock.yaml",
		"services/go-api/go.mod",
		"services/go-worker/go.mod",
		"java/api/pom.xml",
		"gradle/service/build.gradle",
		"crates/cargo-api/Cargo.toml",
		"crates/cargo-api/Cargo.lock",
		"dotnet/SyntheticWorker/SyntheticWorker.csproj",
		"dotnet/SyntheticWorker/packages.lock.json",
		"python/requirements/requirements.txt",
		"python/pyproject/pyproject.toml",
		"python/pipfile/Pipfile",
		"python/pipfile/Pipfile.lock",
		"python/poetry/pyproject.toml",
		"python/poetry/poetry.lock",
		"php/api/composer.json",
		"php/api/composer.lock",
		"ruby/api/Gemfile",
		"ruby/api/Gemfile.lock",
	}
}

func sameStringSlice(got any, want []any) bool {
	values, ok := got.([]any)
	if !ok || len(values) != len(want) {
		return false
	}
	for i := range values {
		if values[i] != want[i] {
			return false
		}
	}
	return true
}
