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

## #11241: gosec loads packages by directory

The gosec gate used to pass one `go/packages` pattern per file, and x/tools
splits a pattern list at 16,383 bytes. `internal/storage/postgres` (473 files,
16,443 bytes) outgrew that even through the short alias, so the fail-closed gate
refused every code PR. The gate now pins gosec
`v2.30.1-0.20261026162949-ff25931a2edd`, which loads module packages by
directory (securego/gosec#1803 and #1806), and the alias code is deleted. v2.30.0
predates both commits. The move to the first release containing them is #11243.

Measured locally on the same worktree, `gosec -severity=low -confidence=low
-no-fail -fmt=json ./...` over `go/` (the SARIF step's scan), host load average
about 30:

| Run | Go errors | Files | Findings | Wall time |
| --- | --- | --- | --- | --- |
| v2.27.1, before | 24 | 9,053 | 0 (SSA skipped on postgres) | 112.27 s |
| new pin, before triage | 0 | 9,053 | 3 (G302 x2, G602) | 70.85 s |
| new pin, after triage | 0 | 9,053 | 0 | 55.57 s |

The three new-rule findings: G302 on `Chmod(0o644)` in
`collector/repo/git/selection_copy_commit.go` and on `Chmod(0o755)` in
`cli/graphinstall/install.go` carry a leading `#nosec G302` with the reason (the
modes are intentional, the latter pinned by `install_test.go`). The G602 in
`query/supply/chain/impact/version_resolution.go` was a false positive on an
index guarded by `winnerIndex == -1`; the winner is now captured in the loop that
finds it, so no index is needed.

The gate's seeded cases live in `scripts/test-precommit-go-gosec.sh`: a 401-file
package over the limit even via the old alias with a planted G115 in its last
file (old tree: exit 1, "even via alias"; new tree: G115 reported at its real
path), a clean variant (exit 0), and the pseudo-version extractor.

No-Regression Evidence: the hot-path changes are two `#nosec G302` comments and
one local-variable change in `version_resolution.go` (the winner is copied out of
the loop instead of read by index afterwards, same value, same branch). The
`impact` and `graphinstall` package tests pass (363 and 25 tests), and
`go vet` is clean on the three touched packages. No query, index, batch, lease
or write path changed, so the repo-scale performance contract is unchanged.

No-Observability-Change: no metrics, spans, structured-log fields or status
surfaces were added, removed or renamed.
