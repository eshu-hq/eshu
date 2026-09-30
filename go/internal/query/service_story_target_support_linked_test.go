// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const writerLinkedRepoID = "repository:r_1a2b3c4d"

// writerLinkFact is the row shape the target-support read returns for a real
// Jira pull-request link: the keys jira.NewWorkItemExternalLinkEnvelope emits,
// and none of candidate_refs, evidence_refs or linked_entities.
func writerLinkFact(factID, linkedRepositoryID string) map[string]any {
	payload := map[string]any{
		"provider":                 "jira_cloud",
		"provider_work_item_id":    "20001",
		"work_item_key":            "OPS-1",
		"provider_remote_link_id":  "10001",
		"application_name":         "GitHub",
		"correlation_anchor_class": "github_pull_request",
		"url_fingerprint":          "urlfp-1",
	}
	if linkedRepositoryID != "" {
		payload["linked_repository_id"] = linkedRepositoryID
	}
	return map[string]any{
		"fact_id":   factID,
		"fact_kind": "work_item.external_link",
		"payload":   payload,
	}
}

// TestBuildServiceStoryTargetSupportSQLBindsWriterLinkKey is the #7138 builder
// guard. The row read matches the one durable key a support writer emits,
// payload->>'linked_repository_id', in its plain (index-usable) form, and no
// longer binds the three documentation ref keys no support writer emits.
func TestBuildServiceStoryTargetSupportSQLBindsWriterLinkKey(t *testing.T) {
	t.Parallel()

	query, args := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{
		Repository: writerLinkedRepoID,
		TargetKind: "repository",
		TargetID:   writerLinkedRepoID,
		Limit:      serviceStoryTargetSupportLimit,
	})

	assertSupportSQLContainsAll(
		t, query,
		"fact.fact_kind = 'work_item.external_link'",
		"fact.payload->>'linked_repository_id' = $",
		"fact.is_tombstone = FALSE",
		"fact.generation_id = scope.active_generation_id",
		"generation.status = 'active'",
		"OFFSET 0",
		"ORDER BY fact.observed_at DESC, fact.fact_id DESC",
	)
	for _, forbidden := range []string{
		"candidate_refs", "evidence_refs", "linked_entities", "@>", "NULLIF",
		"service_name", "mention_text", " like ", "lower(",
	} {
		if strings.Contains(strings.ToLower(query), strings.ToLower(forbidden)) {
			t.Fatalf("target support SQL must not carry %q:\n%s", forbidden, query)
		}
	}
	if len(args) != 2 || args[0] != writerLinkedRepoID {
		t.Fatalf("args = %#v, want [repository id, limit+1]", args)
	}
}

// TestBuildServiceStoryTargetSupportSQLNeedsAKnownRepository proves the
// builder returns no statement rather than an unbounded or empty-key read.
func TestBuildServiceStoryTargetSupportSQLNeedsAKnownRepository(t *testing.T) {
	t.Parallel()

	for name, filter := range map[string]serviceStoryTargetSupportFilter{
		"empty filter":              {},
		"blank repository":          {Repository: "  ", TargetKind: "repository", TargetID: " ", Limit: 10},
		"service without repo":      {TargetKind: "service", TargetID: "workload:a", RepositoryWorkloadCount: 1, RepositoryDefinesTarget: true},
		"service, graph says no":    {Repository: writerLinkedRepoID, TargetKind: "service", TargetID: "workload:a", RepositoryWorkloadCount: 1},
		"service, graph unread":     {Repository: writerLinkedRepoID, TargetKind: "service", TargetID: "workload:a"},
		"unknown target kind":       {Repository: writerLinkedRepoID, TargetKind: "team", TargetID: "t"},
		"service defines target 0n": {Repository: writerLinkedRepoID, TargetKind: "service", TargetID: "workload:a", RepositoryDefinesTarget: true},
	} {
		if query, args := buildServiceStoryTargetSupportSQL(filter); query != "" || args != nil {
			t.Fatalf("%s: buildServiceStoryTargetSupportSQL() = (%q, %#v), want no statement", name, query, args)
		}
	}
	for name, filter := range map[string]serviceStoryTargetSupportFilter{
		"service, sole workload":  {Repository: writerLinkedRepoID, TargetKind: "service", TargetID: "workload:a", RepositoryWorkloadCount: 1, RepositoryDefinesTarget: true},
		"service, several":        {Repository: writerLinkedRepoID, TargetKind: "service", TargetID: "workload:a", RepositoryWorkloadCount: 3, RepositoryDefinesTarget: true},
		"repository, only target": {TargetKind: "repository", TargetID: writerLinkedRepoID},
	} {
		if query, _ := buildServiceStoryTargetSupportSQL(filter); query == "" {
			t.Fatalf("%s: buildServiceStoryTargetSupportSQL() returned no statement, want the linked-row read", name)
		}
	}
}

