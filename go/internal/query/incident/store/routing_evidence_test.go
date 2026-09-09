// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	incidentsql "github.com/eshu-hq/eshu/go/internal/query/incident/sql"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestBuildIncidentRoutingEvidenceShowsDeclaredAppliedObservedConvergence(t *testing.T) {
	t.Parallel()

	got := buildIncidentRoutingEvidence(incidentRoutingEvidenceInput{
		Incident: incidentRoutingTestIncident(),
		Declared: []incidentDeclaredPagerDutyRouting{
			{
				EntityID:              "declared-1",
				RepoID:                "repo-checkout",
				RelativePath:          "env/prod/pagerduty.tfvars",
				DeclarationKind:       "tfvars",
				SourceClass:           "declared",
				Outcome:               "declared",
				ServiceName:           "Checkout API",
				ServiceNameResolution: "literal",
				EscalationPolicy:      "Checkout Primary",
				Environment:           "prod",
			},
		},
		Applied: []incidentAppliedPagerDutyRouting{
			{
				FactID:                    "applied-service",
				SourceClass:               "applied",
				SourceKind:                "terraform_state",
				Outcome:                   "applied",
				ResourceClass:             "service",
				ProviderObjectID:          "P-SVC",
				EscalationPolicyReference: "PEP1",
				TerraformStateAddress:     "pagerduty_service.checkout",
				ProviderAddress:           "registry.terraform.io/pagerduty/pagerduty",
			},
		},
		Observed: []incidentObservedPagerDutyRouting{
			{
				FactID:                    "observed-service",
				SourceClass:               "observed",
				SourceKind:                "pagerduty_api",
				Outcome:                   "observed",
				ServiceID:                 "P-SVC",
				ProviderObjectID:          "P-SVC",
				Status:                    "active",
				EscalationPolicyReference: "PEP1",
			},
		},
	})

	querytestutil.AssertIncidentEdge(t, got, model.IncidentSlotIntendedRouting, model.IncidentTruthExact)
	querytestutil.AssertIncidentEdge(t, got, model.IncidentSlotAppliedRouting, model.IncidentTruthExact)
	querytestutil.AssertIncidentEdge(t, got, model.IncidentSlotLiveRouting, model.IncidentTruthExact)
}

func TestBuildIncidentRoutingEvidenceKeepsNoIaCPagerDutyUseful(t *testing.T) {
	t.Parallel()

	got := buildIncidentRoutingEvidence(incidentRoutingEvidenceInput{
		Incident: incidentRoutingTestIncident(),
		Observed: []incidentObservedPagerDutyRouting{
			{
				FactID:           "observed-service",
				SourceClass:      "observed",
				SourceKind:       "pagerduty_api",
				Outcome:          "observed",
				ServiceID:        "P-SVC",
				ProviderObjectID: "P-SVC",
				Status:           "active",
			},
		},
	})

	querytestutil.AssertIncidentEdge(t, got, model.IncidentSlotIntendedRouting, model.IncidentTruthMissing)
	querytestutil.AssertIncidentEdge(t, got, model.IncidentSlotAppliedRouting, model.IncidentTruthMissing)
	querytestutil.AssertIncidentEdge(t, got, model.IncidentSlotLiveRouting, model.IncidentTruthExact)
}

func TestBuildIncidentRoutingEvidenceFlagsLiveDriftAndPermissionHidden(t *testing.T) {
	t.Parallel()

	drifted := buildIncidentRoutingEvidence(incidentRoutingEvidenceInput{
		Incident: incidentRoutingTestIncident(),
		Applied: []incidentAppliedPagerDutyRouting{
			{
				FactID:                    "applied-service",
				SourceClass:               "applied",
				Outcome:                   "applied",
				ResourceClass:             "service",
				ProviderObjectID:          "P-SVC",
				EscalationPolicyReference: "PEP1",
			},
		},
		Observed: []incidentObservedPagerDutyRouting{
			{
				FactID:                    "observed-service",
				SourceClass:               "observed",
				Outcome:                   "observed",
				ServiceID:                 "P-SVC",
				ProviderObjectID:          "P-SVC",
				EscalationPolicyReference: "PEP2",
			},
		},
	})
	querytestutil.AssertIncidentEdge(t, drifted, model.IncidentSlotLiveRouting, model.IncidentTruthDrifted)

	hidden := buildIncidentRoutingEvidence(incidentRoutingEvidenceInput{
		Incident: incidentRoutingTestIncident(),
		Warnings: []incidentRoutingCoverageWarning{
			{
				FactID:        "warning-1",
				SourceClass:   "observed",
				SourceKind:    "pagerduty_api",
				Reason:        "permission_hidden",
				ResourceClass: "service",
			},
		},
	})
	querytestutil.AssertIncidentEdge(t, hidden, model.IncidentSlotLiveRouting, model.IncidentTruthPermissionHidden)
}

