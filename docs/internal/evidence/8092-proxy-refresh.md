# 8092 proxy-gated refresh — no-regression evidence

Change: `Cache.RequestRefresh` returns nil immediately when a proxy is set,
mirroring the `Args` proxy gate. Only the proxy env path changes; every other
refresh path is byte-identical.

No-Regression Evidence:

- Baseline (commit f9dd62145, PR #8488 head): under proxy env, each qualifying
  failure queued a direct Go-resolver lookup whose result could never pin, and
  the retry waited on it up to `DefaultRefreshWait`.
- After (this branch): the same call returns nil before any lookup is queued.
  `TestRequestRefreshUnderProxySkipsLookup` fails on the baseline (exactly one
  lookup observed) and passes after (zero lookups), RED-to-GREEN.
- Backend/version: hermetic unit tests with fake env (`HTTPS_PROXY` set) and
  the in-repo fake-git harness; no live network. Go toolchain per `go.mod`.
  `go test -race ./internal/collector/repo/git/resolve/ -count=1` exits 0
  (1.2 s, this branch).
- Input shape: proxy env set/unset crossed with qualifying failure; SSH,
  IP-literal, and cache-off configs keep their existing bypass behavior.
- Why safe: the proxy path now does strictly less work than the baseline
  (nil return, no goroutine wake, no DNS). Non-proxy paths are untouched, and
  the full `resolve` package suite plus `-race` pass.

No-Observability-Change: existing `eshu_dp_git_resolve_cache_refresh_requests_total`
(reason label closed set), `eshu_dp_git_resolve_cache_lookups_total`, and
`eshu_dp_git_resolve_cache_lookup_duration_seconds` are unchanged. A
proxy-skipped refresh records nothing, matching the long-standing cache-off
early return in the same function — proxy deployments already observed zero
pins and zero lookups from this cache through the `Args` gate, and the refresh
path now agrees with it.
