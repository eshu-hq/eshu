// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	jirasupport "github.com/eshu-hq/eshu/go/internal/query/support"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// jiraIssueLinkFixture holds the repository ids the #7464 matrix asserts on.
type jiraIssueLinkFixture struct {
	repoID  string // R: linked by issues 1 and 2
	repo2ID string // R2: linked by issues 2 and 3
	repo3ID string // R3: nothing links to it
	repo4ID string // R4: its one link has a blank issue id, as does a transition
}

// TestServiceStoryTargetSupportJiraIssueLinkMatrixLive is the #7464
// correlation-truth matrix against Postgres. A Jira work_item.record or
// work_item.transition carries no repository, so it attaches to a repository
// story through the work_item.external_link of the same issue: same scope, same
// active generation, a non-blank provider_work_item_id on both sides, and a
// link whose linked_repository_id is the target. Fixture payloads carry the
// same durable keys the extracted satellite's envelope writers emit; envelope
// construction moved out of core with the #8858 extraction.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestServiceStoryTargetSupportJiraIssueLinkMatrixLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #7464 Jira issue-link matrix")
	}
	ctx := context.Background()
	reader, db := openStorySupportReader(ctx, t, dsn)
	fixture := seedJiraIssueLinkMatrix(ctx, t, db)

	t.Run("a repository story gets the linked issues' records and transitions after their links", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repoID, TargetKind: "repository", TargetID: fixture.repoID, Limit: 20,
		})
		// Links first (newest first), then records, then transitions, so a
		// busy issue's transitions never crowd out the link that justifies them.
		want := "f-l-other-scope,f-l-i2,f-l-i1b,f-l-i1a,f-rec-i2,f-rec-i1,f-tr-i2,f-tr-i1b,f-tr-i1a"
		if got := strings.Join(supportEvidenceFactIDs(support), ","); got != want {
			t.Fatalf("evidence fact ids = %q, want %q; support = %#v", got, want, support)
		}
		rows, _ := support["evidence"].([]map[string]any)
		for _, row := range rows {
			id := StringVal(row, "fact_id")
			switch {
			case strings.HasPrefix(id, "f-l-"):
				if got := StringVal(row, "link_basis"); got != "linked_repository" {
					t.Fatalf("%s link_basis = %q, want linked_repository", id, got)
				}
			default:
				if got := StringVal(row, "link_basis"); got != "issue_linked_repository" {
					t.Fatalf("%s link_basis = %q, want issue_linked_repository", id, got)
				}
			}
		}
		// The witness is the lowest-id qualifying link of the same issue, so an
		// issue with two links to R still yields its record once.
		witnesses := map[string]string{}
		for _, row := range rows {
			witnesses[StringVal(row, "fact_id")] = StringVal(row, "linked_via_fact_id")
		}
		for id, want := range map[string]string{
			"f-rec-i1": "f-l-i1a", "f-tr-i1a": "f-l-i1a", "f-tr-i1b": "f-l-i1a", "f-rec-i2": "f-l-i2", "f-tr-i2": "f-l-i2",
		} {
			if got := witnesses[id]; got != want {
				t.Fatalf("%s linked_via_fact_id = %q, want %q; witnesses = %v", id, got, want, witnesses)
			}
		}
		if got := IntVal(support, "work_item_count"); got != 9 {
			t.Fatalf("work_item_count = %d, want 9 (4 links, 2 records, 3 transitions)", got)
		}
	})

	t.Run("an issue linked to two repositories is evidence in both stories", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repo2ID, TargetKind: "repository", TargetID: fixture.repo2ID, Limit: 20,
		})
		want := "f-l-i3,f-l-i2r2,f-rec-i3,f-rec-i2,f-tr-i3,f-tr-i2"
		if got := strings.Join(supportEvidenceFactIDs(support), ","); got != want {
			t.Fatalf("R2 evidence fact ids = %q, want %q", got, want)
		}
	})

	t.Run("rows that no live same-scope link reaches stay source-only", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repo3ID, TargetKind: "repository", TargetID: fixture.repo3ID, Limit: 20,
		})
		if got := IntVal(support, "evidence_count"); got != 0 {
			t.Fatalf("evidence_count = %d, want 0 for a repository nothing links to; support = %#v", got, support)
		}
		coverage := mapValue(support, "coverage")
		// Unlinked and active: the record of an unlinked issue (i4), of an issue
		// whose only link is tombstoned (i5) or on a superseded generation (i6),
		// the transition with a blank issue id (its blank-id link names another
		// repository and must not reach it), a record whose id is linked only in
		// another scope (i9), the plain external link and the project metadata row. Linked to some repository, so neither evidence for
		// R3 nor source-only: issues 1, 2 and 3 with their transitions.
		for key, want := range map[string]int{
			"source_only_count":           7,
			"work_item_source_only_count": 7,
		} {
			if got := IntVal(coverage, key); got != want {
				t.Fatalf("coverage.%s = %d, want %d; coverage = %#v", key, got, want, coverage)
			}
		}
	})

	t.Run("a blank provider_work_item_id never joins", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repoID, TargetKind: "repository", TargetID: fixture.repoID, Limit: 20,
		})
		for _, id := range supportEvidenceFactIDs(support) {
			if strings.Contains(id, "blank") {
				t.Fatalf("blank-id row %s attached to R: %v", id, supportEvidenceFactIDs(support))
			}
		}
		// The fourth repository owns the one link with a blank issue id. Its story
		// reads that link as its own evidence and nothing else: a '' = '' join would
		// attach the blank-id transition to it, which only the row read's blank-id
		// guard prevents.
		fourth := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repo4ID, TargetKind: "repository", TargetID: fixture.repo4ID, Limit: 20,
		})
		if got := strings.Join(supportEvidenceFactIDs(fourth), ","); got != "f-l-blank" {
			t.Fatalf("fourth repository evidence = %q, want only its blank-id link f-l-blank (the blank-id transition must not join it)", got)
		}
		// The Go re-check (JiraFactLinked) also drops a blank-id row, so the story
		// alone cannot tell whether the statement's own blank-id guard is there.
		// Read the statement directly: it must return no row for the fourth
		// repository, whose only link has a blank issue id.
		statement, args := jirasupport.JiraIssueLinkSQL(fixture.repo4ID, 20)
		rows, err := db.QueryContext(ctx, statement, args...)
		if err != nil {
			t.Fatalf("JiraIssueLinkSQL(R4) error = %v", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				t.Fatalf("scan JiraIssueLinkSQL(R4) row: %v", err)
			}
			t.Errorf("JiraIssueLinkSQL(R4) returned a row for a blank issue id: %s", raw)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("JiraIssueLinkSQL(R4) rows: %v", err)
		}
	})

	t.Run("a link in another scope never links an issue with the same id", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repoID, TargetKind: "repository", TargetID: fixture.repoID, Limit: 20,
		})
		ids := strings.Join(supportEvidenceFactIDs(support), ",")
		if strings.Contains(ids, "f-rec-i9") {
			t.Fatalf("record i9 attached through a link in another scope: %s", ids)
		}
		if !strings.Contains(ids, "f-l-other-scope") {
			t.Fatalf("the other scope's own link to R must still be evidence: %s", ids)
		}
	})

	t.Run("the row bound keeps links ahead of records and transitions", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceStoryTargetSupportFilter{
			Repository: fixture.repoID, TargetKind: "repository", TargetID: fixture.repoID, Limit: 4,
		})
		if !truncated(support) {
			t.Fatalf("section must report truncated with 4 of 9 rows: %#v", support)
		}
		want := "f-l-other-scope,f-l-i2,f-l-i1b,f-l-i1a"
		if got := strings.Join(supportEvidenceFactIDs(support), ","); got != want {
			t.Fatalf("limit-4 evidence = %q, want %q", got, want)
		}
	})

	serviceFilter := func(count int, defines bool) serviceStoryTargetSupportFilter {
		return serviceStoryTargetSupportFilter{
			Repository: fixture.repoID, TargetKind: "service", TargetID: "workload:payments",
			ServiceID: "workload:payments", Limit: 20,
			RepositoryWorkloadCount: count, RepositoryDefinesTarget: defines,
		}
	}
	t.Run("a service target whose repository defines only it owns the derived rows", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceFilter(1, true))
		for _, id := range []string{"f-rec-i1", "f-tr-i1a"} {
			if !strings.Contains(strings.Join(supportEvidenceFactIDs(support), ","), id) {
				t.Fatalf("service evidence missing %s: %v", id, supportEvidenceFactIDs(support))
			}
		}
		rows, _ := support["evidence"].([]map[string]any)
		for _, row := range rows {
			if StringVal(row, "fact_kind") != "work_item.external_link" {
				if got := StringVal(row, "link_basis"); got != "repository_sole_workload" {
					t.Fatalf("%s link_basis = %q, want repository_sole_workload", StringVal(row, "fact_id"), got)
				}
			}
		}
	})
	t.Run("a service target whose repository defines several is ambiguous for derived rows", func(t *testing.T) {
		support := readStorySupport(ctx, t, reader, serviceFilter(3, true))
		if got := IntVal(support, "evidence_count"); got != 0 {
			t.Fatalf("evidence_count = %d, want 0 when the repository defines several workloads", got)
		}
		if got := IntVal(support, "ambiguous_count"); got != 9 {
			t.Fatalf("ambiguous_count = %d, want 9 (every link and derived row); support = %#v", got, support)
		}
	})
	t.Run("a service target the graph did not confirm stays closed", func(t *testing.T) {
		for _, filter := range []serviceStoryTargetSupportFilter{serviceFilter(1, false), serviceFilter(0, true)} {
			support := readStorySupport(ctx, t, reader, filter)
			if got := len(supportEvidenceFactIDs(support)); got != 0 {
				t.Fatalf("closed service target returned evidence: %v", supportEvidenceFactIDs(support))
			}
		}
	})
}

