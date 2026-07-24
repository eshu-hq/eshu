// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestDriftedAttributesFromAWSEvidencePairsDeclaredAndObserved mirrors
// TestDriftedAttributesFromEvidencePairsDeclaredAndObserved
// (cloud_runtime_drift_value_attributes_test.go) for the AWS-specific
// evidence row type, proving list_aws_runtime_drift_findings gets the same
// drifted_attributes projection as list_cloud_runtime_drift_findings
// (#5453 P2-3).
func TestDriftedAttributesFromAWSEvidencePairsDeclaredAndObserved(t *testing.T) {
	t.Parallel()

	evidence := []postgres.AWSCloudRuntimeDriftEvidenceRow{
		{Key: "arn", Value: "arn:aws:ec2:us-east-1:123456789012:instance/i-1"},
		{Key: "declared_ami", Value: "ami-old"},
		{Key: "observed_ami", Value: "ami-new"},
		{Key: "finding_kind", Value: "image_version_drift"},
	}

	got := driftedAttributesFromAWSEvidence(evidence)
	want := []DriftedAttributeView{{Attribute: "ami", Declared: "ami-old", Observed: "ami-new"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("driftedAttributesFromAWSEvidence() = %#v, want %#v", got, want)
	}
}

// TestDriftedAttributesFromAWSEvidenceEmptyWhenNoValuePairs proves an
// existence-kind finding (no declared_/observed_ atoms) yields nil.
func TestDriftedAttributesFromAWSEvidenceEmptyWhenNoValuePairs(t *testing.T) {
	t.Parallel()

	evidence := []postgres.AWSCloudRuntimeDriftEvidenceRow{
		{Key: "arn", Value: "arn:aws:ec2:us-east-1:123456789012:instance/i-1"},
		{Key: "finding_kind", Value: "orphaned_cloud_resource"},
	}
	if got := driftedAttributesFromAWSEvidence(evidence); got != nil {
		t.Fatalf("driftedAttributesFromAWSEvidence() = %#v, want nil", got)
	}
}
