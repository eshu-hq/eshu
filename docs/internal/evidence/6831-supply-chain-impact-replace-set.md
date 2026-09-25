# Supply-Chain Impact Replace-Set Retraction (#6831)

## Defect

`PostgresSupplyChainImpactWriter` upserted each pass's findings for a
`(scope_id, generation_id)` and never retracted the rows a later pass no
longer derived. A finding's `fact_id` embeds its whole logical identity,
including `repository_id`, so a pass that emitted a repo-less finding and a
later pass that anchored the same finding to a repository produced two
different rows, and both stayed active. The query surface
(`ListFindingsQuery`) returned both. The golden-corpus `ignored_hidden`
suppression is scoped to the anchored repository; it hid the anchored row and,
by design, could not hide the repo-less one, so the check failed with
`count == 1` about 3.7% of the time per leg.

Root-Cause Evidence: planting a repo-less copy of the debian-image finding in
the same `(scope, generation)` of a green gate database, re-running the real
reducer for that scope, and querying the real API returned the anchored row
and the untouched planted twin (`count:2`); with the suppression live the
twin alone survived (`count:1`), the CI signature. Which early pass first
wrote the twin in CI is still unproven; the writer defect holds whatever
wrote it.

## Fix

Each pass's finding set is now the complete truth for its
`(scope, generation)`. `WriteSupplyChainImpactFindings` runs one transaction
(`go/internal/reducer/supplychain/core/writer_retract.go`):

1. `SELECT pg_advisory_xact_lock(hashtextextended(<fact_kind> 0x1F <scope> 0x1F <generation>, 0))`,
   taken before any row lock.
2. The existing batched versioned upsert. On conflict it resets
   `is_tombstone`, so a finding a later pass derives again comes back.
3. `UPDATE fact_records SET is_tombstone = TRUE` for this `fact_kind`,
   `scope_id`, and `generation_id` where `fact_id NOT IN (SELECT unnest($keep))`
   and `fencing_token <= $token`. Rows are tombstoned and never deleted.

A pass whose evidence load hit any bound (`SupplyChainImpactWrite.PartialEvidence`)
upserts but does not retract. It has not seen the complete evidence set, so
hiding a row it did not reach would replace a stale row with a missing one. The
bounded stages that feed the flag are: the active-evidence round and per-call
row caps, the scanner-analysis-scope pair cap, the resolved-digest cap, the
peer-identity repository-id cap, and the OS-package advisory target cap. The
OS-package reader orders by fact id with no rotation, so the stage asks for one
target beyond the cap (500) and treats loaded plus skipped rows above the cap
as truncation. The other loads (scope facts, repositories, manifest
dependencies, JVM reachability, Python file facts) page to completion and have
no cap.

The read path already excludes tombstones: `ListFindingsQuery`, the aggregate,
explain, and readiness queries in `go/internal/query/supply/chain/impact`,
and the canonical-winners rebuild
(`supply_chain_impact_canonical_winners_store.go`) all filter
`is_tombstone = FALSE`. The live test asserts the result through
`ListSupplyChainImpactFindings`.

## Concurrency

Conflict domain: the finding rows of one `(fact_kind, scope_id,
generation_id)`. Row ids embed scope and generation, so different domains
never share a row. Transaction scope is one pass's lock, upsert, and retraction.
The retry scope is the reducer work item, and a redelivered pass rewrites the
same ids and retracts nothing. The advisory lock is the only wait two passes
of one domain can form, and it is always taken first, so no lock-order cycle is
possible. It is released at commit or rollback. Evidence loading and finding
construction run outside it. Passes for other domains hash to other keys and
proceed concurrently.

Why the lock is required: under Read Committed a pass's retraction cannot see
rows that a concurrent, uncommitted pass has upserted. Without the lock, two
overlapping passes commit the union of two different sets. Measured with the
retraction in place and the lock removed:

- `TestSupplyChainImpactWriterSerializesOverlappingPassesLive`:
  pass 2 finished while pass 1 was uncommitted.
