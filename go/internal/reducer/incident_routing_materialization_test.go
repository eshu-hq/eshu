// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
)

type recordingIncidentRoutingEvidenceWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
}

func (w *recordingIncidentRoutingEvidenceWriter) WriteIncidentRoutingEvidence(
	_ context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	w.writeCalls++
	w.writtenRows = append(w.writtenRows, rows...)
	w.writeScopeID = scopeID
	w.writeGenerationID = generationID
	w.writeEvidence = evidenceSource
	return nil
}

func (w *recordingIncidentRoutingEvidenceWriter) RetractIncidentRoutingEvidence(
	_ context.Context,
	scopeIDs []string,
	_ string,
	evidenceSource string,
) error {
	w.retractCalls++
	w.retractScopeIDs = append(w.retractScopeIDs, scopeIDs...)
	w.retractEvidence = evidenceSource
	return nil
}

type stubIncidentRoutingEvidenceLoader struct {
	raw IncidentRoutingRawEvidence
	err error
}

func (l stubIncidentRoutingEvidenceLoader) LoadIncidentRoutingRawEvidence(
	context.Context,
	string,
	string,
) (IncidentRoutingRawEvidence, error) {
	if l.err != nil {
		return IncidentRoutingRawEvidence{}, l.err
	}
	return l.raw, nil
}

func incidentRoutingMaterializationIntent() Intent {
	now := time.Now()
	return Intent{
		IntentID:     "intent-pagerduty-routing-1",
		ScopeID:      "scope-pagerduty",
		GenerationID: "gen-pagerduty",
		Domain:       DomainIncidentRoutingMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
	}
}

func exactIncidentRoutingInput() IncidentRoutingEvidenceInput {
	return IncidentRoutingEvidenceInput{
		Incident: IncidentRoutingIncident{
			Provider:           "pagerduty",
			ProviderIncidentID: "PINCIDENT1",
			ScopeID:            "scope-pagerduty",
			ServiceID:          "PSERVICE1",
			ServiceName:        "Checkout API",
			EvidenceFactID:     "incident-fact-1",
		},
		Declared: []IncidentRoutingDeclaredEvidence{{
			EntityID:              "declared-entity-1",
			RepoID:                "repo-observability",
			RelativePath:          "pagerduty/main.tf",
			DeclarationKind:       "pagerduty_service",
			SourceClass:           "declared",
			Outcome:               "exact",
			ServiceName:           "Checkout API",
			ServiceNameResolution: "literal",
			Environment:           "prod",
			Workspace:             "prod",
			RedactionState:        "redacted",
		}},
		Applied: []IncidentRoutingAppliedEvidence{{
			FactID:                "applied-fact-1",
			SourceClass:           "applied",
			SourceKind:            "terraform_state",
			Outcome:               "exact",
			ResourceClass:         "service",
			ProviderObjectID:      "PSERVICE1",
			TerraformStateAddress: "pagerduty_service.checkout",
			ProviderAddress:       "registry.terraform.io/pagerduty/pagerduty",
			ModuleAddress:         "module.pagerduty",
			StateGenerationID:     "tfstate-gen-1",
			DeclaredMatchState:    "matched",
			RedactionState:        "redacted",
		}},
		Observed: []IncidentRoutingObservedEvidence{{
			FactID:                    "observed-fact-1",
			SourceClass:               "observed",
			SourceKind:                "pagerduty_api",
			Outcome:                   "exact",
			ServiceID:                 "PSERVICE1",
			ProviderObjectID:          "PSERVICE1",
			Status:                    "active",
			EscalationPolicyReference: "PEP1",
			DeclaredMatchState:        "matched",
			RedactionState:            "redacted",
		}},
	}
}

