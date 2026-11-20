# #10878 first-domain slice: AWS runtime drift incremental path

Refs #10878. Part of #10860.

## What landed (branch drive/10878-incremental)

The AWS runtime drift domain now runs on its change signal end to end:

- **Plan** (`aws_drift_incremental.go`): skip on identical ARN snapshots,
  delta on added/changed/removed ARNs, full on daily audit (24 h),
  missing snapshot, or forced fallback. Never skip-on-missing.
- **Handler narrowing** (`aws_cloud_runtime_drift.go`): a delta plan
  classifies only the diff and bounds `EvaluatedARNs` to the diff keys, so
  the generation-authoritative retire retracts removed ARNs without
  touching untouched ones (dead-letter never-widen rule). A skip plan
  succeeds without touching the loader or writer.
- **Reopen gate** (migration 317 + `backfill/aws_drift_reopen_sql.go` +
  `postgres/aws_drift_reopen_fingerprint.go`): succeeded AWS items re-open
  only on missing row, logic-version mismatch, or fingerprint mismatch
  against the scope's current active generation. Claim-fenced record after
  the durable write, mirroring #8283.
- **Container gate** (migration 318 +
  `postgres/container_identity_reopen_gate.go`): the same changed-evidence
  gate for container_image_identity over the fleet identity epoch. The
  listing recomputes the epoch once per pass in a MATERIALIZED CTE reusing
  `probeIdentityEpochQuery` verbatim (CTE header names the columns
  positionally, so the gate can never probe a different set than the
  cache). The handler records the probed epoch after its durable writes via
  the new `FactStore.ProbeContainerIdentityEpoch`, fenced on its claim; a
  quiet fleet converges the whole domain to skips, any active-set change
  re-opens every recorded item through the already-shared once-per-epoch
  fleet load. Production wiring passes the fact store as epoch loader and
  an IngestionStore as recorder for both domains.
- **Shared loads** (`aws_evidence_cache.go`): singleflight per
  scope|generation|input-version, exactly-once per input version, over-cap
  payloads pass through observably (never cached, always counted).
- **Telemetry** (AC6): `changed_keys{mode}`, `skipped{reason}`,
  `fallback_full{reason}`, `audit_corrections` counters, plus the existing
  `correlation_reopen_skipped_total` now covering
  `domain="aws_cloud_runtime_drift"`.

## Proof

- `Performance Evidence:` delta-work shim (prior result 2, disposable
  Postgres 18, production SQL verbatim, medians over 2 warmups + 7 reps):
  full redo 11.18 ms / 67.35 ms at N=2000/8000 ARN generations (~2%
  churn); restructured per-ARN LATERAL delta 1.43 ms / 4.90 ms =
  12.8% / 7.3% of full, ratio improving with N. The naive delta reusing
  the production `aws_generation_arns` CTE full-generation recheck costs
  25.4% / 36.3% and fails the 25% bar -- the delta probes per changed ARN
  through the existing expression indexes and never reuses that CTE. Equal
  at both sizes: E2 unaccounted = 0, E3 state-row mismatch = 0 (20/100
  rows), membership-probe mismatch = 0; BuildCandidates is per-ARN
  independent, removals flow to the EvaluatedARNs-bounded retire.
- Go/SQL digest-twin proof (this slice, local Postgres 18.6, scratch DB
  destroyed): the Go `ComputeARNInputFingerprint` construction over three
  fixture ARNs renders `13cb793e…`, byte-identical to the SQL
  `string_agg(arn || E'\n' || payload_hash, …)` expression over the same
  rows; empty set renders `empty-arn-input` on both sides.
- `Benchmark Evidence:` all four AWS SQL statements (`List`, `Record`,
  `SnapshotRows`, fingerprint scalar) `PREPARE` clean on scratch schema;
  migration 317 applies with its FK intact. Both container statements
  (`List` with the epoch CTE, `Record`) `PREPARE` clean on stub schema;
  migration 318 applies with its FK intact. The container `PREPARE` round
  caught a real defect first (missing `CROSS JOIN current_epoch`,
  `missing FROM-clause entry for table "current_epoch"`), fixed and
  re-proven clean.
