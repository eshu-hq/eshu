# 9984: digest-less OS rows classify per release line

Issue #9984, part of epic #9209. Installed OS rows with no sibling-analysis
digest used to share one index, and the first matching row decided one finding
for the group. This note records what moved, what did not, and what the change
costs. Figures come from commit `3d35d87318` against base `73032877fd`, on macOS
arm64 with `go1.26.10` and a shared host (load 45 to 47 during the benchmark run,
so the timing bands are wide). They are not a CI baseline.

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

## Finding counts

The census takes the seven committed OS advisory records and the five records
that join no OS row. Each OS record meets every release line of its vendor crossed
with versions from its own fixes, in three row orders. The harness is scratch and
is not committed.

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
version) group, and no scope and generation holds two versions of one package. It
holds no `vulnerability.affected_package` fact with an OS purl, and none of its
271966 findings carries an OS match derivation. So no finding can move there
today: the count before and after is the same because nothing joins, not because
the split was exercised. The split has not run in the QA environment.

## Cost

Benchmark Evidence: interleaved with an alternating first mover, eight rounds per
binary, 300 iterations for the fleet benchmarks and 50 for the join benchmark.

| Benchmark | Base | Change | Result |
|---|---|---|---|
| `BenchmarkOSFleetDigestFanout` 10 images | 493.0 µs | 455.9 µs | within noise, p=0.279 |
| `BenchmarkOSFleetDigestFanout` 50 images | 3.494 ms | 3.660 ms | within noise, p=0.878 |
| `BenchmarkOSFleetDigestFanout` 100 images | 6.971 ms | 7.614 ms | within noise, p=0.505 |
| `BenchmarkOSFleetDigestlessFanout` same group, 100 rows | 178.1 µs | 177.4 µs | within noise, p=0.878; +6 allocations |
| `BenchmarkOSFleetDigestlessFanout` spread, 100 rows | 169.4 µs | 594.2 µs | one finding becomes nine |
| `BenchmarkOSAdvisoryJoin` 100 images | 729.3 µs | 1120.3 µs | 5 findings become 15; its rows have no digest |
| `BenchmarkOSAdvisoryJoin` no-join control | 372.1 µs | 348.3 µs | -6.4%, p=0.038, allocations equal; read as host noise |
| `BenchmarkOSAdvisoryJoin` registry control | 354.6 µs | 325.9 µs | within noise, p=0.195 |

The added cost follows the findings the path now produces. Classifying runs once
per group, so it is bounded by the distinct (line, version) pairs in the fleet and
not by the host count. The first version sorted every group and read +44% on the
same-group row at 100 rows. Keeping only each lookup key's least row removed that:
this path has no SBOM component, so the classifier reads one row per shard.

No-Regression Evidence: digest-resolved and non-OS advisories are byte-identical
in the census above, and the `BenchmarkOSFleetDigestFanout` allocation count moved
by at most 0.05%.

No-Observability-Change: the split emits no new metric, span, log key, or status.
Its findings reuse the persisted finding and the existing counters. Finding-level
counts such as `eshu_dp_supply_chain_impact_os_release_not_in_advisory_total` rise
because a foreign-line host now has a finding of its own.

## Limits

- The release label on `eshu_dp_supply_chain_impact_os_advisory_release_selections_total`
  still reads the first row of the fleet index, so a fact joined by rows on two
  lines can report a different label in another row order. The findings do not
  depend on it.
- One digest holding two versions of a package still picks by row order. #9858's
  closing comment named this as a theory. A scratch probe confirms the code path,
  and the QA environment has no row in that shape.
- Rows whose purl names no distro qualifier share a writer identity across lines.
  Shards with one verdict merge their evidence into one finding, and that finding
  keeps the first line's fix. `TestDigestlessFleetPURLWithoutDistroCoalescesAcrossLines`
  pins this. Nothing requires the qualifier, and this note did not measure how
  often a producer leaves it out.
