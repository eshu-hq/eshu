// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommittedCICDShapesRejectCappedFalseAbsence proves that the committed
// B-12 query shapes reject contradictory coverage claims on each repository
// filtered CI/CD response. The response is synthetic; the shape is loaded from
// the same snapshot used by the live golden gate.
func TestCommittedCICDShapesRejectCappedFalseAbsence(t *testing.T) {
	snapshot, err := LoadSnapshot(filepath.Join("..", "..", "..", "testdata", "golden", "e2e-20repo-snapshot.json"))
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}

	shapes := map[string]QueryShape{
		"mcp:list_ci_cd_run_correlations": snapshot.QueryShapes.MCP["list_ci_cd_run_correlations"],
	}
	for name, shape := range snapshot.QueryShapes.HTTP {
		if strings.HasPrefix(name, "GET /api/v0/ci-cd/run-correlations?") && strings.Contains(name, "repository_id=") {
			shapes["http:"+name] = shape
		}
	}
	if len(shapes) != 5 {
		t.Fatalf("repository filtered CI/CD shapes = %d, want 5", len(shapes))
	}

	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				static map[string]any
				want   string
			}{
				{
					name: "false static absence",
					static: map[string]any{
						"state": "absent", "candidate_pool_status": "unknown_at_limit",
					},
					want: `domain "absent"`,
				},
				{
					name: "false summary absence",
					static: map[string]any{
						"state": "unknown", "candidate_pool_status": "unknown_at_limit",
					},
					want: `domain "no_ci_cd_evidence_found"`,
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					row := map[string]any{}
					for _, field := range shape.ResultItemRequiredFields {
						row[field] = "fixture"
					}
					for _, matches := range shape.RequiredJSONObjectMatches {
						for key, value := range matches[0] {
							row[key] = value
						}
					}
					response := map[string]any{
						"collector_readiness": map[string]any{},
						"correlations":        []map[string]any{row},
						"count":               1,
						"evidence_summary": map[string]any{
							"static_workflow_artifacts": tc.static,
							"reason":                    "no_ci_cd_evidence_found",
						},
						"limit":     10,
						"truncated": false,
					}
					body, err := json.Marshal(response)
					if err != nil {
						t.Fatalf("marshal response: %v", err)
					}
					finding := EvaluateQueryShape(name, shape, body)
					if finding.OK || !strings.Contains(finding.Detail, tc.want) {
						t.Fatalf("contradiction accepted or missed: %+v", finding)
					}
				})
			}
		})
	}
}

func TestCommittedCICDMCPShapeAllowsHonestCoverage(t *testing.T) {
	snapshot, err := LoadSnapshot(filepath.Join("..", "..", "..", "testdata", "golden", "e2e-20repo-snapshot.json"))
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	shape := snapshot.QueryShapes.MCP["list_ci_cd_run_correlations"]
	for _, body := range []string{
		`{"collector_readiness":{},"correlations":[{}],"count":1,"evidence_summary":{"static_workflow_artifacts":{"state":"unknown","candidate_pool_status":"unknown_at_limit"},"reason":"static_workflow_coverage_unknown"},"limit":10,"truncated":false}`,
		`{"collector_readiness":{},"correlations":[{}],"count":1,"evidence_summary":{"static_workflow_artifacts":{"state":"absent"},"reason":"no_ci_cd_evidence_found"},"limit":10,"truncated":false}`,
	} {
		if finding := EvaluateQueryShape("mcp:list_ci_cd_run_correlations", shape, []byte(body)); !finding.OK {
			t.Errorf("honest coverage rejected: %+v", finding)
		}
	}
}