// exactIncidentRoutingRawEvidence builds the raw fact envelopes (encoded through
// the typed contracts seam) plus declared evidence that decode back into the
// exactIncidentRoutingInput scenario, so the handler test exercises the real
// load -> decode -> extract path rather than a hand-built typed input. The
// service_id/service and provider fields are set so the decoded incident carries
// the same ServiceName and ServiceID the pre-typing loader produced.
func exactIncidentRoutingRawEvidence(t *testing.T) IncidentRoutingRawEvidence {
	t.Helper()

	incidentPayload, err := factschema.EncodeIncidentRecord(incidentv1.IncidentRecord{
		Provider:           "pagerduty",
		ProviderIncidentID: "PINCIDENT1",
		ServiceID:          stringPtr("PSERVICE1"),
		Service: &incidentv1.ServiceReference{
			ID:      stringPtr("PSERVICE1"),
			Summary: stringPtr("Checkout API"),
		},
	})
	if err != nil {
		t.Fatalf("EncodeIncidentRecord: %v", err)
	}
	appliedPayload, err := factschema.EncodeIncidentRoutingAppliedPagerDutyResource(incidentv1.AppliedPagerDutyResource{
		SourceClass:           "applied",
		SourceKind:            "terraform_state",
		Outcome:               "exact",
		ResourceClass:         "service",
		TerraformStateAddress: "pagerduty_service.checkout",
		ResourceType:          "pagerduty_service",
		ResourceName:          "checkout",
		ModuleAddress:         "module.pagerduty",
		ProviderAddress:       "registry.terraform.io/pagerduty/pagerduty",
		ScopeID:               "scope-pagerduty",
		StateGenerationID:     "tfstate-gen-1",
		StateLineage:          "lineage-1",
		BackendKind:           "s3",
		LocatorHash:           "hash-1",
		DeclaredMatchState:    "matched",
		RedactionState:        "redacted",
		ProviderObjectID:      stringPtr("PSERVICE1"),
	})
	if err != nil {
		t.Fatalf("EncodeIncidentRoutingAppliedPagerDutyResource: %v", err)
	}
	observedPayload, err := factschema.EncodeIncidentRoutingObservedPagerDutyService(incidentv1.ObservedPagerDutyService{
		Provider:                  "pagerduty",
		SourceClass:               "observed",
		SourceKind:                "pagerduty_api",
		Outcome:                   "exact",
		ResourceClass:             "service",
		ProviderObjectID:          "PSERVICE1",
		ScopeID:                   "scope-pagerduty",
		DeclaredMatchState:        "matched",
		RedactionState:            "redacted",
		ServiceID:                 "PSERVICE1",
		Status:                    stringPtr("active"),
		EscalationPolicyReference: stringPtr("PEP1"),
	})
	if err != nil {
		t.Fatalf("EncodeIncidentRoutingObservedPagerDutyService: %v", err)
	}

	return IncidentRoutingRawEvidence{
		Facts: []facts.Envelope{
			{FactID: "incident-fact-1", ScopeID: "scope-pagerduty", FactKind: facts.IncidentRecordFactKind, SchemaVersion: "1.0.0", Payload: incidentPayload},
			{FactID: "applied-fact-1", ScopeID: "scope-pagerduty", FactKind: facts.IncidentRoutingAppliedPagerDutyResourceFactKind, SchemaVersion: "1.0.0", Payload: appliedPayload},
			{FactID: "observed-fact-1", ScopeID: "scope-pagerduty", FactKind: facts.IncidentRoutingObservedPagerDutyServiceFactKind, SchemaVersion: "1.0.0", Payload: observedPayload},
		},
		Declared: []IncidentRoutingDeclaredEvidence{{
			EntityID:              "declared-entity-1",
			RepoID:                "repo-observability",
			RelativePath:          "pagerduty/main.tf",
			DeclarationKind:       "pagerduty_service",
			SourceClass:           "declared",
			Outcome:               "exact",
			ServiceName:           "Checkout API",
			ServiceNameResolution: "literal",
			Environment:           "prod",
			Workspace:             "prod",
			RedactionState:        "redacted",
		}},
	}
}

func stringPtr(value string) *string {
	return &value
}