func truncated(support map[string]any) bool {
	coverage := mapValue(support, "coverage")
	value, _ := coverage["truncated"].(bool)
	return value
}

func seedJiraIssueLinkMatrix(ctx context.Context, t *testing.T, db *sql.DB) jiraIssueLinkFixture {
	t.Helper()
	observed := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for _, scope := range []struct{ scopeID, generation string }{
		{"s-jira", "g-jira"},
		{"s-jira2", "g-jira2"},
	} {
		mustExec(ctx, t, db, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'work_item_project', 'jira', $1, 'jira', $1, $2, $2, 'active', $3, '{}'::jsonb)`, scope.scopeID, observed, scope.generation)
		mustExec(ctx, t, db, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ($1, $2, 'snapshot', $3, $3, 'active', '{}'::jsonb)`, scope.generation, scope.scopeID, observed)
	}
	mustExec(ctx, t, db, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ('g-jira-old', 's-jira', 'snapshot', $1, $1, 'superseded', '{}'::jsonb)`, observed)

	link := func(issueID, rawURL string) facts.Envelope {
		return facts.Envelope{
			FactKind: facts.WorkItemExternalLinkFactKind,
			Payload: map[string]any{
				"provider_work_item_id": issueID,
				"linked_repository_id":  linkRepositoryID(rawURL),
			},
		}
	}
	record := func(issueID, key string) facts.Envelope {
		return facts.Envelope{
			FactKind: facts.WorkItemRecordFactKind,
			Payload: map[string]any{
				"provider_work_item_id": issueID,
				"work_item_key":         key,
			},
		}
	}
	transition := func(issueID string) facts.Envelope {
		return facts.Envelope{
			FactKind: facts.WorkItemTransitionFactKind,
			Payload: map[string]any{
				"provider_work_item_id": issueID,
			},
		}
	}
	const (
		payments = "https://github.com/acme/payments/pull/"
		billing  = "https://github.com/acme/billing/pull/"
		other4   = "https://github.com/acme/other/pull/"
	)
	seed := func(id, scopeID, generation string, env facts.Envelope, at time.Time, tombstone bool) {
		t.Helper()
		seedMatrixFact(ctx, t, db, id, scopeID, generation, env, at, tombstone)
	}
	h := func(n int) time.Time { return observed.Add(-time.Duration(n) * time.Hour) }

	// Issue 1: record, two transitions, two links to R (the record must appear once).
	seed("f-rec-i1", "s-jira", "g-jira", record("20001", "OPS-1"), observed, false)
	seed("f-tr-i1a", "s-jira", "g-jira", transition("20001"), observed, false)
	seed("f-tr-i1b", "s-jira", "g-jira", transition("20001"), observed, false)
	seed("f-l-i1a", "s-jira", "g-jira", link("20001", payments+"1"), h(4), false)
	seed("f-l-i1b", "s-jira", "g-jira", link("20001", payments+"2"), h(3), false)
	// Issue 2: linked to R and to R2.
	seed("f-rec-i2", "s-jira", "g-jira", record("20002", "OPS-2"), observed, false)
	seed("f-tr-i2", "s-jira", "g-jira", transition("20002"), observed, false)
	seed("f-l-i2", "s-jira", "g-jira", link("20002", payments+"3"), h(1), false)
	seed("f-l-i2r2", "s-jira", "g-jira", link("20002", billing+"4"), h(5), false)
	// Issue 3: linked to R2 only.
	seed("f-rec-i3", "s-jira", "g-jira", record("20003", "OPS-3"), observed, false)
	seed("f-tr-i3", "s-jira", "g-jira", transition("20003"), observed, false)
	seed("f-l-i3", "s-jira", "g-jira", link("20003", billing+"5"), h(2), false)
	// Issue 4: no link at all. Issue 5: its only link is tombstoned. Issue 6: its
	// only link sits on a superseded generation.
	seed("f-rec-i4", "s-jira", "g-jira", record("20004", "OPS-4"), observed, false)
	seed("f-rec-i5", "s-jira", "g-jira", record("20005", "OPS-5"), observed, false)
	seed("f-l-i5", "s-jira", "g-jira", link("20005", payments+"6"), observed, true)
	seed("f-rec-i6", "s-jira", "g-jira", record("20006", "OPS-6"), observed, false)
	seed("f-l-i6", "s-jira", "g-jira-old", link("20006", payments+"7"), observed, false)
	// A blank issue id on a transition and on a link naming a fourth repository:
	// '' = '' must not join them.
	seed("f-tr-blank", "s-jira", "g-jira", transition(""), observed, false)
	seed("f-l-blank", "s-jira", "g-jira", link("", other4+"8"), observed, false)
	// Issue id 20009: a record in this scope, linked to R only in another scope.
	seed("f-rec-i9", "s-jira", "g-jira", record("20009", "OPS-9"), observed, false)
	seed("f-l-other-scope", "s-jira2", "g-jira2", link("20009", payments+"9"), observed, false)
	// A link with no repository and a metadata row stay source-only.
	seed("f-l-plain", "s-jira", "g-jira", facts.Envelope{
		FactKind: facts.WorkItemExternalLinkFactKind,
		Payload: map[string]any{
			"provider_work_item_id": "20010",
			"linked_repository_id":  "",
		},
	}, observed, false)
	seed("f-metadata", "s-jira", "g-jira", facts.Envelope{
		FactKind: facts.WorkItemProjectMetadataFactKind,
		Payload: map[string]any{
			"provider_work_item_id": "20010",
		},
	}, observed, false)

	repoID := linkRepositoryID(payments + "1")
	repo2 := linkRepositoryID(billing + "1")
	repo3 := linkRepositoryID("https://github.com/acme/unlinked/pull/1")
	repo4 := linkRepositoryID(other4 + "1")
	if repoID == "" || repo2 == "" || repo3 == "" || repo4 == "" ||
		repoID == repo2 || repoID == repo3 || repoID == repo4 || repo2 == repo4 || repo3 == repo4 {
		t.Fatalf("writer ids not distinct canonical repository ids: %q %q %q %q", repoID, repo2, repo3, repo4)
	}
	return jiraIssueLinkFixture{repoID: repoID, repo2ID: repo2, repo3ID: repo3, repo4ID: repo4}
}

// linkRepositoryID mirrors the extracted satellite's typed-link rule for the
// fixture URLs above: a GitHub pull-request URL resolves to the canonical
// repository id of its owner/repo remote, and anything else carries no
// durable link, so the read path treats it as source-only.
func linkRepositoryID(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	path := strings.Trim(parsed.Path, "/")
	idx := strings.Index(strings.ToLower(path), "/pull/")
	if idx <= 0 {
		return ""
	}
	repoPath := path[:idx]
	if len(strings.Split(repoPath, "/")) != 2 {
		return ""
	}
	id, err := repositoryidentity.CanonicalRepositoryID(parsed.Scheme+"://"+parsed.Host+"/"+repoPath, "")
	if err != nil {
		return ""
	}
	return id
}
