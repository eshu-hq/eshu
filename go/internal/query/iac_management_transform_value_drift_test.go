// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestAWSRuntimeDriftRowToIaCManagementSurfacesDriftedAttributes proves
// list_aws_runtime_drift_findings (via IaCManagementFindingRow, embedded in
// AWSRuntimeDriftFindingRow) carries the same bounded declared/observed
// value pairs list_cloud_runtime_drift_findings does for an
// image_version_drift finding (#5453 P2-3).
func TestAWSRuntimeDriftRowToIaCManagementSurfacesDriftedAttributes(t *testing.T) {
	t.Parallel()

	row := postgres.AWSCloudRuntimeDriftFindingRow{
		FactID:           "fact:aws-ami-drift",
		ScopeID:          "aws:123456789012:us-east-1:ec2",
		GenerationID:     "generation:aws-1",
		SourceSystem:     "aws",
		ARN:              "arn:aws:ec2:us-east-1:123456789012:instance/i-000000000000000a",
		FindingKind:      "image_version_drift",
		ManagementStatus: "managed_by_terraform",
		Confidence:       1,
		Evidence: []postgres.AWSCloudRuntimeDriftEvidenceRow{
			{ID: "e/declared/ami", EvidenceType: "declared_attribute_value", Key: "declared_ami", Value: "ami-0123456789abcdef0", Confidence: 1},
			{ID: "e/observed/ami", EvidenceType: "observed_attribute_value", Key: "observed_ami", Value: "ami-000000000000000a", Confidence: 1},
		},
	}

	finding := awsRuntimeDriftRowToIaCManagement(row)

	want := []DriftedAttributeView{{Attribute: "ami", Declared: "ami-0123456789abcdef0", Observed: "ami-000000000000000a"}}
	if !reflect.DeepEqual(finding.DriftedAttributes, want) {
		t.Fatalf("finding.DriftedAttributes = %#v, want %#v", finding.DriftedAttributes, want)
	}

	// AWSRuntimeDriftFindingRow embeds IaCManagementFindingRow, so the JSON
	// contract inherits DriftedAttributes without any extra wiring in
	// awsRuntimeDriftFindingRows -- this locks that embedding invariant in.
	wrapped := AWSRuntimeDriftFindingRow{IaCManagementFindingRow: finding}
	if !reflect.DeepEqual(wrapped.DriftedAttributes, want) {
		t.Fatalf("AWSRuntimeDriftFindingRow.DriftedAttributes = %#v, want %#v", wrapped.DriftedAttributes, want)
	}
}

// TestAWSRuntimeDriftRowToIaCManagementOmitsDriftedAttributesForExistenceKinds
// is a regression guard: an orphaned/unmanaged finding carries no
// declared_/observed_ evidence and must project no drifted attributes.
func TestAWSRuntimeDriftRowToIaCManagementOmitsDriftedAttributesForExistenceKinds(t *testing.T) {
	t.Parallel()

	row := postgres.AWSCloudRuntimeDriftFindingRow{
		FactID:           "fact:aws-orphan",
		ScopeID:          "aws:123456789012:us-east-1:ec2",
		ARN:              "arn:aws:ec2:us-east-1:123456789012:instance/i-orphan",
		FindingKind:      findingKindOrphanedCloudResource,
		ManagementStatus: managementStatusCloudOnly,
	}

	finding := awsRuntimeDriftRowToIaCManagement(row)
	if len(finding.DriftedAttributes) != 0 {
		t.Fatalf("finding.DriftedAttributes = %#v, want empty", finding.DriftedAttributes)
	}
}
