# Service Report

## Purpose

`servicereport` owns the logic behind `eshu service-report`: reading a
captured `get_service_story` response (and an optional captured
supply-chain-impact-inventory response), decoding both into the inputs
`internal/serviceintel` composes from, and rendering the composed report as
human-readable text. It runs offline against captured JSON -- no query,
store, or LLM path -- so a given captured response renders the same report
every time.

## Ownership boundary

This package owns report-input *logic*: parsing a captured response envelope,
reading the optional supply-chain file, and formatting the composed
`serviceintel.Report` for a terminal. It does not own process wiring: reading
cobra flags, resolving the process's stdin stream, or mapping errors to exit
codes. Those stay in `go/cmd/eshu/service_report_cmd.go`, the cobra `RunE`
wrapper, because `go/cmd/eshu` is `package main` and nothing can import it.
The wrapper resolves process state (flags, `cmd.InOrStdin()`,
`cmd.OutOrStdout()`) and passes it into this package as plain values; this
package returns data and errors, printing only through the `io.Writer` the
caller supplies to `RenderReport`.

This package never composes a report. Nothing in it calls
`serviceintel.Compose` or `serviceintel.FromServiceStory` -- the wrapper's
`runServiceReport` calls both and hands the finished `serviceintel.Report` to
`RenderReport`. Composing is `internal/serviceintel`'s job, and it is not the
CLI's either: `internal/serviceintelhttp` serves the same report over HTTP and
`internal/answerquality` composes reports for its corpus, neither of them
going through `go/cmd/eshu` or this package. What this package does is decode
a captured response into the dossier map and truth envelope those composition
calls consume, adapt the optional supply-chain file through
`serviceintel.FromSupplyChainInventory` -- the one `serviceintel` function its
production code calls -- and render the finished report. (The package's tests
do call `Compose`, to render a genuinely composed report rather than asserting
only against a hand-built stand-in.)

## Exported surface

- `ReadInput` -- reads the captured service-story response from a file path
  or, when the path is blank or whitespace-only, from the supplied
  `io.Reader` (the wrapper's stdin). It has three error returns: a failed
  file read, a failed stdin read, and stdin that arrives empty or
  whitespace-only
- `ParseServiceStoryResponse` -- decodes a captured response into the
  dossier map and optional `query.TruthEnvelope`, accepting both the
  `{"data": ..., "truth": ...}` envelope and a bare dossier object; a `data`
  that decodes to nil (absent or explicitly null) takes the bare path
- `SupplyChainSection` -- reads an optional captured supply-chain inventory
  file and adapts it into a `serviceintel.SectionInput`; returns a nil
  section when the path is blank or whitespace-only
- `RenderReport` -- writes the compact, human-readable view of a
  `serviceintel.Report` to an `io.Writer`

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/serviceintel` -- `FromSupplyChainInventory`, `Report`,
  `ReportSubject`, `SectionInput` and related shapes; this package adapts
  captured HTTP responses into `serviceintel`'s input types and never composes
  a report (`serviceintel.Compose` runs in the wrapper's `runServiceReport`
  alongside `serviceintel.FromServiceStory`, next to the JSON-vs-text branch,
  and in `internal/serviceintelhttp` and `internal/answerquality`, which do
  not involve this package at all)
- `internal/query` -- `query.TruthEnvelope`, decoded from the captured
  response envelope
- Consumed by `go/cmd/eshu`: the `service-report` command
  (`service_report_cmd.go`)

## Telemetry

None. `service-report` runs inline with the CLI invocation against a
captured local file or stdin; there is no background pipeline stage to
instrument.

## Gotchas / invariants

- `ReadInput` treats all-whitespace stdin as an error (no usable `--from`
  path was given and stdin carried nothing). It is not there to stop a silent
  empty-dossier report -- empty bytes never get that far, because
  `ParseServiceStoryResponse` cannot decode them. It is there for the message:
  the operator gets `no service-story response provided; pass --from or pipe
  JSON on stdin` instead of `unexpected end of JSON input`. The check applies
  to stdin only, so an empty file at a real `--from` path is returned as empty
  bytes and fails a step later, at decode, with the unhelpful message.
- A whitespace-only `--from` path reads stdin. `ReadInput` branches on
  `strings.TrimSpace(path) != ""`, so `--from "   "` never opens a file --
  it is the one case where a non-empty flag value behaves as if the flag
  were absent.
- `SupplyChainSection` reuses `ParseServiceStoryResponse` for the
  supply-chain file, so both captured inputs accept the same
  envelope-or-bare-object shape.
- `RenderReport` is a terminal-formatting function only, and it is the sole
  producer of `service-report`'s text output. The JSON output path (`--json`)
  does not call it -- the wrapper marshals `serviceintel.Report` directly so
  the JSON shape stays exactly what `serviceintel` produces.
- The text form prints a fixed subset of the report, not all of it: the
  subject label (service name, plus service id in parentheses when both are
  set), the report-level `supported` / `partial` / `truth_class`, each
  section's `status`, `title`, and its answer's `summary`,
  `unsupported_reasons` and `limitations`, each recommended next call's label
  (the first of `tool`, `route`, `playbook` that is set) and `reason`, and
  each suggested investigation's `basis`, `reason`, next-call label and
  `expected_truth_class`. **Everything else the JSON carries is absent from
  the text by design** -- `schema`, the report-level `truth` envelope and
  aggregated `limitations`, `subject.repo_id` and `subject.repo_name`, each
  section's `kind`, each next call's `arguments`, each investigation's `id`,
  `section` and `evidence_basis`, and every other field of a section's answer
  packet, `evidence_handles` included. `TestRenderReportJSONKeyContract` pins
  that classification key by key, so a field added to `serviceintel.Report`
  fails the test until somebody classifies it.
- `RenderReport` returns nothing and discards its `io.Writer`'s write
  errors, so a broken stdout does not fail `service-report`.

## Related docs

- `docs/public/reference/service-intelligence-report.md` -- the `eshu
  service-report` command reference
- `go/internal/serviceintel/README.md` -- the report composition contract
  this package adapts captured input into
