# #10868 — separate reducer deployment for vulnerability work: Phase 0 scoping evidence

Status: Phase 0 code-evidence complete. Measurements (Phases 1–3) and the
deployment-doc decision (box 5) are pending; they need a QA-shaped snapshot
and built-binary A/B/C runs (method below). No production code changed.

## Box 2 answer: cleanup and readiness scoping

The issue's theory holds only if a domain-limited claim also skips other
domains in cleanup and ranking. Verified by reading (worktree at `origin/main`
=b9267c2336=):

| Surface | Scoped by claim domains? | Evidence |
| --- | --- | --- |
| Batch-claim admission (`base`) | Yes — `AND ($2::text[] IS NULL OR domain = ANY($2::text[]))` | `[batch admission predicate](https://github.com/eshu-hq/eshu/blob/b9267c233673b08bb6a064450477cabd5b55de37/go/internal/storage/postgres/reducer_queue_batch_query.go#L143)` |
| Single-claim admission | Yes — same predicate | `[single-claim admission predicate](https://github.com/eshu-hq/eshu/blob/b9267c233673b08bb6a064450477cabd5b55de37/go/internal/storage/postgres/reducer_queue_claim_query.go#L42)` |
| Representative ranking | Transitively yes — `reps_ranked`/`reps` derive `FROM base` | `reducer_queue_batch_query.go:166-192` |
| Stale-generation fence in claim | Yes — `stale.domain = ANY($2)` | `[stale-generation fence](https://github.com/eshu-hq/eshu/blob/b9267c233673b08bb6a064450477cabd5b55de37/go/internal/storage/postgres/reducer_claim_stale_gate_sql.go#L80)` |
| Standalone stale sweep | Yes — passes `q.claimDomainFilters()` as `$2`; shared (unfiltered) sweeper queues sweep all; `SKIP LOCKED` keeps overlap safe | `[standalone stale sweep](https://github.com/eshu-hq/eshu/blob/b9267c233673b08bb6a064450477cabd5b55de37/go/internal/storage/postgres/reducer_stale_generation_sweep.go#L61)` (lines 61-68 and 102), helpers `reducer_queue_helpers.go:46-66` |
| Readiness gate | Per-row, not claim-filter limited — and that is sufficient: the gate keys on the row's own domain (`reducerClaimReadinessGateSQL`, `reducer_queue_readiness_sql.go:300-307`), and `supply_chain_impact` has no row in the requirements `VALUES` list (`:273-297`), so it passes vacuously. Gating is per-row, but cost is not free: the [readiness-missing precompute](https://github.com/eshu-hq/eshu/blob/b9267c233673b08bb6a064450477cabd5b55de37/go/internal/storage/postgres/reducer_queue_batch_claim_gates.go#L98) scans all reducer pending/retrying/claimed/running rows with no domain predicate, once per claim — sizing that shared cost is Phase 1 shim work. | `reducer_queue_readiness_sql.go:273-307` |
| Live-lease fence (`live_fence_keys`) | NO — unfiltered; see verdict below | `reducer_queue_batch_claim_gates.go:166-174` |

Plumbing: `ESHU_REDUCER_CLAIM_DOMAINS` plural allowlist
(`go/cmd/reducer/config.go:176-213`; empty = nil = all domains; both
envs set = startup error), wired through
`configureReducerQueue` (`go/cmd/reducer/main_helpers.go:65-117`,
`ClaimDomains` at `:100`) to `$2` in every query above.

## Fence verdict (decided in #10935 — intentionally global)

