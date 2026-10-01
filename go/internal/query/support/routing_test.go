// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package support

import (
	"reflect"
	"strings"
	"testing"
)

func TestIncidentRoutingSQLIsClosedForABlankRepository(t *testing.T) {
	t.Parallel()

	for _, repositoryID := range []string{"", "   ", "\t\n"} {
		if query, args := IncidentRoutingSQL(repositoryID, 20); query != "" || args != nil {
			t.Fatalf("IncidentRoutingSQL(%q) = (%q, %v), want no statement", repositoryID, query, args)
		}
	}
}

func TestIncidentRoutingSQLBindsTheRepositoryAndOneExtraRow(t *testing.T) {
	t.Parallel()

	_, args := IncidentRoutingSQL("  repository:r_x  ", 20)
	if want := []any{"repository:r_x", 21}; !reflect.DeepEqual(args, want) {
		t.Fatalf("IncidentRoutingSQL args = %#v, want %#v (trimmed repository, limit+1 so truncation is visible)", args, want)
	}
}

// TestIncidentRoutingSQLKeepsItsProbesFenced guards the #7463 plan proof. As a
// plain join of fact_records to the active scope and generation, Postgres drove
// from the 242 active generations and read every observed service of each
// (40,006 rows, 60k buffer hits, 30 to 64 ms at one million facts). The
// repository-first probes below are what made it 1 to 3 ms with 2k buffer hits:
// each OFFSET 0 stops the planner from flattening its subquery back into the
// join, and the LATERAL probes by provider service id are what let the applied
// and observed service indexes answer.
func TestIncidentRoutingSQLKeepsItsProbesFenced(t *testing.T) {
	t.Parallel()

	query, _ := IncidentRoutingSQL("repository:r_x", 20)
	for _, tc := range []struct {
		fragment string
		want     int
	}{
		{"OFFSET 0", 3},
		{"CROSS JOIN LATERAL (", 2},
		{"AS MATERIALIZED", 1},
		{"fact.is_tombstone = FALSE", 2},
		{"corr.is_tombstone = FALSE", 1},
		{"generation.status = 'active'", 2},
		{"cgen.status = 'active'", 1},
		{"corr.payload->>'repository_id' = $1", 1},
		{"fact.fact_kind = '" + AppliedPagerDutyResourceKind + "'", 1},
		{"fact.fact_kind = '" + ObservedPagerDutyServiceKind + "'", 1},
		{"corr.fact_kind = '" + IncidentCorrelationKind + "'", 1},
		{"fact.payload->>'resource_class' = 'service'", 1},
		{"fact.payload->>'provider_object_id' = correlated.provider_service_id", 1},
		{"corr.payload->>'provenance_only' = 'false'", 1},
		{"corr.payload->>'outcome' IN ('exact', 'derived')", 1},
		{"= 'pagerduty'", 1},
		{"ORDER BY routed.observed_at DESC, routed.fact_id DESC", 1},
		{"LIMIT $2", 1},
	} {
		if got := strings.Count(query, tc.fragment); got != tc.want {
			t.Errorf("IncidentRoutingSQL has %d of %q, want %d:\n%s", got, tc.fragment, tc.want, query)
		}
	}
	if !strings.Contains(query, ObservedServiceKey("fact")+" = correlated.provider_service_id") {
		t.Errorf("IncidentRoutingSQL does not probe the observed service key %q:\n%s", ObservedServiceKey("fact"), query)
	}
	for _, banned := range []string{" LIKE ", "ILIKE", "title", "summary", "name_fingerprint"} {
		if strings.Contains(query, banned) {
			t.Errorf("IncidentRoutingSQL matches on %q; a PagerDuty fact links only through the reducer correlation:\n%s", banned, query)
		}
	}
}

