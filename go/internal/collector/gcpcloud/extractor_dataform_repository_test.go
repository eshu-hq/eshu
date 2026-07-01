// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const dataformRepositoryFullName = "//dataform.googleapis.com/projects/demo-project/locations/us-central1/repositories/analytics"

func dataformRepositoryContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: dataformRepositoryFullName,
		AssetType:        assetTypeDataformRepository,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestDataformRepositoryExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeDataformRepository); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeDataformRepository)
	}
}

func TestExtractDataformRepositoryFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/repositories/analytics",
		"gitRemoteSettings": {"url": "https://github.com/acme/dataform-analytics.git", "defaultBranch": "main", "authenticationTokenSecretVersion": "projects/demo-project/secrets/git-token/versions/3"},
		"serviceAccount": "dataform-runner@demo-project.iam.gserviceaccount.com",
		"workspaceCompilationOverrides": {"defaultDatabase": "demo-warehouse", "schemaSuffix": "_dev", "tablePrefix": "stg_"},
		"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/dataform/cryptoKeys/repo",
		"createTime": "2024-05-01T00:00:00Z",
		"labels": {"team": "data"}
	}`

	got, err := extractDataformRepository(dataformRepositoryContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"git_default_branch":          "main",
		"git_remote_host_fingerprint": dataformGitHostFingerprint("https://github.com/acme/dataform-analytics.git"),
		"service_account_fingerprint": secretsiam.GCPServiceAccountEmailDigest("dataform-runner@demo-project.iam.gserviceaccount.com"),
		"workspace_default_database":  "demo-warehouse",
		"customer_managed_encryption": true,
		"creation_time":               "2024-05-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	assertRelationship(t, got.Relationships, relationshipTypeRepositoryEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/dataform/cryptoKeys/repo", assetTypeKMSCryptoKey)
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the CMEK edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}

	// Neither the raw git remote URL/host nor the raw service-account email leaks.
	blob, _ := json.Marshal(got)
	for _, token := range []string{"github.com", "dataform-analytics", "dataform-runner@demo-project", "git-token"} {
		if containsString(string(blob), token) {
			t.Fatalf("dataform extraction leaked token %q: %s", token, blob)
		}
	}
}

func TestExtractDataformRepositoryMinimal(t *testing.T) {
	const data = `{"gitRemoteSettings": {"defaultBranch": "trunk"}}`
	got, err := extractDataformRepository(dataformRepositoryContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{"git_default_branch": "trunk"}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges without CMEK, got %#v", got.Relationships)
	}
}

func TestExtractDataformRepositoryEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractDataformRepository(dataformRepositoryContext(`{}`))
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

func TestExtractDataformRepositoryMalformedDataErrors(t *testing.T) {
	if _, err := extractDataformRepository(dataformRepositoryContext(`{bad`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
	if _, err := extractDataformRepository(dataformRepositoryContext(``)); err == nil {
		t.Fatalf("expected an error for empty resource data")
	}
}

func TestDataformGitHostFingerprintIsStableAndDeterministic(t *testing.T) {
	if dataformGitHostFingerprint("") != "" {
		t.Errorf("blank host must fingerprint to empty")
	}
	a := dataformGitHostFingerprint("https://GitHub.com/acme/repo.git")
	b := dataformGitHostFingerprint("https://github.com/other/repo.git")
	if a == "" || a != b {
		t.Errorf("fingerprint must key on the case-normalized host only: %q vs %q", a, b)
	}
	if containsString(a, "github.com") {
		t.Errorf("fingerprint must not contain the raw host: %q", a)
	}
}
