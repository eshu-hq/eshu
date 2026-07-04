// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
)

func benchIncidentRecord() incidentv1.IncidentRecord {
	return incidentv1.IncidentRecord{
		Provider:           "pagerduty",
		ProviderIncidentID: "PINCIDENT1",
		ServiceID:          stringPtr("PSERVICE1"),
		Service: &incidentv1.ServiceReference{
			ID:      stringPtr("PSERVICE1"),
			Summary: stringPtr("Checkout API"),
			URL:     stringPtr("https://example.pagerduty.com/services/PSERVICE1"),
		},
		Status: stringPtr("triggered"),
	}
}

func benchAppliedResource() incidentv1.AppliedPagerDutyResource {
	return incidentv1.AppliedPagerDutyResource{
		SourceClass:           "applied",
		SourceKind:            "terraform_state",
		Outcome:               "applied",
		ResourceClass:         "service",
		TerraformStateAddress: "pagerduty_service.checkout",
		ResourceType:          "pagerduty_service",
		ResourceName:          "checkout",
		ModuleAddress:         "module.pagerduty",
		ProviderAddress:       "registry.terraform.io/pagerduty/pagerduty",
		ScopeID:               "scope",
		StateGenerationID:     "tfstate-gen-1",
		StateLineage:          "lineage-1",
		BackendKind:           "s3",
		LocatorHash:           "hash-1",
		DeclaredMatchState:    "matched",
		RedactionState:        "redacted",
		ProviderObjectID:      stringPtr("PSERVICE1"),
	}
}

func benchObservedService() incidentv1.ObservedPagerDutyService {
	return incidentv1.ObservedPagerDutyService{
		Provider:           "pagerduty",
		SourceClass:        "observed",
		SourceKind:         "pagerduty_api",
		Outcome:            "observed",
		ResourceClass:      "service",
		ProviderObjectID:   "PSERVICE1",
		ScopeID:            "scope",
		DeclaredMatchState: "matched",
		RedactionState:     "redacted",
		ServiceID:          "PSERVICE1",
		Status:             stringPtr("active"),
	}
}

func benchCoverageWarning() incidentv1.CoverageWarning {
	return incidentv1.CoverageWarning{
		SourceClass:        "observed",
		SourceKind:         "pagerduty_api",
		Outcome:            "partial",
		ResourceClass:      stringPtr("service"),
		ScopeID:            "scope",
		Reason:             "permission_hidden",
		RedactionState:     "none",
		DeclaredMatchState: "not_compared",
	}
}

// TestIncidentRoutingMaterializationQuarantinesMissingRequiredField is the
// flagship input_invalid regression test for the incident family (Contract
// System v1 §3.2, Wave 4a). It proves the accuracy guarantee AND the per-fact
// isolation contract: an incident_routing.applied_pagerduty_resource fact
// missing its required resource_class key is QUARANTINED as a visible
// input_invalid dead-letter (metric + log + the input_invalid_facts SubSignal)
// while every valid fact in the same batch still projects and the handler
// succeeds, so one malformed fact never stalls the scope generation.
//
// Before the migration this fact read resource_class with a raw payloadString in
// the storage loader, which returns "" for the absent key; incidentRoutingApplied-
// FromEnvelope then compared "" != "service" and SILENTLY dropped the fact — no
// error, no dead-letter, no operator signal. After the migration the reducer
// decodes each fact through factschema.DecodeIncidentRoutingAppliedPagerDutyResource;
// the malformed fact yields a classified *factschema.DecodeError that
// partitionDecodeFailures routes to a per-fact quarantine the handler records.
func TestIncidentRoutingMaterializationQuarantinesMissingRequiredField(t *testing.T) {
	t.Parallel()

	raw := exactIncidentRoutingRawEvidence(t)
	// A malformed applied-resource fact whose required resource_class key is
	// ABSENT (not merely empty). Everything else it needs is present, so the only
	// reason to quarantine it is the missing required field.
	malformedApplied := map[string]any{
		"source_class":            "applied",
		"source_kind":             "terraform_state",
		"outcome":                 "applied",
		"terraform_state_address": "pagerduty_service.broken",
		"resource_type":           "pagerduty_service",
		"resource_name":           "broken",
		"module_address":          "",
		"provider_address":        "registry.terraform.io/pagerduty/pagerduty",
		"scope_id":                "scope-pagerduty",
		"state_generation_id":     "tfstate-gen-1",
		"state_lineage":           "lineage-1",
		"backend_kind":            "s3",
		"locator_hash":            "hash-2",
		"declared_match_state":    "not_compared",
		"redaction_state":         "none",
		// "resource_class" intentionally absent.
	}
	raw.Facts = append(raw.Facts, facts.Envelope{
		FactID:        "applied-broken-1",
		ScopeID:       "scope-pagerduty",
		FactKind:      facts.IncidentRoutingAppliedPagerDutyResourceFactKind,
		SchemaVersion: "1.0.0",
		Payload:       malformedApplied,
	})

	writer := &recordingIncidentRoutingEvidenceWriter{}
	handler := IncidentRoutingMaterializationHandler{
		Loader:               stubIncidentRoutingEvidenceLoader{raw: raw},
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), incidentRoutingMaterializationIntent())
	// Per-fact isolation: the malformed fact does NOT fail the whole intent.
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed applied fact must be quarantined per-fact, not fail the whole intent", err)
	}
	// The malformed fact is recorded as one input_invalid quarantine.
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-resource_class fact must be one input_invalid quarantine", got)
	}
	// The batch's valid slots still materialize: isolation means a poisoned
	// sibling never suppresses valid graph truth.
	if len(writer.writtenRows) != 3 {
		t.Fatalf("written rows = %d, want 3; the valid intended/applied/live rows must still project despite the quarantined fact", len(writer.writtenRows))
	}
	if result.CanonicalWrites != 3 {
		t.Fatalf("CanonicalWrites = %d, want 3", result.CanonicalWrites)
	}
}