func TestBuildIncidentRoutingEvidenceClassifiesDerivedStaleUnresolvedAndRejected(t *testing.T) {
	t.Parallel()

	derived := buildIncidentRoutingEvidence(incidentRoutingEvidenceInput{
		Incident: incidentRoutingTestIncident(),
		Declared: []incidentDeclaredPagerDutyRouting{
			{
				EntityID:              "declared-1",
				SourceClass:           "declared",
				Outcome:               "declared",
				ServiceName:           "Checkout API",
				ServiceNameResolution: "reference",
			},
		},
	})
	querytestutil.AssertIncidentEdge(t, derived, model.IncidentSlotIntendedRouting, model.IncidentTruthDerived)

	stale := buildIncidentRoutingEvidence(incidentRoutingEvidenceInput{
		Incident: incidentRoutingTestIncident(),
		Observed: []incidentObservedPagerDutyRouting{
			{
				FactID:           "observed-service",
				SourceClass:      "observed",
				Outcome:          "observed",
				ServiceID:        "P-SVC",
				ProviderObjectID: "P-SVC",
				Deleted:          true,
			},
		},
	})
	querytestutil.AssertIncidentEdge(t, stale, model.IncidentSlotLiveRouting, model.IncidentTruthStale)

	unresolved := buildIncidentRoutingEvidence(incidentRoutingEvidenceInput{
		Incident: incidentRoutingTestIncident(),
		Warnings: []incidentRoutingCoverageWarning{
			{
				FactID:        "warning-1",
				SourceClass:   "observed",
				SourceKind:    "pagerduty_api",
				Reason:        "not_found",
				ResourceClass: "service",
			},
		},
	})
	querytestutil.AssertIncidentEdge(t, unresolved, model.IncidentSlotLiveRouting, model.IncidentTruthUnresolved)

	rejected := buildIncidentRoutingEvidence(incidentRoutingEvidenceInput{
		Incident: incidentRoutingTestIncident(),
		Declared: []incidentDeclaredPagerDutyRouting{
			{
				EntityID:    "declared-1",
				SourceClass: "declared",
				Outcome:     "rejected",
				ServiceName: "Checkout API",
			},
		},
	})
	querytestutil.AssertIncidentEdge(t, rejected, model.IncidentSlotIntendedRouting, model.IncidentTruthRejected)
}

func TestBuildIncidentRoutingEvidenceKeepsAmbiguousDeclaredRouting(t *testing.T) {
	t.Parallel()

	got := buildIncidentRoutingEvidence(incidentRoutingEvidenceInput{
		Incident: incidentRoutingTestIncident(),
		Declared: []incidentDeclaredPagerDutyRouting{
			{
				EntityID:              "declared-a",
				RepoID:                "repo-a",
				RelativePath:          "env/prod/a.tfvars",
				SourceClass:           "declared",
				Outcome:               "declared",
				ServiceName:           "Checkout API",
				ServiceNameResolution: "literal",
			},
			{
				EntityID:              "declared-b",
				RepoID:                "repo-b",
				RelativePath:          "env/prod/b.tfvars",
				SourceClass:           "declared",
				Outcome:               "declared",
				ServiceName:           "Checkout API",
				ServiceNameResolution: "literal",
			},
		},
	})

	edge := incidentEdgeBySlot(t, got, model.IncidentSlotIntendedRouting)
	if edge.TruthLabel != model.IncidentTruthAmbiguous {
		t.Fatalf("intended routing truth_label = %q, want ambiguous", edge.TruthLabel)
	}
	if len(edge.Candidates) != 2 {
		t.Fatalf("intended routing candidates = %d, want 2", len(edge.Candidates))
	}
}

func TestIncidentContextRoutingQueriesStayBounded(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"FROM content_entities",
		"entity_type = 'PagerDutyDeclaration'",
		"metadata->>'source_class' = 'declared'",
		"lower(coalesce(metadata->>'service_name', '')) = lower($1)",
		"LIMIT $2",
	} {
		queryText := incidentsql.ListDeclaredPagerDutyRoutingQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("listIncidentDeclaredPagerDutyRoutingQuery missing %q:\n%s", want, queryText)
		}
	}
	for _, want := range []string{
		"fact.fact_kind = 'incident_routing.applied_pagerduty_resource'",
		"fact.payload->>'resource_class' = 'service'",
		"fact.payload->>'provider_object_id' = $1",
		"fact.payload->>'name_fingerprint' = $2",
		"LIMIT $3",
	} {
		queryText := incidentsql.ListAppliedPagerDutyRoutingQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("listIncidentAppliedPagerDutyRoutingQuery missing %q:\n%s", want, queryText)
		}
	}
	for _, want := range []string{
		"fact.fact_kind = 'incident_routing.observed_pagerduty_service'",
		"fact.payload->>'service_id' = $1",
		"fact.payload->>'provider_object_id' = $1",
		"fact.payload->>'name_fingerprint' = $2",
		"LIMIT $3",
	} {
		queryText := incidentsql.ListObservedPagerDutyRoutingQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("listIncidentObservedPagerDutyRoutingQuery missing %q:\n%s", want, queryText)
		}
	}
	for _, want := range []string{
		"fact.fact_kind = 'incident_routing.coverage_warning'",
		"fact.scope_id = $1",
		"$2 <> '' AND fact.payload->>'provider_object_id' = $2",
		"$2 <> '' AND fact.payload->>'service_id' = $2",
		"fact.payload->>'resource_class' IN ('service', 'unknown')",
		"LIKE '%permission%'",
		"LIMIT $3",
	} {
		queryText := incidentsql.ListRoutingCoverageWarningsQuery
		if !strings.Contains(queryText, want) {
			t.Fatalf("listIncidentRoutingCoverageWarningsQuery missing %q:\n%s", want, queryText)
		}
	}
}

func incidentRoutingTestIncident() model.IncidentContextIncident {
	return model.IncidentContextIncident{
		Provider:           "pagerduty",
		ProviderIncidentID: "PINC",
		Service: model.IncidentContextReference{
			ID:      "P-SVC",
			Type:    "service",
			Summary: "Checkout API",
			URL:     "https://example.pagerduty.com/services/P-SVC",
		},
		EscalationPolicy: model.IncidentContextReference{ID: "PEP1"},
	}
}
