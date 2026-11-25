# #10880 PR-B3a: the supports write-on-change guard ignores the per-pass payload keys

Branch `drive/10880-prc-b3a`, stacked on PR-C1 (`790fcfa968`). Ruling: `.claude/arbiter-b3-2026-11-24.md` (option O2 alone).
Lab evidence: `.claude/b3-prep-census.md`, `.claude/b3-prep-options.md`.

## What changes

The supports upsert (`supply_chain_impact_supports.go`) writes a conflicting row only when the new row differs from the stored one
(`WHERE ... IS DISTINCT FROM` over the guard columns and the payload). Every reducer pass stamps its own `intent_id` and `cause` into the
finding payload, so an identical replay differed from every stored row in those two keys and rewrote all of them: in the B3 lab 10,000 of
10,000 supports rows, 21.3 WAL records per row.

Now both sides of the payload compare drop the two keys (`payload - 'intent_id' - 'cause'`). The stored payload, the derived columns and the
statement's other clauses are unchanged. A replay that changes nothing else rewrites no row (lab: 0 of 10,000, 1.09 WAL records per row, the
lock record `ON CONFLICT DO UPDATE` takes on the conflicting row). A real change still writes, and a changed row is stored verbatim, including
the new pass's keys.

The change is a re-pin of the supports contract version 1, not a bump: the statement differs from the C1 pin only by the two-key strip on both
sides, so no support row changes and no backfill is owed (`supplyChainImpactSupportsContractPins`; the C1 pin stays as
`supportsContractVersion1C1Pin` for the differential).

## What an operator reads in the stored keys

(Also in the public supports doc, `docs/public/reference/telemetry/supply-chain-impact-supports.md`, "What the stored `intent_id` and `cause` mean".)

After this change the stored `intent_id` and `cause` of a support name the pass that last **changed** the row, not the pass that last
confirmed it. The pass that last confirmed it is the work item: the reducer records the intent's id as the work item id
(`storage/postgres/supply_chain_impact_fanout_inflight.go:60-66`), and the time is `last_pass_at` on the scope's pass-state row. The API and
MCP never serve either key (the served row has no payload, intent or cause field).

## Trace owed by ruling 4.6: the security-alert reconciliation reads neither key

The supports page hands the whole stored payload to its consumers as an envelope
(`storage/postgres/supply_chain_impact_supports_page.go:301-315`, `facts.Envelope{... Payload: payload}`). The security-alert reconciliation
consumes finding envelopes in `reducer/securityalert/reconciliation.go:208-227` (`extractSecurityAlertImpacts`) and reads exactly six
payload keys: `repository_id`, `package_id`, `cve_id`, `advisory_id`, `impact_status`, `observed_version`. It reads neither `intent_id`
nor `cause`. The audit oracle and the Ifá digest already strip both keys (arbiter 4.1 to 4.3).

## The hermetic differential and the proof wording

`supply_chain_impact_supports_guard_test.go` renders the shipped statement and the C1 statement and asserts they differ only by the two-key
strip on both sides of the payload compare; `TestSupportsGuardDifferentialRejectsOtherEdits` seeds a one-sided strip and a third stripped key and
requires the differential to fail. `TestStableFactKeyDoesNotChangeTheVersion1Statements` keeps its precedent role with the base render taken from
the unguarded builder.

The supports mirror proof now reads: **byte-identical for rows the pass inserted or changed; equal apart from `intent_id` and `cause`
otherwise** (`supportsLiveAssertMirror`, and the change matrix below). C2's claim takes the same wording.

## Claims table

Role for every live row: the reducer-contention runner against a disposable Postgres, run as the owning (superuser) role of the lab
container; N is the row count the test writes.

