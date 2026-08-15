# Change Impact And Plan

## Purpose

This package is the body of the `eshu change` command family. `eshu change
impact` answers "what does this diff touch"; `eshu change plan` turns the same
evidence into a ranked list of suggested actions. Both post a changed-file set
to the API and print what comes back.

It exists as its own package because `go/cmd/eshu` is `package main`. Nothing
in the repository can import that directory, so none of this logic was testable
except through a built binary. Moving it here makes the diff parsing, the flag
validation, the fail-closed rules, and the rendering reachable from a normal Go
test.

## Ownership boundary

Owned here: turning a git diff or a `--file` list into `[]FileChange`; the flag
rules `Validate` enforces; the two request bodies; the fail-closed rules that
decide an answer is unusable; and every byte the two commands print.

Not owned here, and deliberately: reading cobra flags, resolving
`cmd.OutOrStdout()`, reading the process environment, holding the API client,
and mapping a result to a process exit code. Those live in
`go/cmd/eshu/change_impact.go`. The exit-code split is the one worth knowing
about — see Gotchas below.

## Exported surface

Changed-file derivation:

- `GitDiffNameStatus(repoPath, baseRef, headRef)` — runs `git diff
  --name-status` with rename and copy detection on.
- `ParseNameStatusDiff(output)` — parses that output into `[]FileChange`.
- `NormalizeStatus(status)` — maps a git status letter to `added`, `deleted`,
  `renamed`, `copied`, or `modified`.
- `ModifiedFiles(paths)`, `ChangedPaths(changes)`, `CleanValues(values)` — the
  helpers for an explicit `--file` list.

Request and response:

- `Options`, `FileChange`, `Envelope`, `EnvelopeError` — the data shapes.
- `ImpactRoute`, `PlanRoute` — the two API paths.
- `ImpactRequestBody(opts)`, `PlanRequestBody(opts)` — the two bodies.
- `FinishImpact(w, opts, envelope, cmdErr)`, `FinishPlan(...)` — render and
  return `cmdErr` unchanged.

Failure classification:

- `Failure`, `FailureKind`, and the four kinds `KindInvalidArgument`,
  `KindEnvelope`, `KindFreshness`, `KindIncomplete`.
- `Kinds()` — those four as a slice, so the caller mapping them to exit codes
  can prove it covered all of them.
- `Validate(opts)`, `ClassifyImpact(envelope)`, `ClassifyPlan(envelope)`.
- `ErrorCodeFromTransport(err)`, `EnvelopeFromTransportError(err)`,
  `EnvelopeFailure(e)`.

See `doc.go` for the godoc contract.

## Dependencies

One internal package, `go/internal/cli/apierr`, for reading an HTTP status out
of an error chain. Everything else is standard library.

There is no cobra dependency, and that is checkable rather than a claim:

```bash
cd go && go list -deps ./internal/cli/change | rg spf13   # prints nothing
```

## Telemetry

None. This package runs one subprocess (`git`) and formats text; it opens no
connection, claims no work, and has no pipeline stage to instrument. The
operator-facing signal is the command's own output and its exit code, both
owned by `go/cmd/eshu/change_impact.go`.

## Gotchas / invariants

- **Exit codes are the caller's, and two of them differ from the shared
  table.** `traceExitCode` in `go/cmd/eshu` answers 1 for `building` and 1 for
  `truncated`; the change family exits 4 on a still-building index and 5 on a
  truncated answer. `changeExitCode` answers those two directly and routes only
  `KindEnvelope` through the shared table. `TestChangeExitCodeMapping` fails if
  that stops being true. It also walks `Kinds()`, so a kind added without its
  arm fails there instead of falling to the default exit code and reading as
  correct. `TestKindsListsEveryDeclaredConstant` parses `failure.go` so that
  walk cannot be short a kind: a constant declared but never added to `Kinds()`
  fails in `change` before it can hide from the mapping test.
- **`ErrorCodeFromTransport` checks the message before the status.** A retry
  wrapper can carry the last response's status while the real answer is that
  the backend was unreachable. `TestErrorCodeFromTransportPrecedence`'s
  status-400-saying-connection-refused row is the only case that tells the two
  orders apart.
- **Rename and copy rows carry both endpoints.** `ParseNameStatusDiff` reads
  git's three-field line as source-then-target. Reading it the other way round
  produces a plausible pair pointing backwards in time, and the API has no way
  to notice.
- **`CleanValues` never returns nil.** A nil slice marshals as JSON `null`,
  which the route reads as a missing field rather than as "no paths".
- **Freshness is checked before truncation.** A stale answer reports staleness,
  not the truncation staleness often causes.
- **The trace helpers here are copies, not shared code.** `mapValue`,
  `stringValue`, `intValue`, `boolValue`, `sliceValue`, and
  `ErrorCodeFromTransport` mirror `go/cmd/eshu`'s `traceMap` and friends and
  `traceErrorCodeFromTransport`, which still have callers that have not moved
  (`component_api.go`, `map.go`, `trace.go`, `trace_render.go`, and the
  freshness family). The two copies coexist until a shared home exists; do not
  delete either side assuming the other covers it.
  `ErrorCodeFromTransport` is the one to watch: it is the only copy with real
  logic in it, and #6117 already edited the original mid-epic. An edit there
  that misses this copy would leave `eshu change impact` and `eshu change plan`
  classifying on the old table. `TestTransportErrorCodeParity` in
  `go/cmd/eshu` feeds one error table to both functions and fails when their
  answers differ.
- **The envelope error message is printed verbatim.** For a transport failure
  it is the Go error, which embeds the service URL an operator set. That is the
  point of the message, and this package does not screen it.
  `TestEnvelopeErrorMessageIsPrintedVerbatim` records the behavior so a future
  reader does not assume a screen exists.

## Related docs

- `go/internal/cli/apierr/README.md` — why the HTTP status crosses the package
  boundary through an interface
- `go/cmd/eshu/change_impact.go` — the cobra wrapper and the exit-code table
- `docs/public/reference/http-api.md` — the two routes this package posts to
