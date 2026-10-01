// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/jira"
	"github.com/eshu-hq/eshu/go/internal/collector/pagerduty"
	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/reducer/incident"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// pagerDutyMatrixFixture holds the repository ids and fact ids the #7463 matrix
// asserts on.
type pagerDutyMatrixFixture struct {
	repoID  string // R: three PagerDuty rows and one Jira link attach
	repo2ID string // R2: one PagerDuty row attaches
	repo3ID string // R3: nothing attaches
}

// TestServiceStoryTargetSupportPagerDutyRoutingMatrixLive is the #7463
// correlation-truth matrix against Postgres. Every fact comes from a production
// writer: the PagerDuty collector for observed services, the Terraform-state
// parser for applied resources, and the reducer's own incident-repository
// correlation writer for the correlation facts the read joins through.
//
// Repository target R: an observed service and an applied pagerduty_service
// whose provider id the reducer correlated exactly, and an observed service
// correlated derived, attach as incident-routing evidence next to R's Jira link.
// Not evidence for R: a team-class applied resource that shares the id, an
// ambiguous correlation (real, with no repository, and a hostile one that names
// R but is provenance-only), an exact but provenance-only correlation and an
// unresolved one that is not provenance-only (each rejected by one filter
// alone), a correlation from another provider that reuses the id, a correlation
// on a superseded generation, a service with no correlation, a tombstoned
// service, a service on a superseded generation, and the coverage warning. A service correlated to R2 attaches to R2 only and, being linked to
// some repository, is not source-only for R3.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestServiceStoryTargetSupportPagerDutyRoutingMatrixLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #7463 writer-shaped PagerDuty routing matrix")
	}
	ctx := context.Background()
	reader, db := openStorySupportReader(ctx, t, dsn)
	fixture := seedPagerDutyRoutingMatrix(ctx, t, db)

	t.Run("repository target sees its correlated PagerDuty rows next to its Jira link", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repoID, TargetKind: "repository", TargetID: fixture.repoID, Limit: 20,
		})
		want := "f-obs-pa,f-link-r,f-app-pa,f-obs-pb"
		if got := strings.Join(supportEvidenceFactIDs(support), ","); got != want {
			t.Fatalf("evidence fact ids = %q, want %q (newest first); support = %#v", got, want, support)
		}
		for _, row := range support["evidence"].([]map[string]any) {
			wantBasis := "incident_repository_correlation"
			if StringVal(row, "fact_id") == "f-link-r" {
				wantBasis = "linked_repository"
			}
			if got := StringVal(row, "link_basis"); got != wantBasis {
				t.Fatalf("evidence %s link_basis = %q, want %q", StringVal(row, "fact_id"), got, wantBasis)
			}
		}
		if got := IntVal(support, "incident_routing_count"); got != 3 {
			t.Fatalf("incident_routing_count = %d, want 3", got)
		}
		if got := IntVal(support, "work_item_count"); got != 1 {
			t.Fatalf("work_item_count = %d, want 1", got)
		}
		if got := IntVal(support, "ambiguous_count"); got != 0 {
			t.Fatalf("ambiguous_count = %d, want 0", got)
		}
	})

	t.Run("a service correlated to another repository is that repository's evidence only", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repo2ID, TargetKind: "repository", TargetID: fixture.repo2ID, Limit: 20,
		})
		if got := strings.Join(supportEvidenceFactIDs(support), ","); got != "f-obs-pd2" {
			t.Fatalf("R2 evidence fact ids = %q, want f-obs-pd2", got)
		}
	})

	t.Run("a repository nothing correlates to has no evidence and counts only unlinked rows as source-only", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repo3ID, TargetKind: "repository", TargetID: fixture.repo3ID, Limit: 20,
		})
		if got := IntVal(support, "evidence_count"); got != 0 {
			t.Fatalf("evidence_count = %d, want 0; support = %#v", got, support)
		}
		coverage := mapValue(support, "coverage")
		// Unlinked, active and not tombstoned: the ambiguous (pc), hostile
		// ambiguous provenance-only (ph), exact provenance-only (pi), unresolved
		// (pj), superseded-correlation (pe), uncorrelated (pf), other-provider (pg)
		// repository-less exact (pk) and whitespace-repository exact (pl, pm) observed
		// services, the team-class applied resource, and the coverage
		// warning. pa, pb and pd2 are linked to some repository and R's Jira link is
		// linked, so none of those is source-only.
		for key, want := range map[string]int{
			"source_only_count":                  12,
			"work_item_source_only_count":        0,
			"incident_routing_source_only_count": 12,
		} {
			if got := IntVal(coverage, key); got != want {
				t.Fatalf("coverage.%s = %d, want %d; coverage = %#v", key, got, want, coverage)
			}
		}
		if reason := supportMissingReason(support); reason != "support_source_only_not_target_linked" {
			t.Fatalf("missing_evidence reason = %q, want support_source_only_not_target_linked", reason)
		}
	})

	serviceFilter := func(count int, defines bool) serviceStoryTargetSupportFilter {
		return serviceStoryTargetSupportFilter{
			Repository: fixture.repoID, TargetKind: "service", TargetID: "workload:payments",
			ServiceID: "workload:payments", Limit: 20,
			RepositoryWorkloadCount: count, RepositoryDefinesTarget: defines,
		}
	}

	t.Run("service target, repository defines exactly the target", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceFilter(1, true))
		if got := IntVal(support, "incident_routing_count"); got != 3 {
			t.Fatalf("incident_routing_count = %d, want 3; support = %#v", got, support)
		}
		for _, row := range support["evidence"].([]map[string]any) {
			if got := StringVal(row, "link_basis"); got != "repository_sole_workload" {
				t.Fatalf("evidence %s link_basis = %q, want repository_sole_workload", StringVal(row, "fact_id"), got)
			}
		}
	})

	t.Run("service target, repository defines several workloads", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceFilter(2, true))
		if got := IntVal(support, "evidence_count"); got != 0 {
			t.Fatalf("evidence_count = %d, want 0 for an ambiguous repository", got)
		}
		if got := IntVal(support, "ambiguous_count"); got != 4 {
			t.Fatalf("ambiguous_count = %d, want 4 (three PagerDuty rows and the Jira link); support = %#v", got, support)
		}
		if reason := supportMissingReason(support); reason != "support_correlation_ambiguous" {
			t.Fatalf("missing_evidence reason = %q, want support_correlation_ambiguous", reason)
		}
	})

	for name, filter := range map[string]serviceStoryTargetSupportFilter{
		"service target, graph unavailable":            serviceFilter(0, false),
		"service target, repository defines none":      serviceFilter(0, true),
		"service target, target not among the defined": serviceFilter(1, false),
	} {
		t.Run(name+" fails closed", func(t *testing.T) {
			support := readStorySupport(ctx, t, reader, filter)
			if got := IntVal(support, "evidence_count") + IntVal(support, "ambiguous_count"); got != 0 {
				t.Fatalf("evidence + ambiguous = %d, want 0 (fail closed); support = %#v", got, support)
			}
		})
	}

	t.Run("evidence is bounded and reports truncation", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repoID, TargetKind: "repository", TargetID: fixture.repoID, Limit: 2,
		})
		// The Jira row is older than f-obs-pa, so the two newest overall are not
		// the Jira statement's rows followed by the routing statement's: this fails
		// if the two statements' rows are concatenated instead of merged newest first.
		if got := strings.Join(supportEvidenceFactIDs(support), ","); got != "f-obs-pa,f-link-r" {
			t.Fatalf("evidence fact ids = %q, want the two newest f-obs-pa,f-link-r", got)
		}
		if truncated, _ := mapValue(support, "coverage")["truncated"].(bool); !truncated {
			t.Fatalf("coverage.truncated = false, want true with limit 2 and four linked rows")
		}
	})
}

