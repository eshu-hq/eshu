// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const cloudFunctionFullName = "//cloudfunctions.googleapis.com/projects/demo-project/locations/us-central1/functions/api-fn"

func cloudFunctionContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: cloudFunctionFullName,
		AssetType:        assetTypeCloudFunction,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestCloudFunctionExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeCloudFunction); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeCloudFunction)
	}
}

func TestExtractCloudFunctionGen2FullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/functions/api-fn",
		"environment": "GEN_2",
		"state": "ACTIVE",
		"updateTime": "2026-06-26T12:00:00Z",
		"buildConfig": {
			"runtime": "nodejs20",
			"dockerRepository": "projects/demo-project/locations/us-central1/repositories/gcf-artifacts",
			"source": {"storageSource": {"bucket": "gcf-sources-123", "object": "api-fn-source.zip"}}
		},
		"serviceConfig": {
			"serviceAccountEmail": "runtime-sa@demo-project.iam.gserviceaccount.com",
			"vpcConnector": "projects/demo-project/locations/us-central1/connectors/serverless-conn",
			"vpcConnectorEgressSettings": "PRIVATE_RANGES_ONLY",
			"ingressSettings": "ALLOW_INTERNAL_ONLY",
			"secretEnvironmentVariables": [{"key": "DB_PASSWORD", "projectId": "demo-project", "secret": "db-password", "version": "latest"}],
			"secretVolumes": [{"mountPath": "/etc/secrets", "projectId": "demo-project", "secret": "app-config"}]
		},
		"eventTrigger": {
			"eventType": "google.cloud.pubsub.topic.v1.messagePublished",
			"pubsubTopic": "projects/demo-project/topics/events",
			"serviceAccountEmail": "trigger-sa@demo-project.iam.gserviceaccount.com"
		}
	}`

	got, err := extractCloudFunction(cloudFunctionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	runtimeDigest := secretsiam.GCPServiceAccountEmailDigest("runtime-sa@demo-project.iam.gserviceaccount.com")
	triggerDigest := secretsiam.GCPServiceAccountEmailDigest("trigger-sa@demo-project.iam.gserviceaccount.com")
	if runtimeDigest == "" || triggerDigest == "" {
		t.Fatalf("service account digests must be non-empty")
	}
	wantAttrs := map[string]any{
		"environment":                 "GEN_2",
		"state":                       "ACTIVE",
		"runtime":                     "nodejs20",
		"ingress_settings":            "ALLOW_INTERNAL_ONLY",
		"vpc_egress":                  "PRIVATE_RANGES_ONLY",
		"event_type":                  "google.cloud.pubsub.topic.v1.messagePublished",
		"secret_mount_count":          2,
		"update_time":                 "2026-06-26T12:00:00Z",
		"service_account_fingerprint": runtimeDigest,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	for _, want := range []string{
		runtimeDigest, triggerDigest,
		"//vpcaccess.googleapis.com/projects/demo-project/locations/us-central1/connectors/serverless-conn",
		"//secretmanager.googleapis.com/projects/demo-project/secrets/db-password",
		"//secretmanager.googleapis.com/projects/demo-project/secrets/app-config",
		"//pubsub.googleapis.com/projects/demo-project/topics/events",
		"//storage.googleapis.com/projects/_/buckets/gcf-sources-123",
	} {
		if !containsStringSlice(got.CorrelationAnchors, want) {
			t.Errorf("missing anchor %q in %#v", want, got.CorrelationAnchors)
		}
	}

	if len(got.Relationships) != 5 {
		t.Fatalf("expected connector + 2 secret + topic + source-bucket edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeFunctionUsesVPCConnector,
		"//vpcaccess.googleapis.com/projects/demo-project/locations/us-central1/connectors/serverless-conn", assetTypeVPCAccessConnector)
	assertRelationship(t, got.Relationships, relationshipTypeFunctionMountsSecret,
		"//secretmanager.googleapis.com/projects/demo-project/secrets/db-password", secretManagerSecretAssetType)
	assertRelationship(t, got.Relationships, relationshipTypeFunctionMountsSecret,
		"//secretmanager.googleapis.com/projects/demo-project/secrets/app-config", secretManagerSecretAssetType)
	assertRelationship(t, got.Relationships, relationshipTypeFunctionTriggeredByTopic,
		"//pubsub.googleapis.com/projects/demo-project/topics/events", assetTypePubSubTopic)
	assertRelationship(t, got.Relationships, relationshipTypeFunctionSourceBucket,
		"//storage.googleapis.com/projects/_/buckets/gcf-sources-123", assetTypeStorageBucket)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != cloudFunctionFullName {
			t.Errorf("relationship source = %q, want function full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeCloudFunction {
			t.Errorf("relationship source asset type = %q", rel.SourceAssetType)
		}
	}
}

func TestExtractCloudFunctionGen1(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-east1/functions/legacy-fn",
		"environment": "GEN_1",
		"status": "ACTIVE",
		"runtime": "python39",
		"serviceAccountEmail": "legacy-sa@demo-project.iam.gserviceaccount.com",
		"ingressSettings": "ALLOW_ALL",
		"sourceArchiveUrl": "gs://legacy-src/functions/legacy-fn-should-not-leak.zip",
		"httpsTrigger": {"url": "https://us-east1-demo-project.cloudfunctions.net/legacy-fn-should-not-leak"},
		"updateTime": "2023-01-15T09:30:00Z"
	}`
	got, err := extractCloudFunction(cloudFunctionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	legacyDigest := secretsiam.GCPServiceAccountEmailDigest("legacy-sa@demo-project.iam.gserviceaccount.com")
	wantAttrs := map[string]any{
		"environment":                 "GEN_1",
		"state":                       "ACTIVE",
		"runtime":                     "python39",
		"ingress_settings":            "ALLOW_ALL",
		"update_time":                 "2023-01-15T09:30:00Z",
		"service_account_fingerprint": legacyDigest,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	// Only the source-archive bucket edge; the object path and https URL never leak.
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the source-bucket edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeFunctionSourceBucket,
		"//storage.googleapis.com/projects/_/buckets/legacy-src", assetTypeStorageBucket)

	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"legacy-fn-should-not-leak", "cloudfunctions.net", "functions/legacy-fn-should-not-leak.zip"} {
		if containsString(string(blob), token) {
			t.Fatalf("gen1 extraction leaked forbidden token %q: %s", token, blob)
		}
	}
}

func TestExtractCloudFunctionEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractCloudFunction(cloudFunctionContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractCloudFunctionMalformedDataErrors(t *testing.T) {
	if _, err := extractCloudFunction(cloudFunctionContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestCloudFunctionSourceBucketName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"gs url with object", "gs://my-bucket/path/obj.zip", "my-bucket"},
		{"gs url bucket only", "gs://my-bucket", "my-bucket"},
		{"gs url trailing slash", "gs://my-bucket/", "my-bucket"},
		{"not gs", "https://example.com/x", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cloudFunctionArchiveBucket(tc.in); got != tc.want {
				t.Errorf("cloudFunctionArchiveBucket(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
