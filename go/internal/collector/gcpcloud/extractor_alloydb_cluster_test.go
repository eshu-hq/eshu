// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const alloyDBClusterFullName = "//alloydb.googleapis.com/projects/demo-project/locations/us-central1/clusters/primary"

func alloyDBClusterContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: alloyDBClusterFullName,
		AssetType:        assetTypeAlloyDBCluster,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestAlloyDBClusterExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeAlloyDBCluster); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeAlloyDBCluster)
	}
}

func TestExtractAlloyDBClusterFullFieldsWithNetworkAndCMEK(t *testing.T) {
	const data = `{
		"displayName": "primary-cluster",
		"uid": "abc123",
		"state": "READY",
		"clusterType": "PRIMARY",
		"databaseVersion": "POSTGRES_15",
		"subscriptionType": "STANDARD",
		"createTime": "2024-06-01T00:00:00Z",
		"networkConfig": {
			"network": "projects/demo-project/global/networks/prod-vpc"
		},
		"encryptionConfig": {
			"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/alloydb-key"
		},
		"encryptionInfo": {
			"encryptionType": "CUSTOMER_MANAGED_ENCRYPTION"
		},
		"automatedBackupPolicy": {
			"enabled": true,
			"location": "us-central1",
			"backupWindow": "3600s",
			"timeBasedRetention": {
				"retentionPeriod": "1209600s"
			}
		},
		"continuousBackupConfig": {
			"enabled": true,
			"recoveryWindowDays": 14
		}
	}`

	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"display_name":                           "primary-cluster",
		"uid":                                    "abc123",
		"state":                                  "READY",
		"cluster_type":                           "PRIMARY",
		"database_version":                       "POSTGRES_15",
		"subscription_type":                      "STANDARD",
		"creation_time":                          "2024-06-01T00:00:00Z",
		"kms_key_name":                           "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/alloydb-key",
		"encryption_type":                        "CUSTOMER_MANAGED_ENCRYPTION",
		"automated_backup_enabled":               true,
		"automated_backup_location":              "us-central1",
		"automated_backup_window":                "3600s",
		"automated_backup_retention_period":      "1209600s",
		"continuous_backup_enabled":              true,
		"continuous_backup_recovery_window_days": int64(14),
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 edges (network, kms), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAlloyDBClusterInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeAlloyDBClusterEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/alloydb-key", assetTypeKMSCryptoKey)

	wantAnchors := []string{
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc",
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/alloydb-key",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractAlloyDBClusterQuantityBasedRetention(t *testing.T) {
	const data = `{
		"state": "READY",
		"automatedBackupPolicy": {
			"enabled": true,
			"quantityBasedRetention": {"count": 5}
		}
	}`

	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["automated_backup_retention_count"] != int64(5) {
		t.Errorf("automated_backup_retention_count = %v, want 5", got.Attributes["automated_backup_retention_count"])
	}
	if _, ok := got.Attributes["automated_backup_retention_period"]; ok {
		t.Errorf("automated_backup_retention_period should be absent when quantity-based retention is used")
	}
}

func TestExtractAlloyDBClusterDeprecatedTopLevelNetworkFallback(t *testing.T) {
	const data = `{
		"state": "READY",
		"network": "projects/demo-project/global/networks/legacy-vpc"
	}`

	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (network), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAlloyDBClusterInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/legacy-vpc", assetTypeComputeNetwork)
}

func TestExtractAlloyDBClusterNetworkConfigTakesPrecedenceOverDeprecatedField(t *testing.T) {
	const data = `{
		"state": "READY",
		"network": "projects/demo-project/global/networks/legacy-vpc",
		"networkConfig": {
			"network": "projects/demo-project/global/networks/prod-vpc"
		}
	}`

	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (network), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAlloyDBClusterInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
}

func TestExtractAlloyDBClusterProjectLessNetworkPartialResolvedAgainstProject(t *testing.T) {
	const data = `{
		"networkConfig": {
			"network": "global/networks/prod-vpc"
		}
	}`

	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAlloyDBClusterInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
}

// TestExtractAlloyDBClusterAlphanumericProjectIDNetworkUnchanged proves a
// networkConfig.network reference that already carries a project id (not a
// numeric project number) is passed through exactly as reported.
func TestExtractAlloyDBClusterAlphanumericProjectIDNetworkUnchanged(t *testing.T) {
	const data = `{
		"networkConfig": {
			"network": "projects/other-project-id/global/networks/shared-vpc"
		}
	}`

	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAlloyDBClusterInNetwork,
		"//compute.googleapis.com/projects/other-project-id/global/networks/shared-vpc", assetTypeComputeNetwork)
}

