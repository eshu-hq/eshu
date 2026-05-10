# Terraform State Finish Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Finish the Terraform-state collector slice enough that it can create claimable work, read exact state safely, emit the accepted fact set, project canonical Terraform-state graph nodes, and support Terraform config-vs-state correlation without depending on the future AWS cloud scanner.

**Architecture:** Keep collectors facts-first. The Terraform-state runtime opens exact local or S3 sources, parses and redacts state, and commits fact envelopes under workflow claim fencing. The coordinator must create exact work items before the runtime can do useful work; reducers and correlation rule packs consume persisted facts after collection. AWS live-resource joins stay out of this plan until AWS scanner facts exist.

**Tech Stack:** Go, Postgres workflow/fact storage, OpenTelemetry, Terraform-state streaming JSON parsing, existing Eshu workflow coordinator, reducer, and correlation DSL packages.

---

## Current Baseline

The merged code already has:

- `collector-terraform-state` runtime wiring in `go/cmd/collector-terraform-state`.
- Exact local and S3 `StateSource` implementations.
- Streaming JSON parser that emits `terraform_state_snapshot`, `terraform_state_resource`, `terraform_state_output`, and `terraform_state_warning`.
- Redaction before persistence for sensitive outputs and classified attributes.
- Claimed commit fencing through `CommitClaimedScopeGeneration`.
- Graph-backed S3 discovery and explicit seed discovery.
- Terraform-state collector kind, fact schema constants, and `source_confidence=observed` for emitted tfstate facts.

The important gap: the runtime can claim and process work, but the coordinator does not yet create Terraform-state workflow runs or work items from discovery candidates. A healthy runtime can sit idle forever.

## Non-Goals

- Do not build AWS scanner collectors here.
- Do not implement state-to-cloud ARN joins or unmanaged-resource detection yet.
- Do not crawl S3 buckets, infer local `.tfstate` files, or persist raw state.
- Do not collapse all remaining work into one PR.

## PR Order

1. **PR 1:** Terraform-state scheduler and work-item creation.
2. **PR 2:** Reader-stack closure for issue #46: ETag continuity, not-modified handling, DynamoDB lock metadata, memory proof, and telemetry.
3. **PR 3:** Parser fact completion for issue #44: module, provider binding, tag observation, and resource anchors.
4. **PR 4:** Terraform-state reducer projectors.
5. **PR 5:** Terraform config-vs-state correlation rule packs.
6. **PR 6:** Operator/admin/docs and GitHub issue closure pass.

Each PR should be reviewed independently and merged before starting the next. Use subagents per task, but avoid parallel implementation against the same files.

---

## PR 1: Terraform-State Scheduler and Work Items

**Goal:** Make the workflow coordinator create exact Terraform-state workflow runs and work items so `collector-terraform-state` has production work to claim.

**Primary issue:** Follow-up under #45 and parent #50.

**Files:**
- Modify: `go/internal/coordinator/service.go`
- Modify: `go/internal/coordinator/config.go`
- Create: `go/internal/coordinator/tfstate_scheduler.go`
- Create: `go/internal/coordinator/tfstate_scheduler_test.go`
- Modify: `go/internal/storage/postgres/workflow_control.go`
- Modify: `go/cmd/workflow-coordinator/main.go`
- Modify: `go/internal/workflow/README.md`

### Task 1.1: Add a Scheduler Boundary

**Step 1: Write the failing test**

Add `TestServiceSchedulesTerraformStateSeedWorkItems` in `go/internal/coordinator/tfstate_scheduler_test.go`.

The test should:
- Build one enabled, claim-capable `terraform_state` instance with one exact S3 seed.
- Run a scheduler method with a fixed clock.
- Assert one `workflow.Run` is created.
- Assert one `workflow.WorkItem` is enqueued.
- Assert the work item uses:
  - `CollectorKind=terraform_state`
  - `CollectorInstanceID` from config
  - `SourceSystem=terraform_state`
  - `ScopeID` from `scope.NewTerraformStateSnapshotScope`
  - `SourceRunID` and `GenerationID` from `scope.NewTerraformStateSnapshotGeneration`
  - no raw S3 locator in failure messages or status fields

**Step 2: Run the failing test**

Run:

