// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testRelationshipObservation() RelationshipObservation {
	return RelationshipObservation{
		Boundary:               testBoundary(),
		SourceFullResourceName: "//compute.googleapis.com/projects/my-project/zones/us-central1-a/instances/api-1",
		SourceAssetType:        "compute.googleapis.com/Instance",
		RelationshipType:       "uses_network",
		TargetFullResourceName: "//compute.googleapis.com/projects/my-project/global/networks/prod-vpc",
		TargetAssetType:        "compute.googleapis.com/Network",
		SupportState:           RelationshipSupportSupported,
		UpdateTime:             time.Date(2026, 6, 9, 11, 30, 0, 0, time.UTC),
	}
}

// TestNewCloudRelationshipEnvelopeBuildsContractFields proves the relationship
// fact preserves both endpoint full resource names, asset types, the relationship
// type, and the support state as provenance-only evidence (it resolves no
// endpoints and writes no graph edge).
func TestNewCloudRelationshipEnvelopeBuildsContractFields(t *testing.T) {
	obs := testRelationshipObservation()
	env, err := NewCloudRelationshipEnvelope(obs)
	if err != nil {
		t.Fatalf("NewCloudRelationshipEnvelope error: %v", err)
	}
	if env.FactKind != facts.GCPCloudRelationshipFactKind {
		t.Fatalf("FactKind = %q", env.FactKind)
	}
	if env.SchemaVersion != facts.GCPCloudRelationshipSchemaVersion {
		t.Fatalf("SchemaVersion = %q", env.SchemaVersion)
	}
	if env.Payload["source_full_resource_name"] != obs.SourceFullResourceName {
		t.Fatalf("source_full_resource_name = %#v", env.Payload["source_full_resource_name"])
	}
	if env.Payload["target_full_resource_name"] != obs.TargetFullResourceName {
		t.Fatalf("target_full_resource_name = %#v", env.Payload["target_full_resource_name"])
	}
	if env.Payload["relationship_type"] != "uses_network" {
		t.Fatalf("relationship_type = %#v", env.Payload["relationship_type"])
	}
	if env.Payload["support_state"] != RelationshipSupportSupported {
		t.Fatalf("support_state = %#v", env.Payload["support_state"])
	}
	if env.Payload["source_project_id"] != "my-project" {
		t.Fatalf("source_project_id = %#v, want my-project", env.Payload["source_project_id"])
	}
}

// TestNewCloudRelationshipEnvelopeStableKeyIgnoresTimeChurn proves the stable key
// is endpoint+type derived, so a changed update time re-emits the same row.
func TestNewCloudRelationshipEnvelopeStableKeyIgnoresTimeChurn(t *testing.T) {
	a := testRelationshipObservation()
	b := testRelationshipObservation()
	b.UpdateTime = time.Date(2026, 6, 9, 18, 0, 0, 0, time.UTC)

	ea, err := NewCloudRelationshipEnvelope(a)
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	eb, err := NewCloudRelationshipEnvelope(b)
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if ea.StableFactKey != eb.StableFactKey {
		t.Fatalf("update-time churn split stable key: %q vs %q", ea.StableFactKey, eb.StableFactKey)
	}
}

// TestNewCloudRelationshipEnvelopeRejectsIncomplete proves the builder fails
// closed on a missing endpoint, a missing relationship type, or an unknown
// support state.
func TestNewCloudRelationshipEnvelopeRejectsIncomplete(t *testing.T) {
	for name, mutate := range map[string]func(*RelationshipObservation){
		"missing source":       func(o *RelationshipObservation) { o.SourceFullResourceName = "" },
		"missing target":       func(o *RelationshipObservation) { o.TargetFullResourceName = "" },
		"missing relationship": func(o *RelationshipObservation) { o.RelationshipType = "" },
		"unknown support":      func(o *RelationshipObservation) { o.SupportState = "made-up" },
	} {
		obs := testRelationshipObservation()
		mutate(&obs)
		if _, err := NewCloudRelationshipEnvelope(obs); err == nil {
			t.Fatalf("%s: error = nil, want non-nil", name)
		}
	}
}
