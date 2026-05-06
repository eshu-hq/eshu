# ADR: Workflow Coordinator And Multi-Collector Runtime Contract

**Date:** 2026-04-20
**Status:** Accepted
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md` — **consumer contract; gates the remaining coordinator work described in §Rollout "collector-specific convergence checkpoints" and "second-pass first-class gates"**

---

## Status Review (2026-05-03)

**Current disposition:** Accepted; Git claim proof path implemented, production
ownership still guarded.

The dark-mode coordinator runtime, config, Compose, Helm, and status wiring
exist. Claims remain disabled by default. The claim-aware Git runner, fenced
release, family fairness scheduler, and active Compose proof guard now exist,
but Kubernetes still keeps coordinator claims dark until remote full-corpus
validation proves clean queue state, API/MCP truth, and evidence drilldown.

**Remaining work:** finish claim-enabled production ownership only after the
remote proof and the future collector families are ready. Webhook intake stays
as a separate trigger path.

## Context

Eshu is no longer a Git-only indexing tool in spirit, even if
Git is still the only fully deployed collector family today.

The current platform already points toward a broader architecture:

- `collector-git`, `ingester`, and `bootstrap-index` own repository selection,
  repo sync, parsing, content shaping, and fact emission
- the reducer already owns durable queue draining, projection, reconciliation,
  replay, and canonical writes
- the API and MCP server already act as read surfaces rather than ingestion
  services
- the multi-source correlation ADR and implementation plan already assume new
  collector families such as AWS cloud scanning and Terraform state scanning
  will arrive as separate sources feeding the same data plane

This ADR builds directly on
`2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`.
It does not introduce a second source-identity model. The coordinator contract
defined here must reuse the published `ScopeKind` and `CollectorKind` values
from that ADR and from `go/internal/scope/scope.go`, including:

- `git`
- `aws`
- `terraform_state`
- `webhook`

At the same time, the current runtime naming and workflow story is too Git-era
and too bootstrap-centric to scale cleanly into the next phase of the
platform.

### What Changed

Three decisions have converged from recent work:

1. The platform should support multiple source families:
   - Git
   - Terraform config
   - Terraform state
   - AWS cloud
   - event-triggered change signals such as GitHub webhooks
2. Collectors should remain source-local and facts-first rather than turning
   the current Git ingester into a multi-source blob.
3. The reducer should remain the single shared substrate for cross-source
   reconciliation and canonical graph truth.

That leaves one missing control-plane question:

> Which runtime owns run creation, scheduling, trigger intake, collector
> instance configuration, completeness, and orchestration across multiple
> collector families?

The current answer is fragmented:

- `bootstrap-index` is documented as a one-shot helper
- `ingester` owns the long-running Git repo-sync loop
- local and cloud runtime docs still present Git-shaped freshness semantics
- future collector milestones would otherwise need to invent their own trigger
  and scheduling model

### Why This Matters Now

This decision cannot wait until after AWS and Terraform state work arrives.

Without an explicit coordinator contract, the platform will drift toward one or
more of these failure modes:

1. the Git ingester absorbs more and more source families until it becomes an
   operationally brittle multi-source service
2. every new collector family invents a second scheduling and freshness model
3. `bootstrap-index` quietly becomes a long-running coordinator even though the
   name and operator contract still describe a one-shot bootstrap helper
4. webhook, schedule, and manual recovery paths bypass a shared durable run
   model and create correctness gaps

That would damage all three platform priorities:

- **Accuracy first**: different source families would no longer share the same
  run semantics or completeness model
- **Performance second**: ad hoc coordination would increase duplicate work,
  stale scans, and unbounded trigger fan-out
- **Reliability third**: operators would have no single place to understand
  what is running, what is stuck, and what is complete

---

## Problem Statement

The platform needs a runtime contract that can answer these questions
truthfully:

1. Which collector instances exist?
2. Which source truth does each collector instance own?
3. Which runs were requested, why were they requested, and which scopes do they
   cover?
4. Which runtime currently owns each claimed unit of work?
5. Which generations are complete, partial, stale, failed, or waiting?
6. Which trigger caused a refresh: bootstrap, schedule, webhook, replay, or
   operator recovery?

The current runtime split cannot answer those questions uniformly because the
platform has source-family services but not a first-class orchestration service.

---

## Decision

### Introduce A First-Class Workflow Coordinator Runtime

The platform should introduce a new long-running runtime named
`workflow-coordinator`.

This runtime should own:

- collector instance registration and configuration
- run creation
- trigger normalization
- scheduling
- durable claims and leases
- completeness checkpoints and run-state reporting
- operator-visible workflow status

It should not own:

- source-specific observation
- source-specific parsing
- source-local fact shaping
- cross-source correlation logic
- canonical graph writes

The coordinator must reuse the existing source identity model:

- `CollectorInstance.kind` must use the shared `CollectorKind` enum
- any coordinator-owned scope references must use the shared `ScopeKind` model
- webhook-triggered runs must remain part of the same identity family rather
  than introducing coordinator-local string variants

### Keep Collectors Source-Local And Separate

Each collector family should remain a separate runtime with one bounded
source-truth boundary.

Examples:

- `collector-git`
- `collector-terraform-state`
- `collector-aws-cloud`
- future `collector-gitlab`
- future `collector-bitbucket`

Collectors should continue to own only:

1. source observation
2. scope assignment
3. generation assignment
4. typed fact emission

### Rename And Retire `ingester` As A Permanent Runtime Identity

This ADR chooses an explicit rename path:

- the current `ingester` implementation is the transitional Git collector
  runtime
- the target long-running runtime identity is `collector-git`
- `ingester` should be retired as the public runtime name once the migration is
  complete

This is a runtime-identity change, not just a code refactor. It has blast
radius across:

- Compose service names
- Helm values and templates
- ArgoCD application wiring
- dashboards and alerts
- operator runbooks

The Git collector keeps the current deployment shape during migration unless a
later ADR changes it:

- `StatefulSet`
- workspace PVC
- bounded repo-sync and parsing ownership

This ADR does not change that workload shape yet. It changes the ownership and
public naming target so the platform stops teaching Git-era service identity as
the long-term contract.

### Keep One Shared Reducer Substrate

The reducer should remain the single shared substrate for:

- cross-source correlation
- deployable-unit admission
- deployment mapping
- workload materialization
- cloud asset resolution
- canonical graph writes

This ADR explicitly rejects a second reducer implementation for new source
families.

### Treat Bootstrap As An Action, Not A Long-Running Service Identity

The platform should not repurpose the name `bootstrap-index` into the permanent
name of the orchestration layer.

Instead:

- `bootstrap` should remain an action or trigger mode
- `workflow-coordinator` should be the long-running service identity
- `bootstrap-index` may remain temporarily as a thin one-shot entrypoint that
  requests a bounded bootstrap run through the new coordinator contract

This keeps operator language accurate and avoids teaching the wrong mental
model to future contributors.

### Model Collectors As Configured Instances, Not A Single Hardcoded Git Path

The control plane should support multiple configured instances of the same
collector family.

That is required for:

- multiple Git orgs
- multiple AWS accounts
- mixed auth modes
- future region- or cluster-scoped collectors

The configuration model should therefore be instance-based, not a single
top-level Git sync block.

### Separate Scheduling Policy From Run Trigger Kind

The control plane should not collapse instance behavior and run cause into one
field.

It should model:

- `collector_instance.mode`
  - `continuous`
  - `scheduled`
  - `manual`
- `run.trigger_kind`
  - `bootstrap`
  - `schedule`
  - `webhook`
  - `replay`
  - `operator_recovery`

That keeps the model truthful when, for example, a continuous Git collector
also receives webhook-triggered refreshes.

### Use Declarative Config As The Source Of Truth

In deployed environments, declarative configuration remains the source of
truth.

That means:

- YAML or chart values define desired collector instances
- the coordinator reconciles that desired state into durable
  `collector_instances` rows
- Postgres stores observed and operational state, not the authoritative desired
  config

Operational rows may track:

- last_seen_generation
- enabled state as last reconciled
- health snapshots
- claim ownership
- backlog and completion status

But dynamic enable/disable and instance shape changes should still flow through
the declarative config path, not through direct mutable database edits.

### Define `webhook-ingress` Responsibilities Explicitly

If deployed, `webhook-ingress` is a narrow trigger-normalization runtime.

It should own only:

- request authentication and signature verification
- replay protection and deduplication
- bounded normalization into coordinator run requests
- back-pressure signaling when trigger intake exceeds claim throughput

It should not:

- perform source collection
- bypass coordinator run creation
- write canonical graph truth
- create a second freshness model outside the coordinator

---

## Architecture

### Runtime Roles

The target runtime contract should be:

| Runtime | Primary Responsibility | Long-Running |
| --- | --- | --- |
| `workflow-coordinator` | scheduling, trigger intake, claims, completeness, run orchestration | Yes |
| `collector-git` | Git source observation, repo sync, parsing, fact emission | Yes |
| `collector-terraform-state` | Terraform state observation and fact emission | Yes |
| `collector-aws-cloud` | cloud API observation and fact emission | Yes |
| `resolution-engine` | cross-source reconciliation and canonical writes | Yes |
| `api` | read/query/admin surface | Yes |
| `mcp` | MCP transport over query/admin surface | Yes |
| `bootstrap-index` | one-shot bootstrap trigger wrapper | No |
| `webhook-ingress` | external event normalization into coordinator triggers | Optional, long-running when deployed |

Runtime service names and collector kinds are related but not identical:

- runtime names such as `collector-aws-cloud` are operator-facing service
  identities
- collector instance `kind` values must still use the shared enum values such
  as `aws` and `terraform_state`
- `resolution-engine` remains the runtime/service identity for what this ADR
  refers to functionally as the reducer substrate

### Control-Plane Flow

The target orchestration flow should be:

1. a trigger arrives:
   - bootstrap
   - schedule
   - webhook
   - operator replay
2. the workflow coordinator creates a durable run request
3. the coordinator expands that request into collector-family work units
4. collector instances claim bounded work through durable leases
5. collectors observe source truth and emit facts
6. projector/reducer work continues through the existing shared data plane
7. the coordinator updates completeness and operator-visible run state

This architecture preserves one workflow model across all collector families
without forcing the coordinator to understand each source family’s parsing
internals.

### Coordinator And Reducer Completeness Handshake

The coordinator must not replace reducer-owned convergence truth.

Instead, completeness should be layered:

1. collectors report source observation and fact-emission completion for the
   claimed bounded work
2. projector and reducer continue to own downstream convergence signals
3. the coordinator reads reducer-published readiness state and computes
   workflow-level completeness from it

The existing reducer-owned phase markers remain the authoritative downstream
signals unless and until a later ADR replaces them explicitly. That includes
readiness rows such as:

- `canonical_nodes_committed`
- `semantic_nodes_committed`
- reducer-owned shared follow-up and materialization phase completion

For workflow status, `complete` must mean:

- all collector work for the run’s bounded slice is acknowledged
- no required projector/reducer domains remain outstanding for that slice
- no second-pass domains such as deployment mapping or workload materialization
  remain queued for that slice

A run is not complete merely because collection finished. The coordinator must
report the distinction between:

- `collection_complete`
- `reducer_converging`
- `complete`

### Production Deployment Model

In Kubernetes or other hosted environments, the coordinator should not launch
collector subprocesses as its normal control path.

Instead:

- Kubernetes should run collector runtimes as their own workloads
- the coordinator should coordinate through durable state and claims
- collectors should poll or stream for claimable work under a bounded lease
  model
- completions, retries, and failures should be recorded in Postgres-backed run
  state

This keeps the production runtime model explicit, debuggable, and compatible
with independent scaling.

### Local And One-Shot Model

Local workflows still need a simple operator path.

For local development and recovery workflows:

- `bootstrap-index` may remain a one-shot wrapper
- that wrapper should use the same coordinator contract rather than bypassing
  it with a separate correctness path
- Compose should still be able to run a one-command local stack, but the
  logical control flow should match the hosted control-plane model

During rollout, the coordinator should be deployable in a dark state:

- mounted and observable
- reading desired collector configuration
- publishing status
- claiming no production work until the Git collector migration is enabled

That allows validation of admin/status, telemetry, and durable run state before
the Git collector cutover.

### Collector Instance Configuration Model

The control plane should use an instance-based configuration model.

Illustrative shape:

```yaml
collectors:
  - id: git-private-org
    kind: git
    provider: github
    enabled: true
    bootstrap: true
    mode: continuous
    config:
      orgs:
        - private-org

  - id: aws-prod
    kind: aws
    enabled: true
    bootstrap: true
    mode: scheduled
    config:
      account_alias: prod
      regions:
        - us-east-1

  - id: tfstate-prod
    kind: terraform_state
    enabled: false
    bootstrap: false
    mode: scheduled
    config:
      state_source: s3
