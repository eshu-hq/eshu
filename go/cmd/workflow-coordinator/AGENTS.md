# AGENTS.md — cmd/workflow-coordinator guidance for LLM assistants

## Read first

1. `go/cmd/workflow-coordinator/README.md` — purpose, wiring, configuration,
   and operational notes
2. `go/cmd/workflow-coordinator/main.go` — the complete wiring: telemetry,
   Postgres, config, metrics, store, service, admin surface, and shutdown
3. `go/internal/coordinator/service.go` — `Service.Run`, the reconcile/reap
   loop; understand the dark/active branching before touching anything
4. `go/internal/coordinator/config.go` — `LoadConfig` and `Config.Validate`;
   all env var names and validation invariants live here
5. `go/internal/app` — `NewHostedWithStatusServer`; understand admin surface
   mounting before adding endpoints

## Invariants this binary enforces

- **Dark by default** — deployment mode defaults to `dark`. Active mode is
  explicitly opt-in. Do not change this default without a deployment decision.
- **Active mode gate** — active mode requires claims enabled and at least one
  enabled claim-capable collector instance in ESHU_COLLECTOR_INSTANCES_JSON.
  `Config.Validate` enforces this at startup and on every `Service.Run` entry.
- **Heartbeat interval must be less than lease TTL** — violated at startup
  means the binary exits with a validation error.
- **No canonical graph writes** — this binary never calls the graph backend.
  It owns only Postgres via `NewWorkflowControlStore`.
- **Signal-driven shutdown** — SIGINT and SIGTERM both cancel the context.
  Do not add `os.Exit` calls outside `main`.

## Common changes and how to scope them

- **Add a new env var** → add parsing in `go/internal/coordinator/config.go`
  `LoadConfig`; add the field to `Config`; add validation in `Config.Validate`
  if needed; update the `README.md` configuration section in this package and
  in `internal/coordinator/README.md`. Run
  `go test ./internal/coordinator -count=1`.

- **Switch to active mode for local testing** → set
  ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE=active,
  ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED=true, and supply a valid
  ESHU_COLLECTOR_INSTANCES_JSON array with at least one entry that has
  `"claims_enabled": true` and `"enabled": true`. Do not change binary
  wiring; the mode gate is entirely in `Config.Validate`.

- **Add a new admin endpoint** → the admin surface is owned by
  `NewHostedWithStatusServer`. Read `internal/app` and `internal/runtime`
  before adding routes; the binary itself does not register HTTP handlers
  directly.

- **Change metrics instrumentation** → metrics are created in
  `internal/coordinator/metrics.go`. Do not add metric creation here in
  `main.go`; add them there and register them in `NewMetrics`. Follow the
  `eshu_dp_workflow_coordinator_` prefix and the observability contract in
  `internal/telemetry/contract.go`.

## Failure modes and how to debug

- Symptom: binary exits immediately after startup → likely a `Config.Validate`
  failure; check the `workflow coordinator failed` log for the specific error.
  Common causes: deployment mode `active` without claims enabled, or invalid
  ESHU_COLLECTOR_INSTANCES_JSON.

- Symptom: `/readyz` returns unhealthy in dark mode → the reconcile loop or
  Postgres connection is failing; check
  `eshu_dp_workflow_coordinator_reconcile_total{outcome="reconcile_error"}`.

- Symptom: `eshu_dp_workflow_coordinator_collector_instance_drift` gauge
  non-zero → desired and durable collector-instance sets disagree; check
  `desired_collector_instances` and `durable_collector_instances` gauges and
  the structured log warning `workflow coordinator collector instance drift
  detected`.

- Symptom: no reap or run-reconciliation activity in metrics → confirm
  deployment mode is `active`; in dark mode, `reap_total` and
  `run_reconcile_total` never increment.

## Anti-patterns specific to this binary

- **Adding business logic to main.go** — `main.go` is wiring only. Business
  logic belongs in `internal/coordinator` or `internal/workflow`.

- **Claiming that trigger normalization or claim ownership happen here** —
  they do not. The binary reconciles collector instances, reaps expired claims,
  and recomputes workflow-run state in active mode. Nothing more.

- **Bypassing Config.Validate** — do not call `Service.Run` without going
  through `LoadConfig`; the validation step prevents mis-configured active mode
  from making changes to durable state.

- **Hard-coding deployment mode** — active/dark state belongs exclusively in
  the ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE env var and `Config`. Do not
  add conditionals in `main.go` that branch on the mode.
