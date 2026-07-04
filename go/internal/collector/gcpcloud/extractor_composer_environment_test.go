// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const composerEnvironmentFullName = "//composer.googleapis.com/projects/demo-project/locations/us-central1/environments/prod"

func composerEnvironmentContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: composerEnvironmentFullName,
		AssetType:        assetTypeComposerEnvironment,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestComposerEnvironmentExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComposerEnvironment); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComposerEnvironment)
	}
}

func TestExtractComposerEnvironmentFullConfig(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/environments/prod",
		"state": "RUNNING",
		"createTime": "2026-01-15T10:00:00Z",
		"config": {
			"gkeCluster": "projects/demo-project/locations/us-central1/clusters/us-central1-prod-abcd1234-gke",
			"dagGcsPrefix": "gs://us-central1-prod-abcd1234-bucket/dags",
			"environmentSize": "ENVIRONMENT_SIZE_MEDIUM",
			"resilienceMode": "HIGH_RESILIENCE",
			"nodeConfig": {
				"network": "projects/demo-project/global/networks/vpc-main",
				"subnetwork": "projects/demo-project/regions/us-central1/subnetworks/sub-main",
				"serviceAccount": "composer-runtime@demo-project.iam.gserviceaccount.com"
			},
			"softwareConfig": {
				"imageVersion": "composer-2.9.6-airflow-2.9.3"
			},
			"encryptionConfig": {
				"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/composer-ring/cryptoKeys/composer-key"
			},
			"privateEnvironmentConfig": {
				"enablePrivateEnvironment": true,
				"privateClusterConfig": {"enablePrivateEndpoint": true},
				"networkingConfig": {"connectionType": "PRIVATE_SERVICE_CONNECT"}
			},
			"workloadsConfig": {}
		}
	}`

	got, err := extractComposerEnvironment(composerEnvironmentContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantSAFingerprint := secretsiam.GCPServiceAccountEmailDigest("composer-runtime@demo-project.iam.gserviceaccount.com")

	wantAttrs := map[string]any{
		"state":                       "RUNNING",
		"creation_time":               "2026-01-15T10:00:00Z",
		"environment_size":            "ENVIRONMENT_SIZE_MEDIUM",
		"resilience_mode":             "HIGH_RESILIENCE",
		"image_version":               "composer-2.9.6-airflow-2.9.3",
		"customer_managed_encryption": true,
		"private_environment_enabled": true,
		"private_endpoint_enabled":    true,
		"networking_connection_type":  "PRIVATE_SERVICE_CONNECT",
		"service_account_fingerprint": wantSAFingerprint,
		"workloads_config_present":    true,
	}
	if diff := diffAttrs(got.Attributes, wantAttrs); diff != "" {
		t.Fatalf("attributes mismatch: %s\n got %#v\nwant %#v", diff, got.Attributes, wantAttrs)
	}

	wantAnchors := []string{
		"//container.googleapis.com/projects/demo-project/locations/us-central1/clusters/us-central1-prod-abcd1234-gke",
		"//compute.googleapis.com/projects/demo-project/global/networks/vpc-main",
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/sub-main",
		wantSAFingerprint,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/composer-ring/cryptoKeys/composer-key",
		"//storage.googleapis.com/projects/_/buckets/us-central1-prod-abcd1234-bucket",
	}
	if diff := diffStringSlices(got.CorrelationAnchors, wantAnchors); diff != "" {
		t.Fatalf("anchors mismatch: %s\n got %#v\nwant %#v", diff, got.CorrelationAnchors, wantAnchors)
	}

	assertRelationship(t, got.Relationships, relationshipTypeComposerEnvironmentUsesGKECluster,
		"//container.googleapis.com/projects/demo-project/locations/us-central1/clusters/us-central1-prod-abcd1234-gke", assetTypeGKECluster)
	assertRelationship(t, got.Relationships, relationshipTypeComposerEnvironmentUsesDAGBucket,
		"//storage.googleapis.com/projects/_/buckets/us-central1-prod-abcd1234-bucket", assetTypeStorageBucket)
	assertRelationship(t, got.Relationships, relationshipTypeComposerEnvironmentUsesNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/vpc-main", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeComposerEnvironmentUsesSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/sub-main", assetTypeComputeSubnetwork)
	assertRelationship(t, got.Relationships, relationshipTypeComposerEnvironmentEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/composer-ring/cryptoKeys/composer-key", assetTypeKMSCryptoKey)
	if len(got.Relationships) != 5 {
		t.Fatalf("expected 5 relationships, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != composerEnvironmentFullName {
			t.Errorf("relationship source = %q, want environment full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeComposerEnvironment {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeComposerEnvironment)
		}
	}
}

func TestExtractComposerEnvironmentBareGKEClusterAndDagPrefixFallbackToStorageConfig(t *testing.T) {
	// Composer 3 environments carry storageConfig.bucket (no gs:// prefix)
	// instead of config.dagGcsPrefix.
	const data = `{
		"state": "RUNNING",
		"config": {
			"gkeCluster": "projects/demo-project/locations/us-central1/clusters/composer-cluster"
		},
		"storageConfig": {"bucket": "composer3-bucket"}
	}`
	got, err := extractComposerEnvironment(composerEnvironmentContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeComposerEnvironmentUsesDAGBucket,
		"//storage.googleapis.com/projects/_/buckets/composer3-bucket", assetTypeStorageBucket)
}

func TestExtractComposerEnvironmentDagGcsPrefixTakesPrecedenceOverStorageConfig(t *testing.T) {
	const data = `{
		"config": {"dagGcsPrefix": "gs://dag-bucket/dags"},
		"storageConfig": {"bucket": "other-bucket"}
	}`
	got, err := extractComposerEnvironment(composerEnvironmentContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeComposerEnvironmentUsesDAGBucket,
		"//storage.googleapis.com/projects/_/buckets/dag-bucket", assetTypeStorageBucket)
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeComposerEnvironmentUsesDAGBucket && rel.TargetFullResourceName == "//storage.googleapis.com/projects/_/buckets/other-bucket" {
			t.Fatalf("expected dagGcsPrefix to take precedence over storageConfig.bucket")
		}
	}
}

func TestExtractComposerEnvironmentDefaultServiceAccountSentinelNotFingerprinted(t *testing.T) {
	const data = `{"config": {"nodeConfig": {"serviceAccount": "default"}}}`
	got, err := extractComposerEnvironment(composerEnvironmentContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, has := got.Attributes["service_account_fingerprint"]; has {
		t.Errorf("expected no service_account_fingerprint for the default sentinel, got %#v", got.Attributes)
	}
	for _, a := range got.CorrelationAnchors {
		if a == "default" {
			t.Fatalf("expected the default sentinel never to be anchored, got %#v", got.CorrelationAnchors)
		}
	}
}

func TestExtractComposerEnvironmentNoLeakageOfSensitiveValues(t *testing.T) {
	const data = `{
		"config": {
			"nodeConfig": {"serviceAccount": "svc@demo-project.iam.gserviceaccount.com"},
			"privateEnvironmentConfig": {
				"privateClusterConfig": {"masterIpv4CidrBlock": "172.16.0.0/23"}
			},
			"softwareConfig": {
				"airflowConfigOverrides": {"core-dags_are_paused_at_creation": "True"},
				"envVariables": {"MY_SECRET": "shhh"}
			}
		}
	}`
	got, err := extractComposerEnvironment(composerEnvironmentContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"172.16.0.0/23", "shhh", "MY_SECRET", "svc@demo-project.iam.gserviceaccount.com"} {
		if containsString(string(blob), banned) {
			t.Fatalf("extraction leaked sensitive token %q: %s", banned, blob)
		}
	}
}

func TestExtractComposerEnvironmentEmptyDataYieldsNoAttributesOrEdges(t *testing.T) {
	got, err := extractComposerEnvironment(composerEnvironmentContext(`{}`))
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

func TestExtractComposerEnvironmentMalformedDataErrors(t *testing.T) {
	_, err := extractComposerEnvironment(composerEnvironmentContext(`{not json`))
	if err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractComposerEnvironmentPrivateEnvironmentFalseIsPreserved(t *testing.T) {
	// An explicit false must be emitted, not treated as "absent" (the field
	// is a *bool internally so false and absent are distinguishable).
	const data = `{"config": {"privateEnvironmentConfig": {"enablePrivateEnvironment": false}}}`
	got, err := extractComposerEnvironment(composerEnvironmentContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, has := got.Attributes["private_environment_enabled"]; !has || v != false {
		t.Errorf("private_environment_enabled = %#v (present=%v), want false (present=true)", v, has)
	}
}
