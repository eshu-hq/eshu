# admin-status

## Purpose

`admin-status` is the local CLI that renders the shared Eshu status report from
Postgres. It loads the same report shape used by the long-running runtime
admin surface and prints it as text or JSON for operator inspection.

## Ownership boundary

This binary owns CLI rendering of the status report. It does not own the
report definition (that lives in `internal/status/`), the Postgres reader
implementation (`internal/storage/postgres/`), or any HTTP transport. The
identical report served at `/admin/status` by long-running runtimes is built
through the same `status.LoadReport` path.

## Entry points

- `main` and `run` in `go/cmd/admin-status/main.go`
- single-process binary; no subcommands
- `eshu-admin-status --version` and `eshu-admin-status -v` print the build-time
  version through `printAdminStatusVersionFlag`, which wraps
  `buildinfo.PrintVersionFlag`, before opening Postgres

## Configuration

Flags (`go/cmd/admin-status/main.go`):

- `--format` selects the output format. Accepts `text` (default) or `json`.

Postgres connection is resolved through `runtime.OpenPostgres`, which reads
the standard ESHU_POSTGRES_DSN environment variable used by the rest of the
runtime.

## Telemetry

The binary does not register OTEL providers or `eshu_dp_*` metrics. Logging
goes through the standard library `log` package and exits non-zero on any
error. Use the long-running runtime `/metrics` endpoints for live telemetry.

## Gotchas / invariants

- one-shot lifecycle: the process exits immediately after printing the report
- version probes are pre-startup checks; keep `printAdminStatusVersionFlag` at
  the top of `main` so status scripts can inspect the binary without database
  credentials
- exits non-zero if the Postgres connection cannot be opened or the report
  cannot be loaded
- unknown `--format` values fail with `unsupported format`
- the report is rendered against the wall clock at call time
  (`time.Now().UTC()`); stale data appears stale, no caching layer is involved

## Related docs

- [Service runtimes](../../../docs/docs/deployment/service-runtimes.md)
- [CLI reference](../../../docs/docs/reference/cli-reference.md)
- [Docker Compose deployment](../../../docs/docs/deployment/docker-compose.md)
