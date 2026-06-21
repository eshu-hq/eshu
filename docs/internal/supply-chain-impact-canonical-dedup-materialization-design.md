# Supply-chain impact finding — maintained canonical dedup materialization

Status: proposed (design / plan)
Issue: follow-up to #3389 (supply-chain read endpoints at scale), endpoint 2
Owner surfaces: `go/internal/reducer`, `go/internal/query`,
`go/internal/storage/postgres`, `schema/data-plane/postgres`

## 1. Problem (measured)

`GET /api/v0/supply-chain/impact/findings` deduplicates at read time:
`listSupplyChainImpactFindingsQuery` runs
`ROW_NUMBER() OVER (PARTITION BY canonical_key ORDER BY priority_score DESC,
has_payload_finding_id DESC, fact_id ASC)` over the full filtered set of active
`reducer_supply_chain_impact_finding` facts, then keeps `canonical_rank = 1`.

`canonical_key` is the `CONCAT_WS` of 11 identity fields. The same real finding
(CVE × package × repo × subject) is emitted once per detecting scope/source, so
the read must collapse duplicates to "one canonical finding, represented by its
highest-priority instance."

Measured on an isolated Postgres 18 seed (500,000 active impact facts across 901
active scopes, `work_mem=16MB`, with #3402's `*_active_scan_idx` present), broad
filter `impact_status=affected_exact` (~125k matches), no-filter first page:

- Dedup `WindowAgg` sorts 125k rows, `external merge Disk: ~98MB`, ~1.9s.
- Cost is `O(filtered active facts)` and the spill grows with corpus size and
  filter breadth. This is the last remaining unbounded read in #3389.

### Why no read-only / index-only fix exists (four measured dead-ends)

1. Keep `payload` out of the dedup window, re-fetch by `fact_id` for the page:
   no improvement — `payload` is TOAST-pointered, rows only shrank 468→420B; the
   spill is the `canonical_key` sort, not the payload. Slightly slower.
2. Btree expression index on the `CONCAT_WS` key: cannot be created —
   `CONCAT_WS` is `STABLE`, Postgres rejects non-`IMMUTABLE` index expressions.
3. Stored `canonical_key` payload field + partial index + the existing
   active-generation JOIN: planner refuses the index-ordered scan (98MB spill
   persists). The filter (`impact_status`/…) and the dedup key cannot share one
   index ordering, so scanning all 500k in key order then filtering costs more
   than scan-the-subset + sort.
4. Same stored key + index, active check rewritten as an order-preserving
   `EXISTS`: planner still refuses it (118MB spill, slower).

Conclusion: the dedup sort is intrinsic to doing FILTER + cross-scope DEDUP in
one read. The only way to remove it is to **stop deduplicating at read time** —
precompute the canonical winner so the read filters instead of windows.

## 2. Goal and invariants

Goal: a maintained, cross-scope materialization that records, per
`canonical_key`, the single winning active fact, so the list read becomes a
bounded `O(page)` keyset read over winners (no window, no sort).

Invariants (must hold after every materialization cycle and every read):

- **I1 (one winner):** at most one materialized winner per `canonical_key` over
  the set of currently-active facts (active generation + `is_tombstone = FALSE`).
- **I2 (correct winner):** the winner is the exact row read-time dedup would
  pick — `ORDER BY priority_score DESC, has_payload_finding_id DESC,
  fact_id ASC` — byte-identical tiebreak.
- **I3 (no ghosts):** when the current winner stops being active (superseded
  generation or tombstone), the winner is re-picked from remaining active facts,
  or removed if none remain. No tombstoned/superseded fact is ever served.
- **I4 (truthful freshness):** a read served from a stale/building winner set
  reports `truth.freshness.state != fresh` with a cause, never silently returns
  stale truth as fresh.
- **I5 (output parity):** the materialized read returns byte-identical rows
  (finding_id, source_confidence, payload) and ordering to the current read for
  every filter/sort/cursor combination, for the same DB snapshot.

Accuracy is gated before performance: a faster read that can serve a stale,
duplicated, or wrong winner is a failure (I1–I5 first).

## 3. Architecture (mirror existing machinery, do not invent)

Reuse the shared-projection runner the reducer already runs:

- `reducer.SharedProjectionRunner` (`shared_projection_runner.go`): a dedicated,
  long-lived goroutine alongside the claim/execute/ack loop, polling
  shared-projection domains, partitioned, lease-coordinated.
- `PartitionLeaseManager` (`shared_projection_worker.go`): heartbeat TTL lease;
  exactly one worker owns a partition at a time.
- Winner upsert mirrors `canonicalReducerFactInsertQuery` style
  (`workload_identity_writer.go`) — `ON CONFLICT … DO UPDATE`.

New pieces:

- **New shared-projection domain** `supply_chain_impact_canonical` added to the
  runner's domain set.
- **New materialized table** `supply_chain_impact_canonical_winners`:
  `canonical_key TEXT PRIMARY KEY, winner_fact_id TEXT NOT NULL,
  finding_id TEXT NOT NULL, priority_score INT NOT NULL,
  winner_scope_id TEXT, winner_generation_id TEXT,
  source_count INT NOT NULL, materialized_at TIMESTAMPTZ NOT NULL`.
  (Winner identity + the tiebreak inputs + provenance count. Payload is NOT
  copied — the read joins `winner_fact_id` → `fact_records` by PK, so payload
  truth has one home and cannot drift.)
- **Read rewrite:** `listSupplyChainImpactFindingsQuery` selects from
  `supply_chain_impact_canonical_winners` joined to `fact_records` by
  `winner_fact_id` (PK), applies the existing filters + keyset cursor + LIMIT,
  no `ROW_NUMBER`.

### Conflict domain and recompute model

- **Conflict domain: `canonical_key`.** Partition the materialization by
  `hash(canonical_key) % N`. One lease-holder per partition ⇒ no two workers
  touch the same `canonical_key` concurrently.
- **Recompute, do not incrementally promote.** Per dirty `canonical_key`, the
  worker re-runs the exact dedup over *current active* facts for that key and
  upserts the winner (or deletes the row if no active facts remain). Recompute
  is idempotent and inherently handles supersession/tombstone/new-source — there
  is no fragile "promote the next one" state machine (I3 by construction).
- **Dirty-set discovery.** A `canonical_key` is dirty when any of its facts are
  written, tombstoned, or have their generation activated/superseded. Source the
  dirty set from the existing fact-change signal (`fact_work_items` /
  reducer intent enqueue) keyed/derived to `canonical_key`; the worker drains
  dirty keys in bounded batches. (Detailed wiring is Phase 1 research output —
  see §8.)

### Transaction vs retry scope (concurrency-deadlock-rigor)

- **Transaction scope:** one `canonical_key` recompute = read active facts for
  the key + upsert/delete the winner row, in one short transaction. Bounded to
  one key's facts (small), never a global sort.
- **Retry scope:** the partition batch. A retried batch re-recomputes keys
  idempotently; re-running a key that already converged is a no-op upsert.
- **Idempotency key:** `canonical_key` (table PK). Duplicate delivery of a dirty
  key converges to the same winner.
- **Ordering / lease:** partition lease gives single-writer-per-key; `ON
  CONFLICT (canonical_key)` is the backstop if lease boundaries ever overlap
  during failover.

### Bad interleavings considered

- *Two workers, same key:* prevented by partition lease; `ON CONFLICT` backstop.
- *Winner tombstoned between materialize and read:* read joins
  `winner_fact_id → fact_records` and re-applies `is_tombstone = FALSE` +
  active-generation guard, so a just-invalidated winner is filtered at read time
  and the key is re-materialized on its dirty signal (no ghost served — I3).
- *New higher-priority source arrives:* marks the key dirty → recompute upserts
  the new winner. Until then the read still returns the previous valid winner
  (correct, just not yet the newest) and freshness reflects backlog (I4).
- *Generation flip mid-cycle:* recompute reads current active set; if it flips
  again, the key is dirtied again and reconverges (eventually-consistent, always
  from a real active snapshot).

## 4. Truth / freshness semantics (eshu-correlation-truth)

- The materialized read sets `truth.freshness.state`:
  - `fresh` when the canonical materialization backlog for the relevant scopes
    is drained,
  - `building`/`stale` with cause `reducer_backlog` (existing cause vocabulary,
    `freshness_causality.go`) when dirty keys are pending.
- `truth.level`/`truth.basis` stay `semantic_facts`/derived as today — the
  winner is still reducer-derived; we are not inventing new truth, only
  precomputing the selection the read already made.
- Proof matrix (must all pass): positive (a real finding materializes one
  winner), negative (a fully-tombstoned key has no winner row and is absent from
  reads), ambiguous (two sources same key → exactly one winner, the correct
  tiebreak), DB proof (winners table inspected directly), query proof (read
  output byte-identical to the legacy `ROW_NUMBER` read on the same snapshot).

## 5. Read rewrite (shape)

```
SELECT w.finding_id, refetch.source_confidence, refetch.payload
FROM supply_chain_impact_canonical_winners AS w
JOIN fact_records AS refetch
  ON refetch.fact_id = w.winner_fact_id
 AND refetch.is_tombstone = FALSE
JOIN ingestion_scopes AS scope
  ON scope.scope_id = refetch.scope_id
 AND scope.active_generation_id = refetch.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = refetch.scope_id
 AND generation.generation_id = refetch.generation_id
 AND generation.status = 'active'
WHERE <existing payload filters on refetch.payload>
  AND <existing keyset cursor on (priority_score, finding_id)>
ORDER BY <existing sort> LIMIT $N
```

- Filters still run on `refetch.payload` (same predicates, same results).
- Keyset cursor uses `w.priority_score` / `w.finding_id` (indexed on the winners
  table) → bounded `O(page)`; no window, no sort spill.
- The active-generation re-guard on the join enforces I3 even if the winner row
  briefly lags a tombstone.

## 6. Schema, indexes, backfill

- Migration adds `supply_chain_impact_canonical_winners` + indexes:
  `(priority_score DESC, finding_id)` for the default+priority keyset,
  `(finding_id)` for the finding_id-sort keyset.
- Backfill: one-time materialization of all existing canonical keys (bounded,
  partitioned, same recompute path) so the table is correct before the read is
  switched. Read switch is gated on backfill completion (capability/feature flag
  or readiness phase), with the legacy `ROW_NUMBER` read as fallback until then.

## 7. Observability (concurrency-deadlock-rigor requires it)

- Metrics/spans: dirty-key backlog depth, recompute duration per key,
  winners upserted/deleted per cycle, partition lease held/lost, conflict-domain
  (`canonical_key`) hotness, recompute failure class.
- The read keeps its existing `postgres.query` span + duration histogram; add
  `truth.freshness` cause emission so an operator sees stale-winner reads.
- Evidence markers (`Performance Evidence:` / `No-Regression Evidence:` +
  `Observability Evidence:`) per `scripts/verify-performance-evidence.sh` on
  every PR that touches the reducer/queue/materialization path.

## 8. Phased delivery (each phase its own measured PR)

1. **Phase 0 — dirty-key wiring research + ADR sign-off.** Confirm exact
   fact-change → `canonical_key` dirty signal (reuse `fact_work_items` / intent
   enqueue) and the runner domain registration points. Output: this doc
   finalized + the dirty-set mechanism named with file refs.
2. **Phase 1 — schema + winners table + backfill writer (no read change).**
   Reducer-side recompute path + partition worker + backfill. Reducer tests:
   positive/negative/ambiguous winner selection; replay/retry matrix
   (duplicate, stale replay, concurrent same-key, drained queue). Winners table
   verified to match the legacy `ROW_NUMBER` result on the seed.
3. **Phase 2 — read switch behind readiness gate.** Rewrite the list read to use
   winners; keep legacy read as fallback until backfill-complete. Byte-identical
   parity tests (full + filter/sort/cursor matrix) vs legacy on one snapshot;
   at-scale `EXPLAIN (ANALYZE, BUFFERS)` showing no sort/spill, `O(page)`.
4. **Phase 3 — freshness + observability + remove legacy read.** Freshness
   cause wiring, dashboards/metrics, delete the `ROW_NUMBER` path once parity +
   freshness are proven.

Phases 1–2 are independently revertable; the read only flips after the winners
table is proven correct, so accuracy is never at risk during rollout.

## 9. Expected outcome

- Read: `O(filtered set)` + 98MB-growing spill → `O(page)` keyset, low-ms,
  bounded regardless of corpus/filter breadth.
- Dedup becomes first-class, testable materialized truth with provenance
  (`source_count`), reusable by any surface that needs "the canonical finding."
- Read-side `work_mem`/temp-I/O pressure under concurrent dashboard load is
  removed.

## 10. Risks / open questions (stated plainly)

- Dirty-key signal precision: must capture generation-activation flips, not just
  fact writes, or winners go stale silently (mitigated by the read's
  active-generation re-guard + freshness cause, but backlog must drain).
- Backfill cost at 4.5M+ facts: bounded/partitioned, but must be measured.
- Added maintained state + write amplification on the materialization path
  (acceptable: moved off the per-read hot path; must show net win with load
  evidence, not just single-query timing).
- Cross-scope partition skew: a hot `canonical_key` partition; mitigate with
  partition count tuning, measured.
