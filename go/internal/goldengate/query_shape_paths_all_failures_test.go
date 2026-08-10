// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"strings"
	"testing"
)

// TestEvaluateQueryShapeReportsEveryFailingRequirement is the #5876
// regression, and it reproduces the exact shape that misdirected three
// separate investigations.
//
// evaluateJSONPathRequirements iterated the required value paths in SORTED
// order and returned on the first failure. In the AWS runtime-drift gate the
// two failing paths were `drift_findings[].drifted_attributes[].attribute` and
// `drift_findings[].finding_kind`, and `d` sorts before `f`. So a response
// that had converged to the wrong finding kind -- the actual defect -- was
// only ever reported as "drifted_attributes[] resolved no values", because
// drifted_attributes is derived from the declared/observed atoms that only an
// image_version_drift carries. Issues #5831, #5837, and #5876 each chased the
// empty array; the finding-kind assertion that would have named the cause was
// never evaluated.
//
// Reporting every failing requirement loosens nothing -- any single failure
// still fails the gate. It only stops the alphabetically-first symptom from
// hiding the diagnosis behind it.
func TestEvaluateQueryShapeReportsEveryFailingRequirement(t *testing.T) {
	t.Parallel()

	shape := QueryShape{
		RequiredResponseFields: []string{"drift_findings"},
		RequiredJSONValues: map[string]any{
			"drift_findings[].drifted_attributes[].attribute": "ami",
			"drift_findings[].finding_kind":                   "image_version_drift",
		},
	}
	// A converged-to-orphan response: no drifted_attributes at all (the
	// symptom that sorts first) and the wrong finding_kind (the cause).
	body := []byte(`{"drift_findings":[{"arn":"arn:aws:ec2:us-east-1:123456789012:instance/i-1","finding_kind":"orphaned_cloud_resource"}]}`)

	finding := EvaluateQueryShape("aws-runtime-drift-all-failures", shape, body)
	if finding.OK {
		t.Fatal("EvaluateQueryShape() OK = true, want a failure: neither required value is satisfied")
	}
	if !strings.Contains(finding.Detail, "drifted_attributes[]") {
		t.Fatalf("detail = %q, want it to name the drifted_attributes failure", finding.Detail)
	}
	if !strings.Contains(finding.Detail, "finding_kind") {
		t.Fatalf(
			"detail = %q, want it to ALSO name the finding_kind failure: reporting only the "+
				"alphabetically-first failing path is what hid the real defect behind the symptom (#5876)",
			finding.Detail,
		)
	}
	if !strings.Contains(finding.Detail, "orphaned_cloud_resource") {
		t.Fatalf("detail = %q, want the observed finding_kind so the cause is readable from the gate line", finding.Detail)
	}
}

