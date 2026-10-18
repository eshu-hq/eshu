# #7638 item 3: lease against representative p99 (Refs #7638)

Status: **measured — ADD-KNOBS.** No production code changed in this step.
No `make pre-push`, no push, no PR. Only this file is new.

## Verdict

**ADD-KNOBS.** The targeted activation-obligation pass p99 on representative
facts is **not** far below the 96 s deadline (lease 120 s minus a fifth):

- 54 admitted passes with built binaries on real-fact data: min 16.98 s,
  p50 18.2 s, max 36.38 s, p99 ~36.0 s (linear interpolation; 36.38 s by
  nearest rank). Margin to the 96 s deadline is 2.6x at p99, 5.3x at p50.
- Scale measured at 61 fact-bearing scopes with ~6 retained generations and
  2.08M fact rows. The dominant cost (~9 s, one statement) probes one index
  range per scope over latest-generation facts, so it grows with scope count;
  61 scopes leave ~16x headroom to the 984-scope full corpus before the
  anchor scan alone approaches the deadline. Full-corpus p99 is unmeasured
  and, by that shape, expected above 96 s.
- Zero `maintenance_timeout` at this scale, and every obligation settled, so
  the consumer works — but the margin is too thin to certify.

The required follow-up is the knob set named in the issue
(`ESHU_ACTIVATION_OBLIGATION_LEASE`, `_POLL_INTERVAL`, `_WORKERS` with env
registry rows, docs and a wiring test), recorded below as NOT_IMPLEMENTED
with rationale. Lease sizing alone cannot cover corpus-proportional growth;
the structural fix is the item-5 predicate-pushdown optimization, which stays
with item 5.

## Time boxes (both reported, neither overrun)

- Venue probe, 10-minute box: **passed at ~minute 1.** SSH to the remote
  validation host succeeded; remote Docker 29.9.0 present. No BLOCKED state.
- Main effort, 100-minute box: **used ~27 minutes** (task start 15:55:39Z to
  stack teardown ~16:22Z on 2026-10-17, all UTC). Bound not hit; measurement
  completed with built binaries, so no resume runbook for the measurement —
  the runbook at the end covers replication and the full-corpus extension.

## What bounds the pass (read from source)

Consumer path, in claim order:

- Claim: `claimObligationQuery` in
  `go/internal/storage/postgres/activation/sql.go` — single oldest
  open row, `FOR UPDATE SKIP LOCKED`, row-self predicate repeated for the
  EvalPlanQual recheck. Cheap; never the bound.
- Settle: `settleObligation` in
  `go/internal/reducer/maintenance/obligation/settle.go` runs Finalize,
  then the maintainer only when the exact
  generation's phase is missing, then Finalize again.
- Deadline: `maintenanceContext` in the obligation `settle.go` cancels
  the pass at `leaseUntil - lease/5`. With the fixed default lease
  (`DefaultActivationLease = 2 * time.Minute` in the obligation
  `runner.go`; poll 10 s, workers 1) the pass must finish inside
  **96 s**, else it counts `maintenance_timeout`, holds the lease, and retries
  after expiry — paying corpus cost every 2 minutes without finishing.
- Maintainer: `ActivationMaintainer.MaintainActivation` in
  `go/internal/storage/postgres/ingestion_backfill_seam.go` runs
  exactly one owed `(scope, generation)` through
  `RunDeferredRelationshipMaintenanceForPartitions` in
  `go/internal/storage/postgres/backfill/targeted/pass.go`, never the
  whole pass. That pass is the bound.
- Wiring sets only `Owner` (`activationObligationRunnerFor` in
  `go/cmd/reducer/activation_obligation_wiring.go`); no LEASE/POLL/WORKERS
  env exists. The registry carries only
  `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED` (the reducer-subsystem row
  in `go/internal/envregistry/entries.go`).

## Measurement design

- Metric: admitted-pass wall (`duration_s` on the
  `deferred_backfill_targeted_completed outcome=completed` line the pass
  itself logs in `go/internal/storage/postgres/backfill/targeted/pass.go`),
  p99 vs 96 s. Refusals
  (`no_memo_baseline`, 0.00–0.01 s, no fact work) are not pass samples.
- Corpus: 61 real repositories stratified across the full size distribution
  (every 16th by size from the 984-repo remote workspace, `.git` excluded,
  466 MB working trees with real manifests and code) — representative facts.
  Scope count (61) and retained generations (~6) are below production shape;
  the scope-scaling bridge is measured separately (see Scaling).