```bash
cd go
go test ./internal/coordinator -run TestServiceSchedulesTerraformStateSeedWorkItems -count=1
```

Expected: fail because no scheduler exists.

**Step 3: Implement the boundary**

Add a small coordinator-owned scheduler abstraction:

```go
type TerraformStatePlanner interface {
    PlanTerraformStateWork(context.Context, TerraformStatePlanRequest) (workflow.Run, []workflow.WorkItem, error)
}
```

Keep it storage-neutral. The concrete planner can use `terraformstate.DiscoveryResolver` and derive work-item identity. Do not open state sources here.

**Step 4: Pass the test**

Run:

```bash
go test ./internal/coordinator -run TestServiceSchedulesTerraformStateSeedWorkItems -count=1
```

**Step 5: Commit**

```bash
git add go/internal/coordinator/tfstate_scheduler.go go/internal/coordinator/tfstate_scheduler_test.go
git commit -m "feat(tfstate): plan workflow work items"
```

### Task 1.2: Wire Scheduler into Active Coordinator Reconcile

**Step 1: Write failing tests**

Add tests proving:
- Dark mode does not schedule work.
- Active mode schedules work after collector instance reconciliation.
- Duplicate candidates do not enqueue duplicate work items.
- Graph waiting produces a waiting status without creating graph-backed work.
- Seed fallback is explicit bootstrap behavior, not accidental graph bypass.

**Step 2: Run focused tests**

```bash
go test ./internal/coordinator -run 'TestService.*TerraformState' -count=1
```

**Step 3: Implement**

Extend `coordinator.Store` only as needed:

```go
CreateRun(context.Context, workflow.Run) error
EnqueueWorkItems(context.Context, []workflow.WorkItem) error
```

Call the scheduler from `runReconcile` only when active mode and claims are enabled.

**Step 4: Wire production dependencies**

In `go/cmd/workflow-coordinator/main.go`, build the Terraform-state planner with:
- `postgres.TerraformStateGitReadinessChecker`
- `postgres.TerraformStateBackendFactReader`
- `postgres.NewWorkflowControlStore`
- the existing tracer/meter where available

**Step 5: Verify**

```bash
go test ./internal/coordinator ./cmd/workflow-coordinator ./internal/storage/postgres ./internal/workflow -count=1
```

**Step 6: Commit**

```bash
git add go/internal/coordinator go/cmd/workflow-coordinator go/internal/storage/postgres go/internal/workflow/README.md
git commit -m "feat(tfstate): schedule claimable workflow work"
```

---

## PR 2: Reader Stack Closure for #46

**Goal:** Close the remaining reader-stack gaps: prior ETag flow, not-modified handling, DynamoDB lock metadata, memory proof, and missing runtime telemetry.

**Primary issue:** #46.

**Files:**
- Modify: `go/internal/collector/terraformstate/types.go`
- Modify: `go/internal/collector/terraformstate/source_s3.go`
- Modify: `go/internal/collector/terraformstate/source_s3_test.go`
- Modify: `go/internal/collector/tfstateruntime/source.go`
- Modify: `go/internal/collector/tfstateruntime/source_test.go`
- Modify: `go/cmd/collector-terraform-state/aws_s3.go`
- Modify: `go/cmd/collector-terraform-state/aws_s3_test.go`
- Modify: `go/internal/telemetry/instruments.go`
- Modify: `go/internal/telemetry/contract.go`
- Modify: `docs/docs/reference/telemetry/metrics.md`
- Modify: `docs/docs/reference/telemetry/traces.md`

### Task 2.1: Carry Previous ETag Through Discovery and Runtime

**Test first:**
- Add a tfstateruntime test proving `DiscoveryCandidate.PreviousETag` becomes `S3GetObjectInput.IfNoneMatch`.
- Add a discovery/config test proving seed and graph candidates can carry prior ETag only from durable metadata, not operator secrets.

**Implementation:**
- Add an explicit freshness metadata field to `DiscoveryCandidate`, for example `PreviousETag string`.
- Pass it into `S3SourceConfig.PreviousETag`.
- Keep ETags opaque. Do not trim quotes.

**Verification:**

```bash
go test ./internal/collector/terraformstate ./internal/collector/tfstateruntime ./cmd/collector-terraform-state -run 'ETag|NotModified|S3' -count=1
```

