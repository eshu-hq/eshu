// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"strings"
	"testing"
)

// TestEvaluateQueryShapeResultCountLogging is the #6785 gate-logging proof.
// EvaluateQueryShape's success detail must always report the REAL length of
// the asserted (or, absent an assertion, every top-level array-valued)
// response field, never a misleading placeholder. Before the fix:
//
//   - a shape with results_field set but no minimum_results, maximum_results,
//     or result_item_required_fields bound never unmarshaled the array (items
//     stayed nil), so the success detail printed "<field> has 0 results"
//     regardless of the real count.
//   - a shape with no results_field printed no count at all, even when the
//     response body carried a populated array-valued field an operator would
//     want to see calibrating a zero floor from a live run.
func TestEvaluateQueryShapeResultCountLogging(t *testing.T) {
	t.Parallel()

	t.Run("results_field without bounds reports the real count, not 0", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"cloud_resources": [{"id":"a"},{"id":"b"},{"id":"c"}]}`)
		shape := QueryShape{
			RequiredResponseFields: []string{"cloud_resources"},
			ResultsField:           "cloud_resources",
			// No MinimumResults, MaximumResults, or ResultItemRequiredFields:
			// needsArrayResult is false, so the pre-fix code never unmarshaled
			// the array for counting.
		}
		f := EvaluateQueryShape("http:list_cloud_resources", shape, body)
		if !f.OK {
			t.Fatalf("shape with no bounds must pass regardless of count: %s", f.Detail)
		}
		if strings.Contains(f.Detail, `"cloud_resources" has 0 results`) {
			t.Fatalf("detail = %q, misleadingly reports 0 results for a 3-element array", f.Detail)
		}
		if !strings.Contains(f.Detail, `"cloud_resources" has 3 results`) {
			t.Errorf("detail = %q, want it to report the real 3-element count", f.Detail)
		}
	})

	t.Run("results_field without bounds naming a non-array field reports non-array, not a count", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"summary": {"total": 3}}`)
		shape := QueryShape{
			RequiredResponseFields: []string{"summary"},
			ResultsField:           "summary",
		}
		f := EvaluateQueryShape("http:get_summary", shape, body)
		if !f.OK {
			t.Fatalf("shape with no bounds must pass regardless of shape: %s", f.Detail)
		}
		if !strings.Contains(f.Detail, "non-array") {
			t.Errorf("detail = %q, want it to flag the results_field as non-array rather than claim a count", f.Detail)
		}
	})

	t.Run("no results_field still reports every top-level array field's real length", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"cloud_resources": [{"id":"a"},{"id":"b"}], "warnings": []}`)
		shape := QueryShape{
			RequiredResponseFields: []string{"cloud_resources"},
		}
		f := EvaluateQueryShape("mcp:analyze_infra_relationships", shape, body)
		if !f.OK {
			t.Fatalf("shape asserting only field presence must pass: %s", f.Detail)
		}
		if !strings.Contains(f.Detail, "array_counts") {
			t.Errorf("detail = %q, want it to carry an array_counts summary when results_field is unset", f.Detail)
		}
		if !strings.Contains(f.Detail, "cloud_resources:2") {
			t.Errorf("detail = %q, want array_counts to report the real 2-element cloud_resources count", f.Detail)
		}
	})
	// #6785 review F4: json.Unmarshal of `null` into a slice succeeds with a
	// zero length, so a null field used to print as "0 results" / "error:0".
	// A null value is absent, not an empty array.
	t.Run("null top-level field is absent from array_counts", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"data": [{"id":"a"}], "error": null}`)
		f := EvaluateQueryShape("mcp:envelope", QueryShape{RequiredResponseFields: []string{"data", "error"}}, body)
		if !f.OK {
			t.Fatalf("presence-only shape must pass: %s", f.Detail)
		}
		if strings.Contains(f.Detail, "error:") {
			t.Fatalf("detail = %q, reports a count for a null field", f.Detail)
		}
		if !strings.Contains(f.Detail, "data:1") {
			t.Errorf("detail = %q, want the real data:1 count", f.Detail)
		}
	})

	t.Run("null results_field without bounds is reported as null, not 0 results", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"findings": null}`)
		shape := QueryShape{RequiredResponseFields: []string{"findings"}, ResultsField: "findings"}
		f := EvaluateQueryShape("http:findings", shape, body)
		if !f.OK {
			t.Fatalf("shape with no bounds must pass: %s", f.Detail)
		}
		if strings.Contains(f.Detail, "has 0 results") {
			t.Fatalf("detail = %q, reports 0 results for a null field", f.Detail)
		}
		if !strings.Contains(f.Detail, "null") {
			t.Errorf("detail = %q, want it to say the field is null", f.Detail)
		}
	})
}