// TestIncidentRoutingMissingRequiredFieldDecodeError proves the raw decode of a
// resource_class-absent applied fact classifies as input_invalid naming exactly
// that field — the direct evidence that the per-fact quarantine above is driven
// by a real classified decode error, not an incidental skip.
func TestIncidentRoutingMissingRequiredFieldDecodeError(t *testing.T) {
	t.Parallel()

	env := facts.Envelope{
		FactKind:      facts.IncidentRoutingAppliedPagerDutyResourceFactKind,
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			"source_class":            "applied",
			"source_kind":             "terraform_state",
			"outcome":                 "applied",
			"terraform_state_address": "pagerduty_service.broken",
			"resource_type":           "pagerduty_service",
			"resource_name":           "broken",
			"module_address":          "",
			"provider_address":        "p",
			"scope_id":                "scope-pagerduty",
			"state_generation_id":     "g",
			"state_lineage":           "l",
			"backend_kind":            "s3",
			"locator_hash":            "h",
			"declared_match_state":    "not_compared",
			"redaction_state":         "none",
		},
	}
	_, err := decodeIncidentRoutingAppliedPagerDutyResource(env)
	if err == nil {
		t.Fatal("decode missing resource_class: error = nil, want a classified decode error")
	}
	var decodeErr *factschema.DecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("decode missing resource_class: error = %T, want *factschema.DecodeError", err)
	}
	if decodeErr.Classification != factschema.ClassificationInputInvalid {
		t.Fatalf("classification = %q, want %q", decodeErr.Classification, factschema.ClassificationInputInvalid)
	}
	if decodeErr.Field != "resource_class" {
		t.Fatalf("field = %q, want resource_class", decodeErr.Field)
	}
}

// BenchmarkBuildIncidentRoutingEvidenceInputs measures the typed-decode cost of
// mapping a realistic per-scope-generation batch of incident and routing fact
// envelopes into the reducer evidence model through the contracts seam. This is
// the work that replaced the storage layer's raw payloadString map reads; the
// benchmark backs the No-Regression Evidence note in AGENTS.md. The corpus is one
// incident plus one applied, one observed, and one warning fact per incident,
// repeated so the decode path, not fixture construction, dominates.
func BenchmarkBuildIncidentRoutingEvidenceInputs(b *testing.B) {
	const incidentCount = 500
	raw := benchmarkIncidentRoutingRawEvidence(b, incidentCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inputs, quarantined, err := buildIncidentRoutingEvidenceInputs(raw)
		if err != nil {
			b.Fatalf("buildIncidentRoutingEvidenceInputs() error = %v, want nil", err)
		}
		if len(inputs) != incidentCount {
			b.Fatalf("len(inputs) = %d, want %d", len(inputs), incidentCount)
		}
		if len(quarantined) != 0 {
			b.Fatalf("quarantined = %d, want 0 for all-valid corpus", len(quarantined))
		}
	}
}

