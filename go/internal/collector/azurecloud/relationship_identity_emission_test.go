// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"context"
	"testing"
)

const (
	fixtureManagedByTarget   = "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachineScaleSets/vmss-web"
	fixtureIdentityPrincipal = "aaaaaaaa-1111-2222-3333-444444444444"
	fixtureIdentityTenant    = "99999999-9999-9999-9999-999999999999"
)

func newRelationshipIdentityProvider(t *testing.T) *fixturePageProvider {
	t.Helper()
	page, err := ParseResourceGraphPage(loadFixture(t, "resources_relationship_identity.json"))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return &fixturePageProvider{pages: map[string]ResourceGraphPage{"": page}}
}

// TestCollectEmitsRelationshipAndIdentityWhenKeyed proves the scan loop emits a
// provenance-only managed_by relationship from the ARM managedBy field and a
// keyed system-assigned identity observation (principal/tenant fingerprinted,
// never raw) from the ARM identity block.
func TestCollectEmitsRelationshipAndIdentityWhenKeyed(t *testing.T) {
	key := testRedactionKey(t)
	result, err := NewCollector(newRelationshipIdentityProvider(t), nil, WithRedactionKey(key)).
		Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	rels := factsOfKind(result.Facts, "azure_cloud_relationship")
	if len(rels) != 1 || result.RelationshipCount != 1 {
		t.Fatalf("relationships = %d (count %d), want 1", len(rels), result.RelationshipCount)
	}
	rel := rels[0]
	if rel.Payload["relationship_type"] != "managed_by" {
		t.Fatalf("relationship_type = %#v, want managed_by", rel.Payload["relationship_type"])
	}
	if rel.Payload["target_arm_resource_id"] != fixtureManagedByTarget {
		t.Fatalf("target_arm_resource_id = %#v", rel.Payload["target_arm_resource_id"])
	}

	ids := factsOfKind(result.Facts, "azure_identity_observation")
	if len(ids) != 1 || result.IdentityObservationCount != 1 {
		t.Fatalf("identities = %d (count %d), want 1", len(ids), result.IdentityObservationCount)
	}
	id := ids[0]
	if id.Payload["identity_type"] != IdentityTypeSystemAssigned {
		t.Fatalf("identity_type = %#v", id.Payload["identity_type"])
	}
	principalFp, _ := id.Payload["principal_fingerprint"].(string)
	if principalFp == "" || principalFp == fixtureIdentityPrincipal {
		t.Fatalf("principal_fingerprint = %q, want non-raw marker", principalFp)
	}
	tenantFp, _ := id.Payload["tenant_fingerprint"].(string)
	if tenantFp == "" || tenantFp == fixtureIdentityTenant {
		t.Fatalf("tenant_fingerprint = %q, want non-raw marker", tenantFp)
	}
	// The raw principal GUID must never appear anywhere in the identity payload.
	for k, v := range id.Payload {
		if s, ok := v.(string); ok && s == fixtureIdentityPrincipal {
			t.Fatalf("raw principal GUID leaked in payload[%q]", k)
		}
	}
}

// TestCollectRelationshipWithoutKeyEmitsRelationshipButNoIdentity proves the
// managed_by relationship is provenance-only (emitted without a key) while the
// identity observation is keyed (skipped without a key), and the resource fact
// still emits.
func TestCollectRelationshipWithoutKeyEmitsRelationshipButNoIdentity(t *testing.T) {
	result, err := NewCollector(newRelationshipIdentityProvider(t), nil).
		Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}
	if result.RelationshipCount != 1 {
		t.Fatalf("RelationshipCount = %d, want 1 (provenance-only, no key needed)", result.RelationshipCount)
	}
	if result.IdentityObservationCount != 0 {
		t.Fatalf("IdentityObservationCount = %d, want 0 without a redaction key", result.IdentityObservationCount)
	}
	if result.ResourceCount != 2 {
		t.Fatalf("ResourceCount = %d, want 2", result.ResourceCount)
	}
}
