// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const cloudSchedulerJobFullName = "//cloudscheduler.googleapis.com/projects/demo-project/locations/us-central1/jobs/nightly"

func cloudSchedulerJobContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: cloudSchedulerJobFullName,
		AssetType:        assetTypeCloudSchedulerJob,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestCloudSchedulerJobExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeCloudSchedulerJob); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeCloudSchedulerJob)
	}
}

func TestExtractCloudSchedulerJobPubsubTarget(t *testing.T) {
	const data = `{
		"schedule": "0 2 * * *",
		"timeZone": "America/New_York",
		"state": "ENABLED",
		"pubsubTarget": {"topicName": "projects/demo-project/topics/scheduler-events", "data": "c2VjcmV0LXNob3VsZC1ub3QtbGVhaw==", "attributes": {"origin": "scheduler"}},
		"retryConfig": {"retryCount": 3},
		"lastAttemptTime": "2026-06-26T02:00:00Z"
	}`

	got, err := extractCloudSchedulerJob(cloudSchedulerJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"schedule":          "0 2 * * *",
		"time_zone":         "America/New_York",
		"state":             "ENABLED",
		"target_type":       "pubsub",
		"retry_count":       3,
		"last_attempt_time": "2026-06-26T02:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	const topic = "//pubsub.googleapis.com/projects/demo-project/topics/scheduler-events"
	if !containsStringSlice(got.CorrelationAnchors, topic) {
		t.Errorf("missing topic anchor %q in %#v", topic, got.CorrelationAnchors)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly the topic edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSchedulerJobTargetsTopic, topic, assetTypePubSubTopic)
	blob, _ := json.Marshal(got)
	if containsString(string(blob), "c2VjcmV0LXNob3VsZC1ub3QtbGVhaw==") {
		t.Fatalf("pubsub payload leaked: %s", blob)
	}
}

func TestExtractCloudSchedulerJobHTTPTarget(t *testing.T) {
	const data = `{
		"schedule": "*/15 * * * *",
		"state": "PAUSED",
		"httpTarget": {
			"uri": "https://api.internal.example.com/hook?token=should-not-leak-token",
			"httpMethod": "POST",
			"oidcToken": {"serviceAccountEmail": "scheduler-sa@demo-project.iam.gserviceaccount.com", "audience": "https://api.internal.example.com/should-not-leak-audience"},
			"headers": {"X-Api-Key": "should-not-leak-header"}
		}
	}`
	got, err := extractCloudSchedulerJob(cloudSchedulerJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	saDigest := secretsiam.GCPServiceAccountEmailDigest("scheduler-sa@demo-project.iam.gserviceaccount.com")
	if got.Attributes["target_type"] != "http" {
		t.Errorf("target_type = %v, want http", got.Attributes["target_type"])
	}
	if got.Attributes["http_method"] != "POST" {
		t.Errorf("http_method = %v, want POST", got.Attributes["http_method"])
	}
	if got.Attributes["service_account_fingerprint"] != saDigest {
		t.Errorf("service_account_fingerprint = %v, want SA digest", got.Attributes["service_account_fingerprint"])
	}
	if _, ok := got.Attributes["http_target_host_fingerprint"]; !ok {
		t.Errorf("expected http_target_host_fingerprint attribute: %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("http target emits no edge, got %#v", got.Relationships)
	}
	blob, _ := json.Marshal(got)
	for _, token := range []string{"should-not-leak-token", "should-not-leak-audience", "should-not-leak-header", "api.internal.example.com", "scheduler-sa@demo-project"} {
		if containsString(string(blob), token) {
			t.Fatalf("http target leaked token %q: %s", token, blob)
		}
	}
}

func TestExtractCloudSchedulerJobAppEngineTarget(t *testing.T) {
	// An App Engine target also carries httpMethod; it must be captured, and the
	// target resolves no CAI-asset edge.
	const data = `{"state": "ENABLED", "appEngineHttpTarget": {"httpMethod": "PUT", "relativeUri": "/task"}}`
	got, err := extractCloudSchedulerJob(cloudSchedulerJobContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["target_type"] != "app_engine" {
		t.Errorf("target_type = %v, want app_engine", got.Attributes["target_type"])
	}
	if got.Attributes["http_method"] != "PUT" {
		t.Errorf("http_method = %v, want PUT", got.Attributes["http_method"])
	}
	if len(got.Relationships) != 0 {
		t.Errorf("app engine target emits no edge, got %#v", got.Relationships)
	}
}

func TestExtractCloudSchedulerJobEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractCloudSchedulerJob(cloudSchedulerJobContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 || len(got.Relationships) != 0 || len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected empty extraction, got %#v", got)
	}
}

func TestExtractCloudSchedulerJobMalformedDataErrors(t *testing.T) {
	if _, err := extractCloudSchedulerJob(cloudSchedulerJobContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
