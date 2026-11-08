# #9222 readback: owned package@version count and rotation math on current QA

Read-only readback for `eshu-hq/eshu#9222` (Wave 1 item 0.2: size
`target_limit` from the measured owned package@version count).
Measured 2026-11-08 against the shared test cluster and
`origin/main` `2d477a517d`. No configuration was changed and nothing
was written outside this note.

## QA state today (read-only)

- The vulnerability-intelligence collector activation (component
  activations configmap, vulnerability-intelligence key) sets no
  `target_limit`
  and no `planning_mode` under `derive_from_owned_packages`, so the
  effective value is the code default: 100, rotating mode
  (`defaultDerivedVulnerabilityTargets` in
  `go/internal/coordinator/vulnerability/intelligence_derivation.go`).
  The owner's `target_limit: 1500` deploy change (chosen 2026-11-01)
  is still held, not merged.
- Ecosystems configured: npm, pypi, go, maven, nuget, composer,
  rubygems, cargo, pub, hex, swift (11).
- Collector image: `ghcr.io/eshu-hq/eshu:sha-93b1af01` (main at the
  PR #10385 merge).

## Measured T

`COUNT(DISTINCT (ecosystem, package_name, version))` over the
reader's own predicate
(`go/internal/storage/postgres/owned_package_targets.go`: active
generation set, `content_entity` / `git` / `Variable` /
`config_kind = dependency`, raw `package_manager` in the
`OwnedPackageManagerAliases` expansion of the 11 configured
ecosystems plus `sbt`, `generation.status = 'active'`),
read-only transaction, `statement_timeout` 120 s, on the read
replica. Result: **T = 32,828** (2026-11-01: 32,729; +0.3%).

| ecosystem | targets |
| --- | --- |
| npm | 28,467 |
| pypi | 2,044 |
| composer | 788 |
| maven | 662 |
| go | 499 |
| rubygems | 355 |
| nuget | 7 |
| cargo | 3 |
| sbt | 2 |
| pub | 1 |
| hex, swift | 0 |

The count matches the reader's `distinct_targets` cardinality: the
extra `ORDER BY` columns only pick the representative row. No
repository, package, or advisory identity from QA appears here.

## Rotation math at T = 32,828

P is the smallest power of two with `P * L >= 2 * T`; one rotation
is P passes at the 30-minute reconcile interval.

| target_limit | P | rotation |
| --- | --- | --- |
| 100 (current effective) | 1024 | 512 h (21.3 d) |
| 1000 | 128 | 64 h |
| 1500 (owner-selected) | 64 | 32 h |
| 2000 | 64 | 32 h |

At L = 100 the full rotation (21.3 days) exceeds the 24 h
`max_staleness` default by a factor of 21: the issue stands. The
owner-selected 1500 holds P = 64 while T stays at or below 48,000.

## Criterion sweep

- Owned package@version count measured before selecting the limit:
  done (29,709 on 2026-10-27; 30,091/30,669 on 2026-10-28;
  31,820 on 2026-10-29; 32,729 on 2026-11-01; 32,828 here).
  Limit 1500 was selected from the 2026-11-01 count.
- Coordinator plan time and claim rate before and after: plan time
  before is measured (about 99 s per pass on 2026-10-27; about 14 s
  `EXPLAIN (ANALYZE)` on 2026-10-28). Claim-rate before/after and
  plan-time after are NOT measured: the deploy change never merged,
  so there is no after window.
- Derived set covers owned packages within one rotation in QA: NOT
  proven, blocked on the same unmerged deploy change.

## Unblock

Merge the held `target_limit: 1500` deploy change, then measure
coordinator plan time and claim rate before/after and verify that
the derived set covers the owned packages within one rotation.
No `eshu` code change is warranted: Phase 0 is configuration-only
and the default stays 100 by design.
