# 9984: digest-less OS rows classify per release line

Issue #9984, part of epic #9209. Installed OS rows with no sibling-analysis
digest used to share one index, and the first matching row decided one finding
for the group. This note records what moved, what did not, and what the change
costs. The census, the benchmark runs, and the allocation counts below were
re-run on the branch commit titled "test(supplychain): benchmark the SBOM base
beside unattached digests", which holds the production code and the benchmarks
measured here (later commits change test assertions and documentation only),
against base `4a889112b1`, a merge commit on main. The runs were made before the
branch was rebased. A review later confirmed that the rebased commit has the same
`core` tree, and a rerun of the census and the allocation counts against main at
`b1dc9788e1` reproduced every count here. The commit is named by its subject
because a rebase changes its hash. The runs were on macOS arm64 with `go1.26.10`.
The host is shared and was busy: the one-minute load average read 34 to 63 during
the benchmark runs, which widens the timing bands (up to ±147%). Allocations and
bytes do not move with load, so they carry the weight. The figures are not a CI
baseline.

## What the change does

`splitOSPackagesByReleaseLine` partitions those rows by (release line, installed
version), and each group runs the existing manifest fanout. A group keeps the
least row of each lookup key by `compareOSPackageRows`, so the row it reads does
not depend on arrival. A line `osHostReleaseLine` cannot read keeps its own distro
and `distro_version`, because nothing proves two such values are one line.

