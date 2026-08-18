# Replacing lib/pq with pgarray and a forked embedded-postgres

## Why this touched hot files

`github.com/lib/pq` carries five 2026 advisories with no upstream fix
(GO-2026-6166, -6168, -6170, -6171, -6172). `govulncheck` is a blocking gate,
so every PR in the repository was failing it, including PRs that changed no Go
code at all.

Removing the dependency meant rewriting 348 call sites of `pq.Array`,
`pq.StringArray`, `pq.QuoteIdentifier` and `pq.Float64Array`. Many of those sit
in `internal/storage/postgres` files whose SQL contains `CREATE`/`MERGE`, so the
content-based hot-path detector selects them. **The SQL text itself is
unchanged.** The only edit in those files is the selector rename, e.g.
`pq.Array(&ids)` to `pgarray.Array(&ids)`.

## No-Regression Evidence:

The risk in this change is not latency, it is *encoding*. A wrong Postgres array
literal does not crash — it writes subtly wrong data or silently fails to match,
and no compiler or ordinary test catches it. So the proof is a differential one,
run in-process **while lib/pq was still a direct dependency**, asserting the new
encoder is byte-identical to the old:

- `StringArray.Value`, 19 rows: nil to `NULL`, empty to `{}`, embedded comma,
  double quote, backslash, both braces, the literal word `NULL`, empty string,
  leading/trailing whitespace, tab and newline, UTF-8 (`héllo`, `日本語`, an
  emoji), quote-plus-backslash, and a `$;`/`--` injection attempt. Each asserted
  `pgarray == pq.StringArray == pq.Array`, and each also frozen against the
  literal pq produced.
- `Float64Array.Value`, 9 rows including `1e-7`, `0.30000000000000004`, negative
  zero via `math.Copysign`, and `1e21`.
- `QuoteIdentifier`, 10 rows including an embedded quote doubled and
  `x"; DROP SCHEMA public; --` becoming `"x""; DROP SCHEMA public; --"`.
- `StringArray.Scan`, 28 rows, asserting decoded values *and* error/no-error
  agreement: bare `NULL` rejected, nested `{{a},{b}}` rejected, unterminated
  quote, garbage after close, and unsupported source types.

The differential bit for real during development: a float row written as Go
`-0.0` (which is `+0`) disagreed with a frozen `{-0}` expectation, and the table
was wrong rather than the encoder.

**The proof can fail.** Two on-disk mutations (not `go test -overlay`, which
cannot falsify a source-reading guard): dropping backslash escaping and quote
doubling turned 5 rows red; changing the parser's bare-`NULL` comparison turned
the 2 scan rows red. Both restored to green.

Whole-module verification on the wired tree, exit codes captured directly:
`go build ./...` 0, `go vet ./...` 0, `go test ./... -count=1` 0 with 765
packages ok and zero failures, `gofmt -l` empty, `verify-dirgate.sh --all` 0.

`govulncheck ./...` and `govulncheck -scan symbol ./...` both exit 0 module-wide
("Your code is affected by 0 vulnerabilities"), against exit 3 with five
advisories before the change.

### What was not proven

A live Postgres run (throwaway `postgres:18-alpine`) executed the storage, query
and reducer suites on **both** this branch and `origin/main`, with the same
image, the same bootstrap and the same command:

```
go test -p 1 ./internal/storage/postgres/... ./internal/query/... ./internal/reducer/... -count=1
```

**Head-only failures: zero.** Every failure on this branch also fails on
`origin/main`.

The most instructive one is `TestSupplyChainImpactRuntimeFilterPlansLive`, which
fails on both sides with the same root cause and different wording — head says
`expected 24 arguments, got 23`, main says
`pq: got 23 parameters but the statement requires 24`. That is a pre-existing
defect: `supplyChainRuntimeFilterListArgs` builds 23 arguments while the query
carries a `$24::timestamptz`. It is not caused by this change, and it is
unrelated to array encoding.

An earlier version of this note claimed the failures were confined to files
containing no `pgarray` reference. That was wrong, and it is recorded here
rather than quietly corrected: the plan test above contains eleven, and
`supply_chain_suppression_paths_performance_live_test.go` contains three. The
head-vs-main differential above is the claim that actually holds, and it is
stronger than the one it replaces — it does not depend on guessing which files
matter.

The two performance-ceiling tests failed on both sides under full-suite load,
which the repo's own guidance says proves nothing about a ceiling.

Not run: anything requiring NornicDB/Neo4j, compose e2e, the golden-corpus gate,
the search-vector scale test, and the remote-validation drivers.

## No-Observability-Change:

No metric, span, log line, status field or pipeline stage is added, removed or
renamed. No query text changes, so no plan changes. The only runtime difference
is which driver `embedded-postgres` opens its bookkeeping connection with, on
the `eshu local` path, and that connection emits no telemetry.

## The embedded-postgres fork

Eshu's own use of lib/pq is gone with `pgarray`, but `cmd/eshu` still linked the
driver through `github.com/fergusstrange/embedded-postgres`, which used
`pq.NewConnector` in one helper feeding `createDatabase` and
`healthCheckDatabase`. Because `database/sql` resolves driver names at run time,
that registration made the advisories reachable from any binary linking the
package, even though every production `sql.Open` here passes `"pgx"`.

Upstream issue #150 asked for this in January 2025 and was closed without a fix;
the maintainer's answer was that he was not planning to do it but would accept a
contribution removing the database library. That premise is now out of date — all
five advisories report `Fixed in: N/A`, so lib/pq has not fixed them.

`eshu-hq/embedded-postgres` forks v1.34.0 and swaps that single helper to the pgx
stdlib driver, tagged `v1.34.0-eshu.1` and wired with a `replace`. The fork's own
suite passes. Three of its tests asserted lib/pq's exact error strings; all three
still fail as intended under pgx, and they now assert the library's own wrapper
text and the semantic condition rather than a third-party driver's phrasing.
