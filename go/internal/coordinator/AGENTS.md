# AGENTS.md — internal/coordinator guidance for LLM assistants

## Read first

1. `go/internal/coordinator/README.md` — ownership boundary, loop structure,
   dark/active branching, exported surface, and metric inventory
2. `go/internal/coordinator/service.go` — `Service.Run`, `runReconcile`,
   `runReapExpiredClaims`, `runWorkflowReconciliation`, and `tickerChan`
3. `go/internal/coordinator/config.go` — `LoadConfig`, `Config.Validate`, env
   var names, and the `withDefaults` application order
4. `go/internal/coordinator/metrics.go` — `otelMetrics`, `NewMetrics`, and the
   type-assertion pattern for `RecordReap`/`RecordRunReconciliation`
5. `go/internal/workflow/service.go` (does not exist — `Store` is defined in
   `service.go` here; the workflow contracts are in `internal/workflow`)
6. `go/internal/telemetry/instruments.go` and `contract.go` — before adding
   metric or span names

## Invariants this package enforces

- **Dark by default** — `deploymentModeDark` is the fallback in `withDefaults`.
  Do not change the default mode without a documented deployment decision.
- **Active mode gate** — `Config.Validate` at line 104 returns an error if
  `DeploymentMode == active` without `ClaimsEnabled=true` and at least one
  enabled claim-capable collector instance. This gate must not be weakened.
- **HeartbeatInterval < ClaimLeaseTTL** — enforced in `Config.Validate` at
  line 123. Violated configurations exit at startup.
- **Nil reap ticker in dark mode** — `reapTicker` is nil in dark mode;
  `tickerChan(nil)` returns a nil channel. The `select` never fires on it.
  Do not replace `tickerChan` with a non-nil channel in dark mode.
- **Type-assertion guard for Reap and RunReconciliation metrics** —
  `recordReap` and `recordRunReconciliation` in `service.go` use interface
  type assertions because `Metrics` only declares `RecordReconcile`. Always
  use `NewMetrics` in production wiring to get all three.
- **Store is required** — `Service.Run` returns an error immediately if
  `s.Store == nil`. Do not add fallback behavior here.

## Common changes and how to scope them

- **Add a new reconcile operation** → add a method to `Store` in `service.go`;
  add a private run helper that calls it and records telemetry; call the helper
  from the appropriate ticker branch in `Service.Run`; add a new observation
  type and a recording method to `Metrics` in `metrics.go`; register counter,
  histogram, and gauge instruments in `NewMetrics`; run
  `go test ./internal/coordinator -count=1`.

- **Change the reconcile interval default** → edit `defaultReconcileInterval`
  in `config.go`; document the change in `README.md` and the configuration
  table; verify that `Config.Validate` still passes with the new default.

- **Add a new config field from env** → add the `envXxx` call in `LoadConfig`;
  add the field to `Config`; apply a default in `withDefaults`; add validation
  in `Validate` if the field has constraints; update the README table.

- **Switch from dark to active mode in tests** → set `Config.DeploymentMode =
  deploymentModeActive`, `Config.ClaimsEnabled = true`, and provide at least
  one `workflow.DesiredCollectorInstance` with `Enabled: true,
  ClaimsEnabled: true` in `Config.CollectorInstances`. Then call
  `cfg.withDefaults()` and `cfg.Validate()` before passing to `Service`.

- **Inject a fake clock** → set `Service.Clock` to a `func() time.Time` that
  returns a fixed time. The `now()` helper uses the injected clock when
  non-nil.

## Failure modes and how to debug

- Symptom: `reconcile_total{outcome="reconcile_error"}` rising →
  `Store.ReconcileCollectorInstances` returning errors; check Postgres
  connectivity and `eshu_dp_postgres_query_duration_seconds`.

- Symptom: `reconcile_total{outcome="state_read_error"}` rising →
  `Store.ListCollectorInstances` failing after a successful reconcile write;
  Postgres health is the first thing to verify.

- Symptom: `collector_instance_drift` gauge non-zero →
  desired and durable sets disagree; check the structured log warning
  `workflow coordinator collector instance drift detected`; verify that
  `ESHU_COLLECTOR_INSTANCES_JSON` is well-formed and that Postgres accepted the
  last `ReconcileCollectorInstances` call.

- Symptom: `reap_total` and `run_reconcile_total` never increment →
  confirm `DeploymentMode=active`; in dark mode these counters are never
  written.

- Symptom: `last_reaped_claims` gauge stuck at `ExpiredClaimLimit` every
  pass → collectors are not completing claims within the lease TTL; investigate
  claim heartbeat rate and `ClaimLeaseTTL` vs `HeartbeatInterval` relationship.

## Anti-patterns specific to this package

- **Adding trigger normalization or claim scheduling here** — these are not
  implemented today. Do not add logic that claims work items on behalf of
  collectors; that ownership boundary belongs to the ingester collector path.

- **Calling Store methods outside runXxx helpers** — all Store calls must go
  through the private `runXxx` methods so telemetry recording is consistent.
  Do not inline `Store.Xxx` calls directly in the `select` loop.

- **Branching on `Store` concrete type** — `Service` accepts any `Store`
  implementation. Do not add `if _, ok := s.Store.(*postgres.WorkflowControlStore)` 
  checks; backend dialect belongs in the storage layer.

- **Disabling Config.Validate** — do not skip validation in production wiring.
  The gate protects against active mode with no claim-capable collectors, which
  would silently fail to do useful work.

- **Widening Metrics interface for partial implementations** — the type-
  assertion pattern in `recordReap` and `recordRunReconciliation` exists so
  tests can pass a minimal `Metrics` stub that only implements
  `RecordReconcile`. Do not change `Metrics` to require all three methods
  without ensuring all test stubs are updated.

## What NOT to change without a design discussion

- `Store` interface method signatures — these form the Postgres contract used by
  `storage/postgres.WorkflowControlStore`; removing or reordering methods
  requires a coordinated storage layer update.
- `deploymentModeDark` / `deploymentModeActive` string values — any change
  breaks existing deployments that set the env var explicitly.
- The nil-ticker guard (`tickerChan`) — it is the only mechanism preventing
  reap calls in dark mode; removing it without a replacement breaks the
  dark/active safety invariant.
