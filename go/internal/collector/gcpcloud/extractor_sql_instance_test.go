// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const sqlInstanceFullName = "//sqladmin.googleapis.com/projects/demo-project/instances/primary-db"

func sqlInstanceContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: sqlInstanceFullName,
		AssetType:        assetTypeSQLInstance,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestSQLInstanceExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeSQLInstance); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeSQLInstance)
	}
}

func TestExtractSQLInstancePrimaryWithPrivateNetworkAndCMEK(t *testing.T) {
	const data = `{
		"databaseVersion": "POSTGRES_15",
		"region": "us-central1",
		"state": "RUNNABLE",
		"instanceType": "CLOUD_SQL_INSTANCE",
		"settings": {
			"tier": "db-custom-2-7680",
			"availabilityType": "REGIONAL",
			"dataDiskSizeGb": "100",
			"ipConfiguration": {
				"ipv4Enabled": false,
				"privateNetwork": "projects/demo-project/global/networks/prod-vpc",
				"authorizedNetworks": [{"value": "203.0.113.0/24"}],
				"sslMode": "ENCRYPTED_ONLY"
			},
			"backupConfiguration": {
				"enabled": true,
				"pointInTimeRecoveryEnabled": true,
				"transactionLogRetentionDays": 7
			}
		},
		"diskEncryptionConfiguration": {
			"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/sql-key"
		},
		"replicaNames": [
			"projects/demo-project/instances/primary-db-replica-1"
		],
		"createTime": "2024-06-01T00:00:00Z"
	}`

	got, err := extractSQLInstance(sqlInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"database_version":               "POSTGRES_15",
		"region":                         "us-central1",
		"state":                          "RUNNABLE",
		"instance_type":                  "CLOUD_SQL_INSTANCE",
		"tier":                           "db-custom-2-7680",
		"availability_type":              "REGIONAL",
		"data_disk_size_gb":              int64(100),
		"public_ip_enabled":              false,
		"ssl_mode":                       "ENCRYPTED_ONLY",
		"authorized_network_count":       1,
		"backups_enabled":                true,
		"point_in_time_recovery_enabled": true,
		"transaction_log_retention_days": int64(7),
		"kms_key_name":                   "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/sql-key",
		"replica_count":                  1,
		"creation_time":                  "2024-06-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	if len(got.Relationships) != 3 {
		t.Fatalf("expected 3 edges (network, kms, replica), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSQLInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeSQLInstanceEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/sql-key", assetTypeKMSCryptoKey)
	assertRelationship(t, got.Relationships, relationshipTypeSQLInstanceHasReplica,
		"//sqladmin.googleapis.com/projects/demo-project/instances/primary-db-replica-1", assetTypeSQLInstance)

	wantAnchors := []string{
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc",
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/sql-key",
		"//sqladmin.googleapis.com/projects/demo-project/instances/primary-db-replica-1",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractSQLInstanceBareReplicaNameResolvedAgainstProject(t *testing.T) {
	const data = `{
		"databaseVersion": "POSTGRES_15",
		"region": "us-central1",
		"state": "RUNNABLE",
		"instanceType": "CLOUD_SQL_INSTANCE",
		"replicaNames": ["primary-db-replica-1"]
	}`

	got, err := extractSQLInstance(sqlInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["replica_count"] != 1 {
		t.Fatalf("replica_count = %v, want 1", got.Attributes["replica_count"])
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (has_replica), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSQLInstanceHasReplica,
		"//sqladmin.googleapis.com/projects/demo-project/instances/primary-db-replica-1", assetTypeSQLInstance)
}

func TestExtractSQLInstanceBareMasterNameResolvedAgainstProject(t *testing.T) {
	const data = `{
		"databaseVersion": "MYSQL_8_0",
		"region": "us-east1",
		"state": "RUNNABLE",
		"instanceType": "READ_REPLICA_INSTANCE",
		"masterInstanceName": "primary-db"
	}`

	got, err := extractSQLInstance(sqlInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (replica_of master), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSQLInstanceReplicaOf,
		"//sqladmin.googleapis.com/projects/demo-project/instances/primary-db", assetTypeSQLInstance)
}

func TestExtractSQLInstanceReadReplicaEdgeToMaster(t *testing.T) {
	const data = `{
		"databaseVersion": "MYSQL_8_0",
		"region": "us-east1",
		"state": "RUNNABLE",
		"instanceType": "READ_REPLICA_INSTANCE",
		"masterInstanceName": "projects/demo-project/instances/primary-db",
		"settings": {
			"tier": "db-n1-standard-1",
			"ipConfiguration": {"ipv4Enabled": true}
		}
	}`

	got, err := extractSQLInstance(sqlInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["public_ip_enabled"] != true {
		t.Errorf("public_ip_enabled = %v, want true", got.Attributes["public_ip_enabled"])
	}
	if got.Attributes["instance_type"] != "READ_REPLICA_INSTANCE" {
		t.Errorf("instance_type = %v, want READ_REPLICA_INSTANCE", got.Attributes["instance_type"])
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (replica_of master), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSQLInstanceReplicaOf,
		"//sqladmin.googleapis.com/projects/demo-project/instances/primary-db", assetTypeSQLInstance)
}

func TestExtractSQLInstanceNeverPersistsRawIPAddresses(t *testing.T) {
	const data = `{
		"databaseVersion": "POSTGRES_15",
		"ipAddresses": [
			{"type": "PRIMARY", "ipAddress": "198.51.100.7"},
			{"type": "PRIVATE", "ipAddress": "10.0.0.5"}
		],
		"settings": {
			"ipConfiguration": {
				"ipv4Enabled": true,
				"authorizedNetworks": [
					{"value": "203.0.113.0/24", "name": "office"},
					{"value": "198.51.100.0/24", "name": "vpn"}
				]
			}
		}
	}`
	got, err := extractSQLInstance(sqlInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(blob)
	for _, token := range []string{"198.51.100.7", "10.0.0.5", "203.0.113.0/24", "198.51.100.0/24", "office", "vpn"} {
		if containsString(s, token) {
			t.Fatalf("sql instance extraction leaked sensitive token %q: %s", token, blob)
		}
	}
	if got.Attributes["authorized_network_count"] != 2 {
		t.Errorf("authorized_network_count = %v, want 2 (presence/count only)", got.Attributes["authorized_network_count"])
	}
	if got.Attributes["public_ip_enabled"] != true {
		t.Errorf("public_ip_enabled = %v, want true", got.Attributes["public_ip_enabled"])
	}
}

func TestExtractSQLInstanceKMSKeyAlreadyCAIPrefixedNotDoublePrefixed(t *testing.T) {
	const data = `{
		"databaseVersion": "POSTGRES_15",
		"diskEncryptionConfiguration": {
			"kmsKeyName": "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/sql-key"
		}
	}`

	got, err := extractSQLInstance(sqlInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRelationship(t, got.Relationships, relationshipTypeSQLInstanceEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/sql-key", assetTypeKMSCryptoKey)
	if got.Attributes["kms_key_name"] != "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/sql-key" {
		t.Errorf("kms_key_name = %v, want unchanged CAI-prefixed value", got.Attributes["kms_key_name"])
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeSQLInstanceEncryptedByKMSKey {
			if strings.Count(rel.TargetFullResourceName, cloudKMSResourceNamePrefix) != 1 {
				t.Fatalf("kms target double-prefixed: %q", rel.TargetFullResourceName)
			}
		}
	}
}

func TestExtractSQLInstancePrivateNetworkProjectLessPartialResolvedAgainstProject(t *testing.T) {
	const data = `{
		"settings": {
			"ipConfiguration": {
				"privateNetwork": "global/networks/prod-vpc"
			}
		}
	}`

	got, err := extractSQLInstance(sqlInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRelationship(t, got.Relationships, relationshipTypeSQLInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
}

func TestExtractSQLInstanceDataDiskSizeGbAsJSONNumber(t *testing.T) {
	const data = `{
		"settings": {
			"dataDiskSizeGb": 250
		}
	}`

	got, err := extractSQLInstance(sqlInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["data_disk_size_gb"] != int64(250) {
		t.Fatalf("data_disk_size_gb = %v, want int64(250)", got.Attributes["data_disk_size_gb"])
	}
}

func TestExtractSQLInstanceNoNetworkOrKMSNoEdges(t *testing.T) {
	const data = `{
		"databaseVersion": "MYSQL_8_0",
		"state": "RUNNABLE",
		"settings": {"tier": "db-f1-micro"}
	}`
	got, err := extractSQLInstance(sqlInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors, got %#v", got.CorrelationAnchors)
	}
	if _, ok := got.Attributes["kms_key_name"]; ok {
		t.Errorf("kms_key_name should be absent when unset: %#v", got.Attributes)
	}
}

func TestExtractSQLInstanceEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractSQLInstance(sqlInstanceContext(`{}`))
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

func TestExtractSQLInstanceMalformedDataErrors(t *testing.T) {
	if _, err := extractSQLInstance(sqlInstanceContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
