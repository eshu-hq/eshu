# AGENTS.md - cmd/payload-usage-manifest guidance

## Read first

1. `README.md` - command purpose, modes, flags, and CI wiring.
2. `main.go` - CLI flag parsing and mode dispatch (thin; all logic delegates
   to `go/internal/payloadusage`).
3. `../../internal/payloadusage/AGENTS.md` - the derivation/comparison logic
   this command wraps. Read that file before changing any behavior beyond
   flag parsing or output formatting.
4. `docs/internal/design/contract-system-v1.md` §6 (enforcement gates) - the
   design contract this command implements (gate 2).

## Invariants

- This package is a THIN CLI wrapper. Do not add derivation, parsing, or
  comparison logic here — it belongs in `go/internal/payloadusage` so
  `go/internal/reducer`'s drift-lock test (`TestPayloadUsageManifest`) can
  call the same logic without importing a `package main`.
- The command is build-time/CI-time only. Do not add runtime storage, graph,
  network, or telemetry dependencies.
- Keep this package under the repo's 500-line file cap.

## Common changes

- **New CLI flag**: add it to `options` and `parseOptions` in `main.go`, then
  thread it into the `payloadusage.Paths` construction in `run`. Do not add a
  flag whose default cannot be resolved relative to `-repo-root` without
  first checking whether `payloadusage.ResolvePaths` already covers it.
- **New output mode**: add a `case` in `run`'s mode switch and a small
  formatting helper (mirroring `writeManifest`/`reportGate`), delegating the
  actual work to `payloadusage.Load`/`payloadusage.Gate`.
