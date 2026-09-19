# #6797 Work-Metric Shim: Postgres Buffers Separate The #6794 Fix From Its Parent

Prove-the-theory-first record for the read-API latency gate (#6797). The
theory: a per-request Postgres work counter from `pg_stat_statements`
(buffers, rows, calls) separates the pre-fix and post-fix status routes by
far more than run-to-run latency noise, so a work budget can fail RED and pass
GREEN where an absolute latency ceiling cannot.

## Method

- One seeded stack (800 scopes, 1,120 generations, 67,200 work items, 150,000
  IaC content_entity facts, ANALYZE after seed), kept with `--keep`.
- `pg_stat_statements` loaded with `shared_preload_libraries`,
  `track=all`, `compute_query_id=on`; the extension created afterward.
- For each route: 2 discarded warmup requests, `pg_stat_statements_reset()`,
  20 counted requests, then `sum(calls)`, `sum(rows)` and
  `sum(shared+local+temp blks read/hit/written)` for the current database,
  excluding statements that mention `pg_stat_statements`. Values below are
  per request (sum / 20). One pass per binary; the metric is deterministic.
- RED: eshu-api built from `59c605e48` (before #6808), sha256
  `0540ac5c4d5ed8ce7169f551bd4ff6ce319ef1d9a0d9c98de2c6db2e42521295`.
- GREEN: eshu-api built from `925e8016f` (contains `10055f58e`), sha256
  `bd46d53ecb530ea6819310b33525e5a17e8c5303b34a8ee9a16a0a9ff80f4494`.
- Each run recorded `lsof -p <pid>` resolving to the binary that was hashed.
- Host: developer Docker-on-macOS. These are counter values, not latency, so
  they do not depend on runner CPU speed.

## Result

| route | HTTP RED/GREEN | calls RED | calls GREEN | blks RED | blks GREEN | blks ratio | rows RED | rows GREEN |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `/collectors` | 200/200 | 29.1 | 26.0 | 712894 | 56179 | 12.7 | 20 | 25 |
| `/collector-readiness` | 200/200 | 1.0 | 1.0 | 1 | 1 | 1.0 | 0 | 0 |
| `/status/collector-readiness` | 200/200 | 1.0 | 1.0 | 1 | 1 | 1.0 | 0 | 0 |
| `/status/collectors` | 200/200 | 29.1 | 26.0 | 719734 | 63019 | 11.4 | 20 | 25 |
| `/status/freshness-causality` | 200/200 | 29.1 | 26.0 | 719734 | 63019 | 11.4 | 20 | 25 |
| `/status/governance` | 200/200 | 30.2 | 27.0 | 737250 | 63020 | 11.7 | 28 | 33 |
| `/status/hosted-readiness` | 200/200 | 29.2 | 26.0 | 737249 | 63019 | 11.7 | 20 | 25 |
| `/status/index` | 200/200 | 25.2 | 22.0 | 735942 | 61712 | 11.9 | 18 | 23 |
| `/status/ingesters` | 200/200 | 25.2 | 22.0 | 735942 | 61712 | 11.9 | 18 | 23 |
| `/status/operations` | 200/200 | 30.1 | 27.0 | 724533 | 67818 | 10.7 | 121 | 126 |
| `/status/operator-control-plane` | 200/200 | 29.1 | 26.0 | 719734 | 63019 | 11.4 | 20 | 25 |
| `/status/pipeline` | 200/200 | 29.2 | 26.0 | 737249 | 63019 | 11.7 | 20 | 25 |
| `/status/semantic-extraction` | 200/200 | 29.1 | 26.0 | 719734 | 63019 | 11.4 | 20 | 25 |
| `/index-status` | 200/200 | 25.1 | 22.0 | 718427 | 61712 | 11.6 | 18 | 23 |
| `/ingesters` | 200/200 | 25.1 | 22.0 | 718427 | 61712 | 11.6 | 18 | 23 |
| `/evidence/bundle` | 200/200 | 29.2 | 26.0 | 737249 | 63019 | 11.7 | 20 | 25 |
| `/repositories` | 200/200 | 1.0 | 1.0 | 1 | 1 | 1.0 | 0 | 0 |
| `/repositories/language-inventory` | 200/200 | 2.0 | 2.0 | 1 | 1 | 1.0 | 0 | 0 |
| `/iac/resources` | 200/200 | 2.0 | 2.0 | 108152 | 108152 | 1.0 | 51 | 51 |
| `/freshness/generations` | 200/200 | 2.0 | 2.0 | 19727 | 19727 | 1.0 | 51 | 51 |
| `/infra/resources/count` | 200/200 | 1.0 | 1.0 | 1 | 1 | 1.0 | 0 | 0 |

## Verdict against the ruling's criteria

- **Buffers criterion: proven.** blks RED / blks GREEN is between 10.7 and 12.7
  on all 14 status-family routes that touch Postgres (criterion: at least 10 on
  at least 9 of 14). The thinnest is `/status/operations` at 10.7.
- **Statement count cannot fail RED.** calls move 29.1 -> 26.0 and 25.2 -> 22.0,
  as the ruling predicted from the fix commit's 29 -> 25 round trips.
- **"Every named route RED >= 3x its derived budget" cannot hold literally.**
  Five named routes have identical RED and GREEN counters because #6808 did not
  change them: `/collector-readiness`, `/status/collector-readiness`,
  `/repositories`, `/repositories/language-inventory`, `/iac/resources`.
  A budget derived from GREEN cannot be exceeded 3x by an identical RED. On the
  14 changed routes it holds: RED / budget = ratio / 3 = 3.6 to 4.2 with the
  ruling's `blks = ceil(max_green_blks * 3.0)` formula.
- **Ruling on the criterion (maintainer):** "Read it as 'every route the
  RED/GREEN pair actually changed' (the 14). An identical RED can't exceed a
  GREEN-derived budget by construction. The 5 unchanged routes get GREEN-derived
  work budgets as forward regression guards, documented as not RED-provable
  with this pair." The buffers criterion is therefore proven for the 14 changed
  routes, and the five unchanged routes are guards only.

## Caveat found while running the shim

The gate's bulk infra-label graph seed created one node per label, not 150,000
(`UNWIND range(0, $count - 1) ... CREATE` yields a single node on NornicDB
when the bound is a parameter expression). The Postgres counters above are
unaffected; the graph-backed routes (`/repositories`, `/infra/resources/*`)
were measured against a near-empty infra graph.

## RED proof: the gate fails the pre-fix build on work alone

The work budget table (rendered from three GREEN runs against the #6793
candidate `545c27e99`; provisional local provenance at the time) was then run against `eshu-api` built from `59c605e48`
(before #6808, sha256
`0540ac5c4d5ed8ce7169f551bd4ff6ce319ef1d9a0d9c98de2c6db2e42521295`, confirmed by
`lsof`) on the standard stack, same seed (800 scopes, 150,000 nodes per infra
label, 150,000 IaC facts). Exit 1. Verbatim breach output:

```text
read-api-latency-gate: 2 route(s) exceeded budget:
  GET /api/v0/infra/resources/count: 5xx observed (p95 10.066324708s, budget 2s) -- HardFailed always breaches regardless of latency
      body: {"detail":"graph query exceeded its deadline","error":"Gateway Timeout"}

  GET /api/v0/infra/resources/inventory: 5xx observed (p95 10.002889334s, budget 2s) -- HardFailed always breaches regardless of latency
      body: {"detail":"graph query exceeded its deadline","error":"Gateway Timeout"}


read-api-latency-gate: 14 route(s) exceeded their Postgres work budget:
  GET /api/v0/collectors: calls 29.1 <= 38, blks 699711.5 > 181534, rows 19.6 <= 50 | p95 298ms
  GET /api/v0/evidence/bundle: calls 29.1 <= 38, blks 704271.3 > 195213, rows 19.6 <= 50 | p95 310ms
  GET /api/v0/index-status: calls 25.1 <= 33, blks 685449.7 > 191292, rows 17.3 <= 46 | p95 206ms
  GET /api/v0/ingesters: calls 25.1 <= 33, blks 720478.9 > 191292, rows 17.9 <= 46 | p95 738ms
  GET /api/v0/status/collectors: calls 29.1 <= 38, blks 721785.9 > 195213, rows 19.9 <= 50 | p95 556ms
  GET /api/v0/status/freshness-causality: calls 29.1 <= 38, blks 721785.9 > 195213, rows 19.9 <= 50 | p95 368ms
  GET /api/v0/status/governance: calls 30.1 <= 39, blks 721786.9 > 195216, rows 26.9 <= 64 | p95 448ms
  GET /api/v0/status/hosted-readiness: calls 29.1 <= 38, blks 721785.9 > 195213, rows 19.9 <= 50 | p95 425ms
  GET /api/v0/status/index: calls 25.1 <= 33, blks 720478.9 > 191292, rows 17.9 <= 46 | p95 518ms
  GET /api/v0/status/ingesters: calls 25.1 <= 33, blks 702964.3 > 191292, rows 17.6 <= 46 | p95 383ms
  GET /api/v0/status/operations: calls 30.1 <= 39, blks 726584.9 > 209610, rows 120.9 <= 252 | p95 390ms
  GET /api/v0/status/operator-control-plane: calls 29.1 <= 38, blks 721785.9 > 195213, rows 19.9 <= 50 | p95 383ms
  GET /api/v0/status/pipeline: calls 29.1 <= 38, blks 721785.9 > 195213, rows 19.9 <= 50 | p95 413ms
  GET /api/v0/status/semantic-extraction: calls 29.1 <= 38, blks 721785.9 > 195213, rows 19.9 <= 50 | p95 451ms
read-api-latency-gate: 2 route(s) exceeded their latency budget; 14 route(s) exceeded their work budget
```

The failure classes are exhaustive: exercised 65/113 (floor 65 held), no
explicitly-budgeted route dropped out of coverage, no meter failure, 0.00
background statements per second while idle. Each of the 14 routes #6808 changed
breaches on `blks` alone, with `calls` and `rows` inside budget, at 3.47x
(`/status/operations`, lowest) to 3.85x its budget, so no budget is one RED
merely brushes; their p95 latency (206ms to 738ms) is under their 1s ceilings,
which is why the ceilings alone could not fail this build. The two infra
timeouts are the pre-#6793 graph path (the read model reports "not installed" on
this stack) and are expected.

## Second RED run and the first GREEN read-back on the committed table

Both runs used the committed work table and the gate code as it was before the
report-table fix described at the end of this section (local Docker on macOS;
each eshu-api binary confirmed by sha256 and `lsof`).

- RED, eshu-api built from `59c605e48` (sha256 `0540ac5c4d5ed8ce7169f551bd4ff6ce319ef1d9a0d9c98de2c6db2e42521295`):
  exit 1. The same 14 routes #6808 changed breach on `blks` alone, `calls` and `rows`
  inside budget, at 3.38x to 3.85x their budget, with p95 279ms to 570ms under
  their 1s ceilings (twelve of them 279ms to 299ms; `/collectors` 407ms and
  `/evidence/bundle` 570ms). `/api/v0/infra/resources/count` also fails with a 504
  "graph query exceeded its deadline" (p95 10.0s) on the pre-#6793 graph path;
  `/infra/resources/inventory` did not breach in this run.
- GREEN, the #6793 candidate `545c27e99` (eshu-api sha256
  `10caee9bb998aad17c1b2340b31a17f26b6c79ec86708ede393e5fd95412db06`): exit 0.
  The seed's exact relational read-back (`ingestion_scopes`, `scope_generations`,
  `fact_work_items`, `fact_records`) held on a freshly migrated database: no
  migration or bootstrap step had written rows to those tables, and nothing
  was short. The read model was ready, `truth.basis` was `hybrid`/`content_index`,
  65 of 113 routes were exercised (floor 65), and every one stayed within its
  latency and work budget.