// TestEvaluateQueryShapeReportsFailuresFromEveryRequirementFamily closes the
// coverage gap the test above leaves.
//
// That test only exercises RequiredJSONValues, but the early return was
// replaced in all four families -- RequiredJSONPaths, RequiredJSONValues,
// RequiredJSONObjectMatches and RequiredAbsentWhenPresent. Restoring an early
// return in any of the other three would hide every later diagnostic again
// while the suite stayed green, because the existing tests for those families
// only assert the overall pass/fail verdict and never look at the detail.
//
// One shape fails all four families at once, and covers BOTH failure branches
// of the two families that have two (a path that cannot resolve versus one
// that resolves to an empty value; an object-match path that cannot resolve
// versus one that resolves but contains no match). That is six of the seven
// append sites. The seventh -- the RequiredJSONValues resolve-error branch --
// is covered by TestEvaluateQueryShapeReportsEveryFailingRequirement above,
// so between the two tests every site dies when reverted to an early return.
func TestEvaluateQueryShapeReportsFailuresFromEveryRequirementFamily(t *testing.T) {
	t.Parallel()

	shape := QueryShape{
		RequiredResponseFields: []string{"data"},
		// Two entries, because RequiredJSONPaths has TWO failure branches and
		// a mutation check showed one test input only reaches one of them:
		// absent_field cannot resolve at all (the resolve-error branch), while
		// blank_field resolves to an empty value (the no-non-empty-values
		// branch). Guarding only one leaves the other free to reinstate an
		// early return unnoticed.
		RequiredJSONPaths: []string{"data.absent_field", "data.blank_field"},
		RequiredJSONValues: map[string]any{
			"data.kind": "expected_kind",
		},
		// Two entries here for the same reason RequiredJSONPaths has two:
		// this family also has two failure branches. data.items[] resolves
		// and contains no matching object; data.absent_items[] cannot
		// resolve at all. Sorted order puts absent_items first, so an early
		// return in the resolve-error branch truncates before items[] and
		// this test dies.
		RequiredJSONObjectMatches: map[string][]map[string]any{
			"data.items[]":        {{"name": "absent_item"}},
			"data.absent_items[]": {{"name": "unreachable"}},
		},
		RequiredAbsentWhenPresent: []AbsentWhenPresent{
			{
				DomainPath:  "data.evidence_boundaries[].domain",
				DomainValue: "ci_cd_run_correlation",
				SiblingPath: "data.ci_cd_evidence",
			},
		},
	}
	// Fails every family at once: no absent_field, kind is wrong, no item
	// named absent_item, and ci_cd_run_correlation is disclosed absent while
	// the ci_cd_evidence sibling is served in the same response.
	body := []byte(`{"data":{
		"blank_field":"",
		"kind":"wrong_kind",
		"items":[{"name":"present_item"}],
		"ci_cd_evidence":{"state":"materialized"},
		"evidence_boundaries":[{"domain":"ci_cd_run_correlation"}]
	}}`)

	finding := EvaluateQueryShape("every-family-fails", shape, body)
	if finding.OK {
		t.Fatal("EvaluateQueryShape() OK = true, want a failure: all four requirement families are unsatisfied")
	}
	for _, want := range []struct {
		family   string
		fragment string
	}{
		{family: "RequiredJSONPaths (resolve error)", fragment: "data.absent_field"},
		{family: "RequiredJSONPaths (resolved but empty)", fragment: "data.blank_field"},
		{family: "RequiredJSONValues", fragment: "data.kind"},
		{family: "RequiredJSONObjectMatches (resolve error)", fragment: "data.absent_items[]"},
		{family: "RequiredJSONObjectMatches (no matching object)", fragment: "data.items[]"},
		{family: "RequiredAbsentWhenPresent", fragment: "disclosure-vs-served contradiction"},
	} {
		if !strings.Contains(finding.Detail, want.fragment) {
			t.Fatalf(
				"detail = %q, want it to name the %s failure (fragment %q): an early return reinstated in that "+
					"branch would truncate the detail and hide every family after it (#5876)",
				finding.Detail, want.family, want.fragment,
			)
		}
	}
}

// TestEvaluateQueryShapeSingleFailureDetailStaysReadable guards the other
// direction: collecting every failure must not turn the common one-failure
// case into a list with separator noise. A single failing requirement reads
// exactly as it did before.
func TestEvaluateQueryShapeSingleFailureDetailStaysReadable(t *testing.T) {
	t.Parallel()

	shape := QueryShape{
		RequiredResponseFields: []string{"result_limits"},
		RequiredJSONValues: map[string]any{
			"result_limits.relationship_count": 1,
		},
	}
	finding := EvaluateQueryShape(
		"single-failure-detail",
		shape,
		[]byte(`{"result_limits":{"relationship_count":2}}`),
	)
	if finding.OK {
		t.Fatal("EvaluateQueryShape() OK = true, want a failure")
	}
	if strings.Contains(finding.Detail, jsonRequirementFailureSeparator) {
		t.Fatalf("detail = %q, want no list separator when exactly one requirement failed", finding.Detail)
	}
	if !strings.Contains(finding.Detail, "observed [2]") {
		t.Fatalf("detail = %q, want the observed value", finding.Detail)
	}
}
