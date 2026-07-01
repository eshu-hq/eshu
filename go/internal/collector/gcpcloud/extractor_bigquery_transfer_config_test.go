// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const bigQueryTransferConfigFullName = "//bigquerydatatransfer.googleapis.com/projects/demo-project/locations/us/transferConfigs/abc123"

func bigQueryTransferConfigContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: bigQueryTransferConfigFullName,
		AssetType:        assetTypeBigQueryTransferConfig,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestBigQueryTransferConfigExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeBigQueryTransferConfig); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeBigQueryTransferConfig)
	}
}

func TestExtractBigQueryTransferConfigFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us/transferConfigs/abc123",
		"displayName": "nightly gcs load",
		"dataSourceId": "google_cloud_storage",
		"destinationDatasetId": "analytics",
		"schedule": "every 24 hours",
		"disabled": false,
		"state": "SUCCEEDED",
		"ownerInfo": {"email": "transfer-runner@demo-project.iam.gserviceaccount.com"},
		"notificationPubsubTopic": "projects/demo-project/topics/transfer-events",
		"encryptionConfiguration": {"kmsKeyName": "projects/demo-project/locations/us/keyRings/bq/cryptoKeys/transfer"},
		"params": {"data_path_template": "gs://secret-bucket/path/*", "query": "SELECT secret FROM t"}
	}`

	got, err := extractBigQueryTransferConfig(bigQueryTransferConfigContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"data_source_id":              "google_cloud_storage",
		"schedule":                    "every 24 hours",
		"state":                       "SUCCEEDED",
		"disabled":                    false,
		"customer_managed_encryption": true,
		"owner_email_fingerprint":     secretsiam.GCPServiceAccountEmailDigest("transfer-runner@demo-project.iam.gserviceaccount.com"),
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	assertRelationship(t, got.Relationships, relationshipTypeTransferConfigWritesToDataset,
		"//bigquery.googleapis.com/projects/demo-project/datasets/analytics", assetTypeBigQueryDataset)
	assertRelationship(t, got.Relationships, relationshipTypeTransferConfigNotifiesTopic,
		"//pubsub.googleapis.com/projects/demo-project/topics/transfer-events", assetTypePubSubTopic)
	assertRelationship(t, got.Relationships, relationshipTypeTransferConfigEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/bq/cryptoKeys/transfer", assetTypeKMSCryptoKey)
	if len(got.Relationships) != 3 {
		t.Fatalf("expected dataset + topic + kms edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}

	// Transfer params (user query text, GCS paths) must never leak.
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, token := range []string{"secret-bucket", "SELECT secret", "data_path_template", "params", "transfer-runner@demo-project"} {
		if containsString(string(blob), token) {
			t.Fatalf("transfer config extraction leaked token %q: %s", token, blob)
		}
	}
}

func TestExtractBigQueryTransferConfigDisabledScheduledQuery(t *testing.T) {
	const data = `{
		"dataSourceId": "scheduled_query",
		"destinationDatasetId": "reports",
		"disabled": true,
		"params": {"query": "SELECT 1"}
	}`
	got, err := extractBigQueryTransferConfig(bigQueryTransferConfigContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"data_source_id": "scheduled_query",
		"disabled":       true,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	assertRelationship(t, got.Relationships, relationshipTypeTransferConfigWritesToDataset,
		"//bigquery.googleapis.com/projects/demo-project/datasets/reports", assetTypeBigQueryDataset)
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the dataset edge, got %#v", got.Relationships)
	}
}

func TestExtractBigQueryTransferConfigEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractBigQueryTransferConfig(bigQueryTransferConfigContext(`{}`))
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

func TestExtractBigQueryTransferConfigMalformedDataErrors(t *testing.T) {
	if _, err := extractBigQueryTransferConfig(bigQueryTransferConfigContext(`{bad`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
	if _, err := extractBigQueryTransferConfig(bigQueryTransferConfigContext(``)); err == nil {
		t.Fatalf("expected an error for empty resource data")
	}
}
