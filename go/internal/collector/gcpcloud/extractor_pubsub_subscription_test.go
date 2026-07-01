// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const pubSubSubscriptionFullName = "//pubsub.googleapis.com/projects/demo-project/subscriptions/orders-worker"

func pubSubSubscriptionContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: pubSubSubscriptionFullName,
		AssetType:        assetTypePubSubSubscription,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestPubSubSubscriptionExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypePubSubSubscription); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypePubSubSubscription)
	}
}

func TestExtractPubSubSubscriptionPushWithDeadLetterAndBigQuery(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/subscriptions/orders-worker",
		"topic": "projects/demo-project/topics/orders",
		"labels": {"team": "platform"},
		"pushConfig": {"pushEndpoint": "https://ingest.example.com/_ah/push?token=SECRETTOKEN"},
		"ackDeadlineSeconds": 30,
		"retainAckedMessages": true,
		"messageRetentionDuration": "604800s",
		"expirationPolicy": {"ttl": "2678400s"},
		"deadLetterPolicy": {"deadLetterTopic": "projects/demo-project/topics/orders-dlq", "maxDeliveryAttempts": 5},
		"bigqueryConfig": {"table": "demo-project.analytics.orders_stream", "state": "ACTIVE"},
		"enableExactlyOnceDelivery": true,
		"filter": "attributes.region = \"us\"",
		"state": "ACTIVE"
	}`

	got, err := extractPubSubSubscription(pubSubSubscriptionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"state":                             "ACTIVE",
		"delivery_type":                     "push",
		"push_endpoint_scheme":              "https",
		"push_endpoint_host_fingerprint":    pubSubPushEndpointHostFingerprint("ingest.example.com"),
		"ack_deadline_seconds":              30,
		"retain_acked_messages":             true,
		"message_retention_duration":        "604800s",
		"expiration_ttl":                    "2678400s",
		"dead_letter_max_delivery_attempts": 5,
		"exactly_once_delivery":             true,
		"filter_present":                    true,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	// The raw push endpoint, its path, and its token must never leak.
	blob, _ := json.Marshal(got)
	for _, token := range []string{"SECRETTOKEN", "_ah/push", "ingest.example.com", "https://ingest"} {
		if containsString(string(blob), token) {
			t.Fatalf("subscription extraction leaked push-endpoint token %q: %s", token, blob)
		}
	}

	assertRelationship(t, got.Relationships, relationshipTypeSubscriptionSubscribesToTopic,
		"//pubsub.googleapis.com/projects/demo-project/topics/orders", assetTypePubSubTopic)
	assertRelationship(t, got.Relationships, relationshipTypeSubscriptionDeadLettersToTopic,
		"//pubsub.googleapis.com/projects/demo-project/topics/orders-dlq", assetTypePubSubTopic)
	assertRelationship(t, got.Relationships, relationshipTypeSubscriptionExportsToBigQueryTable,
		"//bigquery.googleapis.com/projects/demo-project/datasets/analytics/tables/orders_stream", assetTypeBigQueryTable)
	if len(got.Relationships) != 3 {
		t.Fatalf("expected topic + dead-letter + bigquery edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	rel := got.Relationships[0]
	if rel.SourceFullResourceName != pubSubSubscriptionFullName || rel.SourceAssetType != assetTypePubSubSubscription {
		t.Errorf("relationship source = %q/%q, want subscription identity", rel.SourceFullResourceName, rel.SourceAssetType)
	}
}

func TestExtractPubSubSubscriptionPullWithCloudStorage(t *testing.T) {
	const data = `{
		"topic": "projects/demo-project/topics/events",
		"cloudStorageConfig": {"bucket": "events-archive", "filenamePrefix": "raw/"},
		"state": "ACTIVE"
	}`
	got, err := extractPubSubSubscription(pubSubSubscriptionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["delivery_type"] != "cloud_storage" {
		t.Errorf("delivery_type = %v, want cloud_storage", got.Attributes["delivery_type"])
	}
	assertRelationship(t, got.Relationships, relationshipTypeSubscriptionExportsToStorageBucket,
		"//storage.googleapis.com/projects/_/buckets/events-archive", assetTypeStorageBucket)
	assertRelationship(t, got.Relationships, relationshipTypeSubscriptionSubscribesToTopic,
		"//pubsub.googleapis.com/projects/demo-project/topics/events", assetTypePubSubTopic)
}

func TestExtractPubSubSubscriptionBarePull(t *testing.T) {
	const data = `{"topic": "projects/demo-project/topics/plain", "state": "ACTIVE"}`
	got, err := extractPubSubSubscription(pubSubSubscriptionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{"state": "ACTIVE", "delivery_type": "pull"}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the topic edge, got %#v", got.Relationships)
	}
}

func TestExtractPubSubSubscriptionDeletedTopicEmitsNoEdge(t *testing.T) {
	const data = `{"topic": "_deleted-topic_", "state": "ACTIVE"}`
	got, err := extractPubSubSubscription(pubSubSubscriptionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("deleted-topic sentinel must emit no edge, got %#v", got.Relationships)
	}
}

func TestExtractPubSubSubscriptionHTTPPushEndpointSchemeIsCaptured(t *testing.T) {
	// A plain-http push endpoint is a posture worth alerting on; the scheme must
	// be captured while the host is only fingerprinted and the path/query dropped.
	const data = `{"topic": "projects/p/topics/t", "pushConfig": {"pushEndpoint": "http://legacy.internal/hook"}}`
	got, err := extractPubSubSubscription(pubSubSubscriptionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["push_endpoint_scheme"] != "http" {
		t.Errorf("push_endpoint_scheme = %v, want http", got.Attributes["push_endpoint_scheme"])
	}
	if got.Attributes["push_endpoint_host_fingerprint"] != pubSubPushEndpointHostFingerprint("legacy.internal") {
		t.Errorf("host fingerprint mismatch: %#v", got.Attributes["push_endpoint_host_fingerprint"])
	}
	blob, _ := json.Marshal(got)
	if containsString(string(blob), "legacy.internal") || containsString(string(blob), "/hook") {
		t.Fatalf("http push endpoint leaked host/path: %s", blob)
	}
}

func TestExtractPubSubSubscriptionEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractPubSubSubscription(pubSubSubscriptionContext(`{}`))
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

func TestExtractPubSubSubscriptionMalformedDataErrors(t *testing.T) {
	if _, err := extractPubSubSubscription(pubSubSubscriptionContext(`{bad`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
	if _, err := extractPubSubSubscription(pubSubSubscriptionContext(``)); err == nil {
		t.Fatalf("expected an error for empty resource data")
	}
}

func TestPubSubBigQueryConfigTableFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"dotted", "proj.ds.tbl", "//bigquery.googleapis.com/projects/proj/datasets/ds/tables/tbl"},
		{"colon project", "proj:ds.tbl", "//bigquery.googleapis.com/projects/proj/datasets/ds/tables/tbl"},
		{"too few parts", "ds.tbl", ""},
		{"too many parts", "a.b.c.d", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pubSubBigQueryConfigTableFullName(tc.in); got != tc.want {
				t.Errorf("pubSubBigQueryConfigTableFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPubSubPushEndpointHostFingerprintIsStableAndDeterministic(t *testing.T) {
	if pubSubPushEndpointHostFingerprint("") != "" {
		t.Errorf("blank host must fingerprint to empty")
	}
	a := pubSubPushEndpointHostFingerprint("Host.Example.COM")
	b := pubSubPushEndpointHostFingerprint("host.example.com")
	if a == "" || a != b {
		t.Errorf("fingerprint must be case-normalized and stable: %q vs %q", a, b)
	}
	want := "sha256:" + facts.StableID("GCPPubSubPushEndpointHost", map[string]any{"host": "host.example.com"})
	if a != want {
		t.Errorf("fingerprint = %q, want %q", a, want)
	}
}
