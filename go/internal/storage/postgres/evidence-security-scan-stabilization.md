# Evidence — security-scan workflow stabilization (PR #3854)

This PR makes `.github/workflows/security-scan.yml` pass on Go 1.26 (timeouts +
concurrency, the x/net & x/crypto CVE bumps, govulncheck `-scan package`, the
gosec v2.27.1 bump, and the gosec `#nosec` false-positive triage). The triage
touches files in hot-path locations the performance-evidence gate watches
(`go/internal/storage/postgres`, `go/internal/storage/cypher`,
`go/internal/collector`, `go/internal/reducer`, `go/internal/query`), so this
note records why those touches carry no runtime risk.

## Scope of the hot-path touches

- The overwhelming majority are inline `// #nosec <RULE> -- <reason>` comments
  added so the standalone gosec binary (which does not read the repo's
  `//nolint:gosec`) stops re-flagging already-vetted sites: non-cryptographic
  md5/sha1 content-address and identity digests, identifier/label/SQL-statement
  string constants tripping G101, bounded integer conversions, indexer/operator
  file reads at program-derived paths, and SQL builders that interpolate only
  `$N` placeholder indices (values are always bound parameters — verified, no
  injection).
- The only non-comment changes are security hardening, not behavior changes:
  an HTTP `ReadHeaderTimeout` on the runtime server and file/dir permission
  tightening (`0o755`→`0o750`, `0o644`→`0o600`) in cmd/runtime/component/
  extensions helpers.

No Cypher text, query shape, index/DDL, worker/lease/conflict-key, batch size,
goroutine/channel, queue ordering, or graph-write logic was modified on any hot
path.

No-Regression Evidence: hot-path changes are gosec `#nosec` annotations
(comments) plus non-behavioral hardening (HTTP ReadHeaderTimeout, tighter file
permissions). `go build ./...` passes and `golangci-lint run ./...` is clean on
the changed packages; no measured runtime path was altered, so the repo-scale
performance contract is unchanged. The SQL `#nosec` sites were each verified to
interpolate only bound-parameter placeholders, preserving existing query plans.

No-Observability-Change: no metrics, spans, structured-log fields, or status
surfaces were added, removed, or renamed by this PR; the comment annotations and
hardening do not touch any telemetry instrument or emission site.
