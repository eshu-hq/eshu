# AGENTS.md — go/internal/cli/config guidance for LLM assistants

## Read first

1. `go/internal/cli/config/README.md` — purpose, ownership boundary,
   exported surface, and the reasoning behind what is deliberately not
   exported
2. `go/internal/cli/config/doc.go` — the godoc contract
3. `go/cmd/eshu/config_cmd.go` — the cobra `RunE` wrappers that resolve
   process state (flags, real stdin, `os.Environ`, `cmd.OutOrStdout`) and
   call into this package. It is the file that shows how the two halves fit
   together.
4. `go/internal/envregistry/AGENTS.md` — the `ESHU_*` registry
   `ValidateEnv` checks an environment snapshot against. Variable
   definitions belong there, not here.

## Invariants this package enforces

- **No process wiring in this package.** No cobra flags, no `os.Environ`,
  no read of the process's real stdin, no `os.Exit`, and no `fmt.Print*`
  to the real stdout. `go/cmd/eshu` is `package main`, so nothing can
  import it — any symbol that reads a flag, reads real stdin, or maps to
  an exit code has to live in the two wrapper files instead. `ValidateEnv`
  prints only through the `io.Writer` its caller supplies.
- **`Home` is the only function that reads the process environment, and it
  reads exactly two things:** `ESHU_HOME`, and the platform home variable via
  `os.UserHomeDir` — `HOME` on Unix, `USERPROFILE` on Windows, `home` on Plan 9.
  The second is easy to miss because no variable is named at the call site;
  `os.UserHomeDir` is `os.Getenv` underneath (`$GOROOT/src/os/file.go`), and it
  runs on both paths: expanding a leading `~` in `ESHU_HOME`, and the `~/.eshu`
  fallback. **What counts as an env read is decided by the callee's source, not
  by the name at the call site** — an earlier version of these docs claimed no
  other variable was read anywhere in the dependency closure, which
  `os.UserHomeDir` had always falsified.

  No `ESHU_*` setting or credential is resolved from the environment. Before
  adding an import, check its closure with
  `go list -deps ./internal/cli/config` — a dependency that reaches `os.Getenv`
  by any spelling widens this surface and falsifies `doc.go` and `README.md`.
- **File access is confined to the `.env` file under `Home`.** Reading a
  path a caller passes in as a parameter would be acceptable (the
  `internal/cli/servicereport` precedent), but nothing here does that
  today; do not open a second file without saying so in all three docs.
- **`ResolveValue` is a contract five other families inherit.** `client.go`,
  `doctor.go`, `vuln_scan.go`, `first_run_diagnostics.go`, and the config
  commands all resolve settings through it. A change to its precedence
  rules changes what every one of those commands thinks the operator
  configured. Grep the call sites before touching it.

## Common changes and how to scope them

- **Add a persisted setting** → nothing to change here. `SetValue` and
  `ResolveValue` are key-agnostic. Register the variable in
  `internal/envregistry` so `eshu config validate` knows about it.
- **Change how a value is looked up** (precedence, profile suffix,
  trimming) → `ResolveValue` in env.go, plus a case in env_test.go. This
  is the highest-blast-radius function in the package; see the invariant
  above.
- **Add a graph backend** → a case in `ConfigureDatabaseBackend`
  (backend.go). It must write all three keys — `ESHU_GRAPH_BACKEND`,
  `DEFAULT_DATABASE`, `ESHU_NEO4J_DATABASE` — and the `default` branch's
  error text names the accepted backends, so update it too.
- **Change the validate report's layout** → `reportFindings` in
  validate.go. It is unexported and stays that way; `ValidateEnv` is the
  entry point. What counts as a finding is `internal/envregistry`'s
  decision, not this package's.
- **Add a `config` subcommand** → the cobra registration and its `RunE`
  go in `go/cmd/eshu/config_cmd.go`; only the logic it calls belongs here.
  Add a wrapper test to `go/cmd/eshu/config_cmd_test.go`, which exists
  because a wrapper that resolved flags and then called nothing would
  still pass this package's tests.

## Failure modes and how to debug

- Symptom: a command reads a setting the operator swears they set → check
  `EnvFilePath()` first. `Load` returns an empty map for a missing,
  unreadable, or permission-denied file with no error, so a wrong
  `ESHU_HOME` and an empty config are indistinguishable from the caller's
  side. `eshu doctor` prints both paths for exactly this reason.
- Symptom: a profile-scoped value appears to be ignored → an empty
  `<KEY>_<PROFILE>` value deliberately falls through to the base key.
  That is the documented behavior, not a bug.
- Symptom: `eshu config set` succeeds but a later read misses the key →
  `SetValue` rewrites the whole file from `Load`'s map, so a key that
  `Load` skipped (no `=`, or commented out) is dropped on the next write.
  Check the file's actual bytes, not just the command's exit code.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`).
  If new logic needs something only the wrapper has — a cobra flag, real
  process stdin — add a parameter instead.
- **Exporting the whole-file writer.** `SetValue` and `Reset` are the two
  supported writes. An exported `Write(map[string]string)` would let any
  consuming family clobber the operator's entire settings file, and every
  family that imports this package would inherit that capability.
- **Wrapping the two `os` errors marked `//nolint:wrapcheck`.**
  `os.MkdirAll` in `SetValue` and `os.WriteFile` in the private writer
  return a `*PathError` that already names the failing path, and the CLI
  prints it verbatim. `go/.golangci.yml` exempts `go/cmd/*` from wrapcheck
  but not `go/internal/cli/*`, so the linter will ask; the answer is the
  existing marker, not a `%w` wrap that changes what the operator reads.
- **Sorting `config show`'s rows here.** The wrapper builds those rows by
  iterating `Load`'s map, so the order is whatever Go's map iteration
  gives. Making it deterministic is a defensible change, but it is a
  behavior change to a user-visible command — do it deliberately in the
  wrapper with a test, not as a side effect of touching this package.

## What NOT to change without an ADR

- Moving variable definitions or validation rules out of
  `internal/envregistry` and into this package. The registry is the
  single code-owned source the generated reference doc and
  `eshu config validate` both read; splitting it would create a second
  place to look.
- Changing the `.env` format (the `key=value` file, its `0600` mode, or
  the sorted whole-file rewrite). Operators edit this file by hand and
  `internal/runtime/api_key.go` parses the same format independently.
