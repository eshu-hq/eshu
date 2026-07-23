// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestCloudRuntimeDriftFindingViewsSurfacesDriftedAttributes proves the
// bounded declared_/observed_ value pairs a reducer row carries for an
// image_version_drift finding project onto the view's drifted_attributes
// field (#5453).
func TestCloudRuntimeDriftFindingViewsSurfacesDriftedAttributes(t *testing.T) {
	t.Parallel()

	rows := []MultiCloudRuntimeDriftFindingRow{{
		FactID:            "fact:ami-drift",
		ScopeID:           "aws:123456789012:us-east-1:ec2",
		Provider:          "aws",
		CloudResourceUID:  "aws:arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
		FindingKind:       "image_version_drift",
		ManagementStatus:  "managed_by_terraform",
		DriftedAttributes: []DriftedAttributeView{{Attribute: "ami", Declared: "ami-0123456789abcdef0", Observed: "ami-000000000000000a"}},
	}}

	views := cloudRuntimeDriftFindingViews(rows)
	if len(views) != 1 {
		t.Fatalf("len(views) = %d, want 1", len(views))
	}
	want := []DriftedAttributeView{{Attribute: "ami", Declared: "ami-0123456789abcdef0", Observed: "ami-000000000000000a"}}
	if !reflect.DeepEqual(views[0].DriftedAttributes, want) {
		t.Fatalf("views[0].DriftedAttributes = %#v, want %#v", views[0].DriftedAttributes, want)
	}
}

// TestCloudRuntimeDriftFindingViewsOmitsDriftedAttributesWhenAbsent proves an
// existence-kind finding (no value-drift evidence) renders no
// drifted_attributes.
func TestCloudRuntimeDriftFindingViewsOmitsDriftedAttributesWhenAbsent(t *testing.T) {
	t.Parallel()

	rows := []MultiCloudRuntimeDriftFindingRow{{
		FactID:      "fact:orphan",
		ScopeID:     "aws:123456789012:us-east-1:ec2",
		Provider:    "aws",
		FindingKind: "orphaned_cloud_resource",
	}}

	views := cloudRuntimeDriftFindingViews(rows)
	if len(views) != 1 {
		t.Fatalf("len(views) = %d, want 1", len(views))
	}
	if len(views[0].DriftedAttributes) != 0 {
		t.Fatalf("views[0].DriftedAttributes = %#v, want empty", views[0].DriftedAttributes)
	}
}

// TestDriftedAttributesFromEvidencePairsDeclaredAndObserved proves the
// postgres-adapter helper pairs declared_<attr>/observed_<attr> evidence
// keys deterministically, ignoring unrelated evidence keys.
func TestDriftedAttributesFromEvidencePairsDeclaredAndObserved(t *testing.T) {
	t.Parallel()

	evidence := []postgres.MultiCloudRuntimeDriftEvidenceRow{
		{Key: "arn", Value: "arn:aws:ec2:us-east-1:123456789012:instance/i-1"},
		{Key: "declared_ami", Value: "ami-old"},
		{Key: "observed_ami", Value: "ami-new"},
		{Key: "declared_version", Value: "1"},
		{Key: "observed_version", Value: "2"},
		{Key: "finding_kind", Value: "image_version_drift"},
	}

	got := driftedAttributesFromEvidence(evidence)
	want := []DriftedAttributeView{
		{Attribute: "ami", Declared: "ami-old", Observed: "ami-new"},
		{Attribute: "version", Declared: "1", Observed: "2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("driftedAttributesFromEvidence() = %#v, want %#v", got, want)
	}
}
