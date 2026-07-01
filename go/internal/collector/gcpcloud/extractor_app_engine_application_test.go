// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const appEngineApplicationFullName = "//appengine.googleapis.com/apps/demo-project"

func appEngineApplicationContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: appEngineApplicationFullName,
		AssetType:        assetTypeAppEngineApplication,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestAppEngineApplicationExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeAppEngineApplication); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeAppEngineApplication)
	}
}

func TestExtractAppEngineApplicationFullResource(t *testing.T) {
	const data = `{
		"name": "apps/demo-project",
		"id": "demo-project",
		"locationId": "us-central",
		"servingStatus": "SERVING",
		"defaultBucket": "staging.demo-project.appspot.com",
		"defaultHostname": "demo-project.uc.r.appspot.com",
		"databaseType": "CLOUD_FIRESTORE",
		"createTime": "2024-01-01T00:00:00Z"
	}`
	got, err := extractAppEngineApplication(appEngineApplicationContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"location_id":      "us-central",
		"serving_status":   "SERVING",
		"default_bucket":   "staging.demo-project.appspot.com",
		"default_hostname": "demo-project.uc.r.appspot.com",
		"database_type":    "CLOUD_FIRESTORE",
		"creation_time":    "2024-01-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 bucket edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAppEngineApplicationUsesDefaultBucket,
		"//storage.googleapis.com/projects/_/buckets/staging.demo-project.appspot.com", assetTypeStorageBucket)

	wantAnchors := []string{"//storage.googleapis.com/projects/_/buckets/staging.demo-project.appspot.com"}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractAppEngineApplicationEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractAppEngineApplication(appEngineApplicationContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for empty data, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for empty data, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractAppEngineApplicationNoBucket(t *testing.T) {
	// When defaultBucket is absent no edge or anchor is emitted.
	const data = `{
		"locationId": "us-east1",
		"servingStatus": "SERVING",
		"databaseType": "CLOUD_DATASTORE_COMPATIBILITY"
	}`
	got, err := extractAppEngineApplication(appEngineApplicationContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("no bucket present; expected no edges, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("no bucket present; expected no anchors, got %#v", got.CorrelationAnchors)
	}
	if got.Attributes["location_id"] != "us-east1" {
		t.Errorf("location_id = %v, want us-east1", got.Attributes["location_id"])
	}
}

func TestExtractAppEngineApplicationMalformedDataErrors(t *testing.T) {
	if _, err := extractAppEngineApplication(appEngineApplicationContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
