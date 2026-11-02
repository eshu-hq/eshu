# OSV Affected-Entry Skip Evidence (#9806)

An OSV record used to fail whole, and abort its target, when one affected entry
had no package identity. Two live shapes did it: a `Packagist:<repository URL>`
ecosystem suffix (709 Packagist entries, all Drupal) and an entry with no
`package` object (323 Hex entries), plus a `GitHub Actions` entry in a Go
record (2 entries). The converter now reads the base token of an OSV ecosystem
(text before the first `:`) and skips an entry it cannot name a package for,
counting it, instead of failing the record. A record whose every entry is
skipped yields no facts. No fact payload shape changes: `package_id`,
`ecosystem`, `purl`, and every other key keep their derivation.

No-Regression Evidence: the usable-entry path, the one every record pays, is
unchanged within noise after both the skip and the merge. `BenchmarkOSVRecordEnvelopesScale/usable`
builds one record of 10, 50, and 100 PyPI entries with one distinct package per
entry (1x, 5x, 10x). Base `7f6501b7e3` (`origin/main`) and the change were run
in 3 interleaved rounds with `-count=3` each (9 samples per side, the first
mover alternated), on Apple M5 Max, Go 1.26.10, darwin/arm64, load average 5 to
12 (shared host, so this claims no regression only). Medians:

| Scale | Entries | Before ns/op | After ns/op | Before allocs | After allocs | Before B/op | After B/op |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1x | 10 | 44,834 | 45,381 | 1,065 | 1,070 | 80,896 | 85,203 |
| 5x | 50 | 204,490 | 198,105 | 4,790 | 4,797 | 357,834 | 378,435 |
| 10x | 100 | 437,821 | 368,217 | 9,445 | 9,453 | 710,743 | 755,501 |
| single-entry GHSA record (`BenchmarkOSVRecordEnvelopes`) | 1 | 13,064 | 12,533 | 309 | 309 | 23,681 | 23,665 |

Minimum-to-minimum ns/op is +1.9%, -2.7%, and +1.0% at 1x, 5x, and 10x; the
10x median moved -16% on this loaded host, so ns/op is noise, not a speedup. The
cost is memory: a record with more than one usable entry allocates one group
slice sized to its entries (about 4.3 KB at 10 entries, 5 to 8 allocs, +5% to
+6% B/op), plus one index map once it names more than 8 distinct packages. A
record with one usable entry takes a fast path with no group slice: the
single-entry row above is equal in allocs and bytes.

Records that list one package in several entries (release lines) do different
work after the change: the base emits one affected fact per entry and loses all
but the last at the store, the change emits one merged fact. Medians for that
work, base then after: `group5` (each package repeated in five entries) 44,728
to 24,155 ns/op and 1,065 to 604 allocs at 1x, 237,683 to 99,870 and 4,790 to
2,491 at 5x, 397,888 to 202,680 and 9,444 to 4,844 at 10x; `samepkg` (every
entry the same package) 44,869 to 21,177 ns/op and 1,065 to 530 allocs at 1x,
215,909 to 75,183 and 4,790 to 1,700 at 5x, 399,617 to 148,732 and 9,444 to
3,156 at 10x. That is fewer envelopes built, not a faster converter, and no
speedup is claimed.

The skip path is paid only by a skipped entry, and it is cheaper than building
the entry. `skip30` (3, 15, and 30 skipped entries of 10, 50, and 100; records
the base cannot convert at all) costs 37,219 / 142,929 / 290,087 ns/op with
803 / 3,481 / 6,821 allocs after the change, against 45,381 / 198,105 / 368,217
ns/op and 1,070 / 4,797 / 9,453 allocs for the same-size `usable` record: the
30% of entries that are skipped are not built. Recipe: copy
`osv_skip_bench_test.go` onto a detached worktree at the base and run
`go test -run '^$' -bench 'BenchmarkOSVRecordEnvelopesScale|BenchmarkOSVRecordEnvelopes$' -benchmem`
there; the `skip30` variants stop with `b.Skip` on the base because it aborts.

Merge (entries of one package become one fact): real-feed census over the
cached 2026-11-01 `all.zip` copies, converter run on every record. Records whose
entries name a package more than once, now merged: Go 1,140, Maven 2,201,
Packagist 2,293, PyPI 1,167, NuGet 319, RubyGems 378, crates.io 189, Hex 18,
SwiftURL 7 (about 7,700 records the store used to cut to the last entry). Zero
affected facts share a fact id on every feed, and zero packages have two
affected facts in one record. The only remaining shared fact ids are exact
repeats of a reference fact whose URLs differ only by a fragment (for example
`#issuecomment-...`), which sanitize to one identical payload and lose nothing.

Fan-out: a Hex target such as `ash` returns 35 records, 30 of them skipped
whole. The dispatcher folds each record's report into a fixed-size total and
emits at most 6 entry-counter `Add`s and 1 record-counter `Add` per target, so
the series count is bounded by ecosystems x reasons (12 x 6) however many
records or entries a target holds. `TestClaimedSourceEmitsBoundedOSVSkipSignals`
runs the 35-record case and asserts the exact label tuples: 2 entry series and
1 record series. No per-record list is kept across a target.

Real-feed census (cached `all.zip` feeds from 2026-11-01, converter run on
every record): before the change Hex failed 297 of 420 records (323 bad
entries), Packagist 709 of 7,397, Go 1 of 9,815 (2 bad entries). After: every
feed has `failed=0`; skips are Hex `no_package`=323, Go
`unsupported_ecosystem`=2, Packagist 0 (the 709 entries map to
`composer://repo.packagist.org/...`). The skip counts equal the earlier
`bad_entries` for Hex and Go.

Observability Evidence: an operator sees skipped data on two new counters,
`eshu_dp_vulnerability_intelligence_affected_entries_skipped_total` and
`eshu_dp_vulnerability_intelligence_records_skipped_total`, both labelled
`source`, `ecosystem` (the target's canonical Eshu ecosystem or `other`), and
`reason` from a closed set (`no_package`, `blank_ecosystem`,
`unsupported_ecosystem`, `blank_name`, `invalid_purl`, `normalize_failed` for
entries; `no_usable_affected` for records). The raw entry ecosystem and its
credential-stripped suffix are on the per-record
`vulnerability_intelligence.osv_affected_skipped` span event (`advisory_id`,
`reason`, `entry_ecosystem`, `entries_skipped`) on the fetch span and a DEBUG
log line, never on a label. The event and log fire once per conversion attempt:
a fallback retry after a mid-conversion error can repeat them, but never the
counters, which flush once after the record loop
(`TestClaimedSourceFallbackRetryDoesNotDoubleCountOSVSkips`). A skip is a representation limit, not a fetch gap,
so the snapshot `Complete` flag and the fetch attestation are unchanged; the
existing `eshu_dp_vulnerability_intelligence_facts_emitted_total` and the
observation counter keep covering the target. The cache wrapper replays stored
facts, so a replayed claim counts no skips.
