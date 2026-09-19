# 6794 Repository Context Incoming-Anchor Reads

Issue #6794 lists `/repositories/{id}/context` among the slow read routes. The
context handler's stage timers showed the time was not in Postgres. It was in
the graph reads that walk edges into the bound repository. On a
production-scale instance those reads ran past the 10s API deadline, and the
route rendered no incoming relationships or consumers at all.

This note records the diagnosis, the rewrite, and how both were measured.

Source binding: "before" is the Cypher at `2910ceecf`, which is also `925e8016f`
(this branch's merge base) for every file this note touches -- the two commits
are identical on that file set, so either serves as "before".

## Diagnosis

Three reads start from a `Repository` other than the bound one and end on the
bound one, `(x:Repository)-[rel:TYPES]->(r:Repository {id: $repo_id})`:

- `queryRepoRelationshipOverview`, incoming half
  (`go/internal/query/repository/context_helpers.go`);
- `queryRepoConsumers` (same file);
- `queryRepoDeployableUnitRelationshipOverview`, incoming half
  (`go/internal/query/repository/deployable_unit_relationships.go`).

The outgoing halves of the same reads start from the bound repository and are
fast. The mirrored pattern, `(r:Repository {id: $repo_id})<-[rel:TYPES]-(x:Repository)`,
names the same edges.

`QueryRelatedRepositoryArtifactSources`
(`go/internal/query/repositoryartifacts/repository_config_artifacts_loader.go`)
has the same right-anchored second branch and a real accuracy bug: it is one
bare top-level `UNION` of an outgoing and an incoming branch, and on this
backend a bare `UNION` does not return the rows of its second branch (see
[NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md)).
Observed on local `ghcr.io/eshu-hq/nornicdb-amd64-cpu:v1.3.3` with a seeded
hub that has one outgoing and three incoming related repositories: the bare
`UNION` returned 1 related repository (the outgoing one), and two separate
anchored reads merged in Go returned all 4. The measurement came from a
live test run against the split implementation, which is not part of this PR;
that implementation and the observation are recorded on #6812.

That read is **not** changed in this PR -- turning it into two anchored reads
also turns on unmeasured per-source fan-out, which needs its own performance
proof. The defect and its fix are tracked in #6812.

Two more reads have the same right-anchored, arrow-head-into-the-bound-repository
shape and are also out of scope here, tracked in #6811:

- `QueryRepoDeploymentEvidence`'s incoming branch
  (`go/internal/query/repository/deployment_evidence.go`)
  (`(artifact:EvidenceArtifact)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository {id: $repo_id})`);
- `FetchFluxDeploymentSourceTargetBindings`'s expansion read
  (`go/internal/query/impacttrace/impact_trace_deployment_flux_bindings.go`)
  (`(artifact)-[targetRel:EVIDENCES_REPOSITORY_RELATIONSHIP]->(targetRepo:Repository {id: $repo_id})`).

## Theory proof

Performance Evidence: a throwaway Go probe ran each pre-change statement and
its mirrored form three times against one seeded local graph. Backend: local
container `ghcr.io/eshu-hq/nornicdb-amd64-cpu:v1.3.3`, run under amd64
emulation on an arm64 host, so absolute times are inflated and only the
same-run ratio is meaningful. It is not the chart pin
(`nornicdb-cpu-bge:v1.3.3@sha256:81cedbf4…`); the pinned image could not be
pulled on the proof host. Schema: `repository_id` uniqueness constraint and a
`Repository(id)` property index. Seed: 302 `Repository` nodes, 15,000 `File`
nodes under `REPO_CONTAINS`, 600 background `DEPENDS_ON` edges, a hub
repository with 74 incoming typed edges and 5 outgoing, and a leaf with none.

| Read | Repo | Rows | Right-anchored | Left-anchored |
| --- | --- | --- | --- | --- |
| deployable-unit incoming | hub | 4 | 115–129 ms | 1 ms |
| overview incoming | hub | 74 | 126–162 ms | 2–3 ms |
| consumers | hub | 74 | 118–126 ms | 2 ms |
| overview incoming | leaf | 0 | 119–127 ms | 1 ms |
| consumers | leaf | 0 | 114–180 ms | 1 ms |
| overview incoming | mid-graph repo | 2 | 124–225 ms | 1–2 ms |
| overview outgoing (control) | hub | 5 | 1 ms | 1 ms |

Every pair returned identical rows in identical order. The right-anchored cost
does not track the row count: the leaf, with no edges, costs the same as the
hub. An earlier run of the same probe on a larger seed (1,002 repositories,
100,000 `File` nodes) measured about 1.4s for the right-anchored overview read
against 2ms mirrored, which suggests the cost grows with graph size rather
than with the bound repository's degree. The engine source was not read; that
inference comes from timing only.

## Contract proof

- `TestRepositoryContextIncomingReadsAnchorOnTheBoundRepository` and
  `TestRepositoryContextIncomingReadsKeepTheirRowOrder`
  (`go/internal/query/repository`) record every statement the three context
  helpers issue. They fail on `2910ceecf` (a right-anchored incoming read) and
  pass after the rewrite. The second test pins each read's pre-change
  `ORDER BY`.
- `TestLiveNornicDBRepositoryIncomingAnchor`
  (`-tags live_nornicdb_answer_truth`, `go/internal/query/repository`) seeds a
  hub and a leaf with known edges, runs the production functions and the
  pre-change statements against the same container, and checks both against
  rows known by construction. The three context reads match the pre-change
  rows exactly.

## Live A/B (production-scale instance)

Before (`925e8016f`) vs. a build of the three anchor rewrites plus the
related-source `UNION` split recorded on #6812, 3 repositories exercised across
7 alternating rounds, all HTTP 200s. The split was later removed from this PR.
21/21 responses matched after stripping volatile keys (`generated_at`,
`as_of`, `observed_at`, durations, `request_id`, `freshness`, `truth`).

Attributed to this change -- the per-stage timings of the rewritten reads,
which the related-source split does not touch:

- `consumers` stage: p50 10.002s (at the 10s graph-read deadline) -> under
  0.05s;
- `relationship_overview` stage: 6.752s -> 0.064s.

Not attributable to this change alone -- whole-request timings. The measured
build also carried the related-source split, which runs inside the
`content_infrastructure_overview` stage of the same request, so these totals
are shown for context and are non-comparable as a measurement of this commit:

- graph-fallback repository: p50/p95 17.18s/21.30s -> 1.12s/1.31s;
- read-model repository with a few hundred incoming edges: p50/p95
  1.27s/12.09s -> 1.34s/1.83s (the 12s is main's first call, outside every
  timed stage);
- small repository: p50/p95 0.91s/11.88s -> 0.98s/1.57s.

## Observability

No-Observability-Change: the rewrite changes only Cypher text inside existing
reads. Each read is still timed by `eshu_dp_neo4j_query_duration_seconds`
(`operation="read"`), and the context handler's
`repository_query.stage_completed` log carries `stage=relationship_overview`
or `stage=consumers` with `duration_seconds` and `row_count`. A regression
shows as those stage durations approaching the API deadline with
`row_count=0`.