- Harness: built binaries only, never `go test`: the remote compose Neo4j
  stack at the base commit (Go tree identical to this worktree's HEAD —
  `git diff e7675d9ee HEAD -- go/` is empty), consumer flag on via the
  item-1 overlay, per-project ports to avoid every other lane. Six quiet-edit
  rounds (one new file per repo per round) produced 356 obligations; 54
  admitted passes ran `MaintainActivation` on real fact volumes. Rounds 1–5
  overlapped (ingest storm); round 6 ran on an idle system as the separator.
- Signals: resolution-engine pass log lines, obligation/queue tables over
  psql, Postgres slow-statement log (5 s, then 1 s) on the measurement
  stack only, `pg_stat_activity` sampling, one read-only `EXPLAIN` shim for
  plan shape. `pg_stat_statements` is unavailable (extension count 0).

## Results (all UTC 2026-10-17, remote linux/amd64, Postgres 18, Neo4j)

Stack up 15:57:49Z, all services healthy. Initial ingest: 62 scopes, 61
obligations held with `no_memo_baseline` (0.00–0.01 s each, expected before
the first whole pass). Whole pass wrote 61 memos; at 16:01:13Z all 61 gen-1
obligations completed via the phase-ready path (no admitted pass — phases
already published). Rounds fired 16:01:21Z (r1), 16:05:24Z (r2), 16:06:59Z
(r3), 16:09:15Z (r4), 16:10:12Z (r5), 16:15:22Z (r6, idle system).

Admitted passes (`outcome=completed`, n = 54), seconds, sorted:

```
16.98 17.07 17.16 17.17 17.26 17.33 17.41 17.43 17.44 17.48 17.54
17.67 17.67 17.72 17.75 17.76 17.79 17.80 17.81 17.86 17.86 17.86
17.90 17.90 18.00 18.00 18.14 18.19 18.19 18.24 18.27 18.29 18.34
18.46 18.51 18.65 18.65 18.69 18.79 18.85 18.99 19.01 19.03 19.08
19.14 19.16 19.20 19.32 19.63 19.91 20.07 20.86 35.71 36.38
```

min 16.98 s, p50 18.2 s, max 36.38 s, **p99 ~36.0 s** (nearest rank 36.38 s).
All 356 obligations reached terminal state (334 completed, 22 obsolete from
superseded generations); zero `maintenance_timeout`; zero finalize step
failures; 59 initial `no_memo_baseline` holds only. The two 35–36 s passes
are closure-promotion outliers (`promoted=1`, `snapshot_conflicts=1`,
`published=0`, owed partition left in retry) from stacked-generation churn
in rounds 3–5 — real behavior under churn, kept in the p99. The 4–6
`dead_letter` rows are projector `graph_write_timeout` on `source_local`
work during the ingest storm, unrelated to the consumer path.

Fact volumes at end: 2,083,688 fact rows over 61 fact-bearing scopes and 367
generations. Anchor-scan surface: `content` 66,465 rows / 180 MB payload,
`file` 66,122 rows / 579 MB payload, `gcp_cloud_relationship` 0 rows.
Remote host load averaged ~4.7–8.9 on 16 CPUs during the run (shared host;
other lanes' Postgres containers were up but the run's own stack did the
measured work). The idle-system round (r6) measured the same 17–20 s band,
so background-ingest contention does not explain the cost.

### Decomposition (the exact slow layers)

Per ~18 s pass, from the 1 s slow-statement log (107 statements, p50 1.6 s,
p90 8.9 s) and the full slow texts:

1. **Anchor-scoped relationship fact load, ~8.2–10.2 s, once per pass.**
   `LoadAnchorScopedRelationshipFacts` in
   `go/internal/storage/postgres/backfill/scoped_facts.go`: a
   corpus-wide scan joining `fact_records` to the shared
   `latest_generations` DISTINCT ON CTE with eight
   `lower(payload::text) LIKE '%…%'` patterns. No payload/trigram index
   exists (confirmed in `pg_indexes`: only kind-specific lookups). The
   `EXPLAIN` shows one index probe per scope (62 loops, ~305 rows each,
   ~19k rows, ~71k buffers, mostly TOAST payload fetches) plus a final
   sort — i.e. cost scales with scope count times latest-generation fact
   volume, not with the owed partition.
2. **Owed-partition fact load, up to ~5.1 s.** `SELECT … payload FROM
   fact_records WHERE scope_id = $1 AND generation_id = $2 ORDER BY
   observed_at, fact_id`: full-payload TOAST fetch for one generation
   (thousands of real facts). Scales with per-generation fact volume.
3. **Remainder ~4 s**: classification, catalog scan, stale-memo check,
   memo-gated load, batch write (`evidence_facts=0` here), reopen listings,
   fan-in publish, and Go-side processing.

### Scaling (why 2.6x margin at 61 scopes fails the gate)

