// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

func TestGoldenSnapshotAskRequiresEnabledEvidenceBackedToolLoop(t *testing.T) {
	t.Parallel()
	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	shape := snapshot.QueryShapes.MCP["ask"]
	if shape.ExpectedErrorContains != "" {
		t.Fatalf("Ask shape still accepts default-off refusal: %q", shape.ExpectedErrorContains)
	}
	if shape.MinimumResults < 1 || shape.ResultsField != "evidence_handles" {
		t.Fatalf("Ask evidence bound = minimum %d field %q, want positive evidence_handles", shape.MinimumResults, shape.ResultsField)
	}

	positive := []byte(`{
      "answer_prose":"Found one evidence group.",
      "truth_class":"code_hint",
      "result_ref":"eshu://api-result/code/topics/investigate",
      "evidence_handles":[{"kind":"source","repo_id":"repository:r_ea78e8bb","relative_path":"main.go"}],
      "query_trace":[{"tool":"investigate_code_topic","args":{"limit":10,"repo_id":"orders-api","topic":"lib-common"},"supported":true,"truth_class":"code_hint"}],
      "partial":false
    }`)
	if finding := EvaluateQueryShape("ask-enabled", shape, positive); !finding.OK {
		t.Fatalf("enabled evidence-backed Ask response failed: %+v", finding)
	}
	for name, raw := range map[string][]byte{
		"default-off":    []byte(`{"state":"unavailable","reason":"ask is not enabled"}`),
		"empty-evidence": []byte(`{"answer_prose":"Found one evidence group.","truth_class":"code_hint","result_ref":"eshu://api-result/code/topics/investigate","evidence_handles":[],"query_trace":[{"tool":"investigate_code_topic","args":{"limit":10,"repo_id":"orders-api","topic":"lib-common"},"supported":true}],"partial":false}`),
		"wrong-tool":     []byte(`{"answer_prose":"Found one evidence group.","truth_class":"code_hint","result_ref":"eshu://api-result/code/topics/investigate","evidence_handles":[{"kind":"source","repo_id":"repository:r_ea78e8bb","relative_path":"main.go"}],"query_trace":[{"tool":"find_code","supported":true}],"partial":false}`),
	} {
		if finding := EvaluateQueryShape("ask-"+name, shape, raw); finding.OK {
			t.Errorf("seeded %s Ask response passed: %+v", name, finding)
		}
	}
}