// TestExtractAlloyDBClusterNumericProjectNetworkNeverRewritten proves that a
// networkConfig.network reference in the documented common-case form
// projects/<project_number>/global/networks/<id> is carried through with its
// project segment exactly as reported — never rewritten to ctx.ProjectID.
// AlloyDB supports Shared VPC, where the cluster's own project (a service
// project) can reference a network owned by a different host project, so a
// numeric project segment cannot be safely assumed to be the cluster's own
// project number. Rewriting it would risk fabricating an edge to a
// same-named network that happens to exist in the cluster's own project;
// leaving the numeric segment as-is means the edge simply does not resolve
// (Cloud Asset Inventory names Compute Network assets with the project id),
// which is the safe outcome. This anchor/edge target intentionally differs
// from ctx.ProjectID ("demo-project").
func TestExtractAlloyDBClusterNumericProjectNetworkNeverRewritten(t *testing.T) {
	const data = `{
		"networkConfig": {
			"network": "projects/123456789012/global/networks/prod-vpc"
		}
	}`

	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAlloyDBClusterInNetwork,
		"//compute.googleapis.com/projects/123456789012/global/networks/prod-vpc", assetTypeComputeNetwork)
}

// TestExtractAlloyDBClusterSharedVPCHostProjectNetworkNeverRewrittenToServiceProject
// proves the Shared VPC case explicitly: when the cluster's own project id
// ("demo-project", a service project) differs from the network's host
// project number, the emitted edge targets the host project's network
// reference verbatim — it is never rewritten to point at a network in the
// cluster's own (service) project, which would be a fabricated edge to a
// resource that was never observed.
func TestExtractAlloyDBClusterSharedVPCHostProjectNetworkNeverRewrittenToServiceProject(t *testing.T) {
	const data = `{
		"networkConfig": {
			"network": "projects/999888777666/global/networks/host-shared-vpc"
		}
	}`

	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	target := got.Relationships[0].TargetFullResourceName
	if target != "//compute.googleapis.com/projects/999888777666/global/networks/host-shared-vpc" {
		t.Fatalf("edge target = %q, want the host-project reference preserved verbatim", target)
	}
	if containsString(target, "demo-project") {
		t.Fatalf("edge target fabricated a reference to the cluster's own project: %q", target)
	}
}

// TestExtractAlloyDBClusterNumericProjectAlreadyCAIPrefixedNetworkUnchanged
// proves the same never-rewritten behavior when networkConfig.network already
// carries the CAI compute.googleapis.com/ prefix.
func TestExtractAlloyDBClusterNumericProjectAlreadyCAIPrefixedNetworkUnchanged(t *testing.T) {
	const data = `{
		"networkConfig": {
			"network": "//compute.googleapis.com/projects/123456789012/global/networks/prod-vpc"
		}
	}`

	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAlloyDBClusterInNetwork,
		"//compute.googleapis.com/projects/123456789012/global/networks/prod-vpc", assetTypeComputeNetwork)
}

func TestExtractAlloyDBClusterKMSKeyAlreadyCAIPrefixedNotDoublePrefixed(t *testing.T) {
	const data = `{
		"encryptionConfig": {
			"kmsKeyName": "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/alloydb-key"
		}
	}`

	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAlloyDBClusterEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/alloydb-key", assetTypeKMSCryptoKey)
	if got.Attributes["kms_key_name"] != "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/alloydb-key" {
		t.Errorf("kms_key_name = %v, want unchanged CAI-prefixed value", got.Attributes["kms_key_name"])
	}
}

func TestExtractAlloyDBClusterWrongDomainAbsoluteKMSNameRejected(t *testing.T) {
	const data = `{
		"encryptionConfig": {
			"kmsKeyName": "//storage.googleapis.com/some/other/resource"
		}
	}`

	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no edges for wrong-domain KMS name, got %#v", got.Relationships)
	}
	if _, ok := got.Attributes["kms_key_name"]; ok {
		t.Errorf("kms_key_name should be absent for a rejected wrong-domain reference")
	}
}

// TestExtractAlloyDBClusterNeverPersistsCredentials proves initialUser (and any
// password material) is never decoded into the output, even when present in
// the raw CAI blob.
func TestExtractAlloyDBClusterNeverPersistsCredentials(t *testing.T) {
	const data = `{
		"state": "READY",
		"initialUser": {
			"user": "postgres",
			"password": "super-secret-password"
		}
	}`
	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(blob)
	for _, token := range []string{"super-secret-password", "postgres"} {
		if containsString(s, token) {
			t.Fatalf("alloydb cluster extraction leaked credential token %q: %s", token, blob)
		}
	}
}

func TestExtractAlloyDBClusterNoNetworkOrKMSNoEdges(t *testing.T) {
	const data = `{
		"state": "READY",
		"databaseVersion": "POSTGRES_15"
	}`
	got, err := extractAlloyDBCluster(alloyDBClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractAlloyDBClusterEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractAlloyDBCluster(alloyDBClusterContext(`{}`))
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

func TestExtractAlloyDBClusterMalformedDataErrors(t *testing.T) {
	if _, err := extractAlloyDBCluster(alloyDBClusterContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
