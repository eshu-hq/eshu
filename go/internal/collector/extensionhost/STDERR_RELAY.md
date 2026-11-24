# Satellite stderr relay (#9823)

A satellite may fail a claim as `input_limit_exceeded` and print the byte
figures behind it on its stderr record. The wire `Status` cannot carry them
(`DisallowUnknownFields`), so the runner turns them into typed values and the
host logs them on the existing `component extension result` line.

## Decision (the allowlist and its bounds)

- Allowlist: exactly three integer keys, `inflated_bytes`, `budget_bytes`, and
  `compressed_bytes`, read from one JSON stderr line that carries all three as
  non-negative integers. Every other key, every non-JSON line, and any record
  with a missing, null, negative, fractional, overflowing, or string figure is
  dropped whole. The host never holds or logs a satellite string, so a path, an
  image name, or a token cannot reach a log through this relay.
- Gate: two checks, both on result state. A call is parsed
  (`CallResult.InputLimit`) only when its own result state is not `complete` or
  `unchanged`. `ResultRecord.InputLimit` is then set only for a validated SDK
  result, never for a host verdict, and only when the FINAL assembled result
  state is not `complete` or `unchanged`. A partial first page that printed
  figures, followed by a complete page, logs no figures.
- Size bound: at most the first 64 KiB of the stderr buffer is scanned, a line
  over 4 KiB is skipped, and the stderr buffer keeps its existing
  `stderr_limit_bytes` cap.
