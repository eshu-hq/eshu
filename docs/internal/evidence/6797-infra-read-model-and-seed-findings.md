# #6797 Gate Seed Corrections And The Infra Read Model Findings

Evidence for what the read-API latency gate learned once its seed actually
represented graph scale and its run order matched a real deploy.

## Identity Of The Measurements

- Graph backend: `timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f`.
- The #6793 candidate is commit `545c27e9927d2f27ddc0e0beba537a79dcb20d20`.
  Its eshu-api binary has sha256
  `10caee9bb998aad17c1b2340b31a17f26b6c79ec86708ede393e5fd95412db06`, confirmed
  by `lsof -p <pid>` against the hashed file on every run. The `db-migrate`
  image (migration 109 and the graph schema) was built by Docker Compose from
  the same tree, so the API and the schema are the same source.
- Gate code: the branch head of `ci/6797-read-api-latency-gate` at each run
  (recorded in its commit messages); the seed is 800 scopes, 150,000 nodes per
  infra label, 150,000 IaC facts, 20 counted requests after 2 warmups.
- Host: developer Docker-on-macOS, not the CI runner class.
  `absolute_target_applicable: false`. The Postgres work counters
  (calls, rows, buffers) do not depend on CPU speed; latency figures do and are
  not comparable to CI.
- Single execution: every latency in the two "measured finding" tables below is
  ONE execution on an idle NornicDB (CPU checked below 15% between queries),
  not a p50/p95. They establish which variant is orders of magnitude apart, not
  a budget.

## Seed defects the gate had (fixed)

| Defect | How it showed | Fix |
| --- | --- | --- |
| `UNWIND range(0, $count - 1) ... CREATE` created ONE node per label | `K8sResource` count = 1 on a live stack | bound computed in Go, 10,000 per batch; `VerifyGraphNodeCounts` |
| `CASE` inside a `CREATE` property map stored as literal text | `count(DISTINCT n.provider)` = 150,000; count route returned a key per node | values computed in Go, `UNWIND $rows`; `VerifyGraphDimensions` |
| `content_entities` never written | #6793's read model derives from it, so it would be empty: a vacuous pass | `SeedInfraContentEntities`, 1.05M rows over 201 repositories, parity-tested against the graph |
| eshu-api started before the seed | its startup backfill saw an empty table and marked itself complete | seed, then start eshu-api, then sweep |

The Cypher probes are recorded in
`docs/public/reference/nornicdb-write-shape-pitfalls.md`.

## The #6793 fence and backfill (no defect found)

Seeding `content_entities` from a plain connection (no derive-aware session)
marked every repository in `infra_resource_entity_dirty_repos` at once, as
designed. With the backfill marker already present and the marks dirty,
eshu-api ran no backfill and served `/infra/resources/count` from the graph (504
at the 10s deadline). With the marker absent the backfill logged `started`
(repository_count 2, then 201 in the full corpus) and `completed`
(rows_inserted 40,000 in 0.44s; 1,050,000 in 11.5s), the dirty table drained to
0, and the routes answered 200 with `truth.basis` `hybrid` or `content_index`
(the envelope is only returned for `Accept: application/eshu.envelope+json`).
The gate asserts that basis before sweeping.

## Why the owner-ledger backfill marker is seeded

eshu-api's startup runs a one-time CloudResource owner-ledger backfill over the
graph. Two separate attempts failed:

1. The seeded nodes lacked `uid`, `resource_type` and `source_fact_id`. The
   backfill rejected them correctly; the seed was wrong and now carries them.
2. With valid nodes (150,000, `uid` unique) startup was fatal: `graph query
   exceeded its deadline` on the backfill's first page. That is not a wait-longer
   problem.

The seed records the backfill's completion marker
(`graph_node_owner_backfill_state`, key `cloud_resource_owner:v1`) because a
long-lived install has already run this upgrade migration, and the gate measures
steady-state reads. That model is valid, but it hides the finding below.

