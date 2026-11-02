# #8780: cumulative deltas from the newest activated full

## Problem

A git delta was diffed from the scope's active commit, and the projector
resolved its union base (#7797) at that same commit. After one delta
activated, the next delta's baseline was that delta's commit, where no full
generation exists, so it refused `refused_base_missing`, wrote a reindex
watermark, and forced a full re-parse. A fresh delta projected once per full;
every later push paid a forced full (about 8.7 minutes on the busiest
repository) instead of the two-minute delta band.

## Design (ruling Option B)

Every delta is cumulative from the scope's newest activated full generation
F (retention keeps the same row for a delta-active scope, #7802, provided no
activated full carries a status outside active, completed, or superseded; a
mismatch fails closed to a forced full):

- changed = added, modified, and renamed-to paths of `F..head`;
- deleted = `D(F..head) ∪ D(active..head)`; the second term names a file the
  active chain added that the head no longer has, which neither F nor
  `F..head` contains;
- `delta_baseline_commit_sha` = the active commit (the #7319 fence commit,
  unchanged in meaning and decision);
- `delta_union_base_commit_sha` = F's commit (migration 210); readers resolve
  the base at `COALESCE(delta_union_base_commit_sha, delta_baseline_commit_sha)`.

A delta touching at least half of F's tracked files takes a full
(`delta_exceeds_base_half`). The no-change check stays on the active commit,
so a quiet delta-active scope does not re-parse its cumulative set.

## Prove-the-theory shim (before the change)

Theory: cumulative deltas from the newest activated full stay small enough
over a reconcile interval that a delta stays far cheaper than the forced full
it replaces.

Size, from read-only blobless clones of the four busiest default branches by
accepted webhooks in 24 hours. Method: F is the first-parent parent of the
first commit in the window (a stand-in for a full at the start of the
interval); for every first-parent head H in the window,
touched = |diff --name-only --no-renames F..H ∪ D(prev..H)|, and
ratio = touched / files(F).

| repository | window | heads | files(F) | touched max | touched median | ratio max | ratio p95 |
|---|---|---|---|---|---|---|---|
| busiest repo A | 24 h | 67 | 580 | 72 | 37 | 0.1241 | 0.1172 |
| busiest repo A | 72 h | 77 | 580 | 77 | 37 | 0.1328 | 0.1259 |
| busiest repo B | 24 h | 30 | 5907 | 93 | 48 | 0.0157 | 0.0157 |
| busiest repo B | 72 h | 118 | 5856 | 326 | 226 | 0.0557 | 0.0502 |
| busiest repo C | 24 h | 8 | 1609 | 40 | 15 | 0.0249 | 0.0249 |
| busiest repo C | 72 h | 27 | 1553 | 233 | 139 | 0.1500 | 0.1494 |
| busiest repo D | 24 h | 5 | 1041 | 3 | 2 | 0.0029 | 0.0029 |
| busiest repo D | 72 h | 10 | 1040 | 18 | 17 | 0.0173 | 0.0173 |

Bar (ruling §3): p95 ratio at most 0.25. Result: PASS, max p95 0.1259 even
with a 72-hour-old base. The 0.5 valve would not have fired.

Wall, from one hosted QA deployment (scope_generations and fact_work_items on
a read replica). The projector wall (work item created to succeeded) does not
depend on the touched count, because the #8779 union projection rewrites the
whole union:

| repository | kind | touched | facts | projector s | stored to active s |
|---|---|---|---|---|---|
| busiest repo B | delta | 2 | 81,420 | 117 | 123 |
| busiest repo B | delta | 53 | 81,608 | 111 | 120 |
| busiest repo B | full | all | 95,409 | 126 | 523 |
| busiest repo B | full | all | 176,188 | 99 | 468 |
| quiet repo E | delta / full | 1 / all | 20,363 / 42,241 | 29 / 20 | 35 / 347 |
| quiet repo F | delta / full | 4 / all | 13,130 / 30,098 | 24 / 13 | 25 / 310 |

The forced full's cost is the full parse before store (468-523 s stored to
active on repo B, against about 120 s for a delta). A cumulative delta adds
only the parse of its touched set (at most 326 files, 5.6 percent, at 72 h) to
the delta band. Bar: the largest cumulative delta under half the forced-full
wall (about 261 s on repo B). PASS by estimate. The after-measurement below
is not run by this change; it confirms or refutes the estimate after deploy.

