# #10871: quiescence answer shared once per code-call cycle

Refs #10871 (part of #10860, phase 1 idle checks step 3).

## Problem

Before the reducer writes call relationships between functions, it checks
that no repository's code is still being processed. The answer is the same
for every partition that asks in a given cycle, but the code-call runner
asked once per partition per cycle: 8 probes per cycle at the default
partition count, in sequential and concurrent paths alike, plus 8 probes on
every 500 ms blocked re-poll while idle. The second loop
(`repo_dependency`) already probes once per cycle per shard and is out of
scope; the per-intent `deployableUnitCanonicalReposReady` probe has no cycle
to share and is also untouched.

## Fix

`runOneCycleSequential`, `runOneCycleConcurrent`, and `processOnce` evaluate
`projectionLaneBlocked` once at cycle entry and pass the answer by value
into `processPartitionOnce` (new `blockedReason` parameter). The answer
lives only in the cycle's call frame: no Runner field, no cache, no
cross-cycle or cross-lane memo. Helpers live in
`go/internal/reducer/code/call/projection/quiescence.go`
(`gateCycle`, `blockedCycleResult`); SQL text is untouched.

## Measurement

Hermetic counting-drain proof
(`quiescence_once_per_cycle_test.go`, `PartitionCount: 8`):

| Case | Before (probes/cycle) | After (probes/cycle) |
| --- | --- | --- |
| Sequential open cycle, drain wired | active 8, quiescence 8 | active 1, quiescence 1 |
| Concurrent (8 workers) open cycle | active 8, quiescence 8 | active 1, quiescence 1 |
| Blocked cycle (uncommitted) | quiescence 8, claims 8x0, BlockedReadiness 8 | quiescence 1, claims 0, BlockedReadiness 1 |
| Probe error cycle | quiescence 8, claims 0 | quiescence 1, claims 0, cycle errors |
| Flip between cycles | n/a | second cycle re-probes (total 2) and honors the new answer |
| Mid-cycle flip (open at entry, flipped after first claim) | n/a | cycle acts on its entry answer (8 claims), next cycle re-probes |

RED run before the fix: `gate calls = (active=8, quiescence=8)` and
`BlockedReadiness = 7/8` failures. GREEN after: full
`internal/reducer/code/call/projection` package plus `internal/reducer`,
with `-race`, exit 0.

Query plan (fixture-shaped, non-QA): throwaway `EXPLAIN (ANALYZE, BUFFERS)`
with the verbatim production probe text and arguments on an isolated
postgres:18 with the bootstrap schema, 50 git scopes x 30 repository facts,
`VACUUM ANALYZE`d. Uncommitted state (one scope open): ~4.6 ms warm,
short-circuits at the first match (rows=1), served by
`fact_records_collector_status_active_idx` and the
`graph_projection_phase_state` pkey. Steady state (all committed): ~2.4 ms
warm, full 50-scope evaluation, index-served, no fact-table scan. The SQL
text is unchanged by this change, so per-call cost is unchanged by
construction; the saving is call count (8 -> 1 per cycle per dependency).

QA `pg_stat_statements` before/after deltas: NOT_CHECKED. No bounded
read-only QA route exists from here (the `eshu-postgresql-read-0` pod
requires a postgres password; no secret lookup attempted). The paired
before/after numbers above were measured the same way (same hermetic
harness, same partition count), and mean per-call cost is unchanged by
construction.

## Benchmark Evidence (#10871): hermetic probes per cycle 8 -> 1 per gate
dependency (sequential and concurrent), blocked BlockedReadiness 8 -> 1,
probe-error cycles fail before any lease claim; fixture EXPLAIN (ANALYZE,
BUFFERS) above shows index-served plans at ~2.4-4.6 ms warm with unchanged
SQL text.

## No-Regression Evidence (#10871): per-call probe cost unchanged (SQL text
identical); full projection package plus parent reducer package green with
`-race`; existing quiescence, blocked-episode, and live per-repository
(#6184/#7133) proofs untouched with assertions unweakened; worker,
partition, poll-interval, and lease knobs unchanged.

## Safety

The dangerous direction is open-at-probe to uncommitted mid-cycle (a new
generation activating while later partitions write). Two cases exist.
First-commit absence needs no clear: a new scope's nodes do not exist until
its canonical `Write` commits, and the phase publishes only after `Write`
returns (`writeCanonicalProjection` in `projector/runtime/stages.go`), so
an edge row whose endpoint MATCH misses writes nothing. On Neo4j the
no-clear claim holds in effect for re-projection: retract, files, and
entities run as one `ExecuteWrite` transaction
(`cmd/projector/neo4j_executor.go`, atomic-group branch in
`storage/cypher/canonical_node_writer.go`), but the NornicDB
`PhaseGroupExecutor` wiring (`cmd/projector/executor_wiring.go`) and the
sequential fallback commit the retract phase before entities — an
observable clear. In all cases the probe was never atomic with the MATCH
write — today's per-partition probe already spans lease, selection, load,
and write — so the hoist extends a pre-existing window from one partition
pipeline to one cycle (at most one wave older on the default 8-partition /
4-worker concurrent topology; up to 7 extra pipelines only under an
operator-set `workers=1`) rather than adding a hazard class. The
per-intent readiness gate still covers the caller's own acceptance unit.
The residual race (a silently missed edge completing its intent) is
tracked by #10938. Pinned hermetically:
`TestCodeCallProjectionRunnerMidCycleFlipActsOnCycleAnswer` (later
partitions act on the cycle answer) and
`TestCodeCallProjectionRunnerHonorsFlippedAnswerNextCycle` (the next cycle
re-probes). The residual widening question is flagged for the merge review.

## Observability Evidence (#10871): `lane_blocked_total` and
`BlockedReadiness` are now per-cycle (1) instead of per-partition (8) for
`code_calls`; the `lane_gate_seconds` histogram still records one point per
successful consultation (consultations are now per-cycle). `blocked.go`,
the projection `README.md`, the metrics reference, and the #7133 evidence
note are updated to the per-cycle semantics. `repo_dependency` counting is
unchanged.
