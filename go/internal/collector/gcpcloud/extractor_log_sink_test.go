// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const logSinkFullName = "//logging.googleapis.com/projects/demo-project/sinks/audit-export"

func logSinkContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: logSinkFullName,
		AssetType:        logSinkAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestLogSinkExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(logSinkAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", logSinkAssetType)
	}
}

func TestExtractLogSinkBigQueryDestination(t *testing.T) {
	const email = "p1234-567@gcp-sa-logging.iam.gserviceaccount.com"
	const data = `{
		"name": "projects/demo-project/sinks/audit-export",
		"destination": "bigquery.googleapis.com/projects/demo-project/datasets/audit",
		"filter": "logName:cloudaudit.googleapis.com",
		"writerIdentity": "serviceAccount:p1234-567@gcp-sa-logging.iam.gserviceaccount.com",
		"disabled": false,
		"exclusions": [{"name": "drop-debug", "filter": "severity<WARNING"}],
		"createTime": "2024-06-01T00:00:00Z"
	}`
	got, err := extractLogSink(logSinkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	digest := secretsiam.GCPServiceAccountEmailDigest(email)
	wantAttrs := map[string]any{
		"destination_type":                  "bigquery",
		"filter_present":                    true,
		"disabled":                          false,
		"exclusion_count":                   1,
		"creation_time":                     "2024-06-01T00:00:00Z",
		"writer_identity_email_fingerprint": digest,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 destination edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeLogSinkExportsToDataset,
		"//bigquery.googleapis.com/projects/demo-project/datasets/audit", assetTypeBigQueryDataset)
	// anchors: destination + writer identity digest.
	if !reflect.DeepEqual(got.CorrelationAnchors, []string{"//bigquery.googleapis.com/projects/demo-project/datasets/audit", digest}) {
		t.Fatalf("anchors mismatch: %#v", got.CorrelationAnchors)
	}
}

func TestExtractLogSinkDestinationTypes(t *testing.T) {
	cases := []struct {
		name       string
		dest       string
		wantType   string
		wantTarget string
		wantAsset  string
		wantRel    string
	}{
		{"storage", "storage.googleapis.com/my-log-bucket", "storage", "//storage.googleapis.com/projects/_/buckets/my-log-bucket", assetTypeStorageBucket, relationshipTypeLogSinkExportsToBucket},
		{"pubsub", "pubsub.googleapis.com/projects/p/topics/logs", "pubsub", "//pubsub.googleapis.com/projects/p/topics/logs", assetTypePubSubTopic, relationshipTypeLogSinkExportsToTopic},
		{"logbucket", "logging.googleapis.com/projects/p/locations/global/buckets/_Default", "logging", "//logging.googleapis.com/projects/p/locations/global/buckets/_Default", logBucketAssetType, relationshipTypeLogSinkExportsToLogBucket},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := `{"destination": "` + tc.dest + `"}`
			got, err := extractLogSink(logSinkContext(data))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Attributes["destination_type"] != tc.wantType {
				t.Errorf("destination_type = %v, want %v", got.Attributes["destination_type"], tc.wantType)
			}
			assertRelationship(t, got.Relationships, tc.wantRel, tc.wantTarget, tc.wantAsset)
		})
	}
}

func TestExtractLogSinkNeverPersistsWriterEmailOrFilter(t *testing.T) {
	const data = `{
		"destination": "storage.googleapis.com/b",
		"filter": "resource.labels.project_id=\"internal-secret-project\"",
		"writerIdentity": "serviceAccount:secret-writer@internal.iam.gserviceaccount.com"
	}`
	got, err := extractLogSink(logSinkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"internal-secret-project", "secret-writer@internal", "writerIdentity"} {
		if containsString(string(blob), token) {
			t.Fatalf("log sink extraction leaked sensitive token %q: %s", token, blob)
		}
	}
	if got.Attributes["filter_present"] != true {
		t.Errorf("filter_present should be true (presence only): %#v", got.Attributes)
	}
}

func TestExtractLogSinkUnknownDestinationNoEdge(t *testing.T) {
	const data = `{"destination": "example.com/some/thing"}`
	got, err := extractLogSink(logSinkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["destination_type"]; ok {
		t.Errorf("unknown destination service must omit destination_type: %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("unknown destination must emit no edge, got %#v", got.Relationships)
	}
}

func TestExtractLogSinkEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractLogSink(logSinkContext(`{}`))
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

func TestExtractLogSinkMalformedDataErrors(t *testing.T) {
	if _, err := extractLogSink(logSinkContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
