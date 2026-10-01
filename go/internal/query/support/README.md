# Story Target Support

## Purpose

Query text and pure helpers that link PagerDuty incident-routing facts (#7463)
and Jira records and transitions (#7464) to a repository for the service and
repository story `target_support` section. A PagerDuty service fact carries no
repository, so the link goes through the reducer's incident-repository
correlation for its provider service id. A Jira record or transition carries
none either, so it goes through the `work_item.external_link` of the same issue.

## Ownership boundary

Owns the routing statement (`IncidentRoutingSQL`), the correlation set and
predicate the source-only count reads (`AdmissibleCorrelationsSQL`,
`LinkedIncidentRoutingPredicate`), the service-id key expressions
(`AppliedServiceKey`, `ObservedServiceKey`), the fact-kind constants, and the Go
re-check `RoutingFactCorrelatedTo`, and the Jira issue-link statement
(`JiraIssueLinkSQL`), its linked-issue set and predicate (`LinkedIssuesSQL`,
`LinkedIssuePredicate`), and the re-check `JiraFactLinked`. Does not own
statement execution, the repository gate for a service target, the
`work_item.external_link` read itself, or the evidence shaping (`link_basis`,
counts, ambiguity); those stay in the query root's
`service_story_target_support*.go` files, which call into this package.

## Layout

- `routing.go` -- the constants, the key helpers, the three statement builders,
  and the Go re-check.
- `routing_test.go` -- the SQL shape, the two-valued predicate, and the Go
  re-check matrix.
- `jira_issue_link.go` -- the Jira kinds, the issue key, the issue-link
  statement, the linked-issue set and predicate, and the Go re-check.
- `jira_issue_link_test.go` -- the statement shape, the fences and the gate, the
  two-valued predicate, and the re-check matrix.

## Telemetry

None. The package builds text and decides on already-read rows. The read runs
under the root's `postgres.query` span, operation
`list_service_story_target_support`.

## Dependencies

Standard library only. It must not import the query root: the root imports this
package.

## Operational notes

- The routing statement is repository-first. Written as a plain join of
  `fact_records` to the active scope and generation it read 60,000 buffers at one
  million facts (30 to 64 ms); the shipped shape reads about 2,000 (1 to 3 ms).
  Keep each `OFFSET 0` and the LATERAL probes; see
  `docs/internal/evidence/7463-story-target-support-incident-routing.md`.
- The source-only correlation set is `MATERIALIZED`, is read per active
  scope generation through `fact_records_scope_generation_idx` (never every
  retained generation of the kind: 932,000 buffers at 24 retained generations
  against 115,000), and the predicate is an uncorrelated `IN (SELECT ...)`. A per-row `EXISTS`, or candidates folded into
  the join, measured 1.3 to 1.7 s against 85 ms.
- The predicate is two-valued because it sits under `NOT`; keep the
  `COALESCE(key, '')` on the applied key.
- The index literals are bound to migration 003 by
  `TestServiceStoryIncidentRoutingIndexesMatchQuery` in the query root, and the
  issue-link literals to migration 155 by
  `TestServiceStoryTargetSupportIssueIndexMatchesQuery`. Change the statement and
  the index definition together, in a new migration.
- The issue-link read probes each linked issue's records through migration 155
  (kind and issue id are index columns) and reads transitions only while the
  records left the bound unfilled; without the index the second hop read 344,516
  buffers at 50,000 links, and with the kind left out of the key 525,609 buffers
  at 100 transitions per issue. The join key is the issue id and never
  `work_item_key`.

## Related docs

- [Story routes](../../../../docs/public/reference/http-api/story-routes.md)
- [Evidence note](../../../../docs/internal/evidence/7463-story-target-support-incident-routing.md)
- [Jira issue-link evidence note](../../../../docs/internal/evidence/7464-story-target-support-jira-issue-link.md)
