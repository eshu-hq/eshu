# Agent instructions: internal/status/generation

Scope: `go/internal/status/generation` only. The parent package's
instructions in `go/internal/status/AGENTS.md` still apply.

## The one rule that matters here

This package must not import `internal/status` (the root) or any sibling
leaf. The root aggregates the family leaves into `RawSnapshot`/`Report`, so
root and leaves both import `generation`; an import in the other direction
is an import cycle. `GenerationHistorySnapshot` stays in root — do not pull
it down here just because the names look related.

Keep the dependency set to the standard library. A new third-party or
internal import in this package is a design change, not a detail.

## Exporting the surface root calls

`cloneGenerationTransitions`, `generationTransitionsText`, and
`generationTransitionsJSON` are called directly from root `status.go`/
`json.go` and are unexported today. Export them (`CloneTransitions`,
`TransitionsText`, `TransitionsJSON`) on the move — a partial export leaves
root with broken call sites.

## External consumers beyond root

`LifecycleFilter`, `LifecycleRecord`, and `LifecyclePage` are consumed
directly by `internal/query/freshness` (the freshness generation drilldown
surface) and by `internal/mcp`'s freshness-parity tests, not only by root
`internal/status`. Check those call sites, not just the root package, before
changing any of these three types' shape.

## Verification

```bash
cd go && go test ./internal/status/... -count=1
cd go && go test ./internal/query/freshness/... ./internal/mcp -count=1
```

Run the recursive `./internal/status/...` path for the goldens and report
rendering, and the `freshness`/`mcp` packages because they read the
drilldown filter/record/page types directly, not just through the aggregated
`Report`.