func benchmarkIncidentRoutingRawEvidence(b *testing.B, incidentCount int) IncidentRoutingRawEvidence {
	b.Helper()

	incidentPayload := mustEncode(b, func() (map[string]any, error) {
		return factschema.EncodeIncidentRecord(benchIncidentRecord())
	})
	appliedPayload := mustEncode(b, func() (map[string]any, error) {
		return factschema.EncodeIncidentRoutingAppliedPagerDutyResource(benchAppliedResource())
	})
	observedPayload := mustEncode(b, func() (map[string]any, error) {
		return factschema.EncodeIncidentRoutingObservedPagerDutyService(benchObservedService())
	})
	warningPayload := mustEncode(b, func() (map[string]any, error) {
		return factschema.EncodeIncidentRoutingCoverageWarning(benchCoverageWarning())
	})

	envelopes := make([]facts.Envelope, 0, incidentCount*4)
	for i := 0; i < incidentCount; i++ {
		envelopes = append(
			envelopes,
			facts.Envelope{FactID: "incident", ScopeID: "scope", FactKind: facts.IncidentRecordFactKind, SchemaVersion: "1.0.0", Payload: incidentPayload},
			facts.Envelope{FactID: "applied", ScopeID: "scope", FactKind: facts.IncidentRoutingAppliedPagerDutyResourceFactKind, SchemaVersion: "1.0.0", Payload: appliedPayload},
			facts.Envelope{FactID: "observed", ScopeID: "scope", FactKind: facts.IncidentRoutingObservedPagerDutyServiceFactKind, SchemaVersion: "1.0.0", Payload: observedPayload},
			facts.Envelope{FactID: "warning", ScopeID: "scope", FactKind: facts.IncidentRoutingCoverageWarningFactKind, SchemaVersion: "1.0.0", Payload: warningPayload},
		)
	}
	return IncidentRoutingRawEvidence{Facts: envelopes}
}

func mustEncode(b *testing.B, encode func() (map[string]any, error)) map[string]any {
	b.Helper()
	payload, err := encode()
	if err != nil {
		b.Fatalf("encode payload: %v", err)
	}
	return payload
}

// TestCoverageWarningWithoutResourceClassProjects is the failing-first
// regression for the codex P1: the Terraform-state coverage_warning emitter
// (terraformstate.emitIncidentRoutingCoverageWarning) builds its payload from
// incidentRoutingBasePayload, which NEVER sets resource_class — it only sets
// source_class, source_kind, outcome, the state locator, scope_id,
// declared_match_state, and redaction_state, then the warning func adds reason.
// So a real Terraform-state coverage_warning fact carries NO resource_class.
//
// If resource_class is REQUIRED on the CoverageWarning struct, every such fact
// decodes as input_invalid and buildIncidentRoutingEvidenceInputs QUARANTINES
// it, silently dropping real coverage evidence from incident-routing
// materialization. This test builds exactly that Terraform-state-shaped payload
// (no resource_class) and asserts the warning PROJECTS: zero quarantined facts,
// and the warning is preserved in the built input for the incident to consume.
// It fails on the resource_class-required struct and passes once resource_class
// is optional.
func TestCoverageWarningWithoutResourceClassProjects(t *testing.T) {
	t.Parallel()

	// A valid incident anchor so buildIncidentRoutingEvidenceInputs produces a
	// packet at all (it returns no inputs without an incident.record).
	incidentPayload, err := factschema.EncodeIncidentRecord(incidentv1.IncidentRecord{
		Provider:           "pagerduty",
		ProviderIncidentID: "PINCIDENT1",
		ServiceID:          stringPtr("PSERVICE1"),
		Service:            &incidentv1.ServiceReference{ID: stringPtr("PSERVICE1"), Summary: stringPtr("Checkout API")},
	})
	if err != nil {
		t.Fatalf("EncodeIncidentRecord: %v", err)
	}

	// The exact shape emitIncidentRoutingCoverageWarning emits: the base
	// Terraform-state fields plus outcome/reason/redaction_state — and NO
	// resource_class. Built as a raw map (not via Encode) precisely because the
	// point is that the emitter never populates resource_class.
	tfStateWarningPayload := map[string]any{
		"source_class":            "applied",
		"source_kind":             "terraform_state",
		"outcome":                 "unsupported",
		"reason":                  "unsupported_pagerduty_resource",
		"redaction_state":         "none",
		"terraform_state_address": "pagerduty_unsupported.thing",
		"resource_type":           "pagerduty_unsupported",
		"resource_name":           "thing",
		"module_address":          "",
		"provider_address":        "registry.terraform.io/pagerduty/pagerduty",
		"scope_id":                "scope-pagerduty",
		"state_generation_id":     "tfstate-gen-1",
		"state_lineage":           "lineage-1",
		"backend_kind":            "s3",
		"locator_hash":            "hash-1",
		"declared_match_state":    "not_compared",
		// "resource_class" intentionally absent — the emitter never sets it.
	}

	raw := IncidentRoutingRawEvidence{
		Facts: []facts.Envelope{
			{FactID: "incident-fact-1", ScopeID: "scope-pagerduty", FactKind: facts.IncidentRecordFactKind, SchemaVersion: "1.0.0", Payload: incidentPayload},
			{FactID: "warning-fact-1", ScopeID: "scope-pagerduty", FactKind: facts.IncidentRoutingCoverageWarningFactKind, SchemaVersion: "1.0.0", Payload: tfStateWarningPayload},
		},
	}

	inputs, quarantined, err := buildIncidentRoutingEvidenceInputs(raw)
	if err != nil {
		t.Fatalf("buildIncidentRoutingEvidenceInputs error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d (%+v), want 0; a Terraform-state coverage_warning has no resource_class and must not be quarantined", len(quarantined), quarantined)
	}
	if len(inputs) != 1 {
		t.Fatalf("inputs = %d, want 1 (one incident packet)", len(inputs))
	}
	if len(inputs[0].Warnings) != 1 {
		t.Fatalf("warnings = %d, want 1; the coverage warning must project into the incident packet, not be dropped", len(inputs[0].Warnings))
	}
	if got := inputs[0].Warnings[0].Reason; got != "unsupported_pagerduty_resource" {
		t.Fatalf("warning reason = %q, want unsupported_pagerduty_resource", got)
	}
}

