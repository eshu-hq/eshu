# 10057: a digest classifies once per installed version

Issue #10057, part of epic #9209. One image digest that holds a package key at
two installed versions used to classify once, over whichever row arrived first.
With a fix in the advisory fact, one row order read `not_affected_known_fixed`
for a digest that also holds a version below the fix, and the other row order
read `possibly_affected`. This note records what moved, what did not, and what
the change costs. The change builds on #9984, whose census is the baseline.

"The base" in the tables below is `99206f6102`, the #9984 head this branch was
first stacked on. #9984 has since merged as the squash commit `622c948497`, and
the branch is rebased onto it: the five original commits kept their patch ids, the
production files this change touches are byte-identical on both, and the tests pass
at the rebased tip. The census, mutants and the first benchmark run ran before the
rebase, on the branch commit titled "test(supplychain): pin the per-version split
for a digest on the SBOM base" and its parent "fix(supplychain): classify a digest
once per installed version", which hold the production change (later commits touch
tests and docs only). A later commit, "perf(supplychain): compute each finding's
sort key once", moves the final sort's key out of the comparator; the census was
rerun after it (see Cost). Commits are named by subject because a rebase changes
the hash. The runs were on macOS arm64 with `go1.26.10`. The host is shared, so
allocations carry the weight and the timings are not a CI baseline; each run below
states the load it saw.

## What the change does

`appendSupplyChainImpactFleetOSFindings` ran each digest's rows through one
index, and the classifier reads one row per index through
`firstOSPackageImpactPath`. Each digest's rows now go through
`splitOSPackagesByReleaseLine`, the splitter #9984 added for digest-less rows, so
the digest gets one finding per (release line, installed version) it holds. A
shard keeps the least row of each lookup key by `compareOSPackageRows`, so the
row a finding reads, and its evidence fact IDs, no longer depend on arrival.

The finding identity already carries `observed_version` and the impact status, so
the findings of one digest persist side by side and coalesce does not merge them.
The SBOM branch hands its unattached digests and digest-less rows to the same
function, so they take the fix too. An SBOM-attached digest is a separate path:
it already reads every matching version through `matchingOSPackageImpactPaths`
and flags a version conflict, and this change does not touch it.

## Reproduction

One digest, `curl` at `1:1.0-3` and `1:1.0-4`, one scan scope. The base rows were
observed on `99206f6102` and are the same on main (arbiter probe, 2026-11-04):

| Advisory fact | Row order | Findings on the base | Findings after |
|---|---|---|---|
| lists `1:1.0-3` | low, high | 1: `affected_exact` at 1:1.0-3 | 2: `affected_exact` at 1:1.0-3, `possibly_affected` at 1:1.0-4 |
| lists `1:1.0-3` | high, low | 1: `possibly_affected` at 1:1.0-4 | the same 2 |
| fix at `1:1.0-4` | low, high | 1: `possibly_affected` at 1:1.0-3 | 2: `possibly_affected` at 1:1.0-3, `not_affected_known_fixed` at 1:1.0-4 |
| fix at `1:1.0-4` | high, low | 1: `not_affected_known_fixed` at 1:1.0-4 | the same 2 |

The last base row is the false "fixed". `TestDigestHoldingTwoVersionsYieldsOneFindingPerVersion`
fails on the base in both cases (the findings differ by byte between orders) and
passes after. `TestDigestScannedTwiceAtOneVersionKeepsOneFinding` fails on the
base too: a digest scanned twice at one version cited `os-first` in one order and
`os-second` in the other, so row order moved the evidence even without two
versions. `TestSBOMBaseDigestHoldingTwoVersionsYieldsOneFindingPerVersion` covers
an unattached digest beside an attached SBOM subject.

## Finding counts

The census is the #9984 harness with one added class, `digest-multi-version`: the
rows of one (vendor, release) share one scope and one digest, so a digest holds
every version the fixture crossed in. It covers the seven committed OS advisory
records and the five records that join no OS row, in three row orders (forward,
reverse, rotated). The harness is a scratch test file kept outside the repository,
because the core package is at its directory file limit (see
`9984-digestless-fleet.md`). To rerun it, copy `zz_census_10057_test.go` into a
checkout of each side:

```text
cd <checkout>/go && CENSUS_OUT=<out>.json go test ./internal/reducer/supplychain/core/ -run TestZZCensus -count=1
python3 compare.py census-before.json census-after.json
```

