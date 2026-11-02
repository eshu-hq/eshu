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
- A paced or throttled quota item above budget stays pending by design. The
  report counts these as `quota_held_items`, and the drain criterion skips
  only those.

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

## Readings of the spec

Each reading below is the harness's interpretation. The arbiter reads it with
the evidence.

- **Workers.** W = `WORKER_COUNTS` x `WORKER_MULTIPLE` (1 or 2). Each worker
  is one claim loop with its own owner id, one claim at a time: the
  production shape (one claim loop per collector Deployment).
- **Drain.** After the window, the chaos mix stops. Workers then claim and
  complete the rest of the backlog until every non-quota item is terminal,
  no claim is live, and every armed stale writer has fired.
- **Reclaim.** The reclaim time is the reap time (`finished_at` on the
  expired claim row) minus the later of `lease_expires_at` and the commit
  pin. The bound is `reap_interval` + the longest measured reap pass + 1 s +
  clock skew. The requeue delay (5 s) applies after the reap, so it is not
  part of this bound.
- **Starvation.** The longest ClaimNext call, issued or idle, must stay under
  `starvation_bound_seconds`, and every backlog scope must get a grant
  inside the window. With a 100,000-item backlog, the wait from enqueue to
  first claim is minutes by construction. The report does not judge it.
- **Replay.** A seeded share of Complete and FailTerminal calls get a 1 ms
  injected timeout, then one replay on a fresh deadline. The replay must ack
  whether or not the first attempt applied. Release and FailRetryable are
  not replayed: a replay after another worker re-claims the item is rightly
  rejected, and that case belongs to step 9.
- **Pin.** The satellite leg pins a seeded share before completing (expect
  ok). A short pin whose deadline passes while the lease lives must still
  complete. Half of the abandoned claims are pinned past their lease, and
  the reaper must wait for the pin (`reaped_before_pin_deadline` stays empty).
  Stale pins rotate into the step-9 writes.
- **Stale writers.** An abandoned claim's worker writes once with the old
  token (heartbeat, complete, fail_terminal, or pin) after the tracker sees
  the item re-granted at a higher token. The write must return the fence
  rejection. A success aborts, and a generic error fails the criterion.
- **Quota.** Budget is per (kind, scope, hour) on both legs
  (`component_quota_windows`). If the run crosses an hour boundary, a new
  bucket opens. The check is per bucket row.
- **Lock waiters.** A local in-tree advisory round puts up to 24 sessions
  on `pg_advisory_xact_lock` by design. Set the local
  `PG_MAX_LOCK_WAITERS` above the advisory goroutines plus W (40). On the
  live leg, `ESHU_CONTENTION_PG_LOCK_DATABASE=eshu` counts waiters on core's
  database only, against the DP-A ceiling of 5.

## Abort triggers and their two detectors