| Claim | Test | Seeded-violation test | Role | N |
|---|---|---|---|---|
| The statement differs from the C1 pin only by the two-key strip | `TestSupportsGuardDiffersFromC1OnlyByTheTwoKeyStrip`, `TestSupportsGuardDoesNotChangeTheVersion1Statements` (the unguarded render hashes to the C1 pin) | `TestSupportsGuardDifferentialRejectsOtherEdits` (a one-sided strip, a third key) | hermetic | rendered SQL |
| Identical replay rewrites 0 supports | `TestSupplyChainImpactSupportsGuardChangeMatrixLive` (`identical`) | the C1 statement rewrites all (`TestSupplyChainImpactSupportsGuardTransitionLive`, the old-statement leg) | disposable Postgres, owner | 10,000 |
| Content change in 100 findings rewrites exactly 100; evidence-only change exactly 100; same evidence in another order 0; identical again 0 | `TestSupplyChainImpactSupportsGuardChangeMatrixLive` | the C1 statement rewrites every row on each pass | same | 10,000 |
| A changed row is byte-equal to its fact; an unchanged row equals it apart from the two keys | `TestSupplyChainImpactSupportsGuardChangeMatrixLive` (after `content-change`), `supportsLiveAssertMirror` | a stripped content key fails the compare | same | 10,000 |
| First pass after the code change over C1-written supports rewrites 0 | `TestSupplyChainImpactSupportsGuardTransitionLive` | the C1 statement over the same rows rewrites all | same | 10,000 |
| An old and a new writer alternating on one pair converge; the new one never rewrites an unchanged row | `TestSupplyChainImpactSupportsGuardMixedVersionLive` | the old statement in the loop rewrites every row each round | same | 2,000 |
| The live proofs run in the reducer-contention gate | `TestSupplyChainImpactLiveProofsRunInTheReducerContentionGate`, `scripts/lib/reducer_contention_inventory.json`, `specs/live-tests.d/live-tests-07.yaml` | an unenrolled live file fails the ledger and the enrollment test | hermetic | files |

## Live results

Disposable PostgreSQL in the b3-prep lab container, 2026-11-24 18:02Z to 18:03Z, one run of
`go test -run 'TestSupplyChainImpactSupportsGuard|TestSupplyChainImpactSupportsFollowTheWriterLive|TestSupplyChainImpactSupportsSurviveRacingWritersLive|TestSupplyChainImpactLiveProofsRunInTheReducerContentionGate' ./internal/storage/postgres/`:
all PASS, `ok ... 36.734s`. Change matrix 21.0 s (first write 10,000 new, identical replay 0, content change 100, evidence-only 100,
reorder 0, identical again 0), transition 9.5 s, mixed version 3.6 s, follow-the-writer 0.21 s (mirror loosened to the two-key strip),
racing writers 0.78 s, enrollment guard 1.4 s. Static checks on this branch: `golangci-lint run ./internal/storage/postgres/` 0 issues,
`git diff --check` clean, `verify-live-tests-ledger.sh` ok (1349 rows), `verify-license-header.sh` ok.

## No new metric

The existing supports rows counter already has an `unchanged` action; with the guard ignoring the two keys, replays move rows from `updated` to
`unchanged`. No instrument, span or log is added: `No-Observability-Change:` (the counter is the operator signal, and the QA confirmation
below reads it).

## The lock record on a suppressed row (accepted, ruling 2)

`ON CONFLICT DO UPDATE` locks the conflicting row before the guard runs. Lab, steady state: 10,000 lock records of 54 bytes and no page images.
On QA the first touch of each heap page after a checkpoint logs the page; W2 judges it against the rule that supports WAL after must be below
the supports plus retraction plus fact-insert WAL before, in both units. If it is not net negative the ruling returns to the arbiter. A narrow
digest column and a pre-filter that never sends unchanged ids to the upsert are not part of B3a.

## HOT updates: unresolved

The lab measured 0% HOT on supports updates in all 13 configurations, including a plain update of the fill-only columns. The cause is not
established. Three candidates are untested: the varlena representation of the stored `payload`, the `ecosystem` expression, and the smallint
cast on a derived column. Under O2 the only rows still rewritten are real changes, which change the payload (three indexes reference it), so
they cannot be HOT whatever the cause; the key fill does not depend on it (a plain update of one unindexed column was HOT-eligible: 500 of 501
at fillfactor 50). Recorded as a performance finding only; no bisect in this PR (ruling 3).

## QA confirmation after the roll (owed, not run here)

Per upsert call, before and after the roll: rows updated, rows unchanged and WAL bytes. If supports WAL per row offered falls by less than half,
return to the arbiter: it would mean QA replays differ in another payload key.

## For #10860 only (no action here)

The fact upsert is guarded by the token alone (`factwrite/batch_insert_versioned.go:145`), so every pass rewrites every fact row, 18.8 WAL
records each across 154 indexes. C2 removes that for findings; the other reducer writers keep it.

## Not checked

The 2 KB per suppressed row on QA; that `intent_id` and `cause` are the only differing keys on QA replays; the Ifá materialized-edges decode of
the finding; every lab number is the B3 report's, not recomputed here.