| Class | Fixtures | Runs | Findings before | Findings after | Byte-identical runs | Order flips before | Order flips after |
|---|---|---|---|---|---|---|---|
| `digest-multi-version` | 7 | 21 | 51 | 219 | 0 | 7 | 0 |
| `digest-resolved` | 7 | 21 | 219 | 219 | 21 | 0 | 0 |
| `digestless-fleet` | 7 | 21 | 219 | 219 | 21 | 0 | 0 |
| `digestless-three-hosts-per-group` | 7 | 21 | 219 | 219 | 21 | 0 | 0 |
| `digestless-single-row` | 7 | 21 | 21 | 21 | 21 | 0 | 0 |
| `mixed-half-digest` | 7 | 21 | 219 | 219 | 21 | 0 | 0 |
| `sbom-attached-only` | 7 | 21 | 21 | 21 | 21 | 0 | 0 |
| `sbom-plus-digestless-base` | 7 | 21 | 240 | 240 | 21 | 0 | 0 |
| `sbom-plus-unattached-digests` | 7 | 21 | 240 | 240 | 21 | 0 | 0 |
| `control-non-os` | 5 | 15 | 27 | 27 | 15 | 0 | 0 |

Only `digest-multi-version` moved. Before, 168 of its 219 installed rows were
cited by no finding (they lost to the first row of their digest); after, every row
is cited and 219 findings stand, the same count as `digestless-fleet`, which
shows the digest path now splits like the digest-less path. All seven fixtures
flipped with row order before and none do after. By status the class reads 6
`affected_exact`, 29 `possibly_affected` and 16 `not_affected_known_fixed` before,
and 45, 105 and 69 after, which equals the status mix of `digestless-fleet`.
Every other class, including `digest-resolved` (one row per digest) and the SBOM
classes, is byte-identical: the #9984 census is intact.

## Cost

Benchmark Evidence: `BenchmarkOSFleetDigestVersions` (new) and the existing
`BenchmarkOSFleetDigestFanout`, compiled to a test binary per side from the base
(with the new benchmark's finding-count check removed on that side, since the base
returns one finding per digest) and from the branch. Eight interleaved rounds per
side with an alternating first mover, 100 iterations each; medians reported. The
one-minute load average read 14.2 to 18.6 across the run. `digests-N` is N image
digests; `versions-V` is V installed versions per digest.

| Benchmark | Findings (base to after) | Base | After | Ratio | Allocations (base to after) |
|---|---|---|---|---|---|
| `DigestFanout` fleet-1x (10 images) | 10 to 10 | 0.334 ms | 0.342 ms | 1.03 | 3653 to 3713 (+1.6%) |
| `DigestFanout` fleet-5x (50 images) | 50 to 50 | 2.500 ms | 2.507 ms | 1.00 | 32746 to 33049 (+0.9%) |
| `DigestFanout` fleet-10x (100 images) | 100 to 100 | 5.050 ms | 5.156 ms | 1.02 | 69495 to 70096 (+0.9%) |
| `DigestVersions` versions-1, 10 digests | 10 to 10 | 0.251 ms | 0.263 ms | 1.05 | 2590 to 2650 (+2.3%) |
| `DigestVersions` versions-1, 50 digests | 50 to 50 | 1.288 ms | 1.328 ms | 1.03 | 14619 to 14921 (+2.1%) |
| `DigestVersions` versions-1, 100 digests | 100 to 100 | 2.388 ms | 2.503 ms | 1.05 | 27485 to 28089 (+2.2%) |
| `DigestVersions` versions-3, 10 digests | 10 to 30 | 0.270 ms | 1.278 ms | 4.74 | 2771 to 16169 |
| `DigestVersions` versions-3, 50 digests | 50 to 150 | 1.400 ms | 7.549 ms | 5.39 | 15524 to 104957 |
| `DigestVersions` versions-3, 100 digests | 100 to 300 | 2.635 ms | 16.023 ms | 6.08 | 29291 to 231173 |

The common case (one version per digest, same findings) costs 0 to 5% more time
and 1 to 2% more allocations: the per-digest partition. The three-version rows
cost more because they produce three times the findings, and the base's single
finding for them was the wrong answer.

The three-version rows grow faster than the finding count (6.08x at 3x findings
and 100 digests). A CPU profile of `versions-3/digests-100` on the branch puts 42%
of the time in `coalesceAndSortSupplyChainImpactFindings` and 41% in
`supplyChainImpactStableKey`, which marshals the logical identity to JSON inside
the sort comparator, so the marshals grow as n log n in the findings. That is the
existing final sort, shared by every fan-out in the package; this change only
feeds it more findings. Computing each finding's key once before the sort would
remove most of it; that is a separate, measured change and is not part of this
one.

Performance Evidence: no-regression read for the common case above
(`DigestFanout` and `DigestVersions` versions-1: time within 0 to 5%, allocations
+0.9% to +2.3%). The added cost for a digest that holds several versions follows
the findings it now produces and is bounded by the installed versions the digest
holds, not by the host count.