`reducerClaimLiveFenceKeysCTE` scans all live-leased rows
(`status IN ('claimed','running') AND claim_until > $1`) with no `$2`
domain predicate. Decided in #10935 as intentionally global: the fence
keys on conflict identity rather than domain, so a live holder in one
deployment must fence the same key in another; scoping it would surface
as whole-batch aborts the lock recheck cannot prevent. Effect under a
split is conservative (never admits a conflicting row), costing only
some isolation benefit, never correctness — and the set covers live
leases only, not the waiting backlog. Pinned by
`TestClaimBatchLiveFenceKeysStayGlobalAcrossDomains` in
`reducer_queue_claim_domains_test.go` (PR #10936). Measurement arms run
under the global fence.

History: at writing time this was a new finding filed separately per
the arbiter ruling (the follow-up became #10935); the section above
records the decision that closed it.

## Split proposal (pending QA census confirmation)

- V (vulnerability) = `supply_chain_impact`, `advisory_group_projection`
  (measured 5,891 + 1,707 = 7,598 of 8,713 waiting rows in the #10862
  replay mix, `reducer_queue_10862_replay_test.go:33-68`; plus
  `security_alert_reconciliation` pending census — 1 row in the mix).
  REJECTED: G unfiltered (nil = all domains, so G would race V).
- G (everything else) = explicit complement: `AllDomains()` minus V,
  generated at run time (`[domain registry](https://github.com/eshu-hq/eshu/blob/b9267c233673b08bb6a064450477cabd5b55de37/go/internal/reducer/domain.go#L30)`), frozen
  into both deployment values before any measured run.
- Source->domain mapping must come from the census, not from code:
  the claim filter is on `domain` while the issue counts
  `payload->>'source_system'` (`reducer_queue_batch_query.go:69,107`).
  Code binds `vulnerability_intelligence` kinds to `supplychain/core`
  (`fact_bindings.go:57-63`), but which domains carry that source in
  the queue is data.

## Pre-registered adopt/reject thresholds (arbiter-set)

Adopt the split iff on the built binary: (a) cheap git domains
(`submodule_pin` + the issue's git kinds) p95 queue wait falls >= 50%
vs Arm A; (b) `supply_chain_impact` throughput/wait regresses <= 10%;
(c) non-vuln claim `EXPLAIN ANALYZE BUFFERS` shared pages read falls
materially (report exact); (d) Arm C extra connections fit the stated
Postgres budget with headroom. Any miss = record rejection with
evidence. Time-box: 1 working day for Phases 0–2.

## Method for the pending phases (coordinator)

- Census (closes Phase 0): read-only on QA —
  `SELECT domain, status, count(*) FROM fact_work_items WHERE stage='reducer'
  AND (payload->>'source_system')='vulnerability_intelligence'
  GROUP BY domain, status;` plus the git-source counterpart. Freezes V/G.
- Phase 1 shim (cheapest, first): rolled-back `EXPLAIN (ANALYZE,
  BUFFERS)` of the full batch + single claim text x {unfiltered,
  V-only, G-only} on the #10862 replay seed
  (`seedReducerQueue10862Replay`, disposable Docker Postgres 18-alpine,
  2 warmups + 7 interleaved reps, median ms + top-node buffers —
  same method as #10863). Stop here if pages do not drop.
- Phase 2: built-binary A/B/C competing-workers runs on identical
  reset snapshots (same W and pool partitioned in B, additive in C);
  primary metric `eshu_dp_reducer_queue_wait_seconds{domain}`
  (`service_observability.go:49-51`), p50/p95 per domain group.
- Phase 3: `pg_stat_activity` per deployment process + `pg_stat_statements`
  DB-time delta per arm (pool lives outside `configureReducerQueue`).
- Phase 4: decision in `docs/public/services/resolution-engine.md`
  (+ claim-latency-gate reference); comment remaining work on #10860
  (feeds F3 #10882).

## Acceptance-box status

- [x] Box 2 (cleanup/readiness scoping): answered above + fence follow-up.
- [ ] Box 1 (plans/pages per deployment): pending Phase 1 shim.
- [ ] Box 3 (git wait with/without): pending Phase 2.
- [ ] Box 4 (connections/DB time): pending Phase 3.
- [ ] Box 5 (decision in deployment docs): pending numbers.
