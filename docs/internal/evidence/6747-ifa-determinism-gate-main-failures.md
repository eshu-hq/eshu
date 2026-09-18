# #6747: Ifa Determinism Gate failures on main after #6634

Issue #6747 covers the six `Ifa Determinism Gate` failures on `main` between
2026-09-14 and 2026-09-17. This note records the two shapes this branch fixes,
the evidence that establishes each cause, the proof for each fix, and the
hypotheses that were measured and rejected. Runs, job ids, and log excerpts are
quoted from the retained GitHub artifacts and job logs downloaded on
2026-09-17.

## Shape A: a stopped reducer leaves its repo-dependency partition leases held

Symptom (runs 35165747820 and 35200501470, `determinism-matrix`): the N=4
`workload_dependency` cell dies on its post-maintenance drain with repo
dependency intents left nonterminal while every fact row has succeeded:

```
drains: not satisfied after 3m0s (fact residual=0, required intents=2, completion events=0, populated domains=0/0)
  [FAIL] shared_projection_intents_nonterminal: required-nonterminal=2 (limit 0; completed_at IS NULL, excl advisory domains; repo_dependency subset=2; total=2)
```

Root-Cause Evidence: the pre-maintenance reducer of both failing runs logged,
at the SIGTERM the harness sends between the two drains,
`"message":"repo dependency projection cycle failed","error":"quarantine repo
dependency lease for 5m0s: list pending repo dependency intents: context
canceled","failure_class":"repo_dependency_projection_lease_quarantined"`
(35200501470) and `"error":"claim repo dependency lease: claim partition lease:
context canceled"` (35165747820). `RepoDependencyProjectionRunner.processOnce`
released its partition lease only on the success path and only through the
request context, so a cancellation after the claim left the row
`(repo_dependency, k, 4)` owned by the dead process with
`lease_expires_at = claim + 5m`. Every reducer process derives a distinct
owner (`prefix:hostname:pid:nonce/worker-k-of-4`, `go/cmd/reducer/config_projection.go`),
and `claimPartitionLeaseSQL` only updates a row whose lease has expired or
whose owner matches, so the post-maintenance reducer's claim for that
partition returned `false` on every poll for the rest of the drain, and the
runner treated a refused claim as an empty cycle with no log or metric. The
post-maintenance reducer logs of both runs show exactly that: the units on the
other shards were written within two seconds of their resolution, the units on
the leaked shard were never selected, and no error or readiness line appears.

Deterministic reproduction with the production binaries built from
`origin/main` (`f6a0434fa`), same compose stack and cassette as the gate: hold
`ACCESS EXCLUSIVE` on `shared_projection_intents` so the lane blocks after its
claim, then send the reducer the harness's SIGTERM and read the lease table
while the lock is still held. The sequence, from a fresh
`docker compose up -d nornicdb postgres` on the gate's own ports with
`scripts/lib/ifa_determinism_lifecycle.sh`'s `ifa_det_configure_runtime`
exported, so it can be repeated without the throwaway shim:

```bash
eshu-bootstrap-data-plane
eshu-ifa drive -cassette testdata/cassettes/workloaddependency/ifa-workload-dependency-family.json -workers 4
eshu-projector & eshu-reducer & sleep 8
docker compose exec -T postgres psql -U eshu -d eshu \
  -c "BEGIN; LOCK TABLE shared_projection_intents IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(60); COMMIT;" &
sleep 4; kill -TERM "$reducer_pid"; wait "$reducer_pid"
docker compose exec -T postgres psql -U eshu -d eshu -tA -c \
  "SELECT partition_id, lease_owner, lease_expires_at FROM shared_projection_partition_leases WHERE projection_domain='repo_dependency' ORDER BY 1;"
```

Before the fix every row carries the dead process's owner and an expiry five
minutes out; after it every `lease_owner` is NULL.

| binary | leases still owned after the reducer exited | shutdown log lines |
| --- | --- | --- |
| before the fix | 4 of 4 (`.../worker-k-of-4`, expiry +5m) | 4x `quarantine repo dependency lease for 5m0s: list pending repo dependency intents: context canceled` |
| after the fix | 0 of 4 | none |

The full N=4 cell (`run-cell.sh`, two concurrent loops, production defaults)
passed 10/10 and 5/5 iterations at 125s to 132s per cell before the fix; the
SIGTERM only leaks when it lands inside the lane's few-millisecond cycle, which
is why the failure is intermittent on CI's slower runners and why the loop
alone does not reproduce it. The forced interleaving above is the proof of the
leak. The second half, that a leaked lease produces the CI failure, was run
the same way (`run-stall-proof.sh`): the full cell with the four
`repo_dependency` lease rows rewritten after the pre-maintenance drain to a
foreign process-unique owner expiring in five minutes, then the
post-maintenance drain with a fresh reducer process. Before and after the fix
it ends with the CI text verbatim, `drains: not satisfied after 3m0s (fact
residual=0, required intents=3, ...)` and `[FAIL]
shared_projection_intents_nonterminal: required-nonterminal=3 (...;
repo_dependency subset=3; total=3)`, because a valid foreign lease is the
contract; the difference is that the fixed reducer logs `repo dependency
partition lease held by another owner` for each of its four shards at
22:57:07, the first poll, where the old one logged nothing.