```

This example is illustrative only. It does not change the non-goal that the
AWS and Terraform state collectors themselves are separate follow-on
implementation slices.

Key rules:

- the model must support multiple instances per family
- the model must remain schema-validated and bounded
- instance identity must be durable enough for claims, runs, and completeness
  reporting

### Durable State Model

The coordinator should persist control-plane state in Postgres.

At minimum, the design should introduce durable tables for:

- collector instances
- run requests
- collector runs
- leases or claims
- trigger history
- completeness summaries
- retry and failure state

These records should become the operator source of truth for orchestration
status.

### Concurrency And Claiming Invariants

The coordinator authorizes concurrent collection only through durable claims.

Minimum invariants:

1. Every claim must carry:
   - a durable claim identifier
   - an owning collector instance identifier
   - an expiry timestamp
   - a monotonic fencing token
2. Claim completion, heartbeat, and release operations must present the current
   fencing token so expired holders cannot overwrite newer owners after lease
   expiry.
3. Crash recovery must be explicit:
   - expired claims are reaped
   - reaped work is re-queued idempotently
   - the reaper must not require process-local memory to determine ownership
4. The platform must use observable Postgres-backed claim rows as the source of
   truth. Advisory locks may be used as a local optimization later, but not as
   the authoritative claim contract.
5. Claim fairness must be bounded across collector families so one family’s
   backlog cannot starve another indefinitely.

The initial implementation should prefer row-backed claims with bounded
selection and `FOR UPDATE SKIP LOCKED`-style issuance because that model is
durable, inspectable, and fits the existing Postgres-centered control plane.

Before implementation starts, the plan must include a sequence diagram that
shows:

- claim issuance
- heartbeat
- expiry
- reaping
- fenced completion
- reducer-completeness acknowledgment

### Security And Credential Boundaries

The coordinator and trigger path must keep security boundaries explicit.

Minimum requirements:

- `webhook-ingress` must verify request signatures before durable run creation
- webhook replay protection must store enough delivery identity to reject
  duplicates within a bounded replay window
- collector credentials must stay source-family specific:
  - Git tokens or GitHub App credentials remain collector-git scoped
  - AWS credentials remain aws-collector scoped, ideally through runtime
    identity such as IRSA rather than shared static secrets
- the coordinator should not require source-family super-credentials merely to
  schedule work
- coordinator database access should be scoped to orchestration tables plus the
  status surfaces it needs for completeness reporting
- claims, completions, and trigger records must be auditable enough to explain
  who started work and which runtime owned it

---

## Invariants

The following invariants should remain true after the coordinator lands:

1. Collectors remain source-local and facts-first.
2. Collectors do not write canonical graph truth directly.
3. The reducer remains the single cross-source reconciliation path.
4. `bootstrap` remains a trigger mode or action, not the permanent name of a
   long-running orchestration service.
5. New collector families must not introduce a second freshness or completeness
   model.
6. Query surfaces must not become the fallback control plane for orchestration
   gaps.
7. Kubernetes production paths should use durable coordination, not ad hoc
   process spawning from one control pod.
8. The coordinator must compute workflow completeness from reducer-published
   downstream truth; it must not infer completeness from collector completion
   alone.
9. The platform must have one operator-visible pane of glass for workflow
   completeness, exposed from the coordinator admin/status surface.

---

## Observability Requirements

The workflow coordinator should ship with first-class telemetry from day one.

Required signals:

- spans for:
  - run creation
  - trigger normalization
  - collector claim issuance
  - claim expiration
  - completion aggregation
- metrics for:
  - runs created
  - runs completed
  - runs failed
  - claim latency
  - claim contention
  - queue age
  - stale lease count
  - per-collector-family backlog
  - per-instance success/failure rate
- structured logs for:
  - trigger identity
  - collector instance identity
  - scope identity
  - generation identity
  - lease or claim identity
  - failure classification

The coordinator `/admin/status` surface should become the operator pane of
glass for indexing completeness and orchestration state.

It should explicitly report:

- desired collector instances
- active runs
- stuck claims
- per-family backlog
- collection-complete versus reducer-converged state
- the reason a run is still incomplete

At 3 AM, an operator should be able to answer:

- what triggered this run
- which collector has the stuck work
- whether the issue is source auth, parsing, backlog, or reducer convergence
- whether the system is incomplete or merely healthy-but-still-running

---

## Explicit Non-Goals

This ADR does not do the following:

1. implement the AWS collector
2. implement the Terraform state collector
3. move canonical correlation logic into the coordinator
4. add a second reducer
5. define final query-story synthesis for operator narratives
6. commit to one specific webhook provider beyond requiring normalized trigger
   intake
7. replace the reducer-owned phase-state model in this milestone

---

## Rollout Plan

### Phase 1: Contract And Naming

- publish this ADR
- add runtime docs for `workflow-coordinator`
- define the collector instance configuration contract
- define instance modes:
  - `continuous`
  - `scheduled`
  - `manual`
- define run trigger kinds:
  - `bootstrap`
  - `schedule`
  - `webhook`
  - `replay`
  - `operator_recovery`

### Phase 2: Durable Orchestration Substrate

- add Postgres control-plane tables for runs, claims, instances, and
  completeness
- add the coordinator runtime with shared admin/status support
- add telemetry for claims, runs, and stale work detection
- deploy the coordinator dark:
  - status on
  - claims off
  - no production collection ownership yet

### Phase 3: Git Collector Migration

- refactor the current Git ingestion path into a clean `collector-git`
  runtime contract
- keep the existing `StatefulSet` and PVC shape during the migration unless a
  later ADR changes that deployment model
- make bootstrap and continuous Git runs flow through coordinator-owned run
  state
- preserve the current reducer and query correctness model during migration
- retire `ingester` as the public runtime identity at the end of this phase

### Phase 4: Trigger Intake

- add webhook normalization into coordinator-owned run requests
- unify schedule, bootstrap, webhook, and replay semantics
- ensure all trigger paths produce the same durable run and completeness model
- add signature verification, replay protection, idempotent deduplication, and
  bounded back-pressure behavior for webhook floods

### Phase 4a: Concurrency Gate

- implement coordinator production claim ownership only under the concurrency
  contract defined in
  `2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`

### Phase 5: New Collector Families

- add `collector-terraform-state`
- add `collector-aws-cloud`
- reuse the same coordinator, claim, telemetry, and reducer substrate

### Phase 6: Deployment Cutover

- update Compose to reflect the new coordinator-driven workflow model
- update Helm and ArgoCD deployment shapes for the new Go runtimes
- keep `ops-prod` on the stable deployment until the new runtime contract is
  validated
- use `ops-qa` as the rollout environment for the new Go deployment shape once
  the chart and runtime work is ready

---

## Consequences

### Positive

- the platform gets one explicit orchestration model instead of collector- or
  trigger-specific control paths
- future collector families fit into a stable control-plane shape
- operators gain a single place to inspect run state and completeness
- the reducer stays source-neutral and authoritative
- the Git collector can stop serving as the implicit template for every future
  source family

### Negative

- this introduces a real control-plane subsystem, not just a rename
- the migration requires new Postgres state, new runtime docs, and deployment
  changes
- Compose and Helm contracts will need coordinated updates rather than small
  template edits

### Risks

- a weak claim model could create duplicate collection or starvation under
  contention
- an under-instrumented coordinator would become an opaque failure domain
- a partial migration could leave bootstrap, schedule, and webhook paths using
  different correctness models

These risks are acceptable only if the rollout keeps one invariant above all
others:

- there must be exactly one durable orchestration truth path at the end of the
  migration

---

## Downstream Gating — Remaining Coordinator Work

As of 2026-04-20 the coordinator slice has generalized workflow completeness
from a Git-only phase map to a typed `(collector_kind, keyspace, phase)`
contract, removed the hardcoded `code_entities_uid` assumption, and made
reconciliation transactional. Per-collector downstream convergence checkpoints
and first-class gating of second-pass reducer domains
(`deployment_mapping`, `workload_materialization`) follow the registry
shape frozen in §8 of the consumer contract ADR
(`2026-04-20-multi-source-reducer-and-consumer-contract.md`, **Accepted
2026-04-20**).

Rationale: the set of first-class gates is determined by the queries
consumers must answer, not by intuition about which phases feel important.
The consumer contract enumerates the queries, derives which phases must
gate which answers, and back-propagates the registry shape and the
reducer publication list.

With that ADR accepted, codex may resume extending the registry and
publishing second-pass phases (`cross_source_anchor_ready`,
`deployment_mapping`, `workload_materialization`) against the §8 shape
and the per-collector phase bundles enumerated in §6 of the consumer
contract.

---

## Recommendation

The platform should not make `bootstrap-index` the permanent workflow
coordinator service.

It should introduce a new `workflow-coordinator` runtime, keep collectors
source-local and separate, preserve one shared reducer, and treat bootstrap as
an action that flows through the same durable control-plane contract as every
other trigger.

That is the architecture most aligned with the current Go runtime direction,
the multi-source collector roadmap, and the platform’s accuracy-first
operational model.

---

## Appendix: Stable Implementation Workstreams

The work should be split into bounded, relatively stable workstreams. Detailed
task assignment and sprint reshuffling should live in the follow-on
implementation plan, not in this ADR.

### Chunk A: ADR And Runtime Contract

**Primary owner:** Codex

**Scope:**

- runtime docs
- naming contract
- coordinator responsibilities
- control-plane invariants

**Likely files:**

- `docs/docs/adrs/2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md`
- `docs/docs/deployment/service-runtimes.md`
- `docs/docs/reference/service-workflows.md`
- `docs/docs/reference/source-layout.md`

**Subagent split:**

- subagent 1: doc contract consistency
- subagent 2: runtime naming and admin-surface impact

### Chunk B: Durable Coordinator Substrate

**Primary owner:** Claude

**Scope:**

- Postgres control-plane schema
- run requests
- claims or leases
- completeness state

**Likely files:**

- `go/internal/storage/postgres/*`
- `go/internal/status/*`
- `go/internal/runtime/*`
- new `go/internal/workflow/*`

**Subagent split:**

- subagent 1: schema and migrations
- subagent 2: claim model and concurrency review
- subagent 3: telemetry and admin-status surfacing

### Chunk C: Git Collector Migration To Coordinator Claims

**Primary owner:** Codex

**Scope:**

- current Git collector run loop
- coordinator-triggered bootstrap and continuous runs
- remove hidden Git-only orchestration assumptions

**Likely files:**

- `go/cmd/collector-git/*`
- `go/internal/collector/*`
- `go/cmd/bootstrap-index/*`
- `go/internal/app/*`

**Subagent split:**

- subagent 1: bootstrap wrapper migration
- subagent 2: collector claim/ack flow
- subagent 3: local Compose and fixture validation

### Chunk D: Trigger Intake And Webhook Path

**Primary owner:** Claude

**Scope:**

- normalized trigger intake
- webhook to run-request conversion
- shared schedule/bootstrap/webhook semantics

**Likely files:**

- new `go/cmd/webhook-ingress/*`
- new `go/internal/workflow/triggers/*`
- runtime and API/admin status integration

**Subagent split:**

- subagent 1: webhook normalization
- subagent 2: trigger schema and validation
- subagent 3: auth and failure classification review

### Chunk E: Deployment Contract And `ops-qa` Rollout Path

**Primary owner:** Codex

**Scope:**

- Compose shape
- Helm runtime contract
- Argo/overlay rollout path for new Go services

**Likely files:**

- `docker-compose.yaml`
- `docker-compose.neo4j.yml`
- `docs/docs/deployment/docker-compose.md`
- private IaC deployment repo overlays and chart values after this repo lands

**Subagent split:**

- subagent 1: Compose runtime parity
- subagent 2: Helm values and runtime commands
- subagent 3: rollout/runbook validation

### Chunk F: New Collector Families

**Primary owner:** Shared after coordinator lands

**Scope:**

- `collector-terraform-state`
- `collector-aws-cloud`
- collector instance config expansion

**Subagent split:**

- one subagent per collector family for source-model research
- one shared reviewer subagent for reducer/correlation contract compliance