Decision: Option B proceeds; the growth valve ships at 0.5 (the #7926 half
rule) as a bound, as a constant, not a tuning knob.

Not explained, not in scope: a 2-file delta on repo B stores about 81k facts
against 95k for a full; fact volume, not touched count, dominates delta store
cost.

## Local proof (this change)

- Collector, real git repository with commits F, c1, C:
  `TestUpdateRepositoryDiffsCumulativeDeltaFromNewestFull` (changed `b`, `f`;
  deleted `e`; fence c1; union base F), the decision table, the valve, the
  sweep-path state reuse, and the resolver path end to end.
- AC2 oracle on the real snapshotter and fact builder
  (`TestCumulativeDeltaUnionMatchesFullAtHead`): `OverlayDeltaBase(F, d2)`
  equals a full built at C on files, content digests, content_entity keys,
  observability rows by (kind, path), every other stable key, and the
  canonical files, entities, and directories. It surfaced the pre-existing
  observability duplicate, fixed in the same branch.
- Live PostgreSQL: AC1 chain activation with no base-missing class and no
  reindex row, the AC2 content_files and completeness check, the burst, the
  older-full fence negative, the drained-base fail-closed, the guard anchor at
  the union base, and retention keeping the union base.

## Performance Evidence:

Benchmark Evidence: the benchmark table at the end of this section compares
the union read and the collector baseline path on the touched code at the
base and at this change.

