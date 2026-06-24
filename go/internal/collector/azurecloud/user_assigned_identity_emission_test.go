// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"context"
	"testing"
)

// rawUserAssignedGUIDs are every raw principal/client GUID in the fixture; none
// may appear in any emitted payload value.
var rawUserAssignedGUIDs = []string{
	"aaaaaaaa-1111-2222-3333-444444444444", // system principal
	"99999999-9999-9999-9999-999999999999", // tenant
	"bbbbbbbb-1111-2222-3333-444444444444", // uami-a principal
	"dddddddd-1111-2222-3333-444444444444", // uami-a client
	"cccccccc-1111-2222-3333-444444444444", // uami-b principal
	"eeeeeeee-1111-2222-3333-444444444444", // uami-b client
}

// TestCollectEmitsSystemAndUserAssignedIdentities proves a resource with both a
// system-assigned and two user-assigned managed identities emits one identity
// observation each (1 system + 2 user), with every principal/client GUID
// fingerprinted and never carried raw.
func TestCollectEmitsSystemAndUserAssignedIdentities(t *testing.T) {
	page, err := ParseResourceGraphPage(loadFixture(t, "resources_user_assigned_identity.json"))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	provider := &fixturePageProvider{pages: map[string]ResourceGraphPage{"": page}}
	key := testRedactionKey(t)

	result, err := NewCollector(provider, nil, WithRedactionKey(key)).
		Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	ids := factsOfKind(result.Facts, "azure_identity_observation")
	if len(ids) != 3 || result.IdentityObservationCount != 3 {
		t.Fatalf("identity observations = %d (count %d), want 3 (1 system + 2 user)", len(ids), result.IdentityObservationCount)
	}

	var system, user int
	for _, id := range ids {
		switch id.Payload["identity_type"] {
		case IdentityTypeSystemAssigned:
			system++
		case IdentityTypeUserAssigned:
			user++
			if _, ok := id.Payload["client_fingerprint"].(string); !ok {
				t.Fatalf("user-assigned identity missing client_fingerprint: %#v", id.Payload)
			}
		default:
			t.Fatalf("unexpected identity_type %#v", id.Payload["identity_type"])
		}
		for k, v := range id.Payload {
			s, ok := v.(string)
			if !ok {
				continue
			}
			for _, raw := range rawUserAssignedGUIDs {
				if s == raw {
					t.Fatalf("raw GUID %q leaked in payload[%q]", raw, k)
				}
			}
		}
	}
	if system != 1 || user != 2 {
		t.Fatalf("identity types = %d system, %d user; want 1 system, 2 user", system, user)
	}
}