func TestExtractIncidentRoutingEvidenceRowsProjectsExactSlots(t *testing.T) {
	t.Parallel()

	rows, tally := ExtractIncidentRoutingEvidenceRows(exactIncidentRoutingInput())
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want declared+applied+observed routing evidence", len(rows))
	}
	indexed := indexIncidentRoutingRows(rows)
	for _, slot := range []string{"intended_routing", "applied_routing", "live_routing"} {
		row, ok := indexed[slot]
		if !ok {
			t.Fatalf("missing materialized slot %q in rows %#v", slot, rows)
		}
		if anyToString(row["uid"]) == "" || anyToString(row["incident_uid"]) == "" {
			t.Fatalf("slot %q missing deterministic uids: %#v", slot, row)
		}
		if got := anyToString(row["truth_label"]); got != "exact" {
			t.Fatalf("slot %q truth_label = %q, want exact", slot, got)
		}
	}
	if got := anyToString(indexed["intended_routing"]["source_class"]); got != "declared" {
		t.Fatalf("intended source_class = %q, want declared", got)
	}
	if got := anyToString(indexed["applied_routing"]["source_class"]); got != "applied" {
		t.Fatalf("applied source_class = %q, want applied", got)
	}
	if got := anyToString(indexed["live_routing"]["source_class"]); got != "observed" {
		t.Fatalf("live source_class = %q, want observed", got)
	}
	for _, row := range rows {
		for _, forbidden := range []string{
			"deployable_unit_id",
			"image_digest",
			"commit_sha",
			"pull_request_id",
			"work_item_id",
		} {
			if _, exists := row[forbidden]; exists {
				t.Fatalf("routing row over-promoted downstream field %q: %#v", forbidden, row)
			}
		}
	}
	if tally.materialized["exact"] != 3 {
		t.Fatalf("materialized exact tally = %d, want 3", tally.materialized["exact"])
	}
}

func TestExtractIncidentRoutingEvidenceRowsProjectsLiveOnlyNoIaC(t *testing.T) {
	t.Parallel()

	input := exactIncidentRoutingInput()
	input.Declared = nil
	input.Applied = nil

	rows, tally := ExtractIncidentRoutingEvidenceRows(input)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want one live-only PagerDuty routing evidence row", len(rows))
	}
	row := rows[0]
	if got := anyToString(row["slot"]); got != "live_routing" {
		t.Fatalf("slot = %q, want live_routing", got)
	}
	if got := anyToString(row["source_class"]); got != "observed" {
		t.Fatalf("source_class = %q, want observed", got)
	}
	if tally.skipped["missing"] != 2 {
		t.Fatalf("missing tally = %d, want declared+applied slots counted missing", tally.skipped["missing"])
	}
}

