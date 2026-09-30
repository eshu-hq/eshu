// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/jira"
)

// TestServiceStoryTargetSupportMatchesWriterShapedFactsLive is the #7138
// regression. It seeds support facts whose payloads come from the production
// writers themselves (not hand-written JSON), so the read is held to the keys
// real collectors emit. Before #7138 the read matched only candidate_refs,
// evidence_refs and linked_entities, which no support writer emits, and a real
// Jira pull-request link never appeared in its repository's story.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestServiceStoryTargetSupportMatchesWriterShapedFactsLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #7138 writer-shaped target-support proof")
	}
	ctx := context.Background()
	conn := openStorySupportSchema(ctx, t, dsn)

	observed := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	seedWriterShapeScope(ctx, t, conn, "s-jira", "g-jira", observed)
	envelopeCtx := jira.EnvelopeContext{
		ScopeID:             "s-jira",
		GenerationID:        "g-jira",
		CollectorInstanceID: "jira-test",
		ObservedAt:          observed,
		SourceURI:           "https://example.atlassian.net",
	}

	prLink, err := jira.NewWorkItemExternalLinkEnvelope(envelopeCtx, jira.ExternalLink{
		ID:          "10001",
		IssueID:     "20001",
		IssueKey:    "OPS-1",
		Application: jira.LinkApplication{Name: "GitHub", Type: "com.github.integration"},
		Object:      jira.LinkObject{URL: "https://github.com/acme/payments/pull/42"},
	})
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope(pull request) error = %v", err)
	}
	repoID, _ := prLink.Payload["linked_repository_id"].(string)
	if !strings.HasPrefix(repoID, "repository:") {
		t.Fatalf("writer linked_repository_id = %q, want a canonical repository id", repoID)
	}
	otherLink, err := jira.NewWorkItemExternalLinkEnvelope(envelopeCtx, jira.ExternalLink{
		ID:          "10002",
		IssueID:     "20002",
		IssueKey:    "OPS-2",
		Application: jira.LinkApplication{Name: "GitHub", Type: "com.github.integration"},
		Object:      jira.LinkObject{URL: "https://github.com/acme/billing/pull/7"},
	})
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope(other repository) error = %v", err)
	}
	plainLink, err := jira.NewWorkItemExternalLinkEnvelope(envelopeCtx, jira.ExternalLink{
		ID:          "10003",
		IssueID:     "20003",
		IssueKey:    "OPS-3",
		Application: jira.LinkApplication{Name: "Confluence"},
		Object:      jira.LinkObject{URL: "https://example.atlassian.net/wiki/pages/1"},
	})
	if err != nil {
		t.Fatalf("NewWorkItemExternalLinkEnvelope(non-repository link) error = %v", err)
	}
	if _, ok := plainLink.Payload["linked_repository_id"]; ok {
		t.Fatalf("writer set linked_repository_id on a non-repository link: %#v", plainLink.Payload)
	}
	for id, payload := range map[string]map[string]any{
		"f-pr-link":    prLink.Payload,
		"f-other-repo": otherLink.Payload,
		"f-plain-link": plainLink.Payload,
	} {
		seedWriterShapeFact(ctx, t, conn, id, "s-jira", "g-jira", prLink.FactKind, observed, payload)
	}

	filter := serviceStoryTargetSupportFilter{
		Repository: repoID,
		TargetKind: "repository",
		TargetID:   repoID,
		Limit:      20,
	}
	support := readWriterShapeStorySupport(ctx, t, conn, filter)
	if got := IntVal(support, "evidence_count"); got != 1 {
		t.Fatalf("evidence_count = %d, want 1 (the pull-request link to %s); support = %#v", got, repoID, support)
	}
	evidence, _ := support["evidence"].([]map[string]any)
	if got := StringVal(evidence[0], "fact_id"); got != "f-pr-link" {
		t.Fatalf("evidence[0].fact_id = %q, want f-pr-link", got)
	}
	if got := IntVal(support, "work_item_count"); got != 1 {
		t.Fatalf("work_item_count = %d, want 1", got)
	}
}

func seedWriterShapeScope(ctx context.Context, t *testing.T, conn *sql.Conn, scopeID, generationID string, at time.Time) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'work_item_project', 'jira', $1, 'jira', $1, $2, $2, 'active', $3, '{}'::jsonb)`, scopeID, at, generationID); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ($1, $2, 'snapshot', $3, $3, 'active', '{}'::jsonb)`, generationID, scopeID, at); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
}

func seedWriterShapeFact(
	ctx context.Context,
	t *testing.T,
	conn *sql.Conn,
	factID, scopeID, generationID, kind string,
	at time.Time,
	payload map[string]any,
) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, 'jira', $1, $5, $5, FALSE, $6::jsonb)`, factID, scopeID, generationID, kind, at, string(raw)); err != nil {
		t.Fatalf("seed fact %s: %v", factID, err)
	}
}

// readWriterShapeStorySupport runs the shipped target-support statement and the
// shipped Go post-filter, the same two steps ServiceStoryTargetSupportEvidence
// runs, on a connection whose search_path points at the fixture schema.
func readWriterShapeStorySupport(
	ctx context.Context,
	t *testing.T,
	conn *sql.Conn,
	filter serviceStoryTargetSupportFilter,
) map[string]any {
	t.Helper()
	query, args := buildServiceStoryTargetSupportSQL(filter)
	if query == "" {
		t.Fatalf("buildServiceStoryTargetSupportSQL(%#v) returned no statement", filter)
	}
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("target support query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var facts []map[string]any
	for rows.Next() {
		payload, err := scanJSONPayload(rows)
		if err != nil {
			t.Fatalf("scan target support row: %v", err)
		}
		facts = append(facts, payload)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate target support rows: %v", err)
	}
	return buildStoryTargetSupport(filter, facts, false)
}
