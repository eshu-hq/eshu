# #10056 vendor-line join label: strongest result over joined rows

## Problem

`countOSAdvisoryJoins` labeled a joined fact from the first matching package
row in index order. One Debian vendor fact (published for Debian 11) joined to
rows on Debian 11 and Debian 12 read `vendor_source` when the 11 row came
first and `vendor_line_mismatch` when the 12 row came first. The mismatch
label claims the fact is excluded from the exact and known-fixed verdicts,
which is false for the Debian 11 host in the same pass.

## Decision (criterion 1)

One label per joined fact entry: the strongest release result over every row
the fact joins. The per-(fact, line) alternative would change the counter's
cardinality (joined totals would exceed facts) and every sub-signal built on
it; the existential reading matches `joined`'s existing meaning (at least one
row joined). Rank, strongest first: `release_matched` / `vendor_source`,
then `release_untagged`, then `release_unlisted` / `vendor_line_mismatch`,
then unknown. A fact published for a joined line reads `vendor_source`;
a fact naming no joined line still reads `vendor_line_mismatch`. Per-row
verdicts are unchanged; a finding's own row can read weaker than the label,
so `match_reason` stays the per-row source of truth.

## Change

- `strongestOSAdvisoryReleaseResult` walks the same lookup keys with the same
  match predicate as `firstOSPackageImpactPath`, so the joined set is
  unchanged; only the label selection moves.
- The telemetry page row, the coverage table, and the package README now say
  strongest-over-rows instead of first-match-in-index-order.
- Regression: `TestVendorLineJoinLabelIgnoresRowOrder` builds the same fact
  plus Debian 11 and 12 rows in both orders and pins `vendor_source` in both.
  It failed before the fix (`vendor_source` vs `vendor_line_mismatch`).

## Proof

Focused: `go test ./internal/reducer/supplychain/core/ -count=1` green,
`go vet` clean, `git diff --check` clean, `gofumpt` clean. Parent reducer
package references none of these signals, so no caller test covers them.

No-Regression Evidence: scratch benchmark (deleted after the run)
`buildSupplyChainImpactFindingsAndExclusions` over 1 CVE fact, 1 Debian vendor
fact (`distro=debian-11`), and 2 installed rows (Debian 11 + 12) on
darwin/arm64 (Apple M5 Max), Go 1.27.3, `-benchtime 1s -count 3`:

- base `c1cb309c1d`: 26185/26082/26206 ns/op, 45521–45522 B/op, 396 allocs/op
- after `bcbc8673`: 25820/26024/26173 ns/op, 45589–45591 B/op, 400 allocs/op

Ranges overlap; bytes differ by 68 B/op (+0.15%) and allocs by +4/op on a
400-alloc build. The extra work is bounded: one `osAdvisoryReleaseResult`
string-compare per additionally joined row under the fact's lookup keys
(typically one or two rows). No new backend call, queue stage, lock, or graph
write.

No-Observability-Change: no new metric, span, log key, or status. The existing
`eshu_dp_supply_chain_impact_os_advisory_joins_total{source,outcome}`,
`eshu_dp_supply_chain_impact_os_advisory_release_selections_total{source,result}`,
and the `os_advisories_*` / `os_advisories_vendor_line_*` sub-signals keep
their shapes; only which closed `result` value a multi-row fact lands in can
move, from row order to strongest-over-rows.
