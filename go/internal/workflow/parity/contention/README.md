# Workflow Contention Harness

## Purpose

This package runs the deployed-binary claim-plane contention workload that
`go/cmd/workflow-coordinator/REMOVAL_CONTENTION_SPEC.md` describes (#9727,
#8875). One `Run` call is one seed on one leg. It seeds a synthetic backlog,
runs the §5 nine-step mix from W concurrent claim loops for a timed window,
drains, then reverses: prefix-only cleanup and the three-zero residue check.
It writes a JSON report and a human summary.

Passing proves what spec §10 says and nothing more: the claim plane held the
§6 criteria at the recorded concurrency and backlog, bound to one digest. It
moves no authority.

## Ownership boundary

Owner: the workflow extraction drive (#8875). This is a sibling of the
`parity` harness and uses its `Backend` seam (`parity.NewInTreeBackend`,
`parity.NewSatelliteBackend`). It owns no schema and no runtime code. No
production binary imports it.

## Exported surface

- `LoadSpec` reads the §2 fields from the environment and fails closed.
- `Spec`, `Headroom`, `Target` hold the validated fields.
- `Run(ctx, Spec, Leg, Options) (*Report, error)` runs one seed on one leg.
- `Leg` names the backend, the `Observer`, the `HeadroomSource`, and the
  §3 applicability flags. `Options` holds the knobs outside §2.
- `Observer` and `SQLObserver` read and clean a leg database. `HeadroomSource`
  has two implementations: `SQLHeadroom` for a local server and
  `ExporterHeadroom` for postgres-exporter.
- `Report` (`JSON`, `Summary`), `Criterion`, `Violation`, `Trigger`.

## Dependencies

- `workflow/parity` for the `Backend` interface and both legs.
- `storage/postgres` (`WorkflowControlStore`) and `testutil/postgresproof`
  for the in-tree disposable database (entry test only).
- `workflow.ClaimPlaneClient` for the satellite leg over HTTP.
- `pgx` stdlib driver for the observer and headroom reads.

## Telemetry

None. This is test-harness code and adds no metric, span, or log key. The
operator signal is the report: per-operation client latency (p50, p99, max),
outcome codes, the per-second sampler series, pg_headroom peaks, and the
criteria table. The claim plane at `v0.1.1` exports counters only, so the
report marks server-side latency on the satellite leg as `NOT_AVAILABLE`. On
the in-tree leg the store call runs in process, so server side equals client
side.

## Gotchas / invariants

See `AGENTS.md`. The ones that bite:

- The guarded-admission target key is (kind, instance, scope, tenant,
  workspace, subject, policy, acceptance unit). The advisory rounds share
  scope and acceptance unit across goroutines. Without that, 25 of 25
  goroutines "win", and the guard looks broken when it is not.
- On the satellite, an at-budget quota claim answers with a pacing deferral
  (`parity.CodePaced`), not with an idle claim. The harness counts it as
  `claim_paced`, not as a fault.
- Production callers leave `RotationSalt` empty. A fixed salt pins every
  worker to one scope and fails the starvation criterion. The harness sends
  no salt.
- A paced or throttled quota item above budget stays pending by design
  until its hour bucket rolls. The report counts these as
  `quota_held_items`, and the drain criterion skips only those.

- Cleanup deletes only this seed's own rows (`run-<seed>-`, `wi-<seed>-`,
  and their claims) plus the harness scope family (`scope-`). Census and
  residue count the harness id families only. The deployed synthetic
  instances' rows (scope `synth-8875-rehearsal-tfstate`, which carries the
  bare prefix) are therefore "other" rows that must stay unchanged. A
  baseline that already holds harness rows aborts, and the reversal then
  deletes nothing (`cleanup_skipped` says so).

## How the spec maps to code

