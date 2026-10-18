# resolve

Per-host address cache passed to git as `http.curloptResolve` (#8092).

## Why

Each repository sync runs `git fetch` as a new process. libcurl's DNS cache
ends with the process, so every fetch asks the cluster resolver again, and one
unanswered lookup stalls the serial sync loop until the per-command deadline
(#8052) kills it. This package resolves each provider host once with Go's
resolver, keeps the answer, and pins it into clone, fetch, and `ls-remote` so
curl skips its own lookup. Certificate and host name checks do not change.

## Surface

- `Settings` and `SettingsFromEnv`: `ESHU_GIT_RESOLVE_CACHE` (on or off,
  default on), `ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS` (default 5),
  `ESHU_GIT_RESOLVE_CACHE_REFRESH_SECONDS` (default 3600). The zero
  `Settings` is a disabled cache. A set value outside its spellings or range
  is a startup error naming the variable and the value.
- `Cache.Args(ctx, host, port)`: the `[-c, http.curloptResolve=...]` pair for
  one network git command, every A and AAAA address in resolver order, or nil
  to run unpinned. The first command for a host waits at most the lookup
  deadline, then falls back unpinned.
- `Cache.RequestRefresh(host)`: the deadline package's refresh hook. Never
  blocks; the returned channel closes when the lookup finishes.
- `Cache.Bind`: late-binds the sweep's telemetry instruments and logger; the
  cache is built at config load and outlives any one sweep.
- `Options`: the resolver, clock, environment, and tick seams for tests.

## Concurrency

One receiver goroutine runs every lookup. Per host, a request joins the
in-flight lookup or starts exactly one; a request inside the 30 s minimum gap
starts none and is counted `reason=suppressed`. Address sets are replaced only
by a successful non-empty lookup, never evicted, so a failed refresh keeps the
last good set. The cache holds only the latest successful answer and never
mixes in old addresses: a stale first address can cost a whole command
deadline (git sets no connect timeout and libcurl does not fail over between
pinned addresses), so a connect-class failure asks for a refresh before the
retry. The hourly background refresh travels the same path.

## Signals

`eshu_dp_git_resolve_cache_lookups_total{host,result}`,
`eshu_dp_git_resolve_cache_refresh_requests_total{host,reason}`,
`eshu_dp_git_resolve_cache_fallback_total{host,reason}`, and
`eshu_dp_git_resolve_cache_lookup_duration_seconds{host,result}`, the
`git.resolve_cache.lookup` span per lookup, an INFO `git resolve cache lookup`
line per result (WARN on failure), and one WARN per host when a pin is skipped
(`cache_empty`, `proxy_set`).

Performance Evidence: the steady-state path is one mutex-protected map read
and one string join per network git command — under a microsecond next to a
process spawn — and one DNS lookup per host per hour in place of one per git
process. The first command for a host adds at most the lookup deadline
(default 5 s) once per process; the shim on #8092 measured a pinned
`ls-remote` at 0.45–0.6 s with DNS black-holed, where the unpinned command
failed after about 12 s. `TestConcurrentRefreshSingleFlight` proves 32
concurrent failure requests cause exactly one lookup under `-race`.

Observability Evidence: the four `eshu_dp_git_resolve_cache_*` series above,
asserted from a recorded run in `cache_test.go` (lookups by result, refresh
requests by reason including `suppressed` and `periodic`, fallbacks by
`cache_empty` and `proxy_set`), plus the lookup span and log lines.
