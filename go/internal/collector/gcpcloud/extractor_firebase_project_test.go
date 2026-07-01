// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"testing"
)

// fixtureFirebaseProjectData builds a representative CAI
// firebase.googleapis.com/FirebaseProject resource.data blob.
func fixtureFirebaseProjectData(t *testing.T) []byte {
	t.Helper()
	blob := map[string]any{
		"name":          "projects/demo-project",
		"projectId":     "demo-project",
		"projectNumber": "123456789",
		"displayName":   "Demo Firebase App",
		"state":         "ACTIVE",
		"resources": map[string]any{
			"hostingSite":              "demo-project",
			"realtimeDatabaseInstance": "demo-project-default-rtdb",
			"storageBucket":            "demo-project.appspot.com",
			"locationId":               "us-central",
		},
	}
	raw, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("marshal firebase project fixture: %v", err)
	}
	return raw
}

func firebaseProjectCtx(t *testing.T) ExtractContext {
	t.Helper()
	return ExtractContext{
		FullResourceName: "//firebase.googleapis.com/projects/demo-project",
		AssetType:        firebaseProjectAssetType,
		Data:             fixtureFirebaseProjectData(t),
	}
}

// TestExtractFirebaseProjectAttributes proves the bounded attribute set is
// surfaced from resource.data.
func TestExtractFirebaseProjectAttributes(t *testing.T) {
	got, err := extractFirebaseProject(firebaseProjectCtx(t))
	if err != nil {
		t.Fatalf("extractFirebaseProject: %v", err)
	}
	if got.Attributes["state"] != "ACTIVE" {
		t.Errorf("state = %v, want ACTIVE", got.Attributes["state"])
	}
	if got.Attributes["display_name"] != "Demo Firebase App" {
		t.Errorf("display_name = %v, want Demo Firebase App", got.Attributes["display_name"])
	}
	if got.Attributes["location_id"] != "us-central" {
		t.Errorf("location_id = %v, want us-central", got.Attributes["location_id"])
	}
	if got.Attributes["hosting_site_present"] != true {
		t.Errorf("hosting_site_present = %v, want true", got.Attributes["hosting_site_present"])
	}
	if got.Attributes["realtime_database_present"] != true {
		t.Errorf("realtime_database_present = %v, want true", got.Attributes["realtime_database_present"])
	}
	if got.Attributes["default_storage_bucket_present"] != true {
		t.Errorf("default_storage_bucket_present = %v, want true", got.Attributes["default_storage_bucket_present"])
	}
}

// TestExtractFirebaseProjectEdges proves both typed edges resolve to canonical
// CAI full resource names.
func TestExtractFirebaseProjectEdges(t *testing.T) {
	got, err := extractFirebaseProject(firebaseProjectCtx(t))
	if err != nil {
		t.Fatalf("extractFirebaseProject: %v", err)
	}
	var projectEdge, bucketEdge *RelationshipObservation
	for i := range got.Relationships {
		switch got.Relationships[i].RelationshipType {
		case relationshipTypeFirebaseProjectBackedByProject:
			projectEdge = &got.Relationships[i]
		case relationshipTypeFirebaseProjectDefaultBucket:
			bucketEdge = &got.Relationships[i]
		}
	}
	if projectEdge == nil {
		t.Fatalf("missing backed_by_project edge: %#v", got.Relationships)
	}
	if projectEdge.TargetFullResourceName != "//cloudresourcemanager.googleapis.com/projects/123456789" {
		t.Errorf("backing project target = %q, want number-keyed canonical name", projectEdge.TargetFullResourceName)
	}
	if projectEdge.TargetAssetType != assetTypeCloudResourceManagerProject {
		t.Errorf("backing project asset type = %q", projectEdge.TargetAssetType)
	}
	if bucketEdge == nil {
		t.Fatalf("missing default_bucket edge: %#v", got.Relationships)
	}
	if bucketEdge.TargetFullResourceName != "//storage.googleapis.com/projects/_/buckets/demo-project.appspot.com" {
		t.Errorf("default bucket target = %q, want canonical bucket name", bucketEdge.TargetFullResourceName)
	}
	if bucketEdge.TargetAssetType != assetTypeStorageBucket {
		t.Errorf("default bucket asset type = %q", bucketEdge.TargetAssetType)
	}
}

// TestExtractFirebaseProjectBackingProjectFallsBackToProjectID proves that when
// projectNumber is absent no fabricated number-keyed edge is emitted; the edge is
// skipped rather than pointing at an unresolvable target.
func TestExtractFirebaseProjectBackingProjectRequiresNumber(t *testing.T) {
	blob := map[string]any{
		"projectId":   "demo-project",
		"displayName": "No Number",
		"state":       "ACTIVE",
	}
	raw, _ := json.Marshal(blob)
	got, err := extractFirebaseProject(ExtractContext{
		FullResourceName: "//firebase.googleapis.com/projects/demo-project",
		AssetType:        firebaseProjectAssetType,
		Data:             raw,
	})
	if err != nil {
		t.Fatalf("extractFirebaseProject: %v", err)
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeFirebaseProjectBackedByProject {
			t.Errorf("emitted backing-project edge without projectNumber: %#v", rel)
		}
	}
}

// TestExtractFirebaseProjectEmpty proves an empty/minimal blob yields no edges
// and no panic.
func TestExtractFirebaseProjectEmpty(t *testing.T) {
	got, err := extractFirebaseProject(ExtractContext{
		FullResourceName: "//firebase.googleapis.com/projects/demo-project",
		AssetType:        firebaseProjectAssetType,
		Data:             []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("extractFirebaseProject empty: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("empty blob emitted edges: %#v", got.Relationships)
	}
}

// TestExtractFirebaseProjectMalformed proves malformed JSON is a decode error,
// not a panic.
func TestExtractFirebaseProjectMalformed(t *testing.T) {
	_, err := extractFirebaseProject(ExtractContext{
		FullResourceName: "//firebase.googleapis.com/projects/demo-project",
		AssetType:        firebaseProjectAssetType,
		Data:             []byte(`{not json`),
	})
	if err == nil {
		t.Fatalf("expected decode error for malformed data")
	}
}

// TestFirebaseProjectExtractorRegistered proves the asset type is wired into the
// shared registry.
func TestFirebaseProjectExtractorRegistered(t *testing.T) {
	if !HasAssetExtractor(firebaseProjectAssetType) {
		t.Fatalf("firebase project extractor not registered for %q", firebaseProjectAssetType)
	}
}
