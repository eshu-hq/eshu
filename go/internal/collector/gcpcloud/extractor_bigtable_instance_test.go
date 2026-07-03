// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const bigtableInstanceFullName = "//bigtableadmin.googleapis.com/projects/demo-project/instances/prod-instance"

func bigtableInstanceContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: bigtableInstanceFullName,
		AssetType:        assetTypeBigtableInstance,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestBigtableInstanceExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeBigtableInstance); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeBigtableInstance)
	}
}

// TestExtractBigtableInstanceRealCAIShape uses the real Bigtable Admin v2
// Instance resource shape (name, displayName, state, type, edition, labels,
// createTime, tags, satisfiesPz*). The Instance resource has NO clusters,
// encryption, or kmsKeyName field — clusters are a separate CAI asset type
// handled by extractor_bigtable_cluster.go — so the Instance extractor emits
// no edges or anchors and never surfaces a cluster/CMEK attribute.
func TestExtractBigtableInstanceRealCAIShape(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/instances/prod-instance",
		"displayName": "Production Instance",
		"state": "READY",
		"type": "PRODUCTION",
		"edition": "ENTERPRISE",
		"labels": {"env": "prod"},
		"createTime": "2024-06-01T00:00:00Z",
		"satisfiesPzs": false
	}`

	got, err := extractBigtableInstance(bigtableInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"display_name":  "Production Instance",
		"state":         "READY",
		"instance_type": "PRODUCTION",
		"edition":       "ENTERPRISE",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("Instance extractor must emit no edges, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("Instance extractor must emit no anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractBigtableInstanceDevelopmentMinimal(t *testing.T) {
	const data = `{
		"displayName": "Dev Instance",
		"state": "READY",
		"type": "DEVELOPMENT"
	}`

	got, err := extractBigtableInstance(bigtableInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["instance_type"] != "DEVELOPMENT" {
		t.Errorf("instance_type = %v, want DEVELOPMENT", got.Attributes["instance_type"])
	}
	if _, ok := got.Attributes["edition"]; ok {
		t.Errorf("edition should be absent when unset: %#v", got.Attributes)
	}
}

// TestExtractBigtableInstanceIgnoresStrayClusterShape proves the Instance
// extractor never surfaces a cluster/CMEK attribute even if a stray
// clusters/kmsKeyName-shaped field appears in the blob, since those belong to
// the separate Cluster asset type. This is the guard against re-introducing the
// wrong-shape false-green.
func TestExtractBigtableInstanceIgnoresStrayClusterShape(t *testing.T) {
	const data = `{
		"displayName": "Prod",
		"clusters": [{"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/bt-key"}]
	}`
	got, err := extractBigtableInstance(bigtableInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("Instance extractor must not emit a KMS edge from a stray cluster shape: %#v", got.Relationships)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if containsString(string(blob), "bt-key") {
		t.Fatalf("Instance extractor leaked a cluster kmsKeyName: %s", blob)
	}
}

func TestExtractBigtableInstanceEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractBigtableInstance(bigtableInstanceContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for empty data, got %#v", got.Relationships)
	}
}

func TestExtractBigtableInstanceMalformedDataErrors(t *testing.T) {
	if _, err := extractBigtableInstance(bigtableInstanceContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
