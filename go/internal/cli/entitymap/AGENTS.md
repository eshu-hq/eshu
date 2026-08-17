# AGENTS.md — go/internal/cli/entitymap guidance for LLM assistants

## Read first

1. `go/internal/cli/entitymap/README.md` — purpose, ownership boundary,
   exported surface, and the classification-order invariants
2. `go/internal/cli/entitymap/doc.go` — the godoc contract
3. `go/cmd/eshu/map.go` — the cobra `RunE` wrapper that reads flags, builds
   the API client, and maps a `Failure` to an exit code. This is the file
   that shows how the two halves fit together.
4. `go/internal/cli/apierr/README.md` — how a transport error's HTTP status
   crosses the `package main` boundary

## Invariants this package enforces

- **No process wiring here.** No cobra flags, no reads of Eshu config or a
  credential from the process environment, no file access, no `os.Exit`. The
  API is reached only through the `EnvelopePoster` a caller passes in, and
  output goes only to a caller-supplied `io.Writer`. `go/cmd/eshu` is
  `package main`, so nothing can import it — any symbol that reads a flag or
  produces an exit code has to live in `map.go` instead.
- **Classification order is a contract, not a detail.** `Resolve` checks
  transport error, then envelope error, then freshness, then resolution
  status. `ErrorCodeFromTransport` checks 409, then the "connection refused"
  / "request failed" substrings, then the HTTP status. Both orders are pinned
  by tests; changing either changes which exit code an operator gets and what
  they are told to go fix.
- **`transportErrorCode` mirrors `traceErrorCodeFromTransport` in
  `go/cmd/eshu/trace.go`.** They are separate copies on purpose (that file is
  `package main`), so a behavior change to one is a bug unless it is made to
  both. `TestEntityMapTransportClassifierMatchesTrace` in
  `go/cmd/eshu/entitymap_parity_test.go` enforces this by running one input
  table through both classifiers, and
  `TestEntityMapTransportClassifierPinnedDivergences` pins the only allowed
  differences (409 and nil). From inside this package,
  `TestTransportClassifierMatchesItsTraceOriginal` in `twin_source_test.go`
  compares the two bodies' source with the `http.Status*` constants
  normalised to their numeric values, so the focused loop
  `go test ./internal/cli/entitymap/` also goes red on drift.
- **The value readers in `values.go` are copies too**, forked from the
  `trace*` helpers that used to live in `go/cmd/eshu/trace.go`. Those
  originals are gone: the component (#6139) and trace (#6059) extractions
  removed their last callers there. The set these are pinned against is now
  `go/internal/cli/trace/value.go`, whose readers are unexported, so
  "deduplicating" by importing it does not work either.
  `TestEntityMapValueReadersAreTokenIdenticalToTraceHelpers` in
  `go/cmd/eshu/entitymap_parity_test.go` enforces token-identical function
  bodies for all six readers (change both or neither), and
  `TestEntityMapFreshnessStateMatchesTraceReaders` adds a behavioral table
  for the two reachable through the exported surface.
  `TestValueReadersMatchTheirTraceOriginals` in `twin_source_test.go` runs
  the same source comparison from inside this package, so the focused loop
  catches a reader edit too.

## Common changes and how to scope them

- **Add or rename a request field** → edit the body map in `Fetch` and the
  matching `Options` field, then the flag in `go/cmd/eshu/map.go`'s
  `addEntityMapFlags` and `entityMapOptionsFromCommand`. The API's route
  contract is the source of truth for the JSON key.
- **Add a new failure outcome** → add a `FailureKind` constant here, return
  it from `Resolve`, and add its exit code to `entityMapExitCode` in
  `map.go`. `TestEntityMapExitCodeMapsEveryFailureKind` in
  `go/cmd/eshu/map_test.go` covers every kind; add the new row there.
- **Change the text summary's layout** → edit `RenderSummary`,
  `RenderSection`, or the `sections` list in `render.go`. The `--json` shape
  is untouched by those: `WriteJSON` marshals the envelope as decoded.
- **Add a summary section** → add a row to the `sections` var in `render.go`.
  The list is fixed rather than a range over the response map because the
  print order is part of the command's output contract.

## Failure modes and how to debug

- Symptom: `eshu map` reports a bad selector when the backend is down →
  check `transportErrorCode`'s ordering first. The substring checks must run
  before the status switch; a reorder turns a status-400 "connection refused"
  error from `backend_unavailable` into `invalid_argument`.
- Symptom: a section prints its title with nothing under it →
  `RenderSection` returns early on an empty slice, so the cause is upstream:
  the response carried a non-empty slice of values that are not JSON objects,
  which `RenderSection` skips row by row.
- Symptom: `--json` output is missing a member the API returned →
  `Envelope.Data` and `Truth` are `map[string]any` precisely so this cannot
  happen. Look at the API's response instead.

## Anti-patterns specific to this package

- **Mapping a `FailureKind` to an exit code here.** That table lives in
  `go/cmd/eshu/map.go` next to the rest of the CLI's exit codes.
- **Typing `Envelope.Data` or `Envelope.Truth` into structs.** They stay
  untyped so `--json` passes through members the API adds later.
- **Returning a wrapped transport error from `Fetch`.** The message reaches
  the operator verbatim and `ErrorCodeFromTransport` classifies on its
  substrings; the `//nolint:wrapcheck` there states that reason.

## What NOT to change without an ADR

- The classification order in `Resolve` and `ErrorCodeFromTransport`. Both
  are operator-visible behavior contracts, and both were preserved verbatim
  through the extraction out of `package main`.
