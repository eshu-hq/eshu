# Reducer walker derive (#8155, PR3b-2)

This note records the walker slice of the write-time-derivation chain: the
runner loop, its wiring, and the proof that admits it. Parent records:
[the decision](7126-ingest-time-config-artifacts.md) (PR table row PR3b-2,
sections 3.5 and 4),
[the protocol companion](7126-ingest-time-config-artifacts-protocol.md)
(sections 3 and 4),
[the derive core](8155-derive-core.md) (PR3b-2b),
[the 2c-2 wiring](8155-2c2-write-derive-wiring.md) (A2 attribution, S1).
Refs #8155. Arbiter verdict 2026-10-19: APPROVED-WITH-AMENDMENTS A1-A4.

## What landed

- The runner (`go/internal/reducer/maintenance/config/artifacts/runner.go`):
  each cycle derives the dirty join's repositories with trigger `repair`,
  then one claimed walk page with trigger `backfill`, over K workers that
  partition by `repo_id` (`Assign`). Skipped repositories are counted and
  never derived: the dirty path skips them from the repair group, and the
  walk page — a range scan with no skip filter — is filtered against the
  skip check (`SkippedRepos`: failure count at the limit for the current
  extractor), so a skipped repository is never re-derived as backfill. When
  a claimed page reaches the end of the walk and a fresh dirty re-list is
  empty, the cycle records `config_artifact_paths_v1` in
  `config_artifact_backfill_markers`: the first full walk is done.
- Outcome mapping to `eshu_dp_config_artifact_reconcile_total`: `ok` repairs;
  a `backfill` derive that rolls back reports `match` (the repository was
  already current); a `repair` that loses to a racing Write reports
  `superseded`; every failure reports `error`; skipped reports `skipped`
  without a derive. A backfill `superseded` by a racing Write misreports
  `match` for one cycle: when the racing Write's derive failed the
  repository stays dirty and the next cycle repairs it, and when it
  succeeded the repository is already clean and the one wrong point simply
  stands; either way the next cycle labels correctly.
- A read-model-off refusal (`encoding_unsupported`, `fence_missing`) aborts
  the cycle after counting the derives that ran: no walk claim, no marker,
  no immediate retry. A derive the shutdown cancels records nothing.
- The marker helpers (`BackfillMarker`, `BackfillComplete`,
  `RecordBackfillMarker`) beside the walk claim and dirty join, with the
  marker name pinned by test (renaming it orphans recorded installs).
- The walker's own variables: `ESHU_CONFIG_ARTIFACT_WALKER_ENABLED`
  (default true) and `ESHU_CONFIG_ARTIFACT_WALKER_WORKERS` (default 4,
  protocol section 4), with the parse loader beside the derive loader and
  the regenerated registry doc. The shared derive deadline
  (`ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT`) bounds walker derives too.
- The reducer wiring (`go/cmd/reducer/config_artifact_walker_wiring.go`,
  service field, side-runner start): the deriver is built once at start with
  the guard-pinned `ExtractorVersion`, and the storage functions adapt to the
  leaf's ports. Disabled builds no walker and issues no statement; a database
  without transactions leaves the walker off with an ERROR record (reducer
  idiom, like the sibling sweeper and locker), while an invalid walker or
  derive setting still fails startup.

## Amendments

- A1 (no fork): every behavior above is the protocol's section 4 or the
  record's telemetry table, or a named port the PR3b-1a skeleton reserved
  for this slice. The two judgment calls are documented where they are made:
  the backfill/superseded `match` approximation (self-correcting, see
  above) and the literal marker rule (skipped repositories keep the marker
  pending; their next Write or an extractor change retries them).
- A2 (theory-first): the attribution shim and its verdict are in the 2c-2
  note; this slice re-proves nothing and reuses the same `DeriveRepository`,
  so S1 bounds the per-repository cost (next section).
- A3 (parity + rollout): the S4 equality test re-runs against this head
  (section below); shadow zero-mismatch, the offline differential, and the
  re-baselined `config_paths` drift stay PR3c acceptance work, recorded as
  pending there, not here.
- A4 (chain + rollback): this slice joins the chain after 2c-2 and before
  PR3c; no reader changes, so the route still pays its read until PR3c.
  Rollback stays flag-to-live: `ESHU_CONFIG_ARTIFACT_WALKER_ENABLED=false`
  builds no walker (the disabled path is tested to issue no statement), the
  Write derives keep running under their own switch, and readers stay live
  for repositories the walker has not derived.

## Proof

- Hermetic (`go/internal/reducer/maintenance/config/artifacts`): cycle order
  and trigger labels, repair-before-walk with dedup, skipped-never-derived
  on both paths (dirty-skipped counted once, walk-only skipped filtered by
  the skip check and counted once, failing skip check fails the cycle), the
  outcome mapping, marker-only-on-clean-wrap (failure, full page, and
  clean cases), refusal abort (single- and multi-worker), shutdown silence,
  dependency validation, drain-then-wait plus error-then-wait (a failed
  cycle waits the poll interval before the retry).
