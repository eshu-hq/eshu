// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const memcacheInstanceFullName = "//memcache.googleapis.com/projects/demo-project/locations/us-central1/instances/cache-primary"

func memcacheInstanceContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: memcacheInstanceFullName,
		AssetType:        assetTypeMemcacheInstance,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestMemcacheInstanceExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeMemcacheInstance); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeMemcacheInstance)
	}
}

func TestExtractMemcacheInstanceFullWithNetworkAndZones(t *testing.T) {
	const data = `{
		"displayName": "cache-primary",
		"labels": {"env": "prod"},
		"authorizedNetwork": "projects/demo-project/global/networks/prod-vpc",
		"zones": ["us-central1-a", "us-central1-b"],
		"nodeCount": 3,
		"nodeConfig": {"cpuCount": 4, "memorySizeMb": 4096},
		"memcacheVersion": "MEMCACHE_1_6_15",
		"memcacheFullVersion": "memcached-1.6.15",
		"createTime": "2024-06-01T00:00:00Z",
		"state": "READY",
		"maintenanceVersion": "2024-06-01-00-00",
		"effectiveMaintenanceVersion": "2024-06-01-00-00",
		"memcacheNodes": [
			{"nodeId": "node-0", "zone": "us-central1-a", "state": "READY", "host": "10.0.0.5", "port": 11211},
			{"nodeId": "node-1", "zone": "us-central1-b", "state": "READY", "host": "10.0.0.6", "port": 11211}
		]
	}`

	got, err := extractMemcacheInstance(memcacheInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"display_name":                  "cache-primary",
		"zone_count":                    2,
		"node_count":                    int64(3),
		"cpu_count":                     int64(4),
		"memory_size_mb":                int64(4096),
		"memcache_version":              "MEMCACHE_1_6_15",
		"memcache_full_version":         "memcached-1.6.15",
		"creation_time":                 "2024-06-01T00:00:00Z",
		"state":                         "READY",
		"maintenance_version":           "2024-06-01-00-00",
		"effective_maintenance_version": "2024-06-01-00-00",
		"memcache_node_count":           2,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (network), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeMemcacheInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)

	wantAnchors := []string{
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractMemcacheInstanceMinimalNoNetworkNoNodes(t *testing.T) {
	const data = `{
		"nodeCount": 1,
		"nodeConfig": {"cpuCount": 1, "memorySizeMb": 1024},
		"state": "CREATING"
	}`

	got, err := extractMemcacheInstance(memcacheInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["state"] != "CREATING" {
		t.Errorf("state = %v, want CREATING", got.Attributes["state"])
	}
	if got.Attributes["node_count"] != int64(1) {
		t.Errorf("node_count = %v, want 1", got.Attributes["node_count"])
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors, got %#v", got.CorrelationAnchors)
	}
	if _, ok := got.Attributes["display_name"]; ok {
		t.Errorf("display_name should be absent when unset: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["zone_count"]; ok {
		t.Errorf("zone_count should be absent when unset: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["memcache_node_count"]; ok {
		t.Errorf("memcache_node_count should be absent when unset: %#v", got.Attributes)
	}
}

func TestExtractMemcacheInstanceAuthorizedNetworkFullSelfLink(t *testing.T) {
	const data = `{
		"authorizedNetwork": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/prod-vpc"
	}`

	got, err := extractMemcacheInstance(memcacheInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (network), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeMemcacheInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
}

func TestExtractMemcacheInstanceAuthorizedNetworkProjectLessPartialResolvedAgainstProject(t *testing.T) {
	const data = `{
		"authorizedNetwork": "global/networks/prod-vpc"
	}`

	got, err := extractMemcacheInstance(memcacheInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRelationship(t, got.Relationships, relationshipTypeMemcacheInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
}

func TestExtractMemcacheInstanceNeverPersistsNodeHostOrPort(t *testing.T) {
	const data = `{
		"memcacheNodes": [
			{"nodeId": "node-0", "zone": "us-central1-a", "state": "READY", "host": "10.0.0.5", "port": 11211}
		],
		"discoveryEndpoint": "10.0.0.9:11211"
	}`
	got, err := extractMemcacheInstance(memcacheInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(blob)
	for _, token := range []string{"10.0.0.5", "11211", "10.0.0.9", "node-0"} {
		if containsString(s, token) {
			t.Fatalf("memcache instance extraction leaked sensitive/unbounded token %q: %s", token, blob)
		}
	}
}

func TestExtractMemcacheInstanceEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractMemcacheInstance(memcacheInstanceContext(`{}`))
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

func TestExtractMemcacheInstanceMalformedDataErrors(t *testing.T) {
	if _, err := extractMemcacheInstance(memcacheInstanceContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