func TestExtractIncidentRoutingEvidenceRowsDoesNotPromoteUnsafeOutcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mutate      func(*IncidentRoutingEvidenceInput)
		wantOutcome string
	}{
		{
			name: "drifted",
			mutate: func(input *IncidentRoutingEvidenceInput) {
				input.Declared = nil
				input.Applied[0].EscalationPolicyReference = "PEP-OLD"
				input.Observed[0].EscalationPolicyReference = "PEP-NEW"
			},
			wantOutcome: "drifted",
		},
		{
			name: "stale",
			mutate: func(input *IncidentRoutingEvidenceInput) {
				input.Declared = nil
				input.Applied = nil
				input.Observed[0].Deleted = true
			},
			wantOutcome: "stale",
		},
		{
			name: "permission hidden",
			mutate: func(input *IncidentRoutingEvidenceInput) {
				input.Declared = nil
				input.Applied = nil
				input.Observed = nil
				input.Warnings = []IncidentRoutingCoverageWarning{{
					FactID:           "warning-1",
					SourceClass:      "observed",
					SourceKind:       "pagerduty_api",
					Reason:           "permission_hidden",
					ResourceClass:    "service",
					ProviderObjectID: "PSERVICE1",
				}}
			},
			wantOutcome: "permission_hidden",
		},
		{
			name: "ambiguous",
			mutate: func(input *IncidentRoutingEvidenceInput) {
				input.Applied = nil
				input.Observed = nil
				input.Declared = append(input.Declared, input.Declared[0])
				input.Declared[1].EntityID = "declared-entity-2"
			},
			wantOutcome: "ambiguous",
		},
		{
			name: "unresolved",
			mutate: func(input *IncidentRoutingEvidenceInput) {
				input.Declared = nil
				input.Applied = nil
				input.Observed = nil
				input.Warnings = []IncidentRoutingCoverageWarning{{
					FactID:           "warning-2",
					SourceClass:      "observed",
					SourceKind:       "pagerduty_api",
					Reason:           "unresolved",
					ResourceClass:    "service",
					ProviderObjectID: "PSERVICE1",
				}}
			},
			wantOutcome: "unresolved",
		},
		{
			name: "rejected",
			mutate: func(input *IncidentRoutingEvidenceInput) {
				input.Declared = nil
				input.Applied = nil
				input.Observed[0].Outcome = "rejected"
			},
			wantOutcome: "rejected",
		},
		{
			name: "missing",
			mutate: func(input *IncidentRoutingEvidenceInput) {
				input.Declared = nil
				input.Applied = nil
				input.Observed = nil
			},
			wantOutcome: "missing",
		},
		{
			name: "name fingerprint only without incident service id",
			mutate: func(input *IncidentRoutingEvidenceInput) {
				input.Incident.ServiceID = ""
				input.Applied[0].ProviderObjectID = ""
				input.Applied[0].NameFingerprint = incidentRoutingShortFingerprint(input.Incident.ServiceName)
				input.Observed[0].ServiceID = ""
				input.Observed[0].ProviderObjectID = ""
				input.Observed[0].NameFingerprint = incidentRoutingConfigFingerprint(input.Incident.ServiceName)
			},
			wantOutcome: "derived",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := exactIncidentRoutingInput()
			tt.mutate(&input)

			rows, tally := ExtractIncidentRoutingEvidenceRows(input)
			if len(rows) != 0 {
				t.Fatalf("%s produced %d graph row(s), want 0: %#v", tt.name, len(rows), rows)
			}
			if tally.skipped[tt.wantOutcome] == 0 {
				t.Fatalf("skipped[%q] = 0, want outcome counted in %#v", tt.wantOutcome, tally.skipped)
			}
		})
	}
}

func TestIncidentRoutingMaterializationHandlerWritesAndRetracts(t *testing.T) {
	t.Parallel()

	writer := &recordingIncidentRoutingEvidenceWriter{}
	handler := IncidentRoutingMaterializationHandler{
		Loader:               stubIncidentRoutingEvidenceLoader{raw: exactIncidentRoutingRawEvidence(t)},
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), incidentRoutingMaterializationIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.SubSignals["input_invalid_facts"] != 0 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 0 for all-valid facts", result.SubSignals["input_invalid_facts"])
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 for prior generation", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if len(writer.writtenRows) != 3 {
		t.Fatalf("written rows = %d, want 3", len(writer.writtenRows))
	}
	if writer.writeEvidence != incidentRoutingEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, incidentRoutingEvidenceSource)
	}
	if writer.writeScopeID != "scope-pagerduty" || writer.writeGenerationID != "gen-pagerduty" {
		t.Fatalf("write scope/generation = %q/%q, want scope-pagerduty/gen-pagerduty",
			writer.writeScopeID, writer.writeGenerationID)
	}
	if result.CanonicalWrites != 3 {
		t.Fatalf("CanonicalWrites = %d, want 3", result.CanonicalWrites)
	}
}

func indexIncidentRoutingRows(rows []map[string]any) map[string]map[string]any {
	indexed := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		indexed[anyToString(row["slot"])] = row
	}
	return indexed
}