In that RED log the report table printed `OK` for all 14 routes the run failed
on: the status column did not see the work budgets, while the failure summary
below it was correct. The column now agrees with every verdict (5xx, latency,
work budget, unmetered route, named route not exercised), and each run logs the path and sha256 of both
budget tables. Replaying this run's saved report through the fixed table with
the committed work table marks 15 rows `BREACH`: the 14 routes on work, plus
`/infra/resources/count` on latency. The gate itself was not re-run live after
this change; the unit tests and that replay cover it.

A third RED run on the corrected code (eshu-api from `59c605e48`, sha256
`0540ac5c4d5ed8ce7169f551bd4ff6ce319ef1d9a0d9c98de2c6db2e42521295`) closes that
gap: exit 1, and the run log records the sha256 of both budget tables it
enforced (latency `2c3564bf...`, work `037060bc...`; the latency file's header
comment was edited afterwards to record this run, with no row changed, so its
hash differs now, while the work table's is unchanged). The report table prints
`BREACH` for all 14 routes over their work budget (buffers alone, 3.37x to 3.94x;
`/status/operations` lowest, `/index-status` highest) and 17 `BREACH` rows in
total. The other three are `/iac/resources` on latency (p95 3.196s against its
2s ceiling; its work counters were the usual 2 calls, 108,152 buffers, 51 rows),
and `/infra/resources/count` and `/infra/resources/inventory`, both a 504 at the
10s graph deadline (inventory breached this time). `/index-status` is also over
its 1s latency ceiling at 1.033s; the other 13 of the 14 work-breach routes had
p95 295ms to 640ms.

## Final table: re-rendered from three GREEN runs on merged main

Three GREEN runs against `origin/main` at `a39ecb343` (migration 109, the infra
read model and the fence from #6817, all landed; local Docker on macOS, not the
CI runner class), 15m16s total: `default` unchanged (13/21/14, still the
ceiling on the largest unnamed route); the 14 status-family rows and
`/index-status`/`/ingesters` drop from the candidate's buffer figures to their
real merged-main values (for example `/collectors` 39/58431/52, was
38/181534/50); the two infra rows are unchanged (13/74739/80 and 13/74739/14)
because the merged read model's measured work (3 calls, 24,913 buffers, 40 or 4
rows, identical across all three runs) computes to the same table values as
before. All three runs printed `truth.basis` `hybrid`/`content_index` for the
infra routes -- served from the read model, not the pre-fix graph path -- and
all 65 exercised routes passed against the *previous* work table (each run's
log records `work budgets ... sha256:037060bc...`). The re-rendered table
(`sha256:5300197a...`) is built from those runs, so it fits their measurements
by construction, but no run has been swept against the committed table itself;
that first execution will be CI's. The candidate-vs-merged-main half of the
provisional caveat is resolved; the runner-class half is not, since these three
runs are local.

