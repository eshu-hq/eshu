// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package support

import (
	"strings"
	"testing"
)

func TestJiraIssueLinkSQLIsClosedForABlankRepository(t *testing.T) {
	t.Parallel()

	for _, repository := range []string{"", "   ", "\t"} {
		query, args := JiraIssueLinkSQL(repository, 10)
		if query != "" || args != nil {
			t.Fatalf("JiraIssueLinkSQL(%q) = (%q, %v), want no statement", repository, query, args)
		}
	}
}

func TestJiraIssueLinkSQLBindsTheRepositoryAndOneExtraRow(t *testing.T) {
	t.Parallel()

	query, args := JiraIssueLinkSQL(" repo-x ", 10)
	if len(args) != 2 || args[0] != "repo-x" || args[1] != 11 {
		t.Fatalf("args = %#v, want the trimmed repository and limit+1", args)
	}
	for _, want := range []string{
		"fact.fact_kind = '" + WorkItemExternalLinkKind + "'",
		"fact.payload->>'linked_repository_id' = $1",
		"fact.fact_kind = '" + WorkItemRecordKind + "'",
		"fact.fact_kind = '" + WorkItemTransitionKind + "'",
		"fact.payload->>'provider_work_item_id' = linked.issue_id",
		"fact.scope_id = linked.scope_id",
		"fact.generation_id = linked.generation_id",
		"generation.status = 'active'",
		"link.issue_id <> ''",
		"MIN(link.fact_id) AS link_fact_id",
		"'linked_via_fact_id', ranked.link_fact_id",
		"ORDER BY ranked.kind_rank, ranked.observed_at DESC, ranked.fact_id DESC",
		"LIMIT $2",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("JiraIssueLinkSQL missing %q:\n%s", want, query)
		}
	}
	if got := strings.Count(query, "fact.is_tombstone = FALSE"); got != 3 {
		t.Errorf("tombstone predicate count = %d, want 3 (link, record and transition probes):\n%s", got, query)
	}
}

// TestJiraIssueLinkSQLKeepsItsProbesFencedAndGated guards the plan shape the
// #7464 proof measured: each probe is fenced so the planner keeps it a LATERAL,
// and the transition probe runs only when the records did not fill the bound.
func TestJiraIssueLinkSQLKeepsItsProbesFencedAndGated(t *testing.T) {
	t.Parallel()

	query, _ := JiraIssueLinkSQL("repo-x", 10)
	if got := strings.Count(query, "OFFSET 0"); got != 3 {
		t.Errorf("OFFSET 0 fences = %d, want 3 (link, record and transition probes):\n%s", got, query)
	}
	if got := strings.Count(query, "MATERIALIZED"); got != 3 {
		t.Errorf("MATERIALIZED expressions = %d, want 3 (linked, records, transitions):\n%s", got, query)
	}
	if !strings.Contains(query, "WHERE (SELECT COUNT(*) FROM records) < $2") {
		t.Errorf("the transition probe is not gated on the records filling the bound:\n%s", query)
	}
	recordsAt := strings.Index(query, "records AS MATERIALIZED")
	transitionsAt := strings.Index(query, "transitions AS MATERIALIZED")
	if recordsAt < 0 || transitionsAt < recordsAt {
		t.Errorf("records must be defined before the gated transitions:\n%s", query)
	}
	for _, banned := range []string{"work_item_key", "LIKE", "ILIKE", "title", "summary"} {
		if strings.Contains(query, banned) {
			t.Errorf("JiraIssueLinkSQL joins or filters on %q; the issue id is the only durable key:\n%s", banned, query)
		}
	}
}

