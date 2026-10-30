# AGENTS.md — internal/coordinator guidance for LLM assistants

## Read first

1. `go/internal/coordinator/README.md` — ownership boundary, loop structure,
   dark/active branching, exported surface, and metric inventory
2. `go/internal/coordinator/service.go` — `Service.Run`, `runReconcile`,
   `runReapExpiredClaims`, `runWorkflowReconciliation`, and `tickerChan`
3. `go/internal/coordinator/config.go` — `LoadConfig`, `LoadConfigObserved`
   (reports producer-grant decisions; keep it observation-only so a denial
   still skips or fails startup exactly as before), `Config.Validate`, env
   var names, and the `withDefaults` application order
4. `go/internal/coordinator/metrics.go` — `otelMetrics`, `NewMetrics`, and the
   type-assertion pattern for `RecordReap`/`RecordRunReconciliation`
5. `go/internal/coordinator/registry/package/scheduler.go` — bounded
   `package_registry` work-item planning. The directory is `package` but the
   Go package name is `packages`, because `package` is a keyword; importers
   need an explicit `packages "…/coordinator/registry/package"` alias
   - `go/internal/coordinator/planner/tempo/planner.go` and `tempo_service.go` —
     the extracted periodic external-API planner and its root scheduling seam:
     one enabled `configuration.targets[]` entry becomes one claimable work
     item with a per-`scope_id` `FairnessKey`; disabled targets are skipped
   - `go/internal/coordinator/planner/loki/planner.go` and `loki_service.go` —
     the extracted Loki planner and its root scheduling seam under the same
     ordering, clock, admission, retry, and telemetry boundary
   - `go/internal/coordinator/planner/prometheus/planner.go` and
     `prometheus_mimir_service.go` — the extracted Prometheus/Mimir planner and
     root seam; preserve configured order, safe requested-scope metadata, and
     per-target fairness partitions
   - `go/internal/coordinator/planner/grafana/planner.go` and
     `grafana_service.go` — the extracted Grafana planner and root seam;
     preserve all-target validation, configured order, privacy, and the
     instance-ID-to-scope-ID fairness fallback
   - `go/internal/coordinator/planner/pagerduty/planner.go` and
     `pagerduty_service.go` — the extracted PagerDuty planner and root seam;
     preserve schedule/bootstrap/webhook trigger resolution and precedence,
     all-target validation, scope membership, privacy, configured order,
     and provider-partitioned fairness identity
   - `go/internal/coordinator/planner/jira/planner.go` and `jira_service.go` —
     the extracted Jira planner and root seam; preserve
     schedule/bootstrap/webhook trigger resolution and precedence, all-target
     validation, scope membership, privacy, configured order, and
     site-partitioned fairness identity. Like every in-tree selector,
     `shouldScheduleJira` skips an instance whose configuration is an
     `eshu.component.instance.v1` document (see **Component ownership** below)
   - `go/internal/coordinator/scanner/worker/planner.go` and
     `service_scanner_worker.go` — the extracted scanner-worker planner and its
     root scheduling seam; runtime-local paths stay out of requested-scope
     metadata
   - `go/internal/coordinator/planner/gcp/planner.go` and `gcp_service.go` —
     the extracted GCP Cloud Asset Inventory planner and root seam; preserve
     explicit `live_collection_enabled` opt-in, sorted scope order, default
     derivation, and per-scope fairness identity. `service_gcp_freshness.go`
     and `config.go` call `gcp.EnabledScopes` and
     `gcp.ValidateClaimSchedulerConfiguration` instead of reaching into
     the child's private configuration types
   - `go/internal/coordinator/oci/registry/planner.go` and
     `oci_registry_service.go` — the extracted OCI registry planner and root
     seam; preserve per-provider identity resolution across Docker Hub, GHCR,
     ECR, Google Artifact Registry, Azure Container Registry, JFrog, and
     Harbor, and duplicate normalized-target rejection. The child keeps its
     own copy of the tiny `firstNonBlank` helper rather than importing
     `schedule.FirstNonBlank`, which #6781 lifted out of root and which now
     serves the extracted `registry/package` and `vulnerability` planners. Unlike every other extraction
     in this list, the `OCIRegistryPlanner` interface itself stays in
     `service.go` rather than moving into `oci_registry_service.go` — issue
     #6057 scopes this move to the `_scheduler.go` half only and treats
     decomposing `Service`'s interface block as a separate design decision
   - `go/internal/coordinator/planner/tfstate/planner.go` and `tfstate_service.go` —
     the extracted Terraform-state planner and root seam; preserve the run,
     work-item, and generation identity formats, the per-instance
     `FairnessKey`, and the rule that no raw bucket, key, region, or version
     locator reaches a durable row. Unlike every other child here the planner
     is not pure: it carries `GitReadiness` and `BackendFacts` ports and calls
     them while planning, which is why `main.go` constructs it with fields.
     Its plan-key validator stays local and stricter than
     `contract.ValidateSafePlanKey`. As with `oci/registry`, the
     `TerraformStatePlanner` interface itself stays in `service.go` — issue
     #6057 scopes this move to the `_scheduler.go` half only
   - `go/internal/coordinator/planner/aws/freshness/planner.go` and
     `service_aws_freshness.go` — the extracted AWS freshness planner and root
     seam; preserve trigger coalescing by freshness key, sorted target order,
     the per-account `FairnessKey`, and the rule that an unauthorized target
     fails the batch instead of being dropped. Unlike `oci/registry`'s
     `firstNonBlank`, the shared `target_scopes` parsing is exported rather
     than copied: `ParseTargetScopes` and `TargetAuthorized` are ~80 lines of
     decoding plus the authorization predicate, and root's
     `findAWSFreshnessInstance` filter and the planner's rejection are two
     halves of one decision.
     `go/internal/coordinator/planner/aws/scheduled/planner.go` — the extracted
     scheduled AWS family — calls the same `ParseTargetScopes` and plans
     from `[]freshness.TargetScope`, while keeping its own
     exported `ScanEnabled` decode for the sibling `scheduled_scan_enabled`
     flag. The package is not named `awsfreshness` because that name is
     already the repo-wide import alias for
     `internal/coordinator/aws/freshness` (the host-owned trigger and
     service-kind vocabulary, Refs #9296)
   - `go/internal/coordinator/planner/component/extension/planner.go` and
     `component_extension_service.go` — the extracted generic
     component-extension planner and root seam; preserve activation-scoped
     work-item construction and requested-scope privacy (no raw host config
     path or credentials). Unlike every other extraction, the activation
     configuration it plans from is not this planner's own type: it lives in
     `go/internal/coordinator/component/activation` (`Config`, `RuntimeConfig`,
     `ParseConfig`), a dependency-neutral package below both root and the
     planner, landed as its own commit (2026-08-31) because
     `component_activation_config.go` (construction),
     `component_extension_service.go` (eligibility, and the shared
     `hasComponentActivationConfiguration` check that `pagerduty_service.go`
     and `jira_service.go` use to exclude component-configured instances), and
     `governance_audit.go` (audit identity) all depend on the same parsing for
     reasons unrelated to component-extension planning. Those root files and
     the extension planner import `activation`; `component_activation_config_test.go` is the sole
     test importer. The shared contract imports neither root nor a planner.
   - `go/internal/coordinator/cicd/run/planner.go` and
     `cicd_run_service.go` — the nested CI/CD run planner and its root
     scheduling seam; preserve configured target order, deterministic run and
     work-item identities, requested-scope privacy, active and claims gates,
     clock-derived plan keys, and durable open-target admission. The leaf is a
     planner boundary, not an independently deployable service.
6. `go/internal/workflow/service.go` (does not exist — `Store` is defined in
   `service.go` here; the workflow contracts are in `internal/workflow`)
7. `go/internal/telemetry/instruments.go` and `contract.go` — before adding
   metric or span names
8. `go/internal/coordinator/planner/contract/README.md` and `doc.go` — shared
   plan-key grammar and the boundary it does not own

## Where a family lives after #6781

The root was 49 non-test files against the 40-file dirgate cap. Part A moved
out every planner half that had no subpackage home yet. What remains in the
root is the `Service` type and its methods, because Go pins a method to its
type's package — the `*_service.go` halves cannot leave without redesigning
`Service` into composed sub-types, which #6781 does not do.

Movability alone is not the test. The three `service_*_freshness.go` files
keep trigger-resolution types and free functions that Go would let them move;
they stayed because their `Service` methods pin the file in place anyway, so a
move removes nothing from the root. Apply that test before proposing a new
subpackage here: if the root file survives, the split buys nothing.

| Directory | Holds |
| --- | --- |
| `schedule/` | The shared scheduling substrate: target classes and ranking, derived-target skip evidence and budget, rotation buckets and offsets, plan keys, read limits, set and version helpers. Imported by `vulnerability` and `registry/package`; imports neither. |
| `vulnerability/` | Vulnerability-intelligence derivation, OSV batching and chunk sizing, `IntelligenceWorkPlanner`. |
| `registry/package/` | Package-registry derivation, ecosystem targets, `WorkPlanner`. Go package name `packages`. |
| `egress/` | Collector and extension egress policy parsing and decisions. |
| `semantic/` | The egress-gated semantic-provider execution worker. |
| `environment/` | `Bool`, `Int`, `Duration` env parsing, shared by root `LoadConfig` and `semantic`. Named `environment` and not `env` because `.gitignore` carries `env/`, which silently excludes any such directory from git. |
| `governance/audit/` | Redacted audit scope-hash, correlation id, service id, and append timeout. |

Two rules the split had to obey, and both will apply to the next family move:

- A subpackage may never import the root, because the root imports it. A
  helper both need is hoisted into a neutral package below both, never
  exported from one family to another.
- Where only one symbol crosses, prefer a consumer-declared interface or a
  passed-in value over a new package. `semantic` declares its own
  `GovernanceAuditAppender`, and `semantic.NewProviderWorkerMetrics` takes the
  instrument prefix as an argument rather than importing `coordinator`
  for `MetricPrefix`.

## Invariants this package enforces

- **Shard grants follow the base** — `workflowTenantGrantScopeID` maps a
  security-alert shard id to its base scope for the tenant-grant lookup
  (#9019). Keep it limited to `CollectorSecurityAlert` and to ids
  `shard.ParseScopeID` accepts; an ungranted base must still deny its shards
  and retirements (`workflow_tenant_grants_shard_test.go`). The rule is written
  up in `security/alert/README.md`; the coordinator README is at its line cap.
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
- **Work item retention is on by default and non-fatal** (#8829) —
  `LoadConfig` sets `WorkItemRetentionConfig.Enabled` unless the retention age
  is explicitly 0 (default 720h, floor 48h), and it runs only in active mode;
  `Run` fails when it is on with no `WorkItemRetention` store, so any
  active-mode `Service` built from `LoadConfig` must wire one. A failed
  pass logs ERROR and counts `outcome="error"` but never returns from `Run`.
  Keep the loop started after the initial run reconciliation, on its own
  goroutine (`startWorkItemRetention`); never move a pass back into `Run`'s
  select loop, where it delays reap and reconciliation. Shutdown is outcome
  `canceled`, not `error`; decide it from the error (`errors.Is` on
  `context.Canceled` with a done context), never from `ctx.Err()` alone, so a
  real batch failure during shutdown still counts as `error`. Its metric
  names stay literal strings. See `work-item-retention.md`.
- **Shared plan-key validation stays dependency-neutral** — schedulers and
  extension egress parsing call `contract.ValidateSafePlanKey` directly.
  Terraform-state keeps its separate, stricter validator, which moved with the
  planner into the `tfstate` child (`planner/tfstate/planner.go`). The
  `FirstNonBlank` helper now lives in `schedule/derivation.go` alongside the
  rest of the derived-target substrate its package-registry and
  vulnerability-intelligence consumers share (#6781); the extracted
  `oci/registry` child still keeps its own identical copy rather than
  importing a sibling. AWS target-scope
  parsing goes the other way: `freshness.ParseTargetScopes` and
  `TargetAuthorized` are the single definition, and `service_aws_freshness.go`
  and `planner/aws/scheduled/planner.go` both call them rather than keeping a root
  copy that could drift from the planner's own authorization decision.

## Component ownership

An instance whose configuration is an `eshu.component.instance.v1` document
(valid, incomplete, or malformed JSON) belongs to the generic component
scheduler alone, for every collector kind (#8907, #8919). An in-tree planner
reads that configuration as its own target list and fails the whole reconcile,
because schedulers stop at the first error. One predicate enforces it:

- `hasComponentActivationConfiguration` (`component_extension_service.go`) is
  the single parse check. `shouldScheduleComponentExtension` claims what it
  matches; `inTreeSchedulable(instance, kind)` (`in_tree_schedulers.go`) is
  kind + enabled + claims-enabled + not matched, and every in-tree
  `shouldSchedule*` selector returns it. This also covers the AWS, GCP, and
  incident-freshness handoffs, which run outside `runReconcile`.
- Startup validation follows the same rule. `Config.Validate` skips the
  kind-specific claim check (`validateCollectorClaimSchedulingSupported`, the
  GCP `live_collection_enabled` rule) for a matched instance, through
  `isComponentActivationConfiguration`, the raw-configuration form of the same
  parse. `workflow.DesiredCollectorInstance.Validate` checks the component
  document before the Terraform state validator. A gcp-kind in-tree instance
  without `live_collection_enabled` is still a startup error.
- A malformed or non-object configuration on any kind is now reported by the
  generic planner (`plan component extension work for ...`), not by the
  in-tree planner, so the error text names the component planner.
- `reconcileSchedulers()` is the one ordered list `runReconcile` dispatches;
  `incidentFreshnessSelectors()` lists the handoff selectors that run on the
  maintenance ticker. `in_tree_schedulers_test.go` holds every entry to the
  rule and parses the package source, so a new `shouldSchedule*` function that
  is not registered fails the build. Register it; do not add a dispatch block
  to `runReconcile` or a per-kind copy of the exclusion.
- Blank, unrelated, and in-tree target configuration stay with the in-tree
  planner. Skipping is silent by design: it is a partition, and the generic
  scheduler logs and audits its own egress decisions.
- A kind that needs host-derived scopes for its activations gets them from a
  kind-scoped hook, not from a selector: `scheduleActivationDerivedScopes`
  (`component_extension_service.go`) runs inside `scheduleComponentExtensionWork`,
  after the egress decision, and admits through
  `createWorkflowWorkIfNoOpenTargets` (#9311). Today only
  `vulnerability_intelligence` has one. It decodes the opaque
  `activation.Config.ComponentConfig` (filled by the readback only for kinds
  `activationCarriesComponentConfig` names), refuses a host scope kind or
  source system that is not `vulnerability_intelligence` or a derive section
  with blank `sources`/`ecosystems` (`reason=derive_lists_not_explicit`). Both
  refusals live in one pure function, `checkActivationDerivation`.
  `componentActivationRuntimeConfig` (registry activations) and
  `Config.Validate` (every desired instance, through
  `checkActivationDocumentDerivation`, which covers a static
  `ESHU_COLLECTOR_INSTANCES_JSON` activation document) call it, so an invalid
  block is a `LoadConfig` error before any durable write on both config
  paths; the hook calls it again as a
  backstop, and like every reconcile error a refusal there stops the
  coordinator (`runReconcile` returns, `Service.Run` exits). Keep the rules in
  that function; do not fork them per caller. The hook reads targets
  through the same `vulnerabilityOwnedTargets` and
  `vulnerabilityInstalledEvidenceTargets` helpers as the in-tree path, which
  take the decoded derive section. Do not add a second scheduler that selects
  activations: it would bypass egress and break the partition above.

## Common changes and how to scope them

- **Add a new reconcile operation** → add a method to `Store` in `service.go`;
  add a private run helper that calls it and records telemetry; call the helper
  from the appropriate ticker branch in `Service.Run`; add a new observation
  type and a recording method to `Metrics` in `metrics.go`; register counter,
  histogram, and gauge instruments in `NewMetrics`; run
  `go test ./internal/coordinator/... -count=1`.

- **Add a new periodic external-API collector scheduler** (template:
  `planner/grafana/planner.go` + `grafana_service.go`) → add a `<Kind>Planner`
  interface in `<kind>_service.go` and a planner field on `Service` in
  `service.go`; add the concrete request and `PlanXxxWork` planner in
  `planner/<kind>/planner.go` that emits one work item per
  enabled `configuration.targets[]` entry with a per-target `FairnessKey` that
  preserves durable partition metadata; rely on the parent Postgres open-target
  admission guard to prevent overlapping scheduled work; add
  `schedule<Kind>Work` and `shouldSchedule<Kind>` in `<kind>_service.go` and
  take the plan key from `s.scanInterval(instance)` plus
  `scheduledPlanKey(...)` (see **Plan keys for a periodic scheduled planner**
  below); make `shouldSchedule<Kind>` return `inTreeSchedulable(instance,
  scope.Collector<Kind>)` and add the scheduler to `reconcileSchedulers()` in
  `in_tree_schedulers.go` (guarded by active mode and `ClaimsEnabled`; see
  **Component ownership**); wire
  the concrete planner in `go/cmd/workflow-coordinator/main.go`; add the
  `CollectorKind` constant in `internal/scope/collector_kind.go`. The planner must be
  idempotent: `RunID` derives from the instance, resolved trigger, and plan key;
  `GenerationID` and `WorkItemID` derive from the instance, plan key, and target
  scope. Run the child package explicitly with
  `go test ./internal/coordinator/planner/<kind> ./internal/coordinator
  ./internal/scope -count=1`.

- **Change the reconcile interval default** → edit
  `schedule.DefaultReconcileInterval` in `schedule/derivation.go`, which both
  `config.go` and the derived-target plan-key and rotation helpers read; document the change in `README.md` and the configuration
  table; verify that `Config.Validate` still passes with the new default.

- **Plan keys for a periodic scheduled planner** → call
  `s.scanInterval(instance)` then `scheduledPlanKey(instance, observedAt,
  interval)` from `scheduled_work.go`; never add a per-kind copy of the
  truncate-the-clock function (sixteen of them were collapsed in #6720). The
  per-instance `scan_interval` field is decoded there for every kind; a
  kind-specific configuration decoder must not grow its own copy. Freshness
  planners keep their `freshness-` keys and stay on the global interval.

- **Add a new config field from env** → add the `environment.Bool`,
  `environment.Int`, or `environment.Duration` call in `LoadConfig`;
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
  this package. The child scheduler may depend on `contract`; it must not
  import the root coordinator package. Freshness families also keep trigger
  claiming and handed-off/failed transitions in the root service. If a
  configuration type the scheduler's core function returns or consumes is
  ALSO used by a root file that is not its own `<kind>_service.go` — the
  component-extension extraction found `parseComponentInstanceConfig` used by
  `component_activation_config.go`, `pagerduty_service.go`, and
  `governance_audit.go` — do not export it from the child (that forces those
  unrelated files to import a scheduler-specific package) and do not leave it
  in root (the child cannot import root once root imports the child for the
  request type, so the child will not compile). Hoist it into a new
  dependency-neutral package below both root and the child FIRST, as its own
  commit, before extracting the scheduler — see `component/activation`, which
  matches what `contract` already is for plan-key validation and what
  `projector/intent` is for the projector families' equivalent problem. Every
  consumer — root's several call sites and the child — imports the neutral
  package; neither root nor the child imports the other for this purpose.

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
