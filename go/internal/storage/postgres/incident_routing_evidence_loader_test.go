// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestFactStoreLoadIncidentRoutingEvidenceBuildsInputsFromFactsAndDeclarations(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 1, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				incidentRoutingFactRow(
					"incident-fact-1",
					"pagerduty:account:example",
					"pagerduty:generation-1",
					facts.IncidentRecordFactKind,
					"pagerduty",
					observedAt,
					false,
					map[string]any{
						"provider":             "pagerduty",
						"provider_incident_id": "PINCIDENT1",
						"service_id":           "PSERVICE1",
						"service": map[string]any{
							"id":      "PSERVICE1",
							"summary": "Checkout API",
							"url":     "https://example.pagerduty.com/services/PSERVICE1",
						},
						"source_url": "https://example.pagerduty.com/incidents/PINCIDENT1",
					},
				),
				incidentRoutingFactRow(
					"applied-fact-1",
					"pagerduty:account:example",
					"pagerduty:generation-1",
					facts.IncidentRoutingAppliedPagerDutyResourceFactKind,
					"terraform_state",
					observedAt.Add(time.Second),
					false,
					map[string]any{
						"source_class":                 "applied",
						"source_kind":                  "terraform_state",
						"outcome":                      "applied",
						"resource_class":               "service",
						"provider_object_id":           "PSERVICE1",
						"name_fingerprint":             "checkout-hash",
						"escalation_policy_reference":  "PEP1",
						"terraform_state_address":      "pagerduty_service.checkout",
						"provider_address":             "registry.terraform.io/pagerduty/pagerduty",
						"module_address":               "module.pagerduty",
						"state_generation_id":          "tfstate-gen-1",
						"declared_match_state":         "matched",
						"redaction_state":              "redacted",
						"ignored_non_service_resource": "kept out of reducer input",
					},
				),
				incidentRoutingFactRow(
					"applied-team-fact-1",
					"pagerduty:account:example",
					"pagerduty:generation-1",
					facts.IncidentRoutingAppliedPagerDutyResourceFactKind,
					"terraform_state",
					observedAt.Add(2*time.Second),
					false,
					map[string]any{
						"source_class":       "applied",
						"source_kind":        "terraform_state",
						"outcome":            "applied",
						"resource_class":     "team",
						"provider_object_id": "PTEAM1",
					},
				),
				incidentRoutingFactRow(
					"observed-fact-1",
					"pagerduty:account:example",
					"pagerduty:generation-1",
					facts.IncidentRoutingObservedPagerDutyServiceFactKind,
					"pagerduty",
					observedAt.Add(3*time.Second),
					false,
					map[string]any{
						"source_class":                 "observed",
						"source_kind":                  "pagerduty_api",
						"outcome":                      "observed",
						"service_id":                   "PSERVICE1",
						"provider_object_id":           "PSERVICE1",
						"name_fingerprint":             "sha256:checkout",
						"status":                       "active",
						"escalation_policy_reference":  "PEP1",
						"declared_match_state":         "matched",
						"drift_candidate_reason":       "",
						"redaction_state":              "redacted",
						"source_url":                   "https://example.pagerduty.com/services/PSERVICE1",
						"disabled":                     false,
						"deleted":                      false,
						"manually_created":             false,
						"ignored_observed_extra_field": "kept out of reducer input",
					},
				),
				incidentRoutingFactRow(
					"warning-fact-1",
					"pagerduty:account:example",
					"pagerduty:generation-1",
					facts.IncidentRoutingCoverageWarningFactKind,
					"pagerduty",
					observedAt.Add(4*time.Second),
					false,
					map[string]any{
						"source_class":       "observed",
						"source_kind":        "pagerduty_api",
						"reason":             "permission_hidden",
						"resource_class":     "service",
						"provider_object_id": "PSERVICE1",
					},
				),
			}},
			{rows: [][]any{{
				"declared-entity-1",
				"repo-observability",
				"pagerduty/main.tf",
				"pagerduty_service.checkout",
				17,
				mustIncidentRoutingJSON(t, map[string]any{
					"source_class":            "declared",
					"outcome":                 "declared",
					"declaration_kind":        "terraform_module",
					"service_name":            "Checkout API",
					"service_name_resolution": "literal",
					"escalation_policy":       "PEP1",
					"environment":             "prod",
					"workspace":               "prod",
					"redaction_state":         "redacted",
					"duplicate_service_name":  false,
				}),
			}}},
		},
	}
	store := NewFactStore(db)

	inputs, err := store.LoadIncidentRoutingEvidence(
		context.Background(),
		"pagerduty:account:example",
		"pagerduty:generation-1",
	)
	if err != nil {
		t.Fatalf("LoadIncidentRoutingEvidence() error = %v, want nil", err)
	}
	if got, want := len(inputs), 1; got != want {
		t.Fatalf("len(inputs) = %d, want %d", got, want)
	}
	input := inputs[0]
	if got, want := input.Incident.ProviderIncidentID, "PINCIDENT1"; got != want {
		t.Fatalf("incident provider id = %q, want %q", got, want)
	}
	if got, want := input.Incident.ServiceName, "Checkout API"; got != want {
		t.Fatalf("incident service name = %q, want %q", got, want)
	}
	if got, want := input.Incident.ServiceURL, "https://example.pagerduty.com/services/PSERVICE1"; got != want {
		t.Fatalf("incident service url = %q, want %q", got, want)
	}
	if got, want := len(input.Declared), 1; got != want {
		t.Fatalf("declared evidence count = %d, want %d", got, want)
	}
	if got, want := input.Declared[0].ServiceName, "Checkout API"; got != want {
		t.Fatalf("declared service name = %q, want %q", got, want)
	}
	if got, want := len(input.Applied), 1; got != want {
		t.Fatalf("applied evidence count = %d, want only service resource", got)
	}
	if got, want := input.Applied[0].ProviderObjectID, "PSERVICE1"; got != want {
		t.Fatalf("applied provider object id = %q, want %q", got, want)
	}
	if got, want := len(input.Observed), 1; got != want {
		t.Fatalf("observed evidence count = %d, want %d", got, want)
	}
	if got, want := input.Observed[0].Status, "active"; got != want {
		t.Fatalf("observed status = %q, want %q", got, want)
	}
	if got, want := len(input.Warnings), 1; got != want {
		t.Fatalf("warning count = %d, want %d", got, want)
	}
	if got, want := input.Warnings[0].Reason, "permission_hidden"; got != want {
		t.Fatalf("warning reason = %q, want %q", got, want)
	}

	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want facts + declarations", got)
	}
	if !strings.Contains(db.queries[1].query, "FROM content_entities") {
		t.Fatalf("declaration query missing content_entities:\n%s", db.queries[1].query)
	}
	if !strings.Contains(db.queries[1].query, "entity_type = 'PagerDutyDeclaration'") {
		t.Fatalf("declaration query missing PagerDutyDeclaration filter:\n%s", db.queries[1].query)
	}
	if !strings.Contains(db.queries[1].query, "metadata->>'source_class' = 'declared'") {
		t.Fatalf("declaration query missing declared source filter:\n%s", db.queries[1].query)
	}
	if !strings.Contains(db.queries[1].query, "ANY($1::text[])") {
		t.Fatalf("declaration query missing bounded service allowlist:\n%s", db.queries[1].query)
	}
	serviceNames, ok := db.queries[1].args[0].([]string)
	if !ok {
		t.Fatalf("declaration service-name arg type = %T, want []string", db.queries[1].args[0])
	}
	if got, want := strings.Join(serviceNames, ","), "checkout api"; got != want {
		t.Fatalf("declaration service-name arg = %q, want %q", got, want)
	}
}

