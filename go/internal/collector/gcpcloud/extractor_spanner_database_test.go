// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// assertAnchorSet asserts that got contains exactly the given anchors, as a
// set, ignoring order. dedupeNonEmpty's insertion-order behavior is an
// implementation detail, not a contract the extractor promises callers.
func assertAnchorSet(t *testing.T, got []string, want ...string) {
	t.Helper()
	gotSorted := append([]string(nil), got...)
	sort.Strings(gotSorted)
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)
	if !reflect.DeepEqual(gotSorted, wantSorted) {
		t.Fatalf("anchor set mismatch:\n got  %#v\nwant %#v", got, want)
	}
}

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

	// Assert anchor set membership, not ordered slice equality: dedupeNonEmpty
	// preserves insertion order but does not promise a stable ordering
	// contract across future callers, so pinning an exact slice shape would be
	// a brittle assertion on an implementation detail rather than the
	// anchor-set behavior the extractor actually guarantees.
	assertAnchorSet(t, got.CorrelationAnchors, wantInstance, wantKMS)
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

func TestExtractSpannerDatabaseExtraSegmentBetweenInstanceAndDatabaseEmitsNoParentEdge(t *testing.T) {
	// The documented shape is exactly
	// projects/<p>/instances/<i>/databases/<d>. A name carrying an extra
	// segment between the instance and the databases marker
	// (.../instances/<i>/extra/databases/<d>) must not derive a parent edge:
	// a loose Index("/databases/")+Contains("/instances/") check would accept
	// this and silently mint a wrong parent (".../instances/<i>/extra").
	ctx := ExtractContext{
		FullResourceName: "//spanner.googleapis.com/projects/demo-project/instances/prod-instance/extra/databases/orders",
		AssetType:        assetTypeSpannerDatabase,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(`{"state": "READY"}`),
	}

	got, err := extractSpannerDatabase(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no parent edge for an extra-segment full resource name, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for an extra-segment full resource name, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractSpannerDatabaseExtraSegmentAfterDatabaseEmitsNoParentEdge(t *testing.T) {
	// A trailing segment after the database id (a sub-resource path) must
	// also fail closed rather than accepting a database id that is not
	// actually the final path segment.
	ctx := ExtractContext{
		FullResourceName: "//spanner.googleapis.com/projects/demo-project/instances/prod-instance/databases/orders/extra",
		AssetType:        assetTypeSpannerDatabase,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(`{"state": "READY"}`),
	}

	got, err := extractSpannerDatabase(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no parent edge for a trailing-segment full resource name, got %#v", got.Relationships)
	}
}

func TestExtractSpannerDatabaseWrongDomainPrefixEmitsNoParentEdge(t *testing.T) {
	// A wrong-service absolute prefix must never be accepted as a Spanner
	// full resource name, even if the trailing segments otherwise match the
	// instances/databases shape.
	ctx := ExtractContext{
		FullResourceName: "//storage.googleapis.com/projects/demo-project/instances/prod-instance/databases/orders",
		AssetType:        assetTypeSpannerDatabase,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(`{"state": "READY"}`),
	}

	got, err := extractSpannerDatabase(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no parent edge for a wrong-domain-prefixed full resource name, got %#v", got.Relationships)
	}
}

func TestExtractSpannerDatabaseEmptyInstanceOrDatabaseIDEmitsNoParentEdge(t *testing.T) {
	// A blank instance or database id segment must fail closed rather than
	// deriving a parent with a dangling trailing slash.
	for _, name := range []string{
		"//spanner.googleapis.com/projects/demo-project/instances//databases/orders",
		"//spanner.googleapis.com/projects/demo-project/instances/prod-instance/databases/",
	} {
		ctx := ExtractContext{
			FullResourceName: name,
			AssetType:        assetTypeSpannerDatabase,
			ProjectID:        "demo-project",
			Data:             json.RawMessage(`{"state": "READY"}`),
		}
		got, err := extractSpannerDatabase(ctx)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", name, err)
		}
		if len(got.Relationships) != 0 {
			t.Errorf("expected no parent edge for %q, got %#v", name, got.Relationships)
		}
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

func TestExtractSpannerDatabaseMultiRegionKMSKeyNames(t *testing.T) {
	// A multi-region instance configuration can require more than one
	// regional CMEK key to fully cover its regions; the Database
	// EncryptionConfig carries this as the plural kmsKeyNames[] array (per the
	// live Spanner v1 discovery document), distinct from the singular
	// kmsKeyName field. One edge and one anchor must be emitted per key.
	const data = `{
		"name": "projects/demo-project/instances/multi-region/databases/orders",
		"state": "READY",
		"encryptionConfig": {"kmsKeyNames": [
			"projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/key-central1",
			"projects/demo-project/locations/us-east1/keyRings/ring/cryptoKeys/key-east1"
		]}
	}`

	ctx := ExtractContext{
		FullResourceName: "//spanner.googleapis.com/projects/demo-project/instances/multi-region/databases/orders",
		AssetType:        assetTypeSpannerDatabase,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
	got, err := extractSpannerDatabase(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantInstance := "//spanner.googleapis.com/projects/demo-project/instances/multi-region"
	wantKMS1 := "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/key-central1"
	wantKMS2 := "//cloudkms.googleapis.com/projects/demo-project/locations/us-east1/keyRings/ring/cryptoKeys/key-east1"

	kmsEdges := 0
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeSpannerDatabaseEncryptedByKMSKey {
			kmsEdges++
		}
	}
	if kmsEdges != 2 {
		t.Fatalf("expected one CMEK edge per kmsKeyNames[] entry, got %d: %#v", kmsEdges, got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSpannerDatabaseEncryptedByKMSKey, wantKMS1, assetTypeKMSCryptoKey)
	assertRelationship(t, got.Relationships, relationshipTypeSpannerDatabaseEncryptedByKMSKey, wantKMS2, assetTypeKMSCryptoKey)
	assertAnchorSet(t, got.CorrelationAnchors, wantInstance, wantKMS1, wantKMS2)
}

func TestExtractSpannerDatabaseSingularAndPluralKMSKeysBothEmitEdges(t *testing.T) {
	// The singular kmsKeyName and plural kmsKeyNames[] fields are
	// independent per the discovery document; a database could in principle
	// report both across a schema transition, so both must resolve to edges
	// rather than one silently shadowing the other.
	const data = `{
		"name": "projects/demo-project/instances/prod-instance/databases/orders",
		"state": "READY",
		"encryptionConfig": {
			"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/singular-key",
			"kmsKeyNames": ["projects/demo-project/locations/us-east1/keyRings/ring/cryptoKeys/plural-key"]
		}
	}`

	got, err := extractSpannerDatabase(spannerDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantSingular := "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/singular-key"
	wantPlural := "//cloudkms.googleapis.com/projects/demo-project/locations/us-east1/keyRings/ring/cryptoKeys/plural-key"
	assertRelationship(t, got.Relationships, relationshipTypeSpannerDatabaseEncryptedByKMSKey, wantSingular, assetTypeKMSCryptoKey)
	assertRelationship(t, got.Relationships, relationshipTypeSpannerDatabaseEncryptedByKMSKey, wantPlural, assetTypeKMSCryptoKey)
}

func TestExtractSpannerDatabaseDuplicateKMSKeyNamesDeduped(t *testing.T) {
	// A duplicate entry in kmsKeyNames[] (or an overlap with the singular
	// kmsKeyName) must not mint a duplicate edge or anchor.
	const data = `{
		"name": "projects/demo-project/instances/prod-instance/databases/orders",
		"state": "READY",
		"encryptionConfig": {
			"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/key",
			"kmsKeyNames": [
				"projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/key",
				"projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/key"
			]
		}
	}`

	got, err := extractSpannerDatabase(spannerDatabaseContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	kmsEdges := 0
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeSpannerDatabaseEncryptedByKMSKey {
			kmsEdges++
		}
	}
	if kmsEdges != 1 {
		t.Fatalf("expected exactly one deduplicated CMEK edge, got %d: %#v", kmsEdges, got.Relationships)
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
