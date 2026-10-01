# Story Target Support -- Agent Instructions

Scope: `go/internal/query/support/` (package `support`).

## Ownership

This leaf owns the PagerDuty routing link (#7463) and the Jira same-issue link
(#7464) for the story `target_support` section: `routing.go` and
`jira_issue_link.go` (statement builders, key helpers, the Go re-checks). The
query root owns execution (`ContentReader.queryServiceStoryTargetSupportFacts`),
the repository gate, and the evidence shaping, in
`go/internal/query/service_story_target_support*.go`.

## Invariants

- MUST NOT import the query root; the root imports this package.
- MUST NOT run SQL or hold a database handle. It returns text and decides on rows
  already read.
- A PagerDuty fact attaches to a repository only through an admissible
  correlation: `exact` or `derived`, `provenance_only = false`, provider
  `pagerduty`, a non-blank `provider_service_id` and a non-blank, already
  trimmed `repository_id`, on an active generation (a repository-less or
  whitespace-padded correlation links nothing and must not hide its service from
  the source-only count). Never
  match on a name, title, summary or any text, and never trim an id before
  comparing it: the SQL joins on exact equality and the Go re-check agrees.
- Keep the probes fenced (`OFFSET 0`) and the key expressions identical to
  migration 003's index definitions. Changing either without the other turns an
  index condition into a heap filter; the plan proof in the query root fails on
  that.
- The source-only predicate is read under `NOT`; keep it two-valued
  (`COALESCE(key, '') IN (SELECT ...)`, never `NOT IN`, never a bare key).
- A Jira record or transition attaches only through a live
  `work_item.external_link` of the same issue: same `scope_id`, same active
  generation, a non-blank `provider_work_item_id` on both sides, and a link whose
  `linked_repository_id` is the repository. Never join on `work_item_key`, across
  scopes or generations, or on a project-to-repository guess. A derived row is
  evidence only with its `linked_via_fact_id` witness and a non-blank issue id.
- The Jira statement keeps its three `OFFSET 0` fences, its `MATERIALIZED`
  expressions and the one-time gate on the transitions, and the key and kind
  literals identical to migration 155. The linked-issue predicate is read under
  `NOT`; keep it two-valued.
- The set of linkable kinds is the applied PagerDuty service and the observed
  PagerDuty service. `incident_routing.coverage_warning` and every other applied
  resource class stay source-only.

## Change guidance

Adding a linkable kind needs its own measured statement, a writer-shaped
positive, negative and ambiguous case in
`service_story_target_support_pagerduty_matrix_live_test.go`, and the story
routes doc updated in the same change.

## Verification

```bash
cd go && go test ./internal/query/support ./internal/query -run 'Support|Routing|StoryTargetSupport' -count=1
```
