// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const firestoreDatabaseFullName = "//firestore.googleapis.com/projects/demo-project/databases/(default)"

func firestoreDatabaseContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: firestoreDatabaseFullName,
		AssetType:        assetTypeFirestoreDatabase,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestFirestoreDatabaseExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeFirestoreDatabase); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeFirestoreDatabase)
	}
}

func TestExtractFirestoreDatabaseFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/databases/(default)",
		"type": "FIRESTORE_NATIVE",
		"locationId": "nam5",
		"concurrencyMode": "PESSIMISTIC",
		"appEngineIntegrationMode": "DISABLED",
		"pointInTimeRecoveryEnablement": "POINT_IN_TIME_RECOVERY_ENABLED",
		"deleteProtectionState": "DELETE_PROTECTION_ENABLED",
		"cmekConfig": {"kmsKeyName": "projects/demo-project/locations/nam5/keyRings/firestore/cryptoKeys/db"},
		"createTime": "2024-05-01T00:00:00Z"
	}`

	got, err := extractFirestoreDatabase(firestoreDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"database_type":               "FIRESTORE_NATIVE",
		"location_id":                 "nam5",
		"concurrency_mode":            "PESSIMISTIC",
		"app_engine_integration_mode": "DISABLED",
		"point_in_time_recovery":      "POINT_IN_TIME_RECOVERY_ENABLED",
		"delete_protection":           "DELETE_PROTECTION_ENABLED",
		"customer_managed_encryption": true,
		"creation_time":               "2024-05-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantAnchors := []string{"//cloudkms.googleapis.com/projects/demo-project/locations/nam5/keyRings/firestore/cryptoKeys/db"}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 KMS edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeFirestoreEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/nam5/keyRings/firestore/cryptoKeys/db", assetTypeKMSCryptoKey)
	rel := got.Relationships[0]
	if rel.SourceFullResourceName != firestoreDatabaseFullName || rel.SourceAssetType != assetTypeFirestoreDatabase {
		t.Errorf("relationship source = %q/%q, want firestore identity", rel.SourceFullResourceName, rel.SourceAssetType)
	}
}

func TestExtractFirestoreDatabaseGoogleManagedEncryption(t *testing.T) {
	const data = `{
		"type": "DATASTORE_MODE",
		"locationId": "us-east1",
		"createTime": "2023-02-01T00:00:00Z"
	}`
	got, err := extractFirestoreDatabase(firestoreDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"database_type": "DATASTORE_MODE",
		"location_id":   "us-east1",
		"creation_time": "2023-02-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges without CMEK, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors without CMEK, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractFirestoreDatabaseEmptyCMEKKeyOmitsFlag(t *testing.T) {
	// A cmekConfig with a blank key must not set the CMEK flag or emit an edge.
	const data = `{"type": "FIRESTORE_NATIVE", "cmekConfig": {"kmsKeyName": ""}}`
	got, err := extractFirestoreDatabase(firestoreDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["customer_managed_encryption"]; ok {
		t.Errorf("blank kmsKeyName must not set the CMEK flag: %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("blank kmsKeyName must emit no edge, got %#v", got.Relationships)
	}
}

func TestExtractFirestoreDatabaseEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractFirestoreDatabase(firestoreDatabaseContext(`{}`))
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

func TestExtractFirestoreDatabaseMalformedDataErrors(t *testing.T) {
	if _, err := extractFirestoreDatabase(firestoreDatabaseContext(`{bad`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
	if _, err := extractFirestoreDatabase(firestoreDatabaseContext(``)); err == nil {
		t.Fatalf("expected an error for empty resource data")
	}
}

func TestFirestoreKMSKeyFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"relative key", "projects/p/locations/l/keyRings/r/cryptoKeys/k", "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k"},
		{"leading slash", "/projects/p/locations/l/keyRings/r/cryptoKeys/k", "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k"},
		{"already full name", "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k", "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k"},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firestoreKMSKeyFullName(tc.in); got != tc.want {
				t.Errorf("firestoreKMSKeyFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