func TestAdmissibleCorrelationsSQLComputesTheCandidatesOnce(t *testing.T) {
	t.Parallel()

	query := AdmissibleCorrelationsSQL()
	for _, want := range []string{
		"WITH correlation_candidates AS MATERIALIZED (",
		AdmissibleCorrelationsCTE + " AS MATERIALIZED (",
		"corr.fact_kind = '" + IncidentCorrelationKind + "'",
		"corr.is_tombstone = FALSE",
		"corr.payload->>'provenance_only' = 'false'",
		"corr.payload->>'outcome' IN ('exact', 'derived')",
		"cgen.status = 'active'",
		"NULLIF(cand.provider_service_id, '') IS NOT NULL",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("AdmissibleCorrelationsSQL missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "repository_id") {
		t.Errorf("AdmissibleCorrelationsSQL is the set of every correlated service and must not filter on one repository:\n%s", query)
	}
}

// TestLinkedIncidentRoutingPredicateIsTwoValued guards the #6807 rule the
// source-only count follows: the predicate sits under NOT, so a NULL anywhere in
// it would silently drop a fact from the count.
func TestLinkedIncidentRoutingPredicateIsTwoValued(t *testing.T) {
	t.Parallel()

	predicate := LinkedIncidentRoutingPredicate()
	if strings.Contains(predicate, "NOT IN") {
		t.Fatalf("predicate uses NOT IN, which is UNKNOWN when the set holds a NULL:\n%s", predicate)
	}
	wantIn := "IN (SELECT c.provider_service_id FROM " + AdmissibleCorrelationsCTE + " AS c)"
	if got := strings.Count(predicate, wantIn); got != 2 {
		t.Fatalf("predicate has %d uncorrelated %q subplans, want 2 (applied and observed):\n%s", got, wantIn, predicate)
	}
	if strings.Contains(predicate, "EXISTS") {
		t.Fatalf("predicate uses a per-row EXISTS; the measured plan is the hashed IN subplan (1.5 s vs 85 ms at one million facts):\n%s", predicate)
	}
	if !strings.Contains(predicate, "COALESCE(fact.payload->>'provider_object_id', '') IN") {
		t.Fatalf("predicate does not coalesce the applied key, so a fact without it would be UNKNOWN under NOT:\n%s", predicate)
	}
	if !strings.Contains(predicate, "fact.payload->>'resource_class' = 'service'") {
		t.Fatalf("predicate links an applied resource of any class, want service only:\n%s", predicate)
	}
}

func TestRoutingFactCorrelatedTo(t *testing.T) {
	t.Parallel()

	correlation := func(repository, service string) map[string]any {
		return map[string]any{"repository_id": repository, "provider_service_id": service}
	}
	applied := func(class, objectID string) map[string]any {
		return map[string]any{
			"fact_kind": AppliedPagerDutyResourceKind,
			"payload":   map[string]any{"resource_class": class, "provider_object_id": objectID},
		}
	}
	observed := func(payload map[string]any) map[string]any {
		return map[string]any{"fact_kind": ObservedPagerDutyServiceKind, "payload": payload}
	}
	with := func(fact map[string]any, correlation map[string]any) map[string]any {
		fact["correlation"] = correlation
		return fact
	}

	tests := []struct {
		name         string
		fact         map[string]any
		repositoryID string
		want         bool
	}{
		{"applied service correlated to the repository", with(applied("service", "PA"), correlation("repo-a", "PA")), "repo-a", true},
		{"observed service by provider_object_id", with(observed(map[string]any{"provider_object_id": "PA", "service_id": "PA"}), correlation("repo-a", "PA")), "repo-a", true},
		{"observed service falls back to service_id", with(observed(map[string]any{"service_id": "PA"}), correlation("repo-a", "PA")), "repo-a", true},
		{"observed service with a blank provider_object_id falls back to service_id", with(observed(map[string]any{"provider_object_id": "", "service_id": "PA"}), correlation("repo-a", "PA")), "repo-a", true},
		{"repository is trimmed like the filter", with(applied("service", "PA"), correlation("repo-a", "PA")), "  repo-a  ", true},
		{"correlation names another repository", with(applied("service", "PA"), correlation("repo-b", "PA")), "repo-a", false},
		{"correlation names another provider service", with(applied("service", "PA"), correlation("repo-a", "PB")), "repo-a", false},
		{"team class shares the id", with(applied("team", "PA"), correlation("repo-a", "PA")), "repo-a", false},
		{"escalation policy class", with(applied("escalation_policy", "PA"), correlation("repo-a", "PA")), "repo-a", false},
		{"applied resource without a class", with(applied("", "PA"), correlation("repo-a", "PA")), "repo-a", false},
		{"applied resource without an object id", with(applied("service", ""), correlation("repo-a", "")), "repo-a", false},
		{"observed service without any id", with(observed(map[string]any{}), correlation("repo-a", "")), "repo-a", false},
		{"no correlation on the row", applied("service", "PA"), "repo-a", false},
		{"blank correlation repository", with(applied("service", "PA"), correlation("", "PA")), "repo-a", false},
		{"blank target repository", with(applied("service", "PA"), correlation("repo-a", "PA")), "", false},
		{"ids compare exactly, not trimmed", with(applied("service", "PA "), correlation("repo-a", "PA")), "repo-a", false},
		{"coverage warning", with(map[string]any{"fact_kind": "incident_routing.coverage_warning", "payload": map[string]any{}}, correlation("repo-a", "PA")), "repo-a", false},
		{"jira link", with(map[string]any{"fact_kind": "work_item.external_link", "payload": map[string]any{}}, correlation("repo-a", "PA")), "repo-a", false},
		{"no payload", map[string]any{"fact_kind": AppliedPagerDutyResourceKind}, "repo-a", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := RoutingFactCorrelatedTo(tt.fact, tt.repositoryID); got != tt.want {
				t.Fatalf("RoutingFactCorrelatedTo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRoutingFactNamesOnlyTheTwoLinkableKinds(t *testing.T) {
	t.Parallel()

	for kind, want := range map[string]bool{
		AppliedPagerDutyResourceKind:                 true,
		ObservedPagerDutyServiceKind:                 true,
		"incident_routing.coverage_warning":          false,
		IncidentCorrelationKind:                      false,
		"work_item.external_link":                    false,
		"incident_routing.applied_pagerduty_service": false,
		"": false,
	} {
		if got := IsRoutingFact(kind); got != want {
			t.Errorf("IsRoutingFact(%q) = %v, want %v", kind, got, want)
		}
	}
}
