// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/jira"
	"github.com/eshu-hq/eshu/go/internal/collector/pagerduty"
	"github.com/eshu-hq/eshu/go/internal/facts"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// writerMatrixFixture holds the ids the #7138 matrix asserts on.
type writerMatrixFixture struct {
	repoID  string // repository the two live pull-request links point at
	repo2ID string // a different repository with one live link
	repo3ID string // a repository nothing links to
}

// TestServiceStoryTargetSupportWriterShapedMatrixLive is the #7138
// correlation-truth matrix against Postgres. Every payload comes from the
// production writers, so the read is held to the keys real collectors emit.
//
// Repository target: a link to R appears; a link to R2 does not; an
// external_link with no linked_repository_id, a record, a transition, a
// metadata row and a PagerDuty service row are not linked and count as
// source-only; a tombstoned link and a link on a superseded generation are
// excluded from both. Service target: repository-linked support attaches only
// when the graph says the repository defines exactly the target workload, is
// ambiguous when it defines more than one, and fails closed otherwise.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestServiceStoryTargetSupportWriterShapedMatrixLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #7138 writer-shaped support matrix")
	}
	ctx := context.Background()
	reader, db := openStorySupportReader(ctx, t, dsn)
	fixture := seedWriterMatrix(ctx, t, db)

	t.Run("repository target sees only links to that repository", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repoID, TargetKind: "repository", TargetID: fixture.repoID, Limit: 20,
		})
		if got := IntVal(support, "evidence_count"); got != 2 {
			t.Fatalf("evidence_count = %d, want 2 (the two live links to %s); support = %#v", got, fixture.repoID, support)
		}
		ids := supportEvidenceFactIDs(support)
		if strings.Join(ids, ",") != "f-link-r-2,f-link-r-1" {
			t.Fatalf("evidence fact ids = %v, want [f-link-r-2 f-link-r-1] (newest first, no tombstone, no superseded generation)", ids)
		}
		for _, row := range support["evidence"].([]map[string]any) {
			if got := StringVal(row, "link_basis"); got != "linked_repository" {
				t.Fatalf("evidence %s link_basis = %q, want linked_repository", StringVal(row, "fact_id"), got)
			}
		}
		if got := IntVal(support, "work_item_count"); got != 2 {
			t.Fatalf("work_item_count = %d, want 2", got)
		}
		if got := IntVal(support, "incident_routing_count"); got != 0 {
			t.Fatalf("incident_routing_count = %d, want 0: the PagerDuty service has no reducer correlation (#7463 links correlated services)", got)
		}
		if got := IntVal(support, "ambiguous_count"); got != 0 {
			t.Fatalf("ambiguous_count = %d, want 0", got)
		}
	})

	t.Run("a link to another repository is not this repository's evidence", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repo2ID, TargetKind: "repository", TargetID: fixture.repo2ID, Limit: 20,
		})
		if got := strings.Join(supportEvidenceFactIDs(support), ","); got != "f-link-r2" {
			t.Fatalf("R2 evidence fact ids = %q, want f-link-r2", got)
		}
	})

	t.Run("unlinked rows are source-only and linked rows are not", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repo3ID, TargetKind: "repository", TargetID: fixture.repo3ID, Limit: 20,
		})
		if got := IntVal(support, "evidence_count"); got != 0 {
			t.Fatalf("evidence_count = %d, want 0 for a repository nothing links to", got)
		}
		coverage := mapValue(support, "coverage")
		// Active, non-tombstoned, not repository-linked: the plain link, the
		// record, the transition, the project metadata row and the PagerDuty
		// service row. The links to R and R2 are linked elsewhere, so they are
		// neither evidence for R3 nor source-only.
		for key, want := range map[string]int{
			"source_only_count":                  5,
			"work_item_source_only_count":        4,
			"incident_routing_source_only_count": 1,
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
		if got := IntVal(support, "evidence_count"); got != 2 {
			t.Fatalf("evidence_count = %d, want 2; support = %#v", got, support)
		}
		for _, row := range support["evidence"].([]map[string]any) {
			if got := StringVal(row, "link_basis"); got != "repository_sole_workload" {
				t.Fatalf("evidence %s link_basis = %q, want repository_sole_workload", StringVal(row, "fact_id"), got)
			}
		}
		if got := IntVal(mapValue(support, "coverage"), "repository_workload_count"); got != 1 {
			t.Fatalf("coverage.repository_workload_count = %d, want 1", got)
		}
	})

	t.Run("service target, repository defines several workloads", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceFilter(2, true))
		if got := IntVal(support, "evidence_count"); got != 0 {
			t.Fatalf("evidence_count = %d, want 0 for an ambiguous repository", got)
		}
		if got := IntVal(support, "ambiguous_count"); got != 2 {
			t.Fatalf("ambiguous_count = %d, want 2; support = %#v", got, support)
		}
		if reason := supportMissingReason(support); reason != "support_correlation_ambiguous" {
			t.Fatalf("missing_evidence reason = %q, want support_correlation_ambiguous", reason)
		}
		if got := IntVal(mapValue(support, "coverage"), "repository_workload_count"); got != 2 {
			t.Fatalf("coverage.repository_workload_count = %d, want 2", got)
		}
	})

	for name, filter := range map[string]serviceStoryTargetSupportFilter{
		"service target, graph unavailable":            serviceFilter(0, false),
		"service target, repository defines none":      serviceFilter(0, false),
		"service target, target not among the defined": serviceFilter(1, false),
	} {
		t.Run(name+" fails closed", func(t *testing.T) {
			support := readStorySupport(ctx, t, reader, filter)
			if got := IntVal(support, "evidence_count"); got != 0 {
				t.Fatalf("evidence_count = %d, want 0 (fail closed)", got)
			}
			if got := IntVal(support, "ambiguous_count"); got != 0 {
				t.Fatalf("ambiguous_count = %d, want 0 (fail closed)", got)
			}
		})
	}
}

