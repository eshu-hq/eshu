// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"testing"
)

func fixtureFirebaseAppInfoData(t *testing.T) []byte {
	t.Helper()
	blob := map[string]any{
		"name":        "projects/demo-project/webApps/1:123456789:web:abc123",
		"appId":       "1:123456789:web:abc123",
		"displayName": "Demo Web App",
		"platform":    "WEB",
		"namespace":   "com.example.demo",
		"state":       "ACTIVE",
	}
	raw, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("marshal firebase app info fixture: %v", err)
	}
	return raw
}

func firebaseAppInfoCtx(t *testing.T) ExtractContext {
	t.Helper()
	return ExtractContext{
		FullResourceName: "//firebase.googleapis.com/projects/demo-project/webApps/1:123456789:web:abc123",
		AssetType:        firebaseAppInfoAssetType,
		Data:             fixtureFirebaseAppInfoData(t),
	}
}

// TestExtractFirebaseAppInfoAttributes proves the bounded attribute set is
// surfaced from resource.data.
func TestExtractFirebaseAppInfoAttributes(t *testing.T) {
	got, err := extractFirebaseAppInfo(firebaseAppInfoCtx(t))
	if err != nil {
		t.Fatalf("extractFirebaseAppInfo: %v", err)
	}
	if got.Attributes["app_id"] != "1:123456789:web:abc123" {
		t.Errorf("app_id = %v", got.Attributes["app_id"])
	}
	if got.Attributes["platform"] != "WEB" {
		t.Errorf("platform = %v, want WEB", got.Attributes["platform"])
	}
	if got.Attributes["display_name"] != "Demo Web App" {
		t.Errorf("display_name = %v", got.Attributes["display_name"])
	}
	if got.Attributes["namespace"] != "com.example.demo" {
		t.Errorf("namespace = %v", got.Attributes["namespace"])
	}
	if got.Attributes["state"] != "ACTIVE" {
		t.Errorf("state = %v, want ACTIVE", got.Attributes["state"])
	}
}

// TestExtractFirebaseAppInfoProjectEdge proves the app resolves a typed edge to
// its parent Firebase project, keyed by the canonical FirebaseProject full name.
func TestExtractFirebaseAppInfoProjectEdge(t *testing.T) {
	got, err := extractFirebaseAppInfo(firebaseAppInfoCtx(t))
	if err != nil {
		t.Fatalf("extractFirebaseAppInfo: %v", err)
	}
	var edge *RelationshipObservation
	for i := range got.Relationships {
		if got.Relationships[i].RelationshipType == relationshipTypeFirebaseAppBelongsToProject {
			edge = &got.Relationships[i]
		}
	}
	if edge == nil {
		t.Fatalf("missing belongs_to_project edge: %#v", got.Relationships)
	}
	if edge.TargetFullResourceName != "//firebase.googleapis.com/projects/demo-project" {
		t.Errorf("project target = %q, want canonical FirebaseProject name", edge.TargetFullResourceName)
	}
	if edge.TargetAssetType != firebaseProjectAssetType {
		t.Errorf("project target asset type = %q", edge.TargetAssetType)
	}
	if edge.SupportState != RelationshipSupportSupported {
		t.Errorf("project edge support = %q, want supported", edge.SupportState)
	}
}

// TestExtractFirebaseAppInfoNoProjectNoEdge proves that when the full resource
// name carries no derivable project, no unresolvable edge is emitted.
func TestExtractFirebaseAppInfoNoProjectNoEdge(t *testing.T) {
	got, err := extractFirebaseAppInfo(ExtractContext{
		FullResourceName: "//firebase.googleapis.com/webApps/orphan",
		AssetType:        firebaseAppInfoAssetType,
		Data:             []byte(`{"appId":"1:1:web:x","platform":"WEB"}`),
	})
	if err != nil {
		t.Fatalf("extractFirebaseAppInfo: %v", err)
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeFirebaseAppBelongsToProject {
			t.Errorf("emitted project edge without a derivable project: %#v", rel)
		}
	}
}

// TestExtractFirebaseAppInfoEmpty proves an empty data blob yields no attributes
// and no panic, while the parent-project edge is still derived from the full
// resource name (so exactly one relationship is emitted).
func TestExtractFirebaseAppInfoEmpty(t *testing.T) {
	got, err := extractFirebaseAppInfo(ExtractContext{
		FullResourceName: "//firebase.googleapis.com/projects/demo-project/webApps/x",
		AssetType:        firebaseAppInfoAssetType,
		Data:             []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("extractFirebaseAppInfo empty: %v", err)
	}
	// The parent project edge is still derivable from the full resource name even
	// with an empty data blob.
	if len(got.Relationships) != 1 {
		t.Errorf("edges = %d, want 1 (project edge from full name)", len(got.Relationships))
	}
}

// TestExtractFirebaseAppInfoMalformed proves malformed JSON is a decode error.
func TestExtractFirebaseAppInfoMalformed(t *testing.T) {
	_, err := extractFirebaseAppInfo(ExtractContext{
		FullResourceName: "//firebase.googleapis.com/projects/demo-project/webApps/x",
		AssetType:        firebaseAppInfoAssetType,
		Data:             []byte(`{bad`),
	})
	if err == nil {
		t.Fatalf("expected decode error for malformed data")
	}
}

// TestFirebaseAppInfoExtractorRegistered proves the asset type is wired into the
// shared registry.
func TestFirebaseAppInfoExtractorRegistered(t *testing.T) {
	if !HasAssetExtractor(firebaseAppInfoAssetType) {
		t.Fatalf("firebase app info extractor not registered for %q", firebaseAppInfoAssetType)
	}
}
