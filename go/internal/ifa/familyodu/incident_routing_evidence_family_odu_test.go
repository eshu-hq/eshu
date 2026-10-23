// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"testing"
)

// TestIncidentRoutingEvidenceFamilyOduCarriesDeclaredContentEntity pins the
// live leaf's fifth envelope (#6228 incident_routing_evidence live leaf): the
// PagerDuty declaration as a content_entity fact so the committed cassette
// carries what the production loader reads from content_entities.
//
// The production incident loader reads Declared from the content_entities
// table, never from the fact payloads, so a facts-only fixture can never
// converge live: the drive would load zero declared rows and materialize
// zero edges. The declaration envelope below mirrors the git collector's
// ContentEntityFactEnvelope shape (explicit entity_id, entity_metadata map)
// so the entities phase projects it into content_entities with the same
// entity_id the guard's expected-edge set pins (declared-entity-6228).
// BuildIncidentRoutingEvidenceInputs ignores unknown fact kinds without
// quarantining them, so the guard stays green on the same four-fact decode
// while lockstep holds the cassette to this five-fact set.
func TestIncidentRoutingEvidenceFamilyOduCarriesDeclaredContentEntity(t *testing.T) {
	t.Parallel()

	odu := IncidentRoutingEvidenceFamilyOdu().Odu
	if len(odu.Facts) != 5 {
		t.Fatalf("IncidentRoutingEvidenceFamilyOdu facts = %d, want 5 (four incident envelopes plus the declared content_entity)", len(odu.Facts))
	}
	declared := odu.Facts[4]
	if declared.FactKind != ContentEntityFactKind {
		t.Fatalf("fifth envelope fact_kind = %q, want %q", declared.FactKind, ContentEntityFactKind)
	}
	if declared.ScopeID != incidentRoutingEvidenceFamilyScopeID {
		t.Fatalf("declaration scope_id = %q, want the family scope %q", declared.ScopeID, incidentRoutingEvidenceFamilyScopeID)
	}
	if declared.GenerationID != incidentRoutingEvidenceFamilyGenerationID {
		t.Fatalf("declaration generation_id = %q, want the family generation %q", declared.GenerationID, incidentRoutingEvidenceFamilyGenerationID)
	}
	payload := declared.Payload
	if payload["entity_id"] != "declared-entity-6228" {
		t.Fatalf("declaration entity_id = %v, want the literal the expected-edge set pins", payload["entity_id"])
	}
	if payload["entity_type"] != "PagerDutyDeclaration" {
		t.Fatalf("declaration entity_type = %v, want PagerDutyDeclaration (the loader's content_entities predicate)", payload["entity_type"])
	}
	if payload["entity_name"] != "module.checkout" {
		t.Fatalf("declaration entity_name = %v, want module.checkout (the production parser's entity name for the fixture)", payload["entity_name"])
	}
	metadata, ok := payload["entity_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("declaration payload carries no entity_metadata map, so the entities phase would persist no declared fields: %#v", payload)
	}
	for _, key := range []string{"source_class", "service_name", "escalation_policy", "outcome", "redaction_state"} {
		value, ok := metadata[key].(string)
		if !ok || value == "" {
			t.Fatalf("declaration entity_metadata[%q] = %v, want the non-empty production parser value", key, metadata[key])
		}
	}
	if metadata["source_class"] != "declared" {
		t.Fatalf("declaration entity_metadata[source_class] = %v, want declared (the loader's content_entities predicate)", metadata["source_class"])
	}
	if metadata["service_name"] != incidentRoutingEvidenceFamilyServiceName {
		t.Fatalf("declaration entity_metadata[service_name] = %v, want the family service %q", metadata["service_name"], incidentRoutingEvidenceFamilyServiceName)
	}
}
