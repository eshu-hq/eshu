# AGENTS.md — internal/buildinfo guidance for LLM assistants

## Read first

1. `go/internal/buildinfo/README.md` — purpose, exported surface, invariants
2. `go/internal/buildinfo/buildinfo.go` — `Version` var and `AppVersion()`; the
   normalized accessor
3. `go/internal/buildinfo/cli.go` — shared `--version` / `-v` helper for
   service binaries
4. Dockerfile — how the release version is injected via `-ldflags`

## Invariants this package enforces

- **`Version` is write-once via `-ldflags`** — no code path should assign to
  `Version` at runtime. `buildinfo.go:6` declares it as a package-level `var`
  so the linker can override it; treating it as a mutable var at runtime
  creates divergence between the binary and its reported version.
- **`AppVersion()` falls back to `"dev"`** — `buildinfo.go:12` returns `"dev"`
  for local source builds and whitespace-only linker overrides. It first honors
  a non-`"dev"` linker value, then a non-`"(devel)"` Go main-module version from
  `debug.ReadBuildInfo`. All callers must use `AppVersion()`, not `Version`
  directly, so `go install ...@version` and release builds behave uniformly.
- **Version probes are pre-startup only** — service binaries call
  `PrintVersionFlag` before telemetry, Postgres, or graph setup. Keep that
  call at the top of `main` so `eshu-api --version` and sibling probes are safe
  in containers and install checks.

## Common changes and how to scope them

- **Change the default fallback string** — change the literal `"dev"` at
  `buildinfo.go:28`. Then update every status test and MCP server test that
  asserts on the default version string.

- **Add a second version attribute (e.g., git commit SHA)** — add a second
  `var` and a second accessor following the same ldflags pattern. Do not embed
  the SHA into `Version`; keep them separate so operators can query each
  independently.
- **Add a new service binary** — call `PrintVersionFlag(os.Args[1:], os.Stdout, "<binary-name>")`
  before any runtime setup, add a smoke test or build-run proof for
  `--version` and `-v`, and update that package's README/doc.go.

## Failure modes and how to debug

- Symptom: `AppVersion()` always returns `"dev"` in production images →
  cause: `-ldflags` path mismatch — the import path passed to `-X` must
  exactly match `github.com/eshu-hq/eshu/go/internal/buildinfo.Version`
  → fix: verify the `go build -ldflags` invocation in the Dockerfile. For
  `go install ...@version`, also confirm the install target used an actual
  module version and not a local source path.

- Symptom: version string contains leading or trailing whitespace in the
  status response → cause: caller reading `Version` directly instead of
  `AppVersion()` → fix: replace `buildinfo.Version` with `buildinfo.AppVersion()`.
- Symptom: `<service> --version` tries to connect to Postgres → cause: the
  service entrypoint moved `PrintVersionFlag` below runtime setup → fix: put
  the guard back at the top of `main`.

## Anti-patterns specific to this package

- **Reading `Version` directly** — always use `AppVersion()` to get the
  normalized string.
- **Adding a version constant anywhere else** — the whole point of this
  package is that there is exactly one place. Adding a duplicate constant
  creates version drift.
