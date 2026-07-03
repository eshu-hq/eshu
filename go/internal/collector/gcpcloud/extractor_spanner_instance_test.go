// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const spannerInstanceFullName = "//spanner.googleapis.com/projects/demo-project/instances/prod-instance"

func spannerInstanceContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: spannerInstanceFullName,
		AssetType:        assetTypeSpannerInstance,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestSpannerInstanceExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeSpannerInstance); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeSpannerInstance)
	}
}

func TestExtractSpannerInstanceFullAttributesRegional(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/instances/prod-instance",
		"config": "projects/demo-project/instanceConfigs/regional-us-central1",
		"displayName": "Production Instance",
		"nodeCount": 3,
		"state": "READY",
		"labels": {"env": "prod"}
	}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"config":       "regional-us-central1",
		"display_name": "Production Instance",
		"node_count":   int64(3),
		"state":        "READY",
		"label_count":  1,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for an instance with no config edge target, got %#v", got.Relationships)
	}
}

func TestExtractSpannerInstanceProcessingUnitsOnly(t *testing.T) {
	// A Spanner instance is provisioned by exactly one of nodeCount or
	// processingUnits (processingUnits = nodeCount * 1000); both must be
	// captured independently since neither implies the other's presence.
	const data = `{
		"name": "projects/demo-project/instances/pu-instance",
		"config": "projects/demo-project/instanceConfigs/nam3",
		"processingUnits": 500,
		"state": "READY"
	}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["node_count"]; ok {
		t.Errorf("node_count should be omitted when absent, got %#v", got.Attributes)
	}
	if got.Attributes["processing_units"] != int64(500) {
		t.Errorf("processing_units = %v, want 500", got.Attributes["processing_units"])
	}
}

func TestExtractSpannerInstanceMultiRegionConfig(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/instances/multi-region",
		"config": "projects/demo-project/instanceConfigs/nam-eur-asia1",
		"nodeCount": 1,
		"state": "READY"
	}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["config"] != "nam-eur-asia1" {
		t.Errorf("config = %v, want nam-eur-asia1", got.Attributes["config"])
	}
}

func TestExtractSpannerInstanceCreatingStateOmitsPosture(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/instances/creating-instance",
		"config": "projects/demo-project/instanceConfigs/regional-us-east1",
		"nodeCount": 1,
		"state": "CREATING"
	}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["state"] != "CREATING" {
		t.Errorf("state = %v, want CREATING", got.Attributes["state"])
	}
}

func TestExtractSpannerInstancePartialDataOmitsZeroValues(t *testing.T) {
	got, err := extractSpannerInstance(spannerInstanceContext(`{}`))
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

func TestExtractSpannerInstanceMalformedDataErrors(t *testing.T) {
	if _, err := extractSpannerInstance(spannerInstanceContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractSpannerInstanceZeroNodeCountIsOmitted(t *testing.T) {
	// nodeCount/processingUnits are *int64 so a genuinely absent field is
	// distinguishable from an explicit zero; Spanner never provisions a
	// zero-capacity instance, so a zero value here reflects a sparse CAI page,
	// not a real posture, and must not be fabricated into an attribute.
	const data = `{"name": "projects/demo-project/instances/sparse", "nodeCount": 0}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["node_count"]; ok {
		t.Errorf("node_count=0 should be omitted as a sparse/zero-value CAI page, got %#v", got.Attributes)
	}
}

func TestExtractSpannerInstanceLabelCountOnlyNoRawLabels(t *testing.T) {
	// The typed-depth extractor must never surface raw label keys or values —
	// that redaction-safe fingerprinting belongs to the base observation path
	// (parse.go), not the per-asset-type extractor. Only a bounded count.
	const data = `{
		"name": "projects/demo-project/instances/labeled",
		"labels": {"team": "payments", "cost-center": "eng-42", "pii": "customer-email@example.com"}
	}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["label_count"] != 3 {
		t.Errorf("label_count = %v, want 3", got.Attributes["label_count"])
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"payments", "eng-42", "customer-email@example.com", "cost-center", "\"team\""} {
		if containsString(string(blob), banned) {
			t.Errorf("extraction output leaked raw label content %q: %s", banned, blob)
		}
	}
}

func TestExtractSpannerInstanceAdversarialRedactionSweep(t *testing.T) {
	// Full-struct JSON marshal + banned-token sweep per repo convention: any
	// secret-shaped or data-plane token anywhere in the extraction output is a
	// redaction failure regardless of which field it leaked through.
	const data = `{
		"name": "projects/demo-project/instances/adversarial",
		"config": "projects/demo-project/instanceConfigs/regional-us-central1",
		"displayName": "Adversarial Instance With Secret admin-password-123",
		"nodeCount": 5,
		"state": "READY",
		"labels": {"secret-key": "AKIA_FAKE_SECRET_VALUE"},
		"endpointUris": ["spanner.googleapis.com:443"]
	}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	bannedTokens := []string{
		"AKIA_FAKE_SECRET_VALUE",
		"endpointUris",
		"spanner.googleapis.com:443",
		"secret-key",
	}
	for _, token := range bannedTokens {
		if containsString(string(blob), token) {
			t.Errorf("extraction output leaked banned token %q: %s", token, blob)
		}
	}
}