// TestAppliedResourceClassWhitespaceIsTrimmedBeforeServiceFilter guards the
// pre-typing behavior Copilot flagged: the storage loader's
// incidentRoutingPayloadString TRIMMED the value before the reducer compared
// resource_class == "service", so a padded "service " was admitted as a
// service-class applied resource. buildIncidentRoutingEvidenceInputs must trim
// the decoded resource_class the same way — comparing the raw decoded string
// would silently drop a padded fact that used to project. This test builds an
// applied_pagerduty_resource whose resource_class is "service " (trailing space)
// and asserts it still becomes applied evidence, not dropped.
func TestAppliedResourceClassWhitespaceIsTrimmedBeforeServiceFilter(t *testing.T) {
	t.Parallel()

	incidentPayload, err := factschema.EncodeIncidentRecord(incidentv1.IncidentRecord{
		Provider:           "pagerduty",
		ProviderIncidentID: "PINCIDENT1",
		ServiceID:          stringPtr("PSERVICE1"),
		Service:            &incidentv1.ServiceReference{ID: stringPtr("PSERVICE1"), Summary: stringPtr("Checkout API")},
	})
	if err != nil {
		t.Fatalf("EncodeIncidentRecord: %v", err)
	}
	// A padded resource_class value — the exact whitespace case. Built via Encode
	// so every other required field is present; then the padded value is set
	// directly on the payload map so the decode carries the untrimmed string.
	appliedPayload, err := factschema.EncodeIncidentRoutingAppliedPagerDutyResource(incidentv1.AppliedPagerDutyResource{
		SourceClass: "applied", SourceKind: "terraform_state", Outcome: "applied",
		ResourceClass:         "service ", // trailing space
		TerraformStateAddress: "pagerduty_service.checkout", ResourceType: "pagerduty_service",
		ResourceName: "checkout", ModuleAddress: "module.pagerduty",
		ProviderAddress: "registry.terraform.io/pagerduty/pagerduty", ScopeID: "scope-pagerduty",
		StateGenerationID: "tfstate-gen-1", StateLineage: "lineage-1", BackendKind: "s3",
		LocatorHash: "hash-1", DeclaredMatchState: "matched", RedactionState: "redacted",
		ProviderObjectID: stringPtr("PSERVICE1"),
	})
	if err != nil {
		t.Fatalf("EncodeIncidentRoutingAppliedPagerDutyResource: %v", err)
	}

	raw := IncidentRoutingRawEvidence{
		Facts: []facts.Envelope{
			{FactID: "incident-fact-1", ScopeID: "scope-pagerduty", FactKind: facts.IncidentRecordFactKind, SchemaVersion: "1.0.0", Payload: incidentPayload},
			{FactID: "applied-padded-1", ScopeID: "scope-pagerduty", FactKind: facts.IncidentRoutingAppliedPagerDutyResourceFactKind, SchemaVersion: "1.0.0", Payload: appliedPayload},
		},
	}

	inputs, quarantined, err := buildIncidentRoutingEvidenceInputs(raw)
	if err != nil {
		t.Fatalf("buildIncidentRoutingEvidenceInputs error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0", len(quarantined))
	}
	if len(inputs) != 1 {
		t.Fatalf("inputs = %d, want 1", len(inputs))
	}
	if len(inputs[0].Applied) != 1 {
		t.Fatalf("applied evidence = %d, want 1; a padded \"service \" resource_class must be trimmed and admitted like the pre-typing loader did, not dropped", len(inputs[0].Applied))
	}
}
