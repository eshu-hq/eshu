# Collector Entrypoints Gen

## Purpose

`collector-entrypoints-gen` writes or verifies generated collector command
entrypoint files from the checked-in collector entrypoint manifest.

## Ownership boundary

This command is developer tooling. It does not start collectors, talk to
Postgres, claim work, call providers, or modify Helm and Compose runtime
definitions.

## Exported surface

This is a `main` package. The supported CLI flags are `-repo-root`,
`-manifest`, and `-check`; package tests exercise the command through its local
`run` helper.

## Dependencies

The command depends on `internal/collector/entrypoints` for schema validation
and rendering. It uses only local filesystem reads, writes, and byte comparison.

## Telemetry

The command emits no metrics or spans. Its stdout/stderr messages are local
developer feedback only.

Collector Observability Evidence: generated collectors continue to attach the
provider source tracer and instruments through generated service wiring; this
developer command does not alter runtime telemetry contracts.

No-Observability-Change: this command verifies source files and has no runtime
collector process, provider client, status row, metric label, or trace span.

## Gotchas / invariants

`-check` must never rewrite files. A stale-file failure tells reviewers to run
`scripts/generate-collector-entrypoints.sh` and inspect the resulting diff.

Collector Performance Evidence: `go test ./internal/collector/entrypoints/cmd/collector-entrypoints-gen -count=1`
uses temporary files only and proves stale checks without provider, database,
queue, or graph work.

Collector Deployment Evidence: this command does not generate or verify Helm,
Compose, image, or ServiceMonitor files.

## Related docs

- `go/internal/collector/entrypoints/README.md`
- `scripts/generate-collector-entrypoints.sh`
- `scripts/verify-collector-entrypoints-generated.sh`
