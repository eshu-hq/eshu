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

Composing the report is `internal/serviceintel`'s job, and the calls that
drive it stay in the wrapper: `runServiceReport` is the only caller of
`serviceintel.FromServiceStory` and `serviceintel.Compose`. This package
decodes a captured response into the dossier map and truth envelope those
calls consume, adapts the optional supply-chain file through
`serviceintel.FromSupplyChainInventory` (the one `serviceintel` adapter this
package does call), and renders the finished `serviceintel.Report`. It never
calls `serviceintel.Compose` itself.

## Exported surface

- `ReadInput` -- reads the captured service-story response from a file path
  or, when no path is given, from the supplied `io.Reader` (the wrapper's
  stdin), erroring on empty stdin input
- `ParseServiceStoryResponse` -- decodes a captured response into the
  dossier map and optional `query.TruthEnvelope`, accepting both the
  `{"data": ..., "truth": ...}` envelope and a bare dossier object
- `SupplyChainSection` -- reads an optional captured supply-chain inventory
  file and adapts it into a `serviceintel.SectionInput`; returns `nil` when
  no path is given
- `RenderReport` -- writes the compact, human-readable view of a
  `serviceintel.Report` to an `io.Writer`

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/serviceintel` -- `FromSupplyChainInventory`, `Report`,
  `ReportSubject`, `SectionInput` and related shapes; this package adapts
  captured HTTP responses into `serviceintel`'s input types but does not
  compose the report itself (`serviceintel.Compose` runs in the wrapper's
  `runServiceReport`, alongside `serviceintel.FromServiceStory`, since both
  sit next to the mode branch for JSON vs. text output)
- `internal/query` -- `query.TruthEnvelope`, decoded from the captured
  response envelope
- Consumed by `go/cmd/eshu`: the `service-report` command
  (`service_report_cmd.go`)

## Telemetry

None. `service-report` runs inline with the CLI invocation against a
captured local file or stdin; there is no background pipeline stage to
instrument.

## Gotchas / invariants

- `ReadInput` treats all-whitespace stdin as an error (no `--from` path was
  given and stdin carried nothing), rather than falling through to a silent
  empty-dossier report.
- `SupplyChainSection` reuses `ParseServiceStoryResponse` for the
  supply-chain file, so both captured inputs accept the same
  envelope-or-bare-object shape.
- `RenderReport` is a terminal-formatting function only. The JSON output
  path (`--json`) does not call it -- the wrapper marshals
  `serviceintel.Report` directly so the JSON shape stays exactly what
  `serviceintel` produces.

## Related docs

- `docs/public/reference/service-intelligence-report.md` -- the `eshu
  service-report` command reference
- `go/internal/serviceintel/README.md` -- the report composition contract
  this package adapts captured input into
