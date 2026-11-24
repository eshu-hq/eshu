# #11380 one shared undrained-generation set: performance and observability evidence

The batch-claim gate (`gate_waited_generations`) and the drain probe path
(`ReadNVDBaselineDrain` per checkpoint) each defined "undrained generation
of an incomplete year" separately; that drift orphaned older windows once
(eshu-hq/eshu#11366). The fix extracts one shared row source,
`NVDBaselineUndrainedGenerationRowsSQL`, used by the claim gate's
older-window arm and by the new checkpoint family read
(`ReadNVDBaselineUndrainedGenerations`), whose rows the coordinator's drain
loader probes beside each checkpoint's last chunk and folds to one state
per year. See eshu-hq/eshu#11380.

Performance Evidence: the shipped batch claim is byte-identical, plus a
claim-path benchmark before and after, PostgreSQL 18.

- `TestStaleGate11234LeavesEveryOtherByteUnchanged` still passes on the
  pinned digest: the assembled `claimReducerWorkBatchQuery` string is
  byte-for-byte the base text, so the claim plan cannot have changed. The
  older-window arm now interpolates the shared core instead of carrying an
  inline copy.
- Claim benchmark, same host and invocation on the base tree and the
  branch (`ESHU_REDUCER_CLAIM_BENCH_DEPTHS=1000,10000 go test -run '^$'
  -bench BenchmarkReducerQueueClaimDeepQueue ./internal/storage/postgres/`):

| Depth | Before ns/op | After ns/op | Allocs/op before | Allocs/op after |
| --- | --- | --- | --- | --- |
| 1,000 | 5,016,636 | 5,670,498 | 4,764 B / 97 | 4,810 B / 97 |
| 10,000 | 48,923,211 | 46,016,642 | 6,940 B / 100 | 7,147 B / 100 |

Allocations are identical; ns/op moves in opposite directions at the two
depths (shared-host noise, dockerized proof database on the same laptop).
No plan change, as the digest proves.
- The new family read runs once per coordinator tick, not once per claim.
  On a disposable database with one checkpoint and 50 live work items,
  `EXPLAIN (ANALYZE, BUFFERS)` reports a primary-key checkpoint probe
  (`baseline_run_id = $1`, `NOT year_complete`) nested into the same
  stage/status-filtered work-item scan the claim arm already performs:
  execution 0.082 ms, 4 shared buffers. No new table, migration, or index:
  the read reuses the tables and predicates both call sites already scan.

No-Regression Evidence: one definition, two call sites, live PostgreSQL.

- `TestClaimGateAndCheckpointShareOneUndrainedSet` pins the sharing
  verbatim: both shipped statements embed the core. Mutant A1 (claim arm
  re-split to an inline lasts-only copy) fails it, the cross-consistency
  test below, and the #11366 pin
  `TestClaimBatchGatePriorityBoostsOlderBaselineWindowLive`; the fixed
  tree passes all three.
- `TestClaimGateAndCheckpointNameTheSameUndrainedSetLive` runs the
  shipped gate CTE standalone (sliced verbatim from the batch query) and
  reads the family set on one fixture, then asserts both name exactly the
  checkpoint lasts plus the orphaned older window, and neither names
  ordinary rows, terminal windows, completed years, or other runs. Mutant
  B1 (family read re-split to drop the run filter) fails it and the
  family live test; the fixed tree passes.
- `TestReadNVDBaselineUndrainedGenerationsListsOlderWindowsLive` pins
  the family read: the orphan is listed; the last chunk, terminal,
  completed-year, and other-run windows are not.
- `TestFoldNVDBaselineDrainStates*` (5 tests) pins the probe-half fold,
  including the identity (one state folds to itself, so every existing
  single-key fixture keeps its planner input) and the fail-closed empty
  and unknown-phase cells.
- Existing #11372 pin tests green unmodified (5/5 gate-priority live
  tests), the rejoin differential green (19/19 non-empty cells), and the
  full `coordinator`, `coordinator/vulnerability`, `vulnerabilitystore`,
  and `workflow-coordinator` suites green. Three `storage/postgres`
  failures (`WinnersQuietAbsoluteReprove`, `WinnersStagingAutovacuumSoak`,
  `SchemaBootstrapCoordinationDefaults`) fail identically on the pristine
  base: they need `/proc/loadavg` and an `ESHU_DEPLOY_CHECKOUT` the laptop
  does not provide.

No-Observability-Change: the change adds no metric, span, log line,
worker, lease, or runtime knob. The tick's drain span keeps its attribute
names (`nvd_baseline_drain_years_probed` now counts probe keys,
`nvd_baseline_drain_years_loaded` counts folded years); the held-year
gauges and timeout counters flow through the existing instruments. The
production proof of the orphan drain is the QA watch on eshu-hq/eshu#10728.

NOT_CHECKED: the QA orphan-drain observation (needs the QA environment),
and a production-scale `EXPLAIN` of the family read (same shape as the
claim arm the #11366 evidence already measured; CI owns the heavy gates).
