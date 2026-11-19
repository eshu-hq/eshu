# #10873: skip saving facts that did not change

Issue #10873 (part of #10860): on QA, `fact_records` holds 83.7M rows and took
88.3M updates with ~0 done in place. Saving an already-stored fact rewrote its
row every time: both timestamps were stamped from the incoming copy and both
are indexed. This note records the guard, the reader census the issue asked
for, and what it was proved against.

Rule: phase 1, storage, step 1. The change waited for the arbiter's ruling on
timestamps; the ruling's full text is the parent task's prior result. Its
load-bearing decisions: a skipped save keeps the old timestamps with no
fact-level last-seen elsewhere; the guard sits on `upsertFactBatchSuffix`
only, with the fencing token inside the compared tuple; unchanged-skipped
envelopes stay admitted for derived data via a race-free same-transaction
discriminator; measurement must attribute the 88M updates to writer paths
first.

## Change

`upsertFactBatchSuffix` (`go/internal/storage/postgres/facts_streaming.go`)
keeps the #4444 predicate verbatim and first, and ANDs one NULL-safe leg per
SET column except the two timestamps, payload last:

```sql
WHERE fact_records.fencing_token <= EXCLUDED.fencing_token
  AND (
    fact_records.fact_kind IS DISTINCT FROM EXCLUDED.fact_kind
    OR ... -- every SET column except observed_at/ingested_at, token included
    OR fact_records.payload IS DISTINCT FROM EXCLUDED.payload
  )
```

`fencing_token` is inside the tuple: a higher-token same-content write still
lands to advance the stored token, otherwise a later intermediate-token
different-content writer would overwrite a fact it should lose to.
`is_tombstone` is inside it: both flip directions always write. Timestamps are
out: they move on every re-observation, so comparing them would stop the guard
from ever skipping. A skipped row keeps its old timestamps and its old xmin.

`selectUnchangedFactIDs` (`go/internal/storage/postgres/unchanged_guard.go`)
resolves the new RETURNING ambiguity on the same transaction: rows absent from
RETURNING are re-read by fact_id and admitted when the stored guarded tuple
(including token) already equals the incoming envelope; mismatch or a deleted
row stays excluded (the conservative direction — derivation is a pure function
of envelope content the winning batch carries itself). Writer and discriminator
share `guardedFactValuesFor` normalization so they cannot drift.

Scope is `upsertFactBatchSuffix` only (both statement shapes flow through the
one constant). Not `SingleInsertQuery` (no fencing WHERE, different columns),
not the factwrite batch/versioned paths, not search-document or reference-key
upserts. Streaming producers were censused: the suffix has two production call
sites (`ingestion.go` commit with afterBatch, `vulnerability_suppression_store.go`
with nil afterBatch), both generation-checked; git and AWS collectors mint
generation-scoped fact_ids, and generation-free ids exist only on the
unchanged factwrite paths.

No index or DDL change. No new metric: the skip share is reported below, and
per-kind production telemetry stays out until a production-shaped share is
claimed.

## Reader census: what each timestamp reader needs when a save is skipped

`observed_at` and `ingested_at` on a fact row are both stamped from
`Envelope.ObservedAt` (`facts_streaming.go`); `ingested_at` is not an
independent clock. Semantics after this change: last-changed-in-generation,
consistent with generation chronology (#6738: a generation ID fixes its
original chronology) and the #9694 doctrine (active facts keep the
`observed_at` of the last real change; freshness is compensated from source
fetch states).

- Ordering and keyset loaders (`facts.go` list query, `ingestion_queries.go`
  latest-relationship load, backfill catalog/generation-guard/deferred/argocd
  loads, secrets/IAM trust-chain keyset, container-image identity pages) need
  a stable within-snapshot order. Kept-old values preserve it.
- Since-filters (`observed_at >= T`: documentation read model twice, semantic
  evidence model, workitem query) see last-changed time. A same-generation
  replay no longer re-enters the window; accepted per #9694.