| Spec | Code |
| --- | --- |
| §2 fields, fail closed | `spec.go` `LoadSpec` (names every blank field) |
| §3 per-leg applicability | `Leg.Pin`, `Leg.Advisory`, `Leg.Quota`; `NOT_APPLICABLE` carries the parity sentinel |
| §4 ids, dry claim | `ids.go`; `run.go` `isolationProbe` (an issued claim before any write aborts) |
| §5 steps 1-4, 7-9 | `worker.go` (claim, heartbeat hold, 60/15/15/10 endings, heartbeat skip, pin, replay, stale writes) |
| §5 step 4 reaper | `loops.go` `reapLoop` (local legs only; live uses the deployed reaper) |
| §5 step 5 quota | `seed.go` `seedQuota`: "at" scope gets budget+extra items, "below" gets budget-1 |
| §5 step 6 advisory | `loops.go` `advisoryRound` (25 goroutines, barrier start) |
| §6 sampler | `loops.go` `sample` (1 s; census and fencing every 10 ticks) |
| §6 criteria | `analysis.go` `finish` |
| §7 triggers | `tracker.go` (client) and `loops.go` (database); `run.go` `abort` |
| §8 reversal | `run.go` `reverse`; `sqlobserver.go` `DeletePrefixed` |
| §9 evidence | `report.go` |

## What a pass proves and does not prove

A clean seed proves this much. The claim plane under test, at one digest and
at the recorded W and backlog, held the §6 criteria for the steps that apply
to its leg. Those criteria are exactly-once, fencing monotonicity,
stale-fence rejection, reclaim, replay acks, starvation bound, no overshoot,
zero deadlocks, zero loss, drain, and three-zero residue. The live leg also
proves the core server stayed inside pg_headroom while it ran.

It does not prove:

- **The cross-service commit fence.** Core does not consume the commit pin
  on `origin/main` (`PLANE_DECLARATION_9376.md` §4). The pin step proves the
  satellite route's single-holder and stale rejection only, not
  generation-level fencing.
- **Behavior of real collectors.** The claimers are harness goroutines with
  a synthetic instance, synthetic items, and no fact emission or commits.
  No real collector claims a harness item.
- **Throughput.** The live leg runs through `kubectl port-forward` from the
  drive host, and the local satellite leg runs an emulated amd64 image. The
  hop and the emulator distort latency, so no throughput or latency bar can
  be derived from either. The live leg yields correctness and headroom only.
  The binding throughput comparison is `REMOVAL_PREP.md` §4.
- **Grant or revert state.** The harness cannot see the grant expire or
  the revert PR merge; the operator watches both.
- **Authority.** A pass moves no authority, and a new digest voids it (spec
  §10).

## Spec readings

Each row interprets one spec sentence. `SpecReadings()` in `readings.go`
holds the same list, and every report carries it under `spec_readings`, so
the arbiter can accept or reject each reading at the live-run review. Line
numbers refer to `REMOVAL_CONTENTION_SPEC.md` on this branch.

| ID | Spec | Reading |
| --- | --- | --- |
| `workers` | §5 step 1, line 145 | W = `WORKER_COUNTS` x `WORKER_MULTIPLE`. Each worker is one claim loop with its own owner id and one claim at a time, the production shape. |
| `quota_held` | §5 step 5, line 153; §6, line 188 | `no_overshoot` passes when no (kind, scope, hour) bucket is above budget and one reached it. Held items are informational and excluded from drain; a new hour bucket may drain them. |
| `pin` | §5 step 7, lines 158-160 | Pin before complete. A short pin outlived while the lease lives must still complete. Half the abandoned claims are pinned past their lease, and the reaper must wait. Stale pins join step 9. |
| `replay` | §5 step 8, line 161 | "Terminal" means Complete and FailTerminal. The first call gets a 1 ms deadline, then one replay must ack. Release and FailRetryable are not replayed. |
| `stale_writers` | §5 step 9, line 163 | One write with the old token after the item is re-granted at a higher token. Success aborts; a non-rejection code fails. |
| `reclaim` | §6, line 181 | `finished_at` of the expired claim minus the later of `lease_expires_at` and the pin, against `reap_interval` + the longest reap pass + 1 s + skew. The requeue delay is not in the bound. |
| `starvation` | §6, line 186 | Longest ClaimNext call under the bound, and every backlog scope granted inside the window. Enqueue-to-claim wait is not judged. |
| `drain` | §6, line 195 | After the window, workers claim and complete the rest until only quota-held items are non-terminal, no claim is live, and every stale writer fired. |
| `planner_statistics` | §2 `backlog_items`, line 84 | Wait read-only (default 5m) for autovacuum to analyze `workflow_work_items` after seeding; `stats_ready=false` records a cold-statistics window. |
| `isolation_probe` | §4, lines 125-126 and 135-136 | The harness instance `synth-8875-rehearsal-instance-1` is not an enumerated live instance (`synth-8875-tfstate`, `synth-8875-pkgreg`), so `claims_enabled` from #510 does not apply to it and no row names it. The dry claim returns idle by construction. The report records `isolation_probe` as `VACUOUS` on both targets, and the arbiter rules on the instance. |
| `zero_loss` | §6, line 193 | Each sample requires synthetic item rows >= created (terminal plus non-terminal by construction). An advisory insert can briefly put rows above created. After the run, every client-acked terminal must be terminal in the database. |
| `lock_waiters` | §2 `pg_headroom`, line 89 | Live counts lock waiters on core's database only. Locally the ceiling sits above the 25 advisory goroutines plus W. |

