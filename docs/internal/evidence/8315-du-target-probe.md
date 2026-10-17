# #8315 deployable_unit target probe: two-clause verdict is wrong on NornicDB

## Theory

The pre-write target guard built one statement with sequential anchored
`MATCH` clauses per distinct target. For `deployable_unit_edges` that is two
clauses (`(:Repository {id:$s})`, `(:Repository {id:$t})`). Hypothesis from
the #8283 CI failure: NornicDB answers that shape present when only the first
anchor exists, so the guard passes and the `UNWIND/MATCH/MATCH/MERGE` write
binds nothing while reporting success.

## Proof (scratch probe, throwaway test, removed before commit)

Backend: pinned `ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:74a8ed7b…`
(emulated amd64 under qemu on an aarch64 host — labelled emulated per the
issue) and `ghcr.io/eshu-hq/ci-neo4j-community` for the Neo4j column. Seed:
one `(:Repository {id: src})`, target absent.

| Probe shape | NornicDB, target absent | NornicDB, both present | Neo4j, target absent | Neo4j, both present |
| --- | --- | --- | --- | --- |
| `MATCH (:Repository {id:$s}) MATCH (:Repository {id:$t}) RETURN 1 LIMIT 1` | 1 row (WRONG) | 1 row | 0 rows | 1 row |
| `MATCH (:Repository {id:$repo_id}) RETURN 1 LIMIT 1` | 0 rows | 1 row | 0 rows | 1 row |

The two-clause verdict is a member of the multi-clause-read family in
`docs/public/reference/nornicdb-query-pitfalls.md` (a clause between the
anchor `MATCH` and `RETURN` corrupts the verdict; here a present verdict for
an absent target). A comma-separated pattern is not a fix — the backend
treats it like two clauses. The fix emits one single-clause statement per
distinct target and ANDs the verdicts in Go, the same single-pattern shape
the #8283 post-write verifier (`repositoryPresenceProbeCypher`) already
proves live on both backends.

## Change

`go/internal/storage/cypher/edge/writer/target_probe.go` (split out of
`unroutable.go` to hold the file cap):
`buildTargetPresenceProbeStatements` returns one `MATCH … RETURN 1 LIMIT 1`
statement per distinct target for all three guarded domains
(`handles_route` Endpoint+Function, `runs_in` repo DEFINES Workload,
`deployable_unit_edges` source+deployment repos).
`checkBatchTargetsPresent` fails the batch on the first miss with the same
`targetMissingError` (non-counting) / `targetProbeError` (counting) contract.
Per-chunk construction is unchanged, so a chunk's probes still cover at most
one write batch worth of rows (#6730 fence); each statement is constant size.

## No-Regression Evidence:

- Unit: `go test ./internal/storage/cypher/edge/writer -count=1` green,
  `-race` green; parent `go test ./internal/storage/cypher -count=1` green;
  `go vet` clean. New unit pins: every probe statement has exactly one
  `MATCH`, is read-only, and DU probes anchor both endpoints; chunk test pins
  3+2 single-pattern probes at BatchSize 2 for 3 distinct-target rows.
- Live (local, emulated amd64 NornicDB + Neo4j): new
  `TestBoltWriteEdgesDeployableUnitAbsentTargetFailsClosed` FAILED before the
  fix (silent success, the #8283 CI shape) and PASSES after; the
  present-targets happy path passes. All four
  `TestBoltWriteEdges{HandlesRoute,DeployableUnit}*` live tests pass on both
  backends. Full live-backend CI lanes (amd64) run the same tests on merge.
- Perf: correctness fix, no bench claimed. Before/after on the same input
  shape: the hot write template is byte-identical; probes change from 1
  two-clause statement per chunk to ≤2×batch-rows single-clause indexed point
  lookups per chunk (DU worst case; deduped per chunk). Focused live timings
  0.01–0.25s per test on both backends, no measurable regression on the write
  path; the guard's per-write-batch fence is unchanged.

## No-Observability-Change:

No new metric, span, log field, queue stage, or worker knob. Misses still
emit `eshu_dp_shared_edge_target_miss_total{domain}` plus the same WARN
fields; probe faults keep the same WARN. Probe-call count rises (several
cheap point lookups per chunk) but carries no new label cardinality.