- Round-1 review fixes (#11018): per-ARN state-fold STOPPED on the D2
  cost gate (see below); the shipped
  posture is audit-bounded staleness — the fingerprint stays aws-only
  (D4-collapsed), skip/no-reopen additionally requires a fresh audit
  (listing audit-stale leg + planner `last_audit_at` clock), and the wired
  audit counts what a delta would have missed.
- Performance Evidence: D2 shim (disposable Postgres 18.6, bootstrap
  schema verbatim, ~1KB aws payloads, 70% state coverage, 2 warmups +
  7 reps, medians): N=2000 current-snapshot 6.82ms vs candidate 352.32ms
  vs full legs (resources 3.85ms + state 10.98ms) 14.83ms → candidate
  2376% of full (bar: ≤25%); N=8000 current 18.35ms vs candidate 89.7ms
  (lucky plan) –5302.5ms (sort spill, temp files) vs full 51.72ms →
  173%–10253%. The aws-only status quo itself is 35–46% of full, so the
  literal bar is unsatisfiable for ANY per-ARN fold. Plans show index
  probes only (keyset, state ARN, active-generation indexes; zero seq
  scans) but the final join degrades to nested-loop-with-filter under
  missing stats and recomputes per outer row — an estimate-fragile shape
  unsafe for the hot path. Two intermediate shapes rejected with numbers
  along the way (correlated scalar subqueries: 853ms at N=2000; O(N²)
  join filters: >60s hangs). VERDICT PER D2: STOP the fold, numbers
  recorded here; re-escalated to the arbiter for bar interpretation vs
  scope-level save-time state epoch vs audit-bound acceptance.
  Functional legs proven before stopping: row-set equality
  current==candidate (2000/8000 rows), state-only change flips the
  candidate fingerprint with identical aws payloads.
- No-Regression Evidence: the shipped D3/D4/D5/D6 shapes keep the
  current cost class (single index scans + sorts; no new per-ARN probes):
  D4 collapses the pair set the listing already scans (one row per ARN
  instead of one per fact row — fewer groups, same scan); the D3
  audit-stale leg adds two timestamp compares on the already-joined
  fingerprint row after the existing OR short-circuits; D5 adds one
  nullable column written once per run under the existing claim fence;
  D6 replays classification only on the daily audit run. Hermetic suites
  green (`contract`, `awscloud` + `-race`, `backfill`, `migrations`,
  `telemetry`, reducer root, `cmd/reducer`); live Postgres proofs green
  (`TestAWSDriftFingerprintCollapsesDuplicateARNsLive` RED pre-fix with
  SQL 2-pair vs Go 1-pair digests → GREEN post-fix, plus determinism
  across physical row orders and the hermetic same-fragment guard;
  `TestAWSDriftReopenFingerprintAuditClockLive` full-sets /
  later-claim-delta-preserves / later-full-advances;
  `TestAWSDriftReopenCandidatesAuditStaleLive` NULL/stale re-opens,
  fresh skips with equal fingerprints; D7 filter-bypass mutation turns
  both hardened handler tests RED, revert GREEN). Migration path: 317
  untouched (shipped — #7002), column lands in new guarded migration
  320 (metadata-only ALTER, no rewrite); bootstrap applies 345→346 clean
  on live Postgres.
- Observability Evidence: the wired `audit_corrections_total` counter
  is covered by production-path tests with a real meter (audit with a
  config-only miss emits exactly 1; delta, fallback-full, and zero-miss
  audits record no point); the skip/changed-keys/fallback counters keep
  their existing tests. Sustained nonzero corrections means the skip path
  is systematically blind to a real input class (counter semantics only;
  defect filing is follow-up work).
- Concurrency proof (AC5 + AC4 ride): 16 callers on one input version
  share exactly one load; a moved version reloads; over-cap payloads pass
  through without caching (`TestAWSDriftEvidenceCache*`, `-race` in the
  package suite below). Claim fencing rides the #8283 predicate verbatim
  (`FOR NO KEY UPDATE`, upsert keeps newest claim, zero rows affected
  maps to `ErrExecutionClaimRejected`).
- Mutation proof: inverting the empty-snapshot sentinel in
  `ComputeARNInputFingerprint` fails `TestComputeARNInputFingerprint*`;
  restoring it greens. Handler delta test pins untouched ARNs out of the
  retire bound; the skip test pins zero loader/writer calls.
- Suites: `awscloud`, `containerimage`, `contract`, `backfill`,
  `migrations`, `telemetry`, `cmd/reducer` build green; `go vet` clean;
  `gofmt` clean; race x2 on the new concurrency tests green.

## Before/after vs the #10862 baseline (honest status)

- Before (issue census): 27,547 working s / 771 runs (35.7 s avg) for the
  AWS drift domain. After: projected from the shim -- no-change maintenance
  replays skip (~0 s handler work plus one fingerprint subquery);
  ~2%-churn runs cost ~13% of a full run and classify only changed ARNs.
  Runs-per-changed-repo and working-seconds-per-changed-fact land on the
  new counters (`changed_keys`, `skipped`, `fallback_full`), read beside
  `eshu_dp_reducer_handler_duration_seconds`.
- NOT_CHECKED on live QA (no QA DSN on this box): real changed-ARN
  fraction between consecutive active AWS generations, per-domain p95
  full-run seconds for the audit budget, no-change-run skip timing, and
  the first sustained-nonzero-`audit_corrections` watch (counter semantics
  only: filing a defect off it is follow-up work, not an automatic rule —
  see the round-1 note above). The acceptance run is
  a live wave on this branch: green `required-gates-complete` plus the
  counter dashboard showing skip > 0, `fallback_full` == 0, and zero
  `audit_corrections` on the first daily audit.
- AC6 changed_keys emission is live in the handler; `skipped_no_change`
  maps to `incremental_skipped_total` (named for the closed reason set).

## Ordered plan for the remaining four domains (arbiter order)

1. **Supply-chain impact** (51% share, hardest equivalence): dirty
   (CVE, package, PURL) marked key in the winners dirty-log style,
   deriving the scoped stamp from facts changed since the last succeeded
   pass; until-stable expansion stays bounded to stamp reach. Prove
   genuine-pass stamp provenance before building (unresolved: who stamps
   a genuine pass with changed keys since last success).
2. **Container-image derivation narrowing**: gate reopen on the identity
   epoch (count/max/fingerprint compare already exists), narrow derivation
   to changed (scope:digest) pairs from the epoch diff. Prove the epoch
   probe cost first (`EXPLAIN ANALYZE` of the `string_agg` probe per call;
   if material, replace with a save-time version counter per the #10875
   sidecar pattern).
3. **Workload materialization**: fingerprint the unit's own generation plus
   `ResolvedInputFingerprintSQL` over its resolved rows to skip no-change
   runs; consume the #10876 waiting set-aside instead of
   bulk-load-then-defer under the fused loader. Whole-repo work units by
   definition (1.3 s avg -- the win is runs/loads avoided).
4. **Search documents** (last, needs new capture): dirty document keys
   captured at content-index write time (tables are overwritten snapshots,
   no generation diff exists) plus per-repo fingerprint to skip unchanged
   repos. Transitional exception per arbiter: <= 15 s avg per run, runs per
   changed repo not above the #10862 baseline rate, expiring at
   dirty-capture landing or the phase-2 gate review.

Each domain repeats this slice's ladder: RED equivalence tests first,
theory shim before building, golden-corpus/live enrollment, audit with
correction counting, concurrency proof, then the receipt chain. No new
domain starts while the AWS live wave is red.
