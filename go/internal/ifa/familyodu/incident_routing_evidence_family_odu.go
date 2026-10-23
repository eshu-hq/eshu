// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	factsincident "github.com/eshu-hq/eshu/go/internal/facts/incident"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
)

// The incident_routing_evidence family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// A DIRECT-materialization family: the reducer writes it straight to
// cypher.IncidentRoutingEvidenceWriter through the
// WriteIncidentRoutingEvidence port. The relationship types are
// HAS_INTENDED_ROUTING, HAS_APPLIED_ROUTING, and HAS_LIVE_ROUTING, one per
// routing slot, each interpolated from the closed slot vocabulary the writer
// screens every row against — read them off
// cypher.IncidentRoutingEvidenceRelationshipTypes, never by deriving from
// the port or family name.
//
// Facts are built as typed incidentv1 values and encoded through the
// factschema EncodeIncidentRecord /
// EncodeIncidentRoutingAppliedPagerDutyResource /
// EncodeIncidentRoutingObservedPagerDutyService encoders, never as hand-built
// maps (Contract System v1).
//
// The declared PagerDuty half is content-entity metadata, which the git
// collector ships as a content_entity fact: the Odù carries it as one
// (incidentRoutingEvidenceFamilyDeclaredContentEntityFact), so the committed
// cassette the live gate replays projects the same content_entities row the
// production loader reads. The family's guard derives Declared by running
// the production HCL parser over the sibling pagerduty.tf fixture into a
// pagerduty_declarations row and decoding it through the production
// reducer/incident seam, then drives the production builder over Facts plus
// Declared.
//
// Scope note: the scope below is deliberately NOT any sibling cassette's
// scope. Scopes carry one ACTIVE generation; driving a second generation
// into an occupied scope supersedes the first family's generation, its
// handler never runs, and the sibling exact-set assert fails with zero
// edges. One live generation per scope per cell is the contract.
const (
	// IncidentRoutingEvidenceFamilyOduName is this Odù's catalog name, the
	// ref a materialized_edges:incident_routing_evidence coverage row would
	// name to resolve through it.
	IncidentRoutingEvidenceFamilyOduName = "odu:ifa-incident-routing-evidence-family"

	// incidentRoutingEvidenceFamilyScopeID is the single PagerDuty scope
	// every fact in this Odù belongs to. The reducer handler loads one
	// scope generation's facts, so a fixture spanning scopes would not
	// mirror any real intent.
	incidentRoutingEvidenceFamilyScopeID = "pagerduty:eshu-fixture-incident-routing"

	// incidentRoutingEvidenceFamilyGenerationID is the one scope generation
	// the Odù replays. The reducer's handler loads a single scope
	// generation's facts, so every fact below shares it.
	incidentRoutingEvidenceFamilyGenerationID = "gen-ifa-incident-routing-evidence-family-1"

	// incidentRoutingEvidenceFamilyCollectorKind mirrors what the
	// PagerDuty collector stamps on the incident and observed facts, so the
	// Odù describes the same envelopes a live generation would carry rather
	// than agreeing only on the payload.
	incidentRoutingEvidenceFamilyCollectorKind = "pagerduty"

	// incidentRoutingEvidenceFamilyAppliedCollectorKind is the source system
	// the Terraform-state collector stamps on applied routing facts.
	incidentRoutingEvidenceFamilyAppliedCollectorKind = "terraform_state"

	// incidentRoutingEvidenceFamilySourceConfidence marks these facts as
	// directly observed.
	incidentRoutingEvidenceFamilySourceConfidence = "observed"

	// incidentRoutingEvidenceFamilyServiceName is the PagerDuty service the
	// incident, the applied resource, the observed service, and the
	// pagerduty.tf module declaration all agree on. One shared literal is
	// what makes the three slots converge exact; a second service with no
	// declaration would materialize nothing, which the tombstoned applied
	// fact below already proves in the negative.
	incidentRoutingEvidenceFamilyServiceName = "Checkout API"

	// incidentRoutingEvidenceFamilyServiceID is the provider service id the
	// incident anchor, the applied resource, and the observed service join
	// on. Synthetic, matching the collector's redaction posture.
	incidentRoutingEvidenceFamilyServiceID = "PSERVICE6228"

	// incidentRoutingEvidenceFamilyIncidentID is the provider incident id
	// anchoring the family's one incident packet. Synthetic.
	incidentRoutingEvidenceFamilyIncidentID = "PINCIDENT6228"

	// incidentRoutingEvidenceFamilyEscalationPolicy is the escalation policy
	// reference the declaration, the applied resource, and the observed
	// service share. Agreement here is what keeps the observed slot exact
	// rather than drifted; a mismatch would flip the whole packet
	// provenance-only and the guard would red on zero rows.
	incidentRoutingEvidenceFamilyEscalationPolicy = "PEP1"
)

