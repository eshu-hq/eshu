# Query Package Moves Evidence (#6818)

Issue #6818 removes stuttering package names under `go/internal/query` by
moving packages to plain names. Each move rewrites import paths and package
qualifiers in the files that import the moved package. Some of those importers
are "hot" to `scripts/verify-performance-evidence.sh` because they contain
Cypher or goroutine fan-out, even though the move leaves their behavior
unchanged. This note records that evidence, with one section per move. Later
#6818 moves append a section here.

## `query/queryspan` to `query/tracing`

Baseline: merge base `9e542becc` (`origin/main` when the move was made).
After: branch `refactor/6818-tracing`, move commits `d2aeb0589`,
`c594ca6ed`, `af9e66c82`. No backend or version applies. The change is
compile-time only: no query runs differently, and no Postgres or NornicDB
work is involved.

The gate flagged two importers as hot:

- `go/internal/query/impact/contract.go` is hot for its Cypher text (the
  `MATCH (provider:Repository ...)-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)`
  read).
- `go/internal/query/impact/resource_investigation.go` is hot for its bounded
  goroutine fan-out (`sync.WaitGroup`, `chan error`, three `go func()`).
  It has no Cypher text.

In both files the diff changes two lines: the import
`github.com/eshu-hq/eshu/go/internal/query/queryspan` becomes
`.../query/tracing`, and the `queryspan.` qualifier on
`StartHandlerSpanWith` and `HandlerTracer` becomes `tracing.`.

No-Regression Evidence: the Cypher text and the concurrency code are
unchanged. The proof applies the substitution to the base blob and checks that
the result matches the head blob byte for byte:

```bash
git show 9e542becc:<file> \
  | sed 's#query/queryspan"#query/tracing"#; s/\bqueryspan\./tracing./g' \
  | cmp -s - <(git show HEAD:<file>)
# contract.go: exit 0
# resource_investigation.go: exit 0
```

That leaves no other changed byte, so no Cypher line and no goroutine,
channel, or WaitGroup line changed. A second check used `go/scanner` to
extract every string literal outside the import block, from both the base and
head versions of each file. It found the same list both times: 113 literals
in `contract.go` (sha256 prefix `a2ffd812bb2ee4ec` on both sides) and 38 in
`resource_investigation.go` (`fef564adc3f0b6ff` on both sides). The Cypher
statement is one of those literals, so the query sent to the graph is the
same. `go test ./internal/query/impact/... ./internal/query/tracing/...
./internal/query/language/... -count=1` passed (368 tests, exit 0). The
concurrency shape, fan-out width, and error channel capacity are unchanged, so
no performance measurement applies.

No-Observability-Change: the package move leaves the span name, attributes,
and tracer name unchanged. `tracing/handler.go` differs from
`queryspan/handlerspan.go` only in its package clause. The tracer is still
`otel.Tracer("eshu/go/internal/query")`, and the span still carries
`http.route`, `eshu.capability`, and `service.namespace`. Saved span queries
and dashboards that match on these names still work. The
`docs/public/observability/telemetry-coverage.md` row now points at
`handler.go`.
