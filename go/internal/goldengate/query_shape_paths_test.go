// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import "testing"

// TestEvaluateQueryShapeRequiredAbsentWhenPresent proves the four cell
// combinations of the mutual-exclusion primitive in isolation: the assertion
// must fail only when the sibling is present AND the domain is disclosed
// absent in the SAME response; every other combination passes.
func TestEvaluateQueryShapeRequiredAbsentWhenPresent(t *testing.T) {
	t.Parallel()

	shape := QueryShape{
		RequiredResponseFields: []string{"data"},
		RequiredAbsentWhenPresent: []AbsentWhenPresent{
			{
				DomainPath:  "data.evidence_boundaries[].domain",
				DomainValue: "ci_cd_run_correlation",
				SiblingPath: "data.ci_cd_evidence",
			},
		},
	}

	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "sibling present and domain disclosed absent fails",
			body: `{"data":{"ci_cd_evidence":{"state":"materialized"},"evidence_boundaries":[{"domain":"ci_cd_run_correlation"}]}}`,
			want: false,
		},
		{
			name: "sibling present and domain not disclosed passes",
			body: `{"data":{"ci_cd_evidence":{"state":"materialized"}}}`,
			want: true,
		},
		{
			name: "sibling present and boundary lists a different domain passes",
			body: `{"data":{"ci_cd_evidence":{"state":"materialized"},"evidence_boundaries":[{"domain":"container_image_identity"}]}}`,
			want: true,
		},
		{
			name: "sibling absent passes regardless of the domain marker",
			body: `{"data":{"evidence_boundaries":[{"domain":"ci_cd_run_correlation"}]}}`,
			want: true,
		},
		{
			name: "sibling absent and domain absent passes",
			body: `{"data":{}}`,
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			finding := EvaluateQueryShape("evidence-boundary-shape", shape, []byte(c.body))
			if finding.OK != c.want {
				t.Errorf("OK=%v, want %v (detail: %s)", finding.OK, c.want, finding.Detail)
			}
		})
	}
}

// TestEvaluateQueryShapeSeededDisclosureContradiction is the seeded-
// contradiction proof for eshu-hq/eshu#5581: it reproduces the exact #5472
// defect class (a get_service_story-shaped response disclosing
// ci_cd_run_correlation as absent via evidence_boundaries while the sibling
// ci_cd_evidence field in the SAME response actually serves it) at the
// goldengate assertion layer, not just the query-package unit tests that
// caught the original bug. The honest current shape of get_service_story
// (no evidence_boundaries field at all, per evidenceBoundariesFor) is GREEN;
// re-introducing the false boundary turns the SAME shape RED.
func TestEvaluateQueryShapeSeededDisclosureContradiction(t *testing.T) {
	t.Parallel()

	shape := QueryShape{
		RequiredResponseFields: []string{"ci_cd_evidence", "code_to_runtime_trace"},
		RequiredAbsentWhenPresent: []AbsentWhenPresent{
			{
				Description: "ci_cd_run_correlation is served by the top-level ci_cd_evidence field (#5472, #5581)",
				DomainPath:  "evidence_boundaries[].domain",
				DomainValue: "ci_cd_run_correlation",
				SiblingPath: "ci_cd_evidence",
			},
			{
				Description: "container_image_identity is served by code_to_runtime_trace's image_package segment evidence (#5472, #5581)",
				DomainPath:  "evidence_boundaries[].domain",
				DomainValue: "container_image_identity",
				SiblingPath: "code_to_runtime_trace.segments[].evidence[].identity_outcome",
			},
		},
	}

	honest := []byte(`{
		"ci_cd_evidence": {"static_workflow_artifacts": {"state": "materialized"}},
		"code_to_runtime_trace": {
			"segments": [
				{"name": "image_package", "evidence": [{"identity_outcome": "exact_digest"}]}
			]
		}
	}`)
	if finding := EvaluateQueryShape("get_service_story", shape, honest); !finding.OK {
		t.Fatalf("honest get_service_story shape (no evidence_boundaries) must be GREEN: %s", finding.Detail)
	}

	seededCICD := []byte(`{
		"ci_cd_evidence": {"static_workflow_artifacts": {"state": "materialized"}},
		"code_to_runtime_trace": {
			"segments": [
				{"name": "image_package", "evidence": [{"identity_outcome": "exact_digest"}]}
			]
		},
		"evidence_boundaries": [{"domain": "ci_cd_run_correlation", "read_surface": "get_service_story", "reason": "postgres_only_read_model"}]
	}`)
	if finding := EvaluateQueryShape("get_service_story", shape, seededCICD); finding.OK {
		t.Fatalf("seeded ci_cd_run_correlation contradiction must be RED: %s", finding.Detail)
	}

	seededImage := []byte(`{
		"ci_cd_evidence": {"static_workflow_artifacts": {"state": "materialized"}},
		"code_to_runtime_trace": {
			"segments": [
				{"name": "image_package", "evidence": [{"identity_outcome": "exact_digest"}]}
			]
		},
		"evidence_boundaries": [{"domain": "container_image_identity", "read_surface": "get_service_story", "reason": "postgres_only_read_model"}]
	}`)
	if finding := EvaluateQueryShape("get_service_story", shape, seededImage); finding.OK {
		t.Fatalf("seeded container_image_identity contradiction must be RED: %s", finding.Detail)
	}
}