Supporting measurements:

- **Planner statistics.** Without statistics, the in-tree claim statement
  picks `workflow_work_items_family_queue_depth_idx` with a row estimate of
  1. It then scans 49,750 rows in each of 200 loops: 18.7 s per claim at
  100,000 items, against 7.8-12 ms after `ANALYZE` (local shim, Postgres
  18.6).
- **Sampler cost.** `EXPLAIN (ANALYZE, BUFFERS)` of each observer query on
  100,000 items and 100,000 claims, with the v0.1.1 satellite schema on
  local Postgres 18.6, three repetitions:
  - Every 1 s tick: snapshot item counts 17-20 ms (parallel seq scan), and
    active claims 3.3 ms (bitmap on `workflow_claims_active_expiry_idx`).
  - Every 10 ticks: fencing 123-160 ms (index-only scan of the
    `(work_item_id, fencing_token)` key, merged with the item primary key),
    double terminal 31 ms, and census of items 16-17 ms, claims 20-21 ms,
    and runs 0.06 ms.
  - About 40 ms of database time per second on average: roughly 4% of one
    backend on the server the sampler watches.
- **Workers in practice.** At W=3 and the default mix, the window runs about
  5 claims/s. The 2% long holds of 30 s dominate, and the drain then runs at
  about 140 items/s locally.

## Write guard and abort independence

`Run` wraps the leg backend in a guard (`guard.go`). It refuses, before the
backend sees it, any write that names a non-synthetic id or a collector
instance other than `InstanceID`, or that calls a global route. A refusal
aborts with `non_synthetic_id_touched`.

Two loops watch the run, each with its own lifetime:

- **The watcher** (`watch.go`) runs from before the isolation probe until
  the reversal has finished. Its first reading is synchronous, so a server
  already over a ceiling stops the run before any write. Each
  `SampleInterval` it reads the abort file, the pg_headroom ceilings and
  the deadlock counter, and the SQLSTATE 40P01 count.
- **The database sampler** (`loops.go`) runs from the baseline census
  (before the seed) through the drain. It checks dual live claims, zero
  loss, the queue-depth cap, fencing, double terminals, and non-synthetic
  changes.

Both fail closed: `MaxSampleFailures` (3) consecutive failed reads abort
with `sampler_unavailable`, and more than `MaxSampleErrors` (5) failed reads
in total make the verdict `INCOMPLETE`. A run with no sampler reading is
also `INCOMPLETE`, and so is a live run with no headroom reading or no core
third zero. The report names the reason in `incomplete_reason`. The live
target refuses `SAMPLER_DISABLED`. On the live target, a headroom reading
that lacks an enforced series counts as a failed read: connections, lock
waiters, and the deadlock counter always, and replication lag when its
ceiling is set. An exporter URL aimed at the wrong port-forward still
returns a valid Prometheus page, so three such reads abort the run. A live
run where an enforced ceiling was never measured is `INCOMPLETE`. The live
target also needs one complete headroom reading before the isolation probe.
Without it, the run aborts with nothing written.

These §7 triggers are enforced in code and need nothing from the operator:

- dual claim, fencing regression, lost item, non-synthetic id
- exactly-once, stale write accepted, queue-depth cap
- deadlock, lock wait, pg_headroom (exporter ceilings)
- wall-clock cap, residue