- The anchor load's loop count equals the scope count (62 loops measured).
  Fact rows grew 1.2M → 1.8M across rounds with pass durations flat,
  consistent with scope-dominated, not row-dominated, cost at fixed scope
  count. 61 → 984 scopes is ~16x the loops: the anchor scan alone projects
  to well above the 96 s deadline at full corpus, before the owed-load and
  remainder are added. Representative retained depth (25 vs ~6 here) adds to
  the DISTINCT ON sorts and reopen listings on top.
- The step-3 tiny-facts fixture measured 0.21–0.30 s because its payloads
  are tiny and few; real payloads (759 MB across 132k anchor-kind rows here)
  change the cost by ~60–100x. Tiny-facts numbers do not transfer.

## Knob verdict and NOT_IMPLEMENTED sketch

The p99 is not far below 96 s and scales against the deadline, so the
issue's second branch applies: add the three knobs. **NOT_IMPLEMENTED** —
this step is measurement-only, and defaults must not move until a
full-corpus p99 sets the LEASE value; prescribing it from 61 scopes would
be a guess. Rationale recorded so the implementing lane starts here:

- `ESHU_ACTIVATION_OBLIGATION_LEASE` (duration, default `2m`): read in
  `activationObligationRunnerFor` into `Config.Lease`. Must exceed
  full-corpus pass p99 plus headroom; the 61-scope p99 (36 s) already
  consumes a third of the current 120 s.
- `ESHU_ACTIVATION_OBLIGATION_POLL_INTERVAL` (duration, default `10s`)
  into `Config.PollInterval`; `ESHU_ACTIVATION_OBLIGATION_WORKERS` (int,
  default `1`) into `Config.Workers`. No evidence demands changing their
  defaults; expose them so operators can trade drain speed against DB load.
- Registry rows in `go/internal/envregistry/entries.go` (subsystem
  `reducer`), docs in `docs/public/reference/env-registry.md` and
  `docs/public/deployment/service-runtimes.md`, wiring test next to the
  existing `go/internal/reducer/activation_obligation_wiring_test.go`:
  getenv-override table (valid durations/int, invalid/empty falling back to
  defaults, flag off still yielding a nil runner).
- Plain warning for the implementing lane: a bigger lease only buys margin
  while the anchor scan stays corpus-wide. If full-corpus p99 lands in the
  hundreds of seconds, no sane lease covers it and the item-5
  predicate-pushdown becomes the prerequisite, not an optimization.

## Knob implementation (feat/7638-obligation-proof-p1)

Status: **IMPLEMENTED** (Refs #7638; packets 3-12 untouched). The sketch above
is now code, with two deviations: the getenv-override table lives in
`go/cmd/reducer/activation_obligation_wiring_test.go` — the direct sibling of
`activationObligationRunnerFor` — not next to the `go/internal/reducer`
Service-wiring test; and the owning package exports
`DefaultActivationLease`/`DefaultActivationPollInterval`/
`DefaultActivationWorkers` so the wiring defaults cannot drift from
`withDefaults()` (single source of truth). No default value changed, so the
worker-default rule needs no new headroom evidence.

No-Regression Evidence: baseline is `Config{Owner}` plus `withDefaults()` at
`Run` (2m/10s/1). After the change the wired `Config` carries the same values
when the env is unset or unparsable, proven by
`TestActivationObligationConsumerKnobsFlowIntoConfig` (5/5 subtests green)
beside the still-green disabled, maintainer-wiring, non-transactional, and
service-composition cases. No SQL, lock, claim, or IO path changed, and
`withDefaults()` still applies at `Run`, so a set knob and an unset knob
reach the identical effective config as before. Backend/version: none
touched — pure env parsing, no Postgres or Neo4j involved. Input shape:
duration/int env table covering valid values plus empty, whitespace, zero,
negative, and garbage values, all falling back without failing startup
(`loadDurationOrDefault`/`loadPositiveIntOrDefault` soft-fallback, matching
the other reducer maintenance knobs). Queue/row counts: none — no queue or
table touched. Defaults are unchanged per the verdict above; full-corpus p99
remains NOT_RUN, so no tuned LEASE value is prescribed.

No-Observability-Change: no new spans, metrics, logs, or gauges; the
consumer's instruments and failure classes (`recordOutcome`, `recordFailure`,
`recordHeld`) are untouched.

## Exact commands run (exit codes)

Connection values lived in shell vars per the remote-validation skill and
are not repeated here. Worktree
`…/eshu-wt/7638-obligation-proof`, branch `feat/7638-obligation-proof-p1`,
verified before every write. Base commit `e7675d9ee`; Go tree identical
(`git diff e7675d9ee HEAD -- go/` empty, exit 0).

