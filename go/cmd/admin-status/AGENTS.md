# cmd/admin-status Agent Rules

These rules apply only inside `go/cmd/admin-status/`. Root `AGENTS.md` still
controls global proof, performance, concurrency, and skill requirements.

## Read First

- `go/cmd/admin-status/README.md`
- `go/cmd/admin-status/doc.go`
- `go/cmd/admin-status/main.go`
- `go/internal/status/README.md`
- `go/internal/runtime/README.md`

## Local Invariants

- MUST keep this binary one-shot: open Postgres, load one report, render it,
  and exit.
- MUST keep version probes before Postgres opening.
- MUST keep report shape owned by `internal/status`; this command only parses
  flags, opens Postgres, and renders the shared report.
- MUST keep accepted `--format` values to `text` and `json` unless the CLI
  contract and tests change together.
- MUST return `unsupported format` for unknown formats before writing report
  output.
- MUST keep this binary without OTEL providers and `eshu_dp_*` metrics; live
  telemetry belongs to long-running runtimes.
- MUST keep report time based on `time.Now().UTC()` at call time. There is no
  cache layer here.

## Change Gates

- New output formats MUST be added only in `renderStatus`, documented in the
  flag help and CLI reference, and covered by `main_test.go`.
- New flags MUST be parsed in `renderStatus`; report-shaping behavior belongs
  in `internal/status` options, not in `main`.
- Postgres connection behavior MUST continue through `runtimecfg.OpenPostgres`
  so the binary uses the same runtime DSN contract as other Eshu commands.

## Focused Verification

```bash
cd go
go test ./cmd/admin-status -count=1
go doc -cmd ./cmd/admin-status
```