SIGINT or SIGTERM to the entry test aborts as `external_abort`, and the
reversal still runs. `signal.NotifyContext` keeps catching signals until
the test returns, so a second Ctrl-C during cleanup is held, not acted on.
SIGKILL skips cleanup entirely. In that case delete the seed's rows by
hand, child tables first, with the statements under "Manual cleanup" below.
The reversal runs on its own 15 min `cleanupBudget`, outside
`wall_clock_cap` (reported as `cleanup_budget`): a run can end up to 15 min
after the cap fires. A headroom breach during the reversal is recorded as an
abort, but it does
not pause the batched deletes. Spec §8 puts removing the synthetic rows
ahead of everything else.

A run stopped before its baseline census deletes nothing, and neither does
a run whose baseline already held harness rows. Neither can tell its own
rows from earlier ones; `cleanup_skipped` says which case applied.

Two §7 triggers stay operator-watched, because the harness cannot observe
them: the grant expiring, and the revert PR merging. The same holds for
signals the exporter does not carry: container CPU and memory, satellite
mode drift, and restarts. The operator's watch loop writes the abort file
for any of them (runbook step 4). The queue-depth cap (`queue_depth_cap`
in the report) is the backlog, plus the quota workload, plus one advisory
round per interval.

## Abort triggers and their two detectors

| Trigger | Client tracker | Database sampler |
| --- | --- | --- |
| `dual_claim` | grant landed while an earlier epoch was provably held (abandoned, ended only later, or its holder's own write fenced away inside its live lease) | item with two `active` claim rows |
| `fencing_regression` | duplicate token, or a token not above one already returned | claim token above the item's current token, or a duplicate token |
| `lost_item` | client-acked terminal that is not terminal in the database | fewer synthetic rows than created |
| `non_synthetic_id_touched` | guard refused a write; grant or reaped claim without the prefix; dry claim issued | non-synthetic census changed against the baseline |
| `exactly_once` | grant after a terminal ack; two terminal acks | item with two terminal claim rows |
| `stale_write_accepted` | a superseded-token write succeeded | — |
| `deadlock_detected`, `lock_wait_above_bound`, `pg_headroom` | SQLSTATE 40P01 on any call | headroom sample against the ceilings |
| `wall_clock_cap`, `external_abort` | cap context | abort file present |
| `residue_nonzero` | — | baseline already prefixed; any zero broken after cleanup |
| `queue_depth_cap` | — | more synthetic rows than the run planned |

## Environment

The §2 fields (all required; `LoadSpec` names every blank one):
`ESHU_CONTENTION_TARGET` (`local`|`live`), `_DIGEST_UNDER_TEST`,
`_IAC_ENABLE_PR`, `_IAC_REVERT_PR`, `_AUTHORITY_DECISION`, `_WORKER_COUNTS`,
`_WORKER_MULTIPLE`, `_BACKLOG_ITEMS`, `_BACKLOG_SCOPES`, `_QUOTA_BUDGET`
(a number, or `NOT_APPLICABLE`), `_WINDOW_SECONDS`, `_WALL_CLOCK_CAP` (at
most 2h), `_STARVATION_BOUND_SECONDS`, `_LOCK_WAIT_BOUND_MS`,
`_PG_MAX_CONNECTIONS`, `_PG_MAX_LAG_SECONDS`, `_PG_MAX_CPU_MILLICORES`,
`_PG_MAX_MEMORY_MI`, `_PG_MAX_LOCK_WAITERS`, `_BASELINE_THROUGHPUT`, `_SEED`.
A local target may set `not_applicable_local` for the IaC PRs, the authority
decision, lag, CPU, and memory. A live target refuses that value and needs
`ESHU_CONTENTION_LIVE_GRANT=6558745112`. That value is the operator's
acknowledgement that the run sits under the standing grant; the harness
cannot read the grant, so it is not an authorization check. The harness
enforces connections, lag, and lock waiters itself. CPU and memory are
container figures that no SQL view or exporter series carries: the report
records them as operator-enforced, and the step-4 watch loop enforces them
from the same variables. `_LEASE_TTL`, `_HEARTBEAT_INTERVAL`,
and `_REAP_INTERVAL` default to `workflow/defaults.go` (60s/20s/20s).

Leg wiring: `PARITY_INTREE_DSN` + `PARITY_INTREE_DISPOSABLE=1` (in-tree);
`ESHU_CONTENTION_SATELLITE_URL`, `_SATELLITE_DSN`, `_SATELLITE_DATABASE`
(must match the connected name; never `eshu` or a maintenance database),
`_SATELLITE_QUOTAS` (`none`, or the exact `package_registry=<budget>` the
claim plane was started with). Optional: `_PG_EXPORTER_URL` (required on
live), `_PG_LOCK_DATABASE`, `_CORE_DSN` (read-only third-zero count),
`_REPORT_DIR`, `_ABORT_FILE`, `_CLOCK_SKEW` (default 250ms local, 2s live),
`_SAMPLE_INTERVAL`, `_SAMPLER_DISABLED=1` (overhead measurement only; the
verdict is then `OVERHEAD_MEASUREMENT_ONLY`). Every DSN and URL must be
loopback.

## Local run

The in-tree leg creates and drops whole databases, so it gets its own
throwaway server (`wf9727-pg-it`). It refuses a server that holds an `eshu`
or `workflow_claims` database, or that is the satellite DSN's server (same
`system_identifier`, or the same host and port when that cannot be read).
Every DSN must also be loopback on its first host and on every pgx fallback
host. On a local target a host name is resolved, and every address it
resolves to must be loopback; a name that does not resolve is refused. The
live target accepts loopback IP literals only (`127.0.0.1` or `[::1]`), so no
name is resolved between the check and the dial. The in-tree DSN must name
exactly one server (a TLS fallback to the same host is still one server).
The disposable-database helper dials it again, so a second host could be a
server the safety check never saw.

