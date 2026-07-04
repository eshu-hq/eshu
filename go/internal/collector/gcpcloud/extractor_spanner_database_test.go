// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const spannerDatabaseFullName = "//spanner.googleapis.com/projects/demo-project/instances/prod-instance/databases/orders"

func spannerDatabaseContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: spannerDatabaseFullName,
		AssetType:        assetTypeSpannerDatabase,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestSpannerDatabaseExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeSpannerDatabase); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeSpannerDatabase)
	}
}

func TestExtractSpannerDatabaseFullAttributes(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/instances/prod-instance/databases/orders",
		"state": "READY",
		"databaseDialect": "GOOGLE_STANDARD_SQL",
		"versionRetentionPeriod": "3600s",
		"earliestVersionTime": "2026-07-01T00:00:00Z",
		"createTime": "2026-01-01T00:00:00Z",
		"defaultLeader": "us-central1",
		"enableDropProtection": true,
		"encryptionConfig": {"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/key"}
	}`

	got, err := extractSpannerDatabase(spannerDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"state":                    "READY",
		"database_dialect":         "GOOGLE_STANDARD_SQL",
		"version_retention_period": "3600s",
		"earliest_version_time":    "2026-07-01T00:00:00Z",
		"create_time":              "2026-01-01T00:00:00Z",
		"default_leader":           "us-central1",
		"enable_drop_protection":   true,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantInstance := "//spanner.googleapis.com/projects/demo-project/instances/prod-instance"
	wantKMS := "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/key"
	if len(got.Relationships) != 2 {
		t.Fatalf("expected parent-instance and CMEK edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSpannerDatabaseInInstance, wantInstance, assetTypeSpannerInstance)
	assertRelationship(t, got.Relationships, relationshipTypeSpannerDatabaseEncryptedByKMSKey, wantKMS, assetTypeKMSCryptoKey)

	wantAnchors := []string{wantInstance, wantKMS}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractSpannerDatabaseParentInstanceDerivedFromName(t *testing.T) {
	// The Database resource carries no separate parent-instance field; the
	// parent is derived from the database's own CAI full resource name
	// (.../instances/<i>/databases/<d>), mirroring the Bigtable Cluster
	// extractor's parent-instance derivation.
	const data = `{"name": "projects/demo-project/instances/prod-instance/databases/orders", "state": "READY"}`

	got, err := extractSpannerDatabase(spannerDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantInstance := "//spanner.googleapis.com/projects/demo-project/instances/prod-instance"
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly the parent-instance edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSpannerDatabaseInInstance, wantInstance, assetTypeSpannerInstance)
}

func TestExtractSpannerDatabaseMalformedFullResourceNameEmitsNoParentEdge(t *testing.T) {
	// A full resource name that does not match the documented
	// .../instances/<i>/databases/<d> shape must not mint a fabricated parent
	// edge; fail closed rather than guess.
	ctx := ExtractContext{
		FullResourceName: "//spanner.googleapis.com/projects/demo-project/instances/prod-instance",
		AssetType:        assetTypeSpannerDatabase,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(`{"state": "READY"}`),
	}

	got, err := extractSpannerDatabase(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no parent edge for a malformed full resource name, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for a malformed full resource name, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractSpannerDatabasePostgreSQLDialect(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/instances/prod-instance/databases/pg-db",
		"state": "READY",
		"databaseDialect": "POSTGRESQL"
	}`

	got, err := extractSpannerDatabase(spannerDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["database_dialect"] != "POSTGRESQL" {
		t.Errorf("database_dialect = %v, want POSTGRESQL", got.Attributes["database_dialect"])
	}
}

