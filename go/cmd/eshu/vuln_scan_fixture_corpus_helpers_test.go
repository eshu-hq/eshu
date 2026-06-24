// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

func vulnScanFixtureCorpusMatrixCase(tc vulnScanFixtureCorpusCase) vulnScanFixtureMatrixCase {
	repoID := "repo-fixture-" + tc.fixture
	matrix := vulnScanFixtureMatrixCase{
		name:         tc.name,
		fixture:      tc.fixture,
		repositoryID: repoID,
		freshness:    "fresh",
		evidenceSources: []map[string]any{
			{"family": "package.consumption", "fact_count": 1, "freshness": "fresh"},
			{"family": "package.registry", "fact_count": 1, "freshness": "fresh"},
			{"family": "vulnerability.advisory", "fact_count": 4, "freshness": "fresh"},
		},
		sourceSnapshots: []map[string]any{
			{"source": "osv", "ecosystem": tc.ecosystem, "freshness": "fresh", "complete": true},
		},
	}
	switch tc.state {
	case "vulnerable":
		matrix.readinessState = "ready_with_findings"
		matrix.exitCode = 3
		matrix.findings = []map[string]any{syntheticFixtureFinding(tc, repoID)}
	case "ready_zero":
		matrix.readinessState = "ready_zero_findings"
		matrix.exitCode = 0
	case "malformed":
		matrix.readinessState = "evidence_incomplete"
		matrix.exitCode = 4
		matrix.missingEvidence = []any{"malformed_lockfile"}
		matrix.wantMissingEvidence = "malformed_lockfile"
	case "unsupported":
		matrix.readinessState = "unsupported"
		matrix.exitCode = 5
		matrix.missingEvidence = []any{"unsupported_targets"}
		matrix.unsupportedTargets = []map[string]any{
			{"target_kind": "ecosystem", "reason": "matcher_not_available", "ecosystem": tc.ecosystem, "count": 1},
		}
		matrix.wantMissingEvidence = "unsupported_targets"
	case "missing_evidence":
		matrix.readinessState = "evidence_incomplete"
		matrix.exitCode = 4
		matrix.evidenceSources = []map[string]any{
			{"family": "package.consumption", "fact_count": 1, "freshness": "fresh"},
			{"family": "package.registry", "fact_count": 1, "freshness": "fresh"},
		}
		matrix.missingEvidence = []any{"advisory_sources"}
		matrix.wantMissingEvidence = "advisory_sources"
	default:
		matrix.readinessState = "readiness_unavailable"
		matrix.exitCode = 4
	}
	return matrix
}

func syntheticFixtureFinding(tc vulnScanFixtureCorpusCase, repoID string) map[string]any {
	packageName := "synthetic-" + tc.manager + "-vulnerable"
	finding := map[string]any{
		"finding_id":        "finding-" + tc.fixture,
		"cve_id":            "CVE-2026-SYNTHETIC-" + tc.manager,
		"advisory_id":       "GHSA-synthetic-" + tc.manager + "-0001",
		"package_id":        tc.ecosystem + ":" + packageName,
		"package_name":      packageName,
		"ecosystem":         tc.ecosystem,
		"package_manager":   tc.manager,
		"impact_status":     "affected_exact",
		"observed_version":  "1.0.0",
		"fixed_version":     "1.0.1",
		"repository_id":     repoID,
		"dependency_scope":  "runtime",
		"direct_dependency": tc.direct,
		"dependency_depth":  1,
		"evidence_fact_ids": []any{"fact-" + tc.fixture + "-package", "fact-" + tc.fixture + "-advisory"},
	}
	if tc.dev {
		finding["dependency_scope"] = "development"
	}
	if len(tc.dependencyPath) > 0 {
		finding["dependency_path"] = tc.dependencyPath
		finding["dependency_depth"] = len(tc.dependencyPath) - 1
	}
	return finding
}

func expectedFixtureCorpusReadinessState(tc vulnScanFixtureMatrixCase) string {
	if tc.wantMissingEvidence == "package_registry_metadata" ||
		tc.wantMissingEvidence == "advisory_cache_stale" {
		return "evidence_incomplete"
	}
	return tc.readinessState
}