```sh
docker network create wf9727-net
docker run -d --name wf9727-pg-it -p 127.0.0.1::5432 \
  -e POSTGRES_USER=wf9727 -e POSTGRES_PASSWORD=wf9727 postgres:18
docker run -d --name wf9727-pg --network wf9727-net -p 127.0.0.1::5432 \
  -e POSTGRES_USER=wf9727 -e POSTGRES_PASSWORD=wf9727 postgres:18
docker exec wf9727-pg psql -U wf9727 -d postgres -c 'CREATE DATABASE wf9727_satellite'
docker run -d --name wf9727-cp --platform linux/amd64 --network wf9727-net \
  -p 127.0.0.1::8080 ghcr.io/eshu-hq/eshu-workflow@sha256:e1b564afa5f72167e3911aea7c881973c74bab123183c03d9d6af5a2d80e47a4 \
  --listen=:8080 --admin-listen=:8081 --reap-interval=2s --quotas=package_registry=5 \
  --database-url='postgres://wf9727:wf9727@wf9727-pg:5432/wf9727_satellite?sslmode=disable'
# PARITY_INTREE_DSN names wf9727-pg-it's mapped port; the satellite DSN names
# wf9727-pg's. Export the §2 fields and leg wiring above, then, from go/:
go test ./internal/workflow/parity/contention/ -count=1 -v -timeout 150m \
  -run 'TestContention(InTree|Satellite)$'
docker rm -f wf9727-cp wf9727-pg wf9727-pg-it && docker network rm wf9727-net
```

The real-Postgres observer tests (`sqlobserver_pg_test.go`) use the same
gate as the parity suite (`PARITY_INTREE_DSN` + `PARITY_INTREE_DISPOSABLE=1`)
and skip without it. No CI job sets that gate, so they run locally only.

## Live leg runbook (orchestrator only)

