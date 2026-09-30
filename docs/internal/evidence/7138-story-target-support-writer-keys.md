# Story target support: link through the keys writers emit (#7138)

The story target-support read (`ContentReader.ServiceStoryTargetSupportEvidence`,
`go/internal/query/service_story_target_support.go`) admitted a support fact only
when its payload named the target in `candidate_refs`, `evidence_refs`, or
`linked_entities`. No writer of the twelve support kinds emits any of those keys:

```bash
rg -n 'candidate_refs|evidence_refs|linked_entities' go/ sdk/ specs/ -g '!*_test.go'
```

Every hit is a documentation or semantic writer, a documentation factschema, a
reader, or migration 122. The `work_item.external_link` and `incident_routing.*`
factschema documents declare `linked_repository_id`, `provider_work_item_id`,
`service_id`, `provider_object_id`, and `resource_class`, and none of the three.
The read returned zero rows for every real fact; its tests passed only because
they seeded the read's own assumed keys.

## What changed

- **Repository target:** the active, non-tombstoned `work_item.external_link` facts
  whose `payload->>'linked_repository_id'` equals the repository id. The Jira
  collector sets that key only for a confidently typed GitHub pull-request or
  GitLab merge-request link, and it is `repositoryidentity.CanonicalRepositoryID`
  of the link's repository remote, the id the git ingester stores.
  `TestLinkedRepositoryIDEqualsIngesterRepositoryID` proves the equality for SSH,
  HTTPS, `.git`, mixed-case, and GitLab-subgroup remotes.
- **Service target:** the same links reached through the service's repository,
  only when the graph shows the repository defining exactly that workload. One
  bounded read per story, `MATCH (r:Repository {id:$repo_id})-[:DEFINES]->(w:Workload)
  RETURN w.id AS id ORDER BY id LIMIT 3`, decides in Go: one id equal to the target
  links (`link_basis: repository_sole_workload`); two or more including the target
  are ambiguous (`support_correlation_ambiguous`, `evidence_count` 0); everything
  else, including no graph, a failed read, and an identity-only context, fails
  closed. The `reducer_workload_identity` gate first proposed was withdrawn: its
  `entity_keys` are `workload:<repo name>`, one per repository snapshot, so it
  degenerates to name equality and over-admits a monorepo's same-named workload.
- **Source-only:** an active support fact with no durable link, that is, anything
  except a `work_item.external_link` carrying a non-empty `linked_repository_id`.
  A link to another repository is neither evidence nor source-only.
- **Removed from the support read:** the three-key SQL containment, the Go
  re-check, the ambiguity checker, and the kind-alias expansion
  (`service/story_target_support_refs.go`). The documentation read keeps its own
  matching, untouched.
- **Migration 151:** partial expression index
  `fact_records_story_support_link_repo_idx` on
  `(scope_id, generation_id, (payload->>'linked_repository_id'))` where
  `fact_kind = 'work_item.external_link' AND is_tombstone = FALSE`. The statement
  uses the plain `fact.payload->>'linked_repository_id' = $1` form; a `NULLIF(...)`
  wrapper cannot use this expression.
