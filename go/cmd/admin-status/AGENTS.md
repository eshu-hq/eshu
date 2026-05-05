# AGENTS.md — cmd/admin-status guidance for LLM assistants

## Read first

1. `go/cmd/admin-status/README.md` — binary purpose, flags, invariants, and
   gotchas
2. `go/cmd/admin-status/main.go` — `run` and `renderStatus`; the entire
   business logic lives here
3. `go/internal/status/README.md` — `LoadReport`, `RenderText`, `RenderJSON`,
   and `Reader`; this binary is a thin caller of that package
4. `go/internal/runtime/README.md` — `OpenPostgres`; the only external dep
   this binary uses for configuration

## Invariants this package enforces

- **One-shot lifecycle** — the process exits after printing the report; there
  is no poll loop or long-running goroutine. Any change that adds a background
  goroutine violates this contract.
- **Format gate** — unknown `--format` values return `fmt.Errorf("unsupported
  format %q", ...)` before any output is written. Enforced at
  `main.go:86`.
- **No OTEL registration** — the binary intentionally omits OTEL providers and
  `eshu_dp_*` metrics. Logging goes through the stdlib `log` package. Do not
  add OTEL bootstrap here; use the long-running runtime `/metrics` endpoints
  for live telemetry.
- **Wall-clock report time** — `renderStatus` receives `time.Now().UTC()` as
  the `now` argument. The report reflects real wall-clock state with no
  caching layer. Enforced at `main.go:49`.

## Common changes and how to scope them

- **Add a new output format** → add a case to the `switch` in `renderStatus`
  in `main.go`; update the `--format` flag description; add a test in
  `main_test.go`. Why: the switch is the only format dispatch point; missing
  cases fall through to the `unsupported format` error.

- **Add a new flag** → extend the `flag.FlagSet` in `renderStatus`; thread
  the value into `statuspkg.LoadReport` or `statuspkg.DefaultOptions` if it
  affects report scope. Why: `run` delegates fully to `renderStatus`, so flag
  parsing lives there, not in `main`.

## Failure modes and how to debug

- Symptom: binary exits with a Postgres connection error → cause: ESHU_POSTGRES_DSN
  is missing or wrong → check the env var value and that the Postgres service is
  running; `run` returns the error from `runtimecfg.OpenPostgres` before
  calling `renderStatus`.

- Symptom: binary exits with `unsupported format` → cause: `--format` received
  a value other than `text` or `json` → check how the flag was passed; the
  string comparison is case-insensitive after `strings.ToLower`.

- Symptom: report shows stale-looking data → cause: the data in Postgres is
  stale, not this binary; `statuspkg.LoadReport` reads live rows at call
  time with no cache.

## Anti-patterns specific to this package

- **Adding OTEL providers** — this binary is intentionally a lightweight
  one-shot CLI. OTEL bootstrap adds startup latency and requires a collector
  endpoint. Use the long-running runtimes for live telemetry.

- **Logic beyond flag parsing in `main`** — `main` must only call `run`; `run`
  must only open Postgres and delegate to `renderStatus`. Any report-shaping
  logic belongs in `internal/status`.

## What NOT to change without an ADR

- The `--format` flag name and accepted values — external scripts and operator
  runbooks depend on these; see `docs/docs/reference/cli-reference.md`.