- Hermetic (`go/internal/storage/postgres/config/artifacts`): the skip
  check plumbing (passthrough, empty-ids short-circuit, query errors
  including a missing table).
- Hermetic (`go/internal/content`, `go/cmd/reducer`, `go/internal/reducer`):
  loader defaults/parsing/rejections, wiring defaults/disabled/invalid
  (bad workers, bad timeout, pool without transactions), refused checks
  still build and refuse without a statement, trigger and reason parity with
  the storage constants, service start/nil-disable.
- Live (`TestConfigArtifactWalkerCycleLive`, disposable Postgres 18):
  two never-derived repositories repair through the real dirty join,
  deriver, walk claim and marker record; the second cycle matches both as
  already current; `derives_total{repair,ok}=2`,
  `{backfill,superseded}=2`, `reconcile{repaired,match}=2+2`, the
  `stale{missing}` gauge drains to 0, both state rows read
  `content_version=1` with non-NULL `row_count`.
- Live (`TestBackfillMarkerRoundTripLive`): absent after bootstrap, present
  after one record, stable across a second record.
- Live (`TestSkippedReposPredicateLive`): only the repository over the
  failure limit for the current extractor is reported skipped; under-limit,
  old-extractor-failure, and successful repositories are not.
- Live (`TestConfigArtifactWalkerSkipsFailedRepositoriesLive`): a skipped
  repository is never derived on either path while its sibling repairs; the
  marker stays pending; the skipped failure record is untouched (still five
  failures, still NULL `row_count`).
- S4 (`TestDeriveStoredRowsEqualLivePathLive`, PR3b gate): re-run against
  this head 2026-10-20 on disposable Postgres 18.6: 220 repositories
  (43 with rows), 160 live rows, 160 stored rows, differing 0/0, PASS in
  2.42 s.

Performance Evidence: walker cycle cost on disposable Postgres 18.6, local
shared host, 2026-10-20 (throwaway harness in the style of S1, deleted before
the PR): a repair cycle over 20 never-derived (empty) repositories took
21.5 ms wall (about 1.1 ms per repository) and recorded the marker; five
steady match cycles over the same 20 took 7.1-9.2 ms, mean 8.0 ms (about
0.4 ms per repository). The per-repository derive is the same
`DeriveRepository` S1 measured on the built binaries (golden corpus median
+0.8 to +1.1 ms added Write wall; 5,000-file monster +0.21 s against the 2 s
worst-repository bar), so a full backfill is dominated by the largest
repositories and costs seconds once. An idle clean cycle issues only the
dirty join and one walk claim (both milliseconds; the steady-cycle figure
covers them). The poll interval (5 minutes) bounds the steady-state rate;
each cycle's work is bounded by the page budget (500) times K=4 workers.

No-Regression Evidence: the walker is a new side runner, so the regression
surface is the reducer without it: disabled (the turned-off path) builds no
walker and issues no statement (`TestConfigArtifactWalkerRunnerForDisabled`
counts zero statements); enabled, an empty cycle issues two bounded indexed
statements per 5 minutes (dirty join over the versions/state tables, one
walk claim) and no derive. No existing path is edited except additive
service assembly (one field, one start block, one builder call with error
propagation); the Write path, projector wiring, reader, and retention are
untouched.

Observability Evidence: first emission of
`eshu_dp_config_artifact_reconcile_total{outcome}`,
`eshu_dp_config_artifact_stale_repos{reason}`, the `backfill`/`repair`
trigger series of the derive counter and histogram, the per-derive
`config_artifacts.derive` span from the walker, and the
`config_artifacts.walker.*` logs. The live cycle test reads every new
series back through an SDK reader (counts and the drained-to-zero gauge);
the hermetic cycle test asserts two span names. Rows added to
`docs/public/reference/telemetry/metrics.md` and
`docs/public/observability/telemetry-coverage.md`; no operator-dashboard
change (no config-artifact metric is dashboarded).

## NOT_CHECKED and follow-ups

- PR3c shadow/stored, the read-mode flag, and the section 6 acceptance reads
  (need QA; the QA content-stage p95 denominator behind the S1 10% sub-bar
  stays deferred P2-1 from #8648, closing at PR3c acceptance).
- The `config_paths` drift (3,655 rows Oct-19 vs the 2,986 anchor):
  re-baseline in `live` mode before the PR3c acceptance reads.
- A permanently failing repository with no future Writes keeps the backfill
  marker pending (literal record rule; the skip exits on next Write or
  extractor change). Flagged in the leaf AGENTS.md as arbitration territory
  if a carve-out is ever wanted.
- A panic inside the neutral package's chunk-read goroutines (unchanged from
  the derive core note).