- **Not in this change:** `incident_routing.*` linking (#7463), `work_item.record`
  and `transition` linking through the same-issue join (#7464), and attaching
  support to identity-only service shells. Until #7463 lands
  `incident_routing_count` is always 0.

## Correlation truth matrix

`TestServiceStoryTargetSupportWriterShapedMatrixLive` seeds payloads built by the
production writers (`jira.NewWorkItemExternalLinkEnvelope`, `NewWorkItemRecordEnvelope`,
`NewWorkItemTransitionEnvelope`, `NewWorkItemProjectMetadataEnvelope`,
`pagerduty.NewObservedPagerDutyServiceEnvelope`) and runs
`ContentReader.ServiceStoryTargetSupportEvidence` end to end:

| Case | Result |
| --- | --- |
| repository R, two live links to R | `evidence_count` 2, `link_basis` `linked_repository`, newest first |
| tombstoned link to R, link on a superseded generation | excluded from evidence and from source-only |
| repository R2, one link to R2 | evidence is that link only |
| repository R3 (nothing links to it) | `evidence_count` 0; source-only 5 (plain link, record, transition, project metadata, PagerDuty service); links to R and R2 not counted; reason `support_source_only_not_target_linked` |
| service target, graph says the repository defines exactly the target | 2 evidence rows, `link_basis` `repository_sole_workload`, `coverage.repository_workload_count` 1 |
| service target, repository defines several workloads including it | `evidence_count` 0, `ambiguous_count` 2, `support_correlation_ambiguous` |
| service target, graph unavailable / defines none / defines another workload | `evidence_count` 0, `ambiguous_count` 0 (fail closed) |

The handler-level cases with the graph double are
`TestServiceStoryTargetSupportGateReadsRepositoryDefines` (one bounded read per
story, `repo_id` bound, no aggregate; sole, several, sole-but-other, three
without the target, none, and a failing read) and
`TestServiceStoryTargetSupportGateSkipsGraphWithoutAWorkloadNode` (nil graph and
identity-only issue no graph read and fail closed).

## Proof

RED, on the parent commit with the tests and no fix (exit 1 at that commit):

```text
--- FAIL: TestServiceStoryTargetSupportMatchesWriterShapedFactsLive
    evidence_count = 0, want 1
--- FAIL: TestServiceStoryTargetSupportWriterShapedMatrixLive (5 of 8 subtests)
    coverage.source_only_count = 8, want 5      # the three linked rows counted as source-only
--- FAIL: TestBuildServiceStoryTargetSupportSQLBindsWriterLinkKey, ...SQLNeedsAKnownRepository,
          ...SourceOnlySQLUsesLinkPredicate, TestServiceStoryTargetSupportLinkIndexMatchesQuery,
          TestBuildStoryTargetSupport{Attaches,Drops,ServiceTargetGates}...,
          TestServiceStoryTargetSupportGate*, ...StageEvent*
```

GREEN commands; the live ones need
`ESHU_POSTGRES_DSN` (the matrix and semantics tests create their own schema) and
`ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN` plus
`ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1` (the plan test creates and
drops its own database):

```bash
cd go && ESHU_POSTGRES_DSN=... go test ./internal/query -run 'StoryTargetSupport|TargetSupport' -count=1
cd go && ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN=... ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1 \
  go test ./internal/query -run TestServiceStoryTargetSupportUsesSupportKindsIndexLive -count=1 -v
```

`TestServiceStoryTargetSupportUsesSupportKindsIndexLive` now seeds one Jira scope
with 50,000 active external links (about 10% keyed, 1% to the target) beside the
600,000-fact repository corpus, and asserts, in both `force_custom_plan` and
`force_generic_plan`, that the row read uses `fact_records_story_support_link_repo_idx`
with `linked_repository_id` as an **index condition**, and that the source-only
count uses `fact_records_story_support_kinds_idx`. Seeded violation: rewriting the
row read's predicate to `NULLIF(fact.payload->>'linked_repository_id','') = $1`
still "uses the index" by name (a Bitmap Index Scan on the scope/generation prefix,
9,275 buffer hits instead of 1,811) and only the index-condition assertion catches it:
`fact_records_story_support_link_repo_idx is scanned by its scope/generation prefix
only; the linked_repository_id predicate is a heap filter`.

## Performance evidence

Performance Evidence: two theories were measured before any code was written,
then the change was measured end to end.

**Postgres row read** (postgres:18.6, shared laptop under load average 15 to 36;
interleaved runs, alternating first mover, medians of 9 after a discarded warm-up,
`PREPARE` with `plan_cache_mode` forced to custom and generic; statements dumped
from the shipped builders). Corpus A reproduces the #7126 fixture (200 repository
scopes x 3 generations x 1,000 facts = 600,000, support kinds about 1%). Corpus B
adds one Jira scope with three generations, 50,000 active `work_item.external_link`
facts (10.9% carrying `linked_repository_id`, 1% the target's, target rows on the
superseded generations and 5 tombstoned), 20,000 records, 30,000 transitions.

| Statement | Corpus | Median exec | Buffers |
| --- | --- | ---: | ---: |
| current (three-key containment) | A | 11.4 ms | 8,839 |
| current | B | 73.9 ms | (all ~125k active support facts) |
| new, no index | B | 13.2 ms | 5,670 |
| new + migration 151 index | B, 50k links | 1.74 ms | 478 |
| new, no index / with index | 500k links | 206 ms / 17.4 ms | |

The no-index new read sits inside the #7126 band (14.4 ms before, 11.3 ms after on
600k facts). The heap filter dominates it (about 49.5k of 50k external-link rows
discarded per probe), so it is linear in active links per scope; the index removes
that and is why migration 151 ships. Index size 656 kB at 50k links, 3.9 MB at
500k; insert WAL for 50k external-link rows rose about 7% (noisy, an upper bound),
and 50k non-link rows rose 0%. Binding the single kind (13.2 ms) instead of the
twelve-kind cross join (14.6 ms) removes about eleven no-op probes per scope, so
the row read has no kind fan-out. The source-only aggregate replaces its ref
predicate with the link predicate: 41.4 ms to 31.5 ms on corpus B, 5.2 to 4.8 ms
on corpus A; it scans every active support fact either way and no index can serve a
`NOT` over the whole support set, the same class as before.