### Task 2.2: Treat Not-Modified as Unchanged Work, Not a Collector Failure

**Test first:**
- Add a claimed-source test where S3 returns `terraformstate.ErrStateNotModified`.
- Assert the runtime returns a collected generation representing unchanged metadata, or releases/completes according to the chosen workflow contract without recording a raw error.
- Add telemetry test for `eshu_dp_tfstate_s3_conditional_get_not_modified_total`.

**Implementation:**
- Keep `ErrStateNotModified` unwrap-safe.
- Decide the workflow behavior explicitly:
  - Prefer completed no-op work item with snapshot metadata when prior generation is known.
  - If prior generation is unknown, release or retry with a safe failure class.

**Verification:**

```bash
go test ./internal/collector/tfstateruntime ./internal/collector ./cmd/collector-terraform-state -run 'NotModified|Claimed' -count=1
```

### Task 2.3: Add DynamoDB Lock Metadata as Read-Only

**Test first:**
- Add a `DynamoDBLockMetadataClient` interface test in `terraformstate`.
- Assert lock metadata fields are copied into `SourceMetadata`.
- Assert write-shaped operations are not exposed by the interface.
- Add AWS adapter tests with a fake DynamoDB client covering `GetItem` or `Query` only.

**Implementation:**
- Extend `SourceMetadata` with safe fields such as:
  - `LockDigest string`
  - `LockIDHash string`
  - `LockObservedAt time.Time`
- Add read-only lock metadata config to S3 source.
- Add command-level AWS SDK adapter behind a small interface. Keep SDK types out of `terraformstate` and `tfstateruntime`.

**Verification:**

```bash
go test ./internal/collector/terraformstate ./cmd/collector-terraform-state -run 'Dynamo|Lock|Metadata' -count=1
```

### Task 2.4: Add Memory Envelope Proof

**Test first:**
- Add a generated large-state test that streams at least 10,000 resource instances without storing a fixture file.
- Use a skipped-by-default benchmark for 100 MiB scale if a normal unit test would be too slow.
- Assert no full-payload `json.Unmarshal` appears in `go/internal/collector/terraformstate`.

**Implementation:**
- If allocations scale with all facts, introduce parser batch streaming:
  - `ParseStream(ctx, reader, options, sink)`
  - Keep `Parse` as a compatibility wrapper for tests.
- Keep raw state bytes inside the parser window.

**Verification:**

```bash
go test ./internal/collector/terraformstate -run 'Large|Memory|Streaming' -count=1
go test ./internal/collector/terraformstate -bench 'BenchmarkTerraformStateLarge' -benchmem -run '^$'
```

### Task 2.5: Complete Reader Telemetry

**Test first:**
- Add instrument registration tests for:
  - `eshu_dp_tfstate_snapshots_observed_total{backend_kind,result}`
  - `eshu_dp_tfstate_snapshot_bytes_bucket`
  - `eshu_dp_tfstate_resources_emitted_total`
  - `eshu_dp_tfstate_redactions_applied_total{reason}`
  - `eshu_dp_tfstate_s3_conditional_get_not_modified_total`
  - `eshu_dp_tfstate_parse_duration_seconds_bucket`
- Add span coverage for `tfstate.fact.emit_batch`.

**Implementation:**
- Record bounded labels only.
- Never attach raw locator, bucket, key, local path, output value, attribute value, or state JSON to telemetry.

**Verification:**

```bash
go test ./internal/telemetry ./internal/collector/terraformstate ./internal/collector/tfstateruntime -run 'TerraformState|Telemetry|Metric|Span' -count=1
```

---

## PR 3: Complete Terraform-State Fact Emission for #44

**Goal:** Emit all accepted Terraform-state fact kinds and useful correlation anchors without implementing Terragrunt yet.

**Primary issue:** #44.

**Files:**
- Split: `go/internal/collector/terraformstate/parser.go`
- Create: `go/internal/collector/terraformstate/outputs.go`
- Create: `go/internal/collector/terraformstate/resources.go`
- Create: `go/internal/collector/terraformstate/modules.go`
- Create: `go/internal/collector/terraformstate/providers.go`
- Create: `go/internal/collector/terraformstate/tags.go`
- Create: `go/internal/collector/terraformstate/warnings.go`
- Modify: `go/internal/collector/terraformstate/parser_test.go`
- Add focused tests as new files to keep each under 500 lines.

