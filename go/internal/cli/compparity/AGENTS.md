# AGENTS.md — internal/cli/compparity guidance for LLM assistants

## Read first

1. `doc.go` — the package contract, including why command paths and the
   first-run exercise are injected instead of computed here.
2. `README.md` — ownership boundary, dependencies, and invariants.
3. `go/cmd/eshu/competitive_parity_cmd.go` — the cobra wrapper that resolves
   flags and streams, walks the cobra tree for command paths, supplies the
   first-run exercise, and maps a failing report to the exit code.
4. `go/internal/competitiveparity/AGENTS.md` — the validator this package
   feeds. Expectation and scoring changes belong there, not here.

## Invariants this package enforces

- **Share-safe failure details** — `exerciseFailureDetail` maps each exercise
  ID to a static string. The underlying errors can carry local filesystem
  paths and the artifact is shareable output, so the real error text never
  reaches an `ExerciseResult`. The cmd/eshu wrapper test
  `TestCompetitiveParityValidateReportsMissingDocs` asserts the temp repo
  root does not leak.
- **No silent pass for an unwired exercise** — `ExerciseResults` records a
  nil first-run exercise as a failed `first_run_report_artifact`, pinned by
  `TestExerciseResultsNilFirstRunExerciseFails`.
- **Missing docs are the validator's finding** — `Inventory` skips a doc
  path that does not exist so the doc check fails in the report, but any
  other read error (for example a directory at the path) surfaces as an
  error, pinned by `TestInventoryReportsUnreadableDoc`.
- **The exercise fixture must stay supported** — `SupportedSupplyChainPacket`
  has to produce a supported, complete packet or the investigation exercise
  proves nothing; `TestSupportedSupplyChainPacketIsSupportedComplete` pins it.

## Common changes and how to scope them

- **Add an exercise** → add its entry to the `checks` slice in
  `ExerciseResults`, a static detail in `exerciseFailureDetail`, and the
  exercise func in `exercises.go`. If it needs anything from `package main`,
  inject it as a parameter the way the first-run exercise is injected.
- **Change what the gate reads from the repo** → the doc set comes from
  `internal/competitiveparity.DefaultExpectations` via `DocPaths`; change the
  expectations there rather than hard-coding paths here.
- **Move the first-run exercise in here** → only after the first-run
  evidence helpers leave `go/cmd/eshu`; then drop the injection parameter in
  the same change.

## Anti-patterns specific to this package

- Importing cobra or reading flags, environment, or process streams — that
  is the wrapper's job, and `go list -deps` staying free of `spf13` is a
  documented property of this package.
- Putting real error text, absolute paths, or repo-root values into exercise
  details or the rendered artifact.
- Writing files. The package is read-only; the wrapper owns the `--out`
  write.

## What NOT to change without an ADR

- Exercise IDs, their order, or the static failure detail strings — they are
  part of the `competitive_parity_gate.v1` artifact consumers already parse.
