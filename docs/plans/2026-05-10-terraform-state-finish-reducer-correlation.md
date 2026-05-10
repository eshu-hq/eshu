# Terraform State Reducer and Correlation Follow-Up Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task after PRs 1-3 from `2026-05-10-terraform-state-finish.md` are merged.

**Goal:** Project Terraform-state facts into canonical graph nodes, add Terraform config-vs-state correlation, and make the feature operable without depending on AWS cloud scanner facts.

**Architecture:** Reducers consume committed `terraform_state_*` facts after collection. Correlation rules compare Git Terraform config evidence against Terraform-state evidence. AWS state-to-cloud joins remain deferred until live AWS scanner facts exist.

**Tech Stack:** Go, Eshu reducer packages, correlation DSL, Postgres/Cypher storage adapters, OpenTelemetry.

---

## PR 4: Terraform-State Reducer Projectors

**Goal:** Consume Terraform-state facts and project canonical Terraform-state graph nodes/checkpoints.

**Primary issues:** #43 and #50.

**Files:**
- Modify: `go/internal/reducer/tfstate/contract.go`
- Create: `go/internal/reducer/tfstate/projector.go`
- Create: `go/internal/reducer/tfstate/projector_test.go`
- Modify storage/cypher or Postgres projector files used by existing reducer projectors.
- Modify reducer service wiring where projectors are registered.

### Task 4.1: Resource Projector

**Test first:**
- Feed `terraform_state_resource` facts.
- Assert canonical Terraform resource rows/nodes are written with lineage, serial, module, provider, and anchors.
- Assert source confidence remains `observed` for source facts and `inferred` only for reducer-created correlation/materialization rows.

**Implementation:**
- Read only committed fact rows.
- Reject unknown tfstate schema versions.
- Preserve source references and generation identity.
- Publish no AWS cloud joins.

### Task 4.2: Module and Output Projectors

**Test first:**
- Feed module/output facts.
- Assert canonical module/output graph nodes are written.
- Assert phase checkpoints publish:
  - `terraform_resource_uid.canonical_nodes_committed`
  - `terraform_module_uid.canonical_nodes_committed`

**Implementation:**
- Keep module and output projectors independent.
- Ensure reducer idempotency across repeated runs.

**Verification:**

```bash
go test ./internal/reducer/tfstate ./internal/reducer ./internal/storage/postgres ./internal/storage/cypher -run 'TerraformState|Tfstate' -count=1
```

---

## PR 5: Terraform Config-vs-State Correlation

**Goal:** Add Terraform-state DSL inputs and config-vs-state drift rules that do not require AWS scanner facts.

**Primary issue:** #43.

**Files:**
- Create: `go/internal/correlation/rules/terraform_state_rules.go`
- Create: `go/internal/correlation/rules/terraform_config_state_drift_rules.go`
- Add tests under `go/internal/correlation/rules`
- Modify DSL reducer input mapping where existing rule packs are loaded.

### Task 5.1: Terraform-State Evidence Atoms

Create evidence atoms from canonical tfstate rows:
- resource address
- module address
- provider address
- ARN
- resource ID/name
- tags
- lineage and serial

**Test first:**
- Add fixtures with resource, module, output, and tag facts.
- Assert emitted atoms contain no raw state locator or redacted attribute value.

### Task 5.2: Config-vs-State Drift Rule Pack

Implement only config-vs-state drift:
- declared in config, missing from state
- present in state, missing from config
- changed deterministic anchors

Do not implement:
- state-to-cloud ARN joins
- unmanaged cloud resources
- orphan AWS resources

**Verification:**

```bash
go test ./internal/correlation/rules ./internal/correlation/engine ./internal/reducer/dsl -run 'TerraformState|Drift' -count=1
```

---

## PR 6: Operator/Admin/Docs and Issue Closure

**Goal:** Make the feature operable and close the tracking loop.

**Files:**
- Modify: `go/internal/status/coordinator.go`
- Modify: `go/internal/storage/postgres/workflow_status.go`
- Modify: `docs/docs/reference/environment-variables.md`
- Modify: `docs/docs/reference/telemetry/metrics.md`
- Modify: `docs/docs/reference/telemetry/traces.md`
- Modify: `docs/docs/deployment/service-runtimes.md`
- Modify: `go/internal/collector/terraformstate/README.md`
- Modify: `go/cmd/collector-terraform-state/README.md`

### Task 6.1: Admin Status Detail

Expose safe workflow detail:
- collector kind
- latest failure class
- safe failure message
- waiting-on-git-generation state
- counts of pending/claimed/completed/retryable/terminal items

Never expose:
- local file paths
- S3 bucket/key
- raw state values

### Task 6.2: Docs and GitHub Issues

Update docs with:
- how work gets scheduled
- seed vs graph discovery behavior
- redaction guarantees
- telemetry
- common failure classes
- how to validate locally

Update GitHub:
- Comment on #46 with completed and remaining acceptance criteria.
- Close #46 only after PR 2 merges.
- Comment on #44 with parser/fact completion.
- Close #44 only after PR 3 and Terragrunt decision are complete.
- Keep #43 open until reducer + config-vs-state drift is merged.
- Keep AWS-dependent state-to-cloud joins as follow-up under AWS milestone.

**Verification:**

```bash
go test ./internal/status ./internal/storage/postgres ./cmd/collector-terraform-state ./cmd/workflow-coordinator -count=1
git diff --check
```
