// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const cloudTasksQueueFullName = "//cloudtasks.googleapis.com/projects/demo-project/locations/us-central1/queues/tasks-q"

func cloudTasksQueueContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: cloudTasksQueueFullName,
		AssetType:        assetTypeCloudTasksQueue,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestCloudTasksQueueExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeCloudTasksQueue); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeCloudTasksQueue)
	}
}

func TestExtractCloudTasksQueueAppEngineRouting(t *testing.T) {
	const data = `{
		"state": "RUNNING",
		"rateLimits": {"maxDispatchesPerSecond": 500, "maxConcurrentDispatches": 1000, "maxBurstSize": 100},
		"retryConfig": {"maxAttempts": 5, "maxRetryDuration": "3600s"},
		"appEngineRoutingOverride": {"service": "worker", "version": "v2"},
		"purgeTime": "2026-06-20T00:00:00Z"
	}`

	got, err := extractCloudTasksQueue(cloudTasksQueueContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"state":                      "RUNNING",
		"max_dispatches_per_second":  float64(500),
		"max_concurrent_dispatches":  1000,
		"max_burst_size":             100,
		"retry_max_attempts":         5,
		"app_engine_routing_service": "worker",
		"purge_time":                 "2026-06-20T00:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	// The App Engine routing override is kept as a bounded attribute only. A CAI
	// Queue full resource name carries the numeric project number, which does not
	// match the App Engine application id used in an App Engine Service full
	// resource name, so no resolvable edge can be built and none is emitted.
	if len(got.Relationships) != 0 {
		t.Fatalf("cloud tasks queue emits no typed edge, got %#v", got.Relationships)
	}
	const svc = "//appengine.googleapis.com/apps/demo-project/services/worker"
	if containsStringSlice(got.CorrelationAnchors, svc) {
		t.Errorf("unresolvable app engine service name must not be an anchor: %#v", got.CorrelationAnchors)
	}
}

func TestExtractCloudTasksQueueHTTPTarget(t *testing.T) {
	const data = `{
		"state": "PAUSED",
		"httpTarget": {
			"uriOverride": {"host": "api.internal.example.com", "scheme": "HTTPS", "pathOverride": {"path": "/should-not-leak"}},
			"oidcToken": {"serviceAccountEmail": "tasks-sa@demo-project.iam.gserviceaccount.com", "audience": "https://should-not-leak-audience"}
		}
	}`
	got, err := extractCloudTasksQueue(cloudTasksQueueContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	saDigest := secretsiam.GCPServiceAccountEmailDigest("tasks-sa@demo-project.iam.gserviceaccount.com")
	if got.Attributes["service_account_fingerprint"] != saDigest {
		t.Errorf("service_account_fingerprint = %v, want SA digest", got.Attributes["service_account_fingerprint"])
	}
	if _, ok := got.Attributes["http_target_host_fingerprint"]; !ok {
		t.Errorf("expected http_target_host_fingerprint: %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("http target queue emits no edge, got %#v", got.Relationships)
	}
	blob, _ := json.Marshal(got)
	for _, token := range []string{"api.internal.example.com", "should-not-leak", "should-not-leak-audience", "tasks-sa@demo-project"} {
		if containsString(string(blob), token) {
			t.Fatalf("http target leaked token %q: %s", token, blob)
		}
	}
}

func TestExtractCloudTasksQueueHostPortStrippedBeforeFingerprint(t *testing.T) {
	// A "host:port" URI override must fingerprint identically to the bare host so
	// the same endpoint correlates across extractors regardless of an explicit
	// port, matching the hostname-only reduction other extractors apply.
	withPort, err := extractCloudTasksQueue(cloudTasksQueueContext(
		`{"httpTarget": {"uriOverride": {"host": "api.internal.example.com:443"}}}`,
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bare, err := extractCloudTasksQueue(cloudTasksQueueContext(
		`{"httpTarget": {"uriOverride": {"host": "api.internal.example.com"}}}`,
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := withPort.Attributes["http_target_host_fingerprint"]
	want := bare.Attributes["http_target_host_fingerprint"]
	if got == nil || got != want {
		t.Errorf("port must be stripped before fingerprinting: with-port %v != bare %v", got, want)
	}
}

func TestExtractCloudTasksQueueEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractCloudTasksQueue(cloudTasksQueueContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 || len(got.Relationships) != 0 || len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected empty extraction, got %#v", got)
	}
}

func TestExtractCloudTasksQueueMalformedDataErrors(t *testing.T) {
	if _, err := extractCloudTasksQueue(cloudTasksQueueContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
