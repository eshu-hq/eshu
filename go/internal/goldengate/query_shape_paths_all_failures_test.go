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
