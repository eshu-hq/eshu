# #9567 QA acceptance readback 1 (2026-11-05 ~01:10-01:25Z, read-only)

Scope: reducer fleet state, live `/metrics` signals, Postgres probe-error check,
queue/wave state, node fit. No restarts, syncs, or deploys performed.

## 1. Deployed image vs fix SHAs

Reducer is `deployment.apps/eshu-resolution-engine` (revision 117), image
`ghcr.io/eshu-hq/eshu` at the tag built from source commit `b41a05ba76`
(merge of #10051). The registry digest was verified live against the tag
and is omitted here; the ancestry checks below pin the content.

Ancestry checks (`merge-base --is-ancestor` against the tag commit):

- #9613 (`6ca3ad9294`, catalog projection + memory-limit gauge + heap signals): IN.
- #9913 merge (`e214a675ab`, bounded activation-obligation inbound load): IN.
- #10040 merge (`0cb384ca89`, workload probe quoting): IN.

Deploy mechanism is ArgoCD: the Deployment carries an
`argocd.argoproj.io/tracking-id` annotation pointing at this cluster's app
plus `argocd.argoproj.io/sync-wave: 2` (revision 117). No rollout action
taken: the running image already carries all three fixes, so Phase 2's
restart clause did not trigger. The exact tracking-id value was observed
live and is omitted here per the no-environment-identifiers rule.

## 2. Restart table

The reducer pod (`eshu-resolution-engine` Deployment, revision 117) on
its 247 GiB node:

- `restartCount: 0`, no last termination recorded.
- Pod `startTime 2026-11-05T00:11:02Z`, container started `00:11:16Z`
  (uptime ~70 min at last sample 01:24:35Z).
- Every pod in namespace `eshu` shows age ~67m: cluster-wide restart at
  ~00:11Z (cause not investigated; read-only lane).

Zero OOMKilled events in this pod's life.

## 3. Resource settings (as deployed)

- `resources.requests: cpu 500m, memory 1Gi`
- `resources.limits: cpu 8, memory 32Gi`
- `GOMEMLIMIT=24GiB`, `ESHU_REDUCER_WORKERS=8`
- Prometheus metrics on `:9464` (`ESHU_PROMETHEUS_METRICS_ENABLED=true`).

The 1Gi request against multi-GiB real use (flagged in the issue) is
unchanged. Node fit below makes it non-urgent but it is still wrong as a
scheduling signal.

## 4. Live `/metrics` scrape (in-pod curl, 01:1xZ)

Previously-absent signals are present and nonzero on this image:

- `eshu_dp_gomemlimit_bytes` = 2.5769803776e+10 (24.0 GiB, matches env).
- `go_gc_gomemlimit_bytes` = 2.5769803776e+10 (OTEL runtime agrees).
- `go_memstats_heap_alloc_bytes` = 1.1795120656e+10 (~10.98 GiB at scrape).
- `go_memstats_heap_inuse_bytes` = 1.2139487232e+10 (~11.30 GiB).
- `go_memstats_heap_sys_bytes` = 1.2147621888e+10 (~11.31 GiB).
- `go_memstats_heap_released_bytes` = 8.060928e+06 (~7.7 MiB).
- `go_memstats_next_gc_bytes` = 1.5513582626e+10 (~14.4 GiB).
- `process_resident_memory_bytes` = 1.243199488e+10 (~11.58 GiB).
- `go_goroutines 104`, `go_threads 16`.

The #9613 telemetry gap (`eshu_dp_gomemlimit_bytes` registered but absent)
is closed on the deployed image.

## 5. Queue / wave state

Reducer queue gauges at first scrape (~01:1xZ):

- `eshu_dp_queue_depth{status=pending}` 925, `{in_flight}` 8,
  `{retrying}` 3. `eshu_dp_worker_pool_active` 7-8 (all workers busy).
- Per-domain completions since pod start (~67 min):
  `container_image_identity` 1380 (queue-wait sum 347547 s, mean ~252 s),
  `supply_chain_impact` 533 (queue-wait sum 1.1188e6 s, mean ~2098 s).
- `eshu_dp_queue_oldest_age_seconds` 253481 (~70 h, source `git`): a stale
  tail item, not the live wave.
- By 01:21:22Z pending fell 925 -> 707 and retrying rose 3 -> 10
  (~211 intents completed in under a minute); then held at 707/8/10
  through 01:24:35Z.

A QA-shaped wave (140 identity + 190 impact intents, 8 workers) is
comfortably exceeded by completed volume alone (1380 + 533) with all 8
workers active: a wave was in flight during this readback. Per-domain
pending/claimed split is NOT exposed by any gauge label (queue depth is
only `queue` x `status`); that split needs the queue-table SQL behind
`go/cmd/reducer/postgres_gauge_wiring.go`, not scraped here.

## 6. Peak working-set samples (30 s cadence)

`kubectl top pod` working set (ws), with in-pod heap alloc / resident:

| Taken (UTC) | ws | heap alloc | resident | pool active | pending |
|---|---|---|---|---|---|
| ~01:1x | 12208Mi (11.9 GiB, 37% of limit) | ~11.0 GiB | ~11.6 GiB | 8 | 925 |
| 01:19:35 | 11832Mi | 0.97 GiB | 8.16 GiB | 7 | 925 |
| 01:20:02 | 4225Mi | 0.87 GiB | 1.83 GiB | 8 | 925 |
| 01:20:28 | 1936Mi | 1.26 GiB | 2.03 GiB | 4 | 925 |
| 01:20:55 | 1945Mi | 0.83 GiB | 1.91 GiB | 4 | 925 |
| 01:21:22 | 1988Mi | 0.87 GiB | 2.01 GiB | 8 | 707 |
| 01:21:48 | 1995Mi | 1.19 GiB | 2.02 GiB | 7 | 707 |
| 01:22:22 | 2122Mi | 1.00 GiB | 2.06 GiB | 7 | 707 |
| 01:22:49 | 2168Mi | 4.14 GiB | 6.37 GiB | 7 | 707 |
| 01:23:15 | 3053Mi | 3.35 GiB | 4.74 GiB | 7 | 707 |
| 01:23:42 | 7114Mi | 4.98 GiB | 6.42 GiB | 6 | 707 |
| 01:24:08 | 6558Mi | 1.49 GiB | 3.04 GiB | 8 | 707 |
| 01:24:35 | 2553Mi | 1.75 GiB | 2.49 GiB | 8 | 707 |

Observed maximum: 12208Mi (~11.9 GiB, ~37% of the 32Gi limit), under the
50% (16 GiB) acceptance bar. Burst-then-collect pattern holds on the fixed
image: heap grows to ~5 GB, GC reclaims within a minute, working set
follows (01:23:42 -> 01:24:35: 7.1 -> 2.6 GiB).

Caveat: sampling started ~68 min after pod start, so the wave's earlier
peak (1380 + 533 completions mostly pre-date sampling) is NOT captured.
These figures bound the observed tail, not the full wave.

## 7. Postgres probe-error verdict (#9986)

`syntax error at or near "k8s_resources" at character 494`:

- 71 occurrences in the trailing 24 h of `eshu-postgresql` logs.
- Bursts only: 2026-11-04 23:53:58-23:54:33Z and 2026-11-05
  00:11:10.028-00:11:16.489Z (the latter straddles the cluster-wide
  pod restart; errors at 00:11:10-12 predate the new reducer container
  start at 00:11:16).
- Zero occurrences in the 60+ min since (`--since=60m` count = 0).

Verdict: quiet on the current image for over an hour; last burst
coincides with the rollout, consistent with old-image drain. NOT a clean
"fix verified": the emitter was not proven to be the reducer image, and
the pre-restart burst (23:53Z) shows the error was alive on whatever ran
then. Keep watching; next burst reopens against #9986/#10040.

## 8. Node fit

- Reducer node: capacity ~247 GiB, usage 57377Mi (23%),
  `MemoryPressure=False`.
- Postgres primary (`eshu-postgresql-0`) node: capacity ~61 GiB;
  node usage 56612Mi (94%), pod usage ~55.4 GiB;
  `MemoryPressure=False`.
- Postgres read replica node at 18% usage.

The issue-era colocation (reducer + Postgres primary on one 95% node) is
gone: different nodes, reducer node at 23%. The pressure point moved with
Postgres: its primary node sits at 94% with the PG pod itself holding
~55 GiB. That is a Postgres-sizing observation, not a reducer blocker,
but any future "node pressure" attribution must start there.

## 9. Cheapest wave-driving option (no wave triggered)

No rescan was triggered (per brief). If the orchestrator orders a driven
wave, the natural lever is the OSV reopen path that produced the original
kills; the exact command was not established in this read-only lane
(left for the orchestrator: driving options live with lane 1 / the
activation-obligation work in #9781).

## 10. Gaps / follow-up candidates (no code filed)

- Per-domain pending/claimed gauge labels (or document the queue-table
  query) so acceptance reads do not need DB access.
- Prove the `k8s_resources` emitter identity (which binary emits the
  pre-gate probe) to close the #9986 verdict loop.
- `resources.requests.memory: 1Gi` still misstates real use by an order
  of magnitude; reset from the measured profile per the criteria.
