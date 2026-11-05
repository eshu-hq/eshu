# Representative selection joins the installed row (#9990)

One fact names a (CVE, package id) group. Source order picks it, and on a scanned
host a fact from another source can win that pick and join no installed row. With
no SBOM path and no consuming repository the group then has no anchor, and the
finding disappears. The fix lets the facts of the group that do join name and decide
the finding, for a group with no SBOM path and no consuming repository, and counts the
groups no fact can join. It applies when the source-order fact joins nothing, and when
a blank-ecosystem fact joins beside a fact that cannot (second ruling below).

Decisions in this change come from the arbiter ruling on #9990: the ordering rule
left this branch, the reselection ranks under the vendor of the row a fact joins, a
blank ecosystem reads the version family from the group's OS purls, and a joined row
with no family to read stays visible. A later ruling (9914-r5, decision 3) classifies a
reselected group from the facts that join, so a fact that cannot join offers no fix. A
third (9990-orderdep, decision 3.5) does the same for a blank-ecosystem fact that joins
the row directly, because the earlier fix had left that path reading the whole group. A
fourth (9990-f6) splits a blank group whose joined rows are mixed, by row: a row with an
installed version is decided by the facts that join, and a row with no version keeps the
whole-group read it has at the base.

No-Regression Evidence: a parity differential on `BuildSupplyChainImpactFindings`
against base `b16cb9689b`, with the branch's production files. The harness and the
commands to rerun it are in [9990-parity](9990-parity/README.md). It records the
input digest and the output of every call on the package's own test corpus, and on a
grid: 5 hosts (ubuntu, debian, alpine, alma, rocky) x 11 own-fact ecosystems x 11
other-fact ecosystems x 10 sources x other-fact purl on or off x 4 anchors (none,
consumption, SBOM, analysis), each in 2 fact orders, plus a language control. That is
49,480 shapes and 98,960 grid calls, 2,160 of them language calls.

| Measured against base `b16cb9689b` | Result |
| --- | --- |
| Test-corpus calls changed (519 fleet, 42 authorized) | 0 |
| Language grid calls changed | 0 of 2,160 |
| Grid calls whose row changed | 0 |
| Grid calls that lost a row | 0 |
| Grid calls with a new row where the base had none | 17,554 |
| Shapes whose findings depend on fact order | 7,860 (7,860 at the base) |
| Shapes order-dependent at the head and not at the base | 0 |

- "Changed" and "lost" are 0 in both fact orders of every shape, so a finding that
  exists at the base is byte-identical.
- On this grid the new rows' findings (18,254) are `affected_exact` /
  `dpkg_exact_affected_version` (6,410), `possibly_affected` / `os_release_unverified`
  (8,880), `possibly_affected` / `installed_version_malformed` (2,860), and
  `possibly_affected` / `unsupported_ecosystem` (104). A blank own-fact ecosystem
  accounts for 6,186 of those findings. This is what the grid produces, not every
  verdict a new row can carry: see "What the grid cannot see" below.