The live leg writes to the satellite's `workflow_claims` database on the core
Postgres server. It runs only under the standing grant (#8875 comment
6558745112), inside a DP-A window with a fresh enable/revert pair. The
in-tree leg always runs on a dedicated local throwaway server, never through
the core port-forward (`127.0.0.1:15432` below is core's server); the entry
test refuses it there. Run seeds 1 and 2 at W=3 (1x) and W=6 (2x),
then more seeds until two consecutive seeds are clean.

0. **Check pacing is off on the live claim plane.** Read
   `kubectl -n eshu get deploy eshu-workflow-claim-plane -o
   jsonpath='{.spec.template.spec.containers[0].args}'`. DP-A recorded
   `--listen=:8080 --admin-listen=:8081` only. If any `--pacing-*` flag
   is set above 0, stop and take it back to the arbiter (see "Known
   limitations"). NOT_CHECKED by the harness itself.
1. **Post the §2 fields on #8875 first.** Use the values and sources from
   DP-A §2: digest `sha256:e1b564af…e47a4`, the new IaC enable/revert PR
   numbers, the authority links, `worker_counts` 3 (1x) and 6 (2x),
   60s/20s/20s, `backlog_items` 100000 over 200 scopes, `quota_budget`
   `NOT_APPLICABLE` (no `--quotas` on the live claim plane), `window_seconds`,
   a `wall_clock_cap` of at most 2h, `starvation_bound_seconds` (3x the probe
   p99 through the same port-forward), `lock_wait_bound_ms` (the local
   in-tree leg's max lock wait), and pg_headroom: 85 connections, 30 s lag,
   10000m CPU sustained 60 s, 62 GiB memory, 5 lock waiters on `eshu`. A blank
   field stops the run.
2. **Post the freeze readings**: two reads at least 40 s apart of the DP-A §5
   step-1 list (satellite depth and counters, 0 WARN/ERROR, 0 restarts,
   in-tree `work_item_status` and `active_claims`, core `synth-8875-%` = 0,
   Postgres backends, CPU, memory, lag, and deadlocks).
3. **Port-forwards** (confirm the service ports with `kubectl -n eshu get svc
   eshu-workflow-claim-plane -o wide` first):

   ```sh
   kubectl -n eshu port-forward svc/eshu-workflow-claim-plane 18080:8080 &
   kubectl -n eshu port-forward pod/eshu-postgresql-0 15432:5432 19187:9187 &
   ```

4. **Abort watch** for what the exporter cannot see: CPU, memory, satellite
   mode drift, and restarts. Export the step-5 variables first, in the same
   shell: the loop reads the abort file path and the CPU and memory ceilings
   from them. Six consecutive samples over the CPU ceiling is the sustained
   60 s. Any hit writes the abort file. The watcher reads it every
   `SampleInterval` (1 s) from before the first write until cleanup ends,
   so the harness aborts and reverses within one sample.

   ```sh
   ABORT="$ESHU_CONTENTION_ABORT_FILE"; rm -f "$ABORT"; over=0
   CPU_MAX="$ESHU_CONTENTION_PG_MAX_CPU_MILLICORES" MEM_MAX="$ESHU_CONTENTION_PG_MAX_MEMORY_MI"
   while sleep 10; do
     read -r cpu mem < <(kubectl -n eshu top pod eshu-postgresql-0 --containers --no-headers \
       | awk '$2=="postgresql"{gsub("m","",$3); m=$4; if (m ~ /Gi$/) {sub("Gi","",m); m*=1024} else sub("Mi","",m); print $3, m}')
     if [ "${cpu:-0}" -gt "$CPU_MAX" ]; then over=$((over+1)); else over=0; fi
     [ "$over" -ge 6 ] && echo "postgres cpu ${cpu}m over ${CPU_MAX}m for 60s" > "$ABORT"
     [ "${mem%.*}" -gt "$MEM_MAX" ] && echo "postgres memory ${mem}Mi over ${MEM_MAX}Mi" > "$ABORT"
     args=$(kubectl -n eshu get deploy eshu-workflow-coordinator-satellite -o jsonpath='{.spec.template.spec.containers[0].args}')
     case "$args" in *--deployment-mode=active*--claims-enabled=true*) ;; *) echo "satellite mode drift: $args" > "$ABORT";; esac
     restarts=$(kubectl -n eshu get pods -o jsonpath='{range .items[*]}{.metadata.name}={.status.containerStatuses[0].restartCount}{"\n"}{end}' | rg 'workflow-(claim-plane|coordinator-satellite)' | awk -F= '{s+=$2} END {print s+0}')
     [ "$restarts" -gt 0 ] && echo "satellite or claim-plane restarted ($restarts)" > "$ABORT"
   done &
   ```

5. **Run one seed and leg** (from `go/` on the drive host; the credentials
   come from the cluster secret into the shell only, never into a file):

   ```sh
   export ESHU_CONTENTION_TARGET=live ESHU_CONTENTION_LIVE_GRANT=6558745112
   export ESHU_CONTENTION_SATELLITE_URL=http://127.0.0.1:18080
   export ESHU_CONTENTION_SATELLITE_DSN="postgres://$WF_USER:$WF_PASS@127.0.0.1:15432/workflow_claims?sslmode=disable"
   export ESHU_CONTENTION_SATELLITE_DATABASE=workflow_claims ESHU_CONTENTION_SATELLITE_QUOTAS=none
   export ESHU_CONTENTION_PG_EXPORTER_URL=http://127.0.0.1:19187/metrics ESHU_CONTENTION_PG_LOCK_DATABASE=eshu
   export ESHU_CONTENTION_CORE_DSN="postgres://$CORE_RO_USER:$CORE_RO_PASS@127.0.0.1:15432/eshu?sslmode=disable"
   export ESHU_CONTENTION_ABORT_FILE="$RUN_DIR/abort"     # RUN_DIR: a private local directory
   export ESHU_CONTENTION_REPORT_DIR="$RUN_DIR/reports"
   export ESHU_CONTENTION_PG_MAX_CPU_MILLICORES=10000 ESHU_CONTENTION_PG_MAX_MEMORY_MI=63488
   # plus every §2 field from step 1 (QUOTA_BUDGET=NOT_APPLICABLE)
   go test ./internal/workflow/parity/contention/ -count=1 -v -timeout 150m \
     -run 'TestContentionSatellite$'
   ```

   Expect claim-plane `ERROR` log lines `protocol handler error ... context
   canceled` on `CompleteClaim` and `FailClaimTerminal`: they come from the
   step-8 injected 1 ms timeouts, one per replayed call (compare the count
   with the report's `*_replay_first` codes). They are not an abort signal.
   Any other new WARN or ERROR line is.

   Budget the go test timeout as `wall_clock_cap` plus the 15 min cleanup
   budget: the reversal runs on its own deadline after the cap fires, so a
   run can end up to 15 min after the cap. The report records it as
   `cleanup_budget`.

   The live leg never calls the reap route. The deployed claim-plane reaper
   (`--reap-interval` default 20 s) and the satellite coordinator's reap loop
   do step 4. A run without `_CORE_DSN` reports the third zero as
   `NOT_CHECKED` and the verdict as `INCOMPLETE`.
6. **After each seed**: the queue depth must return to the frozen baseline
   (pending 1 per synthetic kind, claimed 0). The report's `final_census`
   must show no non-synthetic delta. Attach the JSON and summary to #8875.
   On any abort, follow spec §8: the harness has already stopped, drained,
   prefix-deleted, and re-checked residue. Next, merge the revert PR, and
   the owner syncs.

## Known limitations

- **Pacing latch rows.** With claim-plane pacing on (`--pacing-*` flags
  above 0), the harness's claims would upsert `workflow_claim_pacing` latch
  rows keyed `<kind>/<instance>` (`store/pacing_quota_pin.go:58` at v0.1.1),
  including the shared global row. Those
  keys carry no harness prefix, so the census counts them as "other" rows.
  The sampler would then see a non-synthetic change and abort, and cleanup
  cannot delete them. The census filter on that table (`Prefix%` on
  `domain`) never matches a harness row. Runbook step 0 keeps the run off
  a pacing-enabled claim plane. The harness does not check the flags
  itself: NOT_CHECKED.

## Manual cleanup

After a SIGKILL, delete one seed's rows (`<seed>` as recorded) child-first
on the leg database. These are the same filters `DeletePrefixed` uses:

```sql
DELETE FROM workflow_claims WHERE work_item_id LIKE 'synth-8875-rehearsal-wi-<seed>-%';
DELETE FROM component_quota_windows WHERE scope_id LIKE 'synth-8875-rehearsal-scope-%';
DELETE FROM workflow_run_progress WHERE run_id LIKE 'synth-8875-rehearsal-run-<seed>-%';
DELETE FROM workflow_run_completeness WHERE run_id LIKE 'synth-8875-rehearsal-run-<seed>-%';
DELETE FROM workflow_work_items WHERE work_item_id LIKE 'synth-8875-rehearsal-wi-<seed>-%';
DELETE FROM workflow_runs WHERE run_id LIKE 'synth-8875-rehearsal-run-<seed>-%';
```

The `component_quota_windows` statement is not seed-scoped, matching
`DeletePrefixed`: quota scope ids carry no seed. On the live database, batch
each statement (`ctid IN (SELECT ctid ...
LIMIT 1000)`) as `DeletePrefixed` does. Then rerun the residue counts.

## Related docs

- `go/cmd/workflow-coordinator/REMOVAL_CONTENTION_SPEC.md` (the contract)
- `go/internal/workflow/parity/README.md` (the parity harness and its legs)
- [Local Testing](../../../../../docs/public/reference/local-testing.md)
