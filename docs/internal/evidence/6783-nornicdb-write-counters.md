# #6783 live evidence: NornicDB Bolt summary counters

Date: 2026-09-23. Worktree `eshu-6783-statement-coverage`, branch
`gate/6783-statement-coverage`. Proves slice-B counters end to end and
classifies NornicDB counter fidelity for the coverage gate design.

## Pins

- NornicDB: `timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f`
  (`-e NORNICDB_NO_AUTH=true -e NORNICDB_EMBEDDING_ENABLED=false`,
  isolated containers `eshu-6783-cov-nornic`, loopback `:27921`).
- Neo4j: `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
  (`-e NEO4J_AUTH=none`, `eshu-6783-cov-neo4j`, loopback `:27931`).
- Harness: temp test `go/internal/backendconformance/zz_coverage_live_test.go`
  (deleted after the run; log below), real inventoried statement
  (`DefaultWriteCorpus()[0]`: `MERGE (r:Repository {id: $repo_id}) SET
  r.name = $name`) plus CREATE/DELETE probes, through
  `WrapExecutor` with `ESHU_DIFFERENTIAL_CAPTURE=1`, fresh containers per
  run unless noted. Retention: destroy (containers removed at closeout).

## Results (fresh containers, run 3 — PASS log)

| Backend  | Statement                              | Reported counters                                      | Readback |
|----------|----------------------------------------|--------------------------------------------------------|----------|
| nornicdb | corpus MERGE…SET (create path)         | NodesCreated:1, rest 0                                 | 1 row    |
| nornicdb | CREATE (n:CoverageCounterProbe {id})   | NodesCreated:1, rest 0                                 | —        |
| nornicdb | MATCH … DELETE n                       | NodesDeleted:1, rest 0                                 | —        |
| neo4j    | corpus MERGE…SET (create path)         | NodesCreated:1, PropertiesSet:2, LabelsAdded:1         | 1 row    |
| neo4j    | CREATE probe                           | NodesCreated:1, PropertiesSet:1, LabelsAdded:1         | —        |
| neo4j    | MATCH … DELETE n                       | NodesDeleted:1                                         | —        |

Manifest match in the same run: checked-in
`internal/backendconformance/corpus.go:DefaultWriteCorpus` reported
executed on both backends — manifest text agrees with live recording
text.

## Classification

1. **NornicDB does not report `PropertiesSet` or `LabelsAdded`** (0 where
   Neo4j reports 2/1 on the same statements). Observed on MERGE…SET and
   CREATE-with-properties, consistent across the fresh-container runs.
   Graph truth is unaffected (nodes/labels/properties all persist —
   readback proves it). Candidate for #6787 upstream tracking; filing
   upstream needs owner OK, so not filed.
2. **Node/relationship created/deleted counters are correct** on NornicDB
   (2 fresh-container runs agree).
3. **Early-boot zeros are suspect, not asserted**: 1 of 3 fresh-boot runs
   reported all-zero counters on its first MERGE (~105s after container
   start, node proven created by readback); 2 of 2 runs at ≥120s report
   correctly. Plausibly a counters-subsystem startup race. B-7 legs boot
   long before any write, so this does not affect gate legs; do not cite
   the single observation as a defect.

## Impact on #6783

None blocking. Execution proof (records) and the never-executed /
always-empty FAIL rules do not depend on counters. NornicDB legs will
list every executed write under the report's advisory
`write-without-counters` / partial-counters section — that section
exists precisely for this fidelity gap.

## Full log (run 3)

```text
=== RUN   TestZZLiveCoverageCounters
    nornicdb: counters = {NodesCreated:1 ...}
    nornicdb: readback rows = 1
    nornicdb: probe "CREATE (n:CoverageCounterProbe {id: $id}" counters = {NodesCreated:1 ...}
    nornicdb: probe "MATCH (n:CoverageCounterProbe {id: $id})" counters = {NodesDeleted:1 ...}
    neo4j: counters = {NodesCreated:1 ... PropertiesSet:2 LabelsAdded:1 ...}
    neo4j: readback rows = 1
    neo4j: probe "CREATE ..." counters = {NodesCreated:1 ... PropertiesSet:1 LabelsAdded:1 ...}
    neo4j: probe "MATCH ... DELETE n" counters = {NodesDeleted:1 ...}
    live manifest match: internal/backendconformance/corpus.go:DefaultWriteCorpus executed on both backends with counters
--- PASS: TestZZLiveCoverageCounters (1.71s)
PASS
```

(Ellipses hide consistent zeros; durations are run durations, not
benchmarks. Raw log: `/tmp/6783-live-counters.log` on the run host.)

## Coverage exemptions (live proof, 2026-09-23)

The statement-coverage phase ran over the Sept-21 B-7 differential captures
(`/tmp/diff-capture/nornicdb`: 2712 records, `/tmp/diff-capture/neo4j`: 2658
records) with the checked-in manifest: before exemptions, 12 never-executed
builders and 36 always-empty reads per backend, **identical sets on both
backends** — corpus limits, not backend divergence. After exemptions: 2
executed, 0 never-executed, 20 exempted per backend, `[PASS]
statements_executed` (raw log `/tmp/6783-coverage-green.log`).

Method per gap: template/fragment presence probe over both captures'
recordings (`/tmp/6783-presence.log`), then write-side binding analysis
(bound params vs write params, create-vs-retract shapes, leg line ordering).
Salient results:

- `BuildRepoDependencySplitRetractStatements` **executed** (single-rel 70/73,
  single-runson 36/37, single-artifact 70/73 execs nornicdb/neo4j) but
  unattributed: it emits branch-chosen package consts via helpers that
  discovery cannot resolve inter-procedurally — exempted with the counts.
- `TestLiveBackendConformance` green on both cov containers the same day
  (nornicdb 0.10s, neo4j 3.13s test durations; logs
  `/tmp/6783-live-nornicdb.log`, `/tmp/6783-live-neo4j.log`), proving the
  corpus-helper exemptions (`DefaultWriteCorpus`, answer-truth, value-flow)
  execute outside B-7 replay.
- `source_tool` breakdown read: every B-7 RUNS_ON write sets
  `rel.source_tool = null`, so `IS NOT NULL` matches nothing by
  construction — exempted, with a product observation (a breakdown over an
  always-null property) recorded on #6783, not filed as a defect.
- Cloud/SecurityGroup evidence scans run at line 2 of the reducer leg,
  before any aws/gcp rels are written (first write line 720) — ordering,
  identical on both backends.
- `ResetRepositorySubtreeInGraph` / `DeleteRepositoryFromGraph` (#6783
  acceptance): absent from all `*.go` on origin/main (only evidence docs
  mention them) — resolved by deletion, nothing to inventory.
