# Shared-Snapshot Export Rollout and Integrity-Assessment Plan

Issue #7957 (P1). The deny-by-default qualification gate is merged (#8201):
an unqualified reader refuses shared-snapshot export with
`db.ErrSnapshotExportUnqualified`, answered as a permanent 503 with
`details.reason=snapshot_export_unqualified` and no fallback. This plan
covers how a qualified PostgreSQL build reaches production, and how to
assess data integrity separately. It authorizes no deployment and no
write to any deployed reader.

## Hard constraint

A package upgrade does **not** repair existing wrong hint bits. The two
upstream defects (`ef89c839cdc8`, backpatched as `74c9832ca610`; and
`ca99016f939d`, backpatched as `3977cde272bc`) corrupt visibility
information at write time on the affected build. Replacing the binary
stops new damage; it does not revisit pages already marked. Every rollout
step below is separate from integrity assessment, and no green rollout
signal may be read as proof that existing data is clean.

## Entry gate: pin the qualified build first (AC1 remainder)

No rollout starts until one record binds all five fields:

- Authenticated `server_version` and `server_version_num` from the running
  server (not `postgres --version` on a client), plus `in_recovery`.
- The deployed image digest (e.g. `sha256:<digest>`).
- The package revision carrying both backports.
- The source or backport record for **both** fixes: `74c9832ca610`
  (standby export subxip[]) and `3977cde272bc` (import xmin/VACUUM race).

The patched `18.6-eshu-7957-amd64` image record, verified live on the
QA reader on 2026-10-20 and 2026-10-21 (this paragraph) and in the
AC1 pin record cited below, is:

- Published image (org-internal visibility):
  `ghcr.io/eshu-hq/postgresql-eshu:18.6-eshu-7957-amd64` at digest
  `sha256:26accacbc9276027930b7a117cf16127a9092021d6fed42a0e0aaf5a3bf93769`.
- Running server on the QA reader: `server_version 18.6-eshu-7957`,
  `server_version_num 180006`, `pg_is_in_recovery()=true`, container image
  equal to the published digest (read from the live pod spec and image IDs).
- Build declaration: `ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS` carries the
  exact-build record (`18.6-eshu-7957`, digest above, recipe
  `images/postgresql/Dockerfile @ 6fc7463`); the live API logs
  `postgres.reader.snapshot_export_qualification` with `qualified=true` and
  `reason=declared_build`.
- Pin and declaration path: IaC PR 289 (pin, merged 2026-10-19) and IaC
  PR 296 (declaration, merged 2026-10-21) in the separate infrastructure
  repository. The IaC 289 pin commit `188246e` records in the header
  comment of its `postgresql-values.yaml` pin file: image
  `ghcr.io/eshu-hq/postgresql-eshu:18.6-eshu-7957-amd64`, the digest
  above, and recipe `images/postgresql/Dockerfile @ 6fc7463 (REL_18_6 +
  2x REL_18_STABLE fixes)`. This is a from-source build
  (`--with-extra-version=-eshu-7957`); it has no distribution package
  revision separate from that record. The package-revision field above
  is therefore bound by recipe revision
  `6fc74636e6d4d9e869efb624f1bee1e61f66d7d9`, which pins source tree
  `a8643a2276b48a9ddc8948e03ea57a8ea6162194` and both backports below.
- Source/backport record for both fixes: the IaC recipe pins the upstream
  commits (standby export `74c9832ca610`, import/VACUUM `3977cde272bc`)
  with patch-id agreement between the recipe tree and the built source
  (issue thread, 2026-10-17).

The `--with-extra-version` suffix requirement above is met: `server_version`
is `18.6-eshu-7957`, so a bare `18.6` declaration only logs a WARN and
cannot qualify an unpatched build. The entry gate is open for the reader
leg. The primary still runs the unpatched 18.3 image (OnDelete hold); its
promotion is a named-owner window, not this plan, and the mixed state is
safe to hold because the guard refuses unqualified snapshot export.

## Stage 1: isolated fixture proof (AC2, AC4)

Run on an owned disposable primary/standby pair. Never reproduce either
upstream fault on a deployed reader; checks against one stay read-only.

- Unsafe build, undeclared: `TestSnapshotExportRefusedOnUndeclaredStandby`
  must refuse with the sentinel, zero borrows, and no `BEGIN`,
  `pg_export_snapshot`, or `SET TRANSACTION SNAPSHOT` on the standby
  (`log_statement=all` window).
