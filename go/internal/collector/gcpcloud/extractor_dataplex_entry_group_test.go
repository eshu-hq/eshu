// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const dataplexEntryGroupFullName = "//dataplex.googleapis.com/projects/demo-project/locations/us-central1/entryGroups/analytics"

func dataplexEntryGroupContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: dataplexEntryGroupFullName,
		AssetType:        assetTypeDataplexEntryGroup,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestDataplexEntryGroupExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeDataplexEntryGroup); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeDataplexEntryGroup)
	}
}

func TestExtractDataplexEntryGroupFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/entryGroups/analytics",
		"displayName": "Analytics catalog",
		"description": "team analytics entries",
		"transferStatus": "TRANSFER_STATUS_MIGRATED",
		"createTime": "2024-05-01T00:00:00Z",
		"updateTime": "2024-06-01T00:00:00Z",
		"labels": {"team": "data"}
	}`

	got, err := extractDataplexEntryGroup(dataplexEntryGroupContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"transfer_status": "TRANSFER_STATUS_MIGRATED",
		"creation_time":   "2024-05-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	// An entry group is a container: contained entries reference it from their own
	// assets and the project is base-observation placement, so it derives no
	// outbound edges or anchors.
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractDataplexEntryGroupMinimal(t *testing.T) {
	const data = `{"transferStatus": "TRANSFER_STATUS_PENDING"}`
	got, err := extractDataplexEntryGroup(dataplexEntryGroupContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{"transfer_status": "TRANSFER_STATUS_PENDING"}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
}

func TestExtractDataplexEntryGroupEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractDataplexEntryGroup(dataplexEntryGroupContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
}

func TestExtractDataplexEntryGroupFractionalCreateTime(t *testing.T) {
	// Dataplex createTime can carry sub-second precision; it must still normalize
	// to whole-second RFC3339 rather than being silently dropped.
	const data = `{"createTime": "2024-05-01T00:00:00.123456Z"}`
	got, err := extractDataplexEntryGroup(dataplexEntryGroupContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["creation_time"] != "2024-05-01T00:00:00Z" {
		t.Errorf("creation_time = %v, want 2024-05-01T00:00:00Z (fractional seconds normalized)", got.Attributes["creation_time"])
	}
}

func TestExtractDataplexEntryGroupMalformedDataErrors(t *testing.T) {
	if _, err := extractDataplexEntryGroup(dataplexEntryGroupContext(`{bad`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
	if _, err := extractDataplexEntryGroup(dataplexEntryGroupContext(``)); err == nil {
		t.Fatalf("expected an error for empty resource data")
	}
}
