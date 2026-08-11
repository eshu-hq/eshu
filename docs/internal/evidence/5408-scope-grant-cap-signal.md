# #5408 — surfacing the SHAPE-A grant-cap degradation

Observability change on the scoped infra read path. No query, clause, or
admission behaviour changes.

## What was wrong

`scopeGrantInlineScalars` reports `capped=true` when a scoped token's grant set
overflows `maxScopeGrantInlineTerms` (128) and the SHAPE-A inline-map
disjunction families (USES, DEFINES-collision) are truncated. All six call
sites discarded it (`scalars, _ :=`), so a token granted more than 128
repositories got quietly incomplete infra reads with nothing an operator could
see.

The degradation is fail-closed — rows go missing, never appear that should not,
and direct-ownership plus DEPLOYMENT_SOURCE admission still apply. That is why
this is an observability gap rather than a correctness bug, and why the fix
adds a signal instead of changing admission.

## Why the signal comes from the filter, not the call sites

The issue proposed threading a logger through the string-builder call sites.
That would count wrong. `infraSearchScopeClause` alone rebuilds the SHAPE-A
disjunction three times for one request, so per-call-site emission records a
single degraded read as three, and "how many reads came back incomplete" stops
being answerable from the counter.

The cap is a property of the token's grant set, not of any one clause, so
`grantInlineCapExceeded()` asks the access filter and handlers call it once
where they derive `access`. It calls `scopeGrantInlineScalars` rather than
recomputing `len(union) > max`, so the signal cannot drift from the truncation
it describes.

## Performance Evidence:

The signal is not free: `grantInlineCapExceeded` builds and sorts the grant-id
union, which the infra search path already did three times per request. This
adds a fourth.

`go test ./internal/query -run '^$' -bench BenchmarkGrantInlineCapExceeded
-benchtime=200x -count=3`, darwin/arm64, best of three:

| grant shape | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| all-scopes (early return) | **6.9** | 0 | 0 |
| typical scoped token, 8 grants | **259** | 128 | 1 |
| at the cap, 128 grants | **2,893** | 8,872 | 4 |
| over the cap, 512 grants | **12,743** | 36,776 | 4 |

The worst measured case — a pathological 512-grant token, which is already
near-all-scopes and the exact case this signal exists to report — adds ~12.7µs
and ~37KB per read. The reads it sits on are bounded graph queries measured in
milliseconds, so the added cost is well under 1% of the request it reports on,
and the common shapes (all-scopes, or a typical 8-grant token) are 6.9ns and
259ns.

## No-Regression Evidence:

The emitted Cypher is byte-identical. The diff on
`(*InfraHandler).searchResources` is one call added before the `access.empty()`
early return; it touches no clause, no `whereExtra`, and no query text.

That is worth stating explicitly because this change updates the frozen
`source_sha256` for that symbol in both `hot-cypher.yaml` and
`query-source-coverage.yaml`. That freeze exists to force a human to re-check
the query plan when a hot-path query function changes, so the re-check is the
sentence above rather than a regenerated digest with no explanation.

`go test ./internal/query ./internal/queryplan ./internal/telemetry -count=1` —
ok, including `TestLegacyQueryplanManifestBindsProductionQueries`, which binds
`hot-cypher.yaml` (updating only `query-source-coverage.yaml` leaves it red).

## Observability Evidence:

New counter `eshu_dp_query_scope_grant_inline_capped_total`, label `surface`
(`infra_search`, `infra_ecosystem_overview`).

- `TestRecordScopeGrantInlineCapEmitsOncePerRead` asserts exactly **1** for an
  over-cap token, **0** at the cap, and **0** for an all-scopes caller — the
  once-per-read property the counter's meaning depends on.
- `TestGrantInlineCapExceededMatchesScalarTruncation` asserts the signal agrees
  with the truncation in both directions, including the case where neither the
  repository nor the scope set reaches the cap alone but their union does.
- `TestRecordScopeGrantInlineCapSurvivesNilDependencies` pins that a handler
  with no `Instruments` and no logger does not panic. An operator signal must
  never be the thing that breaks a read.

The metric carries only the caller-chosen `surface` label, never grant ids, so
a request cannot push it into unbounded cardinality. The accompanying log line
carries the grant **counts** rather than ids: an operator seeing a non-zero rate
needs to know which token to fix, and sizes answer that without putting grant
contents into logs.

`scripts/verify-telemetry-coverage.sh` — "telemetry-coverage.md and
instruments.go agree, no new untracked stages".

## What an operator does with it

A non-zero rate means a token is granted more than 128 repositories and its
infra reads are quietly incomplete. The fix is to widen the cap or move that
caller to an all-scopes token — not to read the missing rows as absence of
infrastructure.

Related: #5403, #5384, epic #5161.
