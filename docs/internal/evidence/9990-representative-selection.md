# Representative selection joins the installed row (#9990)

One fact names a (CVE, package id) group. Source order picks it, and on a scanned
host a fact from another source can win that pick and join no installed row. With
no SBOM path and no consuming repository the group then has no anchor, and the
finding disappears. The fix lets a fact of the group that does join name the
finding, only for a group with no anchor, and counts the groups no fact can join.

Decisions in this change come from the arbiter ruling on #9990: the ordering rule
left this branch, the reselection ranks under the vendor of the row a fact joins, a
blank ecosystem reads the version family from the group's OS purls, and a joined row
with no family to read stays visible.

No-Regression Evidence: a parity differential on `BuildSupplyChainImpactFindings`
against base `2002489ef2`, with the branch's production files. The harness and the
commands to rerun it are in [9990-parity](9990-parity/README.md). It records the
input digest and the output of every call on the package's own test corpus, and on a
grid: 5 hosts (ubuntu, debian, alpine, alma, rocky) x 11 own-fact ecosystems x 11
other-fact ecosystems x 10 sources x other-fact purl on or off x 4 anchors (none,
consumption, SBOM, analysis), each in 2 fact orders, plus a language control. That is
49,480 shapes and 98,960 grid calls, 2,160 of them language calls.

| Measured against base `2002489ef2` | Result |
| --- | --- |
| Test-corpus calls changed (519 fleet, 42 authorized) | 0 |
| Language grid calls changed | 0 of 2,160 |
| Grid calls whose row changed | 0 |
| Grid calls that lost a row | 0 |
| Grid calls with a new row where the base had none | 17,554 |
| Shapes whose findings depend on fact order | 5,256 (7,860 at the base) |

- "Changed" and "lost" are 0 in both fact orders of every shape, so a finding that
  exists at the base is byte-identical.
- The new rows' findings are `affected_exact` / `dpkg_exact_affected_version` (6,482),
  `possibly_affected` / `os_release_unverified` (8,884), and `possibly_affected` /
  `installed_version_malformed` (2,888). A blank own-fact ecosystem accounts for
  6,186 of those findings. None is `unsupported_ecosystem`.
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
- The authorized path shares the builder. It is covered here by its 42 corpus calls
  and by `TestAuthorizedScanRepresentativeJoinsTheInstalledRow`, not by the grid.

Known limit: `representativeAffectedPackage` still ranks a group under its first
fact's ecosystem, so a group whose facts disagree on the ecosystem can depend on fact
order, as it does at the base. A trial fix here moved 4,950 existing rows, so it is a
separate issue that must land before any OS-vendor `affected_package` producer.
From source, not live data: the only non-reducer writers of the fact kind are
`collector/vulnerabilityintelligence/envelope.go` (OSV, which writes `os` for an OS
package) and `gitlab_gemnasium_envelope.go` (GLAD, which writes the raw slug prefix of
a language package). They can disagree on a language package's spelling, such as `gem`
and `rubygems`, and every language spelling ranks under one table
(`TestLanguageGroupKeepsItsPickAcrossEcosystemSpellings`).

Benchmark Evidence: allocation counts at baseline `2002489ef2` against this branch's
production files, at `-benchtime=5x -benchmem`. They do not depend on host load. The
input for `OSRepresentativeSelection` is 500 groups of 2-3 facts with one installed row
each, with no backend because the builder is pure. The timing comparison needs load1
under 9, interleaved runs, `-count` of 10 or more, and `benchstat`; it is recorded in
the table below only once a quiet host has run it.

| Benchmark | Base allocs | Head allocs | Delta |
| --- | --- | --- | --- |
| `OSAdvisoryJoin/10x` | 4,238 | 4,246 | +8 |
| `OSAdvisoryJoin/miss-10x` | 2,245 | 2,270 | +25 |
| `OSFleetDigestFanout/fleet-10x` | 69,487 | 69,496 | +9 |
| `OSRepresentativeSelection/joined` | 214,677 | 219,182 | +4,505 |
| `OSRepresentativeSelection/language` | 82,089 | 86,091 | +4,002 |
| `OSRepresentativeSelection/no-joinable` | 69,590 | 79,591 | +10,001 |
| `OSRepresentativeSelection/reselected` | 92,592 | 198,171 | +105,579 |

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