The collector baseline read is one statement on the standalone path (the
active row on `scope_generations_active_scope_idx` plus one ordered LIMIT 1
read on `scope_generations_scope_latest_lookup_idx`), and zero extra
statements on the sweep path, which reuses the cycle's `FullReconcileState`
(one more projected column on the `projected_full` lateral). The cumulative
diff adds at most one `git diff` (only when the active commit is not the
full's) and one `git ls-tree` per delta sync. The union read keeps the #7797
shape: one target primary-key row (one more column), one base bitmap probe,
one prune probe for a superseded base, and the base fact pages; it never
depends on chain length.

Benchmarks, base `c5f784d73c` against this change, same benchmark code (only
the version adapters differ: the base's two-column target row and the base's
active-commit baseline), run base, head, head, base with `-count=4` each
(n=8), `benchstat`, Apple M1 Max. The host was shared and loaded (load
average 53-62 during the run), so absolute numbers are noisy; the comparisons
are paired.

| benchmark | base | head | delta |
|---|---|---|---|
| `BenchmarkFactStoreLoadProjectionFactsUnion` (200-row delta over a 2,000-file base, fake queryer) | 2.982 ms ± 1% | 2.988 ms ± 2% | ~ (p=0.721) |
| same, B/op and allocs/op | 5.756 MiB, 67.94k | 5.757 MiB, 67.94k | +0.01%, +0.00% |
| `BenchmarkOverlayDeltaBaseUnion` (2,000-file base with observability rows, 200-file delta) | 3.136 ms ± 3% | 2.827 ms ± 1% | -9.83% (p=0.000) |
| same, B/op and allocs/op | 3.412 MiB, 5.711k | 3.372 MiB, 5.911k | -1.19%, +3.50% |
| `BenchmarkUpdateRepositoryChainedDelta` (500-file repo, fetch, decide, diff, checkout) | 89.95 ms ± 2% | 112.07 ms ± 2% | +24.59% (p=0.000) |

The union read does not regress. The overlay gets faster because it now drops
the base's observability rows under touched paths instead of carrying them.
The chained delta sync costs about 22 ms more: the cumulative diff runs one
extra `git diff` and one `git ls-tree` process. That is the designed cost of
Option B, paid per chained delta sync, and it replaces the refused delta plus
forced full (about 470-520 s stored to active on busiest repo B) that the base
path led to. Commands: `go test ./internal/facts ./internal/storage/postgres
-run '^$' -bench 'BenchmarkOverlayDeltaBaseUnion|BenchmarkFactStoreLoadProjectionFactsUnion'
-benchmem -count=4` and `go test ./internal/collector/repo/git -run '^$'
-bench BenchmarkUpdateRepositoryChainedDelta -benchtime=20x -count=4`.

## Observability Evidence:

`eshu_dp_collector_delta_baseline_fallback_total{skip_reason}` gains
`no_projected_full` and `delta_exceeds_base_half`;
`eshu_dp_collector_delta_touched_ratio{outcome}` records every cumulative
diff's touched/base ratio. The `git repository sync completed` log carries
`delta_union_base_commit_sha`, `delta_changed_count`, `delta_deleted_count`,
and `active_is_delta`. The `projector.delta_union` span event and INFO log, and
the `build_projection` stage log, carry `chained`; fence refusal logs, span
events, and `failure_details` carry `delta_union_base_commit_sha`. Are deltas
chaining? `eshu_dp_projector_delta_union_total{outcome="union_base_found"}`
rises while
`eshu_dp_projector_delta_baseline_fence_total{outcome="refused_base_missing"}`
and `eshu_dp_collector_reconciliation_full_snapshots_total{reason="repository_reindex_requested"}`
stay flat.

## After-measurement (hosted QA, 2026-11-02, pinned image)

Deployment pin (a): CONFIRMED. The hosted QA cluster (namespace `eshu`) runs
`ghcr.io/eshu-hq/eshu:sha-a854ed2a42d36f29a22c34295ab382fbaaf41180`
on every workload; `git merge-base --is-ancestor f0b35f531c a854ed2a...`
holds (2,351 commits after the #8892 merge). All pods (re)started
2026-11-02T21:51:12-44Z, so the window below (21:51:44Z to about 22:52Z, roughly
one hour) is entirely post-deploy. Sources are read-only: Prometheus
(`monitoring-kube-prometheus-prometheus`), the ingester `/metrics` endpoint via
`kubectl exec eshu-0`, ingester pod logs, and the QA read replica
(`eshu-postgresql-read-0`, `pg_is_in_recovery() = true`,
`default_transaction_read_only = on`).

Chained deltas project and activate (b); the refused-then-forced-full path is gone:

- 13 delta generations ingested post-deploy across 6 repository scopes, every
  one carrying `delta_union_base_commit_sha`; 12 activated, max `activated_at`
  2026-11-02T22:52:30Z (the 13th was superseded before activation and its retry
  activated).
- The busiest scope (`r_33474efb`, 5 deltas) ran a 4-deep chained sequence
  `fffec9b4 -> 14a7bccd -> 2ae279ff -> e72b923e`, each baselined on the previous
  delta, all activated, no forced full between them. A second scope
  (`r_a26b5a1b`) ran a 3-deep chain `ae8617db -> acc2a3a2 -> d52a915e` on one
  union base (`fee38917`), all activated. Three more chained activations
  (`168e2be9`, `ccb623ae`, `42bdbd6f`) carry `active_is_delta = true` in the
  collector log. This is acceptance criterion 1 running on the pinned image.
- `eshu_dp_projector_delta_baseline_fence_total`: post-deploy `matched = 11`
  (ack), `refused_active_differs = 1` (preflight). No `refused_base_missing`
  series on the pinned pod, and none on any ingester instance in the trailing
  7d window (only `matched` plus `refused_active_differs`).
- `eshu_dp_collector_reconciliation_full_snapshots_total`: post-deploy
  `interval_elapsed = 18` only. Trailing 7d shows `interval_elapsed` plus one
  `never_reconciled` (a first-seen scope), and no
  `repository_reindex_requested` anywhere.
- `fact_work_items` post-deploy: 390 `succeeded`, 0 `dead_letter`, 0 rows with
  a refusal failure class. One `superseded` row with
  `projector_delta_baseline_mismatch` is the benign in-flight case (the active
  moved while that delta was queued; its retry activated) and accounts for the
  single `refused_active_differs`. One `retrying` row is
  `supply_chain_impact_fanout_failed`, an unrelated domain. Two `claimed` rows
  carry `reducer_handler_abandoned_at_shutdown`, restart fallout from the
  21:51Z rollout.

Per cumulative delta (busiest scope `r_33474efb` first, then the other scopes;
`observed_at == ingested_at` on every row, so observed-to-active is the
push-to-visible proxy):

| head | scope | status | chained (baseline is delta) | touched (changed+deleted) | observed to active s | write started to active s |
|---|---|---|---|---|---|---|
| fffec9b4 | r_33474efb | superseded (was active) | yes | not log-detailed | 155 | 83 |
| 14a7bccd | r_33474efb | superseded (was active) | yes | not log-detailed | 472 | 88 |
| 2ae279ff | r_33474efb | superseded (was active) | yes | 79 + 1 = 80 | 107 | 56 |
| e72b923e | r_33474efb | active | yes | 84 + 1 = 85 | 109 | 65 |
| ae8617db | r_a26b5a1b | superseded (was active) | yes | not log-detailed | 6 | 2 |
| acc2a3a2 | r_a26b5a1b | superseded (was active) | yes | not log-detailed | 5 | 1 |
| d52a915e | r_a26b5a1b | active | yes | not log-detailed | 4 | 1 |
| 168e2be9 | r_0a05b11a | active | yes | not log-detailed | 33 | 30 |
| 7ac4bbd9 | r_79581267 | active | no (baseline is full) | not log-detailed | 3 | 1 |
| 37bb6bf0 | r_12780cc2 | superseded (was active) | yes | not log-detailed | 102 | 95 |
| 42bdbd6f | r_12780cc2 | active | yes | not log-detailed | 27 | 13 |
| ccb623ae | r_a09c7db8 | active | yes | not log-detailed | 127 | 96 |

Touched counts come from the `git repository sync completed` log
(`delta_changed_count`, `delta_deleted_count`, both `active_is_delta = true`,
union base `f69b61ac`, a repository on branch `dev` (scope `r_33474efb`):
79+1 and 84+1 files, far below half of any full. Distribution bound over all
cumulative diffs: `eshu_dp_collector_delta_touched_ratio{outcome = delta}` has
12 post-deploy observations with bucket counts `le = 0.25 -> 10` and
`le = 0.5 -> 12`, so the max touched ratio sits in (0.25, 0.5] and the mean is
about 0.08. Only the two table rows above have per-delta log detail; older log
lines for the other ten diffs had aged out of the pod log buffer.

Push-to-visible against the quiet band (63-194 s): completed values are 3, 4,
5, 6, 27, 33, 102, 107, 109, 127, 155, and 472 s. Eleven of twelve sit inside
or below the band. The 472 s outlier is the `14a7bccd` supersede-then-retry
cycle (first attempt superseded by the moved-baseline mismatch, retry queued
about 384 s, projection tail 88 s), not projection cost. Projection tails
(`projection_write_started_at` to `activated_at`) run 1-96 s; the projector
work-item wall averages 39 s with max 116 s (n = 12). A per-delta parse versus
store split is not separable from the available signals (the stage histogram is
not delta-labelled); that granularity stays NOT_CHECKED.

Valve fire count: `eshu_dp_collector_delta_baseline_fallback_total` has no
series on the pinned pod, so zero fires, corroborated by the touched-ratio
buckets (max at most 0.5). Trailing-7d maxima on pre-pin instances
(`delta_exceeds_base_half` 15, `no_projected_baseline` 2) are pre-deploy
behavior outside this window.

Gap (one): `eshu_dp_projector_delta_union_total{outcome = "union_base_found"}`
does not rise because it is never emitted: zero series at the ingester
`/metrics` endpoint (`kubectl -n eshu exec eshu-0 -- sh -c 'wget -qO-
http://127.0.0.1:9464/metrics' | grep "^eshu_dp_projector_delta_union"`
exits 1) and zero samples in Prometheus over 7d, although the counter is
registered in code (`registerProjectorDeltaUnion`,
`go/internal/telemetry/instruments.go`) and 12 union-based deltas activated
through its `recordDeltaUnion` call sites. The DB count above (13 union-based
deltas, 12 activated, max 22:52:30Z) is the corroborating signal; wiring the
instrument on this path (or proving it intentionally nil here) is follow-up
work, not part of this measurement.

Per-scope post-deploy ledger: 38 generations ingested (19 full active, 6 full
superseded by reconcile, 6 delta active, 7 delta superseded after activation or
retry); refused-by-class 0; forced-full 0. Hard bar: every cumulative delta
was faster than the 8-9 minute forced full it replaces on the busiest
repository; the slowest completed delta took 472 s end to end with an 88 s
projection tail.

## Known windows

A delta-active scope exposes only the active delta's search documents, and
`changed-since` answers `baseline_not_comparable` for a delta end (#7282).
Cumulative deltas lengthen the delta-active window from one cycle to the
reconcile interval on churning repositories; tracked in #8801.
