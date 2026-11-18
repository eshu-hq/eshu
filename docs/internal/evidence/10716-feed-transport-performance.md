# 10716 feed transport: performance and observability evidence

Lane: kill switch (`nvd_feed_transport`), widened NVD cursor reader
(cursor, set, head, updated_at), transport label, feed metrics, seed
selector vocabulary, claim-plane golden trace and fencing tests. Branch
`drive/10716-nvd-feed-switch-telemetry`.

## Performance Evidence:

Backend: Apple M5 Max (darwin/arm64), Homebrew Go 1.27.3. Functional
proof ran on disposable postgres:18-alpine; CI (linux/amd64) runs the
full suite.

- Baseline: the per-tick cursor probe is a single-row keyed SELECT
  (`cursor_unix_micro, head_unix_micro` by `(collector_instance_id,
  scope_id)`) with no locks taken; the lag gauge and anomaly counter
  observe one in-process struct; the extensionhost armed the cursor
  writer whenever the verdict said advance.
- After: the same SELECT returns one more 8-byte column
  (`updated_at_unix_micro`, UTC-normalized in Go). No predicate, join,
  index, lock, retry, pacing, or serialization change anywhere in the
  lane. The arming decision adds one integer comparison (`advance_to
  > head`) before arming; the transport/selector folds are exact-match
  closed-set lookups.
- Microbenchmark (new fold code, worst case of the three inputs cycled):

  `BenchmarkNVDWindowFeedSelectorScratch-18 100000 1.6 ns/op`
  `BenchmarkNVDTransportFoldScratch-18      100000 1.8 ns/op`

  Input shape: closed sets of short strings (longest 27 chars). The
  folds run at most once per window claim or probe tick; at 1.8 ns/op
  they are four orders of magnitude below the claim's I/O.
- No-Regression Evidence: every affected package suite passes on the
  final head, including the disposable-Postgres live tests (cursor
  compare-and-advance, concurrent serialization, stale-advance,
  split-partial/full ordering, scan shape, ownership accumulation; the
  R5 trace chain and R6 fencing tests; race detector clean), so the
  added column, comparison, and registrations changed no behavior the
  tests pin. The change cannot regress p95s: one extra 8-byte column on
  an indexed single-row read, ns-scale folds, in-process metric
  recording with explicit buckets (1 s to 900 s, unit s), no new I/O.

## Observability Evidence:

New signals (all registered in `go/internal/telemetry`, coverage doc
row "Vulnerability intelligence NVD 2.0 feed transport window claims"):

- `eshu_dp_vulnerability_nvd_window_claims_total` and
  `eshu_dp_vulnerability_nvd_window_claim_duration_seconds` (labels
  `transport`=api/feed/unknown, `outcome`=committed/refused/
  failed_retryable/failed_terminal).
- `eshu_dp_vulnerability_nvd_window_cursor_advance_total` (labels
  `transport`, `outcome`=advanced/held).
- `eshu_dp_vulnerability_nvd_feed_age_seconds` (scrape-time gauge),
  `eshu_dp_vulnerability_nvd_feed_guard_trips_total` (label closed
  `failure_class`), `eshu_dp_vulnerability_nvd_feed_deadline_short_total`,
  `eshu_dp_vulnerability_nvd_feed_sha256_mismatch_total`,
  `eshu_dp_vulnerability_nvd_feed_meta_unavailable_total`.
- Seed selector vocabulary (`nvd_feed_before_seed`,
  `nvd_feed_covered_seed`, `nvd_feed_beyond_seed`,
  `nvd_feed_seed_unavailable`, `other`) is registered with an exact-match
  fold; no emitter reads it until the satellite stamps the selector.
- No-Observability-Change for the cursor lag: the lag gauge and anomaly
  counter keep their names and units; the lag gauge gains only the
  closed `transport` label (head is the feed horizon on the feed path).

Credential, URL, and response-body handling (leak read of every new or
touched log line in the lane): the invalid-setting WARN logs the key
name `nvd_feed_transport`, the literal `off`, the instance id, and the
closed reason `invalid_value` — never the configured value. The cursor
report logs counts and timestamps only, never a scope id. The new
tests log only test-local values. No provider response body reaches a
fact, log line, or metric label.