func TestExtractSpannerDatabaseReadyOptimizingState(t *testing.T) {
	// READY_OPTIMIZING is a real lifecycle state per the live Spanner v1
	// discovery document's Database.state enum (a database still being
	// optimized after a restore); it must be kept verbatim, not collapsed
	// into READY.
	const data = `{
		"name": "projects/demo-project/instances/prod-instance/databases/restored",
		"state": "READY_OPTIMIZING"
	}`

	got, err := extractSpannerDatabase(spannerDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["state"] != "READY_OPTIMIZING" {
		t.Errorf("state = %v, want READY_OPTIMIZING", got.Attributes["state"])
	}
}

func TestExtractSpannerDatabaseNoEncryptionConfigEmitsNoKMSEdge(t *testing.T) {
	// A database using Google-default encryption reports an empty
	// encryptionConfig (per the live discovery doc: "For databases that are
	// using Google default or other types of encryption, this field is
	// empty"); no CMEK edge or anchor should be fabricated.
	const data = `{"name": "projects/demo-project/instances/prod-instance/databases/orders", "state": "READY"}`

	got, err := extractSpannerDatabase(spannerDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeSpannerDatabaseEncryptedByKMSKey {
			t.Errorf("expected no CMEK edge without encryptionConfig, got %#v", rel)
		}
	}
	if _, ok := got.Attributes["kms_key_name"]; ok {
		t.Errorf("kms_key_name attribute should be absent, got %#v", got.Attributes)
	}
}

func TestExtractSpannerDatabaseEnableDropProtectionFalseIsPreserved(t *testing.T) {
	// enableDropProtection is a plain proto3 boolean defaulting to false; an
	// explicit false must be distinguishable from an absent field, mirroring
	// the Backend Service extractor's EnableCDN tri-state treatment.
	const data = `{
		"name": "projects/demo-project/instances/prod-instance/databases/orders",
		"state": "READY",
		"enableDropProtection": false
	}`

	got, err := extractSpannerDatabase(spannerDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := got.Attributes["enable_drop_protection"]
	if !ok {
		t.Fatalf("enable_drop_protection=false must be preserved as an explicit posture, got %#v", got.Attributes)
	}
	if v != false {
		t.Errorf("enable_drop_protection = %v, want false", v)
	}
}

func TestExtractSpannerDatabaseAbsentEnableDropProtectionOmitted(t *testing.T) {
	const data = `{"name": "projects/demo-project/instances/prod-instance/databases/orders", "state": "READY"}`

	got, err := extractSpannerDatabase(spannerDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["enable_drop_protection"]; ok {
		t.Errorf("absent enable_drop_protection must be omitted, got %#v", got.Attributes)
	}
}

func TestExtractSpannerDatabasePartialDataOmitsZeroValues(t *testing.T) {
	got, err := extractSpannerDatabase(ExtractContext{
		FullResourceName: "not-a-recognizable-name",
		AssetType:        assetTypeSpannerDatabase,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(`{}`),
	})
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

func TestExtractSpannerDatabaseMalformedDataErrors(t *testing.T) {
	if _, err := extractSpannerDatabase(spannerDatabaseContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractSpannerDatabaseWrongDomainKMSKeyEmitsNoEdge(t *testing.T) {
	// A malformed or wrong-domain absolute CMEK reference — which real Cloud
	// Asset Inventory never emits — must fail closed per the shared
	// cmekKeyFullResourceName strict-domain contract.
	const data = `{
		"name": "projects/demo-project/instances/prod-instance/databases/orders",
		"state": "READY",
		"encryptionConfig": {"kmsKeyName": "//storage.googleapis.com/projects/demo-project/buckets/evil"}
	}`

	got, err := extractSpannerDatabase(spannerDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeSpannerDatabaseEncryptedByKMSKey {
			t.Errorf("expected no CMEK edge for a wrong-domain reference, got %#v", rel)
		}
	}
}

func TestExtractSpannerDatabaseAdversarialRedactionSweep(t *testing.T) {
	// Full-struct JSON marshal + banned-token sweep per repo convention: DDL
	// statements, encryptionInfo key-version detail, and restore-source
	// content must never leak through the extraction output.
	const data = `{
		"name": "projects/demo-project/instances/prod-instance/databases/adversarial",
		"state": "READY",
		"databaseDialect": "GOOGLE_STANDARD_SQL",
		"encryptionConfig": {"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/key"},
		"encryptionInfo": [{"encryptionType": "CUSTOMER_MANAGED_ENCRYPTION", "kmsKeyVersion": "projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/key/cryptoKeyVersions/1"}],
		"restoreInfo": {"sourceType": "BACKUP", "backupInfo": {"backup": "projects/demo-project/instances/prod-instance/backups/secret-backup-name"}}
	}`

	got, err := extractSpannerDatabase(spannerDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	bannedTokens := []string{
		"cryptoKeyVersions",
		"restoreInfo",
		"secret-backup-name",
		"BACKUP",
		"kmsKeyVersion",
	}
	for _, token := range bannedTokens {
		if containsString(string(blob), token) {
			t.Errorf("extraction output leaked banned token %q: %s", token, blob)
		}
	}
}
