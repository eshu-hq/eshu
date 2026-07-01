// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const eventarcTriggerFullName = "//eventarc.googleapis.com/projects/demo-project/locations/us-central1/triggers/run-trigger"

func eventarcTriggerContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: eventarcTriggerFullName,
		AssetType:        assetTypeEventarcTrigger,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestEventarcTriggerExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeEventarcTrigger); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeEventarcTrigger)
	}
}

func TestExtractEventarcTriggerCloudRun(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/triggers/run-trigger",
		"eventFilters": [
			{"attribute": "type", "value": "google.cloud.pubsub.topic.v1.messagePublished"},
			{"attribute": "topic", "value": "projects/demo-project/topics/events"}
		],
		"serviceAccount": "eventarc-sa@demo-project.iam.gserviceaccount.com",
		"destination": {"cloudRun": {"service": "api-service", "region": "us-central1", "path": "/events-should-not-leak"}},
		"transport": {"pubsub": {"topic": "projects/demo-project/topics/events", "subscription": "projects/demo-project/subscriptions/sub"}},
		"channel": "projects/demo-project/locations/us-central1/channels/my-channel",
		"createTime": "2024-06-01T00:00:00Z"
	}`

	got, err := extractEventarcTrigger(eventarcTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest("eventarc-sa@demo-project.iam.gserviceaccount.com")
	if saDigest == "" {
		t.Fatalf("service account digest must be non-empty")
	}
	wantAttrs := map[string]any{
		"event_type":                  "google.cloud.pubsub.topic.v1.messagePublished",
		"event_filter_count":          2,
		"destination_type":            "run",
		"transport_type":              "pubsub",
		"creation_time":               "2024-06-01T00:00:00Z",
		"service_account_fingerprint": saDigest,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	const service = "//run.googleapis.com/projects/demo-project/locations/us-central1/services/api-service"
	const topic = "//pubsub.googleapis.com/projects/demo-project/topics/events"
	const channel = "//eventarc.googleapis.com/projects/demo-project/locations/us-central1/channels/my-channel"
	for _, want := range []string{saDigest, service, topic, channel} {
		if !containsStringSlice(got.CorrelationAnchors, want) {
			t.Errorf("missing anchor %q in %#v", want, got.CorrelationAnchors)
		}
	}
	if len(got.Relationships) != 3 {
		t.Fatalf("expected service + topic + channel edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeTriggerTargetsService, service, assetTypeRunService)
	assertRelationship(t, got.Relationships, relationshipTypeTriggerTransportTopic, topic, assetTypePubSubTopic)
	assertRelationship(t, got.Relationships, relationshipTypeTriggerUsesChannel, channel, assetTypeEventarcChannel)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != eventarcTriggerFullName {
			t.Errorf("relationship source = %q", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeEventarcTrigger {
			t.Errorf("relationship source asset type = %q", rel.SourceAssetType)
		}
	}
	// The cloudRun path must not leak.
	blob, _ := json.Marshal(got)
	if containsString(string(blob), "events-should-not-leak") {
		t.Fatalf("cloudRun path leaked: %s", blob)
	}
}

func TestExtractEventarcTriggerCloudFunction(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-east1/triggers/fn-trigger",
		"eventFilters": [{"attribute": "type", "value": "google.cloud.storage.object.v1.finalized"}],
		"serviceAccount": "eventarc-sa@demo-project.iam.gserviceaccount.com",
		"destination": {"cloudFunction": "projects/demo-project/locations/us-east1/functions/my-fn"},
		"createTime": "2023-01-15T09:30:00Z"
	}`
	got, err := extractEventarcTrigger(eventarcTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["destination_type"] != "function" {
		t.Errorf("destination_type = %v, want function", got.Attributes["destination_type"])
	}
	assertRelationship(t, got.Relationships, relationshipTypeTriggerTargetsFunction,
		"//cloudfunctions.googleapis.com/projects/demo-project/locations/us-east1/functions/my-fn", assetTypeCloudFunction)
}

func TestExtractEventarcTriggerWorkflow(t *testing.T) {
	const data = `{"destination": {"workflow": "projects/demo-project/locations/us-central1/workflows/wf"}}`
	got, err := extractEventarcTrigger(eventarcTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["destination_type"] != "workflow" {
		t.Errorf("destination_type = %v, want workflow", got.Attributes["destination_type"])
	}
	assertRelationship(t, got.Relationships, relationshipTypeTriggerTargetsWorkflow,
		"//workflows.googleapis.com/projects/demo-project/locations/us-central1/workflows/wf", assetTypeWorkflow)
}

func TestExtractEventarcTriggerEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractEventarcTrigger(eventarcTriggerContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 || len(got.Relationships) != 0 || len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected empty extraction, got %#v", got)
	}
}

func TestExtractEventarcTriggerMalformedDataErrors(t *testing.T) {
	if _, err := extractEventarcTrigger(eventarcTriggerContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
