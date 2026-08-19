# AGENTS.md — go/internal/cli/freshness guidance for LLM assistants

## Read first

1. `go/internal/cli/freshness/README.md` — purpose, ownership boundary,
   exported surface, and the five invariants under "Gotchas / invariants"
2. `go/internal/cli/freshness/doc.go` — the godoc contract
3. `go/cmd/eshu/freshness.go` — the cobra wrapper for all three commands. It
   shows how the two halves fit: flags in, `RunGenerations` called,
   `freshnessExitError` converting a `*Failure` into `commandExitError`. The
   `changed-since` and `service-changed-since` wrappers live in the same file
   and are the same shape.
4. `go/cmd/eshu/freshness_parity_test.go` — drives this package against the
   real `internal/query.FreshnessHandler`, so a change to the rendered fields
   is checked against the canonical envelope, not a stub.

## Invariants this package enforces

- **No process wiring here.** No cobra flags, no process environment, no
  `os.Exit`, no `os.Stdout`. Every write goes to an `io.Writer` the caller
  supplies. `go/cmd/eshu` is `package main` and cannot be imported, so any
  symbol that reads a flag or names `commandExitError` belongs in the wrapper.
- **`ExitCodeForErrorCode` maps error codes, never freshness states.** These
  commands report a `building` or `stale` index and exit 0. Adding those
  strings to the switch would change operator-visible exit codes and is a
  decision, not a cleanup. `TestExitCodeForErrorCodeRejectsBuildingSpelling`
  and `TestRunGenerationsExitsZeroWhileTheIndexIsBuilding` guard it.
- **`ExitCodeForErrorCode` is one of two identical copies.** The other is
  `traceExitCode` in `go/cmd/eshu/trace.go` (the original, serving
  `eshu trace`, `eshu map`, component_api, and the envelope arm of
  `eshu change impact`). `go/cmd/eshu` is package main, so nothing can import
  it. Edit one and you must edit both — `TestExitCodeTableParity` in
  `go/cmd/eshu` feeds one code table to both copies and names the one that
  answered differently. Without it a one-sided edit ships an exit code that
  scripts branch on, with every test in the tree still green.
- **`ErrorCodeFromTransport` checks the message before the status.** The two
  `strings.Contains` calls run first and their `err != nil` guards are load
  bearing. `TestErrorCodeFromTransportMessagePrecedesStatus` fails if the
  order changes — it uses a status 400 whose text says "connection refused".
- **`ErrorCodeFromTransport` is one of four identical copies.** The others are
  `traceErrorCodeFromTransport` in `go/cmd/eshu/trace.go` (the original, still
  serving `eshu trace` and component_api), `change.ErrorCodeFromTransport`, and
  `transportErrorCode` in `go/internal/cli/entitymap`, which now serves
  `eshu map`. `go/cmd/eshu` is package main, so nothing can import the original
  and each family carries its own. Edit one and you must edit all four —
  `TestTransportErrorCodeParity` in `go/cmd/eshu` feeds one error table to the
  first three and names the one that answered differently, and
  `TestEntityMapTransportClassifierMatchesTrace` pins the entitymap copy.
- **Renderers write through `writef`.** Not `fmt.Fprintf`. See `write.go` for
  why, and do not add a second `//nolint:wrapcheck` without reading it.
- **The value readers are private copies.** `mapValue`, `sliceValue`,
  `stringValue`, and `intValue` were forked from `go/cmd/eshu`'s `traceMap` /
  `traceSlice` / `traceString` / `traceInt`, and `boolValue` from `traceBool`.
  The originals are gone: the component (#6139) and trace (#6059) extractions
  removed their last `go/cmd/eshu` callers, and the originals with them.
  `go/internal/cli/change/envelope.go` and
  `go/internal/cli/component/values.go` hold sets under these same names,
  `go/internal/cli/trace/value.go` holds the non-bool readers plus a strings
  reader, and `go/internal/cli/entitymap/values.go` a differently named set.
  An edit here belongs in every set that has the reader you touched;
  `TestEnvelopeReaderParity` in `go/cmd/eshu` compares each reader across only
  the copies that reader has and fails when one drifts, and
  `TestEntityMapValueReadersAreTokenIdenticalToTraceHelpers` pins the entitymap
  set. Do not try to share them through a new package without an owner's
  decision — every command family keeps its own copy and a shared home is a
  cross-family change.

## Common changes and how to scope them

- **Add a selector to an existing command** → add the field to the options
  struct, add one `setSelector` call in the path builder, add the flag and the
  field assignment in the wrapper, and extend that command's path table test.
  The wrapper's `...OptionsCarryEveryFlag` test in `go/cmd/eshu` fails if the
  flag is declared but never read.
- **Change what a summary prints** → edit the one `RenderXSummary` and its
  private helpers. The summary tests compare whole strings, so the diff shows
  exactly what an operator's terminal gains or loses. Re-run
  `go/cmd/eshu/freshness_parity_test.go` too: it asserts the rendered output
  against the real handler's envelope.
- **Add a new freshness subcommand** → follow `servicechangedsince.go`: a
  route constant, an options struct, a path builder, a `Fetch`, a `Run`, and a
  `RenderXSummary`. Reuse `fetch`, `finish`, `failureOf`, and
  `RenderEnvelopeError` rather than reimplementing the failure path.
- **Change an exit code** → change `ExitCodeForErrorCode`, its copy
  `traceExitCode` in `go/cmd/eshu/trace.go`, the table in
  `TestExitCodeForErrorCodeTable`, and the table in `TestExitCodeTableParity`
  in the same commit, and say in the PR body which scripted callers the change
  affects.

## Failure modes and how to debug

- Symptom: a command exits 1 where an operator expected 4 → check the error
  *code* the API returned, not the freshness state. `index_building` maps to
  4; the bare string `building` is a state and maps to the default 1. This is
  intentional; see the README.
- Symptom: a connection failure reports `api_error` instead of
  `backend_unavailable` → the error text no longer contains "connection
  refused" or "request failed", or the message checks were moved after the
  status switch.
- Symptom: `--json` exits 0 on a failed request → `finish` stopped returning
  `cmdErr` after writing the envelope. `TestRunGenerationsJSONWritesTheEnvelopeAndStillFails`
  covers it.
- Symptom: a selector with surrounding spaces reaches the server → a path
  builder is setting the raw value instead of going through `setSelector`.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported. If new logic needs a
  flag value or the real stdin, add a parameter.
- **Wrapping the transport error.** `fetch` returns `GetEnvelope`'s error
  unchanged on purpose: its text is rendered verbatim to the operator and its
  substrings drive `ErrorCodeFromTransport`. Wrapping changes both.
- **Screening the rendered error message.** It carries the request URL, and
  that is asserted in `redaction_test.go` with the limit written next to it. If
  screening is wanted, it is a product decision with an owner, and that test
  has to change deliberately.
- **Merging the three `RunX` functions.** They differ in options type, path,
  and renderer. The shared parts are already factored into `fetch`, `finish`,
  and `failureOf`.

## What NOT to change without an ADR

- The exit-code table, and the decision that a non-fresh freshness state exits
  0 here while the trace, change, and map families exit 4. Scripts depend on
  both halves.
- The `--json` output shape. It is the canonical envelope passed through, not
  a CLI-specific projection.