- Qualified build, declared: `TestSnapshotExportDeclaredStandbySharesOneSnapshot`
  must show one export and identical snapshots on all four readers, and
  `TestSnapshotExportConsistencySmoke` must show four imported readers plus
  one separate plain standby session reading identical sha256 digests.
- Concurrency on the qualified build: the result, cancellation, retry, and
  cleanup suites (`TestReadSnapshotSet*`, `TestFleetConcurrent*`,
  `TestSnapshotSetDenialAndCancellationLeaveNoReservation`) must pass with
  `-race` and leave no reserved connection, permit, or member slot.

## Stage 2: query-budget proof (AC5)

Rerun the matched before/after measurement from the qualification note
with the same corpus, profile, topology, and storage state on the
qualified build: statement counts on the touched path, the interleaved
allocations bench, and `BenchmarkFleetSnapshotSetup` /
`BenchmarkFleetStalledFirstMember` (both NOT_CHECKED to date; they need an
owned primary and two physical standbys). A regression against the last
known-good baseline is a bug, not rollout cost.

## Stage 3: controlled rollout

1. Deploy the pinned image to one standby. Confirm the startup log
   `postgres.reader.snapshot_export_qualification` reports INFO with the
   declared `server_version`, and the gauge
   `eshu_dp_postgres_reader_snapshot_export_qualified` reads 1 for that
   member while unqualified members stay 0 and keep refusing.
2. Confirm `eshu_dp_postgres_reader_snapshot_export_denials_total` stays
   flat for the new member and the request span carries
   `code_topic.execution_mode` other than `denied`.
3. Roll the remaining readers only after the first member serves
   production snapshot sets cleanly. Keep one unqualified reader refusing
   until the end as the control that the gate still bites.

## Integrity assessment (separate from rollout)

- Scope: any table read through a shared snapshot on an unpatched build
  while subtransaction overflow or concurrent VACUUM was possible.
- Method, read-only: compare standby-served reads against primary-served
  reads for the same snapshot window; inspect standby logs for the
  defect windows; treat any mismatch as suspect pages to be re-verified
  from the primary, never as proof of cleanliness on match alone.
- Explicitly out of scope for this plan: any claim that upgrading, running
  VACUUM, or re-reading repairs hint bits. If suspect pages are found,
  that is a new incident with its own owner decision, not a rollout step.

## Operator signals (at 3 AM)

Gauge, denial counter with closed `reason` labels, startup INFO/WARN logs,
per-request denial WARN, and the `postgres.query` span denial attributes
are defined in [the read-routing reference](../../public/deployment/postgres-read-routing.md#shared-snapshot-export-qualification).
A busy unqualified reader logs denials at request rate: declare the build
or upgrade the reader; do not filter the log.

## Open items

- Build pin (digest, package revision) for the qualified image: RECORDED in
  the entry-gate section above (digest verified live; package-revision field
  bound by recipe revision `6fc74636e6d4d9e869efb624f1bee1e61f66d7d9` —
  from-source build, no separate distribution package revision).
- AC2/AC4 proof on the qualified image: disposable-pair runs green on
  2026-10-21 (issue thread; owning qualification lane). The bare-metal
  amd64 re-run (recipe fail/pass pair plus `-race` live suites) stays open;
  the venue is an owner decision.
- AC5 proof: split per the 2026-10-21 arbiter ruling. AC5b (QA
  `<1 s` code-topic endpoint budget on the qualified route) is green as of
  2026-10-21: 10 timed full-body samples, median 0.197 s, p95 0.285 s, every
  sample under budget; a causal probe ties one 200 to exactly one
  `pg_export_snapshot` plus three `SET TRANSACTION SNAPSHOT` calls, and all
  timed 200s match that path (sub-0.3 s against the 1.184 s single-statement
  median from #7516, gauge 1, zero denials); corpus fingerprints taken around
  the window (issue thread). AC5a (statement counts, fleet benches on a real-amd64 disposable
  pair from the pinned digest) stays open with the bare-metal venue.
- AC6a (this plan's stale items): closed by this revision. AC6b integrity
  read-out: no mismatch found on 2026-10-21 (issue thread; read-only
  assessment lane). A same-snapshot-window pinned comparison and any
  surviving 18.3-era log surface stay open if the owner wants them.
- Stage 3 steps 1-2 observed complete on the QA reader (gauge 1 for the
  declared member, zero `snapshot_export_denied` log lines on the pod, so no
  new denials). Step 3
  (remaining readers, unqualified-reader control) and the primary promotion
  window stay with the owner.
- CI standby mirror (`postgres:18.6-bookworm`) and local Compose tag stay
  unqualified until they resolve at or above 18.7 or are replaced.
