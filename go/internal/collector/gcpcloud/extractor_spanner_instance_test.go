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
		"label_count":  int64(1),
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantConfig := "//spanner.googleapis.com/projects/demo-project/instanceConfigs/regional-us-central1"
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly the instance-config edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSpannerInstanceUsesInstanceConfig, wantConfig, assetTypeSpannerInstanceConfig)
	wantAnchors := []string{wantConfig}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
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
	wantConfig := "//spanner.googleapis.com/projects/demo-project/instanceConfigs/nam-eur-asia1"
	assertRelationship(t, got.Relationships, relationshipTypeSpannerInstanceUsesInstanceConfig, wantConfig, assetTypeSpannerInstanceConfig)
}

func TestExtractSpannerInstanceBareConfigIDQualifiesToProject(t *testing.T) {
	// A sparse CAI page may carry a bare config id with no "projects/" prefix;
	// it is qualified against the instance's own project (a Spanner instance
	// always references a config visible to its project), so the edge still
	// resolves to a full InstanceConfig resource name.
	const data = `{
		"name": "projects/demo-project/instances/bare",
		"config": "regional-us-central1",
		"nodeCount": 1
	}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["config"] != "regional-us-central1" {
		t.Errorf("config = %v, want regional-us-central1", got.Attributes["config"])
	}
	wantConfig := "//spanner.googleapis.com/projects/demo-project/instanceConfigs/regional-us-central1"
	if len(got.Relationships) != 1 {
		t.Fatalf("expected the instance-config edge from a bare id, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSpannerInstanceUsesInstanceConfig, wantConfig, assetTypeSpannerInstanceConfig)
}

func TestExtractSpannerInstanceAlreadyPrefixedConfigNotDoublePrefixed(t *testing.T) {
	// An already CAI-prefixed config reference must not be double-prefixed.
	const data = `{
		"name": "projects/demo-project/instances/prefixed",
		"config": "//spanner.googleapis.com/projects/demo-project/instanceConfigs/regional-us-central1"
	}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantConfig := "//spanner.googleapis.com/projects/demo-project/instanceConfigs/regional-us-central1"
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly one config edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSpannerInstanceUsesInstanceConfig, wantConfig, assetTypeSpannerInstanceConfig)
	// The short-name attribute is still the trailing segment, not the prefix.
	if got.Attributes["config"] != "regional-us-central1" {
		t.Errorf("config attribute = %v, want regional-us-central1", got.Attributes["config"])
	}
}

func TestExtractSpannerInstanceNoConfigEmitsNoEdge(t *testing.T) {
	// An instance blob with no config reference (a sparse page) emits no edge
	// and no anchor rather than fabricating an unresolvable endpoint.
	const data = `{"name": "projects/demo-project/instances/no-config", "nodeCount": 1, "state": "READY"}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["config"]; ok {
		t.Errorf("config attribute should be absent, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges without a config reference, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors without a config reference, got %#v", got.CorrelationAnchors)
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

func TestExtractSpannerInstanceExplicitZeroNodeCountIsPreserved(t *testing.T) {
	// nodeCount/processingUnits are *int64 so an explicit reported zero is
	// distinguishable from a genuinely absent field, and the extractor must
	// keep that distinction. Zero is a real Spanner posture, not a sparse-page
	// artifact: the projects.instances REST resource reports nodeCount 0 for a
	// FREE_INSTANCE and can report 0 for a standard instance still in the
	// CREATING state before capacity is assigned. Dropping an explicit zero
	// would erase capacity evidence and make a free-tier/creating instance
	// indistinguishable from one whose field CAI simply did not populate.
	const data = `{"name": "projects/demo-project/instances/free", "nodeCount": 0}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := got.Attributes["node_count"]
	if !ok {
		t.Fatalf("node_count=0 must be preserved as a real reported value, got %#v", got.Attributes)
	}
	if v != int64(0) {
		t.Errorf("node_count = %v, want int64(0)", v)
	}
}

func TestExtractSpannerInstanceAbsentNodeCountIsOmitted(t *testing.T) {
	// A genuinely absent nodeCount (the field is not present in the CAI blob at
	// all — the common shape for a processing-units-provisioned instance) is a
	// nil *int64 and must be omitted, so the nil-vs-explicit-zero distinction is
	// symmetric: nil omits, explicit zero is kept.
	const data = `{"name": "projects/demo-project/instances/pu-only", "processingUnits": 1000}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["node_count"]; ok {
		t.Errorf("absent node_count must be omitted, got %#v", got.Attributes)
	}
	if got.Attributes["processing_units"] != int64(1000) {
		t.Errorf("processing_units = %v, want 1000", got.Attributes["processing_units"])
	}
}

func TestExtractSpannerInstanceFreeInstanceZeroCapacityPreserved(t *testing.T) {
	// A FREE_INSTANCE reports processingUnits 0 (free tier has no provisioned
	// compute capacity). That explicit zero must survive so an operator can
	// distinguish a free-tier instance from a paid one whose capacity field CAI
	// failed to populate.
	const data = `{
		"name": "projects/demo-project/instances/free-tier",
		"config": "projects/demo-project/instanceConfigs/regional-us-central1",
		"instanceType": "FREE_INSTANCE",
		"processingUnits": 0,
		"state": "READY"
	}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := got.Attributes["processing_units"]
	if !ok {
		t.Fatalf("processing_units=0 must be preserved for a FREE_INSTANCE, got %#v", got.Attributes)
	}
	if v != int64(0) {
		t.Errorf("processing_units = %v, want int64(0)", v)
	}
}

func TestExtractSpannerInstanceCreatingZeroNodeCountPreserved(t *testing.T) {
	// A standard instance still in the CREATING state can report nodeCount 0
	// before capacity is assigned. That explicit zero is real posture — the
	// instance genuinely has no nodes yet — and must be preserved.
	const data = `{
		"name": "projects/demo-project/instances/provisioning",
		"config": "projects/demo-project/instanceConfigs/regional-us-central1",
		"nodeCount": 0,
		"state": "CREATING"
	}`

	got, err := extractSpannerInstance(spannerInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := got.Attributes["node_count"]
	if !ok {
		t.Fatalf("node_count=0 must be preserved for a CREATING instance, got %#v", got.Attributes)
	}
	if v != int64(0) {
		t.Errorf("node_count = %v, want int64(0)", v)
	}
	if got.Attributes["state"] != "CREATING" {
		t.Errorf("state = %v, want CREATING", got.Attributes["state"])
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
	if got.Attributes["label_count"] != int64(3) {
		t.Errorf("label_count = %v, want int64(3)", got.Attributes["label_count"])
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
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