// TestBuildServiceStoryTargetSupportSourceOnlySQLUsesLinkPredicate keeps the
// #7138 source-only predicate: an active support fact with no durable target
// link, where only an external_link carrying linked_repository_id is linked.
func TestBuildServiceStoryTargetSupportSourceOnlySQLUsesLinkPredicate(t *testing.T) {
	t.Parallel()

	query, _ := buildServiceStoryTargetSupportSourceOnlySQL(serviceStoryTargetSupportFactKinds())
	assertSupportSQLContainsAll(
		t, query,
		"NOT (fact.fact_kind = 'work_item.external_link' AND NULLIF(fact.payload->>'linked_repository_id', '') IS NOT NULL)",
	)
	for _, forbidden := range []string{"candidate_refs", "evidence_refs", "linked_entities", "jsonb_array_length"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("source-only SQL still carries %q:\n%s", forbidden, query)
		}
	}
}

// TestServiceStoryTargetSupportLinkIndexMatchesQuery binds the row read to its
// partial expression index (#7138), derived from the builder and not
// hand-copied: the index carries the same kind literal, tombstone predicate and
// key expression as the statement, so Postgres can prove the predicate in
// custom and generic plans alike.
func TestServiceStoryTargetSupportLinkIndexMatchesQuery(t *testing.T) {
	t.Parallel()

	query, _ := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{
		Repository: writerLinkedRepoID, TargetKind: "repository", TargetID: writerLinkedRepoID, Limit: 10,
	})
	migration := normalizeSQLWhitespace(migrationSQLByName(t, "fact_records_story_support_link_repo_idx"))
	for _, want := range []string{
		"ON fact_records (scope_id, generation_id, (payload->>'linked_repository_id'))",
		"WHERE fact_kind = 'work_item.external_link' AND is_tombstone = FALSE",
	} {
		if !strings.Contains(migration, normalizeSQLWhitespace(want)) {
			t.Fatalf("link-repo index migration missing %q:\n%s", want, migration)
		}
	}
	for _, want := range []string{
		"fact.fact_kind = 'work_item.external_link'",
		"fact.is_tombstone = FALSE",
		"fact.payload->>'linked_repository_id' = $",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("row read missing the index-provable fragment %q:\n%s", want, query)
		}
	}
}

func TestBuildStoryTargetSupportAttachesWriterLinkToRepositoryTarget(t *testing.T) {
	t.Parallel()

	got := buildStoryTargetSupport(serviceStoryTargetSupportFilter{
		Repository: writerLinkedRepoID,
		TargetKind: "repository",
		TargetID:   writerLinkedRepoID,
		Limit:      serviceStoryTargetSupportLimit,
	}, []map[string]any{writerLinkFact("link-r", writerLinkedRepoID)}, false)

	if gotCount := querycontract.IntVal(got, "evidence_count"); gotCount != 1 {
		t.Fatalf("evidence_count = %d, want 1: %#v", gotCount, got)
	}
	row := querycontract.MapSliceValue(got, "evidence")[0]
	if gotBasis := querycontract.StringVal(row, "link_basis"); gotBasis != "linked_repository" {
		t.Fatalf("evidence[0].link_basis = %q, want linked_repository", gotBasis)
	}
	payload := querycontract.MapValue(row, "payload")
	if gotID := querycontract.StringVal(payload, "linked_repository_id"); gotID != writerLinkedRepoID {
		t.Fatalf("evidence[0].payload.linked_repository_id = %q, want %q", gotID, writerLinkedRepoID)
	}
}

