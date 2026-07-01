// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const cloudFunctionV1FullName = "//cloudfunctions.googleapis.com/projects/demo-project/locations/us-central1/functions/api-fn-v1"

func cloudFunctionV1Context(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: cloudFunctionV1FullName,
		AssetType:        assetTypeCloudFunctionV1,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestCloudFunctionV1ExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeCloudFunctionV1); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeCloudFunctionV1)
	}
}

func TestExtractCloudFunctionV1FullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/functions/api-fn-v1",
		"status": "ACTIVE",
		"runtime": "python39",
		"entryPoint": "handler",
		"availableMemoryMb": 256,
		"serviceAccountEmail": "runtime-sa@demo-project.iam.gserviceaccount.com",
		"vpcConnector": "serverless-conn",
		"vpcConnectorEgressSettings": "PRIVATE_RANGES_ONLY",
		"ingressSettings": "ALLOW_INTERNAL_ONLY",
		"sourceArchiveUrl": "gs://gcf-sources-v1/api-fn-v1-should-not-leak.zip",
		"eventTrigger": {"eventType": "google.pubsub.topic.publish", "resource": "projects/demo-project/topics/events", "service": "pubsub.googleapis.com"},
		"secretEnvironmentVariables": [{"key": "DB", "projectId": "demo-project", "secret": "db-password", "version": "latest"}],
		"secretVolumes": [{"mountPath": "/etc/secrets", "projectId": "demo-project", "secret": "app-config"}]
	}`

	got, err := extractCloudFunctionV1(cloudFunctionV1Context(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest("runtime-sa@demo-project.iam.gserviceaccount.com")
	if saDigest == "" {
		t.Fatalf("service account digest must be non-empty")
	}
	wantAttrs := map[string]any{
		"status":                      "ACTIVE",
		"runtime":                     "python39",
		"entry_point":                 "handler",
		"available_memory_mb":         256,
		"ingress_settings":            "ALLOW_INTERNAL_ONLY",
		"vpc_egress":                  "PRIVATE_RANGES_ONLY",
		"trigger_type":                "event",
		"event_type":                  "google.pubsub.topic.publish",
		"secret_mount_count":          2,
		"update_time":                 "",
		"service_account_fingerprint": saDigest,
	}
	// update_time is absent in this payload; drop the placeholder for comparison.
	delete(wantAttrs, "update_time")
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	const conn = "//vpcaccess.googleapis.com/projects/demo-project/locations/us-central1/connectors/serverless-conn"
	for _, want := range []string{
		saDigest, conn,
		"//secretmanager.googleapis.com/projects/demo-project/secrets/db-password",
		"//secretmanager.googleapis.com/projects/demo-project/secrets/app-config",
		"//pubsub.googleapis.com/projects/demo-project/topics/events",
		"//storage.googleapis.com/projects/_/buckets/gcf-sources-v1",
	} {
		if !containsStringSlice(got.CorrelationAnchors, want) {
			t.Errorf("missing anchor %q in %#v", want, got.CorrelationAnchors)
		}
	}

	if len(got.Relationships) != 5 {
		t.Fatalf("expected source-bucket + connector + 2 secret + topic edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeFunctionUsesVPCConnector, conn, assetTypeVPCAccessConnector)
	assertRelationship(t, got.Relationships, relationshipTypeFunctionSourceBucket,
		"//storage.googleapis.com/projects/_/buckets/gcf-sources-v1", assetTypeStorageBucket)
	assertRelationship(t, got.Relationships, relationshipTypeFunctionTriggeredByTopic,
		"//pubsub.googleapis.com/projects/demo-project/topics/events", assetTypePubSubTopic)
	assertRelationship(t, got.Relationships, relationshipTypeFunctionMountsSecret,
		"//secretmanager.googleapis.com/projects/demo-project/secrets/db-password", secretManagerSecretAssetType)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != cloudFunctionV1FullName {
			t.Errorf("relationship source = %q", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeCloudFunctionV1 {
			t.Errorf("relationship source asset type = %q", rel.SourceAssetType)
		}
	}
}

func TestExtractCloudFunctionV1HTTPSTrigger(t *testing.T) {
	const data = `{
		"status": "ACTIVE",
		"runtime": "nodejs18",
		"serviceAccountEmail": "http-sa@demo-project.iam.gserviceaccount.com",
		"httpsTrigger": {"url": "https://us-east1-demo-project.cloudfunctions.net/http-fn-should-not-leak"},
		"updateTime": "2023-01-15T09:30:00Z"
	}`
	got, err := extractCloudFunctionV1(cloudFunctionV1Context(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["trigger_type"] != "https" {
		t.Errorf("trigger_type = %v, want https", got.Attributes["trigger_type"])
	}
	if _, ok := got.Attributes["event_type"]; ok {
		t.Errorf("https trigger must not report an event_type: %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges for a bare https function, got %#v", got.Relationships)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"http-fn-should-not-leak", "cloudfunctions.net"} {
		if containsString(string(blob), token) {
			t.Fatalf("https trigger URL leaked token %q: %s", token, blob)
		}
	}
}

func TestExtractCloudFunctionV1DefaultServiceAccount(t *testing.T) {
	// A gen1 function with no explicit serviceAccountEmail runs as the default
	// {projectId}@appspot.gserviceaccount.com identity; that default must be
	// derived and fingerprinted so it correlates like the explicit case.
	const data = `{"status": "ACTIVE", "runtime": "go121"}`
	got, err := extractCloudFunctionV1(cloudFunctionV1Context(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantDigest := secretsiam.GCPServiceAccountEmailDigest("demo-project@appspot.gserviceaccount.com")
	if wantDigest == "" {
		t.Fatalf("default SA digest must be non-empty")
	}
	if got.Attributes["service_account_fingerprint"] != wantDigest {
		t.Errorf("service_account_fingerprint = %v, want default-SA digest", got.Attributes["service_account_fingerprint"])
	}
	if !containsStringSlice(got.CorrelationAnchors, wantDigest) {
		t.Errorf("expected default-SA digest anchor in %#v", got.CorrelationAnchors)
	}
}

func TestExtractCloudFunctionV1EmptyDataYieldsNothing(t *testing.T) {
	got, err := extractCloudFunctionV1(cloudFunctionV1Context(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 || len(got.Relationships) != 0 || len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected empty extraction, got %#v", got)
	}
}

func TestExtractCloudFunctionV1MalformedDataErrors(t *testing.T) {
	if _, err := extractCloudFunctionV1(cloudFunctionV1Context(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
