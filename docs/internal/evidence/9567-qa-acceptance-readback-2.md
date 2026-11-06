# #9567 QA acceptance readback 2 (2026-11-06 ~11:48-12:36Z, read-only)

Scope: reducer fleet state, live `/metrics` signals, queue/wave state,
Postgres probe-error check, node fit. No restarts, syncs, or deploys
performed.

Provenance: the readback-1 author went silent for ~3 days, so this drive
took over acceptance. A perf lane captured the wave (61 samples,
30 s cadence, `/tmp/wave-samples.tsv`) and fell silent before writing;
the orchestrator wrote this note from the lane's samples plus independent
live scrapes. Every figure below is re-derived here, not quoted from the
lane's interim report.

## 1. Deployed image vs fix SHAs

Reducer is `deployment.apps/eshu-resolution-engine` (revision 122; was
117 at readback 1), image `ghcr.io/eshu-hq/eshu` at the tag built from
source commit `81bf03f3b6c` (tag digest verified live against the tag and
omitted here, per the no-environment-identifiers rule).

Ancestry checks (`merge-base --is-ancestor` against the deployed commit):

- #9613 (`6ca3ad9294`, catalog projection + memory-limit gauge + heap signals): IN.
- #9913 merge (`e214a675ab`, bounded activation-obligation inbound load): IN.
- #10040 merge (`0cb384ca89`, workload probe quoting): IN.

Deploy mechanism is ArgoCD (this cluster's app, `sync-wave: 2`). The
117 -> 122 roll between readbacks advanced the image; its trigger is
beyond the retained event window, and no OOMKilled appears in retained
namespace events, so there is no evidence the roll was OOM-driven. No
rollout action taken: the running image already carries all three fixes.

## 2. Restart table

The reducer pod (`eshu-resolution-engine`, revision 122):

- `restartCount: 0`, no last termination recorded.
- Pod `startTime 2026-11-05T23:09:20Z` (~13.5 h uptime at last sample).
- Zero OOMKilled events in this pod's life.

## 3. Resource settings (as deployed)

- `resources.requests: cpu 500m, memory 1Gi`
- `resources.limits: cpu 8, memory 32Gi`
- `GOMEMLIMIT=24GiB` (gauge `eshu_dp_gomemlimit_bytes` = 2.5769803776e+10,
  confirmed live), `ESHU_REDUCER_WORKERS=8`
- Prometheus metrics on `:9464`.

The 1Gi request against multi-GiB real use (flagged in the issue and in
readback 1) is unchanged: still wrong as a scheduling signal, still
non-urgent given node fit below.

## 4. Wave shape and peak samples

Ambient OSV-pass-driven drain (lane-observed: vuln-intel/secalert
collectors restarted ~11:50Z; 529 pending at first sample, 8/8 workers
busy). No manual trigger: the readback-1 passive-observation path
reproduced. Sampling window 11:49:14-12:20:17Z, 61 samples.

Peak (fully-validated sample row):

- 11:52:24Z: working set **13756Mi (~13.43 GiB, ~42.0% of the 32Gi
  limit)**, heap alloc 760Mi, resident 9134Mi, pending 341, in-flight 8,
  pool active 8.
- Heap maximum one sample earlier (11:51:52Z): 12343Mi with resident
  14071Mi — the heap had already been collected by the peak-ws scrape.
  Burst-then-collect holds on the fixed image.
- Drained tail: working set back to ~855Mi by 12:20:17Z, pool active 0,
  heap 119Mi, resident 707Mi at the after-snapshot.

Observed maximum 42.0% of limit: **under the 50% (16 GiB) acceptance
bar. PASS on the memory criterion**, with 0 restarts and 0 OOM events
across the wave.

Caveat (sampling gap): from 12:05:04Z to 12:13:58Z the queue-gauge
columns (`pending`, `inflight`, `oldest_s`) scrape empty while the
memory columns continue (19 memory-only rows of 61). The peak is
unaffected (a fully-validated row), and the drained tail is corroborated
by the after-snapshot family depths below, but per-sample drain tracing
has a ~9 min hole. Minimum pending over fully-validated rows: 89
(from 529 at first sample).

## 5. Completion volumes (QA shape, since pod start)

`eshu_dp_reducer_executions_total` at the after-snapshot:

- `container_image_identity`: 9047 succeeded, 0 failed.
- `supply_chain_impact`: 33213 succeeded, 51 failed (~0.15%).
- `eshu_dp_queue_dead_letters_total{failure_class="supply_chain_impact_fanout_failed"}`: 6.

A QA-shaped wave (140 identity + 190 impact intents) is comfortably
exceeded by completed volume alone, with all 8 workers busy at peak: a
wave was in flight during this readback. Same basis as readback 1
(since-start counters); the wave-window slice is pending 529 -> drain.
The 51 impact failures / 6 dead letters are small (0.15%) and stay
within the retry/dead-letter design; a rise there reopens separately,
not against the memory bar.

## 6. Postgres probe-error verdict (#9986)

`syntax error at or near "k8s_resources"`: **0 occurrences** in the
trailing 4 h of `eshu-postgresql-0` logs. Quiet since before readback 1;
the "watch the next burst" condition from readback 1 has not fired.

## 7. Node fit

- Reducer node: usage 3059Mi (5%). No pressure.
- Postgres primary (`eshu-postgresql-0`) node: usage 55349Mi (92%).
  The pressure point is unchanged since readback 1 (was 94%): a
  Postgres-sizing observation, not a reducer blocker, but any future
  "node pressure" attribution must still start there.

## 8. Gaps / follow-up candidates (no code filed)

- Per-domain pending/claimed gauge labels (or document the queue-table
  query) so acceptance reads do not need DB access. (Carried over.)
- Prove the `k8s_resources` emitter identity to close the #9986 verdict
  loop. (Carried over; still quiet.)
- `resources.requests.memory: 1Gi` still misstates real use by an order
  of magnitude; reset from the measured profile per the criteria.
  (Carried over.)
- Explain the 12:05-12:14Z queue-gauge scrape gap (metrics-side: memory
  columns continued, so not a reducer stall, but the sampler should not
  silently record empty queue columns).