- Rate bound: no new log line. The figures ride on the one result line each
  claim already writes, so a failed claim adds three numeric fields, not log
  volume. A paged claim keeps the figures of its last call that carried them,
  except that a retryable or terminal continuation verdict page (#10780)
  reports only its own figures, or none, never an earlier page's.
- Not covered: a satellite that exits non-zero still logs only the existing
  `stderr_bytes` and `stderr_truncated` counts, and the legacy single-call
  `RunCollector` path relays nothing. Both production runners use the paged
  path.

## Evidence

No-Regression Evidence: before this change the host parsed no stderr. Command
(from `go/`):

```
go test ./internal/collector/extensionhost -run '^$' \
  -bench 'AttachStderrFigures/(complete|terminal_typical|terminal_full_buffer_no_match)$' \
  -benchtime=200000x -count=3
go test ./internal/collector/extensionhost -run '^$' \
  -bench 'AttachStderrFigures/terminal_full_buffer_bad_json' \
  -benchtime=2000x -count=3
```

Environment: go1.26.10, linux/arm64, no backend. The bad-JSON cases run
2000 iterations because each iteration takes milliseconds.

| Case | ns/op (3 runs) | B/op | allocs/op |
| --- | --- | --- | --- |
| `complete` (state compare, no parse) | 14.2-15.4 | 0 | 0 |
| `terminal_typical` (one-line record) | 1418-1703 | 288 | 9 |
| `terminal_full_buffer_no_match` (64 KiB of lines the `inflated_bytes` prefilter skips) | 78625-80430 | 0 | 0 |
| `terminal_full_buffer_bad_json_short_lines` (64 KiB of `{"inflated_bytes":"x"}` lines, one failed decode each) | 1539589-1724756 | about 934500 | 19943 |
| `terminal_full_buffer_bad_json_long_lines` (16 lines just under the 4 KiB cap, each failing decode) | 170850-181506 | 5248 | 112 |

The full buffer of non-matching lines is the cheapest full scan, not the worst
case: the `bytes.Contains` prefilter skips each line without a decode. The
worst case is a buffer of short lines that carry `inflated_bytes` but fail
`Unmarshal`: about 1.5-1.7 ms, 0.93 MB, and 19943 allocations per call. That
cost is bounded by the 64 KiB scan limit and the 4 KiB line cap, runs once per
non-complete call, next to a process or container launch that the same call
already paid (launch time was not measured here). A complete or unchanged call
costs a state compare. No queue, lease, claim, or graph path is touched.

No-Observability-Change: no new metric or span. The existing
`eshu_dp_component_extension_results_total{failure_class}` still counts failed
claims, and the result log line gains three keys (`telemetry.LogKeys()`),
proven by `TestFailedClaimRelaysAllowlistedFiguresFromSatelliteStderr`.

## Where an operator reads them

The `component extension result` log line's `extension_status.inflated_bytes`,
`extension_status.budget_bytes`, and `extension_status.compressed_bytes` keys,
next to `failure_class`.

## Seed timing relay (#11387)

A satellite that serves an NVD window prints its seed read on the same
stderr channel as one JSON line carrying exactly three integer keys,
`nvd_seed_read_ms`, `nvd_seed_buckets`, and `nvd_seed_bytes`. The host relays
them onto the same `component extension result` line as
`extension_status.nvd_seed_read_ms`, `extension_status.nvd_seed_buckets`, and
`extension_status.nvd_seed_bytes`. The parse reuses every bound above (three
integer keys, no strings, 4 KiB line cap, 64 KiB scan cap, last complete line
wins, hostile lines dropped whole), and the gate is the mirror image: only a
complete FINAL assembled state relays, so a failed, partial, or unchanged
claim never logs seed figures.

### Decision (why log keys, not a metric or status row)

The issue named three candidate surfaces: host log keys, a metric, or a
status row. The relay uses host log keys because it is the only surface that
meets the existing bounds without new machinery: the pinned SDK Result in
`collector@v0.2.0-rc.4` carries no `Log` field, so a satellite diagnostic
Log has no host path and stderr is the only production channel; the
package rule is never to relay a satellite string or add a log line, which a
metric (new instrument and label review) and a status row (recorder and store
change) would both reopen. Integers on the existing line preserve the privacy
bound (the host never holds or logs a satellite string) and add no log volume.
The `nvd_seed_read_ms` / `nvd_seed_bytes` pair is the Bar1 recorded pair
tracked by eshu-hq/eshu#11386: those key names and this surface are its
contract and must stay stable and visible.

## Evidence

No-Regression Evidence: before this change the host parsed no seed record
on the complete path. The full package suite stays green after it
(`cd go && go test ./internal/collector/extensionhost/ -count=1`), including
the pins that the input-limit relay still fires only on non-complete FINAL
state (`TestFailedClaimRelaysAllowlistedFiguresFromSatelliteStderr`,
`TestCompleteAndUnchangedClaimsDoNotRelaySatelliteStderr`) and the new
failing-first seed pins (`TestCompleteClaimRelaysAllowlistedSeedFiguresFromSatelliteStderr`,
`TestNonCompleteClaimsDoNotRelaySeedFigures`,
`TestParseSeedFiguresDropsHostileLines`).

Performance Evidence: the seed parse runs once per complete call, next to a
process or container launch that the same call already paid. Baseline: a
complete call previously cost one result-state compare and no parse.
After, measured on Apple M5 Max, darwin/arm64, no backend
(`cd go && go test ./internal/collector/extensionhost -run '^$' -bench
'ParseSeedFigures' -benchtime=100000x -count=3`):

| Case | ns/op (3 runs) | B/op | allocs/op |
| --- | --- | --- | --- |
| `complete_typical` (one-line seed record) | 526-566 | 72 | 5 |
| `complete_full_buffer_no_match` (64 KiB of lines the `nvd_seed_read_ms` prefilter skips) | 101916-112160 | 0 | 0 |
| `complete_full_buffer_bad_json_short_lines` (64 KiB of `{"nvd_seed_read_ms":"x"}` lines, one failed decode each) | 751941-1046241 | about 840000 | 20969 |

The shape matches the #9823 input-limit evidence: a non-matching buffer is
the cheapest full scan (zero allocs), and the worst case is bounded by the
same 64 KiB scan limit and 4 KiB line cap. Non-complete calls keep paying
only the state compare (`attachSeedFigures` returns before any parse), so
the failure path is byte-identical in cost to before.

Observability Evidence: the three keys land on the existing
`component extension result` line, proven by
`TestCompleteClaimRelaysAllowlistedSeedFiguresFromSatelliteStderr` (exact
integer values on the line, no sentinel content) and pinned in the
`telemetry.LogKeys()` contract test. No new metric, span, worker, queue
domain, or log line: the existing
`eshu_dp_component_extension_results_total` still counts the served claim,
and the seed pair is read off the result log line.