## Measured finding: the owner-ledger first page is an upgrade hazard (#6842)

`MATCH (n:CloudResource) RETURN <35 props> ORDER BY n.uid LIMIT 500`
(`go/internal/query/cloud_resource_owner_backfill.go`), 150,000 nodes, read-only:

| Variant | Time (one execution) |
| --- | ---: |
| as-is (`ORDER BY n.uid LIMIT 500`, no predicate) | 17.9s |
| same, `uid`-only projection | 25.2s |
| same, `LIMIT 5000` | did not finish in 180s (abandoned) |
| no `ORDER BY`, `LIMIT 500` | 0.04s |
| `ORDER BY n.uid`, no `LIMIT` (all 150,000 rows) | 6.5s |
| next-page shape `WHERE n.uid > $after ORDER BY n.uid LIMIT 500` | 0.39s |
| first page plus `WHERE n.uid > ''` | 0.11s |
| first page plus `WHERE n.uid IS NOT NULL` | 0.08s |
| `uid` seek | 0.0s |

`ORDER BY` with `LIMIT` and no predicate on the ordered property is slower than
sorting every row and grows with the `LIMIT`; a range or not-null predicate on
`uid` uses the index and is about two orders of magnitude faster. The next-page
query already carries such a predicate; the first page does not. On an install
with about 150,000 CloudResource nodes and no marker, API startup is fatal.
Candidate fix: give the first page the same `uid` predicate. The 5,000-row
variant was abandoned client-side and kept running on the server, so it is a
lower bound, not a measurement.

## Measured finding: a cold first hit on the count route's graph pass (#6843)

Even with the read model ready, `/infra/resources/count` still reads the two
graph-only labels (CloudResource and TerraformStateResource, 150,000 nodes each)
from the graph. `infraGraphOnlyCountCypher` took 8.28s on its first execution
and 0.0s on the next two (result cache). One run's first infra request returned
504 `backend_timeout` at the 10s deadline; another run's returned 200. The
gate's truth probe retries a 5xx up to 3 times (it asks which store served the
route, not how fast) and never retries a wrong basis; the sweep's discarded
warmup requests keep the cold hit out of the measured p95.

## Known-tight route, now a tracked latency exemption (#6858)

`/api/v0/iac/resources` measured 1.87s, 2.07s and 1.23s against its 2s ceiling
in three GREEN runs with the real graph, 1.53s in a fourth GREEN run, 3.20s in
a RED run, and 3.01s then 3.12s in two CI-runner-class runs against merged
main (PR #6860, the first CI attempt and its rerun). Its work counters are stable (2 calls, 108,152 buffers, 51
rows) across every run, so the work budget guards it and would catch a real
regression on this route; the ceiling itself is the fragile part.

A blocking gate that is red against merged main's own current behavior fails
CI for every unrelated PR, so this route's latency ceiling is now a tracked
exemption rather than a loosened number: `LatencyExemptions`
(`go/cmd/read-api-latency-gate/latency_exemption.go`) names the route, cites
issue #6858 (moving the route off the graph), and states the reason. The
mechanism is deliberately narrow: `EvaluateBudgets` skips only a non-HardFailed
latency breach on a listed route, so a 5xx on this route and any work-budget
breach still fail the run exactly as before (`TestExemptRouteStillBreachesOnWorkBudget`,
`TestEvaluateBudgetsStillBreaksExemptRouteOnHardFailed`); `printReport` marks
the row `BREACH-EXEMPT(#6858)`, never `OK`, so a reader never mistakes it for a
pass; `ValidateLatencyExemptions` refuses to start the gate if an entry is
missing its issue or reason, so nothing can be exempted without a tracked
reason; and an unlisted route is never exempt
(`TestLatencyExemptionsIsEmptyUnlessExplicitlyGranted`). The 2s ceiling is not
loosened, and this entry is removed when #6858 lands.
