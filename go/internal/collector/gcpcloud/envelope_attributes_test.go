// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func attributesTestBoundary() Boundary {
	return Boundary{
		CollectorInstanceID: "collector-1",
		ParentScopeKind:     ParentScopeProject,
		ParentScopeID:       "demo-project",
		AssetTypeFamily:     "bigquery",
		ContentFamily:       "resource",
		LocationBucket:      "us",
		ScopeID:             "gcp:project:demo-project:bigquery:resource:us",
		GenerationID:        "gen-1",
		FencingToken:        1,
	}
}

func TestNewCloudResourceEnvelopeEmitsAttributesAndAnchors(t *testing.T) {
	obs := ResourceObservation{
		Name:      "//bigquery.googleapis.com/projects/demo-project/datasets/analytics/tables/events",
		AssetType: assetTypeBigQueryTable,
		Attributes: map[string]any{
			"table_type":         "TABLE",
			"schema_field_count": 2,
			"num_rows":           int64(42),
		},
		CorrelationAnchors: []string{
			"//bigquery.googleapis.com/projects/demo-project/datasets/analytics",
			"//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1",
		},
	}

	env, err := NewCloudResourceEnvelope(attributesTestBoundary(), obs, redact.Key{})
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}
	if env.SchemaVersion != facts.GCPCloudResourceSchemaVersion {
		t.Errorf("schema version = %q, want %q", env.SchemaVersion, facts.GCPCloudResourceSchemaVersion)
	}

	gotAttrs, ok := env.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing attributes map: %#v", env.Payload["attributes"])
	}
	if !reflect.DeepEqual(gotAttrs, obs.Attributes) {
		t.Errorf("attributes = %#v, want %#v", gotAttrs, obs.Attributes)
	}
	gotAnchors, ok := env.Payload["correlation_anchors"].([]string)
	if !ok {
		t.Fatalf("payload missing correlation_anchors slice: %#v", env.Payload["correlation_anchors"])
	}
	if !reflect.DeepEqual(gotAnchors, obs.CorrelationAnchors) {
		t.Errorf("correlation_anchors = %#v, want %#v", gotAnchors, obs.CorrelationAnchors)
	}
}

func TestNewCloudResourceEnvelopeOmitsEmptyTypedDepth(t *testing.T) {
	obs := ResourceObservation{
		Name:      "//compute.googleapis.com/projects/demo/zones/z/instances/vm-1",
		AssetType: "compute.googleapis.com/Instance",
	}
	env, err := NewCloudResourceEnvelope(attributesTestBoundary(), obs, redact.Key{})
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}
	if got := env.Payload["attributes"]; got != nil {
		if m, ok := got.(map[string]any); !ok || len(m) != 0 {
			t.Errorf("expected nil/empty attributes, got %#v", got)
		}
	}
	if got := env.Payload["correlation_anchors"]; got != nil {
		if s, ok := got.([]string); !ok || len(s) != 0 {
			t.Errorf("expected nil/empty correlation_anchors, got %#v", got)
		}
	}
}

func TestGCPCloudResourceSchemaVersionBumpedForTypedDepth(t *testing.T) {
	if facts.GCPCloudResourceSchemaVersion == "1.0.0" {
		t.Fatalf("schema version must bump past 1.0.0 when attributes/correlation_anchors are added")
	}
}
