# ifa

## Purpose

`ifa` is the command entry point for the Ifá conformance platform
([#4393](https://github.com/eshu-hq/eshu/issues/4393),
[#4394](https://github.com/eshu-hq/eshu/issues/4394)). P0 shipped a thin shell
proving the command/package boundary. P1 adds two subcommands over that
shell: `ifa coverage`, which reconciles `go/internal/ifa`'s derived
expectations against `specs/ifa-coverage-manifest.v1.yaml`, and
`ifa expectations`, which prints the derivation itself. P2
(`drive.go`, issue #4395) adds `ifa drive`, the concurrent replay driver verb:
it drives `go/internal/replay/concurrentreplay.Driver` over a recorded
cassette against a real Postgres `IngestionStore`, proving the acceptance
clause "driver passes -race; same Odù drains (`fact_work_items.residual_max:0`)
at N=1" end to end.

## Ownership Boundary

This command owns CLI entry wiring, flag parsing, and report I/O only. All
conformance, derivation, and coverage logic lives in `go/internal/ifa`;
`coverage.go` and `expectations.go` here are thin subcommand wrappers that load
inputs from disk and call into that library. `drive.go` is a thin wrapper the
same way: cassette parsing and concurrent-safe draining live in
`go/internal/replay/cassette` and `go/internal/replay/concurrentreplay`, not
here.

## Exported Surface

- `ifa -version` - prints the command's version banner (P0, unchanged).
- `ifa coverage [-specs-dir] [-snapshot] [-manifest] [-replay-manifest]
  [-gates] [-report-out] [-blocking]` - runs `ifa.RunCoverage` and prints the
  advisory/blocking summary; writes the JSON report when `-report-out` is set;
  exits non-zero only when `-blocking` is passed and the gate fails.
- `ifa expectations [-specs-dir] [-snapshot] [-replay-manifest] [-kind]
  [-format json]` - prints `ifa.Derive`'s output as JSON, optionally filtered
  to one fact kind.
- `ifa drive -cassette <path> [-workers N] [-postgres-dsn]` - drives
  `concurrentreplay.Driver` at the requested worker count (default 1) over the
  cassette at `-cassette`, committing through a Postgres `IngestionStore`, and
  prints the resulting `Report` (workers used, generations committed, wall
  time). It applies no schema DDL and runs neither the projector nor the
  reducer itself — draining the `fact_work_items` rows it enqueues requires
  `cmd/projector`/`cmd/reducer` running separately against the same database,
  exactly as `scripts/verify-ifa-replay-drive.sh` orchestrates.

## Dependencies

The command depends on `go/internal/ifa`, `go/internal/facts`,
`go/internal/goldengate`, `go/internal/replaycoverage`, and
`go/internal/cigates` for loading and reconciling its `coverage`/`expectations`
inputs. It intentionally does not depend on collector or parser internals —
that boundary is unchanged.

`ifa drive` (P2) widens the command's dependency graph beyond P0/P1's
database-free footprint by design: `docs/internal/design/4389-ifa-conformance-platform.md`'s
"Placement" section lists `internal/projector` (`FactStore.LoadFacts`),
`internal/reducer` as a library, and `internal/storage/postgres` "for the
replay slice" as `internal/ifa`'s own contract-only dependencies, alongside
`internal/replay` (cassette codec, canonicalizer, reused verbatim). The ADR
draws the line at collector and parser *internals* specifically ("It must not
import collector internals (1846-file blast radius) or parser internals; it
observes their output through `facts.Envelope`"), not at Postgres or the
reducer as a library. `drive.go` therefore imports
`go/internal/replay/cassette`, `go/internal/replay/concurrentreplay`,
`go/internal/runtime`, and `go/internal/storage/postgres` — the same
collector/database dependencies every cassette-mode collector binary already
takes — without violating that boundary. Do not read the pre-P2 "does not
depend on collector or parser internals" line as forbidding Postgres; the ADR
is explicit that Postgres and reducer-as-library are in scope for the replay
slice.

## Telemetry

No runtime telemetry is emitted. This is not a deployed service; the coverage
report and stdout summary are the operator-facing artifacts.

## Related Docs

- `go/internal/ifa/README.md`
- `docs/internal/design/4389-ifa-conformance-platform.md`
