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

// TestBigQueryTransferConfigDestinationDatasetProjectBound locks the verified
// cross-project resolution bound for #4469. BigQuery Data Transfer's CAI/DTS
// resource exposes destinationDatasetId only as a bare dataset id — no
// project-qualified destination field exists anywhere in the TransferConfig
// schema (verified against the live datatransfer v1 discovery document and the
// googleapis transfer.proto). A cross-project transfer config is created inside
// its destination project (its own name/project IS the destination project by
// GCP's resource model), so resolving the destination dataset against the
// config's own project (ctx.ProjectID) is correct for both same-project and
// "cross-project-looking" configs. This test guards that bound so a future
// change cannot silently re-point the edge at a fabricated project.
func TestBigQueryTransferConfigDestinationDatasetProjectBound(t *testing.T) {
	tests := []struct {
		name         string
		dataSourceID string
		params       string
		destination  string
		wantDataset  string
	}{
		{
			// A plain (non-copy) scheduled query writing into a dataset in the
			// config's own project: the common same-project shape.
			name:         "same project destination",
			dataSourceID: "scheduled_query",
			params:       `{"query": "SELECT 1"}`,
			destination:  "analytics",
			wantDataset:  "//bigquery.googleapis.com/projects/demo-project/datasets/analytics",
		},
		{
			// A cross-region/cross-project copy config still surfaces only a
			// bare destinationDatasetId; the destination project is the config's
			// own project, never the source project parsed out of params.
			name:         "cross-project copy resolves to config's own project",
			dataSourceID: "cross_region_copy",
			params:       `{"source_project_id": "other-source-project", "source_dataset_id": "warehouse"}`,
			destination:  "warehouse",
			wantDataset:  "//bigquery.googleapis.com/projects/demo-project/datasets/warehouse",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := `{
				"dataSourceId": "` + tc.dataSourceID + `",
				"destinationDatasetId": "` + tc.destination + `",
				"params": ` + tc.params + `
			}`
			got, err := extractBigQueryTransferConfig(bigQueryTransferConfigContext(data))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertRelationship(t, got.Relationships, relationshipTypeTransferConfigWritesToDataset,
				tc.wantDataset, assetTypeBigQueryDataset)
			if len(got.Relationships) != 1 {
				t.Fatalf("expected only the dataset edge, got %#v", got.Relationships)
			}
			// A copy config's source project is a params value and must never
			// leak into a fact or become the destination project.
			blob, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal extraction: %v", err)
			}
			if containsString(string(blob), "other-source-project") {
				t.Fatalf("params source project leaked into extraction: %s", blob)
			}
		})
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
