# #11117 A reducer roll with a matching winners contract runs no full resweep

Refs #11117. Binding arbiter rulings:
[11117-winners-resweep-ruling-2026-11-21.md](../arbiter/11117-winners-resweep-ruling-2026-11-21.md)
and its audit-gate addendum
[11117-winners-audit-gate-ruling-2026-11-21.md](../arbiter/11117-winners-audit-gate-ruling-2026-11-21.md)
(2026-11-21).
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
  The hermetic `TestResweepWinnersUnlessCurrentSkipsACurrentContract` is the
  guard for this re-read; the live two-maintainer proof below does not
  exercise the stale-read interleaving.
- The keyed tick re-reads the stored version under the lock too (review F1).
  Above this binary's version it consumes, applies and stamps nothing and
  reports a stand-down, so neither a keyed apply nor the over-cap full fallback
  runs an older binary's statements over a table stamped with a newer
  contract. `TestResweepWinnersKeyedStandsDownUnderTheLock` (disabling the
  re-read turns it red) and `TestWinnersMaintainerCountsAnUnderLockStandDown`
  pin it.
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

Rollout (reasoned, not run): migration 329's `ALTER TABLE` takes ACCESS
EXCLUSIVE on `supply_chain_impact_winners_materialization`. A pre-A full in
flight holds ROW EXCLUSIVE on that table from its rebuild statement to its
commit (about 6,400 s), so the schema-bootstrap hook (lock_timeout 5 s, 3 min
of retries) fails behind it. Deploy with no full in flight, or cancel the full
first; a cancel is accuracy-safe (one transaction, rolled back).

PR 1 and PR 2 ship in one release. The first PR 2 binary finds a NULL version
and runs one drained full across all pods. Every later roll with the same
contract runs none. Rollback: older binaries ignore the columns. A rollback to
a binary that predates this change, or across a version bump, must NULL both
columns as a runbook step. Otherwise a newer binary rolled forward later would
trust a version that the older binary's writes no longer match.

Bump rule (merge-order ruling,
[11113-deploy-ruling-2026-11-21.md](../arbiter/11113-deploy-ruling-2026-11-21.md)): bump the
version only when the rendered winners SQL changes, or when impact facts or
scope activation are rewritten outside the capture owners. #11113 and #11103
change neither and need no bump. This change merges last in its release, so
it pins the hash on the final SQL.

Known limit: `cmd/ifa`'s `rebuildWinnersOnce` runs the unguarded
`ResweepWinners`, which stamps its own contract unconditionally. An ifa build
older than the stored contract therefore lowers the stored version. It heals
itself: the next maintainer tick sees a lower version and runs one extra full.

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
makes that repair, and on QA the audit does not run (see the staged reader's
caps below): A does not make the audit run on QA. The arbiter ruled that A
ships as is, because the startup full is a measured harm on every roll and the
missed-mark harm has no observed instance. A may merge once the writer-side
verify is measured at backlog scale and the audit has operator signals:

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
  tick after the commit commits. An independent review rerun read 28.640 s at
  load1 17 on the shared Postgres, so the hold ranges from 9.1 to 14.2 s on
  uncontended Postgres (those runs were at load1 28 to 35; what differed was
  Postgres contention, not host load) up to 28.6 s when Postgres is contended,
  against the 30 s bound. The corpus has one fact per key and small payloads; QA has about 13
  kept facts per identifier and larger payloads (#11120). An earlier measurement of 1.6 to 5.0 s is void:
  its synthetic facts lacked `subject_digest`, so the keyed probe matched
  nothing. The proof now fails if the probe keeps no facts.
- **Verify at the #11103 rename wave (arbiter ruling on #11103, section 3).**
  The same proof with `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_VERIFY_SCALE=wave`
  (200,000 stored winners, 176,000 pending candidates, the same queues and
  scope marks) holds the lock 27.606 s at load1 28.5 and 22.853 s at load1
  21.0. Both are under the 30 s bound, with little margin. During the hold a
  keyed tick is Skipped and a capture insert completes in about 3 ms.
- **The staged reader's caps count the corpus, not the backlog.**
  `StagedAuditMaxStagedFacts` (1,000,000) and `StagedAuditMaxDistinctKeys`
  (250,000) bound the whole read phase, which stages every active scope
  generation's facts. Past either cap the tick defers on `budget` before the
  writer verify runs, at any backlog
  (`TestStagedAuditTickCapsDeferPastTheCorpusBudget`: at the cap the tick
  verifies, one past it defers). Read-only QA measurement on the replica
  (2026-11-21 11:01Z) shows the cap is passed. This is measured as a lower
  bound over the cap, not inferred:
  - 11,619 active census pairs hold 8,274,197 active finding facts. That is
    the most one staged tick could stage.
  - The exact grouped count with the shipped joins timed out at 900 s, so the
    reading subtracts the source-candidate exclusions from that total. It
    takes off 15,987 facts with a classification that is not static and
    active, 0 facts with a scanner association, and 0 facts in
    `image://derived/%` scopes (QA has none).
  - The staged source facts are therefore at least 8,258,210, more than eight
    times the 1,000,000 cap. The operator arm adds at most one scope and
    cannot lower that total.
  - Stored winners number 79,614, so the 250,000 distinct-key cap is not the
    binding one; the fact cap is.

  Every staged audit tick on QA defers on budget, and the writer verify never
  runs.
- **Budget ruling.** The arbiter ruled that A ships as is, with this
  amendment and #11131
  ([11117-staged-audit-budget-ruling-2026-11-21.md](../arbiter/11117-staged-audit-budget-ruling-2026-11-21.md)).
- **No automatic backstop on QA until #11131 ships.** At `staged-audits`
  routing past the caps, QA has no automatic backstop for a missed mark until
  #11131 ships. After A, the only backstop on QA is the manual force-full
  runbook step (NULL both contract columns so the next tick runs one full of
  about 6,400 s). Each use is a QA data mutation and needs its own arbiter
  review with readings. Convergence still rests on the marks staged in each
  writer's transaction.
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
   the staged-audit deferrals, and the completion log's read seconds and
   repairs. `audit_last_success_age_seconds` and the staged-audit `last_*`
   gauges have no value until a pass completes in that process (and reset on
   a roll), so read their absence, not a climbing value.
3. Roll once more with the matching contract. This is the worst case: the
   backlog is present and no full runs.
4. After the NULL-version full commits, read
   `eshu_dp_supply_chain_impact_winners_staged_audit_deferrals_total{reason="budget"}`,
   the absence of the staged-audit `last_*` gauges (a budget-deferred tick
   records nothing there), and the per-pass `staged winners audit deferred`
   log lines with their `reason` and `read_seconds` fields, for three
   consecutive ticks:
   - If every tick defers on budget, and either `read_seconds` exceeds 60 s
     (the unrenewed lease TTL, so two pods read at once) or standby replay lag
     rises during the read, set
     `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL=0` on QA. Record it as a
     temporary state that ends when #11131 ships.
   - If the first tick completes, the budget premise is wrong. Report the
     reading and downgrade #11131 to a headroom issue at lower priority.

Roll back if any of these happens:

- an audit is in flight for over 30 min;
- the keyed `last_commit_timestamp_seconds` stalls for over 5 min while an
  audit is in flight;
- `resweep_skipped_total` climbs in that window;
- an audit transaction on the writer is older than 5 min.

The lever is `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL=0`. It turns the
audit off and keeps A. Record it as a temporary state. While the audit is off,
or deferring on budget, the only backstop is the manual force-full runbook
step: NULL both contract columns so the next tick runs one full. Each use is a
QA data mutation that needs its own arbiter review with readings.
