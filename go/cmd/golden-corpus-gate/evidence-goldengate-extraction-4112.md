# Evidence — extract the pure assertion core into `internal/goldengate` (#4112 / R-10)

Scope: `go/cmd/golden-corpus-gate/{graph.go,query.go,mcp.go,runner.go}` plus the
new `shared.go`. The `verify-performance-evidence` gate content-flags `graph.go`
because it holds the gate's scalar-count Cypher (`MATCH (n:Label) RETURN
count(n)`, etc.). This change does **not** touch any Cypher, query shape, write
path, transaction scope, or concurrency.

## What changed

The pure, I/O-free assertion logic (`report.go`, `snapshot.go`, `evaluate.go`)
moved verbatim out of `package main` into the importable package
`go/internal/goldengate`, with the `evaluate*` functions exported as
`Evaluate*`. `cmd/golden-corpus-gate/shared.go` re-exports those symbols under
the original package-local names via Go type/identifier aliases, so the gate's
call sites changed by name only:

- `graph.go`, `query.go`, `mcp.go`, `runner.go` now call `EvaluateNodeCount`,
  `EvaluateEdgeCount`, `EvaluateRequiredCorrelation`, `EvaluateEdgeProperty`,
  `EvaluateNodeProperty`, `EvaluateRequiredNode`, `EvaluateNodePresent`,
  `EvaluateQueryShape`, `EvaluateDrains`, `EvaluateTiming` — the same functions,
  same arguments, same results, now resolved through the alias to the shared
  package.

The Cypher executed by `boltGraphCounter` (`CountNodes`, `CountEdges`,
`CountCorrelation`, `ListNodeProperty`, `ListCorrelationEdgeProperty`) is
byte-for-byte unchanged; `graph.go`'s only edits are the renamed function calls.

The motivation is the R-10 contributor conformance suite (`go/conformance`),
which replays a cassette credential-free and Docker-free and must assert against
the **same** logic with no forked copy — now guaranteed because both the gate and
the suite import `internal/goldengate`.

## No-Regression Evidence:

No runtime behaviour changed. The graph workload against the backend is identical
— the same scalar-count Cypher runs the same number of times with the same
cardinality, batch size, and transaction scope; only the Go symbol resolving the
returned `Finding` moved packages. The behaviour-preserving extraction is proven
by the gate's own full unit suite, which exercises every evaluator and the
graph/query/drain checkers against fakes, passing unchanged after the move:

```
$ cd go && go test ./internal/goldengate/... ./cmd/golden-corpus-gate/... -count=1
ok  github.com/eshu-hq/eshu/go/internal/goldengate
ok  github.com/eshu-hq/eshu/go/cmd/golden-corpus-gate
```

No new query, no extra round trip, no change to the count tolerances or required
correlations the snapshot pins.

## No-Observability-Change:

`golden-corpus-gate` is an offline CI assertion binary, not a runtime service; it
emits no metrics, spans, or status. Its operator-facing output is the
pass/required-fail report it already prints (`Report.Write`, unchanged). This
change adds no runtime stage and alters no telemetry contract. The new
`internal/goldengate` package and the `go/conformance` suite are likewise
offline, no-observability test surfaces.