// TestBuildStoryTargetSupportDropsRowsThatDoNotCarryTheTargetLink is the Go
// half of the two-step read: a row that names another repository, no
// repository, or only the documentation ref keys (which no support writer
// emits) is not evidence, whatever the SQL returned.
func TestBuildStoryTargetSupportDropsRowsThatDoNotCarryTheTargetLink(t *testing.T) {
	t.Parallel()

	seededRefs := writerLinkFact("seeded-refs", "")
	seededRefs["payload"].(map[string]any)["candidate_refs"] = []any{map[string]any{"kind": "repository", "id": writerLinkedRepoID}}
	got := buildStoryTargetSupport(serviceStoryTargetSupportFilter{
		Repository: writerLinkedRepoID,
		TargetKind: "repository",
		TargetID:   writerLinkedRepoID,
		Limit:      serviceStoryTargetSupportLimit,
	}, []map[string]any{
		writerLinkFact("other-repo", "repository:r_ffffffff"),
		writerLinkFact("no-key", ""),
		seededRefs,
	}, false)

	if gotCount := querycontract.IntVal(got, "evidence_count"); gotCount != 0 {
		t.Fatalf("evidence_count = %d, want 0 for rows without the target's link: %#v", gotCount, got)
	}
	if gotCount := querycontract.IntVal(got, "ambiguous_count"); gotCount != 0 {
		t.Fatalf("ambiguous_count = %d, want 0", gotCount)
	}
}

func TestBuildStoryTargetSupportServiceTargetGatesOnGraphDefines(t *testing.T) {
	t.Parallel()

	service := func(count int, defines bool) serviceStoryTargetSupportFilter {
		return serviceStoryTargetSupportFilter{
			Repository:              writerLinkedRepoID,
			TargetKind:              "service",
			TargetID:                "workload:payments-api",
			ServiceID:               "workload:payments-api",
			Limit:                   serviceStoryTargetSupportLimit,
			RepositoryWorkloadCount: count,
			RepositoryDefinesTarget: defines,
		}
	}
	rows := []map[string]any{writerLinkFact("l1", writerLinkedRepoID), writerLinkFact("l2", writerLinkedRepoID)}

	t.Run("sole defined workload is the target", func(t *testing.T) {
		got := buildStoryTargetSupport(service(1, true), rows, false)
		if gotCount := querycontract.IntVal(got, "evidence_count"); gotCount != 2 {
			t.Fatalf("evidence_count = %d, want 2: %#v", gotCount, got)
		}
		for _, row := range querycontract.MapSliceValue(got, "evidence") {
			if gotBasis := querycontract.StringVal(row, "link_basis"); gotBasis != "repository_sole_workload" {
				t.Fatalf("link_basis = %q, want repository_sole_workload", gotBasis)
			}
		}
		if gotCount := querycontract.IntVal(querycontract.MapValue(got, "coverage"), "repository_workload_count"); gotCount != 1 {
			t.Fatalf("coverage.repository_workload_count = %d, want 1", gotCount)
		}
	})

	t.Run("repository defines several workloads including the target", func(t *testing.T) {
		got := buildStoryTargetSupport(service(2, true), rows, false)
		if gotCount := querycontract.IntVal(got, "evidence_count"); gotCount != 0 {
			t.Fatalf("evidence_count = %d, want 0", gotCount)
		}
		if gotCount := querycontract.IntVal(got, "ambiguous_count"); gotCount != 2 {
			t.Fatalf("ambiguous_count = %d, want 2", gotCount)
		}
		missing := querycontract.MapSliceValue(got, "missing_evidence")
		if len(missing) != 1 || querycontract.StringVal(missing[0], "reason") != "support_correlation_ambiguous" {
			t.Fatalf("missing_evidence = %#v, want support_correlation_ambiguous", missing)
		}
		if detail := querycontract.StringVal(missing[0], "detail"); !strings.Contains(detail, "2 workloads") {
			t.Fatalf("missing_evidence detail = %q, want it to name the 2 workloads", detail)
		}
		if gotCount := querycontract.IntVal(querycontract.MapValue(got, "coverage"), "repository_workload_count"); gotCount != 2 {
			t.Fatalf("coverage.repository_workload_count = %d, want 2", gotCount)
		}
	})

	for name, filter := range map[string]serviceStoryTargetSupportFilter{
		"repository defines none":         service(0, false),
		"graph unavailable":               service(0, false),
		"target not among defined":        service(1, false),
		"several defined, target missing": service(2, false),
	} {
		t.Run(name+" fails closed", func(t *testing.T) {
			got := buildStoryTargetSupport(filter, rows, false)
			if gotCount := querycontract.IntVal(got, "evidence_count"); gotCount != 0 {
				t.Fatalf("evidence_count = %d, want 0 (fail closed)", gotCount)
			}
			if gotCount := querycontract.IntVal(got, "ambiguous_count"); gotCount != 0 {
				t.Fatalf("ambiguous_count = %d, want 0 (fail closed)", gotCount)
			}
		})
	}
}
