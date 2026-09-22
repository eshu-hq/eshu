# Agent instructions: internal/status/cloud

Scope: `go/internal/status/cloud` only. The parent package's instructions in
`go/internal/status/AGENTS.md` still apply.

## The one rule that matters here

This package must not import `internal/status` (the root) or any sibling
leaf. The root aggregates the family leaves into `RawSnapshot`/`Report`, so
root and leaves both import `cloud`; an import in the other direction is an
import cycle. `collector` is allowed to import `cloud` (a declared, one-way
exception) to fold `AWSScanStatus` into its unified runtime view — that
exception does not run in reverse, so this package must never import
`collector` or reference any of its types.

Keep the dependency set to the standard library plus `internal/status/shared`.
A new third-party or internal import in this package is a design change, not
a detail.

## Changing the wire helpers

`AWSScanJSON`, `AWSScansJSON`, `AWSFreshnessJSON`, and `AWSFreshnessJSONFrom`
render operator-facing status JSON. Their struct tags and time formatting are
the published contract for the status HTTP surfaces and MCP status tools.

Before changing any of them, know that they are locked by byte-for-byte
goldens: `internal/status/testdata/render_json_golden.json`,
`render_text_golden.txt`, and the explicit dotted-key-path list in
`render_json_key_paths_golden.txt`. A failure there is a real API break to
justify, not a golden to regenerate reflexively.

## Naming note

This package kept the `AWS` prefix on its exported names
(`AWSScanStatus`, `AWSScanJSON`, `AWSFreshnessSnapshot`, ...) and only
dropped the redundant `Cloud` segment — `AWSCloudScanStatus` became
`AWSScanStatus`, not `ScanStatus`. `AWS` disambiguates the collector family
from a future non-AWS cloud collector in the same package; do not rename to
`cloud.ScanStatus` under a literal reading of the no-stutter rule, since that
would collide in meaning with any future provider added to this package.

## Verification

```bash
cd go && go test ./internal/status/... -count=1
```

Run the recursive `./internal/status/...` path, not the bare package — the
goldens that prove this leaf's contract live in the parent package's tests.
Also run `./internal/status/collector/...` (once it exists) when changing
`AWSScanStatus`'s shape, since `collector.RuntimeStatuses` folds it into the
unified runtime view.