| Trigger | Client tracker | Database sampler |
| --- | --- | --- |
| `dual_claim` | grant landed while an earlier epoch was provably held (abandoned, ended only later, or its holder's own write fenced away inside its live lease) | item with two `active` claim rows |
| `fencing_regression` | duplicate token, or a token not above one already returned | claim token above the item's current token, or a duplicate token |
| `lost_item` | client-acked terminal that is not terminal in the database | fewer synthetic rows than created |
| `non_synthetic_id_touched` | grant or reaped claim without the prefix; dry claim issued | non-synthetic census changed against the baseline |
| `exactly_once` | grant after a terminal ack; two terminal acks | item with two terminal claim rows |
| `stale_write_accepted` | a superseded-token write succeeded | — |
| `deadlock_detected`, `lock_wait_above_bound`, `pg_headroom` | SQLSTATE 40P01 on any call | headroom sample against the ceilings |
| `wall_clock_cap`, `external_abort` | cap context | abort file present |
| `residue_nonzero` | — | baseline already prefixed; any zero broken after cleanup |

## Environment

The §2 fields (all required; `LoadSpec` names every blank one):
`ESHU_CONTENTION_TARGET` (`local`|`live`), `_DIGEST_UNDER_TEST`,
`_IAC_ENABLE_PR`, `_IAC_REVERT_PR`, `_AUTHORITY_DECISION`, `_WORKER_COUNTS`,
`_WORKER_MULTIPLE`, `_BACKLOG_ITEMS`, `_BACKLOG_SCOPES`, `_QUOTA_BUDGET`
(a number, or `NOT_APPLICABLE`), `_WINDOW_SECONDS`, `_WALL_CLOCK_CAP` (at
most 2h), `_STARVATION_BOUND_SECONDS`, `_LOCK_WAIT_BOUND_MS`,
`_PG_MAX_CONNECTIONS`, `_PG_MAX_LAG_SECONDS`, `_PG_MAX_CPU_MILLICORES`,
`_PG_MAX_LOCK_WAITERS`, `_BASELINE_THROUGHPUT`, `_SEED`. A local target may
set `not_applicable_local` for the IaC PRs, the authority decision, lag, and
CPU. A live target refuses that value and needs
`ESHU_CONTENTION_LIVE_GRANT=6558745112`. `_LEASE_TTL`, `_HEARTBEAT_INTERVAL`,
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

```sh
docker network create wf9727-net
docker run -d --name wf9727-pg --network wf9727-net -p 127.0.0.1::5432 \
  -e POSTGRES_USER=wf9727 -e POSTGRES_PASSWORD=wf9727 postgres:18
docker exec wf9727-pg psql -U wf9727 -d postgres -c 'CREATE DATABASE wf9727_satellite'
docker run -d --name wf9727-cp --platform linux/amd64 --network wf9727-net \
  -p 127.0.0.1::8080 ghcr.io/eshu-hq/eshu-workflow@sha256:e1b564afa5f72167e3911aea7c881973c74bab123183c03d9d6af5a2d80e47a4 \
  --listen=:8080 --admin-listen=:8081 --reap-interval=2s --quotas=package_registry=5 \
  --database-url='postgres://wf9727:wf9727@wf9727-pg:5432/wf9727_satellite?sslmode=disable'
# export the §2 fields and leg wiring above, then, from go/:
go test ./internal/workflow/parity/contention/ -count=1 -v -timeout 150m \
  -run 'TestContention(InTree|Satellite)$'
docker rm -f wf9727-cp wf9727-pg && docker network rm wf9727-net
```

## Live leg runbook (orchestrator only)

The live leg writes to the satellite's `workflow_claims` database on the core
Postgres server. It runs only under the standing grant (#8875 comment
6558745112), inside a DP-A window with a fresh enable/revert pair. The
in-tree leg always runs locally. Run seeds 1 and 2 at W=3 (1x) and W=6 (2x),
then more seeds until two consecutive seeds are clean.

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
   mode drift, and restarts. Six consecutive samples over a ceiling is the
   sustained 60 s. Any hit writes the abort file, and the harness aborts and
   cleans up within one sample.

   ```sh
   ABORT=/home/ubuntu/.cache/wf9727-abort; rm -f "$ABORT"; over=0
   while sleep 10; do
     read -r cpu mem < <(kubectl -n eshu top pod eshu-postgresql-0 --containers --no-headers \
       | awk '$2=="postgresql"{gsub("m","",$3); m=$4; if (m ~ /Gi$/) {sub("Gi","",m); m*=1024} else sub("Mi","",m); print $3, m}')
     if [ "${cpu:-0}" -gt 10000 ]; then over=$((over+1)); else over=0; fi
     [ "$over" -ge 6 ] && echo "postgres cpu ${cpu}m over 10000m for 60s" > "$ABORT"
     [ "${mem%.*}" -gt 63488 ] && echo "postgres memory ${mem}Mi over 62Gi" > "$ABORT"
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
   export ESHU_CONTENTION_ABORT_FILE=/home/ubuntu/.cache/wf9727-abort
   export ESHU_CONTENTION_REPORT_DIR=/home/ubuntu/.cache/wf9727-reports/live
   # plus every §2 field from step 1 (QUOTA_BUDGET=NOT_APPLICABLE)
   go test ./internal/workflow/parity/contention/ -count=1 -v -timeout 150m \
     -run 'TestContentionSatellite$'
   ```

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

## Related docs

- `go/cmd/workflow-coordinator/REMOVAL_CONTENTION_SPEC.md` (the contract)
- `go/internal/workflow/parity/README.md` (the parity harness and its legs)
- [Local Testing](../../../../../docs/public/reference/local-testing.md)
