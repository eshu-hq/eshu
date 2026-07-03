// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const bigtableClusterFullName = "//bigtableadmin.googleapis.com/projects/demo-project/instances/prod-instance/clusters/prod-c1"

func bigtableClusterContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: bigtableClusterFullName,
		AssetType:        assetTypeBigtableCluster,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestBigtableClusterExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeBigtableCluster); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeBigtableCluster)
	}
}

// TestExtractBigtableClusterRealCAIShapeWithCMEK uses the real Bigtable Admin
// v2 Cluster resource shape (name, location, state, serveNodes,
// nodeScalingFactor, defaultStorageType, encryptionConfig.kmsKeyName). It
// proves the parent-instance edge (derived from the cluster resource-name
// parent) and the CMEK edge (from encryptionConfig.kmsKeyName, NOT a flat
// field).
func TestExtractBigtableClusterRealCAIShapeWithCMEK(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/instances/prod-instance/clusters/prod-c1",
		"location": "projects/demo-project/locations/us-central1-b",
		"state": "READY",
		"serveNodes": 3,
		"nodeScalingFactor": "NODE_SCALING_FACTOR_1X",
		"defaultStorageType": "SSD",
		"encryptionConfig": {
			"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/bt-key"
		}
	}`

	got, err := extractBigtableCluster(bigtableClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"location":             "us-central1-b",
		"state":                "READY",
		"serve_nodes":          int64(3),
		"node_scaling_factor":  "NODE_SCALING_FACTOR_1X",
		"default_storage_type": "SSD",
		"kms_key_name":         "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/bt-key",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 edges (parent instance, kms), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeBigtableClusterInInstance,
		"//bigtableadmin.googleapis.com/projects/demo-project/instances/prod-instance", assetTypeBigtableInstance)
	assertRelationship(t, got.Relationships, relationshipTypeBigtableClusterEncryptedByKMS,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/bt-key", assetTypeKMSCryptoKey)

	wantAnchors := []string{
		"//bigtableadmin.googleapis.com/projects/demo-project/instances/prod-instance",
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/bt-key",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

// TestExtractBigtableClusterNoCMEKOnlyParentEdge proves a Google-default-
// encrypted cluster (no encryptionConfig) still resolves the parent-instance
// edge but emits no CMEK edge or key attribute.
func TestExtractBigtableClusterNoCMEKOnlyParentEdge(t *testing.T) {
	const data = `{
		"location": "projects/demo-project/locations/us-east1-b",
		"state": "READY",
		"serveNodes": 5,
		"defaultStorageType": "SSD"
	}`

	got, err := extractBigtableCluster(bigtableClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := got.Attributes["kms_key_name"]; ok {
		t.Errorf("kms_key_name should be absent for a non-CMEK cluster: %#v", got.Attributes)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the parent-instance edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeBigtableClusterInInstance,
		"//bigtableadmin.googleapis.com/projects/demo-project/instances/prod-instance", assetTypeBigtableInstance)
}

func TestExtractBigtableClusterCMEKAlreadyCAIPrefixedNotDoublePrefixed(t *testing.T) {
	const data = `{
		"encryptionConfig": {
			"kmsKeyName": "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/bt-key"
		}
	}`

	got, err := extractBigtableCluster(bigtableClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRelationship(t, got.Relationships, relationshipTypeBigtableClusterEncryptedByKMS,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/bt-key", assetTypeKMSCryptoKey)
	if got.Attributes["kms_key_name"] != "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/bt-key" {
		t.Errorf("kms_key_name = %v, want bare relative form matching the anchor/edge normalization", got.Attributes["kms_key_name"])
	}
}

func TestExtractBigtableClusterParentInstanceDerivation(t *testing.T) {
	if got := bigtableParentInstanceFullName(bigtableClusterFullName); got != "//bigtableadmin.googleapis.com/projects/demo-project/instances/prod-instance" {
		t.Errorf("parent = %q, want the instance prefix", got)
	}
	if got := bigtableParentInstanceFullName("//bigtableadmin.googleapis.com/projects/p/instances/i"); got != "" {
		t.Errorf("a name with no /clusters/ segment must yield no parent, got %q", got)
	}
	if got := bigtableParentInstanceFullName(""); got != "" {
		t.Errorf("blank name must yield no parent, got %q", got)
	}
}

func TestExtractBigtableClusterNeverPersistsTableOrRowData(t *testing.T) {
	const data = `{
		"location": "projects/demo-project/locations/us-central1-b",
		"serveNodes": 3,
		"tableSchema": "SECRET_SCHEMA",
		"rowData": "SECRET_ROWS"
	}`
	got, err := extractBigtableCluster(bigtableClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"SECRET_SCHEMA", "SECRET_ROWS"} {
		if containsString(string(blob), token) {
			t.Fatalf("bigtable cluster extraction leaked data-plane token %q: %s", token, blob)
		}
	}
}

func TestExtractBigtableClusterEmptyDataYieldsParentEdgeOnly(t *testing.T) {
	got, err := extractBigtableCluster(bigtableClusterContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	// The parent edge is derived from the resource name, not the data blob, so
	// it is still present even for an empty data blob.
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the resource-name-derived parent edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeBigtableClusterInInstance,
		"//bigtableadmin.googleapis.com/projects/demo-project/instances/prod-instance", assetTypeBigtableInstance)
}

func TestExtractBigtableClusterMalformedDataErrors(t *testing.T) {
	if _, err := extractBigtableCluster(bigtableClusterContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