## Performance and observability evidence for the seed's graph writes

No-Regression Evidence: `go/cmd/read-api-latency-gate`'s seed code
(`seed_graph.go`, `seed_graph_ranges.go`, `seed_graph_verify.go`,
`seed_iac_graph_rows.go`, `seed_infra_nodes.go`, `seed_owner_ledger.go`) and
`work_meter.go` run only inside this CI-only binary, against a throwaway
Postgres/NornicDB stack that `docker-compose.read-api-latency-gate.yaml`
creates and tears down per run. None of it is imported by eshu-api, the
ingester, the reducer, or any other production binary (it lives entirely under
`go/cmd/read-api-latency-gate`), so it cannot regress a production query plan,
write path, or backend resource. This is CI tooling measuring its own seed, not
a change to a shipped hot path, which is why "No-Regression" fits rather than
"Benchmark Evidence": there is no production baseline to compare against.
Backend: `timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f`
over Bolt, plus Postgres 18 via pgx COPY. Input shape: 800 scopes, 150,000
nodes per infra label across 8 labels plus 150,000 correlated IaC graph nodes
and 150,000 IaC content-entity facts -- 1.35M graph nodes total. The write
shapes and their batch-size costs are measured
in `docs/public/reference/nornicdb-write-shape-pitfalls.md`: bulk unconstrained
labels batch 10,000 rows per `UNWIND $rows CREATE` statement; `uid`-unique-constrained
labels batch 250 (250-batch 6.6s, 1,000-batch 14.3s, 5,000-batch 91.7s per
50,000 rows measured there; a single 50,000-row statement did not finish in 15
minutes). Terminal counts are read back and asserted, not estimated:
`VerifyGraphNodeCounts` confirms an exact per-label count derived from the plan
(150,000 for each infra label, plus one graph node per IaC fact of the matching
entity type: 200,000 for `TerraformResource`, 50,000 each for
`TerraformModule` and `TerraformDataSource`), and
`VerifyContentEntityCounts` confirms 1,050,000 `content_entities` rows, and
`VerifyRelationalCounts` (`seed_verify_relational.go`) confirms the seeded
`ingestion_scopes`/`scope_generations`/`fact_work_items`/`fact_records` counts
exactly on a freshly migrated database, failing the run before any request is
sent to eshu-api if short. This is safe because the seed is read back and
verified every run rather than trusted, the code path never executes against
production data, and the three live GREEN runs plus the RED run recorded
elsewhere in this document completed the identical seed-and-verify sequence
without a mismatch.

No-Observability-Change: this package emits its own stderr progress lines and
a JSON work report (`WriteWorkReport`) for its own CI consumption, and its
`tableProvenance` lines log which budget tables a run enforced. None of it adds
a metric, span, or log to eshu-api, the ingester, or the reducer, so there is
no production operator-facing signal to add.