**Graph DEFINES read** (Neo4j 2026.09.0 community, `neo4j:2026-community`, 20,000
Repository, 40,000 Workload, 23,668 `DEFINES` written with the materializer's
`MERGE`, 299,502 File via `CONTAINS`, Repository.id and Workload.id unique
constraints). `PROFILE` is one shape for every repo: `NodeUniqueIndexSeek
Repository(id)` then a typed `Expand(All)` on `DEFINES`, `Filter(w:Workload)`,
`Top(id ASC LIMIT 3)`, no `AllNodesScan` and no label scan.

| Repo shape | db hits (min/med/max) | Client wall median |
| --- | --- | ---: |
| 1 workload | 10 / 14 / 19 | 2.56 ms |
| 3 workloads | 17 / 22 / 24 | 2.43 ms |
| 60 workloads (monorepo) | 185 | 2.70 ms |
| 60 workloads + 10k `CONTAINS` + 1k `DEPENDS_ON` | 185 | 2.64 ms |
| 0 workloads / unknown id | 9 to 17 / 1 | 1.82 / 2.13 ms |

`LIMIT 3` is not pushed below the expand: cost is about 3 db hits per `DEFINES`
edge, bounded by the repo's fan-out (605 hits, under 2 ms, at 200 workloads). One
statement per story, never per workload.

**Story stage, before and after, same fixture.** The
`service_query.stage_completed` `support_target_evidence` stage is the loader
call. The fixture is corpus A' (writer-shaped: 200 repository scopes with external
links on 1% of facts, half keyed to the target) plus the 50,000-link Jira scope,
Postgres 18 on the shared laptop at load average 25 to 50, Neo4j 2026 community
holding three repositories. Nine interleaved rounds after two warm-ups,
alternating first mover; the "before" run is the same harness on commit
`725fa19425` (no graph read, no migration 151), the "after" run on this branch.

| Scenario | Before (median) | After (median) | After evidence |
| --- | ---: | ---: | --- |
| service, repository defines exactly the target | 54.4 ms | 5.8 ms | 10 (row cap) |
| service, repository defines two workloads | 52.2 ms | 4.4 ms | 0 (rows reported ambiguous) |
| service, repository defines the target, no links | 50.0 ms | 15.6 ms | 0, source-only |
| repository target | 51.0 ms | 3.0 ms | 10 (row cap) |

The "before" evidence count is 0 in every scenario: the read matched nothing. The
graph read through the same Bolt reader measured 1.9 ms median over 18 runs, which
is included in the service rows. The added read is under a tenth of the
before-cost of every scenario and the stage is faster even with it, because the old
statement evaluated twelve containment predicates over the Jira scope's facts. The
no-links row is the source-only aggregate (about 14 ms here), which still runs when
the row read is empty, exactly as before.

Proof limits: single client, warm cache, laptop under load (ratios and buffer
counts are the reliable signal, absolute milliseconds are inflated against an idle
host), synthetic distributions; the production active `work_item.external_link`
cardinality is unmeasured, and the index is what keeps the read flat if a Jira
scope holds several hundred thousand links. Neo4j only; NornicDB is parked. The
graph read was not measured under concurrent write load.

## Observability

Observability Evidence: no new metric, span, or queue. The service story's
`service_query.stage_completed` event for `support_target_evidence` now also
carries `target_support_ambiguous_count`, `target_support_missing_reason` (the
first `missing_evidence` reason), `repository_workload_count`,
`repository_defines_target`, and, when the graph read failed,
`repository_defines_error`; the repository story's
`repository_query.stage_completed` event for `target_support` carries
`target_support_ambiguous_count` and `target_support_missing_reason`. An operator
answers "why does this story show no support" from the event alone: a zero
`repository_workload_count` with `repository_defines_error` set is an unavailable
graph, a count of two or more with `support_correlation_ambiguous` is a monorepo,
and `support_source_only_not_target_linked` is Jira facts with no repository link.
Asserted by `TestServiceStoryTargetSupportStageEvent*` and
`TestRepositoryStoryTargetSupportStageEventExplainsAnEmptyBlock`. The Postgres read
keeps the `postgres.query` span with `db.operation=list_service_story_target_support`;
the DEFINES read runs through the existing bounded graph reader and its
`query.graph_read` telemetry.

## Rejected alternatives

- Keeping the three-key matching "just in case": no writer emits it and only
  hand-seeded facts satisfy it.
- A catalog-correlation gate for services: provider-declared optional fields fail
  closed for every uncatalogued repository.
- Attaching repository-linked support to a multi-workload repository without a
  label: the over-admission the correlation-truth rules refuse.
- Name equality between the service and the repository as the gate: over-admits a
  monorepo's same-named workload.
