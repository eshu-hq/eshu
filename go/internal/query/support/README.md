# Story Target Support

## Purpose

Query text and pure helpers that link PagerDuty incident-routing facts to a
repository for the service and repository story `target_support` section
(#7463). A PagerDuty service fact carries no repository, so the link goes through
the reducer's incident-repository correlation for its provider service id.

## Ownership boundary

Owns the routing statement (`IncidentRoutingSQL`), the correlation set and
predicate the source-only count reads (`AdmissibleCorrelationsSQL`,
`LinkedIncidentRoutingPredicate`), the service-id key expressions
(`AppliedServiceKey`, `ObservedServiceKey`), the fact-kind constants, and the Go
re-check `RoutingFactCorrelatedTo`. Does not own statement execution, the
repository gate for a service target, the Jira link read, or the evidence shaping
(`link_basis`, counts, ambiguity); those stay in the query root's
`service_story_target_support*.go` files, which call into this package.

## Layout

- `routing.go` -- the constants, the key helpers, the three statement builders,
  and the Go re-check.
- `routing_test.go` -- the SQL shape, the two-valued predicate, and the Go
  re-check matrix.

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
- The source-only correlation set is `MATERIALIZED`, and the predicate is an
  uncorrelated `IN (SELECT ...)`. A per-row `EXISTS`, or candidates folded into
  the join, measured 1.3 to 1.7 s against 85 ms.
- The predicate is two-valued because it sits under `NOT`; keep the
  `COALESCE(key, '')` on the applied key.
- The index literals are bound to migration 003 by
  `TestServiceStoryIncidentRoutingIndexesMatchQuery` in the query root. Change
  the statement and the index definition together, in a new migration.

## Related docs

- [Story routes](../../../../docs/public/reference/http-api/story-routes.md)
- [Evidence note](../../../../docs/internal/evidence/7463-story-target-support-incident-routing.md)
