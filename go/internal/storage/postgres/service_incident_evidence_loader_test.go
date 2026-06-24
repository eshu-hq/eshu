// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestServiceIncidentEvidenceLoaderMapsExactProviderServiceToCatalogService(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{
					"component:default/checkout",
					"pagerduty",
					"INC-1",
					"applied_routing",
					"incident_routing.applied_pagerduty_resource",
					"stable-applied",
					"exact",
					"pd-svc-1",
					"matched",
					"allowed",
				},
				{
					"component:default/checkout",
					"pagerduty",
					"INC-1",
					"live_routing",
					"incident_routing.observed_pagerduty_service",
					"stable-live",
					"exact",
					"pd-svc-1",
					"",
					"partial",
				},
			}},
		},
	}
	loader := NewServiceIncidentEvidenceLoader(db)

	byService, err := loader.GetIncidentEvidenceForServices(
		context.Background(),
		[]string{"component:default/checkout"},
	)
	if err != nil {
		t.Fatalf("GetIncidentEvidenceForServices() error = %v, want nil", err)
	}
	records := byService["component:default/checkout"]
	if len(records) != 2 {
		t.Fatalf("incident records = %d, want 2", len(records))
	}

	applied := records[0]
	if applied.Provider != "pagerduty" || applied.ProviderIncidentID != "INC-1" {
		t.Fatalf("applied durable incident identity = %#v, want pagerduty/INC-1", applied)
	}
	if applied.Slot != "applied_routing" ||
		applied.EvidenceKind != "incident_routing.applied_pagerduty_resource" ||
		applied.EvidenceID != "stable-applied" {
		t.Fatalf("applied durable evidence identity = %#v, want stable applied routing identity", applied)
	}
	if applied.TruthLabel != "exact" ||
		applied.ProviderObjectID != "pd-svc-1" ||
		applied.DeclaredMatchState != "matched" ||
		applied.RedactionState != "allowed" {
		t.Fatalf("applied observable fields = %#v, want exact pd-svc-1 matched allowed", applied)
	}

	live := records[1]
	if live.Slot != "live_routing" ||
		live.EvidenceKind != "incident_routing.observed_pagerduty_service" ||
		live.EvidenceID != "stable-live" ||
		live.RedactionState != "partial" {
		t.Fatalf("live evidence row = %#v, want stable live routing row", live)
	}

	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want one bounded load", len(db.queries))
	}
	if got := db.queries[0].args[0]; got == nil {
		t.Fatal("query missing service id filter arg")
	}
}

func TestServiceIncidentEvidenceLoaderEmptyServicesIsNoOp(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	loader := NewServiceIncidentEvidenceLoader(db)
	byService, err := loader.GetIncidentEvidenceForServices(context.Background(), []string{"", "  "})
	if err != nil {
		t.Fatalf("GetIncidentEvidenceForServices() error = %v, want nil", err)
	}
	if byService != nil {
		t.Fatalf("GetIncidentEvidenceForServices() = %v, want nil for empty service set", byService)
	}
	if len(db.queries) != 0 {
		t.Fatalf("queries = %d, want no query for empty service set", len(db.queries))
	}
}

func TestServiceIncidentEvidenceQueryUsesDurableExactFailClosedJoin(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"reducer_service_catalog_correlation",
		"reducer_incident_repository_correlation",
		"incident_routing.applied_pagerduty_resource",
		"incident_routing.observed_pagerduty_service",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		"payload->>'provenance_only' = 'false'",
		"payload->>'outcome' IN ('exact', 'derived')",
		"HAVING COUNT(DISTINCT fact.payload->>'service_id') = 1",
		"stable_fact_key",
	} {
		if !strings.Contains(serviceIncidentEvidenceQuery, want) {
			t.Fatalf("serviceIncidentEvidenceQuery missing %q:\n%s", want, serviceIncidentEvidenceQuery)
		}
	}
}

func TestServiceIncidentEvidenceBoundedQueryAppliesRowLimit(t *testing.T) {
	t.Parallel()

	if !strings.Contains(serviceIncidentEvidenceBoundedQuery, "LIMIT $2") {
		t.Fatalf("bounded query must cap rows with LIMIT $2:\n%s", serviceIncidentEvidenceBoundedQuery)
	}
	// The bounded query is the unbounded join plus the row cap, so it keeps the
	// same durable fail-closed admissibility gate.
	if !strings.HasPrefix(serviceIncidentEvidenceBoundedQuery, serviceIncidentEvidenceQuery) {
		t.Fatal("bounded query must extend the unbounded query, not diverge from its join")
	}
}

func TestServiceIncidentEvidenceForServicesBoundedPassesLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	loader := NewServiceIncidentEvidenceLoader(db)

	if _, err := loader.GetIncidentEvidenceForServicesBounded(
		context.Background(), []string{"component:default/checkout"}, 512,
	); err != nil {
		t.Fatalf("GetIncidentEvidenceForServicesBounded() error = %v, want nil", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want one bounded load", len(db.queries))
	}
	if got := db.queries[0].args; len(got) != 2 || got[1] != 512 {
		t.Fatalf("bounded query args = %v, want [serviceIDs 512]", got)
	}
}

func TestServiceIncidentEvidenceForServicesBoundedNonPositiveLimitIsNoOp(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	loader := NewServiceIncidentEvidenceLoader(db)
	got, err := loader.GetIncidentEvidenceForServicesBounded(context.Background(), []string{"component:default/checkout"}, 0)
	if err != nil {
		t.Fatalf("GetIncidentEvidenceForServicesBounded() error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil for a non-positive row limit", got)
	}
	if len(db.queries) != 0 {
		t.Fatalf("queries = %d, want no query for a non-positive row limit", len(db.queries))
	}
}

var _ reducer.ServiceScopedIncidentEvidenceLoader = ServiceIncidentEvidenceLoader{}
