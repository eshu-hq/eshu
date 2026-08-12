# CI/CD run correlation: one loader assertion instead of two

## What changed

`crossScopeIdentityLookupPlanned` became `crossScopeIdentityLookup` and returns
the typed loader (or `nil`) instead of a bool. `Handle` resolves it once and
hands the same value to both consumers: the cross-scope readiness floor gets
`identityLoader != nil`, and `loadActiveCICDRunCorrelationFacts` takes the
loader as a parameter.

Before this, the same type assertion ran twice — once in the planned-lookup
check and again inside the load. The second was unreachable, so the risk was
drift rather than a live bug: if the first check's condition changed, the dead
path stayed behind while its sibling moved.

## Why the second assertion was unreachable

Traced rather than assumed. `loadActiveCICDRunCorrelationFacts` had exactly one
caller, `Handle`. Its first statement was the planned-lookup check, which
returns early when the assertion fails. And `h` is a value receiver, so
`h.FactLoader` is a fixed copy for the whole call — both assertions read the
same value and cannot disagree, even if the original handler is mutated
concurrently.

## No behaviour change

Every caller shape behaves as before, including a `FactLoader` that does not
implement the cross-scope seam. That case is covered end to end by
`TestCICDRunCorrelationDoesNotDeferWithoutTheCrossScopeLoaderSeam`, which drives
`Handle` with a loader implementing `FactLoader` but not the seam and asserts
the write happens with the readiness lookup never consulted — the floor stays
inapplicable rather than becoming "not ready".

One edge is unchanged by the refactor and worth naming: a typed-nil pointer
implementing the interface still yields a non-nil interface value and still
calls through, exactly as the old assertion did.

No-Regression Evidence: pure deduplication of a type assertion on the handler's
setup path. One fewer interface assertion per `Handle` call; no query, loop,
lock, lease, batch, or queue behaviour is touched. `go test ./internal/reducer/
./cmd/reducer/ -count=1` exit 0, and `go test -race ./cmd/reducer/ -run
TestActiveWorkerExecutorTracksConcurrency -count=50` exit 0.

No-Observability-Change: no metric, span, log, or status field is added,
removed, or renamed. The readiness-defer log line and its
`cross_scope_producer_not_ready` failure class are untouched.

## What this does not claim

No new tests. This is deduplication with no observable change, and inventing
coverage for it would be noise rather than proof. The behaviour that matters is
already guarded by the test named above.

Refs #5709
