// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"strings"
	"testing"
)

// TestEvaluateQueryShapeResultsFieldDeterministic is the eshu-hq/eshu#5566
// seeded-defect proof. The fixture body carries TWO array-valued fields: a
// request-echo array ("requested_kinds", non-empty because the caller passed
// filter values back) and the real result collection ("drift_findings"),
// which has regressed to empty -- for example a reducer bug dropped every
// finding for this scope. minimum_results:1 exists to catch exactly that
// regression.
//
// The pre-#5566 EvaluateQueryShape picked the first array-valued field in
// required_response_fields ORDER: with the echo field listed first it counted
// "requested_kinds" (2 items) and PASSED even though the real collection was
// empty; only when the real field happened to be listed first did it
// correctly fail. Reordering the identical semantic defect flipped the gate
// between pass and fail -- see the branch's PR body for the literal old-code
// reproduction transcript (a standalone run of a byte-for-byte copy of the
// prior implementation; that heuristic is no longer reachable from this
// package).
//
// With ResultsField explicit, the same fixture correctly fails regardless of
// required_response_fields order, and an unset/misnamed/non-array
// ResultsField fails loudly instead of guessing.
func TestEvaluateQueryShapeResultsFieldDeterministic(t *testing.T) {
	t.Parallel()

	body := []byte(`{"requested_kinds": ["network","iam"], "drift_findings": []}`)

	echoFirst := QueryShape{
		RequiredResponseFields: []string{"requested_kinds", "drift_findings"},
		MinimumResults:         1,
		ResultsField:           "drift_findings",
	}
	if f := EvaluateQueryShape("mcp:list_terraform_config_state_drift_findings", echoFirst, body); f.OK {
		t.Fatalf("empty drift_findings must fail even with the echo field listed first: %s", f.Detail)
	}

	realFirst := QueryShape{
		RequiredResponseFields: []string{"drift_findings", "requested_kinds"},
		MinimumResults:         1,
		ResultsField:           "drift_findings",
	}
	if f := EvaluateQueryShape("mcp:list_terraform_config_state_drift_findings", realFirst, body); f.OK {
		t.Fatalf("empty drift_findings must fail with the real field listed first too: %s", f.Detail)
	}

	t.Run("results_field unset fails loudly, does not silently pick a field", func(t *testing.T) {
		shape := QueryShape{
			RequiredResponseFields: []string{"requested_kinds", "drift_findings"},
			MinimumResults:         1,
		}
		f := EvaluateQueryShape("mcp:list_terraform_config_state_drift_findings", shape, body)
		if f.OK {
			t.Fatal("an array-result assertion with no results_field must never pass")
		}
		if !strings.Contains(f.Detail, "results_field is required") {
			t.Errorf("detail = %q, want it to name the missing results_field", f.Detail)
		}
	})

	t.Run("results_field not listed in required_response_fields fails loudly", func(t *testing.T) {
		shape := QueryShape{
			RequiredResponseFields: []string{"requested_kinds", "drift_findings"},
			MinimumResults:         1,
			ResultsField:           "drift_kinds", // not in required_response_fields
		}
		f := EvaluateQueryShape("mcp:list_terraform_config_state_drift_findings", shape, body)
		if f.OK {
			t.Fatal("a results_field absent from required_response_fields must never pass")
		}
		if !strings.Contains(f.Detail, `results_field "drift_kinds" is not listed`) {
			t.Errorf("detail = %q, want it to name the unlisted results_field", f.Detail)
		}
	})

	t.Run("results_field naming a non-array field fails loudly", func(t *testing.T) {
		shape := QueryShape{
			RequiredResponseFields: []string{"requested_kinds", "drift_findings"},
			MinimumResults:         1,
			ResultsField:           "requested_kinds",
		}
		// requested_kinds happens to satisfy the floor by coincidence in the
		// shared fixture; use a body where it is an object to prove the type
		// check, not just the count.
		objectBody := []byte(`{"requested_kinds": {"not": "an array"}, "drift_findings": []}`)
		f := EvaluateQueryShape("mcp:list_terraform_config_state_drift_findings", shape, objectBody)
		if f.OK {
			t.Fatal("a non-array results_field must never pass")
		}
		if !strings.Contains(f.Detail, "is not an array-valued field") {
			t.Errorf("detail = %q, want it to name the non-array results_field", f.Detail)
		}
	})

	t.Run("real regression proven live once results_field is correct", func(t *testing.T) {
		populated := []byte(`{"requested_kinds": [], "drift_findings": [{"address":"aws_instance.a"}]}`)
		shape := QueryShape{
			RequiredResponseFields: []string{"requested_kinds", "drift_findings"},
			MinimumResults:         1,
			ResultsField:           "drift_findings",
		}
		f := EvaluateQueryShape("mcp:list_terraform_config_state_drift_findings", shape, populated)
		if !f.OK {
			t.Fatalf("a populated drift_findings must pass: %s", f.Detail)
		}
		if !strings.Contains(f.Detail, `"drift_findings" has 1 results`) {
			t.Errorf("detail = %q, want it to name drift_findings as the asserted field", f.Detail)
		}
	})
}
