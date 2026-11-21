# #11117 A reducer roll with a matching winners contract runs no full resweep

Refs #11117. Binding arbiter ruling: `.claude/arbiter-11117.md` (2026-11-21).
This note covers PR 2 of the ruling (A, acceptance criteria AC2 and AC4). PR 1
(the drain) is in [11117-winners-full-drain.md](11117-winners-full-drain.md).

## What changed

- Migration `329_supply_chain_impact_winners_contract.sql` adds
  `contract_version INTEGER` and `contract_hash TEXT` to
  `supply_chain_impact_winners_materialization`. Both are nullable with no
  default, so the migration changes metadata only. It does not backfill a
  version.
- `SupplyChainImpactWinnersContractVersion` (1) and the hash of the rendered
  rebuild statement plus every keyed build and apply statement. A hermetic test
  pins the hash to the version. Changing the text of a covered statement fails
  that test until the author bumps the version or re-pins the hash with a
  differential proof. A seeded-violation test changes each covered statement
  in turn and requires the pin to fail; it requires the clean set to pass.
- Both full resweeps record the binary's contract in the transaction that
  commits them. Keyed ticks and their idle stamps never touch the columns. The
  keyed tick's over-cap fallback rebuild does not write them either: it runs
  only when the stored contract already matches.
- The maintainer's per-process `fullDone` flag is gone. Every tick reads the
  stored contract:
  - no row, or a NULL or lower version: the guarded full runs. Every instance
    tries it, and the lease plus the resweep advisory lock let one commit.
  - equal version: a keyed tick runs, and the audit runs on its cadence.
  - higher version: the binary stands down and runs no full, keyed tick or
    audit.
- The guarded full re-reads the stored version in its own transaction after it
  takes the resweep lock. Under READ COMMITTED that read sees a full that
  another instance committed while this one waited. So a decision read that
  went stale between the tick's read and the lock never runs a second full.
- A full that is skipped because another instance holds the lock no longer
  makes this instance retry its own full. The next tick re-reads the row, sees
  the other instance's commit, and runs keyed.

## Proofs

- Core (hermetic): `TestWinnersMaintainerMatchingContractRunsKeyedWithoutAFull`
  (the first tick is keyed, zero full calls),
  `...SkippedFullsThenMatchingContractRunsNoOwnFull` (4 skipped attempts, then
  the other instance's commit, then no full of its own),
  `...LowerContractRunsOneFull`, and `...HigherContractStandsDown` (no full,
  keyed tick or audit, and the stand-down counter increments).
- Store (hermetic): `TestResweepWinnersUnlessCurrentSkipsACurrentContract`
  (equal and higher versions stop after the claim and the contract read; no
  row, NULL and lower versions run the full and stamp once), plus the pin pair.
- Live Postgres 18.6:
  `TestSupplyChainImpactWinnersResweepContractRunsOneFullAcrossMaintainersLive`
  runs two maintainers on one database with the real lease store. It covers no
  watermark row, a NULL version and a lower version, and in each case exactly
  one full commits across both. The stored contract then equals the binary's,
  both maintainers run keyed ticks, and the table matches a fresh recompute. At
  RED, two fulls committed.

## Concurrency

Conflict domain: the winners singleton row and the winners table, under the
existing single-owner partition lease (`supply_chain_impact_winners`, one
partition, TTL 60 s, interval 30 s) and the resweep advisory lock. No lock,
lease or order is added. The decision read is one singleton `SELECT` before
the lease claim. The guarded re-read runs after the advisory lock, inside the
full's transaction. The worker count, batch size and lease settings are
unchanged.

No-Regression Evidence: each maintainer tick adds one primary-key read of the
singleton row, and each audit interval adds one more. With two instances at
30 s that is about 4 reads a minute against a one-row table. A full adds one
singleton read and one singleton `UPDATE` inside a transaction that runs for
6,388 to 6,635 s on QA. Not measured beyond the focused proofs above: the reads
are primary-key lookups of one row.

Observability Evidence: AC4 signals, registered in
`instruments_supply_chain_impact_winners.go`:
`eshu_dp_supply_chain_impact_winners_resweep_in_flight_age_seconds{path}`,
`eshu_dp_supply_chain_impact_winners_last_commit_timestamp_seconds{path}`,
`eshu_dp_supply_chain_impact_winners_contract_version{kind}`,
`eshu_dp_supply_chain_impact_winners_full_resweeps_total{reason}` (reasons
`no_watermark`, `contract_version`, `keyed_cap`) and
`eshu_dp_supply_chain_impact_winners_stand_downs_total`. Two logs go with them:
`supply chain impact winners maintainer standing down` (warn, once per entry)
and `supply chain impact winners full resweep recorded the contract` (info).

## Rollout and rollback

PR 1 and PR 2 ship in one release. The first PR 2 binary finds a NULL version
and runs one drained full across all pods. Every later roll with the same
contract runs none. Rollback: older binaries ignore the columns. A rollback to
a binary that predates this change, or across a version bump, must NULL both
columns as a runbook step. Otherwise a newer binary rolled forward later would
trust a version that the older binary's writes no longer match.

Bump rule (merge-order ruling, `.claude/arbiter-11113-deploy.md`): bump the
version only when the rendered winners SQL changes, or when impact facts or
scope activation are rewritten outside the capture owners. #11113 and #11103
change neither and need no bump. This change merges last in its release, so
it pins the hash on the final SQL.

## Theory gate for A (not yet closed)

The ruling makes "skip the full, keep the audit" sufficient only if the audit
completes on QA. That needs QA reads this lane did not take. The read list is
in the lane handoff (NOT_CHECKED here).
