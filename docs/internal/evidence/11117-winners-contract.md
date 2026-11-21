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

## The audit gate (arbiter addendum, 2026-11-21)

QA readings by the coordinator show that the audit has never completed on QA:
no `path="audit"` series appeared in 7 days. Before this change the audit waited
on the per-process `fullDone` flag, and every roll reset it. QA runs
`ESHU_POSTGRES_READ_ROUTING=staged-audits`, so its auditor is the staged
standby scan. The arbiter ruled that A's gate is unprovable on QA today but not
disproven. Skipping the startup full stays correct: convergence after a roll
rests on durable state (marks staged in the writer's transaction, the queues,
the persisted watermark), not on process memory. What A removes is the
accidental repair that every roll's full used to make. Under A only the audit
makes that repair, so A may merge once the writer-side verify is measured at
backlog scale and the audit has operator signals:

- **Verify at backlog scale (item 4).** The live proof
  `TestSupplyChainImpactWinnersResweepAuditVerifyAtBacklogShapeLive`
  (`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_VERIFY_SCALE=qa`; the smoke shape
  runs in the gate) seeds 80,000 stored winners, 50,000 changed candidates (all
  pending), 48,000 log rows, 374,000 staged rows and a whole-generation scope
  mark in each queue. It times `VerifyStagedCandidates` from its lock claim to
  its commit: 14.210 s at load1 35.5 and 9.086 s at load1 28.6 (disposable
  Postgres 18.6). The bound is one keyed interval, 30 s, so the verify stays
  unchunked (item 5a not needed). In the same run a keyed tick during the hold
  returns Skipped, a capture insert completes in 68 to 134 ms, and the keyed
  tick after the commit commits. The margin is 2 to 3x on a corpus with one
  fact per key and small payloads; QA has about 13 kept facts per identifier
  and larger payloads (#11120). An earlier measurement of 1.6 to 5.0 s is void:
  its synthetic facts lacked `subject_digest`, so the keyed probe matched
  nothing. The proof now fails if the probe keeps no facts.
- **Staged-audit telemetry (item 5b).**
  `eshu_dp_supply_chain_impact_winners_staged_audit_deferrals_total{reason}`,
  `..._staged_audit_ticks_total`, and the last tick's `..._last_batches`,
  `..._last_max_batch_seconds`, `..._last_staged_facts` and
  `..._last_distinct_keys`.
- **Audit success (item 5c).**
  `eshu_dp_supply_chain_impact_winners_audit_last_success_age_seconds`, plus
  one `supply chain impact winners audit completed` line for every completed
  pass, zero repairs included.
- **Gate (item 5d).** The audit is gated by the persisted contract only.
  There is no process-local gate and no backlog-depth gate
  (`TestWinnersMaintainerAuditRunsOnAMatchingContractWithoutAFull`).
- **Lease (item 5e).** The 60 s audit lease is not renewed, so a read phase
  longer than 60 s can let a second instance start a pass. The proof
  `TestSupplyChainImpactWinnersResweepAuditOverlappingPassesAreHarmlessLive`
  shows that overlapping passes are harmless. A verify that runs while another
  holds the resweep lock is Skipped. A verify that runs after another has
  committed finds that pass's repairs already pending and queues none. Each
  repair is staged once. The cost is a repeated standby read, not a wrong or
  duplicate write.

AC5 plan on QA:

1. Deploy A and B together and let the NULL-version full commit.
2. Watch the first audit pass. The first pass is part of the proof. Watch
   `resweep_duration_seconds{path="audit"}`,
   `resweep_in_flight_age_seconds{path="audit"}`,
   `audit_last_success_age_seconds`, the staged-audit gauges and deferrals,
   and the completion log's read seconds and repairs.
3. Roll once more with the matching contract. This is the worst case: the
   backlog is present and no full runs.

Roll back if any of these happens:

- an audit is in flight for over 30 min;
- the keyed `last_commit_timestamp_seconds` stalls for over 5 min while an
  audit is in flight;
- `resweep_skipped_total` climbs in that window;
- an audit transaction on the writer is older than 5 min.

The lever is `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL=0`. It turns the
audit off and keeps A. Record it as a temporary state. While the audit is off,
the backstop is the force-full runbook step: NULL both contract columns so the
next tick runs one full.
