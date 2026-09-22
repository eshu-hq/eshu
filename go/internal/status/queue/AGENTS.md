# Agent instructions: internal/status/queue

Scope: `go/internal/status/queue` only. The parent package's instructions in
`go/internal/status/AGENTS.md` still apply.

## The one rule that matters here

This package must not import `internal/status` (the root) or any sibling
leaf. The root aggregates the family leaves into `RawSnapshot`/`Report`, so
root and leaves both import `queue`; an import in the other direction is an
import cycle. If you find yourself needing a type from the root here (for
example the aggregate `QueueSnapshot` lifecycle counts, which stay in root),
the type is in the wrong place — that logic belongs in root, not here.

Keep the dependency set to the standard library plus `internal/status/shared`.
A new third-party or internal import in this package is a design change, not
a detail.

## Exporting the surface root calls

`cloneQueueBlockages`, `renderQueueBlockageLines`, `cloneQueueFailure`, and
`queueFailureText` are called directly from root `status.go`
(`renderCoordinatorLines`-adjacent report rendering) and are unexported
today. Export all four (`CloneBlockages`, `RenderBlockageLines`,
`CloneFailure`, `FailureText`) on the move — a partial export leaves root
with broken call sites.

The JSON projection for both types (`queueFailureJSON`/
`queueFailureJSONFromReport` and `queueBlockageJSON`/`queueBlockagesJSON`)
stays in root `json.go`; it is not part of this leaf's file list and does
not need to move. Root's projection functions will reference `Blockage` and
`FailureSnapshot` by their new qualified names.

## Credential and cardinality discipline

`FailureSnapshot` is explicitly a status-payload-only type: `WorkItemID`,
`ScopeID`, and `GenerationID` are unbounded-cardinality identifiers and must
never be promoted to a metric label. `FailureText` bounds
`FailureMessage`/`FailureDetails` at 240 characters each before rendering —
preserve that bound in any new caller.

## Verification

```bash
cd go && go test ./internal/status/... -count=1
```

Run the recursive `./internal/status/...` path, not the bare package — the
goldens and report-rendering tests that exercise this leaf's contract live
in the parent package's tests.