func TestLinkedIssuesSQLIsAFollowOnCTEWithoutBlankIds(t *testing.T) {
	t.Parallel()

	query := LinkedIssuesSQL()
	if !strings.HasPrefix(query, ", "+LinkedIssuesCTE+" AS MATERIALIZED (") {
		t.Errorf("LinkedIssuesSQL must follow another CTE and be materialized once:\n%s", query)
	}
	for _, want := range []string{
		"fact.fact_kind = '" + WorkItemExternalLinkKind + "'",
		"fact.is_tombstone = FALSE",
		"fact.payload->>'linked_repository_id' IS NOT NULL",
		"fact.payload->>'linked_repository_id' <> ''",
		"lgen.status = 'active'",
		"COALESCE(link.issue_id, '') <> ''",
		"OFFSET 0",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("LinkedIssuesSQL missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "$") {
		t.Errorf("the linked-issue set is global and must not bind a repository:\n%s", query)
	}
}

// TestLinkedIssuePredicateIsTwoValued guards the #6807 rule the source-only count
// follows: the predicate sits under NOT, so a NULL anywhere in it would silently
// drop a record or transition from the count.
func TestLinkedIssuePredicateIsTwoValued(t *testing.T) {
	t.Parallel()

	predicate := LinkedIssuePredicate()
	for _, want := range []string{
		"fact.fact_kind IN ('" + WorkItemRecordKind + "', '" + WorkItemTransitionKind + "')",
		"COALESCE(fact.payload->>'provider_work_item_id', '') <> ''",
		"(scope.scope_id, scope.active_generation_id, fact.payload->>'provider_work_item_id') IN (SELECT li.scope_id, li.generation_id, li.issue_id FROM " + LinkedIssuesCTE + " AS li)",
	} {
		if !strings.Contains(predicate, want) {
			t.Errorf("LinkedIssuePredicate missing %q:\n%s", want, predicate)
		}
	}
	if strings.Contains(predicate, "NOT IN") {
		t.Errorf("LinkedIssuePredicate must not use NOT IN:\n%s", predicate)
	}
}

func TestJiraFactLinked(t *testing.T) {
	t.Parallel()

	const target = "repo-r"
	row := func(kind, issueID, witness, linkedRepo string) map[string]any {
		return map[string]any{
			"fact_kind":             kind,
			"payload":               map[string]any{"provider_work_item_id": issueID},
			"linked_via_fact_id":    witness,
			"linked_via_repository": linkedRepo,
		}
	}
	tests := []struct {
		name string
		fact map[string]any
		repo string
		want bool
	}{
		{"record with a witness", row(WorkItemRecordKind, "20001", "f-link", target), target, true},
		{"transition with a witness", row(WorkItemTransitionKind, "20001", "f-link", target), target, true},
		{"target is trimmed like a story target", row(WorkItemRecordKind, "20001", "f-link", target), "  " + target + " ", true},
		{"record without a witness", row(WorkItemRecordKind, "20001", "", target), target, false},
		{"record with a blank issue id", row(WorkItemRecordKind, "", "f-link", target), target, false},
		{"record with no payload", map[string]any{"fact_kind": WorkItemRecordKind, "linked_via_fact_id": "f-link", "linked_via_repository": target}, target, false},
		{"witness link names another repository", row(WorkItemRecordKind, "20001", "f-link", "repo-other"), target, false},
		{"witness link names no repository", row(WorkItemRecordKind, "20001", "f-link", ""), target, false},
		{"blank target links nothing", row(WorkItemRecordKind, "20001", "f-link", ""), "  ", false},
		{"external link is not a derived row", row(WorkItemExternalLinkKind, "20001", "f-link", target), target, false},
		{"metadata is never linked", row("work_item.project_metadata", "20001", "f-link", target), target, false},
		{"routing kind is not a Jira row", row(AppliedPagerDutyResourceKind, "20001", "f-link", target), target, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := JiraFactLinked(tt.fact, tt.repo); got != tt.want {
				t.Fatalf("JiraFactLinked() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsJiraIssueFactNamesOnlyRecordAndTransition(t *testing.T) {
	t.Parallel()

	for kind, want := range map[string]bool{
		WorkItemRecordKind:           true,
		WorkItemTransitionKind:       true,
		WorkItemExternalLinkKind:     false,
		"work_item.project_metadata": false,
		"work_item.metadata_warning": false,
		AppliedPagerDutyResourceKind: false,
	} {
		if got := IsJiraIssueFact(kind); got != want {
			t.Errorf("IsJiraIssueFact(%q) = %v, want %v", kind, got, want)
		}
	}
}
