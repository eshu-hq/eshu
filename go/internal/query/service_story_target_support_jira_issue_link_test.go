// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/support"
)

func jiraDerivedFact(id, kind, issueID, witness, observedAt string) map[string]any {
	fact := map[string]any{
		"fact_id":     id,
		"fact_kind":   kind,
		"scope_id":    "s-jira",
		"observed_at": observedAt,
		"payload":     map[string]any{"provider_work_item_id": issueID, "work_item_key": "OPS-1"},
	}
	if witness != "" {
		fact["linked_via_fact_id"] = witness
		fact["linked_via_repository"] = writerLinkedRepoID
	}
	return fact
}

func TestBuildStoryTargetSupportAttachesDerivedJiraRowsThroughTheirWitness(t *testing.T) {
	t.Parallel()

	otherRepoRow := jiraDerivedFact("rec-other-repo", support.WorkItemRecordKind, "20003", "link-x", "2026-09-30T12:00:00Z")
	otherRepoRow["linked_via_repository"] = "repo-someone-else"
	facts := []map[string]any{
		writerLinkFact("link-r", writerLinkedRepoID),
		jiraDerivedFact("rec", support.WorkItemRecordKind, "20001", "link-r", "2026-09-30T12:00:00Z"),
		jiraDerivedFact("tr", support.WorkItemTransitionKind, "20001", "link-r", "2026-09-30T12:00:00Z"),
		// Never evidence: no witness, a witness link that names another repository
		// (a row read for a different story), a blank issue id, a kind the join does
		// not derive.
		otherRepoRow,
		jiraDerivedFact("rec-no-witness", support.WorkItemRecordKind, "20002", "", "2026-09-30T12:00:00Z"),
		jiraDerivedFact("rec-blank-id", support.WorkItemRecordKind, "", "link-r", "2026-09-30T12:00:00Z"),
		jiraDerivedFact("meta", "work_item.project_metadata", "20001", "link-r", "2026-09-30T12:00:00Z"),
	}
	got := buildStoryTargetSupport(serviceStoryTargetSupportFilter{
		Repository: writerLinkedRepoID, TargetKind: "repository", TargetID: writerLinkedRepoID, Limit: serviceStoryTargetSupportLimit,
	}, facts, false)

	rows := querycontract.MapSliceValue(got, "evidence")
	if len(rows) != 3 {
		t.Fatalf("evidence rows = %d, want the link and the two witnessed derived rows: %#v", len(rows), rows)
	}
	wantBasis := map[string]string{"link-r": "linked_repository", "rec": "issue_linked_repository", "tr": "issue_linked_repository"}
	for _, row := range rows {
		id := querycontract.StringVal(row, "fact_id")
		if basis := querycontract.StringVal(row, "link_basis"); basis != wantBasis[id] {
			t.Fatalf("%s link_basis = %q, want %q", id, basis, wantBasis[id])
		}
		if id != "link-r" && querycontract.StringVal(row, "linked_via_fact_id") != "link-r" {
			t.Fatalf("%s lost its witness: %#v", id, row)
		}
	}
	if count := querycontract.IntVal(got, "work_item_count"); count != 3 {
		t.Fatalf("work_item_count = %d, want 3", count)
	}
}

func TestBuildStoryTargetSupportGatesDerivedJiraRowsForAServiceTarget(t *testing.T) {
	t.Parallel()

	facts := []map[string]any{jiraDerivedFact("rec", support.WorkItemRecordKind, "20001", "link-r", "2026-09-30T12:00:00Z")}
	service := func(count int, defines bool) serviceStoryTargetSupportFilter {
		return serviceStoryTargetSupportFilter{
			Repository: writerLinkedRepoID, TargetKind: "service", TargetID: "workload:payments", ServiceID: "workload:payments",
			Limit: serviceStoryTargetSupportLimit, RepositoryWorkloadCount: count, RepositoryDefinesTarget: defines,
		}
	}
	sole := buildStoryTargetSupport(service(1, true), facts, false)
	if rows := querycontract.MapSliceValue(sole, "evidence"); len(rows) != 1 ||
		querycontract.StringVal(rows[0], "link_basis") != "repository_sole_workload" {
		t.Fatalf("sole-workload service evidence = %#v, want one repository_sole_workload row", rows)
	}
	several := buildStoryTargetSupport(service(3, true), facts, false)
	if querycontract.IntVal(several, "evidence_count") != 0 || querycontract.IntVal(several, "ambiguous_count") != 1 {
		t.Fatalf("several-workload service = %#v, want the derived row ambiguous", several)
	}
	closed := buildStoryTargetSupport(service(1, false), facts, false)
	if querycontract.IntVal(closed, "evidence_count") != 0 || querycontract.IntVal(closed, "ambiguous_count") != 0 {
		t.Fatalf("service the graph did not confirm = %#v, want no rows", closed)
	}
}

func TestSortServiceStoryTargetSupportFactsRanksLinksThenRecordsThenTransitions(t *testing.T) {
	t.Parallel()

	facts := []map[string]any{
		jiraDerivedFact("tr-new", support.WorkItemTransitionKind, "1", "w", "2026-09-30T13:00:00Z"),
		jiraDerivedFact("rec-old", support.WorkItemRecordKind, "1", "w", "2026-09-30T10:00:00Z"),
		{"fact_id": "link-old", "fact_kind": "work_item.external_link", "observed_at": "2026-09-30T09:00:00Z"},
		{"fact_id": "pd-new", "fact_kind": support.ObservedPagerDutyServiceKind, "observed_at": "2026-09-30T14:00:00Z"},
		jiraDerivedFact("rec-new", support.WorkItemRecordKind, "2", "w", "2026-09-30T11:00:00Z"),
	}
	sortServiceStoryTargetSupportFacts(facts)
	var ids []string
	for _, fact := range facts {
		ids = append(ids, querycontract.StringVal(fact, "fact_id"))
	}
	if got, want := strings.Join(ids, ","), "pd-new,link-old,rec-new,rec-old,tr-new"; got != want {
		t.Fatalf("order = %s, want %s: links and routing first (newest first), then records, then transitions", got, want)
	}
}

func TestSourceOnlySQLSubtractsIssuesLinkedInTheSameScopeAndGeneration(t *testing.T) {
	t.Parallel()

	query, _ := buildServiceStoryTargetSupportSourceOnlySQL(serviceStoryTargetSupportFactKinds())
	for _, want := range []string{
		support.LinkedIssuesCTE + " AS MATERIALIZED (",
		support.LinkedIssuePredicate(),
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("source-only SQL missing %q:\n%s", want, query)
		}
	}
	if strings.Index(query, support.AdmissibleCorrelationsCTE+" AS MATERIALIZED") > strings.Index(query, support.LinkedIssuesCTE+" AS MATERIALIZED") {
		t.Fatalf("the linked-issue CTE must follow the correlation CTE in one WITH clause:\n%s", query)
	}
}
