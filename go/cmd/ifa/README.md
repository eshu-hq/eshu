# ifa

## Purpose

`ifa` is the command entry point for the Ifá conformance platform
([#4393](https://github.com/eshu-hq/eshu/issues/4393),
[#4394](https://github.com/eshu-hq/eshu/issues/4394)). P0 shipped a thin shell
proving the command/package boundary. P1 adds two subcommands over that
shell: `ifa coverage`, which reconciles `go/internal/ifa`'s derived
expectations against `specs/ifa-coverage-manifest.v1.yaml`, and
`ifa expectations`, which prints the derivation itself.

## Ownership Boundary

This command owns CLI entry wiring, flag parsing, and report I/O only. All
conformance, derivation, and coverage logic lives in `go/internal/ifa`;
`coverage.go` and `expectations.go` here are thin subcommand wrappers that load
inputs from disk and call into that library.

## Exported Surface

- `ifa -version` - prints the command's version banner (P0, unchanged).
- `ifa coverage [-specs-dir] [-snapshot] [-manifest] [-replay-manifest]
  [-gates] [-report-out] [-blocking]` - runs `ifa.RunCoverage` and prints the
  advisory/blocking summary; writes the JSON report when `-report-out` is set;
  exits non-zero only when `-blocking` is passed and the gate fails.
- `ifa expectations [-specs-dir] [-snapshot] [-replay-manifest] [-kind]
  [-format json]` - prints `ifa.Derive`'s output as JSON, optionally filtered
  to one fact kind.

## Dependencies

The command depends on `go/internal/ifa`, `go/internal/facts`,
`go/internal/goldengate`, `go/internal/replaycoverage`, and
`go/internal/cigates` for loading and reconciling its inputs. It intentionally
does not depend on collector or parser internals.

## Telemetry

No runtime telemetry is emitted. This is not a deployed service; the coverage
report and stdout summary are the operator-facing artifacts.

## Related Docs

- `go/internal/ifa/README.md`
- `docs/internal/design/4389-ifa-conformance-platform.md`