- `TestSupplyChainImpactWriterConcurrentPassesConvergeLive`: 8 racing passes
  left an active set mixing six passes' rows.

Both pass with the lock. The overlap test also asserts that `pg_locks` shows
exactly one ungranted advisory lock while pass 2 waits, and that a pass for a
different scope completes while pass 1 holds its lock.

Fencing: the retraction applies the insert's `fencing_token <= $token` guard,
so a row stamped by a writer with a higher token is never retracted by a pass
with a lower one (`TestSupplyChainImpactWriterRetractionHonorsBoundaryLive`).
This domain does not issue fencing tokens today (every row is 0). Under the
lock, the last pass to commit therefore wins the whole set. That is the same
ordering the upsert already had, now applied to the set rather than to
individual rows. A stalled worker whose lease expired can still commit its set
after a fresher pass. Closing that needs a database-issued token like
`aws_cloud_runtime_drift`'s admission watermark; that is outside this fix.

## Performance

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` of the production
retraction statement on PostgreSQL 18.6, with the full bootstrap schema (101
`fact_records` indexes), 520,000 `fact_records` rows across 501 scopes, and one
target scope holding 20,000 finding rows:

| Case | Plan | Execution |
| --- | --- | --- |
| steady state, keep all 20,000, 0 retracted | generic | 8.2 ms |
| keep 10,000, 10,000 retracted | generic | 143.8 ms |
| keep 10,000, 10,000 retracted | custom | 187.3 ms |
| scope with no finding rows | generic | 0.17 ms |
| rejected form `fact_id <> ALL($4)`, 10,000 retracted | generic | 1,627.1 ms |
| advisory lock statement | - | 0.007 ms |

All plans use the existing `fact_records_scope_generation_idx (scope_id,
generation_id, fact_kind, observed_at DESC)`, so no new index is needed. The
keep set is checked through a hashed subplan. `fact_id <> ALL($param)` was
rejected because the reducer's pgx connection caches prepared statements, and
under a generic plan that form scans the array linearly for each row (an
earlier, smaller shim measured 2,889 ms against 57 ms). The retracting case
spends most of its time maintaining the table's partial indexes, and it only
happens when a pass supersedes findings. The steady-state pass pays one index
range scan over its own scope's findings.

Per-pass statement count rises from 1 to 3 (lock, upsert chunk(s),
retraction). The committed cost budget
`testdata/cassettes/replayoffline/supply-chain-impact.cost-budget.json` moves
from 1 to 3. The N+1 negative control records 6 and still fails that budget.

## Observability

Observability Evidence: `eshu_dp_supply_chain_impact_findings_retracted_total`
(counter, label `domain` only) counts tombstoned rows per pass. The handler
logs `supply chain impact superseded findings retracted` with `scope_id`,
`generation_id`, `intent_id`, and `findings_retracted` when a pass retracts
anything. A partial-evidence pass retracts nothing and logs nothing here; it
shows as `active_evidence_truncated=true` in the evidence summary and sub-signal. The reducer result carries the
`findings_retracted` sub-signal, and the evidence summary carries
`retracted=N`. Lock waits show up in `pg_locks` as `locktype = 'advisory'`, and
the lock, upsert, and retraction are timed by the existing
`eshu_dp_postgres_query_duration_seconds` (operation=write) through the
instrumented transaction. A sustained retraction rate on a steady corpus means
passes disagree about one generation's evidence.

## Proof

- `go test ./internal/storage/postgres -run SupplyChainImpactWriter` with
  `ESHU_POSTGRES_TEST_DSN`: six live tests.
  - RED before the fix:
    `active finding repositories = ["" "" "repository:r_6831_anchor"]`.
  - GREEN after the fix.
- `go test ./internal/reducer/supplychain/core`: statement order and keep-set
  arguments, empty-pass retraction, `PartialEvidence` skip, rollback on each
  statement's failure, handler counter and sub-signal, and truncation mapping.
