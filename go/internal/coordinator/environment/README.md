# Coordinator environment parsing

## Purpose

`environment` parses `int`, `bool`, and `time.Duration` values out of
environment variable strings, with a fallback for missing or blank values and
an error naming the key for a value that fails to parse.

## Ownership boundary

This package owns only the three parse functions. It does not own which
environment variable names exist, what their fallbacks are, or how a parse
error is handled; those decisions stay with each caller. The coordinator
root's `LoadConfig` (`config.go`) and `coordinator/semantic`'s worker
configuration (`provider_config.go`) both call this package to parse their
own environment variables.

## Exported surface

- `Int(getenv func(string) string, key string, fallback int) (int, error)`
- `Bool(getenv func(string) string, key string, fallback bool) (bool, error)`
- `Duration(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error)`

See `doc.go` for the godoc contract.

## Dependencies

- Standard library only (`fmt`, `strconv`, `strings`, `time`).

The package does not import the parent coordinator package or any other
coordinator subpackage.

## Telemetry

None. Parsing runs once during each caller's configuration load, before any
runtime telemetry exists.

No-Observability-Change: this move adds or renames no metric, span, log
field, status field, queue, worker, lease, or runtime setting.

## Gotchas / invariants

- The package is named `environment`, not `env`. The repository's
  `.gitignore` line 49 carries `env/` as a Python-virtualenv exclusion
  pattern, which silently excludes any directory named `env/` from git. A
  package placed at `env/` builds locally and is never committed; naming it
  `environment` avoids that trap entirely.
- `Bool` accepts only values `strconv.ParseBool` accepts (`1`, `t`, `T`,
  `TRUE`, `true`, `True`, `0`, `f`, `F`, `FALSE`, `false`, `False`), not an
  arbitrary truthy string.
- `Duration` requires a unit suffix accepted by `time.ParseDuration`; a bare
  number is a parse error, not seconds.
- A missing or all-whitespace value returns `fallback` with a nil error in
  all three functions; only a non-blank, unparseable value is an error.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/coordinator/config.go`
- `go/internal/coordinator/semantic/provider_config.go`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
