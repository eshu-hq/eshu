# CLI Component Command Bodies

## Purpose

The logic behind the `eshu component` command family, extracted from
`go/cmd/eshu` (issue #6059, epic #6053) so it lives where tests can import
it. It covers the local package-manager operations (inspect, verify, install,
list, enable, disable, uninstall), the extension conformance runner wrapper,
index verification for publication gates, the collector scaffolder behind
`component init collector`, the advisory extraction-readiness and
schema-versions reports, and the API-backed inventory and diagnostics
readbacks.

## Ownership boundary

This package owns rendering and orchestration only. Manifest, policy, and
registry semantics stay in `go/internal/component`; conformance execution in
`go/internal/extensionconformance`; index validation in
`go/internal/componentindex`; readiness policy in `go/internal/extraction`;
fact schema tables in `go/internal/facts`. On the other side, everything
process-bound stays in `go/cmd/eshu`: cobra flag resolution, the
`ESHU_COMPONENT_HOME` / `ESHU_HOME` fallback chain, the HTTP client, the
transport-failure classifier, and the envelope-code-to-exit-code table. A
Run function receives plain values and a writer, and returns the error the
command exits with.

## Exported surface

The `Run*` functions are one-per-subcommand command bodies; `FetchInventory`,
`FetchDiagnostics`, and `FinishAPI` serve the two API-backed subcommands over
the `EnvelopeFetcher` interface; `Envelope` / `EnvelopeError` are the
canonical response shape; `CLIOutput` and its members are the
`eshu.component.cli.v1` JSON payload. See `doc.go` for the godoc contract.

## Dependencies

`go/internal/component` (imported as `componentcore` to avoid shadowing the
package name), `go/internal/componentindex`, `go/internal/extensionconformance`,
`go/internal/extraction`, `go/internal/facts`, `go/internal/scope`, and
`gopkg.in/yaml.v3` for index decoding. No cobra anywhere in the dependency
closure (`go list -deps | rg spf13` is empty) and no direct `net/http`
import: the transport arrives through `EnvelopeFetcher`.

## Telemetry

None. These are CLI command bodies; their observable surface is the rendered
output itself.

No-Observability-Change: this package emits no metrics, spans, or logs, and
the extraction neither added nor removed an instrument. The commands render
onto the writer `go/cmd/eshu` hands them, exactly as they did before the
move.

No-Regression Evidence: command behavior is covered by
`go test ./internal/cli/component ./cmd/eshu -count=1`, and the extraction is
proven by a byte-identical CLI parity table (stdout, stderr, and exit codes)
between binaries built from the base commit and from this branch.

## Gotchas / invariants

- Behavior preservation is the extraction's contract: rendered bytes and
  returned errors must match what `go/cmd/eshu` produced before the move.
  Error text is operator-facing output -- do not add wrapping prefixes, which
  is also why several returns carry `//nolint:wrapcheck` justifications.
- Two JSON conventions coexist deliberately. The component payload and the
  API envelope disable HTML escaping (`writeJSON`); the extraction-readiness
  and schema-versions surfaces keep the encoder default and escape it. Do not
  unify them -- that changes emitted bytes.
- The envelope readers in `values.go` are private copies of `go/cmd/eshu`'s
  trace helpers, coexisting with the sets in `internal/cli/change` and
  `internal/cli/freshness` and with a differently named one in
  `internal/cli/entitymap`. Not every set has every reader: `boolValue`'s
  `go/cmd/eshu` original left with its last caller in #6059, and `stringsValue`
  is here but not in change or freshness, neither of which renders a string
  list. So an edit belongs in every set that has the reader you touched.
  `TestEnvelopeReaderParity` in `go/cmd/eshu` compares each reader across only
  the copies that reader has.
- `newCollectorSpec` resolves the scaffold output directory against the
  process working directory (`filepath.Abs`), the one piece of process state
  this package touches.
- `loadComponentIndex` deliberately replaces the file-read error with a fixed
  message so an operator's local path never leaks into archivable output.

## Related docs

- `docs/internal/design/license-headers.md` (scaffold templates carry
  publisher-owned headers)
- `docs/internal/remote-validation/prod-component-extension-inventory.md`
  (deployed proof of the inventory readback)
