# ifa

## Purpose

`ifa` is the command entry point for the Ifá conformance platform. In P0
([#4393](https://github.com/eshu-hq/eshu/issues/4393)) it is intentionally a
small shell that proves the command/package boundary exists while the library
establishes Odù canonicalization over `facts.Envelope`.

## Ownership Boundary

This command owns CLI entry wiring only. The contract-layer Odù behavior lives
in `go/internal/ifa`; future `make prove` orchestration should call that library
instead of putting conformance logic in `main.go`.

## Exported Surface

Run `go run ./cmd/ifa -version` to confirm the command is available.

## Dependencies

The P0 command uses only the Go standard library.

## Telemetry

No runtime telemetry is emitted. This is not a deployed service.

## Related Docs

- `go/internal/ifa/README.md`
- `docs/internal/design/4389-ifa-conformance-platform.md`
