# Agent instructions: internal/status/shared

Scope: `go/internal/status/shared` only. The parent package's instructions in
`go/internal/status/AGENTS.md` still apply.

## The one rule that matters here

This package must not import any other package under `internal/status`, and
must not import `internal/status` itself. The root aggregates the family leaves
into `RawSnapshot`/`Report`, so root and leaves both import `shared`; an import
in the other direction is an import cycle. If you find yourself needing a type
from the root here, the type is in the wrong place — move it down, do not
reach up.

Keep the dependency set to the standard library. A new third-party or internal
import in this package is a design change, not a detail.

## Changing the wire helpers

`NamedCountJSON`, `NamedCountsJSON` and `NullableRFC3339Value` render
operator-facing status JSON. Their struct tags and time formatting are the
published contract for the status HTTP surfaces and MCP status tools.

Before changing any of them, know that they are locked by byte-for-byte
goldens: `internal/status/testdata/render_json_golden.json`,
`render_text_golden.txt`, and the explicit dotted-key-path list in
`render_json_key_paths_golden.txt`. A failure there is a real API break to
justify, not a golden to regenerate reflexively.

Keep `NamedCount` and `NamedCountJSON` field-for-field identical — the
projection uses a direct struct conversion and will stop compiling otherwise.

## Verification

```bash
cd go && go test ./internal/status/... -count=1
```

Run the recursive `./internal/status/...` path, not the bare package — the
goldens that prove these helpers' contract live in the parent package's tests.
