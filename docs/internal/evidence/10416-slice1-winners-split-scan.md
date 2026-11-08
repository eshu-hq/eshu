# Slice 1: winners-audit split scan (#10416)

The winners audit's slow candidate scan now rides the stale reader while
verify, repair queueing, and commit stay on the writer transaction unchanged.
This note records the no-regression and observability evidence for the split.

## What moved and what did not

Moved to the stale reader: one pure single-statement SELECT
(`selectWinnersAuditCandidatesSQL`), the legacy candidate body without its
`CREATE TEMP TABLE AS`. Everything after the scan runs on the writer in one
transaction with the same statements as the legacy path: ident seed (now
batched `INSERT ... unnest` instead of CTAS), `ANALYZE`, the unchanged resweep
lock claim, keyed verification, pending check, repair queueing, and commit.

Not moved: findings/grouped reroutes, F1/F2, claim/verify/INSERT, the lock,
any candidate-count cap. Default routing (`off`) wires no stale source, so the
legacy path runs byte-identically (pinned by
`TestWinnersAuditLegacyStatementsUnchanged` and the three pre-existing audit
live tests).

## Proof

No-Regression Evidence (#10416 slice 1): the split names the same candidate
set as the legacy path and queues the same repairs. `go test
./internal/storage/postgres/ -run
'TestSupplyChainImpactWinnersResweepAudit|TestSupplyChainImpactWinnersResweepStaleScan|TestSupplyChainImpactWinnersResweepStaleDeferral'
-count=1` with `ESHU_POSTGRES_TEST_DSN` (PostgreSQL 18.6) plus the disposable
pair: 10/10 pass, and the 7 split-scan proofs also pass with `-race`. The stale positive case mirrors the legacy missed-capture
scenario exactly (1 repair queued once with source `audit`, then keyed repair
and parity, then a clean second audit); changed-before-lock and already-pending
candidates queue nothing; zero candidates opens no writer transaction
(`begins == 0` asserted); a refused stale read defers with no writer contact
and no insert. Batching: 1001 distinct stale candidates seed in 2 INSERTs
(batch 1000 + 1) and queue all 1001 with source `audit`. Candidate SELECT
shape on settled fixture data (`EXPLAIN (ANALYZE, BUFFERS)` in
`TestSupplyChainImpactWinnersResweepStaleScanCandidateExplainLive`): hash full
join of the winner recompute against the stored winners on `canonical_key`
with the `IS DISTINCT FROM` row filter, actual time 0.19 ms, 27 shared-buffer
hits. The resweep lock claim is unchanged (same advisory xact lock); the seed
lands before the claim so the lock is held only for verification, and the stale
scan itself takes no lock. No speedup is claimed at fixture scale: the win is
writer-load shedding under a real standby, which gates enabling, not landing.

No-Observability-Change (#10416 slice 1): no new metric. A refused stale read
is already counted at the access layer by family (`audits`) through
`ObserveStaleDeferral`; the audit span gains a `deferred` attribute and one
info log line, and a deferred tick records no duration, no repairs, no
capture, and no skip. Duration, repairs, capture-by-source, skipped, dirty-log
gauges, and the stale-key warn log keep their exact meanings, pinned by the
existing maintainer signal tests.