func TestFactStoreLoadIncidentRoutingEvidenceSkipsDeclarationReadWithoutIncidentAnchor(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				incidentRoutingFactRow(
					"observed-fact-1",
					"pagerduty:account:example",
					"pagerduty:generation-1",
					facts.IncidentRoutingObservedPagerDutyServiceFactKind,
					"pagerduty",
					time.Date(2026, time.June, 1, 11, 0, 0, 0, time.UTC),
					false,
					map[string]any{
						"source_class":       "observed",
						"source_kind":        "pagerduty_api",
						"resource_class":     "service",
						"provider_object_id": "PSERVICE1",
						"service_id":         "PSERVICE1",
					},
				),
			}},
		},
	}
	store := NewFactStore(db)

	inputs, err := store.LoadIncidentRoutingEvidence(
		context.Background(),
		"pagerduty:account:example",
		"pagerduty:generation-1",
	)
	if err != nil {
		t.Fatalf("LoadIncidentRoutingEvidence() error = %v, want nil", err)
	}
	if len(inputs) != 0 {
		t.Fatalf("inputs = %#v, want no incident-routing packets without incident.record anchor", inputs)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want only fact query", got)
	}
}

func incidentRoutingFactRow(
	factID string,
	scopeID string,
	generationID string,
	factKind string,
	sourceSystem string,
	observedAt time.Time,
	tombstone bool,
	payload map[string]any,
) []any {
	return []any{
		factID,
		scopeID,
		generationID,
		factKind,
		factKind + ":" + factID,
		"1.0.0",
		sourceSystem,
		int64(0),
		"reported",
		sourceSystem,
		"source-key-" + factID,
		"https://example.pagerduty.com/source/" + factID,
		"source-record-" + factID,
		observedAt,
		tombstone,
		mustIncidentRoutingJSON(nil, payload),
	}
}

func mustIncidentRoutingJSON(t *testing.T, value any) []byte {
	if t != nil {
		t.Helper()
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		if t == nil {
			panic(err)
		}
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return encoded
}
