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

	if got.Attributes["service_id"] != "default" {
		t.Errorf("service_id = %v, want default", got.Attributes["service_id"])
	}
	if got.Attributes["split_shard_by"] != "IP" {
		t.Errorf("split_shard_by = %v, want IP", got.Attributes["split_shard_by"])
	}
	if got.Attributes["version_count"] != 2 {
		t.Errorf("version_count = %v, want 2", got.Attributes["version_count"])
	}
	// traffic_allocations is a sorted "versionID=percentage" string slice so it
	// survives the cloud-inventory readback sanitizer (which drops nested maps).
	allocs, ok := got.Attributes["traffic_allocations"].([]string)
	if !ok {
		t.Fatalf("traffic_allocations = %#v, want []string", got.Attributes["traffic_allocations"])
	}
	if !reflect.DeepEqual(allocs, []string{"v1=0.7", "v2=0.3"}) {
		t.Errorf("traffic_allocations = %#v, want [v1=0.7 v2=0.3]", allocs)
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

func TestExtractAppEngineServiceBlankVersionKeySkipped(t *testing.T) {
	// A blank/whitespace allocation key must be skipped entirely: it emits no
	// edge or anchor (it has no resolvable version full name), it does not
	// persist an empty "=pct" entry in traffic_allocations, and it does not
	// inflate version_count. Only the real version survives.
	const data = `{
		"id": "svc",
		"split": {"allocations": {"  ": 0.4, "v9": 0.6}}
	}`
	got, err := extractAppEngineService(appEngineServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["version_count"] != 1 {
		t.Errorf("version_count = %v, want 1 (blank key skipped)", got.Attributes["version_count"])
	}
	allocs, _ := got.Attributes["traffic_allocations"].([]string)
	if !reflect.DeepEqual(allocs, []string{"v9=0.6"}) {
		t.Errorf("traffic_allocations = %#v, want [v9=0.6]", allocs)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (blank key produces none), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	if len(got.CorrelationAnchors) != 1 {
		t.Errorf("expected 1 anchor, got %#v", got.CorrelationAnchors)
	}
}

func TestAppEngineServiceVersionFullNameNormalizesInputs(t *testing.T) {
	// The builder trims both inputs and drops a trailing slash on the service
	// name so a target full resource name never carries whitespace or a doubled
	// slash.
	got := appEngineVersionFullName("//appengine.googleapis.com/apps/p/services/default/", "  v1  ")
	const want = "//appengine.googleapis.com/apps/p/services/default/versions/v1"
	if got != want {
		t.Errorf("appEngineVersionFullName normalized = %q, want %q", got, want)
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
