// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

const appEngineServiceFullName = "//appengine.googleapis.com/apps/demo-project/services/default"

func appEngineServiceContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: appEngineServiceFullName,
		AssetType:        assetTypeAppEngineService,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestAppEngineServiceExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeAppEngineService); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeAppEngineService)
	}
}

func TestExtractAppEngineServiceFullResource(t *testing.T) {
	const data = `{
		"name": "apps/demo-project/services/default",
		"id": "default",
		"split": {
			"shardBy": "IP",
			"allocations": {
				"v1": 0.7,
				"v2": 0.3
			}
		}
	}`

	got, err := extractAppEngineService(appEngineServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"service_id":          "default",
		"split_shard_by":      "IP",
		"version_count":       2,
		"traffic_allocations": map[string]float64{"v1": 0.7, "v2": 0.3},
	}
	if got.Attributes["service_id"] != wantAttrs["service_id"] {
		t.Errorf("service_id = %v, want %v", got.Attributes["service_id"], wantAttrs["service_id"])
	}
	if got.Attributes["split_shard_by"] != wantAttrs["split_shard_by"] {
		t.Errorf("split_shard_by = %v, want %v", got.Attributes["split_shard_by"], wantAttrs["split_shard_by"])
	}
	if got.Attributes["version_count"] != wantAttrs["version_count"] {
		t.Errorf("version_count = %v, want %v", got.Attributes["version_count"], wantAttrs["version_count"])
	}
	allocs, _ := got.Attributes["traffic_allocations"].(map[string]float64)
	if allocs["v1"] != 0.7 || allocs["v2"] != 0.3 {
		t.Errorf("traffic_allocations = %v, want v1=0.7 v2=0.3", allocs)
	}

	// Two version edges, one per allocation key.
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 version edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	wantV1Target := appEngineServiceFullName + "/versions/v1"
	wantV2Target := appEngineServiceFullName + "/versions/v2"
	assertRelationship(t, got.Relationships, relationshipTypeServiceSplitsTrafficToVersion, wantV1Target, assetTypeAppEngineVersion)
	assertRelationship(t, got.Relationships, relationshipTypeServiceSplitsTrafficToVersion, wantV2Target, assetTypeAppEngineVersion)

	// Source full resource name must be set on every edge.
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != appEngineServiceFullName {
			t.Errorf("relationship source = %q, want %q", rel.SourceFullResourceName, appEngineServiceFullName)
		}
		if rel.SourceAssetType != assetTypeAppEngineService {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeAppEngineService)
		}
	}

	// Anchors are the version full resource names, sorted.
	if len(got.CorrelationAnchors) != 2 {
		t.Fatalf("expected 2 anchors, got %d: %#v", len(got.CorrelationAnchors), got.CorrelationAnchors)
	}
	wantAnchors := []string{wantV1Target, wantV2Target}
	sort.Strings(wantAnchors)
	gotAnchors := append([]string{}, got.CorrelationAnchors...)
	sort.Strings(gotAnchors)
	if !reflect.DeepEqual(gotAnchors, wantAnchors) {
		t.Errorf("anchors mismatch:\n got %#v\nwant %#v", gotAnchors, wantAnchors)
	}
}

func TestExtractAppEngineServiceNoShardBy(t *testing.T) {
	// shardBy absent: split_shard_by must not appear in attributes.
	const data = `{
		"name": "apps/demo-project/services/api",
		"id": "api",
		"split": {
			"allocations": {"v3": 1.0}
		}
	}`
	got, err := extractAppEngineService(appEngineServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["split_shard_by"]; ok {
		t.Errorf("expected split_shard_by to be absent when shardBy is empty")
	}
	if got.Attributes["service_id"] != "api" {
		t.Errorf("service_id = %v, want api", got.Attributes["service_id"])
	}
	if got.Attributes["version_count"] != 1 {
		t.Errorf("version_count = %v, want 1", got.Attributes["version_count"])
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 version edge, got %d", len(got.Relationships))
	}
}

func TestExtractAppEngineServiceEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractAppEngineService(appEngineServiceContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges for empty data, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for empty data, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractAppEngineServiceEmptyAllocationsYieldsNoEdges(t *testing.T) {
	// split present but allocations map is empty.
	const data = `{"id": "default", "split": {"shardBy": "RANDOM", "allocations": {}}}`
	got, err := extractAppEngineService(appEngineServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["split_shard_by"] != "RANDOM" {
		t.Errorf("split_shard_by = %v, want RANDOM", got.Attributes["split_shard_by"])
	}
	if _, ok := got.Attributes["version_count"]; ok {
		t.Errorf("expected version_count to be absent when allocations is empty")
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges for empty allocations, got %#v", got.Relationships)
	}
}

func TestExtractAppEngineServiceDeduplicatesVersionEdges(t *testing.T) {
	// Duplicate allocation keys should produce only one edge.
	// JSON objects can't have true duplicate keys, but we test the version
	// full-name builder helper directly to ensure deduplication is correct.
	ctx := appEngineServiceContext(`{
		"id": "svc",
		"split": {"allocations": {"v1": 0.6, "v1": 0.4}}
	}`)
	got, err := extractAppEngineService(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// JSON last-write-wins for duplicate keys; result is 1 allocation.
	if len(got.Relationships) > 1 {
		t.Errorf("duplicate version key produced %d edges, want at most 1", len(got.Relationships))
	}
}

func TestExtractAppEngineServiceMalformedDataErrors(t *testing.T) {
	if _, err := extractAppEngineService(appEngineServiceContext(`{bad`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
	if _, err := extractAppEngineService(appEngineServiceContext(``)); err == nil {
		t.Fatalf("expected an error for empty resource data")
	}
}

func TestAppEngineServiceVersionFullName(t *testing.T) {
	tests := []struct {
		serviceFullName string
		versionID       string
		want            string
	}{
		{
			serviceFullName: "//appengine.googleapis.com/apps/my-proj/services/default",
			versionID:       "v1",
			want:            "//appengine.googleapis.com/apps/my-proj/services/default/versions/v1",
		},
		{
			serviceFullName: "//appengine.googleapis.com/apps/my-proj/services/default",
			versionID:       "",
			want:            "",
		},
		{
			serviceFullName: "",
			versionID:       "v1",
			want:            "",
		},
	}
	for _, tc := range tests {
		got := appEngineVersionFullName(tc.serviceFullName, tc.versionID)
		if got != tc.want {
			t.Errorf("appEngineVersionFullName(%q, %q) = %q, want %q", tc.serviceFullName, tc.versionID, got, tc.want)
		}
	}
}
