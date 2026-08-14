# CLI Config

## Purpose

`config` owns the logic behind the `eshu config` command family. Three
things live here:

- the persisted settings store -- one `key=value` file, `.env`, inside the
  Eshu config directory, which every other CLI family resolves values from
- the graph-backend selection written by `eshu config db <backend>`
- the `ESHU_*` environment check behind `eshu config validate`

`ResolveValue` is the reason this package matters beyond its own command.
Five other `go/cmd/eshu` files read persisted settings through it -- the API
client (`client.go`), `doctor.go`, `vuln_scan.go`, `first_run_diagnostics.go`,
and the config commands themselves -- so its behavior is the CLI's whole
notion of "what did the operator configure".

## Ownership boundary

This package owns settings *logic*: resolving the config directory, reading
and writing the `.env` file, normalizing a backend name into the three keys
the runtime reads, and turning registry findings into a report. It does not
own process wiring: reading cobra flags, resolving cobra output streams,
calling `os.Environ`, reading the reset confirmation from stdin, or mapping a
result to an exit code. Those stay in `go/cmd/eshu/config_cmd.go` and
`go/cmd/eshu/config_validate.go`, the cobra `RunE` wrappers, because
`go/cmd/eshu` is `package main` and nothing can import it.

`Home` reads `ESHU_HOME`. That is the only environment variable read here,
and the only one read anywhere in this package's dependency closure --
`internal/envregistry`, the sole non-standard-library import, holds a static
registry and reads no environment of its own. Every other `ESHU_*` value this
package deals with comes out of the `.env` file or arrives as a parameter.

This package prints nothing to the process's stdout or stderr. `ValidateEnv`
writes only to the `io.Writer` its caller supplies; the wrapper passes
`cmd.OutOrStdout()`. It never calls `os.Exit`.

## Exported surface

Settings store (`env.go`):

- `Home` -- the config directory: `$ESHU_HOME` when set (leading `~` expanded
  against the user's home directory), otherwise `~/.eshu`
- `EnvFilePath` -- the `.env` path inside `Home`
- `Load` -- the whole settings file as a map; a missing or unreadable file
  yields an empty (non-nil) map, because "operator has never run
  `eshu config set`" is the normal first-run state, not an error
- `ResolveValue` -- one setting, preferring the profile-scoped
  `<KEY>_<PROFILE>` variant when a non-empty profile is given
- `SetValue` -- persist one key, creating the config directory if needed and
  preserving every other key already in the file
- `Reset` -- clear every setting, leaving an empty `.env` behind

Backend selection (`backend.go`):

- `ConfigureDatabaseBackend` -- normalize a backend name and persist the three
  keys the runtime reads, returning the canonical name

Environment validation (`validate.go`):

- `EnvironMap` -- parse `os.Environ()`-style `KEY=VALUE` pairs into a map (the
  caller supplies the slice; this package never calls `os.Environ`)
- `ValidateEnv` -- check an environment snapshot against the `ESHU_*` registry,
  write the report to an `io.Writer`, and return a non-nil error when any
  finding is error-level

Deliberately unexported: the whole-file writer behind `SetValue`/`Reset`, and
`reportFindings`. Exporting the writer would hand every consuming family a way
to clobber the operator's entire settings file when all they need is
`SetValue`; `Reset` names the one whole-file write the CLI actually performs.
The `ESHU_HOME` variable name and the `.eshu`/`.env` literals are unexported
for the same reason -- `Home` and `EnvFilePath` are the supported way to ask.

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/envregistry` -- `Registry`, `Finding`, and `Registry.Validate`;
  the wrapper builds the registry with `envregistry.Default()` and passes it in
- Consumed by `go/cmd/eshu`: `config_cmd.go` and `config_validate.go` (the
  command wrappers), plus `client.go`, `doctor.go`, `vuln_scan.go`, and
  `first_run_diagnostics.go`, which resolve persisted settings

## Telemetry

None. Every function here runs inline with a CLI invocation against one local
file; there is no background pipeline stage to instrument. Failures surface as
returned errors the wrapper prints.

## Gotchas / invariants

- **An empty profile-scoped value falls through to the base key.**
  `ResolveValue("ESHU_SERVICE_URL", "prod")` with `ESHU_SERVICE_URL_PROD=` set
  to empty returns the base `ESHU_SERVICE_URL`, so an operator can leave a
  profile entry blank without blanking the shared default.
- **`Load` never reports a read failure.** A missing, unreadable, or
  permission-denied `.env` all return an empty map. Callers that need to know
  whether the file exists check `EnvFilePath` themselves -- `doctor.go` does
  exactly that.
- **`Load` parses leniently.** Blank lines, `#` comments, and lines without an
  `=` are skipped; keys and values are trimmed; only the first `=` splits, so a
  value may contain `=`.
- **`Reset` does not create the config directory, `SetValue` does.** Resetting
  settings that were never written fails on the write rather than
  materializing a directory as a side effect.
- **`ConfigureDatabaseBackend` writes three keys, not one.**
  `ESHU_GRAPH_BACKEND` selects the driver while `DEFAULT_DATABASE` and
  `ESHU_NEO4J_DATABASE` name the database inside it. Leaving any of them
  behind points the CLI at one backend and the driver at another.
- **Two `os` errors are returned unwrapped on purpose**
  (`//nolint:wrapcheck`): `os.MkdirAll` in `SetValue` and `os.WriteFile` in the
  private writer. Both already return a `*PathError` naming the failing path,
  and the CLI prints that text verbatim to the operator; wrapping would change
  operator-visible output for no added context. `go/.golangci.yml` exempts
  `go/cmd/*` from wrapcheck but not `go/internal/cli/*`, which is why the
  markers appear here and did not exist before the move.
- **The `.env` file is written `0600`** because it holds API keys, and rewritten
  whole on every `SetValue`. Lines are sorted so the file's bytes are stable
  and diffable across runs.

## Related docs

- `go/cmd/eshu/config_cmd.go` and `go/cmd/eshu/config_validate.go` -- the cobra
  wrappers; read these to see which half of the command lives where
- `go/internal/envregistry/README.md` -- the `ESHU_*` registry
  `ValidateEnv` checks against
- `docs/public/reference/env-registry.md` -- the committed copy of the same
  generated reference `eshu config validate --reference` prints