// seedPagerDutyRoutingMatrix seeds the scopes and writer-built facts the matrix
// reads. See the test comment for what each id stands for.
func seedPagerDutyRoutingMatrix(ctx context.Context, t *testing.T, db *sql.DB) pagerDutyMatrixFixture {
	t.Helper()
	at := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	for _, s := range []struct{ scopeID, source, generation, old string }{
		{"s-jira", "jira", "g-jira", ""},
		{"s-pd", "pagerduty", "g-pd", "g-pd-old"},
		{"s-tf", "terraform_state", "g-tf", "g-tf-old"},
	} {
		seedRoutingScope(ctx, t, db, s.scopeID, s.source, s.generation, s.old, at)
	}

	// Jira: one link to R, built by the production writer.
	jiraCtx := jira.EnvelopeContext{
		ScopeID: "s-jira", GenerationID: "g-jira", CollectorInstanceID: "jira-test",
		ObservedAt: at, SourceURI: "https://example.atlassian.net",
	}
	link := func(id, issue, url string) facts.Envelope {
		t.Helper()
		env, err := jira.NewWorkItemExternalLinkEnvelope(jiraCtx, jira.ExternalLink{
			ID: id, IssueID: "2" + id, IssueKey: issue,
			Application: jira.LinkApplication{Name: "GitHub", Type: "com.github.integration"},
			Object:      jira.LinkObject{URL: url},
		})
		if err != nil {
			t.Fatalf("NewWorkItemExternalLinkEnvelope(%s) error = %v", url, err)
		}
		return env
	}
	linkR := link("1", "OPS-1", "https://github.com/acme/payments/pull/1")
	repoID, _ := linkR.Payload["linked_repository_id"].(string)
	repo2, _ := link("2", "OPS-2", "https://github.com/acme/billing/pull/2").Payload["linked_repository_id"].(string)
	repo3, _ := link("3", "OPS-3", "https://github.com/acme/unlinked/pull/3").Payload["linked_repository_id"].(string)
	if repoID == "" || repo2 == "" || repo3 == "" || repoID == repo2 || repoID == repo3 {
		t.Fatalf("writer ids not distinct canonical repository ids: %q %q %q", repoID, repo2, repo3)
	}
	seedMatrixFact(ctx, t, db, "f-link-r", "s-jira", "g-jira", linkR, at.Add(-90*time.Minute), false)

	// PagerDuty observed services, built by the collector writer.
	observed := func(factID, serviceID, generation string, observedAt time.Time, tombstone bool) {
		t.Helper()
		env, err := pagerduty.NewObservedPagerDutyServiceEnvelope(pagerduty.EnvelopeContext{
			ScopeID: "s-pd", GenerationID: generation, CollectorInstanceID: "pd-test", ObservedAt: observedAt,
		}, pagerduty.ConfigService{ID: serviceID, Summary: "service " + serviceID})
		if err != nil {
			t.Fatalf("NewObservedPagerDutyServiceEnvelope(%s) error = %v", serviceID, err)
		}
		seedMatrixFact(ctx, t, db, factID, "s-pd", generation, env, observedAt, tombstone)
	}
	observed("f-obs-pa", "PA", "g-pd", at.Add(-time.Hour), false)
	observed("f-obs-pb", "PB", "g-pd", at.Add(-3*time.Hour), false)
	observed("f-obs-pc", "PC", "g-pd", at, false)
	observed("f-obs-ph", "PH", "g-pd", at, false)
	observed("f-obs-pi", "PI", "g-pd", at, false)
	observed("f-obs-pj", "PJ", "g-pd", at, false)
	observed("f-obs-pd2", "PD2", "g-pd", at, false)
	observed("f-obs-pe", "PE", "g-pd", at, false)
	observed("f-obs-pf", "PF", "g-pd", at, false)
	observed("f-obs-pg", "PG", "g-pd", at, false)
	observed("f-obs-pk", "PK", "g-pd", at, false)
	observed("f-obs-pl", "PL", "g-pd", at, false)
	observed("f-obs-pm", "PM", "g-pd", at, false)
	observed("f-obs-pa-tomb", "PA", "g-pd", at, true)
	observed("f-obs-pa-old", "PA", "g-pd-old", at, false)

	// Applied resources, parsed from Terraform state by the production parser.
	applied := parseAppliedPagerDutyState(t, at)
	seedMatrixFact(ctx, t, db, "f-app-pa", "s-tf", "g-tf", applied.service, at.Add(-2*time.Hour), false)
	seedMatrixFact(ctx, t, db, "f-app-team-pa", "s-tf", "g-tf", applied.team, at, false)
	seedMatrixFact(ctx, t, db, "f-warn-user", "s-tf", "g-tf", applied.warning, at, false)

	// Reducer correlation facts, written by the reducer's own writer.
	writeCorrelations(ctx, t, db, "g-tf", at, []incident.IncidentRepositoryCorrelationDecision{
		{Provider: "pagerduty", ProviderServiceID: "PA", RepositoryID: repoID, Outcome: incident.IncidentRepositoryCorrelationExact},
		{Provider: "pagerduty", ProviderServiceID: "PB", RepositoryID: repoID, Outcome: incident.IncidentRepositoryCorrelationDerived},
		{
			Provider: "pagerduty", ProviderServiceID: "PC", Outcome: incident.IncidentRepositoryCorrelationAmbiguous,
			ProvenanceOnly: true, CandidateRepositoryIDs: []string{repoID, repo2},
		},
		{
			Provider: "pagerduty", ProviderServiceID: "PH", RepositoryID: repoID,
			Outcome: incident.IncidentRepositoryCorrelationAmbiguous, ProvenanceOnly: true,
		},
		// Exact but provenance-only, naming R: only the provenance filter rejects it.
		{
			Provider: "pagerduty", ProviderServiceID: "PI", RepositoryID: repoID,
			Outcome: incident.IncidentRepositoryCorrelationExact, ProvenanceOnly: true,
		},
		// Not provenance-only but not an edge-bearing outcome, naming R: only the
		// outcome filter rejects it.
		{
			Provider: "pagerduty", ProviderServiceID: "PJ", RepositoryID: repoID,
			Outcome: incident.IncidentRepositoryCorrelationUnresolved,
		},
		{Provider: "pagerduty", ProviderServiceID: "PD2", RepositoryID: repo2, Outcome: incident.IncidentRepositoryCorrelationExact},
		{Provider: "opsgenie", ProviderServiceID: "PG", RepositoryID: repoID, Outcome: incident.IncidentRepositoryCorrelationExact},
		// Exact and not provenance-only but with no repository: the writer persists
		// it as given, no repository read can return it, and it must not hide the
		// service from the source-only count either.
		{Provider: "pagerduty", ProviderServiceID: "PK", Outcome: incident.IncidentRepositoryCorrelationExact},
		// A repository id the writer stored with surrounding whitespace, or only
		// whitespace: story targets are trimmed before the exact match, so no story
		// can ever read these, and they must stay source-only too.
		{Provider: "pagerduty", ProviderServiceID: "PL", RepositoryID: "  ", Outcome: incident.IncidentRepositoryCorrelationExact},
		{Provider: "pagerduty", ProviderServiceID: "PM", RepositoryID: " " + repoID + " ", Outcome: incident.IncidentRepositoryCorrelationExact},
	})
	writeCorrelations(ctx, t, db, "g-tf-old", at, []incident.IncidentRepositoryCorrelationDecision{
		{Provider: "pagerduty", ProviderServiceID: "PE", RepositoryID: repoID, Outcome: incident.IncidentRepositoryCorrelationExact},
	})
	return pagerDutyMatrixFixture{repoID: repoID, repo2ID: repo2, repo3ID: repo3}
}