The SBOM branch's base holds the same rows plus digests no attached SBOM names,
and first-row-wins applied there too, so it now runs the same fleet pass. That is
why the census class with an SBOM subject and unattached digests goes from 78
findings to 240: each unattached digest now gets its own finding, where one
matching row used to decide for the digests that shared a key. The no-SBOM digest
path (#9918) is byte-identical, with 0 rows moved.

When two shards produce findings with one writer identity but different reasons,
coalesce used to call it an SBOM component version conflict. That fits two SBOM
components, and it does not fit two digest-less lines whose purl names no distro
qualifier, where no SBOM was read. The label now needs an SBOM component on one of
the merged findings (`findingHasSBOMComponentPath`). Without one, the merge keeps
the lesser reason, so arrival order cannot change it, and the union of both
missing-evidence lists and rows. This was found in review and is pinned by `TestDigestlessFleetPURLWithoutDistroKeepsReasonWithoutSBOM`
and `TestCoalesceNamesAnSBOMConflictOnlyWhenAnSBOMWasRead`. The smaller fix is in
coalesce: putting the release line in the writer identity would change the
identity, and so the finding ID, of every finding on the digest path (#9918).

## Finding counts

The census takes the seven committed OS advisory records and the five records
that join no OS row. Each OS record meets every release line of its vendor crossed
with versions from its own fixes, in three row orders. The harness is a scratch test
file (`zz_census_9984_test.go`) and a comparison script, kept outside the
repository. It is not committed: the core package is at its directory file limit
and the repository has no test-support location for a one-off census. To rerun it,
copy the test file into a checkout of each side and compare the two outputs:

```text
cp zz_census_9984_test.go <checkout>/go/internal/reducer/supplychain/core/
cd <checkout>/go && CENSUS_OUT=<out>.json go test ./internal/reducer/supplychain/core/ -run TestZZCensus -count=1
python3 compare.py census-base.json census-head.json
```

The base checkout is `4a889112b1` and the head checkout is the commit named in
the opening paragraph.

| Class | Runs | Findings before | Findings after | Byte-identical runs | Fixtures flipping by order, before | After |
|---|---|---|---|---|---|---|
| Rows resolving to a digest | 21 | 219 | 219 | 21 | 0 | 0 |
| No OS join (control) | 15 | 27 | 27 | 15 | 0 | 0 |
| One digest-less row | 21 | 21 | 21 | 21 | 0 | 0 |
| SBOM subject, attached rows only | 21 | 21 | 21 | 21 | 0 | 0 |
| Digest-less fleet | 21 | 30 | 219 | 0 | 7 | 0 |
| Three hosts per group | 21 | 30 | 219 | 0 | 7 | 0 |
| Half the rows with a digest | 21 | 141 | 219 | 0 | 7 | 0 |
| SBOM subject plus digest-less rows | 21 | 51 | 240 | 0 | 7 | 0 |
| SBOM subject plus unattached digests | 21 | 78 | 240 | 0 | 7 | 0 |

A digest-less fleet now reaches the same verdict mix as the same rows with
digests: 45 `affected_exact`, 69 `not_affected_known_fixed`, and 105
`possibly_affected`.

## Rows move

Counting which finding cites each installed row, in the same row order:

| Class | Row observations | Same finding | Moved | Newly cited | Lost citation | Uncited both |
|---|---|---|---|---|---|---|
| Rows resolving to a digest | 219 | 219 | 0 | 0 | 0 | 0 |
| Digest-less fleet | 219 | 30 | 0 | 189 | 0 | 0 |
| Three hosts per group | 657 | 19 | 0 | 200 | 11 | 427 |
| Half the rows with a digest | 219 | 141 | 0 | 78 | 0 | 0 |
| SBOM subject plus digest-less rows | 240 | 51 | 0 | 189 | 0 | 0 |
| SBOM subject plus unattached digests | 240 | 78 | 0 | 162 | 0 | 0 |

The 11 lost citations are real. A group cites its least row, so a host that used
to be cited by arriving first can give way to another host in its group. Hosts in
one group share a line and a version, so they share a verdict, but a finding still
cites one of them. It did before this change too; the group is just smaller now.

## QA environment

Read-only on the Postgres replica, after confirming the AWS identity, with no
mutation. The QA environment holds 56 `vulnerability.os_package` rows, 28 of them
active, all in one scope on one distro version. Every package has one (line,
version) group, and no scope and generation holds two versions of one package.
That count was not re-run grouped by source key, the shape #10057 describes. It
holds no `vulnerability.affected_package` fact with an OS purl, and none of its
271966 findings carries an OS match derivation. So no finding can move there
today: the count before and after is the same because nothing joins, not because
the split was exercised. The split has not run in the QA environment.

## Cost

Benchmark Evidence: measured at the commit named in the opening paragraph against
base `4a889112b1`, ten
interleaved rounds per side with an alternating first mover, 300 iterations for the
fleet benchmarks and 50 for the join benchmark, compared with `benchstat`. The
one-minute load average read 34 to 51 during the run, so the timing bands run ±8 to
±147% and a "within noise" row says only that the difference is under the band.
Allocations do not move with load and are the stronger evidence. The scale names
1x, 5x, and 10x are 10, 50, and 100 images or rows. The base has no
`BenchmarkOSFleetDigestlessFanout`, so a copy of it ran in the base checkout.

| Benchmark | Base | Change | Result |
|---|---|---|---|
| `DigestFanout` 10 images | 421.6 µs ±54% | 434.1 µs ±55% | within noise, p=0.631; allocations 3654 to 3652 (-0.05%) |
| `DigestFanout` 50 images | 3.621 ms ±92% | 3.376 ms ±72% | within noise, p=0.853; allocations -0.01% |
| `DigestFanout` 100 images | 6.350 ms ±141% | 6.832 ms ±38% | within noise, p=1.000; allocations equal |
| `DigestlessFanout` same group, 10 rows | 41.6 µs ±27% | 41.3 µs ±36% | within noise, p=1.000; +6 allocations (+2.1%) |
| `DigestlessFanout` same group, 100 rows | 166.6 µs ±42% | 167.9 µs ±19% | within noise, p=0.912; +6 allocations (+0.6%) |
| `DigestlessFanout` spread, 10 rows | 46.5 µs ±21% | 439.5 µs ±33% | the same nine groups; allocations 288 to 3724 |
| `DigestlessFanout` spread, 100 rows | 162.2 µs ±35% | 568.8 µs ±118% | one finding becomes nine; allocations 1014 to 4451 |
| `OSAdvisoryJoin` 100 images | 714.2 µs ±22% | 1084.7 µs ±20% | 5 findings become 15; its rows have no digest |
| `OSAdvisoryJoin` no-join control | 340.9 µs ±34% | 340.1 µs ±15% | within noise, p=0.631; allocations equal |
| `OSAdvisoryJoin` registry control | 310.0 µs ±18% | 318.0 µs ±8% | within noise, p=0.912; allocations equal |

The added cost follows the findings the path now produces. Digest-less rows
classify once per group, so that part is bounded by the distinct (line, version)
pairs in the fleet and not by the host count. Image digests do not fold into
groups: each digest is its own group, as it has been on the no-SBOM path since
#9918. The first version sorted every group and read +44% on the same-group row at
100 rows. Keeping only each lookup key's least row removed that: this path has no
SBOM component, so the classifier reads one row per shard.

### SBOM base beside unattached digests

The SBOM base now takes the same pass, so its cost grows with the unattached
digests, which the table above does not cover.
`BenchmarkOSFleetSBOMBaseUnattachedDigests` runs one verified SBOM subject beside
1, 10, 100, and 1000 digests that each have a scan row and no SBOM, all on one line
and one version. Twelve interleaved rounds per side with an alternating first
mover, 30 iterations each, compared with `benchstat`. Host load was 57 to 63 during
this run (36 to 60 in an earlier identical run, whose bytes and allocations matched
to the digit).

| Unattached digests | Findings | Time | Bytes per op | Allocations per op |
|---|---|---|---|---|
| 1 | 2 to 2 | 84.7 µs ±39% to 106.4 µs ±21%, p=0.143 | 53.5 KiB to 54.6 KiB (+2.2%) | 456 to 468 (+2.6%) |
| 10 | 2 to 11 | 128.6 µs ±26% to 859.1 µs ±32% (+568%) | 77.5 KiB to 380.6 KiB (+391%) | 588 to 3516 (+498%) |
| 100 | 2 to 101 | 416 µs ±70% to 7.81 ms ±69% (+1776%) | 275 KiB to 3.56 MiB (+1223%) | 1.77k to 35.6k (+1910%) |
| 1000 | 2 to 1001 | 5.61 ms ±42% to 67.4 ms ±23% (+1101%) | 2.78 MiB to 35.6 MiB (+1179%) | 13.5k to 338k (+2403%) |

The cost is linear in unattached digests, about 34 KiB and 325 allocations for each
added finding. Time per added finding reads 28.6 µs at 1000 digests in a review run at
load 18 to 20 (30.8 ms less 2.3 ms, over 999 added findings) and about 60 µs in the
loaded run above; the no-SBOM path in the table above costs about 70 µs for each image at 100 images.
Before this change the SBOM base produced one finding for all of these digests,
because the first matching row decided for the group. That was the wrong answer for
every digest but one. Each digest is one image with its own installed rows, so it
gets its own finding, and the cost follows the findings.

No-Regression Evidence: digest-resolved and non-OS advisories are byte-identical
in the census above. At the final head, allocations per op of
`BenchmarkOSFleetDigestFanout` moved by -0.05% at 10 images (3654 to 3652), -0.01%
at 50 images, and not at all at 100 images (p=0.250), over ten rounds each.

No-Observability-Change: the split emits no new metric, span, log key, or status.
Its findings reuse the persisted finding and the existing counters. Finding-level
counts such as `eshu_dp_supply_chain_impact_os_release_not_in_advisory_total` rise
because a foreign-line host now has a finding of its own.

## Limits

- The release label on `eshu_dp_supply_chain_impact_os_advisory_release_selections_total`
  still reads the first row of the fleet index, so a fact joined by rows on two
  lines can report a different label in another row order. The findings do not
  depend on it. Tracked in #10056.
- One digest holding two versions of a package key still picks by row order, with
  no change here. #9858's closing comment named this as a theory, and a source-key
  join makes the shape ordinary. Debian's `bsdutils` (`1:2.38.1-5+deb12u3`) and
  `libblkid1` (`2.38.1-5+deb12u3`) both come from source `util-linux`, so one source
  key holds two versions that differ by an epoch (package pages for bookworm, as
  read in review). A probe with synthetic versions in that shape gives
  `affected_exact` in one row order and `possibly_affected` in the other. That both
  packages sit in one scanned image is inferred, not observed. It applies to an
  unattached digest on the SBOM base too. Tracked in #10057.
- Rows whose purl names no distro qualifier share a writer identity across lines.
  Shards with one verdict merge their evidence into one finding that keeps the first
  line's fix; `TestDigestlessFleetPURLWithoutDistroCoalescesAcrossLines` pins this.
  Shards with different reasons under one status merge too. The merged finding keeps
  the lesser reason and both missing-evidence lists and is not labeled an SBOM
  conflict (`TestDigestlessFleetPURLWithoutDistroKeepsReasonWithoutSBOM`). Nothing
  requires the qualifier, and this note did not measure how often a producer leaves
  it out.
- A merge that keeps the lesser reason also takes the priority and fix that
  reason yields. In a probe with a debian-11 advisory (rerun at this head, both
  row orders), a row on line 10 merged with
  a row on `sid` scored 11 with no fix, where the `sid` row alone scored 16 with fix
  `7.88.1-10+deb12u3`. Before this change the answer was 11 or 16 depending on row
  order; now it is always the lesser reason's. The status does not change.
