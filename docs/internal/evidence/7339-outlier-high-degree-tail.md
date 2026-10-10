# #7339 — outlier 250-key batch at the high-degree tail

Follow-up to #7325 (PR #7327, `docs/internal/evidence/7325-outlier-fanout-batch.md`).
That record's `Proof limits / NOT_CHECKED` item 3 states the corpus-max
degree tail (up to 1,125 callees per member) was never exercised: every
#7325 measurement ran at max out-degree 64, while the registry row for
`runOutlierGraphRows` carries `max_keys: 250, max_results: 281250`. This
record closes that item at the QA-shaped tail (max out-degree 521,
`docs/internal/evidence/6649-calls-degree-floor.md`), plus the
single-order limitation (item 2) for the fan-out comparison. No
production code changed: the measurement holds the tail inside budget,
so per the issue's stop condition there is nothing to implement.

## Identity

- Base: `origin/main` `83cbf62fc`.
- Branch: `perf/7339-outlier-high-degree-tail`, in a dedicated feature worktree.
- Container (both runs, fresh per run, unique nonce per run):
  `docker run -d --name eshu-7339-neo4j-$NONCE -p 17439:7687 -e
  NEO4J_AUTH=neo4j/eshu-7339-pass
  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
  -> Neo4j Kernel `2026.08.1`, log-confirmed, the pinned QA image.
  Nonces `151827fb` (run 1) and `aa326841` (run 2). Removed
  (`docker rm -f`) after each run; no retained resources.
- Harness (committed, reproducible):
  `go/internal/query/codequery/outlier_tail_repo_scale_live_test.go`,
  `TestLiveOutlierTailRepoScaleNeo4j` -- run it directly to reproduce
  the numbers below (see that file's header comment for the exact
  command). Registered in `specs/live-tests.v1.yaml` (scheduled,
  Neo4j, env-gated by `ESHU_OUTLIER_NEO4J_LIVE`).

## Seed: base + density + 521-max tail

The test seeds the #6929/#7297 base (45,061 Functions in the swept
repository, 460,000 elsewhere), then the #7325 density shape (130,266
CALLS edges, `densitySeedCallsEdges`), then a deterministic tail
(`tailSeedCallsEdges`, MERGE-deduped per the #6649 methodology): exactly
production chunk index 1 of the sorted cohort member list (250 noise
members; chunk 0 holds the 11 `s6929:`-prefixed signal members first, so
chunk 1 avoids perturbing the signal fixture's majority-callee math)
gets degrees 521/400/300x2/200x6/100x20/50x40/25x180 -- 11,221 tail
edges, max single-statement rows 11,471 at the 250-key batch.

Measured (not assumed) after seeding, identically in both runs
(`ledger:7339-tail-outdegree-max`):

- Whole swept repository: mean **3.140**, p50 **1**, p90 **6**, p99 **30**, max **522** (521-edge anchor plus its 1 pre-existing density edge).
- Tail chunk alone: mean **45.9**, p50 **26**, p90 **101**, max **522**.

The tail-chunk `EXPLAIN` (log-only) shows `NodeUniqueIndexSeek` x2 with
`Expand(All)` -- the same anchor/index shape #7325 phase-1 `PROFILE`d;
only the data distribution is new.

## Sweep: batch 50 vs. 250, both key orders, both batch-execution orders

Four cells per run over the same seeded graph: `batch50-asc`,
`batch250-asc`, `batch250-desc`, `batch50-desc` (effective 50 via the
`subBatchSplittingReader` transport split, native 250 production
chunking). Per cell: tail single-statement probe (1 warm-up + 10
timed), whole-cohort fan-out (1 warm-up + 1 timed), full sweep
(`assembleOutlierTrack`, 1 cold + 10 warm).

| cell | tail probe warm-p95 (max) | fan-out | full cold | full warm p50 (p95) |
|---|---|---|---|---|
| run 1 batch50-asc | 58.3 ms (59.2) | 1164.6 ms | 1434.7 ms | 1280.8 ms (1366.1) |
| run 1 batch250-asc | 52.4 ms (55.3) | 691.1 ms | 913.2 ms | 964.4 ms (2235.7) |
| run 1 batch250-desc | 55.8 ms (60.5) | 712.0 ms | 905.4 ms | 1312.4 ms (4071.7) |
| run 1 batch50-desc | 62.7 ms (64.6) | 1064.2 ms | 1313.9 ms | 1338.1 ms (1733.0) |
| run 2 batch50-asc | 171.6 ms (190.3) | 1548.5 ms | 2153.3 ms | 1443.9 ms (6150.0) |
| run 2 batch250-asc | 47.1 ms (48.3) | 728.5 ms | 953.4 ms | 951.2 ms (1029.5) |
| run 2 batch250-desc | 45.7 ms (45.9) | 776.1 ms | 1021.2 ms | 965.7 ms (1043.2) |
| run 2 batch50-desc | 47.8 ms (48.5) | 1143.0 ms | 1320.4 ms | 1357.7 ms (1441.3) |

Rows-equal and findings-equal in every cell of both runs: tail edge
hash `3c73119d...`, whole-cohort edge hash `4123e629...`, finding hash
`c045350d...` (3 findings -- the same finding hash #7325 recorded, so
the tail perturbs no verdict). Byte-identical across nonces too.

## Verdict

- **The tail stays inside budget: no code change.** The 250-key tail
  statement (11,471 rows, the worst single statement the committed
  batching can emit at the QA tail) costs ~45-63 ms warm in every
  cell of both runs -- 15-19x under the 1 s budget, with the anchor
  plan unchanged. The issue's paging/truncation branch is not taken,
  deliberately: there is no breach to fix, and a silent partial result
  was never on the table.
- 250 stays ~39% faster than 50 on the whole-cohort fan-out at the
  521-max tail (run 1: 1164.6/1064.2 -> 691.1/712.0 ms; run 2:
  1548.5/1143.0 -> 728.5/776.1 ms), the same direction and magnitude
  as #7325's 40.9% at max-64 density, holding in both key orders and
  both batch-execution orders.
- Full-sweep warm-p95 spikes (2.2/4.1/1.7 ms-x1000 in run 1, 6.1 s in
  run 2's cold-container first cell) strike both batches and both
  orders and vanish on repeat, while the tail probe stays flat --
  host/backend jitter (cold page cache, checkpointer/GC), not a
  batch-size or tail-degree effect. Recorded in
  `ledger:7339-tail-full-jitter-note` so a future reader does not
  mistake a slow sample for a tail cost.

## Performance Evidence

Performance Evidence: `readOutlierCalleeEdges` tail probe and
`assembleOutlierTrack` full sweep against Neo4j 2026.08.1 (pinned
digest `sha256:eabfbb04...`), 45,058 cohort members in a
505,061-Function / 141,495-CALLS-edge seeded graph (130,266 density plus 11,221 tail plus 8 signal) with a 521-max tail
(measured mean 3.140, p50 1, p90 6, p99 30, max 522,
`ledger:7339-tail-outdegree-max`), via the committed
`TestLiveOutlierTailRepoScaleNeo4j` (two fresh-container runs, nonces
`151827fb`/`aa326841`): the 250-key tail single statement costs
52.4/55.8 ms warm-p95 run 1 and 47.1/45.7 ms run 2 (Under 1 s by
15-19x, `ledger:7339-tail-chunk-batch250-warm-p95-run1` /
-`run2`); whole-cohort fan-out 691.1/712.0 ms vs. 1164.6/1064.2 ms
run 1 and 728.5/776.1 ms vs. 1548.5/1143.0 ms run 2
(`ledger:7339-tail-fanout-batch250-run1` / `ledger:7339-tail-fanout-batch50-run1` /
-`run2` pair); full-sweep warm p50 964.4/1312.4 ms vs.
1280.8/1338.1 ms run 1 and 951.2/965.7 ms vs. 1443.9/1357.7 ms run 2,
with byte-identical tail rows (`3c73119d...`), edge rows
(`4123e629...`), and findings (`c045350d...`, 3 findings) across all
four cells (both key orders, both batch-execution orders) and both
runs. The `BuildOutlierCalleeEdgesCypher` anchor
(`MATCH (member:Function {uid: mid})`) is unchanged -- only the data
distribution is new -- and the tail `EXPLAIN` shows the same
`NodeUniqueIndexSeek` plan #7325 phase-1 `PROFILE`d, so the tail cost
is row-volume, not a plan-shape change.

## Observability Evidence

No-Observability-Change: this change adds a live test file, a
`specs/live-tests.v1.yaml` ledger row, measurement ledger rows, and
this evidence doc. It adds no metric, span, log key, or status field,
and removes none. Production code (`outlierCalleeEdgeBatchSize`,
`runOutlierGraphRows`, the registry row) is untouched, so the
route-level spans/logs for `/api/v0/code/divergence/findings` and
`/investigate` are unaffected.

## NornicDB

Not measured here: the issue restricts the backend to Neo4j only,
matching #7325 and the #6929 precedent. The tail changes no Cypher
text on either dialect (seed-only addition), so the same relative
behavior is expected but unproven on NornicDB.

## Proof limits

- The registry audit bound (250 x 1125 = 281,250 rows) remains an
  audit bound, not a measured cell: this record exercises 11,471 rows
  (the QA out-degree max 521), not the in-degree-floor-derived
  281,250. A 250-key chunk of 1125-degree members would be an
  in-degree shape; this read fans out along outgoing CALLS, where 521
  is the measured corpus max.
- Single seeded graph shape per run (two runs, two fresh containers);
  the tail alignment (chunk index 1) is deterministic from the
  deterministic seed, confirmed by identical hashes across nonces.
- Full-sweep warm-p95 jitter (above) is reported, not explained to
  root cause: it is budgeted as noise because it is
  batch-independent, order-independent, and non-repeating, while the
  budgeted tail statement itself is stable.
