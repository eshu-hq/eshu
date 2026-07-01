// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const artifactRepoFullName = "//artifactregistry.googleapis.com/projects/demo-project/locations/us-central1/repositories/team-docker"

func artifactRepoContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: artifactRepoFullName,
		AssetType:        assetTypeArtifactRegistryRepository,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestArtifactRegistryRepositoryExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeArtifactRegistryRepository); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeArtifactRegistryRepository)
	}
}

func TestExtractArtifactRegistryRepositoryFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/repositories/team-docker",
		"format": "DOCKER",
		"mode": "STANDARD_REPOSITORY",
		"description": "team docker images",
		"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/ar/cryptoKeys/repo-key",
		"sizeBytes": 10485760,
		"createTime": "2024-06-01T00:00:00Z",
		"cleanupPolicies": {"delete-old": {"id": "delete-old"}, "keep-recent": {"id": "keep-recent"}}
	}`

	got, err := extractArtifactRegistryRepository(artifactRepoContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"format":                      "DOCKER",
		"mode":                        "STANDARD_REPOSITORY",
		"size_bytes":                  int64(10485760),
		"cleanup_policy_count":        2,
		"customer_managed_encryption": true,
		"creation_time":               "2024-06-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	const kms = "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/ar/cryptoKeys/repo-key"
	if !containsStringSlice(got.CorrelationAnchors, kms) {
		t.Errorf("missing CMEK anchor %q in %#v", kms, got.CorrelationAnchors)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly the CMEK edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeArtifactRepoEncryptedByKMSKey, kms, assetTypeKMSCryptoKey)
	if got.Relationships[0].SourceFullResourceName != artifactRepoFullName {
		t.Errorf("relationship source = %q", got.Relationships[0].SourceFullResourceName)
	}
	if got.Relationships[0].SourceAssetType != assetTypeArtifactRegistryRepository {
		t.Errorf("relationship source asset type = %q", got.Relationships[0].SourceAssetType)
	}
}

func TestExtractArtifactRegistryRepositoryNoCMEK(t *testing.T) {
	const data = `{"format": "NPM", "mode": "STANDARD_REPOSITORY", "createTime": "2023-01-15T09:30:00Z"}`
	got, err := extractArtifactRegistryRepository(artifactRepoContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"format":        "NPM",
		"mode":          "STANDARD_REPOSITORY",
		"creation_time": "2023-01-15T09:30:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges without CMEK, got %#v", got.Relationships)
	}
	if _, ok := got.Attributes["customer_managed_encryption"]; ok {
		t.Errorf("no CMEK must not report the flag: %#v", got.Attributes)
	}
}

func TestExtractArtifactRegistryRepositoryEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractArtifactRegistryRepository(artifactRepoContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 || len(got.Relationships) != 0 || len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected empty extraction, got %#v", got)
	}
}

func TestExtractArtifactRegistryRepositoryMalformedDataErrors(t *testing.T) {
	if _, err := extractArtifactRegistryRepository(artifactRepoContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
