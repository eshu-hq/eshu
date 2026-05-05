# AGENTS.md — internal/app guidance for LLM assistants

## Read first

1. `go/internal/app/README.md` — purpose, lifecycle flow, exported surface,
   and gotchas
2. `go/internal/app/app.go` — `Application`, `Lifecycle`, `Runner`, `NewHosted`;
   the core wiring contract
3. `go/internal/app/lifecycle.go` — `ComposeLifecycles` and lifecycleChain;
   understand rollback on partial start before touching lifecycle composition
4. `go/internal/app/status_server.go` — `MountStatusServer` and
   `NewHostedWithStatusServer`; how runtime.NewStatusAdminServer attaches
5. `go/internal/runtime/README.md` — the runtime helpers this package composes
6. `go/cmd/reducer/main.go` — canonical caller of `NewHostedWithStatusServer`;
   shows runtimecfg.WithPrometheusHandler option in context
7. `go/cmd/ingester/main.go` — shows `NewHosted` + `MountStatusServer` split;
   also shows runtimecfg.WithRecoveryHandler usage

## Invariants this package enforces

- **`Application.Run` requires both `Lifecycle` and `Runner`** — `app.go:56–62`;
  either being nil returns an error before any Start is called. Wire both
  before calling Run.

- **`ComposeLifecycles` rolls back on partial Start** — `lifecycle.go:32–40`;
  if Start fails on the Nth lifecycle in the chain, Stop is called in
  reverse order on all previously started lifecycles. Do not bypass this by
  calling Start manually on individual lifecycles.

- **Stop runs under defer** — `app.go:67`; `Lifecycle.Stop` is called via
  `defer func()` whether `Runner.Run` succeeds or returns an error. Lifecycle
  implementations must tolerate Stop being called after a failed Start.

- **Separate metrics listener only when metrics address differs** —
  `status_server.go:19`; `MountStatusServer` checks
  `strings.TrimSpace(app.Config.MetricsAddr)` against `ListenAddr`. If they
  are equal or the metrics address is empty, no second server is created.
  Do not assume a metrics server exists when calling `MountStatusServer`.

## Common changes and how to scope them

- **Add a new hosted binary** → call `NewHostedWithStatusServer(
  serviceName, runner, statusReader, opts...)` in the binary's main; no
  changes to this package are needed. See `go/cmd/reducer/main.go` as the
  pattern.

- **Attach a custom lifecycle hook alongside the admin server** → after
  calling `NewHosted`, pass the resulting `Application.Lifecycle` and your new
  lifecycle to `ComposeLifecycles`, then assign back to `Application.Lifecycle`
  before calling `Application.Run`. Do not embed lifecycle logic inside the
  `Runner` if it has a distinct Stop contract.

- **Add a new field to `Application`** → add the field to the struct in
  `app.go`, set it in both `NewHosted` and any constructor that builds
  `Application` directly; update the relevant cmd/ wiring and tests;
  run `go test ./internal/app -count=1`. Consider whether the field belongs
  in `Application` (consumed by the binary's wiring) or in `runtime.Config`
  (consumed by runtime helpers).

- **Change the `Lifecycle` interface** → runtime.NewLifecycle and
  runtime.HTTPServer both satisfy this interface; any change to method
  signatures must be coordinated across all three, plus every cmd/ that passes
  a custom `Lifecycle`. Run the full cmd gate after:
  `go test ./cmd/... -count=1`.

- **Change how the metrics server is mounted** → touch `status_server.go`;
  verify the nil-check on the runtime.NewStatusMetricsServer return; update
  `status_server_test.go`; do not rely on the metrics server pointer being
  non-nil unless the metrics address is set and differs from the listen address.

## Failure modes and how to debug

- Symptom: binary exits with "lifecycle is required" or "runner is required" →
  `Application.Run` found a nil field; check that `NewHosted` or the
  constructor returned without error before calling Run.

- Symptom: one of two admin HTTP servers fails to start, then the binary
  exits → `ComposeLifecycles.Start` returned an error on the second server;
  the first server was stopped by rollback. Check the bound address for
  conflicts (ESHU_LISTEN_ADDR / ESHU_METRICS_ADDR) and the structured log
  for "listen on ...: bind: address already in use".

- Symptom: `/metrics` endpoint returns only status gauges, OTEL Prometheus
  data is missing → runtimecfg.WithPrometheusHandler(providers.PrometheusHandler)
  was not passed as a StatusAdminOption; check the opts... in the
  `NewHostedWithStatusServer` call in the binary's main.

- Symptom: binary hangs on shutdown and does not respond to SIGTERM → the
  `Runner.Run` implementation does not respect context cancellation; check
  that the runner exits promptly when ctx.Done() closes.

## Anti-patterns specific to this package

- **Do not call individual lifecycle Start / Stop methods directly** —
  always go through `ComposeLifecycles` or `Application.Run`. Direct calls
  skip rollback on partial failure.

- **Do not put retry, queue, or graph logic here** — this package is a thin
  shell. Business logic belongs in the domain packages (internal/reducer,
  internal/projector, internal/collector, etc.) and is passed as a `Runner`
  implementation.

- **Do not add telemetry emission** — this package intentionally emits no
  spans, metrics, or structured logs. All observability is owned by the
  runtime and status packages this shell composes.

- **Do not inline `MountStatusServer` logic into a new constructor** — reuse
  `MountStatusServer` in any new convenience constructor. Forking the
  attachment logic produces inconsistent metrics port handling.

## What NOT to change without an ADR

- `Lifecycle` interface method signatures — runtime.NewLifecycle,
  runtime.HTTPServer, and every cmd-level lifecycle adapter satisfy this
  interface; changing the signature requires a coordinated update across all
  callers.
- `Application` field names — cmd-level wiring code references
  `Application.Config`, `Application.Lifecycle`, `Application.Runner`, and
  `Application.Observability` directly; renaming breaks all callers.
- `ComposeLifecycles` rollback semantics — the rollback-on-Start-failure
  guarantee is relied on by multi-server binaries to avoid resource leaks;
  changing the behavior requires evidence that no binary depends on the
  current ordering.
