// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package support

import "strings"

// The Jira fact kinds a story target-support read links to a repository through
// the same issue's external link (#7464).
const (
	// WorkItemRecordKind is a Jira issue. It carries no repository.
	WorkItemRecordKind = "work_item.record"
	// WorkItemTransitionKind is one field change of a Jira issue. It carries no
	// repository either.
	WorkItemTransitionKind = "work_item.transition"
	// WorkItemExternalLinkKind is a Jira remote link. The collector stamps
	// linked_repository_id on a pull or merge request link it can resolve.
	WorkItemExternalLinkKind = "work_item.external_link"

	// workItemIssueKey is the one durable key the three kinds share: the Jira
	// issue id, unique per site. work_item_key is never joined on, because it
	// changes when an issue moves.
	workItemIssueKey = "provider_work_item_id"

	// LinkedIssuesCTE is the name of the common table expression the source-only
	// statement defines with LinkedIssuesSQL.
	LinkedIssuesCTE = "linked_issues"
)

// IssueKey is the Jira issue id of a work item fact, written as
// fact_records_story_support_issue_idx (migration 155) spells its key.
func IssueKey(alias string) string {
	return alias + ".payload->>'" + workItemIssueKey + "'"
}

// JiraIssueLinkSQL renders the bounded read of the work_item.record and
// work_item.transition facts of every issue that has a live
// work_item.external_link to repositoryID: same scope, same active generation, a
// non-blank provider_work_item_id on both sides. It returns "" and no arguments
// for a blank repository. The arguments are repositoryID and limit+1.
//
// The statement is the shape the #7464 plan proof measured:
//
//  1. the links of repositoryID are probed through
//     fact_records_story_support_link_repo_idx (migration 152) per active scope,
//     one row per issue, with the lowest link fact id as the join witness;
//  2. each linked issue's records are probed through
//     fact_records_story_support_issue_idx, keyed on the fact kind and the issue
//     id, fenced by OFFSET 0 so the planner cannot flatten the LATERAL;
//  3. the transitions are read only when the records did not fill the bound
//     (the gate is a one-time filter, so the probe never runs otherwise).
//
// Records rank ahead of transitions, because an issue can carry up to a hundred
// transitions and a bounded section would otherwise hold only those. An issue
// with several links to repositoryID yields each fact once, and an issue linked to
// two repositories yields its facts for each. Every row carries linked_via_fact_id,
// which JiraFactLinked re-checks.
func JiraIssueLinkSQL(repositoryID string, limit int) (string, []any) {
	if strings.TrimSpace(repositoryID) == "" {
		return "", nil
	}
	return `
WITH linked AS MATERIALIZED (
  SELECT link.scope_id, link.generation_id, link.issue_id, MIN(link.fact_id) AS link_fact_id
  FROM ingestion_scopes AS scope
  JOIN scope_generations AS generation
    ON generation.scope_id = scope.scope_id
   AND generation.generation_id = scope.active_generation_id
  CROSS JOIN LATERAL (
    SELECT fact.fact_id, fact.scope_id, fact.generation_id, ` + IssueKey("fact") + ` AS issue_id
    FROM fact_records AS fact
    WHERE fact.scope_id = scope.scope_id
      AND fact.generation_id = scope.active_generation_id
      AND fact.fact_kind = '` + WorkItemExternalLinkKind + `'
      AND fact.is_tombstone = FALSE
      AND fact.payload->>'linked_repository_id' = $1
    OFFSET 0
  ) AS link
  WHERE generation.status = 'active'
    AND link.issue_id <> ''
  GROUP BY link.scope_id, link.generation_id, link.issue_id
), records AS MATERIALIZED (
  SELECT probe.fact_id, probe.fact_kind, probe.scope_id, probe.generation_id, probe.source_system,
         probe.source_record_id, probe.observed_at, probe.payload, linked.link_fact_id
  FROM linked
  CROSS JOIN LATERAL (
    SELECT ` + issueFactColumns + `
    FROM fact_records AS fact
    WHERE fact.scope_id = linked.scope_id
      AND fact.generation_id = linked.generation_id
      AND fact.fact_kind = '` + WorkItemRecordKind + `'
      AND fact.is_tombstone = FALSE
      AND ` + IssueKey("fact") + ` = linked.issue_id
    OFFSET 0
  ) AS probe
  ORDER BY probe.observed_at DESC, probe.fact_id DESC
  LIMIT $2
), transitions AS MATERIALIZED (
  SELECT probe.fact_id, probe.fact_kind, probe.scope_id, probe.generation_id, probe.source_system,
         probe.source_record_id, probe.observed_at, probe.payload, linked.link_fact_id
  FROM linked
  CROSS JOIN LATERAL (
    SELECT ` + issueFactColumns + `
    FROM fact_records AS fact
    WHERE (SELECT COUNT(*) FROM records) < $2
      AND fact.scope_id = linked.scope_id
      AND fact.generation_id = linked.generation_id
      AND fact.fact_kind = '` + WorkItemTransitionKind + `'
      AND fact.is_tombstone = FALSE
      AND ` + IssueKey("fact") + ` = linked.issue_id
    OFFSET 0
  ) AS probe
  ORDER BY probe.observed_at DESC, probe.fact_id DESC
  LIMIT $2
)
SELECT jsonb_build_object(
    'fact_id', ranked.fact_id,
    'fact_kind', ranked.fact_kind,
    'scope_id', ranked.scope_id,
    'generation_id', ranked.generation_id,
    'source_system', ranked.source_system,
    'source_record_id', ranked.source_record_id,
    'observed_at', ranked.observed_at,
    'payload', ranked.payload,
    'linked_via_fact_id', ranked.link_fact_id
) AS payload
FROM (
  SELECT 1 AS kind_rank, records.* FROM records
  UNION ALL
  SELECT 2 AS kind_rank, transitions.* FROM transitions
) AS ranked
ORDER BY ranked.kind_rank, ranked.observed_at DESC, ranked.fact_id DESC
LIMIT $2
`, []any{strings.TrimSpace(repositoryID), limit + 1}
}

