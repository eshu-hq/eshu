# #9806 PR-2b-1: OSV release tags, dormant reducer selection

Refs #9806.

The OSV converter now tags each OS range with the release lines that listed it,
and the reducer carries the logic that reads the tag. Nothing reads the tag in
production yet: factschema v0.1.0-rc.12 has no `releases` field, so the typed
decode drops it and every live fact stays untagged. No live verdict change was observed across the 738 live OSV records tried
(81,354 host rows, base against head, identical output), with one deliberate
exception (ruling decision 6, below).
The activation PR (after the SDK declares the field) turns the tag on.

## Rules landed

- **Listed host line.** When every ECOSYSTEM range names its lines and one names
  the host's, only that line's ranges and fixes decide, and the exact-version
  list is dropped (`ReleaseLine` set). Only this case can read `affected_exact`.
- **Unlisted host line.** When every range is tagged and none names the host's
  line, the answer is never exact and `ReleaseLine` stays empty.
  - A listed range that contains the version: `possibly_affected` /
    `os_release_unlisted`, missing evidence "the advisory lists no range for the
    installed package's release line".
  - No listed range contains it and every listed fix is at or below it:
    `not_affected_known_fixed` / `dpkg_known_fixed_unlisted_release`. A Debian
    or Ubuntu version string stamps the release (`+debNuM`, `ubuntuN.M`), so it
    orders across lines.
  - Alpine gets `apk_known_fixed_unlisted_release` only when, for every listed
    fix `U-rN`, the host's upstream version is above `U`, or equals `U` and
    `N == 0`. Otherwise `os_release_unlisted`. The `-rN` counter is per branch:
    the live Alpine pull lists the same upstream fix at different `-r` on
    different branches in 22 of 196 curl records, and 233 of 1,963 Alpine fixes
    are backport revisions (`-r` above 0).
  - The remediation's first patched version for such a host is a listed line's
    fix, not the host's.
- **Single-fix shortcut.** For an OSV OS fact, an exact-version list that names
  the observed version is read before the single-fix shortcut (a record that
  lists a version as affected while its one fix is below it now reads
  `os_release_unverified`, not known fixed).
  **This edit is not behind the dormancy gate.** It is the one live exception: an
  untagged OSV fact whose exact-version list names the installed version while
  its single fix is at or below it gave `not_affected_known_fixed` before and
  gives `possibly_affected` / `os_release_unverified` now (ruling decision 6).
  A differential run of the base and this tree over all 738 live OSV records
  (Debian 298, Ubuntu 244, Alpine 196 curl pulls; 3 hosts per distro, every
  listed version, introduced, fixed and last_affected, up to about 40 sampled per
  record, both entry orders = 81,354 host rows) produced identical output, so the
  exception was not observed on that corpus.
- **Unlisted settlement reads lists first.** The same rule holds when a tagged
  fact names no range for the host's line. An exact-version list that names the
  host is read before the unlisted known-fixed settlement. When some fact is
  unlisted, a list on a fact that carries release evidence (a vendor advisory)
  counts too, because that fact settles the host exactly. Both apply only when
  a tagged fact is present, so no live verdict moves. The same hole in the
  untagged single-fix shortcut, where a vendor list is skipped, was tracked in
  #9929 and is closed there; this change does not touch it.
- **Any untagged range** keeps the whole fact on the untagged rules.

## Proof

| Check | Result |
|---|---|
| Unlisted rule and apk guard, written first | RED: 9 of 9 rows plus the settle test and the shortcut test failed on behavior (exit 1); GREEN after the change (exit 0). |
| Mutant "extrapolate apk by revision" (guard disabled) | RED on the 3.23 `8.14.1-r5` row and the mixed `-r0`/`-r4` row (exit 1); restored, GREEN. |
| Dormancy pin | Converter output carries `releases`; the real decode seam leaves `Releases == nil`; the bookworm `7.88.1-10+deb12u2` row reads `possibly_affected` / `os_release_unverified`. The activation PR deletes this test. |
| Shared vector table (`testdata/os_release_line_vectors.json`) | Both key functions read it. Seeded drift in the reducer (Alpine keeps three fields) and in the converter (Pro not dropped) each failed one test (exit 1); restored, GREEN. |
| Success-log sub-signals | RED when the sub-signal lines were removed (exit 1); GREEN restored. |
| Unlisted settlement reads the list first (automated review P2) | RED: the new dpkg row read `not_affected_known_fixed` / `dpkg_known_fixed_unlisted_release` (exit 1); GREEN after the guard (exit 0). Removing the guard turns the new dpkg and apk rows RED. |
| Vendor list wins over an unlisted OSV fact (independent review) | RED: 8 of 8 cases (dpkg and apk, several fixes and one fix, both fact orders) read known fixed while the vendor fact alone reads `affected_exact` (exit 1); GREEN after the scoped change (exit 0). |
| Untagged twin stays as it is (independent review H1) | `TestUntaggedVendorVersionListKeepsSingleFixShortcut` pins today's untagged answer (`not_affected_known_fixed` / `dpkg_known_fixed`, both fact orders) and passes (exit 0). Widening the guard to every join turns it RED with `affected_exact` (exit 1); restored, GREEN. #9929 deleted that test and flipped its answer on purpose, as `TestUntaggedVendorVersionListBeatsOSVSingleFix`. |
| `factschema-diff`, `payload-usage-manifest`, `extensionconformance`, accuracy golden gate | all exit 0. No schema edit, no new read. |