No-Regression Evidence: every census class except `digest-multi-version` is
byte-identical in the table above, including all 21 runs of `digest-resolved` and
of `sbom-plus-unattached-digests`.

No-Observability-Change: the split emits no new metric, span, log key, or status.
Finding-level counters that count findings, such as
`eshu_dp_supply_chain_impact_os_release_not_in_advisory_total`, rise because a
digest that holds a version on a foreign line, or below a fix, now has a finding
of its own.

## Mutants

Nine mutants ran against the final tree in a private detached worktree
(`mutants10057.py` beside the harness; the worktree was removed after). Eight were
killed; one survives and is equivalent.

| Mutant | Result | Killed by |
|---|---|---|
| D01 digest rows unsplit (the old behavior) | killed | all three new tests |
| D02 keep only the first shard of a digest | killed | `TestDigestHolding...`, `TestSBOMBaseDigestHolding...` |
| D03 keep only the last shard of a digest | killed | the same two |
| D04 shard key ignores the installed version | killed | the same two, and `TestDigestlessFleetGroupsOnePerLineAndVersion` |
| D05 shard keeps the first row seen | killed | `TestDigestScannedTwice...` and two #9984 tests |
| D06 shard keeps the greatest row | killed | `TestDigestlessFleetRepresentativeIsStable` (#9984) |
| D07 shard keys unsorted | killed | `TestDigestlessFleetCoalescedShardsKeepOneStableFix` (#9984) |
| D08 digest list unsorted | survived | equivalent: the final sort orders every finding by stable key, and two digests never share an identity, so digest order cannot reach the output |
| D09 skip the split when a scope holds one digest | killed | all three new tests |

## Expected change on rollout

Finding-level counters and the per-pass fact counts rise once this ships, on the
first pass that reruns for a scope and generation that holds a digest with several
versions of a key. In the census the multi-version class goes from 51 to 219
findings, and its status mix from 6 `affected_exact`, 29 `possibly_affected` and 16
`not_affected_known_fixed` to 45, 105 and 69. Read that rise as the fix: the
digests were already there, and their other versions had no finding. A reader at
3 AM should not treat it as a regression in the counters, in `FactsWritten` for the
pass, or in the finding count on the API. The size on a live fleet depends on how
many digests hold several versions of one key, which this note did not measure.

## Limits

- Consumers see more rows. A digest that holds `V` versions of a package key now
  persists up to `V` findings for the advisory where it persisted one, each with
  its own `observed_version` and verdict. The identity already carries
  `observed_version` and `impact_status`, so nothing collides: the writer's stable
  key and fact ID, and the read side's canonical key
  (`query/supply/chain/impact`, `CanonicalFindingKeySQL`), carry them, and the
  keyed winners probe takes whole `(package_id, repository_id, subject_digest)`
  triples, so a hot triple only gets `V` times larger. A search of
  `query/supply/chain/impact`, `mcp` and `storage/postgres` found no
  GROUP BY or DISTINCT on `subject_digest` that assumes one finding per digest.
  Not read line by line: the investigation packet, the `ask` catalog,
  `answerquality`, and the advisory loader the vulnerability service uses.
- Findings already stored for a digest that held several versions came from the
  single-row classification and may be wrong (a false `not_affected_known_fixed`,
  or a missing version). This change does not retire them and adds no backfill or
  replay trigger. The writer replaces findings per (scope, generation) in one
  transaction (`writer_retract.go`): once a full pass reruns for that scope and
  generation it upserts the new findings and retracts every stored row it did not
  derive again, unless the pass carries partial evidence, which skips the
  retraction. Until such a pass runs, the old rows stay. What re-queues the intents
  after a reducer deploy was not checked, and nothing here says stored data is
  corrected. The QA environment's exposure was not measured (no QA contact in
  this change); the read-only count by source key that issue #10057 asks for is
  still open.
- Rows whose purl names no distro qualifier still share a writer identity across
  lines when the version and digest match; that is the #9984 limit, unchanged.
- One digest that holds the same version on two release lines (for example a
  debian-11 and a debian-12 row at `1:1.0-3`) yields one finding per line. That is
  the #9984 rule applied to the digest, not a regression, but it means a digest is
  no longer guaranteed one finding per version.
- The per-target scan path reaches the same function and takes the same fix; it
  has no separate test.
- The release label on
  `eshu_dp_supply_chain_impact_os_advisory_release_selections_total` is #10056 and
  is not touched.
- The arbiter's note asks which version the source-key join compares when a binary
  version carries an epoch and the fix does not. This change does not alter the
  comparison; it only stops one row from deciding for the rest.