// incidentRoutingEvidenceFamilyStableFactKey derives one fact's durable dedup
// key in a readable fixture-local format rather than a hex digest, instead
// of hand-typing a string the expected-edge fixture cannot independently
// check.
func incidentRoutingEvidenceFamilyStableFactKey(factKind, identity string) string {
	return fmt.Sprintf(
		"pagerduty:%s:%s",
		factKind, identity,
	)
}

// incidentRoutingEvidenceFamilyDeclaredEntityID is the content-entity id the
// fixture's PagerDuty declaration carries. It is the literal the guard's
// expected-edge set pins as the intended slot's evidence id
// (declared-entity-6228): the entities phase preserves a content_entity
// fact's payload entity_id into content_entities, and the production loader
// reads it back verbatim, so the live drive converges on the unchanged
// expected set instead of minting a second identity for the same declaration.
const incidentRoutingEvidenceFamilyDeclaredEntityID = "declared-entity-6228"

// incidentRoutingEvidenceFamilyDeclaredContentEntityFact builds the
// declaration content_entity envelope the live drive needs.
//
// The production incident loader reads Declared from the content_entities
// table, never from the incident fact payloads, so a facts-only fixture
// loads zero declared rows live and materializes zero edges. The envelope
// mirrors the git collector's ContentEntityFactEnvelope shape: an explicit
// entity_id (preserved verbatim by BuildContentEntityRecord), the
// PagerDutyDeclaration label the loader's predicate names, the production
// parser's entity name and start line for the sibling pagerduty.tf fixture,
// and an entity_metadata map carrying the parser row minus its reserved keys
// (name, line_number, lang), exactly the stripping snapshotEntityMetadata
// applies before persisting. The builder seam ignores unknown fact kinds
// without quarantining them, so the guard keeps proving the same four-fact
// decode while lockstep holds the cassette to this five-fact set.
func incidentRoutingEvidenceFamilyDeclaredContentEntityFact() facts.Envelope {
	return facts.Envelope{
		ScopeID:          incidentRoutingEvidenceFamilyScopeID,
		GenerationID:     incidentRoutingEvidenceFamilyGenerationID,
		FactKind:         ContentEntityFactKind,
		StableFactKey:    "content_entity:" + incidentRoutingEvidenceFamilyDeclaredEntityID,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: incidentRoutingEvidenceFamilySourceConfidence,
		Payload: map[string]any{
			"graph_id":      incidentRoutingEvidenceFamilyDeclaredEntityID,
			"graph_kind":    "content_entity",
			"entity_id":     incidentRoutingEvidenceFamilyDeclaredEntityID,
			"repo_id":       "repo-observability",
			"relative_path": "pagerduty/main.tf",
			"entity_type":   "PagerDutyDeclaration",
			"entity_name":   "module.checkout",
			"start_line":    13,
			"end_line":      13,
			"language":      "hcl",
			"entity_metadata": map[string]any{
				"declaration_kind":             "terraform_module",
				"escalation_policy":            incidentRoutingEvidenceFamilyEscalationPolicy,
				"escalation_policy_resolution": "literal",
				"module_name":                  "checkout",
				"module_source_fingerprint":    "b6e0e0110eeff737",
				"module_source_redacted":       true,
				"outcome":                      "declared",
				"path":                         "pagerduty/main.tf",
				"redaction_state":              "none",
				"service_name":                 incidentRoutingEvidenceFamilyServiceName,
				"service_name_resolution":      "literal",
				"source_class":                 "declared",
				"source_revision":              "unknown",
			},
		},
	}
}

