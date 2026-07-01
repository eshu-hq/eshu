// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const workloadIdentityPoolFullName = "//iam.googleapis.com/projects/123456789/locations/global/workloadIdentityPools/demo-pool"

func workloadIdentityPoolContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: workloadIdentityPoolFullName,
		AssetType:        workloadIdentityPoolAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestWorkloadIdentityPoolExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(workloadIdentityPoolAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", workloadIdentityPoolAssetType)
	}
}

func TestExtractWorkloadIdentityPoolFullResource(t *testing.T) {
	const data = `{
		"name": "projects/123456789/locations/global/workloadIdentityPools/demo-pool",
		"displayName": "Demo Pool",
		"description": "CI trust pool",
		"state": "ACTIVE",
		"disabled": false
	}`
	got, err := extractWorkloadIdentityPool(workloadIdentityPoolContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"state":    "ACTIVE",
		"disabled": false,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("pool derives no outbound edges (providers are inbound children), got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("pool derives no outbound anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractWorkloadIdentityPoolDeletedState(t *testing.T) {
	const data = `{"state": "DELETED", "disabled": true}`
	got, err := extractWorkloadIdentityPool(workloadIdentityPoolContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["state"] != "DELETED" {
		t.Errorf("state = %v, want DELETED", got.Attributes["state"])
	}
	if got.Attributes["disabled"] != true {
		t.Errorf("disabled = %v, want true", got.Attributes["disabled"])
	}
}

func TestExtractWorkloadIdentityPoolAbsentDisabledOmitted(t *testing.T) {
	const data = `{"state": "ACTIVE"}`
	got, err := extractWorkloadIdentityPool(workloadIdentityPoolContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["disabled"]; ok {
		t.Errorf("absent disabled must be omitted: %#v", got.Attributes)
	}
}

func TestExtractWorkloadIdentityPoolEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractWorkloadIdentityPool(workloadIdentityPoolContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
}

func TestExtractWorkloadIdentityPoolMalformedDataErrors(t *testing.T) {
	if _, err := extractWorkloadIdentityPool(workloadIdentityPoolContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
