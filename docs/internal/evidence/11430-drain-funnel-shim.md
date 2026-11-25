# #11430 drain-funnel shim: flat split — admission is 98% open for supply_chain_impact

Date: 2026-11-25. Lane: drive/11430-drain-bottleneck (leaf child).
Tree: branch `drive/11430-drain-bottleneck` (ahead of origin/main; no production code touched).
Prior: arbiter verdicts confirm claim-starvation at pick time (246 last_attempt_at
touches/hr, ~831 never-attempted of ~8353 pending, attempt_count 0 throughout);
suspects in order (a) stale-generation fence, (b) conflict-representative fence
(same_rn / live_fence_keys), (c) projector_ok; readiness exonerated for
supply_chain_impact. This shim tests exactly that ordering on the QA replica.

## Verdict

FLAT SPLIT. No bucket reaches the ~50% conviction threshold — the largest,
projector_drain, is 1.8% of domain candidates. All three suspects are exonerated
*as admission fences* for supply_chain_impact at QA-observed shape. Per the
ONLY-then rule there is no convicted bucket, so no second Prove-The-Theory-First
shim was built. The starvation is not in admission: 8668 of 8827 domain
candidates (98.2%) are claimable representatives right now, yet 828 rows were
never attempted and 94.5% sit at attempt_count 0. Drain-rate / pick-order
theories (FIFO behind a slow tick, batch-limit trim, worker domain sharding)
are NOT_CHECKED by this lane and belong to the coordinator.

## Recipe (no hand-copied SQL)

Derived at runtime from the shipped Go constants via a throwaway in-package
test injected with `go -overlay` (file lives in /tmp only, never in the tree):

- `reducerClaimBatchCandidateChainCTEs()` verbatim in both the shipped claim
  and `reducerClaimBatchFunnelQuery` (guarded), and `reducerClaimStaleGateCTEs()`
  verbatim in the funnel (guarded).
- Funnel file: `reducerClaimBatchFunnelQuery` verbatim, placeholders
  substituted literally ($1 clock_timestamp(), $2 ARRAY['supply_chain_impact'],
  $3 '11430-funnel', $4 clock_timestamp()+1h, $5 TRUE, $6 0, $7 0).
- Pre-base file: funnel text truncated at the stable marker
  `base AS MATERIALIZED (`, cut back to the `-- base: every shared gate`
  comment block, trailing `,` trimmed (byte-prefix of the funnel, guarded),
  plus a counting tail over the shared CTEs (`stale_reducer_candidates`,
  `readiness_missing`, `projector_live_scopes`) and `fact_work_items`.
- Global funnel: mechanical `ARRAY['supply_chain_impact']` → `NULL`
  substitution of the derived funnel file (structure untouched).
- Domain/attempt censuses are plain GROUP BY / FILTER queries (no chain
  replay), recorded below for share-of-backlog context.

## Commands and observed values (read-only vs QA, `sh /tmp/qa-read.sh <file>`)

1. `go test ./internal/storage/postgres -run Test11430DeriveFunnelShim
   -count=1 -overlay /tmp/overlay-11430.json` (from `go/`) → PASS,
   wrote `/tmp/11430_funnel_domain.sql` (23117 B) and `/tmp/11430_prebase.sql`
   (16436 B). Placeholder scan: no `$N` remains. Guards (chain/stale
   verbatim, byte-prefix, marker present) all green.
2. Per-domain funnel (`$2 = supply_chain_impact`, `$5 = TRUE`):

   | candidates | not_ready | not_representative | projector_drain | semantic_cap | pre_fence | representatives |
   |---|---|---|---|---|---|---|
   | 8827 | 0 | 0 | 159 | 0 | 8668 | 8668 |

   Reconciles: 0+0+159+0+8668 = 8827; live-lease fence = 8668−8668 = 0.
3. Pre-base splits (same `$2`):

   | split | value | share of universe (8834) |
   |---|---|---|
   | universe | 8834 | — |
   | stale_fenced | 0 | 0% |
   | not_yet_visible | 0 | 0% |
   | live_leased | 8 | 0.09% |
   | in_base_recomputed | 8826 | 99.9% (≈ funnel candidates 8827, ±1 live race) |
   | distinct_keys_universe | 8834 | 100% distinct — no key collapse |
   | distinct_keys_in_base | 8826 | 100% distinct |
   | gen_is_active / gen_not_active / gen_scope_missing | 8834 / 0 / 0 | 100% on active generation |
   | readiness_missing_domain | 0 | 0% (exonerated, as ruled) |
   | projector_live_scope_count (global) | 1022 | context for the 159 drained |
   | stale_off_active_check | 0 | consistency check |

4. Global funnel (`$2 = NULL`):

   | candidates | not_ready | not_representative | projector_drain | semantic_cap | pre_fence | representatives |
   |---|---|---|---|---|---|---|
   | 9900 | 52 | 5 | 159 | 0 | 9684 | 9684 |

   Reconciles: 52+5+159+0+9684 = 9900; live fence 0. supply_chain_impact is
   8827/9900 (89%) of global candidates — it dominates, not starved by peers.
   The 52 not_ready / 5 not_representative live entirely outside supply_chain.
5. Domain census (universe by domain): supply_chain_impact 8833 (9 leased),
   advisory_group_projection 1108 (7 leased), all other domains ≤19 rows.
6. Attempt census (supply_chain_impact): universe 8833, attempt_count=0 in
   8345 (94.5%), last_attempt_at NULL in 828 (≈ arbiter's 831), inflight 9.

## Suspect disposition

- (a) stale-generation fence: 0 fenced of 8834. EXONERATED.
- (b) conflict-representative fence: not_representative 0, live fence 0,
  keys 100% distinct in universe and in base. EXONERATED.
- (c) projector_ok: 159/8827 = 1.8%. EXONERATED as the starvation bucket
  (real but two orders of magnitude below the bar).
- readiness: 0 missing for the domain. EXONERATED (confirms ruling).

## Not covered

- No `$5 = FALSE` cell was run: projector_drain at 1.8% cannot clear 50%
  even if the gate-off cell zeroes it; the cell is NOT_CHECKED, not needed.
- No second shim: no convicted bucket exists, so the ONLY-then gate stops
  this lane here. Concurrency proof (contention, EvalPlanQual, lease safety)
  was not built for any bucket.
- Small live races between statements (±1 row across runs minutes apart) are
  expected on QA and noted, not investigated.
- Pick-order / limit-trim / handler-drain causation for the 828
  never-attempted rows is NOT_CHECKED; the numbers above are the coordinator's
  input, not a root cause.
