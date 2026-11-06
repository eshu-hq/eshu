# #10135 impact readiness: a release that left the fleet retires

Refs #10135. Follow-up to #10015, which left the known limit in place: a
release that left the fleet kept its package not covered, naming the release,
until something besides the plan window could say the release was gone. A
rotating plan slice can never say that (arbiter ruling arb-10015), so this
change decides it from the installed fleet itself.

## What changed

`readinessDerivedSourceStateCTE` keeps the newest state per requested package,
distro and release, and every non-fresh release row counts (#10015). It now
retires one more shape: a non-fresh Alpine release row that no active
`vulnerability.os_package` fact still carries reads `fresh`, so it stops
holding the package not covered. The check is the batched EXISTS probe in
`readinessDerivedReleaseFleetProbeCTE` (candidate (a) from the shim below):
one EXISTS per distinct resolved (package, release) triple, probed only for
triples that are not fresh or are fresh but past the freshness cutoff. Fresh
rows inside the cutoff never consult the fleet, and neither do pending rows,
so a release the fleet still carries keeps counting in every plan window.

The probe predicate inverts the coordinator's release minting exactly: the
reader ecosystem the planner filters on, the installed binary name or source
name it reads, and the Alpine major.minor line derived from `distro_version`
(`installedAlpineOSVRelease`, #9993). Both name arms must miss for absence to
hold, so a source-only install still counts as installed. Non-Alpine releases
and planned-only rows have no presence row and default to present, which keeps
them on the pre-change behavior. Migrations 281 and 282 serve the two arms
with partial expression indexes on (name, release line) and (source_package,
release line); the release-line text is built once by
`osPackageFleetReleaseLineExpression` and pinned to the migration files and
the probe by `TestReadinessFleetReleaseLineExpressionMatchesMigration`.

A retired release is named in the aggregate's `retired_releases` detail as
`source:distro:release` (for example `osv:alpine:v3.17`) and never in
`incomplete_reasons`. The `SourceState` wire struct, the OpenAPI schema and
the HTTP reference carry the new field.

## Why the fleet, not the plan

Same ground as #10015 (see `10015-readiness-release-coverage.md`): the plan
is one limit-sized window, single-pass is a fixed slice, the budget counts
queries not rows, and the reader never returns its total. A plan that does not
name a release says nothing about whether the release left the fleet. The only
complete source is the active `vulnerability.os_package` facts, which is what
the planner itself reads.

## Shim proof (theory first)

Measured before any code on a disposable local Postgres 18.6 with 181,601
fact rows, the hot package present in 3,000 images
(`/Users/allen/.codex/run-state/10135-shim1-handoff-20261105.md`):

| Probe | Median | Buffers | Plan |
|---|---|---|---|
| (a) EXISTS hit, musl/3.18 | 0.054 ms | 10 shared hit | index seek, no seq scan |
| (a) EXISTS miss, musl/3.16 | 0.039 ms | 3 shared hit | index seek, no seq scan |
| (b) last-seen table hit | 0.035 ms | 4 shared hit | PK seek |

Candidate (a) shipped in batched form per the handoff recommendation: one
statement over the whole non-fresh triple set, so worst-case cost is one round
of index seeks regardless of the non-fresh count. Candidate (b) stays rejected:
it needs a coordinator write path, a rotation-aging rule the reader cannot
feed, and an unproven two-coordinator concurrency proof.

## Shipped proof (before/after on the touched path)

Same-machine relative evidence (MacBook arm64, Homebrew Postgres 18.6, local
scratch server; host load not recorded). `EXPLAIN (ANALYZE, BUFFERS)` on the
readiness statement:

- #10015 corpus (2500 Alpine + 2500 npm states, 200 planner runs, no
  installed facts, probe input empty), `os` x 100 request, measured at HEAD:
  25.872 ms (56.235 ms before the change on the same corpus; the delta sits
  inside run-to-run noise, no regression); `npm` x 100: 3.733 ms
  (7.421 ms before, within noise).
- Firing corpus (100 packages x 2 releases, older states failed, installed
  facts for both releases or the older one removed), `os` x 100 request,
  measured at HEAD: 22.519 ms / 2074 shared-hit blocks with the release
  present, 18.162 ms / 1758 blocks with it left. Both fleet indexes serve the
  probe (BitmapOr of the two arm seeks); no sequential scan inside the probe
  legs.
- Worst-case ceiling (scratch run, reverted): 1000 packages x 2 releases with
  every triple non-fresh, 1000 triples probed. Present 147.112 ms / 21283
  blocks, absent 77.091 ms / 17180 blocks, all-fresh same corpus 102.751 ms /
  12283 blocks. The present path pays heap fetches for its 1000 matches; the
  absent path is index-only. The realistic case (a handful of non-fresh
  triples) stays under a millisecond and dozens of blocks per the shim's
  per-probe cost, flat at fleet scale.

`TestSupplyChainImpactReadinessFleetProbePlanLive` pins the firing shape
(index seeks, no scan, 500 ms local envelope);
`TestSupplyChainImpactReadinessReleaseScopeQueryPlanLive` pins the existing
corpus shape unchanged. The mixed-fixture answer table is
`TestSupplyChainImpactReadinessDerivedReleaseCoverageLive` plus the state-only
family, all green with fleet facts seeded wherever a non-fresh release is
meant to stay installed.

## Concurrency

The statement stays one read in a repeatable-read snapshot with no lock,
lease or write. The two new indexes build concurrently. Materializing both
probe CTEs pins run-once evaluation: an early shape let the planner inline
the presence leg and re-evaluate it per outer row (20,000 loops, 40,000
buffers at 100 packages), which the plan proof now guards against.

## Full-rotation proof (real planner)

#9993 merged during this change, so the rotation test runs instead of
skipping: 450 Alpine facts (pkgs 0-149 on 3.17.3, pkgs 0-299 on 3.18.4), the
real reader and the real planner over nine consecutive windows, both fact-id
orders, 4 of 9 windows slicing exactly one release of pkg-104:

- pkg-104 (v3.17 fresh, v3.18 failed, both installed): `failed` naming
  `osv:failed:alpine:v3.18` in all 9 windows of both layouts.
- pkg-250 (v3.18 fresh, v3.17 failed and gone: img-a carries only pkgs 0-149,
  so no active fact carries pkg-250 at 3.17): `fresh`, naming nothing, in all
  9 windows of both layouts.
- pkg-200 (v3.18 fresh, stale legacy scope): `fresh` in all windows.
- pkg-120 (v3.18 fresh, no v3.17 state): `pending` exactly in the windows whose
  plan names v3.17, `fresh` in the rest.

The window-dependent verdicts the rotating plan caused before #10015 do not
come back: no window reads a present release fresh-by-absence, and no window
counting a gone release differs from another.

## Limits (NOT_CHECKED)

- Wall-clock figures are same-machine relative evidence only (MacBook arm64,
  Homebrew Postgres 18.6, local scratch server, host load not recorded); no
  absolute reference-profile claim.
- Unreadable `distro_version` values (fewer than two numeric dot fields)
  never match a release; a fleet carrying only unreadable rows for a release
  reads that release retired, consistent with the planner skipping those rows.
- `TRIM` strips spaces where Go's `TrimSpace` strips every unicode space; a
  `distro_version` with exotic padding is the one shape the SQL mirror
  misreads, and `/etc/os-release` never writes it.