- `MAX(observed_at)`/`MAX(ingested_at)` freshness (collector evidence summary
  per-scope aggregate, supply-chain readiness `latest_observed_at` family,
  identity epoch probe, advisory display fields) becomes last-changed and stays
  compensated by generation/source-state freshness (`FreshenFamiliesFromFetch`;
  the evidence-summary maintainer's 60s cadence against a 24h stale bound).
- Alerts tie-break (`observed_at DESC, ingested_at DESC` under content/rank
  keys) stays deterministic; further ties break on `fact_id`.
- Evidence-display `observed_at` fields become first-stored/last-changed;
  informational only.
- Changed-since never reads fact timestamps: classification digests stored
  payloads per generation, and baseline resolution uses generation
  `observed_at` only. Tombstone flips write, so retired/revived classification
  is intact.
- Identity epoch count/MAX/fingerprint stays stable on replay, which is
  correct for an unchanged set; the fingerprint covers generation membership.
- Repository freshness reads `scope_generations`, not fact records: unaffected.
- Repository catalog freshness keys use the in-memory envelope time, not the
  stored row, so replayed generations still advance the cache key — one more
  reason skipped envelopes stay admitted to afterBatch.
- No non-test wall-clock staleness predicate over fact records exists
  (`observed_at < now() - interval`): the one hit is a test fixture, and the
  projector timestamp shifts are test fixtures too.

## Proof

Live tests run against PostgreSQL 18.6 on a throwaway local instance
(`TestUpsertFactBatchSkipsUnchangedRow` first failed on the unguarded code:
equal replays moved xmin on every leg). All in `go/internal/storage/postgres`:

- `unchanged_skip_proof_live_test.go`: skip keeps xmin and both timestamps
  with the fact_id still accepted; per-column change matrix (all 12 guarded
  columns, NULL transitions both directions) writes on exactly one flip and
  skips on the equal replay.
- `unchanged_skip_fencing_live_test.go`: fencing matrix (higher-token
  same-content advances; lower-token same- or different-content fenced and
  excluded; equal-token overwrite preserved), tombstone flips both directions,
  all-skipped streaming replay reaches afterBatch whole (the
  advisory-completeness seam), two-session EvalPlanQual interleave (blocked
  identical replay unblocks, skips, admits, keeps the winner's row), and
  pg_stat counters on a 2,000-row corpus.
- `unchanged_skip_bench_live_test.go`: before/after on the same
  1,000-row corpus and the changed-batch cost bench.
- `unchanged_guard_lockstep_test.go`: hermetic SET/guard/INSERT
  lockstep with seeded RED/GREEN (missing leg, compared timestamp, reworded
  fencing predicate, duplicated SET column); the pre-existing #4444
  cross-batch proof passes unmodified.

No-Regression Evidence: 500-row fully changed batches, payload flipped every
iteration under both shapes, 10 iterations x2: guarded 8.4–9.2 ms/op,
unguarded 8.6–9.0 ms/op — overlapping ranges, no measurable compare cost.
Same-corpus replay: unguarded shape `n_tup_upd` +1000 with `n_tup_hot_upd` 0
(every rewrite a new heap copy, matching the issue's ~0-in-place production
profile); guarded shape +0/+0. 2,000-row guarded replay +0; fully changed
batch +2000.

Observability Evidence: no new metric, span, or log. The guard is a pure SQL
predicate change on the existing upsert path already covered by
`eshu_dp_postgres_query_duration_seconds`; skip vs write is visible in the row
shape (xmin, timestamps) these tests assert directly.

Performance-measurement note: on the local PG18 instance, cumulative-stats
visibility lagged burst-DDL test sessions by tens of seconds (rows committed
and cross-connection visible, stats still zero; short-lived sessions read
immediately). Stats assertions therefore poll with a deadline
(`waitForStatCounters`); production before/after reads need the same settling.

## Not done, and what remains open

- No fact-level last-seen column or table: it would reintroduce the per-replay
  write being removed; last-seen lives at generation/source-state grain.
- No skip on fencing-token mismatch, no volatile-payload-key stripping, no
  factwrite/`SingleInsert` widening, no pre-read write classification.
- QA writer attribution, read-only part measured: via the QA read
  replica (SELECT-only; standby `pg_stat_*` counters
  read zero because standbys do not track primary writes, and
  `pg_stat_statements` there is standby-local, so neither attributes primary
  writers). Cheap reader-safe observations: `fact_records` carries 152 indexes
  at 426 GB total; active-row composition from the 17,330-row
  `collector_evidence_summary` is git 7.13M, vulnerability_intelligence 6.58M,
  aws 45K, documentation 7K, remainder <4K each. NOT_CHECKED: the per-writer
  statement share of the 88.3M updates and the token-equal unchanged share per
  collector — that needs primary `pg_stat_statements`-by-shape (statement
  shapes: `upsertFactBatchSuffix` INSERT ... ON CONFLICT vs factwrite
  batch/versioned vs `SingleInsertQuery`) or pg_stat deltas plus local replay
  from a host that can reach the primary. Any material factwrite share becomes
  a sibling in-ledger issue Part of #10860, never silent widening.
  Cross-generation identical facts are INSERTs this guard cannot save, so the
  measured skip share bounds the win.