- Worktree/branch/prior-evidence checks: exit 0 throughout.
- `gh issue view 7638 --comments` / `--json title,body`: exit 0 (item-3
  text and item-1 run report quoted from the 2026-10-17 comments).
- Venue probe (SSH date/uname/go version): exit 0 at ~minute 1.
- Remote inventory (run dir, workspace count 984, Docker 29.9.0, images
  cached, ports, checkout `e7675d9ee`, corpus layout/sizes): exit 0.
- Corpus build (every-16th-by-size pick, 61 repos, rsync without `.git`,
  466 MB): exit 0.
- Compose `up -d ingester resolution-engine projector eshu mcp-server`
  with the item-1 overlay and per-project ports: first attempt failed on
  the overlay path (file lives in the run dir, not the checkout); second
  attempt healthy (exit 0). No `--build`: images reused from the identical
  base.
- Polling (psql queue/obligation/memo/counts, `docker logs` rg extracts,
  `pg_stat_activity` sampling, `ALTER SYSTEM` slow-log 5 s → 1 s +
  `pg_reload_conf`): exit 0. Two benign nonzero rg exits (zero-match
  whole-pass count early; zero `maintenance_timeout` count at the end).
- Read-only `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)` of the anchor query
  (2-pattern variant): exit 0.
- Six quiet-edit rounds (one new file per repo per round): exit 0.
- Teardown `down -v` for this project only: exit 0; residual 0 containers
  and 0 volumes; other lanes' containers untouched and listed present.

## Reproduce / extend (runbook, no embedded connection values)

Resolve `SSH_TARGET`/`SSH_OPTS` from the user-local remote-validation
skill into shell vars. Work under a fresh run directory basename of your
choosing; reuse of the item-1 checkout directory at `e7675d9ee` is valid
only while the Go tree still matches it (`git diff` check above).

1. Build the corpus: sort the full-corpus workspace by `du -s`, take every
   16th entry (~61 repos spanning the size mix), rsync with
   `--exclude=.git` into a fresh corpus dir.
2. Launch from the checkout with a unique `COMPOSE_PROJECT_NAME`, the
   consumer-flag overlay, `ESHU_FILESYSTEM_HOST_ROOT` pointing at the
   corpus dir, and Postgres/Neo4j/API/MCP/metrics ports set off every
   occupied port (`ss -ltn` first).
3. Converge: poll `fact_work_items` by status and `activation_obligations`
   by state until the initial held wave completes after the first whole
   pass writes memos (initial `no_memo_baseline` refusals are expected).
4. Fire a round: write one new file per repo; wait for all obligations of
   the new generations to reach terminal state; harvest
   `deferred_backfill_targeted_completed outcome=completed … duration_s=`
   lines from the resolution-engine log.
5. For the full-corpus extension of this verdict, repeat with the whole
   workspace as the corpus and ≥100 admitted passes before sizing
   `ESHU_ACTIVATION_OBLIGATION_LEASE`; keep the slow-statement log on to
   confirm whether the anchor scan still dominates.
6. Teardown with project-scoped `down -v` only; verify zero residual
   containers/volumes for the project and that other projects still run.
   Never broad-prune.

## Limits and NOT_CHECKED

- Scope count 61 and retained depth ~6 are below production shape (984, 25);
  the full-corpus p99 that must size the LEASE knob is NOT_RUN — the
  projection argument above is not a substitute for it.
- Single-worker consumer only; multi-worker contention behavior not probed.
- Graph/API truth of the woken edges not asserted (item 1's province).
- The two 35–36 s samples are retained-generation-churn outliers from
  deliberately stacked rounds; their production frequency is unmeasured.
- The 4–6 projector `graph_write_timeout` dead letters were not diagnosed
  (outside this item's metric; flagged for the owning lane).

## Interim default: 30m (arbiter-ruled, feat/7638-obligation-proof-p2)

Status: **IMPLEMENTED interim** (Refs #7638). `DefaultActivationLease` moves
2m → 30m with no other behavior change: the knob stays user-tunable with soft
fallback, the consumer stays off by default, and the lease-minus-a-fifth
deadline rule (`maintenanceContext`: cancel at `leaseUntil - lease/5`) is
unchanged, so the effective pass deadline moves 96 s → 24 minutes.

Item-2 full-corpus (984-scope) measurement behind the ruling: passes up to
395 s; at the 2m default, 8 `maintenance_timeout` with 0 completions; at 30m,
3/3 completions.

Revisit trigger: lower the default back only when a rewrite (item-5
predicate-pushdown or equivalent) re-measures the pass p99 far below the
deadline on the full corpus, with the same admitted-pass metric and corpus
shape recorded here.
