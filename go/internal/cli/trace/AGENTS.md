# Agent Instructions: internal/cli/trace

Scoped rules for `go/internal/cli/trace`. The root `AGENTS.md` / `CLAUDE.md` and
the package [`README.md`](README.md) still apply.

## Read first

- [`doc.go`](doc.go) — the package contract
- [`README.md`](README.md) — ownership boundary and the invariants below
- `go/cmd/eshu/trace.go` — the command that consumes this package. Read it
  before changing any exported signature; it is the only caller.

## Invariants

1. **Nothing here imports cobra, reads the process environment, or decides a
   process exit code.** Those belong to `go/cmd/eshu`.
   `TestPackageImportsStayStandardLibraryOnly` in `doc_lockstep_test.go` is the
   gate: it rejects any non-standard-library import, so this holds without
   anyone remembering to check. By hand:
   `cd go && go list -deps ./internal/cli/trace | rg spf13` — empty is passing.

2. **Renderer output is pinned byte for byte.** `RenderServiceSummary` and
   `RenderServiceError` are covered by exact-string tests here and again through
   the command in `go/cmd/eshu/trace_test.go`. A line reordered, reworded, or
   newly emitted is a behavior change; update both suites deliberately and say
   why in the commit.

3. **`FetchServiceStory` must not wrap its transport error.** `go/cmd/eshu`
   renders that text verbatim to the operator and matches substrings of it to
   pick an error code, which picks the exit code. The `//nolint:wrapcheck` is
   there for that reason — do not "fix" it.

4. **The readers in `value.go` are copies.** Their siblings are the `mapValue`
   / `sliceValue` / `stringValue` / `intValue` sets in `internal/cli/change`,
   `internal/cli/freshness`, and `internal/cli/component` (component and this
   package also carry `stringsValue`), plus entitymap's differently named set.
   The `go/cmd/eshu` originals (`traceMap` and friends) are gone -- #6139 and
   #6059 removed their last callers there. Edit one copy and you edit every
   set that has that reader, or `TestEnvelopeReaderParity` in `go/cmd/eshu`
   fails and names yours; the entitymap twin tests pin that family's set
   against this one.

5. **Do not add a `boolValue` here** unless something genuinely reads a boolean.
   The parity test records its absence and fails when a reader named
   `boolValue` reappears here unregistered. That is the only name it knows, so
   a bool reader added under another name passes it. If
   you add one under any name, register it in that test's `names` map and drop
   the `absent` entry in the same change.

## Common changes

- **A new field in the summary:** add the read and the line in `render.go`,
  extend the exact-output test in `render_test.go`, and check whether
  `go/cmd/eshu/trace_test.go` asserts the surrounding lines.
- **A new query selector:** add the field to `ServiceQuery`, set it in
  `FetchServiceStory` only when non-empty, and fill it from a flag in
  `go/cmd/eshu/trace.go`. An empty selector must contribute no query parameter.
  `ServiceQuery` carries request selectors only — an output-mode or rendering
  flag stays in the `go/cmd/eshu` wrapper.
- **A new envelope shape from the API:** add the arm and a case to the shape
  tests. Prefer widening a reader over adding a fifth one.

## Failure modes seen here

- A reader that handles `int` but not `float64` reports every count from a real
  API response as zero, while every hand-built test envelope still passes.
- Rendering a section header before checking whether the section has rows tells
  an operator a path was found when none was.
- Wrapping the transport error changes the exit code an operator's script
  branches on, and no test outside `go/cmd/eshu` would notice.

## Do not change without owner review

- The exported signatures, while `go/cmd/eshu` is the only caller — a change
  here is a change to the command.
- The rendered line format, for the reasons in invariant 2.
