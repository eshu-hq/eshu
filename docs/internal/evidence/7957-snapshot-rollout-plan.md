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

The patched `18.6-eshu-7957-amd64` image is built and pushed but neither
public nor deployed, and its digest and package revision are not recorded
in this repository. Recording them here is the remaining AC1 work; until
then the gate is closed. The qualified image must also set an
`--with-extra-version` suffix so the exact `server_version` string in
`ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS` can only match that build:
a bare `18.6` label matches patched and unpatched builds alike and only
logs a WARN.

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

- Build pin for the qualified image: RECORDED 2026-10-21.
  Digest `ghcr.io/eshu-hq/postgresql-eshu@sha256:26accacbc9276027930b7a117cf16127a9092021d6fed42a0e0aaf5a3bf93769`
  (`18.6-eshu-7957`, live-corroborated on the deployed reader). Pinned via
  IaC PR 289 in the separate infrastructure repository (merged 2026-10-19)
  and declared via infrastructure-repo PR 296 (merged 2026-10-20;
  API reports `qualified=true reason=declared_build`). Package revision:
  NOT RECORDED.
- AC2/AC4/AC5 proof on the qualified image: PARTIAL. AC2/AC4 halves pass
  on a disposable pair (2026-10-21 lane runs) but that host is arm64, so
  the amd64 run the criteria require is still owed, as is every AC5
  measurement (statement counts, fleet benches, endpoint budget). The
  image is deployed on the read replica and declared; the primary still runs
  bitnami 18.3 on OnDelete hold, promotion pending an owner window.
- CI standby mirror (`postgres:18.6-bookworm`) and local Compose tag stay
  unqualified until they resolve at or above 18.7 or are replaced.