func seedRoutingScope(ctx context.Context, t *testing.T, db *sql.DB, scopeID, source, generation, oldGeneration string, at time.Time) {
	t.Helper()
	mustExec(ctx, t, db, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'incident_service', $2, $1, $2, $1, $3, $3, 'active', $4, '{}'::jsonb)`, scopeID, source, at, generation)
	mustExec(ctx, t, db, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ($1, $2, 'snapshot', $3, $3, 'active', '{}'::jsonb)`, generation, scopeID, at)
	if oldGeneration != "" {
		mustExec(ctx, t, db, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ($1, $2, 'snapshot', $3, $3, 'superseded', '{}'::jsonb)`, oldGeneration, scopeID, at)
	}
}

type appliedPagerDutyFacts struct {
	service, team, warning facts.Envelope
}

// parseAppliedPagerDutyState runs the production Terraform-state parser over a
// state holding a pagerduty_service and a pagerduty_team that share the id PA,
// and an unsupported pagerduty_user that yields the coverage warning.
func parseAppliedPagerDutyState(t *testing.T, at time.Time) appliedPagerDutyFacts {
	t.Helper()
	state := `{"serial":1,"lineage":"lineage-7463","resources":[
		{"mode":"managed","type":"pagerduty_service","name":"payments","module":"module.pagerduty","provider":"provider[\"registry.terraform.io/PagerDuty/pagerduty\"]","instances":[{"attributes":{"id":"PA","name":"Payments","escalation_policy":"PDEP1","alert_creation":"create_alerts_and_incidents"}}]},
		{"mode":"managed","type":"pagerduty_team","name":"payments","module":"module.pagerduty","provider":"provider[\"registry.terraform.io/PagerDuty/pagerduty\"]","instances":[{"attributes":{"id":"PA","name":"Payments Team"}}]},
		{"mode":"managed","type":"pagerduty_user","name":"owner","module":"module.pagerduty","provider":"provider[\"registry.terraform.io/PagerDuty/pagerduty\"]","instances":[{"attributes":{"id":"PDUSER1"}}]}
	]}`
	key, err := redact.NewKey([]byte("test-redaction-key"))
	if err != nil {
		t.Fatalf("redact.NewKey() error = %v", err)
	}
	rules, err := redact.NewRuleSet("test-schema", []string{"password"})
	if err != nil {
		t.Fatalf("redact.NewRuleSet() error = %v", err)
	}
	scopeValue, err := scope.NewTerraformStateSnapshotScope("repo-scope-7463", "s3", "s3://tfstate-prod/services/payments/terraform.tfstate", nil)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v", err)
	}
	generation, err := scope.NewTerraformStateSnapshotGeneration(scopeValue.ScopeID, 1, "lineage-7463", at)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() error = %v", err)
	}
	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), terraformstate.ParseOptions{
		Scope: scopeValue, Generation: generation,
		Source:     terraformstate.StateKey{BackendKind: terraformstate.BackendS3, Locator: "s3://tfstate-prod/services/payments/terraform.tfstate"},
		ObservedAt: at, RedactionKey: key, RedactionRules: rules, FencingToken: 1,
	})
	if err != nil {
		t.Fatalf("terraformstate.Parse() error = %v", err)
	}
	var out appliedPagerDutyFacts
	for _, env := range result.Facts {
		switch {
		case env.FactKind == facts.IncidentRoutingAppliedPagerDutyResourceFactKind && env.Payload["resource_class"] == "service":
			out.service = env
		case env.FactKind == facts.IncidentRoutingAppliedPagerDutyResourceFactKind && env.Payload["resource_class"] == "team":
			out.team = env
		case env.FactKind == facts.IncidentRoutingCoverageWarningFactKind:
			out.warning = env
		}
	}
	if out.service.FactKind == "" || out.team.FactKind == "" || out.warning.FactKind == "" {
		t.Fatalf("parser did not emit the applied service, team and coverage warning: %#v", result.Facts)
	}
	if out.service.Payload["provider_object_id"] != "PA" {
		t.Fatalf("applied service provider_object_id = %#v, want PA", out.service.Payload["provider_object_id"])
	}
	return out
}

func writeCorrelations(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	generation string,
	at time.Time,
	decisions []incident.IncidentRepositoryCorrelationDecision,
) {
	t.Helper()
	writer := incident.PostgresIncidentRepositoryCorrelationWriter{DB: db, Now: func() time.Time { return at }}
	if _, err := writer.WriteIncidentRepositoryCorrelations(ctx, incident.IncidentRepositoryCorrelationWrite{
		IntentID: "intent-" + generation, ScopeID: "s-tf", GenerationID: generation,
		SourceSystem: "terraform_state", Cause: "matrix", Decisions: decisions,
	}); err != nil {
		t.Fatalf("WriteIncidentRepositoryCorrelations(%s) error = %v", generation, err)
	}
}
