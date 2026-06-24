// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testRelationshipObservation() RelationshipObservation {
	providerTime := time.Date(2026, 6, 9, 11, 30, 0, 0, time.UTC)
	return RelationshipObservation{
		Boundary:            testBoundary(),
		SourceARMResourceID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-web-01",
		RelationshipType:    "uses_network_interface",
		TargetARMResourceID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Network/networkInterfaces/nic-web-01",
		SupportState:        RelationshipSupportSupported,
		ProviderTime:        &providerTime,
	}
}

// TestNewRelationshipEnvelopeBuildsContractFields proves the relationship fact
// preserves both endpoint ARM identities, the relationship type, and the support
// state as provenance-only evidence (it resolves no endpoints and writes no
// edges).
func TestNewRelationshipEnvelopeBuildsContractFields(t *testing.T) {
	obs := testRelationshipObservation()
	env, err := NewRelationshipEnvelope(obs)
	if err != nil {
		t.Fatalf("NewRelationshipEnvelope error: %v", err)
	}
	if env.FactKind != facts.AzureCloudRelationshipFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.AzureCloudRelationshipFactKind)
	}
	if env.SchemaVersion != facts.AzureCloudRelationshipSchemaVersion {
		t.Fatalf("SchemaVersion = %q", env.SchemaVersion)
	}
	if env.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q", env.CollectorKind)
	}
	if env.Payload["source_arm_resource_id"] != obs.SourceARMResourceID {
		t.Fatalf("source_arm_resource_id = %#v", env.Payload["source_arm_resource_id"])
	}
	if env.Payload["target_arm_resource_id"] != obs.TargetARMResourceID {
		t.Fatalf("target_arm_resource_id = %#v", env.Payload["target_arm_resource_id"])
	}
	if env.Payload["relationship_type"] != "uses_network_interface" {
		t.Fatalf("relationship_type = %#v", env.Payload["relationship_type"])
	}
	if env.Payload["support_state"] != RelationshipSupportSupported {
		t.Fatalf("support_state = %#v", env.Payload["support_state"])
	}
	// ParseARMIdentity normalizes resource types to lower case, matching the
	// azure_cloud_resource fact.
	if env.Payload["source_resource_type"] != "microsoft.compute/virtualmachines" {
		t.Fatalf("source_resource_type = %#v", env.Payload["source_resource_type"])
	}
	if env.Payload["target_resource_type"] != "microsoft.network/networkinterfaces" {
		t.Fatalf("target_resource_type = %#v", env.Payload["target_resource_type"])
	}
}

// TestNewRelationshipEnvelopeStableKeyIgnoresTimeChurn proves the stable fact key
// is endpoint+type derived, so a changed observation time re-emits the same row
// within a generation instead of splitting it.
func TestNewRelationshipEnvelopeStableKeyIgnoresTimeChurn(t *testing.T) {
	a := testRelationshipObservation()
	b := testRelationshipObservation()
	later := time.Date(2026, 6, 9, 18, 0, 0, 0, time.UTC)
	b.ProviderTime = &later

	ea, err := NewRelationshipEnvelope(a)
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	eb, err := NewRelationshipEnvelope(b)
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if ea.StableFactKey != eb.StableFactKey {
		t.Fatalf("provider time churn split stable key: %q vs %q", ea.StableFactKey, eb.StableFactKey)
	}
}

// TestNewRelationshipEnvelopeRejectsIncomplete proves the builder fails closed on
// a missing endpoint, missing relationship type, or an unknown support state, so
// a half-observed relationship is never fabricated into evidence.
func TestNewRelationshipEnvelopeRejectsIncomplete(t *testing.T) {
	for name, mutate := range map[string]func(*RelationshipObservation){
		"missing source":       func(o *RelationshipObservation) { o.SourceARMResourceID = "" },
		"missing target":       func(o *RelationshipObservation) { o.TargetARMResourceID = "" },
		"missing relationship": func(o *RelationshipObservation) { o.RelationshipType = "" },
		"unknown support":      func(o *RelationshipObservation) { o.SupportState = "made-up" },
	} {
		obs := testRelationshipObservation()
		mutate(&obs)
		if _, err := NewRelationshipEnvelope(obs); err == nil {
			t.Fatalf("%s: error = nil, want non-nil", name)
		}
	}
}

// TestNewRelationshipEnvelopeDefaultsSupportState proves a blank support state
// defaults to supported (an observed relationship is supported by definition).
func TestNewRelationshipEnvelopeDefaultsSupportState(t *testing.T) {
	obs := testRelationshipObservation()
	obs.SupportState = ""
	env, err := NewRelationshipEnvelope(obs)
	if err != nil {
		t.Fatalf("NewRelationshipEnvelope error: %v", err)
	}
	if env.Payload["support_state"] != RelationshipSupportSupported {
		t.Fatalf("support_state = %#v, want %q", env.Payload["support_state"], RelationshipSupportSupported)
	}
}