- The 104 `unsupported_ecosystem` findings are a blank joining fact with no purl whose
  group holds no other joining fact, and whose only OS purl belongs to a fact that
  cannot join (for example the vendor's fact spelled `debian` on an Ubuntu host). The
  group is decided by the facts that join, so the blank fact no longer borrows its
  version family from that purl, and reads what the same fact reads alone. Before the
  narrowing these were `affected_exact` (48), `installed_version_malformed` (28) and
  `os_release_unverified` (28). The exact verdict returns when the family is read from
  the joined row's package manager (#10034 item 2), which also changes existing `os`
  rows, so it needs its own grid.
- Order-dependence is the base's set and none is added. An earlier head (`0aa2b5578a`)
  read 8,146 shapes, 286 more than the base's 7,860, because a blank-ecosystem fact that
  joined the row directly was still classified from the whole group while the same group
  in the other fact order was classified from the facts that join. In those 286 shapes
  the base has no row in either order. The two orders differed in the evidence and
  advisory sources in all 286, the advisory id in 80, the fixed version in 4, and the
  fix branches or remediation in 30 (26 of them with the same fixed version). A further
  376 shapes read the whole group in both orders and so were wrong without being
  order-dependent. The narrowing of a directly joined blank
  group closes both; the head now reads 7,860, the same shapes as the base, with
  `compare.py` printing 0 shapes order-dependent at the head and not at the base.
- The 7,860 shapes that the base already has are #10034 item 1. A group whose facts
  disagree on the ecosystem still ranks its pick under the first fact's ecosystem. Of
  those shapes, 4,654 have a base row in both orders (1,184 differ by row count, 774 by
  verdict, 214 by fix, 2,482 by evidence only), and 3,206 have a base row in one order
  only (261 by row count, 156 by verdict, 134 by fix, 776 by advisory id, 1,879 by
  evidence only). In the second class this change adds a row in the other order, decided
  by the facts that join while the base row is decided by the whole group.
  The outcome label differs by order for a mixed group in the same way: the order that
  joins directly counts `joined`, the other counts `reselected`.
- An SBOM path, a consumer, and a joined row are the only sources
  `supplyChainImpactFindingHasOwnedAnchor` can read, and the reselection runs only
  when all three are absent, so it can only reach a group that yields no finding.
- The family lookup reads a raw blank ecosystem only. The first attempt read any
  ecosystem the identity table does not normalize, which moved 880 existing rows: all
  of them had the ecosystem `dpkg` and an OS purl of another family.
  `TestBlankEcosystemReadsTheFamilyAndASpelledOneKeepsItsMatcher` pins the narrow rule.
- The unsupported-matcher exemption moves 0 grid calls, because every grid own-fact
  carries an OS purl. `TestJoinedRowWithNoDerivableFamilyStaysVisible` and
  `TestBlankEcosystemFactWithNoOSEvidenceIsStillDropped` cover it.
- The authorized path shares the builder. This grid covers it only through its 42
  corpus calls, plus `TestAuthorizedScanRepresentativeJoinsTheInstalledRow` and
  `TestReselectedGroupIsDecidedByTheFactsThatJoin`.
- An independent review's wider grids, with one installed row per host, read changed 0
  and lost 0 at `599b7642a4`: fleet 1,146,960 calls (210,020 with a new row, 936,940
  identical) and authorized 8,400 calls (2,280 with a new row, 6,120 identical). They
  are not committed here, and the split of ruling 9990-f6 cannot fire on a host with one
  row, so they were not rerun at this head.

What the grid cannot see:

- A row with no installed version. Every grid host carries `1.0-3`. The base decides
  `installed_version_missing` before it reads the ecosystem, so a blank representative
  that joins such a row already has a base row, and narrowing it would move that row.
  The narrowing leaves it alone, and `TestRowWithNoVersionKeepsTheWholeGroup` and
  `TestNarrowedDirectGroupReadsEveryRowTheRepresentativeJoins` cover it. A host fleet
  with both kinds of row is the multi-row grid below, not this one.
- Known-fixed. Every grid own-fact carries an affected list, which blocks the single-fix
  shortcut, and no shape has the host vendor's fix above the host, so the grid never
  produces `not_affected_known_fixed`.
- Other hosts and spellings. There is no redhat, fedora, centos, or amazonlinux host, and
  `dpkg` is the only spelling that normalizes to nothing and names a matcher. There is
  no whitespace ecosystem on the grid;
  `TestBlankEcosystemReadsTheFamilyAndASpelledOneKeepsItsMatcher` pins that at the matcher.
- The apk comparison. Alpine runs only with version `1.0-3`, so the grid reaches
  `installed_version_malformed` there and never the real apk matcher.

Multi-row grid (ruling 9990-f6): the grid above gives every host one installed row, so it
cannot see a group whose joined rows are mixed. A second harness, in
[9990-parity](9990-parity/README.md), has 3 hosts x 13 row topologies (one to three rows,
with and without an image digest, with and without an installed version) x 5 own-fact
ecosystems x 2 fact shapes x 6 other facts x 3 anchors. That is 7,020 shapes and 42,120
calls: each shape in both fact orders and both row orders, plus the own fact alone as a
control. Run on private worktrees at the base `b16cb9689b`, the previous head
`599b7642a4`, and this branch's code:

| Versioned-row findings against the own-only control | Findings | Differ | False `not_affected_known_fixed` |
| --- | --- | --- | --- |
| Previous head (`599b7642a4`) | 2,112 | 280 | 64 |
| This head, split by row | 2,152 | 0 | 0 |

- Base rows moved: 0, in both builds. Against the base, this head leaves 12,692 calls
  identical and adds a row on 15,388 (the previous head added 15,332; the 56 extra are
  the row with no version, which the split now reads when it shares one index with the
  versioned row).
- Authorized path, 288 calls: base rows moved 0. The previous head and this head differ
  only where one target holds both rows (16 corrected, 16 added); two targets are equal.
- Shapes order-dependent by fact order or row order, against this branch's base: 3,348 at
  the base, 3,708 at the previous head and 3,700 here. 360 are order-dependent here and
  not at the base, and 8 are the reverse. The previous head has the same 360, so the
  split did not add them. Main has since changed how digest-less rows (#10111) and
  per-digest rows (#10143) are classified, so this figure overstates what a merge leaves.
- The same grid against main, my own runs, tip merged into each main (the prose
  conflicts resolved by keeping this branch; no Go file conflicts):

  | Main | Order-dependent on main | Order-dependent with this branch | Added | Removed | Base rows moved | Versioned rows vs control (findings, differ, false known-fixed) |
  | --- | --- | --- | --- | --- | --- | --- |
  | `eaa15d68d8`, the parent of #10143 | 2,926 | 2,992 | 70 | 4 | 0 | 2,420, 0, 0 |
  | `35023cc39d`, current, holds #10143 | 2,852 | 2,852 | 0 | 0 | 0 | 2,496, 0, 0 |

  Against `eaa15d68d8` the 70 shapes added are all topology `T3-same-digest-v-n` (two
  rows of one digest, one versioned and one not); no digest-less shape is above main
  there. On rows sharing one digest the order-dependent count is above main until #10143
  lands. It has landed: with #10143 on main the added and removed sets are empty
  (arbiter ruling 9990-f6, 2.8). Identical calls against main: 12,628 at `eaa15d68d8` and
  12,564 at `35023cc39d`; the rest only add a row. The authorized path, 288 calls, moves
  no row against either main (144 identical and 144 only-added at `35023cc39d`; 152 and
  136 at `eaa15d68d8`). This does not change the committed grid's figure of "none
  added", which is about its own 49,480 shapes.
- This note did not isolate why a shared shard reads in an order-dependent way. The
  ruling's reading is that the classifier reads one row per shard.
- A row with no installed version keeps the base's whole-group read, with the other fact
  in its evidence, in the order where the blank fact is the source-order representative
  (`TestVersionlessRowKeepsTheBaseWholeGroupRead`).

Fixed here: on an Ubuntu 22.04 host at `1.0-3`, take Ubuntu's own fact with a purl and
`fixed_versions` of `1.0-5`, plus an OSV fact with no purl and `fixed_versions` of
`1.0-2`, spelled blank, `deb`, or `dpkg`, with no consumer or SBOM. That group used to
read `not_affected_known_fixed` from the OSV fix. It now reads `possibly_affected` /
`version_not_in_advisory_range` with `1.0-5`, on both paths and in every fact order
(`TestReselectedGroupIsDecidedByTheFactsThatJoin`).

The same fixture with the two facts spelled differently used to fail on the direct
path. Take the vendor's fact spelled blank (purl, `fixed_versions` of `1.0-5`) and an
`osv` fact spelled `ubuntu` with no purl and a fix of `1.0-2`, with no consumer or SBOM.
In 12 of the 24 fact orders the blank vendor fact joined the row directly and the group
was read whole: the host read `not_affected_known_fixed` from the OSV fact that cannot
join. It now reads `possibly_affected` /
`version_not_in_advisory_range` with `1.0-5` in every order
(`TestDirectlyJoinedBlankGroupIsDecidedByTheFactsThatJoin`), and every order equals the
finding built without the fact that cannot join
(`TestFactThatCannotJoinChangesNothingOnADirectBlankRow`). A blank vendor fact whose fix
is below the host reads `not_affected_known_fixed` in every order, not
`os_release_unverified` in half of them (`TestKnownFixedVerdictDoesNotDependOnFactOrder`).
Spelled ecosystems that join (`os` and `dpkg`), a row with no version, a consumer, and a
group whose facts all join keep their pick (`representative_direct_test.go`).

A third shape used to fail the same way across rows. Take the same two facts and two
images, one at `1.0-3` and one with no installed version. The row with no version has a
base row (`installed_version_missing`, decided before the ecosystem is read), so the
narrowing leaves it alone. But that one row also turned the narrowing off for the
versioned row, which has no base row, and the fact that cannot join decided it: in 3 of
6 fact orders the host at `1.0-3` read `not_affected_known_fixed` against the vendor's fix
of `1.0-5`. The authorized path got it right only because each target has its own index.
A group whose joined rows are mixed now splits by row before anything else runs. The
versioned row is decided by the facts that join, and the row with no version keeps the
whole-group read it has at the base, with the other fact in its evidence
(`TestVersionedRowBesideAVersionlessRowIsDecidedByTheFactsThatJoin`,
`TestVersionedRowEqualsTheOwnOnlyControl`, `TestVersionlessRowKeepsTheBaseWholeGroupRead`).
The split is by row, not per shard, because a digest shard can hold both kinds of row.

Known class, #10034 item 5: a group that is not reselected still ranks its fix by the
ecosystem spelling, not by the vendor of the joined row. Add a consuming repository to
that shape and it is never reselected: blank, `deb`, and `dpkg` read
`not_affected_known_fixed` / `dpkg_known_fixed` with `1.0-2` from the other source,
whether `nvd` or `osv`, while `ubuntu` and `os` read `version_not_in_advisory_range` with
`1.0-5`. The `deb` and `dpkg` rows exist at the base, so fixing this moves existing rows
and needs its own grid; the blank row is new here and reads what those spellings read.
Reach today is zero, from source and not live data: no vendor `affected_package`
producer exists, OSV skips a blank ecosystem and writes `os`, and GLAD rejects a slug with
no ecosystem. It must land before any OS-vendor producer.

Known limit: `representativeAffectedPackage` still ranks a group under its first
fact's ecosystem, so a group whose facts disagree on the ecosystem can depend on fact
order, as it does at the base. A trial fix here moved 4,950 existing rows, so it is
#10034, which must land before any OS-vendor `affected_package` producer. A blank fact
also reads the group's first OS purl. Reselection now removes a foreign purl from that
read, but it stays with a consumer anchor, and with no anchor at all when every fact of
the group joins. On the Ubuntu host, a blank fact with a `pkg:apk/alpine/curl` fact in
the group reads `os_release_unverified` in one order and `installed_version_malformed`
in the other. On a Debian host, a blank vendor fact beside a blank fact from `debian`
that carries that purl reads `affected_exact` in one order and
`installed_version_malformed` in the other, with no row at the base in either. The family
from the joined row, in #10034 item 2, closes both. `os` reads the family the same way.
From source, not live data: the only non-reducer writers of the fact kind are
`collector/vulnerabilityintelligence/envelope.go` (OSV, which writes `os` for an OS
package) and `gitlab_gemnasium_envelope.go` (GLAD, which writes the raw slug prefix of
a language package). They can disagree on a language package's spelling, such as `gem`
and `rubygems`, and every language spelling ranks under one table
(`TestLanguageGroupKeepsItsPickAcrossEcosystemSpellings`).

Benchmark Evidence: allocation counts at baseline `b16cb9689b` against this branch's
production files, at `-benchtime=5x -benchmem`. They do not depend on host load. The
input for `OSRepresentativeSelection` is 500 groups of 2-3 facts with one installed row
each, with no backend because the builder is pure. The timing comparison needs load1
under 9, interleaved runs, `-count` of 10 or more, and `benchstat`; it is recorded in
the table below only once a quiet host has run it.

| Benchmark | Base allocs | Head allocs | Delta |
| --- | --- | --- | --- |
| `OSAdvisoryJoin/10x` | 4,232 | 4,245 | +13 |
| `OSAdvisoryJoin/miss-10x` | 2,245 | 2,267 | +22 |
| `OSFleetDigestFanout/fleet-10x` | 69,496 | 69,511 | +15 |
| `OSRepresentativeSelection/joined` | 214,681 | 219,179 | +4,498 |
| `OSRepresentativeSelection/language` | 82,089 | 86,089 | +4,000 |
| `OSRepresentativeSelection/no-joinable` | 69,590 | 79,592 | +10,002 |
| `OSRepresentativeSelection/reselected` | 92,591 | 192,676 | +100,085 |

- Against the previous head (`0aa2b5578a`), rebuilt the same way, no benchmark moves by
  more than 14 allocations (`OSRepresentativeSelection/joined` 219,174 to 219,180,
  `no-joinable` 79,590 to 79,590, `reselected` 192,670 to 192,665, `language` 86,090 to
  86,089). The narrowing of a directly joined group checks the ecosystem after the join
  check and before any row is collected, and every benchmark fact carries a spelled
  ecosystem, so none of them reaches the new work. The new work runs only for a
  blank-ecosystem representative that joins, which no producer writes today.
- Against the previous head (`599b7642a4`), medians of 3 runs, the committed benchmarks
  move by at most 5 allocations (`joined` 219,179 to 219,179, `no-joinable` 79,592 to
  79,592, `reselected` 192,675 to 192,676, `OSFleetDigestFanout/fleet-10x` 69,506 to
  69,511), inside a run-to-run spread of up to 21. The row split costs a spelled
  ecosystem two comparisons and one trim.
- `BenchmarkSplitDirectBlankGroup`, 100 hosts that each carry a digest, previous head to
  this head: `spelled-all-versioned` 31,068 to 31,063; `spelled-one-versionless` 38,093
  to 38,094; `blank-all-versioned` 27,886 to 27,904 (+18); `blank-all-versionless`
  30,142 to 30,169 (+27); `blank-one-versionless` 37,914 to 28,016. The blank cases
  pay one more row walk, about 20 to 30 allocations a group; no producer writes them. A
  blank group with mixed rows falls by about 9,900, because the versioned rows now
  classify on one fact instead of two. Timing is not read.
- `reselected` is new work, not a slowdown: at the base those groups yield nothing,
  and at head they cost what a joined group costs and yield the finding.
- `no-joinable` is the largest cost on a group that still yields nothing: about 20
  allocations per group. A profile of that shape puts it in
  `affectedOSPackageLookupKeys`, reached through `firstOSPackageImpactPath` per fact.
  If the quiet-host delta on `no-joinable` or `joined` is over 10%, the duplicate
  lookup between `withJoinableOSRepresentative` and `recordOSRepresentativeOutcome`
  comes out before push.
- An earlier timing at load 35-66 showed `no-joinable` at +13.2% with a 12-13%
  run-to-run range. That load makes it non-comparable, so it decides nothing.

Observability Evidence: `eshu_dp_supply_chain_impact_os_representative_joins_total`
(counter, labels `source` and `outcome`) and the `os_representatives_*` success-log
sub-signals, one point per OS-shaped group per index the pass evaluates.
`TestRecordBuildStatsWritesOSRepresentativeSignals` reads both surfaces through the
real handler method and a manual metric reader, and
`TestAuthorizedPassFoldsRepresentativeOutcomesPerTarget` pins the per-index count.
A rising `no_joinable_representative` names groups with rows that no fact can join,
and the [telemetry reference](../../public/reference/telemetry/supply-chain-active-evidence.md)
says what to read next. A residual finding carries `unsupported_ecosystem` and its
missing evidence and counts in `joined` or `reselected`; it has no counter of its own.

Why this is safe: the choice reads only the group and the index, so a retry gives
the same answer, and the mark goes on a copy because the index shares the group
across installed rows and scan targets. No queue, lease, worker, or write path
changes. The terminal counts are the rows in the tables above; no reducer or graph
run was performed for this note.