### Task 3.1: Split Parser Before Adding More Logic

Move existing output/resource/warning helper methods into separate files without changing behavior.

Verification:

```bash
go test ./internal/collector/terraformstate -count=1
wc -l go/internal/collector/terraformstate/*.go go/internal/collector/terraformstate/*_test.go
```

### Task 3.2: Emit Module Facts

**Test first:**
- Parse resources with `module`.
- Assert one `terraform_state_module` fact per module address.
- Assert stable keys do not change with resource order.

**Implementation:**
- Deduplicate module facts by module address.
- Payload should include at least `module_address` and `resource_count`.
- Add source references without raw local/S3 locator.

### Task 3.3: Emit Provider Binding Facts

**Test first:**
- Parse resources using provider strings.
- Assert `terraform_state_provider_binding` facts are emitted.
- Assert provider strings do not introduce raw secret leakage.

**Implementation:**
- Deduplicate by resource address + provider address.
- Payload should include `resource_address`, `provider_address`, provider namespace/type when parseable.

### Task 3.4: Emit Tag Observation Facts

**Test first:**
- Parse `tags` and `tags_all` maps with scalar values.
- Assert one `terraform_state_tag_observation` fact per tag.
- Assert non-scalar tag values are dropped with warning.
- Assert sensitive-looking tag keys or values are redacted if rules match.

**Implementation:**
- Extract tag maps before generic composite dropping.
- Keep tag source refs stable.

### Task 3.5: Add Correlation Anchors to Resource Facts

**Test first:**
- Parse attributes with `arn`, `id`, `name`, `region`, `account_id`.
- Assert `correlation_anchors` contains deterministic, non-secret anchors.

**Implementation:**
- Only include anchors when values are scalar and not redacted.
- Do not infer AWS truth. These are Terraform-state anchors only.

Verification for PR 3:

```bash
go test ./internal/collector/terraformstate ./internal/facts -count=1
```

---

## PRs 4-6

PRs 4-6 are the reducer, correlation, and operator-closure work. Keep those
separate from the collector/runtime work so we can review the graph projection
contract without mixing it into parser changes.

Detailed task breakdown lives in
`docs/plans/2026-05-10-terraform-state-finish-reducer-correlation.md`.

---

## Subagent Execution Model

Use one implementation subagent per task. Do not run implementation subagents in parallel when they touch the same package.

Recommended ownership:

- **Scheduler worker:** `go/internal/coordinator`, `go/cmd/workflow-coordinator`, workflow-control store interfaces.
- **Reader worker:** `go/internal/collector/terraformstate`, `go/internal/collector/tfstateruntime`, `go/cmd/collector-terraform-state`.
- **Parser worker:** Terraform-state parser file split and fact emission.
- **Reducer worker:** `go/internal/reducer/tfstate` and storage projection.
- **Correlation worker:** `go/internal/correlation` and `go/internal/reducer/dsl`.
- **Docs/status worker:** admin status, docs, GitHub issue updates.

Each task needs:

1. Failing test.
2. Minimal implementation.
3. Focused test pass.
4. Self-review.
5. Spec review subagent.
6. Code-quality review subagent.
7. Commit.

Run broader verification before every PR:

```bash
cd go
go test ./internal/collector/terraformstate ./internal/collector/tfstateruntime ./cmd/collector-terraform-state ./internal/coordinator ./cmd/workflow-coordinator ./internal/reducer/tfstate ./internal/correlation/... ./internal/storage/postgres -count=1
golangci-lint run ./cmd/collector-terraform-state ./cmd/workflow-coordinator ./internal/collector/terraformstate ./internal/collector/tfstateruntime ./internal/coordinator ./internal/reducer/tfstate ./internal/correlation/... ./internal/storage/postgres
git diff --check
```

## First Task to Start

Start with **PR 1, Task 1.1: Add a Scheduler Boundary**.

Reason: without durable work-item creation, the collector runtime can be correct and still do nothing in production. Fact enrichment, reducer projection, and correlation all depend on having actual Terraform-state work flowing through claims.
