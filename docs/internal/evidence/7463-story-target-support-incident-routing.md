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
target. A correlation with a blank `repository_id`, or one stored with surrounding
whitespace of any kind `strings.TrimSpace` trims (the check uses the same set), is
not admissible for the source-only count either: no repository's
story can ever read it (story targets are trimmed before the exact match), so
counting its service as linked would hide it from both sides. This mirrors `storage/postgres/service_incident_evidence_loader.go`.

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
provider services), plus, for the retention run, 21 more superseded generations
of every PagerDuty scope that each carry the 20,000 correlations again (24
retained generations, the default retention policy). `EXPLAIN (ANALYZE,
BUFFERS)` through a prepared statement, `plan_cache_mode` forced custom and
generic.

| Statement and shape | Custom | Generic | Buffers |
|---|---|---|---|
| Routing read, plain join to the active scope and generation (rejected) | 43 ms | 39 ms | 61,163 hit |
| Routing read, repository-first with fenced LATERAL probes (shipped), `repo-x`, shim | 3.22 ms | 1.01 ms | 2,070 hit |
| Same, on the committed plan proof's corpus, 24 retained generations | 0.37 ms | 0.24 ms | 85 hit |
| Same, a repository with no correlation | 0.11 ms | 0.10 ms | 3 hit |
| Source-only count before this change | 41 to 71 ms | | about 96,000 |
| Source-only with a per-row `EXISTS` on the correlation set (rejected) | 1,580 ms | 1,461 ms | 381,975 hit |
| Source-only with `IN (SELECT ...)`, candidates not materialized (rejected) | 1,346 ms | 1,676 ms | 381,375 hit |
| Source-only with `IN (SELECT ...)`, candidates `MATERIALIZED`, read by kind across every generation (superseded by the next row), 3 generations | 81 to 85 ms | | 165,605 hit |
| Same shape, 24 retained generations (plan proof) | | | 931,766 hit |
| Source-only with `IN (SELECT ...)`, candidates `MATERIALIZED` and read per active generation (shipped), 24 retained generations (plan proof) | 89 to 226 ms | 76 to 100 ms | 115,042 hit |
| Previous vs shipped shape, 24 retained generations, interleaved on a second database at load 14 to 20 | 940 to 1,190 ms vs 203 to 208 ms | | about 965,000 vs 100,000 |

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

Retention changed the source-only shape. The first shipped shape read every
non-tombstoned correlation fact of the kind and joined to the active generation
afterwards, so its cost grew with the retained generations: 931,766 shared
buffers at 24 retained generations against 165,605 at three, a reviewer's
finding the original three-generation proof could not show. The shipped shape
walks the active scope generations and probes each by `(scope_id, generation_id,
fact_kind)` through `fact_records_scope_generation_idx`, so only active-generation
correlations are read and the cost no longer depends on retention. Both shapes
returned the same count (59,763) on the second database. The figure for the
routing read on this corpus is 85 buffers; the 2,070 above is from the earlier
shim, which I could not reproduce on the committed proof's corpus, so treat it
as a shim figure.

The source-only count costs about 14 to 40 ms more at 17,145 admissible
active correlations. It runs only when the story found no evidence. The added
cost is set by the number of active admissible correlations; it was measured at
that one count.

Worst case, measured by the reviewer on a smaller database (340,000 rows,
generic plan): a repository correlated to 300 provider services, with 20
retained correlation generations, 50 retained observed generations and five by
20 retained applied generations, took 29.7 ms and 30,161 buffers, against 235
buffers for a two-service repository on the same database. The routing read
scales with correlated services times retained generations, because each fenced
probe fetches every generation of a service before the active-generation join.
It stays within a story read's budget at the measured shapes; it would need an
active-generation-first probe or an index that carries the generation if
retention grows well past 50 generations. The source-only correlation set no
longer scales with retention (see above).

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
123's index under 200,000 buffers with 24 retained generations of correlations.

Correlation truth: `TestServiceStoryTargetSupportPagerDutyRoutingMatrixLive`
(set `ESHU_POSTGRES_DSN`) seeds every fact from a production writer, the
PagerDuty collector for observed services, `terraformstate.Parse` for applied
resources, and the reducer's `PostgresIncidentRepositoryCorrelationWriter` for
the correlations, and reads through the shipped `ContentReader`. It covers the
positive case (exact and derived correlation, observed and applied), and the
negative and ambiguous cases: a team-class applied resource sharing the id, a
realistic ambiguous correlation with no repository, a hostile ambiguous
provenance-only one that names the repository, an exact provenance-only
correlation, an unresolved non-provenance-only one and an exact
non-provenance-only one with no repository (each rejected by one filter alone;
the last one stays source-only), another provider reusing the id, a correlation on a superseded
generation, an uncorrelated service, a tombstoned service, a service on a
superseded generation, the coverage warning, a service linked to a second
repository, the service-target gate (one workload, several, none, graph
unavailable, target not among the defined) and the row bound, with the Jira link
seeded older than the first PagerDuty row so the newest-first merge is exercised.

```bash
cd go && go test ./internal/query/support ./internal/query -run 'Support|Routing|StoryTargetSupport' -count=1
cd go && ESHU_POSTGRES_DSN=postgres://... go test ./internal/query -run 'TestServiceStoryTargetSupport(PagerDutyRoutingMatrix|WriterShapedMatrix)Live' -count=1
cd go && ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN=postgres://.../postgres ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1 go test ./internal/query -run TestServiceStoryIncidentRoutingUsesLookupIndexesLive -count=1 -v
```

Observability Evidence: the Postgres read keeps the existing `postgres.query`
span, operation `list_service_story_target_support`, now covering up to three
bounded statements on one read-only snapshot. The repository story `target_support` stage event and the
service story `support_target_evidence` stage event gain
`target_support_incident_routing_count`, so an operator can see whether a story
carries routing evidence without reading the payload. No collector, reducer
queue, graph write, metric instrument, runtime flag or deployment setting
changes.

## Read consistency

The link read, the routing read and the source-only summary each filter on the
active generation, so they run on one read-only repeatable-read snapshot
(`db.ReadStore.BeginReadOnlySnapshot`, the seam `ContentReader` already uses for
the hardcoded-secret read): a generation activated between them cannot put rows of
two generations, or a summary of a third, in one section. Main made two separate
autocommit reads (rows, then the summary when empty); this change would have made
it three. Cost: one `BEGIN` and one `COMMIT`/`ROLLBACK` round trip per story read
that reaches Postgres, none for a closed gate (no statement, no snapshot).
`TestServiceStoryTargetSupportReadsOnOneSnapshot` fails if any read bypasses the
snapshot, and `TestServiceStoryTargetSupportSurfacesASnapshotBeginError` and
`TestServiceStoryTargetSupportClosedGateOpensNoSnapshot` pin the error and the
closed-gate behaviour.

Observability Evidence: the snapshot adds no operator signal beyond the stage
event field described above; a failed snapshot begin returns
`begin service story target support snapshot: ...` on the existing
`postgres.query` span.
