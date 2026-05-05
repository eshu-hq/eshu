# Future AWS Collector Design Constraints

Date: 2026-04-17
Status: working note, not a committed architecture decision
Scope: future AWS collector milestone

## Purpose

This note captures the design constraints for a future AWS collector family so
the work starts from the current platform boundaries instead of from Git-era
assumptions.

This is not an implementation plan. It is a guardrail document for future
design and execution.

## Core Position

The future AWS collector should be its own service.

Do not bloat the existing Git ingester into a multi-source collector process.
The Git collector is the first collector family, not the forever-host for all
collector logic.

The platform direction should remain:

- one collector family owns one source-truth boundary
- each collector family can be operated, tuned, and rolled out independently
- projector and reducer contracts remain shared across collector families

## Naming And Ownership

Use these terms consistently:

- `collector`: a source-truth service family such as Git or AWS
- `ingester`: the runtime pattern for a collector service that observes source
  truth, assigns scope and generation, and emits durable facts
- `reducer`: the shared runtime that drains queued work and owns shared truth

The future AWS milestone should avoid collapsing those terms together.

Good framing:

- Git is one collector family
- AWS will be another collector family
- both feed the same facts-first data plane

Bad framing:

- the existing indexer service should absorb AWS collection behavior
- AWS should introduce a second reconciliation model

## Service Boundary Rules

The future AWS collector service should own only:

- source observation against AWS APIs and related source truth
- bounded scope assignment
- generation assignment
- typed fact emission
- runtime-local health, status, metrics, and retry behavior

It should not own:

- canonical graph correlation
- shared reconciliation policy
- cross-source truth finalization
- direct canonical edge writes as a normal path
- collector-specific repair semantics outside the shared recovery model

## Scope And Generation Rules

Before implementation starts, the AWS collector design must define:

1. Source truth
   Which AWS systems are authoritative for the collector slice:
   live AWS APIs, Terraform state, CloudFormation, or a narrower subset.
2. Scope model
   What one bounded ingestion unit actually is:
   account, account-region, cluster, workspace, or another durable shard.
3. Generation model
   What makes one snapshot authoritative and replaceable for that scope.
4. Acceptance model
   Which bounded acceptance unit will be used downstream in reducer logic.

The acceptance unit must not be assumed to equal `repository_id`.

That assumption is survivable in the current Git collector but becomes a trap
for AWS-shaped sources.

## Facts-First Rule

The AWS collector should follow the same path as the current Git collector:

1. observe source truth
2. assign scope
3. assign generation
4. emit typed facts durably
5. let projector and reducer process downstream consequences

Do not skip from collector observation straight to canonical graph mutation.

## Shared Reconciliation Rule

The shared reducer remains the only place that should own:

- cross-domain materialization
- shared projection intents
- canonical write coordination
- replay and repair logic

If the AWS collector later needs lower latency for some shared follow-up path,
that must be treated as an optimization on top of the reducer contract, not a
replacement for it.

See the companion working note:

- `docs/internal/2026-04-17-inline-shared-followup-investigation.md`

That note explains why dormant inline shared-drain logic should not be reused
as-is for future collector work.

## Runtime Contract

The AWS collector service should match the same runtime contract used by other
Go services:

- CLI entrypoint for local replay and controlled runs
- `/healthz`
- `/readyz`
- `/admin/status` when HTTP admin is mounted
- `/metrics`
- structured JSON logs
- tunable worker, pool, and retry settings

It should be deployable and scalable independently from:

- Git collector runtime
- reducer
- API
- MCP server

## Telemetry And Operability Rules

The AWS collector design should specify, before implementation:

1. the top-level service name and runtime role
2. the scope identifier shape used in logs and metrics
3. the source run identifier shape
4. the backlog and retry signals operators will use
5. the pool and concurrency settings that must be configurable

Collector-specific telemetry is acceptable.
Collector-specific operator workflow is not.

Operators should still be able to reason about the AWS collector with the same
mental model they use for Git collector, reducer, and bootstrap flows.

## Data-Plane Compatibility Rules

The AWS collector must integrate with the shared platform contracts rather than
creating source-specific side channels.

Required characteristics:

- reusable fact envelopes
- projector compatibility where source-local projection is appropriate
- reducer compatibility where shared truth is required
- durable queue ownership for retries and replay
- stable correlation fields in logs, traces, and metrics

Avoid:

- source-specific post-commit repair hooks
- side-band freshness tracking outside scope/generation contracts
- collector-owned direct Neo4j writes in the normal path

## Future AWS-Specific Risks To Resolve In Design

The eventual AWS collector design should explicitly resolve:

1. Whether scope is account-wide or account-region scoped
2. Whether collection is pull-only from live AWS, or reconciled with IaC and
   state snapshots as separate evidence sources
3. How rate limiting and partial source failure affect generation acceptance
4. Whether one repository can map to multiple AWS acceptance units in one run
5. How live cloud evidence and IaC evidence are merged without creating a
   second correctness model

These are design inputs, not implementation details. They should be decided
before code structure is finalized.

## Inline Fast-Path Rule

Do not plan the AWS collector around inline shared followup.

Default assumption:

- the AWS collector emits durable work
- the reducer drains it

If a later latency target requires an inline fast path, that design must be
acceptance-key scoped and validated separately. It should start disabled and
prove itself as an optimization only.

## Verification Expectations

A future AWS collector milestone should not be considered ready without:

- unit tests for scope, generation, and fact identity
- fixture-backed or replay-backed integration tests
- reducer compatibility tests when shared truth changes
- telemetry verification for operator-critical signals
- a local validation story that does not depend on production credentials
- documentation updates for runtime ownership, testing, and observability

## Recommended Future Sequence

When the AWS collector milestone begins, use this order:

1. define source truth and bounded scope
2. define generation and acceptance-unit semantics
3. define typed fact families
4. define service/runtime boundary
5. define reducer/projector integration points
6. define telemetry and operator contract
7. only then write implementation plans

## Bottom Line

The future AWS collector should be a new collector service that plugs into the
existing facts-first platform. It should not be implemented as an expansion of
the current Git collector service, and it should not rely on Git-shaped
assumptions such as `acceptance_unit_id == repository_id`.
