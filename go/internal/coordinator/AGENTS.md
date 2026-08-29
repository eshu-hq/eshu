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
5. `go/internal/coordinator/package_registry_scheduler.go` — bounded
   `package_registry` work-item planning
   - `go/internal/coordinator/tempoplanner/planner.go` and `tempo_service.go` —
     the extracted periodic external-API planner and its root scheduling seam:
     one enabled `configuration.targets[]` entry becomes one claimable work
     item with a per-`scope_id` `FairnessKey`; disabled targets are skipped
   - `go/internal/coordinator/lokiplanner/planner.go` and `loki_service.go` —
     the extracted Loki planner and its root scheduling seam under the same
     ordering, clock, admission, retry, and telemetry boundary
   - `go/internal/coordinator/prometheusmimir/planner.go` and
     `prometheus_mimir_service.go` — the extracted Prometheus/Mimir planner and
     root seam; preserve configured order, safe requested-scope metadata, and
     per-target fairness partitions
   - `go/internal/coordinator/grafanaplanner/planner.go` and
     `grafana_service.go` — the extracted Grafana planner and root seam;
     preserve all-target validation, configured order, privacy, and the
     instance-ID-to-scope-ID fairness fallback
   - `go/internal/coordinator/pagerdutyplanner/planner.go` and
     `pagerduty_service.go` — the extracted PagerDuty planner and root seam;
     preserve schedule/bootstrap/webhook trigger resolution and precedence,
     all-target validation, scope membership, privacy, configured order,
     and provider-partitioned fairness identity
   - `go/internal/coordinator/jiraplanner/planner.go` and `jira_service.go` —
     the extracted Jira planner and root seam; preserve
     schedule/bootstrap/webhook trigger resolution and precedence, all-target
     validation, scope membership, privacy, configured order, and
     site-partitioned fairness identity
   - `go/internal/coordinator/scannerworker/planner.go` and
     `service_scanner_worker.go` — the extracted scanner-worker planner and its
     root scheduling seam; runtime-local paths stay out of requested-scope
     metadata
6. `go/internal/workflow/service.go` (does not exist — `Store` is defined in
   `service.go` here; the workflow contracts are in `internal/workflow`)
7. `go/internal/telemetry/instruments.go` and `contract.go` — before adding
   metric or span names
8. `go/internal/coordinator/plannercontract/README.md` and `doc.go` — shared
   plan-key grammar and the boundary it does not own

## Invariants this package enforces

- **Dark by default** — `deploymentModeDark` is the fallback in `withDefaults`.
  Do not change the default mode without a documented deployment decision.
- **Active mode gate** — `Config.Validate` returns an error if
  `DeploymentMode == active` without `ClaimsEnabled=true` and at least one
  enabled claim-capable collector instance. This gate must not be weakened.
- **HeartbeatInterval < ClaimLeaseTTL** — enforced by `Config.Validate`.
  Violated configurations exit at startup.
- **Nil reap ticker in dark mode** — `reapTicker` is nil in dark mode;
  `tickerChan(nil)` returns a nil channel. The `select` never fires on it.
  Do not replace `tickerChan` with a non-nil channel in dark mode.
- **Type-assertion guard for Reap and RunReconciliation metrics** —
  `recordReap` and `recordRunReconciliation` in `service.go` use interface
  type assertions because `Metrics` only declares `RecordReconcile`. Always
  use `NewMetrics` in production wiring to get all three.
- **Store is required** — `Service.Run` returns an error immediately if
  `s.Store == nil`. Do not add fallback behavior here.
- **Shared plan-key validation stays dependency-neutral** — schedulers and
  extension egress parsing call `plannercontract.ValidateSafePlanKey` directly.
  Terraform-state keeps its separate validator, and the root `firstNonBlank`
  helper remains with its existing OCI/package/vulnerability consumers.

## Common changes and how to scope them

- **Add a new reconcile operation** → add a method to `Store` in `service.go`;
  add a private run helper that calls it and records telemetry; call the helper
  from the appropriate ticker branch in `Service.Run`; add a new observation
  type and a recording method to `Metrics` in `metrics.go`; register counter,
  histogram, and gauge instruments in `NewMetrics`; run
  `go test ./internal/coordinator -count=1`.

- **Add a new periodic external-API collector scheduler** (template:
  `grafanaplanner/planner.go` + `grafana_service.go`) → add a `<Kind>Planner`
  interface in `<kind>_service.go` and a planner field on `Service` in
  `service.go`; add the concrete request and `PlanXxxWork` planner in
  `<kind>planner/planner.go` that emits one work item per
  enabled `configuration.targets[]` entry with a per-target `FairnessKey` that
  preserves durable partition metadata; rely on the parent Postgres open-target
  admission guard to prevent overlapping scheduled work; add
  `schedule<Kind>Work`, `shouldSchedule<Kind>`, and `<kind>PlanKey` in
  `<kind>_service.go`; call `schedule<Kind>Work` in `runReconcile` adjacent to
  the existing schedule calls (guarded by active mode and `ClaimsEnabled`); wire
  the concrete planner in `go/cmd/workflow-coordinator/main.go`; add the
  `CollectorKind` constant in `internal/scope/scope.go`. The planner must be
  idempotent: `RunID` derives from the instance, resolved trigger, and plan key;
  `GenerationID` and `WorkItemID` derive from the instance, plan key, and target
  scope. Run the child package explicitly with
  `go test ./internal/coordinator/<kind>planner ./internal/coordinator
  ./internal/scope -count=1`.

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

- **Extract a scheduler family** → keep its `<kind>_service.go`, root planner
  interface, scheduling position, durable admission, clock, and telemetry in
  this package. The child scheduler may depend on `plannercontract`; it must not
  import the root coordinator package. Freshness families also keep trigger
  claiming and handed-off/failed transitions in the root service.

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

- **Adding trigger normalization or ad hoc claim ownership here** — the
  coordinator may plan bounded work rows for families with explicit planners,
  but it must not claim work on behalf of collectors. Claim ownership belongs
  to collector runtimes through `collector.ClaimedService`.

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