Fix (`go/internal/reducer/repo_dependency_projection_quarantine.go`,
`repo_dependency_projection_runner.go`, `repo_dependency_projection_telemetry.go`):

- A cycle error while the runner's own context is done, in a phase that
  cannot have mutated anything (the claim, the selection scan, the
  empty-cycle exit, the missing-gate exit), is shutdown, not a partition
  fault. `failCycle` releases the partition lease and returns a shutdown error
  instead of a quarantine; `runSerial` exits without recording a cycle
  failure. A shutdown that lands after the acceptance-unit gate opened keeps
  the quarantine and its log line, because the graph write or Postgres commit
  may still be settling and `evidence-5122-repo-dependency-safety-proof.md`
  reserves the lease TTL as that quiescence window. Genuine errors, cycle
  deadlines, and heartbeat loss keep the fail-closed quarantine and still hold
  the lease (existing tests unchanged). Both CI interruptions were in the
  selection scan and the claim, so the release covers the observed failure
  without touching the ambiguous-commit contract.
- `releasePartitionLease` runs on `context.WithoutCancel` with a 10s bound so
  the release reaches Postgres after the cancellation, and logs a warning if it
  still fails.
- A claim refused because another owner holds the partition is logged once
  per contention episode, with a closing line when the claim succeeds again.

No-Regression Evidence: `go test ./internal/reducer -run RepoDependency
-count=1` passes after the change; the three new tests in
`repo_dependency_projection_shutdown_test.go` failed before it with
`processOnce() error = quarantine repo dependency lease for 5m0s: list pending
repo dependency intents: context canceled: a shutdown must not quarantine the
partition lease`, `lease releases across shutdown = 0, want 1`, and
`contended-lease log lines = 0, want exactly 1 per contention episode`. The
existing quarantine proofs (`TestRepoDependencyProjectionRunnerQuarantinesLeaseAfterGraphError`,
`...WholeCycleDeadlineQuarantines`, `...WholeCycleDeadlineIncludesSelection`)
still pass, so the fail-closed hold after a real error is unchanged. The live
forced-SIGTERM proof above went from 4 leaked leases to 0 with no other change.
The conflict domain is the `shared_projection_partition_leases` row per
`(repo_dependency, partition_id, 4)`; the claim and release are single
statements outside any longer transaction; the retry scope is the runner's
poll loop (500ms base, 5s cap). The change adds no work to the claim, selection,
or write path; the only new statement runs once per interrupted cycle.

Observability Evidence: new structured log lines `repo dependency partition
lease held by another owner` (once per contention episode, with partition_id,
partition_count, lease_owner, lease_ttl_seconds), `repo dependency partition
lease acquired after contention`, and `repo dependency partition lease release
failed; the lease expires on its TTL`; the existing
`eshu_dp_shared_projection_lease_quarantines_total` counter no longer counts a
shutdown as a quarantine. The coverage rows for the quarantine and telemetry
files in `docs/public/observability/telemetry-coverage.md` describe the lines,
and `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash
scripts/verify-telemetry-coverage.sh` passes.

## Shape B: the pre-maintenance drain returned before the shared lanes finished

Symptom (runs 35204957758 and 35222680019, `fault-injection (shard 3/4)`):
`run_drain_gate <cell>pre` passed with `shared-required-nonterminal=14` (and
`=4`) and the rationale exact-set assertion on the next line found all three
`rationale_edges` records missing; the retained reducer log shows the
`rationale_edges` lane writing them at 09:40:38.9, after the verdict. The
pre-maintenance predicate (`preMaintenanceQuiescence`, #6634) treated every
nonterminal shared intent as non-blocking.

Fix (`go/cmd/golden-corpus-gate/drains.go`): only the `repo_dependency`
subset, the one shared lane fenced behind the maintenance pass, is tolerated;
any other nonterminal shared intent keeps the poll going, as the strict drain
already does. The count query excludes the advisory domains from that subset so
it is always contained in the required total. RED/GREEN pair in
`drains_pre_maintenance_test.go` (`TestPollPreMaintenanceWaitsForSharedIntents`,
`TestPollPreMaintenanceToleratesRepoDependencyIntents`, two new reject cases);
`go test ./cmd/golden-corpus-gate ./internal/goldengate -count=1` passes.

## Rejected hypotheses

- Acceptance-row generation mismatch or missing row (#6679 shape): rejected;
  the stranded units' intents and acceptance rows are written in one
  transaction by the cross-repo handler, and the leak proof reproduces the
  stall without any acceptance write.
- RUNS_ON workload readiness or canonical-code quiescence blocking the unit:
  rejected; both paths log an INFO line that is absent from every failing
  post-maintenance reducer log.
- The 60s deployment_mapping and workload_materialization retries: rejected;
  the same retries appear in every passing run.
- #6162's restart-backend shape: not involved; none of the six runs ran that
  cell into a failure.