// issueFactColumns are the fact columns the issue read projects.
const issueFactColumns = `fact.fact_id,
           fact.fact_kind,
           fact.scope_id,
           fact.generation_id,
           fact.source_system,
           fact.source_record_id,
           fact.observed_at,
           fact.payload`

// LinkedIssuesSQL is the CTE the source-only statement appends to its WITH
// clause: the (scope, generation, issue id) of every issue with a live
// work_item.external_link that names a repository, on each scope's active
// generation. It starts with a comma, so it follows AdmissibleCorrelationsSQL. The
// link condition is the same one migration 152 spells, and the set holds no
// blank id, so the predicate built on it is two-valued.
func LinkedIssuesSQL() string {
	return `, ` + LinkedIssuesCTE + ` AS MATERIALIZED (
  SELECT DISTINCT link.scope_id, link.generation_id, link.issue_id
  FROM ingestion_scopes AS lscope
  JOIN scope_generations AS lgen
    ON lgen.scope_id = lscope.scope_id
   AND lgen.generation_id = lscope.active_generation_id
   AND lgen.status = 'active'
  CROSS JOIN LATERAL (
    SELECT fact.scope_id, fact.generation_id, ` + IssueKey("fact") + ` AS issue_id
    FROM fact_records AS fact
    WHERE fact.scope_id = lscope.scope_id
      AND fact.generation_id = lscope.active_generation_id
      AND fact.fact_kind = '` + WorkItemExternalLinkKind + `'
      AND fact.is_tombstone = FALSE
      AND fact.payload->>'linked_repository_id' IS NOT NULL
      AND fact.payload->>'linked_repository_id' <> ''
    OFFSET 0
  ) AS link
  WHERE COALESCE(link.issue_id, '') <> ''
)`
}

// LinkedIssuePredicate is true for a Jira record or transition whose issue has a
// live repository link in the same scope and active generation. It is the Jira
// half of "this support fact has a durable link to some target": a fact linked to
// another repository is neither this target's evidence nor source-only.
//
// It reads LinkedIssuesCTE, which LinkedIssuesSQL defines, and expects the
// source-only statement's scope alias (scope) to be the scope of the fact. The
// predicate is two-valued: the issue id is wrapped in COALESCE and the set holds
// no blank id, so the row-wise IN returns true or false and never UNKNOWN, and
// NOT of it cannot drop a fact from the count.
func LinkedIssuePredicate() string {
	return `(fact.fact_kind IN ('` + WorkItemRecordKind + `', '` + WorkItemTransitionKind + `')
      AND COALESCE(` + IssueKey("fact") + `, '') <> ''
      AND (scope.scope_id, scope.active_generation_id, ` + IssueKey("fact") + `) IN (SELECT li.scope_id, li.generation_id, li.issue_id FROM ` + LinkedIssuesCTE + ` AS li))`
}

// JiraFactLinked re-checks in Go what JiraIssueLinkSQL selected on: fact is a
// work_item.record or work_item.transition, it carries a non-blank
// provider_work_item_id, and the SQL stamped the external link it joined through.
// A row that fails any of these is never evidence, whatever reached it.
func JiraFactLinked(fact map[string]any) bool {
	switch fact["fact_kind"] {
	case WorkItemRecordKind, WorkItemTransitionKind:
	default:
		return false
	}
	payload, _ := fact["payload"].(map[string]any)
	return text(payload, workItemIssueKey) != "" && text(fact, "linked_via_fact_id") != ""
}

// IsJiraIssueFact reports whether kind is one of the two Jira kinds this package
// links to a repository through the same issue's link.
func IsJiraIssueFact(kind string) bool {
	return kind == WorkItemRecordKind || kind == WorkItemTransitionKind
}
