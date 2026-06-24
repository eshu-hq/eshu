// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testResourceChangeObservation() ResourceChangeObservation {
	return ResourceChangeObservation{
		Boundary:             testBoundary(),
		TargetARMResourceID:  "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-web-01",
		ChangeType:           ChangeTypeUpdated,
		ChangeTime:           time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
		Operation:            "Microsoft.Compute/virtualMachines/write",
		ClientType:           "arm",
		ActorID:              "actor-aaaa-bbbb",
		ActorClass:           "service_principal",
		ChangedPropertyPaths: []string{"properties.hardwareProfile.vmSize", "tags.env", "properties.hardwareProfile.vmSize"},
	}
}

// TestNewResourceChangeEnvelopeBuildsContractFields proves the change fact carries
// the bounded change type, deduped changed property PATHS, a fingerprinted actor
// (never raw), and marks a delete as a tombstone candidate only.
func TestNewResourceChangeEnvelopeBuildsContractFields(t *testing.T) {
	obs := testResourceChangeObservation()
	key := testRedactionKey(t)

	env, err := NewResourceChangeEnvelope(obs, key)
	if err != nil {
		t.Fatalf("NewResourceChangeEnvelope error: %v", err)
	}
	if env.FactKind != facts.AzureResourceChangeFactKind {
		t.Fatalf("FactKind = %q", env.FactKind)
	}
	if env.Payload["change_type"] != ChangeTypeUpdated {
		t.Fatalf("change_type = %#v", env.Payload["change_type"])
	}
	paths, ok := env.Payload["changed_property_paths"].([]string)
	if !ok || len(paths) != 2 {
		t.Fatalf("changed_property_paths = %#v, want 2 deduped sorted", env.Payload["changed_property_paths"])
	}
	actorFp, _ := env.Payload["actor_fingerprint"].(string)
	if actorFp == "" || actorFp == obs.ActorID {
		t.Fatalf("actor_fingerprint = %q, want non-raw marker", actorFp)
	}
	if env.Payload["is_tombstone_candidate"] != false {
		t.Fatalf("is_tombstone_candidate = %#v, want false for update", env.Payload["is_tombstone_candidate"])
	}
	// A delete is a tombstone candidate only.
	del := testResourceChangeObservation()
	del.ChangeType = ChangeTypeDeleted
	delEnv, err := NewResourceChangeEnvelope(del, key)
	if err != nil {
		t.Fatalf("delete change: %v", err)
	}
	if delEnv.Payload["is_tombstone_candidate"] != true {
		t.Fatalf("is_tombstone_candidate = %#v, want true for delete", delEnv.Payload["is_tombstone_candidate"])
	}
}

// TestNewResourceChangeEnvelopeRejectsInvalid proves the builder fails closed on a
// missing target, an unknown change type, a zero change time, or a zero key.
func TestNewResourceChangeEnvelopeRejectsInvalid(t *testing.T) {
	key := testRedactionKey(t)
	for name, mutate := range map[string]func(*ResourceChangeObservation){
		"missing target": func(o *ResourceChangeObservation) { o.TargetARMResourceID = "" },
		"unknown type":   func(o *ResourceChangeObservation) { o.ChangeType = "made-up" },
		"zero time":      func(o *ResourceChangeObservation) { o.ChangeTime = time.Time{} },
	} {
		obs := testResourceChangeObservation()
		mutate(&obs)
		if _, err := NewResourceChangeEnvelope(obs, key); err == nil {
			t.Fatalf("%s: error = nil, want non-nil", name)
		}
	}
	if _, err := NewResourceChangeEnvelope(testResourceChangeObservation(), redact.Key{}); err == nil {
		t.Fatal("zero key: error = nil, want non-nil")
	}
}