## One-time re-collection

The new `releases` key does not change a fact ID (the ID hashes kind, stable key,
scope and generation only), but it changes the collection freshness hint, which
hashes the whole payload. The first collection of every OSV target whose records
include a Debian, Alpine or Ubuntu record is therefore not skipped as a duplicate:
that target writes one new generation, then returns to steady state. This is also
what tags the stored facts without a re-ingest (ruling decision 12). Measured on
the committed fixtures: the Debian, Alpine and Ubuntu hints differ between base
and head; the GHSA hint is identical. How many deployed OSV targets carry OS
records: NOT_CHECKED.

## Performance

Performance Evidence: no hot path gets heavier while dormant. Selection returns
the input slice for an untagged fact before it resolves the host line, so the
per-finding cost is one `Releases` length check per ECOSYSTEM range of a joined
OS fact.

No-Regression Evidence: `BenchmarkOSAdvisoryJoin` (fleet) and
`BenchmarkOSAdvisoryJoinTargets` (per target), the PR-2a rows, run as compiled
test binaries from a detached worktree of the base commit and from this tree,
alternating which tree runs first, 6 rounds of `-test.benchtime=1s`.

**Non-quiet host.** Another session was running on the machine: load1 ranged
16.3 to 56.0 across the rounds (median 44.8). The two controls that join on
neither tree (`miss-10x`, `registry-10x`) moved by +10.0% and +1.1% in the
median and -23.9% and -23.4% in the minimum, so wall-time deltas here are noise.
This note makes no speed claim in either direction. Allocations do not depend on
load and are the figures to read.

### Scale (allocations per op, deterministic)

| Row | Allocs/op base | Allocs/op after | B/op base | B/op after |
|---|---|---|---|---|
| fleet 1x (10 images per vendor) | 1,722 | 1,722 | 170,999 | 171,598 (+0.4%) |
| fleet 5x (50) | 2,329 | 2,329 | 267,899 | 268,438 (+0.2%) |
| fleet 10x (100) | 3,083 | 3,083 | 398,429 | 398,956 (+0.1%) |
| per target 1x (10 targets) | 13,900 | 14,101 (+1.4%) | 1,254,397 | 1,274,160 (+1.6%) |
| per target 5x (50) | 67,649 | 68,642 (+1.5%) | 6,079,668 | 6,173,108 (+1.5%) |
| per target 10x (100) | 134,107 | 136,113 (+1.5%) | 12,046,999 | 12,240,126 (+1.6%) |

The growth is the extra `releases` key in each decoded OS range payload (the
decode reads the key and drops it). It is linear in targets, so the per-target
cost stays flat across 1x, 5x, and 10x.

### Timing (non-quiet host; informational)

Median ns/op, base then after: fleet 1x 311,207 / 391,078; 5x 408,702 /
497,468; 10x 584,430 / 817,610; per target 1x 2,008,782 / 2,928,962; 5x
13,208,279 / 13,502,449; 10x 35,110,885 / 22,195,384. Both directions appear
(10x per target is 37% faster in the median, 1x is 46% slower), and the
controls swing the same way, which is what noise looks like.

Observability Evidence: the join counter gains a release result.
`eshu_dp_supply_chain_impact_os_advisory_release_selections_total{source,result}`
counts each joined OS advisory fact as `release_matched`, `release_unlisted`,
`release_untagged`, `host_release_unknown`, or `vendor_source`. The success log
carries `os_advisories_release_matched`, `os_advisories_release_unlisted`, and
`os_advisories_release_untagged`. Until activation every joined OSV fact reads
`release_untagged`; that is expected. A finding's verdict for an unlisted host is
read from its `match_reason` (`os_release_unlisted`,
`dpkg_known_fixed_unlisted_release`, `apk_known_fixed_unlisted_release`).

## Not checked

- How many deployed OSV targets carry Debian, Alpine or Ubuntu records (the size
  of the one-time re-collection).
- Wall-time on a quiet host.
- The B-7 golden-corpus gate on live services (the committed vulnerability
  cassette is a recording, not converter output).
- The satellite collector port, which follows this PR.
