# #10741: advisory timestamp parse, performance and observability

Issue: eshu-hq/eshu#10741. The newest-copy election and the advisory-age score
parsed advisory timestamps with RFC 3339 only. Every NVD timestamp is unzoned
(`2026-08-11T19:33:44.513`), so NVD copies were all "unparseable", ranked
oldest, and the election fell through to the FactID tie-break. The fix is one
shared reader, `parseAdvisoryInstant` in
`go/internal/reducer/supplychain/core/newest_copy.go`, used by both callers. It
tries RFC 3339 first and then the unzoned layout read as UTC.

This note covers the two hot files the performance gate lists:
`newest_copy.go` (`parseAdvisoryInstant`, `newestCopyInstant`) and
`priority.go` (`parsePriorityTime`).

## Performance Evidence

The benchmark is committed beside the code:
`BenchmarkAdvisoryInstantParse` in
`go/internal/reducer/supplychain/core/advisory_instant_bench_test.go`. It runs
the pre-fix parse (`priorAdvisoryInstant`, RFC 3339 only) and the new reader on
the same five inputs in one binary, so both see the same machine state.

Command: `go test -run '^$' -bench BenchmarkAdvisoryInstantParse -benchmem
-count=10 -cpu 1 ./internal/reducer/supplychain/core`

Environment: Apple M5 Max, 18 CPUs, darwin/arm64, go1.26.11. **The host was not
quiet**: load averages were 31 to 48 on 18 CPUs while it ran, so absolute times
spread about 2x between passes. Allocation counts are exact and do not depend on
load. 10 passes each, medians with the min to max range:

| Input | Prior ns/op | New ns/op | Prior allocs | New allocs |
| --- | --- | --- | --- | --- |
| zoned `Z` | 68.3 (25.8 to 84.0) | 63.1 (49.1 to 71.6) | 0 | 0 |
| zoned `Z` with nanoseconds | 109.5 (92.0 to 121.9) | 100.8 (39.0 to 122.0) | 0 | 0 |
| zoned offset | 59.3 (37.3 to 106.0) | 85.1 (40.1 to 114.5) | 0 | 0 |
| unzoned millis (every NVD stamp) | 524.2 (392.6 to 655.8) | 680.2 (393.5 to 1057.0) | 2 | 2 |
| garbage | 445.9 (240.6 to 614.3) | 420.1 (319.4 to 498.0) | 3 | 6 |

No-Regression Evidence: the zoned shapes are the path every GHSA, OSV, and CVE
List stamp takes, and the new reader allocates nothing on them, same as before.
The median differences are inside the pass-to-pass spread in both directions
(the offset row's median is higher while its minimum is within 3 ns), so this
run shows no measurable change on those rows, not an improvement. The new reader
tries RFC 3339 first, so a zoned stamp does no extra work.

Cost of the fix: the unzoned row is the shape that previously failed to parse
and now succeeds, so it pays a second parse. The median moves by about 156 ns
and the allocation count stays at 2. Over the roughly 9,860 active NVD
`vulnerability.cve` facts measured in QA (see eshu#10741), one full pass costs
about 1.5 ms (156 ns x 9,860). The unparseable path (an empty or malformed
value, which ranks oldest) goes from 3 to 6 allocations.

Consequences the issue's ruling accepts, none a regression in this path: NVD
findings gain an `advisory_age` priority contribution they never had, and the
first reduce after deploy rewrites each NVD finding's payload once.

Not checked: a rerun on a quiet host. The allocation counts would not change.
No live-Postgres run: no SQL, graph, queue, or lease code changed, and the SQL
GHSA election and its parity-locked pattern are untouched.

## Observability

No-Observability-Change: the supply-chain-impact reducer domain still emits the
same signals, and this change adds no new skip path, queue, or stage. An
operator sees it through
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`
for the domain, and `eshu_dp_supply_chain_impact_findings_total` for decision
volume (the existing `supply-chain diagnostics` row in
`docs/public/observability/telemetry-coverage-reducer-domains.md`).

No counter for an unparseable timestamp is added here. "Unparseable" is an
existing, defined ranking state (the reader deliberately rejects date-only
values, a space separator, and hour 24), so a counter's baseline would depend
on which sources emit those shapes, which this change does not measure. The
helper is a pure function with no meter handle. If an operator signal is wanted,
add it at decode time, labelled by source, as a follow-up.
