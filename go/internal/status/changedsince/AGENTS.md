# Agent instructions: internal/status/changedsince

Scope: `go/internal/status/changedsince` only. The parent package's
instructions in `go/internal/status/AGENTS.md` still apply.

## The one rule that matters here

This package must not import `internal/status` (the root) or any sibling
leaf. Even though root's `Report`/`RawSnapshot` do not currently aggregate
any changed-since type, the same one-way rule applies structurally: root and
every sibling leaf may import `changedsince`, never the reverse.

Keep the dependency set to the standard library. A new third-party or
internal import in this package is a design change, not a detail.

## This leaf has no root wiring to preserve

Unlike the other six leaves, no function here is called from root
`status.go`/`json.go`/`report.go` today — `internal/query/freshness` is the
only real consumer, importing this package's types directly for its
repository-scope and service-scope changed-since handlers. There is no
render/clone/JSON verb surface to export on the move, and no
`compat_changedsince.go` forwarder is needed unless `internal/query`'s
import path itself needs to keep resolving through the root package (check
`internal/query/freshness/changed_since.go` and `service_changed_since.go`
before assuming a plain import-path update is enough).

## Keep the repository- and service-scope shapes in lockstep

`ServiceFilter`/`ServiceSummary` deliberately reuse `Classification`,
`Counts`, `Sample`, `CategoryDelta`, and `UnavailableReason` from the
repository-scope contract rather than defining parallel types. Do not fork
these shapes when adding a new service-scope evidence family — add the new
`Category` constant to `ServiceCategories` instead, matching the SQL grouping
by `evidence_family` in `internal/query/freshness`.

## Verification

```bash
cd go && go test ./internal/status/... -count=1
cd go && go test ./internal/query/freshness/... ./internal/mcp -count=1
```

Run `internal/query/freshness` and `internal/mcp` because they are the real
consumers of this package's types; the parent `internal/status` suite mostly
proves the doc-comment contract, not runtime behavior for this leaf.
