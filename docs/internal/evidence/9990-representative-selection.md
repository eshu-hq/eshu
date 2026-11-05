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
the row directly, because the earlier fix had left that path reading the whole group.

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
  fix branches or remediation in 26. A further 376 shapes read the whole group in both orders and so
  were wrong without being order-dependent. The narrowing of a directly joined blank
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
- An independent review's wider grids, 1,146,960 fleet calls and 8,400 authorized
  calls, read changed 0 and lost 0. They are not committed here and ran before a
  reselected group, and a directly joined blank group, stopped reading the facts that
  cannot join.

What the grid cannot see:

- A row with no installed version. Every grid host carries `1.0-3`. The base decides
  `installed_version_missing` before it reads the ecosystem, so a blank representative
  that joins such a row already has a base row, and narrowing it would move that row.
  The narrowing leaves it alone, and `TestRowWithNoVersionKeepsTheWholeGroup` and
  `TestNarrowedDirectGroupReadsEveryRowTheRepresentativeJoins` cover it.
- Known-fixed. Every grid own-fact carries an affected list, which blocks the single-fix
  shortcut, and no shape has the host vendor's fix above the host, so the grid never
  produces `not_affected_known_fixed`.
- Other hosts and spellings. There is no redhat, fedora, centos, or amazonlinux host, and
  `dpkg` is the only spelling that normalizes to nothing and names a matcher. There is
  no whitespace ecosystem on the grid;
  `TestBlankEcosystemReadsTheFamilyAndASpelledOneKeepsItsMatcher` pins that at the matcher.
- The apk comparison. Alpine runs only with version `1.0-3`, so the grid reaches
  `installed_version_malformed` there and never the real apk matcher.

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
Spelled ecosystems that join, a row with no version, a consumer, and a group whose facts
all join keep their pick (`representative_direct_test.go`).

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
read, but with a consumer anchor it stays: on the Ubuntu host, a blank fact with a
`pkg:apk/alpine/curl` fact in the group reads `os_release_unverified` in one order and
`installed_version_malformed` in the other. The family from the joined row, in #10034
item 2, closes it. `os` reads the family the same way.
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
| `OSAdvisoryJoin/10x` | 4,232 | 4,249 | +17 |
| `OSAdvisoryJoin/miss-10x` | 2,245 | 2,267 | +22 |
| `OSFleetDigestFanout/fleet-10x` | 69,496 | 69,492 | -4 |
| `OSRepresentativeSelection/joined` | 214,681 | 219,180 | +4,499 |
| `OSRepresentativeSelection/language` | 82,089 | 86,089 | +4,000 |
| `OSRepresentativeSelection/no-joinable` | 69,590 | 79,590 | +10,000 |
| `OSRepresentativeSelection/reselected` | 92,591 | 192,665 | +100,074 |

- Against the previous head (`0aa2b5578a`), rebuilt the same way, no benchmark moves by
  more than 14 allocations (`OSRepresentativeSelection/joined` 219,174 to 219,180,
  `no-joinable` 79,590 to 79,590, `reselected` 192,670 to 192,665, `language` 86,090 to
  86,089). The narrowing of a directly joined group checks the ecosystem after the join
  check and before any row is collected, and every benchmark fact carries a spelled
  ecosystem, so none of them reaches the new work. The new work runs only for a
  blank-ecosystem representative that joins, which no producer writes today.
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