// IncidentRoutingEvidenceFamilyOdu builds the cataloged Odù for the
// incident_routing_evidence direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason S3LogsToFamilyOdu does: a failure means the
// payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func IncidentRoutingEvidenceFamilyOdu() CatalogOdu {
	serviceID := incidentRoutingEvidenceFamilyServiceID
	serviceName := incidentRoutingEvidenceFamilyServiceName
	serviceURL := "https://pagerduty.example.com/services/" + incidentRoutingEvidenceFamilyServiceID
	incidentURL := "https://pagerduty.example.com/incidents/" + incidentRoutingEvidenceFamilyIncidentID
	escalationPolicy := incidentRoutingEvidenceFamilyEscalationPolicy

	incidentPayload, err := factschema.EncodeIncidentRecord(incidentv1.IncidentRecord{
		Provider:           "pagerduty",
		ProviderIncidentID: incidentRoutingEvidenceFamilyIncidentID,
		ServiceID:          &serviceID,
		Service: &incidentv1.ServiceReference{
			ID:      &serviceID,
			Summary: &serviceName,
			URL:     &serviceURL,
		},
		SourceURL: &incidentURL,
	})
	if err != nil {
		panic(fmt.Sprintf(
			"familyodu: catalog_seed %s: encode incident.record payload: %v",
			IncidentRoutingEvidenceFamilyOduName, err,
		))
	}

	appliedPayload, err := factschema.EncodeIncidentRoutingAppliedPagerDutyResource(incidentv1.AppliedPagerDutyResource{
		SourceClass:               "applied",
		SourceKind:                "terraform_state",
		Outcome:                   "exact",
		ResourceClass:             "service",
		TerraformStateAddress:     "pagerduty_service.checkout",
		ResourceType:              "pagerduty_service",
		ResourceName:              "checkout",
		ModuleAddress:             "module.pagerduty",
		ProviderAddress:           "registry.terraform.io/pagerduty/pagerduty",
		ScopeID:                   incidentRoutingEvidenceFamilyScopeID,
		StateGenerationID:         "tfstate-gen-6228",
		StateLineage:              "lineage-6228",
		BackendKind:               "s3",
		LocatorHash:               "hash-6228",
		DeclaredMatchState:        "matched",
		RedactionState:            "none",
		ProviderObjectID:          &serviceID,
		EscalationPolicyReference: &escalationPolicy,
	})
	if err != nil {
		panic(fmt.Sprintf(
			"familyodu: catalog_seed %s: encode applied pagerduty resource payload: %v",
			IncidentRoutingEvidenceFamilyOduName, err,
		))
	}

	status := "active"
	observedPayload, err := factschema.EncodeIncidentRoutingObservedPagerDutyService(incidentv1.ObservedPagerDutyService{
		Provider:                  "pagerduty",
		SourceClass:               "observed",
		SourceKind:                "pagerduty_api",
		Outcome:                   "exact",
		ResourceClass:             "service",
		ProviderObjectID:          incidentRoutingEvidenceFamilyServiceID,
		ScopeID:                   incidentRoutingEvidenceFamilyScopeID,
		DeclaredMatchState:        "matched",
		RedactionState:            "none",
		ServiceID:                 incidentRoutingEvidenceFamilyServiceID,
		Status:                    &status,
		EscalationPolicyReference: &escalationPolicy,
	})
	if err != nil {
		panic(fmt.Sprintf(
			"familyodu: catalog_seed %s: encode observed pagerduty service payload: %v",
			IncidentRoutingEvidenceFamilyOduName, err,
		))
	}

	factsForOdu := []facts.Envelope{
		{
			ScopeID:          incidentRoutingEvidenceFamilyScopeID,
			GenerationID:     incidentRoutingEvidenceFamilyGenerationID,
			FactKind:         factsincident.IncidentRecordFactKind,
			StableFactKey:    incidentRoutingEvidenceFamilyStableFactKey(factsincident.IncidentRecordFactKind, incidentRoutingEvidenceFamilyIncidentID),
			SchemaVersion:    factsincident.IncidentContextSchemaVersionV1,
			CollectorKind:    incidentRoutingEvidenceFamilyCollectorKind,
			SourceConfidence: incidentRoutingEvidenceFamilySourceConfidence,
			Payload:          incidentPayload,
		},
		{
			ScopeID:          incidentRoutingEvidenceFamilyScopeID,
			GenerationID:     incidentRoutingEvidenceFamilyGenerationID,
			FactKind:         factsincident.IncidentRoutingAppliedPagerDutyResourceFactKind,
			StableFactKey:    incidentRoutingEvidenceFamilyStableFactKey(factsincident.IncidentRoutingAppliedPagerDutyResourceFactKind, incidentRoutingEvidenceFamilyServiceID),
			SchemaVersion:    factsincident.IncidentRoutingSchemaVersionV1,
			CollectorKind:    incidentRoutingEvidenceFamilyAppliedCollectorKind,
			SourceConfidence: incidentRoutingEvidenceFamilySourceConfidence,
			Payload:          appliedPayload,
		},
		{
			// NEGATIVE CONTROL: a tombstoned duplicate of the applied fact.
			// The production builder skips tombstones before decoding, so
			// this envelope must contribute no packet and no edge; the
			// guard's exact-set comparison proves it contributed none. A
			// regression that stopped skipping tombstones would project a
			// second applied candidate, flip the slot ambiguous, and red
			// the guard on zero rows versus three expected.
			ScopeID:          incidentRoutingEvidenceFamilyScopeID,
			GenerationID:     incidentRoutingEvidenceFamilyGenerationID,
			FactKind:         factsincident.IncidentRoutingAppliedPagerDutyResourceFactKind,
			StableFactKey:    incidentRoutingEvidenceFamilyStableFactKey(factsincident.IncidentRoutingAppliedPagerDutyResourceFactKind, incidentRoutingEvidenceFamilyServiceID+":tombstoned"),
			SchemaVersion:    factsincident.IncidentRoutingSchemaVersionV1,
			CollectorKind:    incidentRoutingEvidenceFamilyAppliedCollectorKind,
			SourceConfidence: incidentRoutingEvidenceFamilySourceConfidence,
			IsTombstone:      true,
			Payload:          appliedPayload,
		},
		{
			ScopeID:          incidentRoutingEvidenceFamilyScopeID,
			GenerationID:     incidentRoutingEvidenceFamilyGenerationID,
			FactKind:         factsincident.IncidentRoutingObservedPagerDutyServiceFactKind,
			StableFactKey:    incidentRoutingEvidenceFamilyStableFactKey(factsincident.IncidentRoutingObservedPagerDutyServiceFactKind, incidentRoutingEvidenceFamilyServiceID),
			SchemaVersion:    factsincident.IncidentRoutingSchemaVersionV1,
			CollectorKind:    incidentRoutingEvidenceFamilyCollectorKind,
			SourceConfidence: incidentRoutingEvidenceFamilySourceConfidence,
			Payload:          observedPayload,
		},
		incidentRoutingEvidenceFamilyDeclaredContentEntityFact(),
	}

	return CatalogOdu{
		Odu:    Odu{Name: IncidentRoutingEvidenceFamilyOduName, Facts: factsForOdu},
		Detail: "five facts for the direct-materialization incident_routing_evidence family: one incident.record anchor, one applied PagerDuty service resource, one observed PagerDuty service, one tombstoned duplicate applied fact proving tombstones contribute no edge, and one PagerDutyDeclaration content_entity fact the live drive projects into content_entities so the production loader resolves the intended slot, so the HAS_INTENDED_ROUTING / HAS_APPLIED_ROUTING / HAS_LIVE_ROUTING expected set proves exact three-slot convergence and tombstone restraint",
	}
}