// seedWriterMatrix seeds one Jira scope (active generation plus a superseded
// one) and one PagerDuty scope with writer-built facts, then returns the
// repository ids the links resolve to.
func seedWriterMatrix(ctx context.Context, t *testing.T, db *sql.DB) writerMatrixFixture {
	t.Helper()
	observed := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	for _, scope := range []struct{ scopeID, source, generation string }{
		{"s-jira", "jira", "g-jira"},
		{"s-pd", "pagerduty", "g-pd"},
	} {
		mustExec(ctx, t, db, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'work_item_project', $2, $1, $2, $1, $3, $3, 'active', $4, '{}'::jsonb)`, scope.scopeID, scope.source, observed, scope.generation)
		mustExec(ctx, t, db, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ($1, $2, 'snapshot', $3, $3, 'active', '{}'::jsonb)`, scope.generation, scope.scopeID, observed)
	}
	mustExec(ctx, t, db, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ('g-jira-old', 's-jira', 'snapshot', $1, $1, 'superseded', '{}'::jsonb)`, observed)

	jiraCtx := jira.EnvelopeContext{
		ScopeID: "s-jira", GenerationID: "g-jira", CollectorInstanceID: "jira-test",
		ObservedAt: observed, SourceURI: "https://example.atlassian.net",
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
	linkR1 := link("1", "OPS-1", "https://github.com/acme/payments/pull/1")
	linkR2 := link("2", "OPS-2", "https://github.com/acme/payments/pull/2")
	linkOther := link("3", "OPS-3", "https://github.com/acme/billing/pull/3")
	linkTomb := link("4", "OPS-4", "https://github.com/acme/payments/pull/4")
	linkOld := link("5", "OPS-5", "https://github.com/acme/payments/pull/5")
	plain, err := jira.NewWorkItemExternalLinkEnvelope(jiraCtx, jira.ExternalLink{
		ID: "6", IssueID: "26", IssueKey: "OPS-6",
		Application: jira.LinkApplication{Name: "Confluence"},
		Object:      jira.LinkObject{URL: "https://example.atlassian.net/wiki/pages/1"},
	})
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope(plain) error = %v", err)
	}
	record, err := jira.NewWorkItemRecordEnvelope(jiraCtx, jira.Issue{ID: "20001", Key: "OPS-1"})
	if err != nil {
		t.Fatalf("NewWorkItemRecordEnvelope error = %v", err)
	}
	transition, err := jira.NewWorkItemTransitionEnvelope(jiraCtx, jira.Transition{
		ID: "9001", IssueID: "20001", IssueKey: "OPS-1", Field: "status", From: "Open", To: "Done",
	})
	if err != nil {
		t.Fatalf("NewWorkItemTransitionEnvelope error = %v", err)
	}
	metadata, err := jira.NewWorkItemProjectMetadataEnvelope(jiraCtx, jira.ProjectMetadata{ID: "10", Key: "OPS", Name: "Ops"})
	if err != nil {
		t.Fatalf("NewWorkItemProjectMetadataEnvelope error = %v", err)
	}
	pdService, err := pagerduty.NewObservedPagerDutyServiceEnvelope(pagerduty.EnvelopeContext{
		ScopeID: "s-pd", GenerationID: "g-pd", CollectorInstanceID: "pd-test", ObservedAt: observed,
	}, pagerduty.ConfigService{ID: "PSVC1", Summary: "payments"})
	if err != nil {
		t.Fatalf("NewObservedPagerDutyServiceEnvelope error = %v", err)
	}

	seed := func(id, scopeID, generationID string, env facts.Envelope, at time.Time, tombstone bool) {
		t.Helper()
		seedMatrixFact(ctx, t, db, id, scopeID, generationID, env, at, tombstone)
	}
	seed("f-link-r-1", "s-jira", "g-jira", linkR1, observed.Add(-2*time.Hour), false)
	seed("f-link-r-2", "s-jira", "g-jira", linkR2, observed.Add(-time.Hour), false)
	seed("f-link-r2", "s-jira", "g-jira", linkOther, observed, false)
	seed("f-link-tomb", "s-jira", "g-jira", linkTomb, observed, true)
	seed("f-link-old", "s-jira", "g-jira-old", linkOld, observed, false)
	seed("f-link-plain", "s-jira", "g-jira", plain, observed, false)
	seed("f-record", "s-jira", "g-jira", record, observed, false)
	seed("f-transition", "s-jira", "g-jira", transition, observed, false)
	seed("f-metadata", "s-jira", "g-jira", metadata, observed, false)
	seed("f-pd-service", "s-pd", "g-pd", pdService, observed, false)

	repo3, _ := link("7", "OPS-7", "https://github.com/acme/unlinked/pull/7").Payload["linked_repository_id"].(string)
	repoID, _ := linkR1.Payload["linked_repository_id"].(string)
	repo2, _ := linkOther.Payload["linked_repository_id"].(string)
	if repoID == "" || repo2 == "" || repo3 == "" || repoID == repo2 || repoID == repo3 {
		t.Fatalf("writer ids not distinct canonical repository ids: %q %q %q", repoID, repo2, repo3)
	}
	return writerMatrixFixture{repoID: repoID, repo2ID: repo2, repo3ID: repo3}
}

func seedMatrixFact(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	factID, scopeID, generationID string,
	env facts.Envelope,
	at time.Time,
	tombstone bool,
) {
	t.Helper()
	raw, err := json.Marshal(env.Payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", factID, err)
	}
	mustExec(ctx, t, db, `INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, 'matrix', $1, $5, $5, $6, $7::jsonb)`,
		factID, scopeID, generationID, env.FactKind, at, tombstone, string(raw))
}

// openStorySupportReader opens a single-connection pool on a throwaway schema
// with every migration applied, so a ContentReader built on it runs the shipped
// statements end to end. One connection keeps the schema's search_path on every
// statement the reader issues.
func openStorySupportReader(ctx context.Context, t *testing.T, dsn string) (*ContentReader, *sql.DB) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	schema := fmt.Sprintf("story_matrix_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = db.Close()
	})
	for _, stmt := range []string{"CREATE SCHEMA " + schema, "SET search_path TO " + schema + ", public"} {
		mustExec(ctx, t, db, stmt)
	}
	for _, def := range storagepostgres.BootstrapDefinitions() {
		mustExec(ctx, t, db, def.SQL)
	}
	return NewContentReader(db), db
}

func mustExec(ctx context.Context, t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec: %v\n%s", err, query)
	}
}

func readStorySupport(
	ctx context.Context,
	t *testing.T,
	reader *ContentReader,
	filter serviceStoryTargetSupportFilter,
) map[string]any {
	t.Helper()
	model, err := reader.ServiceStoryTargetSupportEvidence(ctx, filter)
	if err != nil {
		t.Fatalf("ServiceStoryTargetSupportEvidence(%#v) error = %v", filter, err)
	}
	if model.Support == nil {
		t.Fatalf("ServiceStoryTargetSupportEvidence(%#v) returned no support block", filter)
	}
	return model.Support
}

func supportEvidenceFactIDs(support map[string]any) []string {
	rows, _ := support["evidence"].([]map[string]any)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, StringVal(row, "fact_id"))
	}
	return ids
}

func supportMissingReason(support map[string]any) string {
	missing, _ := support["missing_evidence"].([]map[string]any)
	if len(missing) == 0 {
		return ""
	}
	return StringVal(missing[0], "reason")
}
