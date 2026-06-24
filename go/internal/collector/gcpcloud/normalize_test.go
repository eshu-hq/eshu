// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"reflect"
	"testing"
)

func TestNormalizeAssetTypeFamily(t *testing.T) {
	cases := map[string]string{
		"compute.googleapis.com/Instance":   "compute",
		"storage.googleapis.com/Bucket":     "storage",
		"iam.googleapis.com/ServiceAccount": "iam",
		"":                                  "unknown",
		"malformed":                         "unknown",
	}
	for assetType, want := range cases {
		if got := AssetTypeFamily(assetType); got != want {
			t.Fatalf("AssetTypeFamily(%q) = %q, want %q", assetType, got, want)
		}
	}
}

func TestNormalizeLocationBucket(t *testing.T) {
	cases := map[string]string{
		"us-central1":   "us-central1",
		"US-CENTRAL1-A": "us-central1-a",
		"":              "global",
		"  ":            "global",
	}
	for location, want := range cases {
		if got := LocationBucket(location); got != want {
			t.Fatalf("LocationBucket(%q) = %q, want %q", location, got, want)
		}
	}
}

func TestNormalizeAncestry(t *testing.T) {
	ancestors := []string{"projects/123456789", "folders/4455667788", "organizations/9988776655"}
	got := NormalizeAncestry(ancestors)
	want := Ancestry{
		ProjectNumber:      "123456789",
		FolderNumbers:      []string{"4455667788"},
		OrganizationNumber: "9988776655",
		Chain:              ancestors,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeAncestry = %#v, want %#v", got, want)
	}
}

func TestNormalizeAncestryMultiFolder(t *testing.T) {
	ancestors := []string{
		"projects/123",
		"folders/inner",
		"folders/outer",
		"organizations/777",
	}
	got := NormalizeAncestry(ancestors)
	if !reflect.DeepEqual(got.FolderNumbers, []string{"inner", "outer"}) {
		t.Fatalf("FolderNumbers = %#v, want [inner outer]", got.FolderNumbers)
	}
	if got.ProjectNumber != "123" || got.OrganizationNumber != "777" {
		t.Fatalf("project/org = %q/%q, want 123/777", got.ProjectNumber, got.OrganizationNumber)
	}
}

func TestProjectIDFromFullName(t *testing.T) {
	cases := map[string]string{
		"//compute.googleapis.com/projects/my-project/zones/us-central1-a/instances/vm-1": "my-project",
		"//storage.googleapis.com/projects/_/buckets/my-bucket":                           "_",
		"//cloudresourcemanager.googleapis.com/projects/host-proj":                        "host-proj",
		"//example.com/locations/global/things/x":                                         "",
	}
	for name, want := range cases {
		if got := ProjectIDFromFullName(name); got != want {
			t.Fatalf("ProjectIDFromFullName(%q) = %q, want %q", name, got, want)
		}
	}
}
