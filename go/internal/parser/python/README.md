# Python Parser

## Purpose

This package parses Python source and notebooks into Eshu parser payloads. It
owns Python syntax traversal, imports, calls, classes, functions, annotations,
lambda and generator support, public API roots, and parser-grounded dead-code
root evidence.

## Ownership Boundary

`internal/parser/python` is a language adapter. It does not own parser registry
dispatch, parse timing, grammar lifecycle, fact emission, graph projection, or
query truth. Notebook extraction stays local to this package; runtime
orchestration remains in the parent parser and collector paths.

## Exported Surface

See `doc.go` and `go doc ./internal/parser/python`. The main export is the
language parser entrypoint used by the parent parser, with helper types kept
package-local unless callers need them for tests or integration.

## Telemetry

This package emits no metrics or spans. Parent parser instrumentation records
file parse duration and parser failures.

## Gotchas / Invariants

- Dead-code roots must be syntax-grounded; package naming or path heuristics
  are not enough.
- Notebook support must keep cell-derived source deterministic and bounded.
- Public API roots, imports, and call inference should stay conservative when
  dynamic Python shapes are ambiguous.
- Payload ordering must remain deterministic across retries.

## Focused Tests

```bash
cd go
go test ./internal/parser/python -count=1
go run ./cmd/eshu docs verify ../go/internal/parser/python --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `go/internal/parser/README.md`
- `docs/public/contributing-language-support.md`
