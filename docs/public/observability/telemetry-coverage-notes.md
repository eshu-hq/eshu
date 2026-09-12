# Telemetry Coverage Notes

Companion to [Telemetry Coverage Contract](telemetry-coverage.md). This page holds the
prose that explains how the coverage gate reads that doc; the stage tables stay there.

## Diff Semantics

- The CI coverage script (X2) reads this doc as a sequence of
  `stage | file:line | metric | category` rows. It parses every line that
  starts with `|` after a section header.
- For each row, the script asserts at least one of:
  1. The metric column lists a registered instrument name from
     `go/internal/telemetry/instruments.go` (X2 grep's the `meter.Int64Counter`,
     `meter.Float64Histogram`, etc., declarations for that exact name).
  2. The metric column contains the literal `No-Observability-Change:` token
     followed by a justification naming the existing signal.
- For each row whose `file:line` references a Go file, the script asserts
  that the file exists and the line range contains a recording call
  (`Instruments.*.Record`, `Instruments.*.Add`, `s.Instruments.*.Record`,
  `s.Instruments.*.Add`, `tracer.Start`, or `slog.*`).
- For each row whose category is `cypher phase`, the script asserts that the
  phase string is registered in `go/internal/storage/cypher/phase_group_metadata.go`.
- A new pipeline stage added to the source tree without a corresponding row
  in this doc fails the X2 + X3 gate. The error message names the missing
  row's expected location.
- Maintainers update this doc in the same PR that adds the new stage. The
  doc is hand-authored; X2 does not auto-generate rows from source code.

## Markers

- `No-Observability-Change:` is a literal string the script grep-parses for.
  It appears in the metric column when the stage does not need a new metric
  because an existing one already diagnoses it.
- The marker is per-stage: one marker per row that does not have a registered
  metric name. The script treats the absence of a registered metric AND the
  absence of a marker as a gate failure.
- The marker is NOT a "I forgot". It is a documented decision that names the
  existing signal and why it covers the stage. Example:
  `No-Observability-Change: git-source collector metrics (FactsEmitted, RepoSnapshotDuration) cover preflight throughput`.
- The marker language matches the `No-Observability-Change:` evidence marker
  in the per-PR commit message policy at
  `docs/internal/agent-guide.md` (evidence markers section). PR authors cite the same row in their
  commit message.

## Regeneration Note

- Each row is one stage. The table is the source of truth for X2.
- To add a new stage:
  1. Register the metric in `go/internal/telemetry/instruments.go` (or
     confirm an existing instrument already diagnoses the path).
  2. Add a row to the appropriate section of this doc, including the
     `file:line` citation to the Go file that records the metric.
  3. Run `wc -l docs/public/observability/telemetry-coverage.md` to confirm
     the doc stays under the 500-line file rule.
  4. Run the docs build:
     `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
     mkdocs build --strict --clean --config-file docs/mkdocs.yml`.
  5. Run `git diff --check` to confirm no whitespace errors.
- To remove a stage, delete the row and remove the matching metric from
  `go/internal/telemetry/instruments.go` in the same PR. X2 will fail
  otherwise because the metric is still registered.

## Linked Work

- [#3633](https://github.com/eshu-hq/eshu/issues/3633) (closed 2026-06-23) —
  root-cause class: defined-but-never-registered instruments. The historical
  precedent that motivated this contract.
- [#3680](https://github.com/eshu-hq/eshu/issues/3680) (open, 2026-06-24,
  closed at the time of #3694 sign-off) — per-collector envelope telemetry.
  The in-flight adoption that adds the per-collector rows above.
- [`go/internal/telemetry/instruments.go`](https://github.com/eshu-hq/eshu/blob/main/go/internal/telemetry/instruments.go)
  — metric source of truth. Every metric name in the tables above resolves to
  a `meter.*(...)` declaration here.
- [`go/internal/telemetry/contract.go`](https://github.com/eshu-hq/eshu/blob/main/go/internal/telemetry/contract.go)
  — dimension keys, span names, log keys. Every span name in the OTEL Span
  Names section resolves to a `Span*` constant here or in the sibling
  `contract_*.go` files.
- [`docs/public/reference/telemetry/index.md`](https://github.com/eshu-hq/eshu/blob/main/docs/public/reference/telemetry/index.md)
  — public operator contract. This coverage doc extends the public contract
  by enumerating every stage, not just the most-used signals.
- [`docs/internal/agent-guide.md` (evidence markers section)](https://github.com/eshu-hq/eshu/blob/main/docs/internal/agent-guide.md)
  — five evidence markers policy. The marker language in the doc and PR
  commits comes from this section.

## Flow Affected

`reducer -> graph write -> telemetry contract`. This is the flow the contract
covers. New reducer projection stages, new shared-edge writers, and new
graph-write statement phases all flow through this contract; the X2 script
is the machine-enforced gate.
