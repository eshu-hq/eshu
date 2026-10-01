# 7463: story target_support links PagerDuty incident-routing facts

Issue #7463, follow-up to #7138 (the writer-key link for Jira) and #7480 (the
graph DEFINES gate). Before this change `incident_routing_count` was always `0`:
the support read matched a Jira key only, and no PagerDuty fact carries a
repository.

## What links, and what does not

A PagerDuty applied service (`incident_routing.applied_pagerduty_resource`,
`resource_class = 'service'`, keyed on `provider_object_id`) or observed service
(`incident_routing.observed_pagerduty_service`, keyed on
`COALESCE(NULLIF(provider_object_id, ''), service_id)`) links to a repository
through `reducer_incident_repository_correlation`: `outcome IN ('exact',
'derived')`, `provenance_only = false`, provider `pagerduty`, a non-blank
`provider_service_id`, on an active generation, with `repository_id` equal to the
target. This mirrors `storage/postgres/service_incident_evidence_loader.go`.

Not linked, and so source-only: `incident_routing.coverage_warning` (no anchor),
an applied resource of any class but `service`, and a service whose id has no
admissible correlation. A service correlated to another repository is evidence
for that repository only, and is neither this target's evidence nor source-only.
A service target applies the same DEFINES gate as the Jira links: evidence only
when the graph shows its repository defining exactly the target, ambiguous when
it defines several, closed otherwise.

The source-only count changes with it. "No durable link to any target" now
includes the correlated PagerDuty service facts, so a service linked to another
repository stops counting as source-only, the way a Jira link to another
repository already did.

## Where the code lives

`go/internal/query` is dirgate-pinned at 252 non-test files, so the pure parts
moved to a new leaf, `go/internal/query/support`: the routing statement, the
correlation set and predicate for the source-only count, and the Go re-check of
a returned row. The root keeps only the glue (`ContentReader` methods and the
evidence shaping), in files that already existed.

## Measurements

PostgreSQL 18.6, one disposable database, about 1,010,000 `fact_records` rows:
the #7138 corpus (200 repository scopes, three generations each, plus one Jira
scope of 50,000 links per generation) and a PagerDuty corpus (40 scopes of 2,000
applied resources or observed services over three generations, 20,000
correlation facts over 5,000 repositories, and `repo-x` correlated to two
provider services). `EXPLAIN (ANALYZE, BUFFERS)` through a prepared statement,
`plan_cache_mode` forced custom and generic.

| Statement and shape | Custom | Generic | Buffers |
|---|---|---|---|
| Routing read, plain join to the active scope and generation (rejected) | 43 ms | 39 ms | 61,163 hit |
| Routing read, repository-first with fenced LATERAL probes (shipped), `repo-x` | 3.22 ms | 1.01 ms | 2,070 hit |
| Same, a repository with no correlation | 0.11 ms | 0.10 ms | 3 hit |
| Source-only count before this change | 41 to 71 ms | | about 96,000 |
| Source-only with a per-row `EXISTS` on the correlation set (rejected) | 1,580 ms | 1,461 ms | 381,975 hit |
| Source-only with `IN (SELECT ...)`, candidates not materialized (rejected) | 1,346 ms | 1,676 ms | 381,375 hit |
| Source-only with `IN (SELECT ...)`, candidates `MATERIALIZED` (shipped) | 81 to 85 ms | | 165,605 hit |

Why the plain join lost: Postgres drove from the 242 active scope generations and
read every observed service of each through the generic kinds index (40,006 rows
filtered in a join), and re-read the correlation facts once per generation. The
shipped shape probes the correlation index by `repository_id`, then each
correlated provider service id through the applied and observed service indexes
by LATERAL, and fences each probe with `OFFSET 0`. Source-only lost the same way:
the correlation subquery was nested-looped against the 242 active scopes and
discarded 4.1 million rows in a join filter; materializing the candidates once
removed it. The routing read returned the expected 8 rows (the applied and
observed facts of the two correlated services, two of each).

The source-only count costs about 14 to 40 ms more at 17,145 admissible
correlations. It runs only when the story found no evidence. The added cost is
set by the number of admissible correlations; it was measured at that one count.

Worst case, measured by the reviewer on a smaller database (340,000 rows,
generic plan): a repository correlated to 300 provider services, with 20
retained correlation generations, 50 retained observed generations and five by
20 retained applied generations, took 29.7 ms and 30,161 buffers, against 235
buffers for a two-service repository on the same database. The routing read
scales with correlated services times retained generations, because each fenced
probe fetches every generation of a service before the active-generation join,
and the source-only correlation set scans every non-tombstoned admissible
correlation fact of every generation. Both stay within a story read's budget at
the measured shapes; they would need an active-generation-first probe or an
index that carries the generation if retention grows well past 50 generations.

No new migration: the three probes use the partial indexes migration 003
already carries (`fact_records_incident_repository_correlation_service_idx`,
`fact_records_incident_routing_applied_service_idx`,
`fact_records_incident_routing_observed_service_idx`).
`TestServiceStoryIncidentRoutingIndexesMatchQuery` binds the statement's literals
to those definitions so a drifted predicate fails there.

## Proof

Performance Evidence: `TestServiceStoryIncidentRoutingUsesLookupIndexesLive`
(set `ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN` to an administrative
`postgres`-database DSN and `ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1`)
seeds the corpus above and asserts, in custom and generic plans, that the
routing read uses the three indexes with the key as an index condition, stays
under 10,000 shared buffers, probes only the correlation index for an
uncorrelated repository, and that the source-only count still uses migration
123's index under 250,000 buffers.

Correlation truth: `TestServiceStoryTargetSupportPagerDutyRoutingMatrixLive`
(set `ESHU_POSTGRES_DSN`) seeds every fact from a production writer, the
PagerDuty collector for observed services, `terraformstate.Parse` for applied
resources, and the reducer's `PostgresIncidentRepositoryCorrelationWriter` for
the correlations, and reads through the shipped `ContentReader`. It covers the
positive case (exact and derived correlation, observed and applied), and the
negative and ambiguous cases: a team-class applied resource sharing the id, a
realistic ambiguous correlation with no repository, a hostile provenance-only
one that names the repository, another provider reusing the id, a correlation
on a superseded generation, an uncorrelated service, a tombstoned service, a
service on a superseded generation, the coverage warning, a service linked to a
second repository, the service-target gate (one workload, several, none, graph
unavailable) and the row bound.

```bash
cd go && go test ./internal/query/support ./internal/query -run 'Support|Routing|StoryTargetSupport' -count=1
cd go && ESHU_POSTGRES_DSN=postgres://... go test ./internal/query -run 'TestServiceStoryTargetSupport(PagerDutyRoutingMatrix|WriterShapedMatrix)Live' -count=1
cd go && ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN=postgres://.../postgres ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1 go test ./internal/query -run TestServiceStoryIncidentRoutingUsesLookupIndexesLive -count=1 -v
```

Observability Evidence: the Postgres read keeps the existing `postgres.query`
span, operation `list_service_story_target_support`, now covering up to two
bounded statements. The repository story `target_support` stage event and the
service story `support_target_evidence` stage event gain
`target_support_incident_routing_count`, so an operator can see whether a story
carries routing evidence without reading the payload. No collector, reducer
queue, graph write, metric instrument, runtime flag or deployment setting
changes.
