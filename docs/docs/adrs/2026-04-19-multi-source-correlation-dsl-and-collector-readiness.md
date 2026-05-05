# ADR: Multi-Source Correlation DSL And Collector Readiness

**Date:** 2026-04-19
**Status:** In Progress
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

---

## Status Review (2026-05-03)

**Current disposition:** In progress.

The constrained DSL direction is already in the tree as code scaffolding and
first-party rule packs. The repo has a deterministic evaluator, a supported
rule schema, and packs for Dockerfile, Docker Compose, GitHub Actions, Jenkins,
Helm, ArgoCD, Kustomize, Terraform, Terragrunt, Ansible, and CloudFormation.

**Remaining work:** finish collector readiness and true multi-source
correlation. Current evidence proves the DSL/rule-pack substrate, but the real
AWS scanner, Terraform-state scanner, webhook freshness ingress, and full
cloud/runtime cross-source joins are still open.

The `ScopeKind` and `CollectorKind` enums already carry the future source
boundaries, but those values are still contracts without the scanner runtimes
behind them.

## Context

Eshu is about to expand beyond Git-only ingestion. The next
major roadmap slices include:

1. AWS cloud scanner
2. Terraform state file scanner
3. Webhook-driven freshness for GitHub Actions changes
4. Additional CI/CD and GitOps tool families such as FluxCD, GitLab, and GoCD

The current Go data plane already has the right high-level platform rule:

- collectors observe and normalize source truth
- cross-source correlation belongs to reducers
- canonical graph truth should not be written directly from collectors

Those rules are documented in the collector authoring guide and service runtime
documentation. The reducer contract also already exposes cross-source-looking
domains such as:

- `workload_identity`
- `cloud_asset_resolution`
- `deployment_mapping`
- `workload_materialization`

However, the current implementation is still uneven.

### What Is Already Strong

- The parser/collector boundary is generic and modular.
- Fact envelopes are source-neutral enough to carry multiple source systems.
- Relationship extraction already preserves typed evidence and provenance.
- The runtime, queue, and reducer substrate are designed to support more than
  one collector family.

### What Is Not Ready Yet

- `ScopeKind` and `CollectorKind` are still effectively Git-only.
- The workload materialization path is still repo-centric and file-fact-centric.
- Adding a new tooling family often requires touching multiple layers:
  parser extraction, evidence extraction, reducer heuristics, and query
  synthesis.
- Repo-level workload candidates are too coarse for:
  - monolith repos with multiple deployable units
  - repos with both production and utility Dockerfiles
  - deployment repos that reference services they do not own
  - cross-source joins between Git intent and cloud-observed runtime truth

This becomes a strategic problem as soon as AWS and Terraform state arrive.
Those sources are not just "more files to parse." They are separate source
truth families that must be correlated with Git, CI/CD, IaC config, and
runtime observations.

### Governing Principle

This ADR adopts the platform priority order explicitly:

- **Accuracy first**: canonical service and cloud truth must be formed from
  converging evidence, not shallow artifact presence.
- **Performance second**: the correlation model must remain bounded and
  predictable as sources and tooling families grow.
- **Reliability third**: adding a new collector or tool family must extend a
  stable contract rather than rewriting graph semantics each time.

---

## Problem Statement

The platform needs a source-neutral way to answer questions such as:

- What deployable units exist?
- Which source repository owns each unit?
- Which build or delivery pipeline produces it?
- Which IaC or GitOps configuration deploys it?
- Which runtime platform or cloud asset proves where it runs?
- Which source system should win when Git intent and observed cloud truth
  disagree?

The current implementation does not model that question directly. Instead, it
mixes:

- parser-specific extraction
- evidence-family logic
- repo-scoped workload admission
- query-time repair and enrichment

That approach will not scale cleanly across:

- Git collectors
- AWS cloud scanners
- Terraform state scanners
- event-driven freshness triggers
- new CI/CD and GitOps families

### Recent Evidence That Sharpens This ADR

Recent local mixed-delivery corpus testing reinforced four constraints that
this ADR now makes explicit:

- repository-level evidence can be real and useful without being strong enough
  to admit a canonical workload
- canonical workload admission can succeed while runtime-instance and
  platform-placement materialization still correctly remain empty
- shared deployment repositories can flood a service trace with unrelated
  config and controller provenance when joins happen at repository scope
  instead of deployable-unit scope
- verification harnesses need a stable repository-selection contract rather
  than accidental assumptions about workspace layout

Those findings narrow the scope of this ADR. The reducer and DSL work must
solve candidate admission quality first. Query/story enrichment can only
become simpler after those reducer decisions are trustworthy.

---

## Decision

This ADR refines and partially supersedes the **extension-mechanism** portion
of `2026-04-19-deployable-unit-correlation-and-materialization-framework.md`.
That earlier ADR remains correct about deployable-unit goals, reducer-owned
truth, and the need for bounded, explainable correlation. This ADR changes the
recommended extension seam from "typed Go rules only" to "a constrained DSL
compiled into typed Go structs at the correlation layer."

### Adopt A Constrained Correlation DSL For Multi-Source Truth

Eshu should introduce a **constrained, typed correlation DSL**
for deployable-unit and cloud-asset correlation.

This DSL should not replace collectors, parsers, reducers, or canonical graph
writers. It should live at the **evidence-correlation layer** and be used by
the reducer to turn normalized facts from multiple source systems into
canonical truth candidates.

### Keep One Reducer Substrate

The platform should keep a single reducer architecture and codebase.

It should **not** create a second reducer implementation for AWS or cloud
correlation. Instead, it should:

- expand the reducer domain model
- add source-neutral correlation stages
- allow separate worker pools or deployments by domain if operational
  isolation becomes necessary

If different scaling or retry behavior is needed later, the platform may run
multiple reducer services that claim different domains from the same durable
queue. That is a runtime topology choice, not a new ownership boundary.

### Treat Collectors As Source Observers, Not Canonical Correlators

Future collectors such as AWS and Terraform state should:

- observe source truth
- assign scopes and generations
- emit typed facts
- never own canonical graph correlation

The reducer remains the home for:

- cross-source correlation
- conflict resolution
- admission into canonical truth
- materialization of canonical graph outputs

---

## Why A DSL Is The Right Next Step

### Why The Previous Typed-Go-Only Position Is No Longer Enough

Typed Go remains the correct implementation language for the platform runtime,
collectors, reducers, and writers. However, using only hard-coded Go branches
for correlation semantics will become increasingly cumbersome as we add:

- AWS observed runtime evidence
- Terraform state-derived concrete resource identity
- new CI/CD tool grammars
- new GitOps controller grammars

The platform needs a way to express repeatable correlation logic such as:

- image repository joins
- service-name joins
- release-name joins
- entrypoint alignment
- deploy-source repo joins
- platform and environment identity rules

Those are not arbitrary programming problems. They are **declarative evidence
correlation rules**.

### Why A Free-Form Rules Engine Is Still The Wrong Move

The platform should avoid an unrestricted or Turing-complete rules language.
That would make performance, safety, testing, and operator explanations harder.

The DSL should be:

- schema-validated
- declarative
- bounded
- explainable
- compiled into typed Go structs

This keeps extension ergonomic for open source contributors without turning the
correlation layer into a second programming runtime.

### Why First-Party Rule Packs Matter

The DSL should not launch as an empty extension point that only benefits future
tools. It should ship with **first-party rule packs for every evidence family
already supported today**.

That does two things:

- proves the DSL is strong enough to express real platform semantics
- gives the open source community concrete examples instead of abstract hooks

The initial release should include first-party rule coverage for the currently
supported families:

- Dockerfile
- Docker Compose
- GitHub Actions
- Jenkins
- Helm
- ArgoCD
- Kustomize
- Terraform config
- Terragrunt
- Ansible
- existing runtime and platform mapping families already surfaced by the Go
  data plane

This ADR treats first-party coverage as part of the design, not optional
follow-up polish.

---

## Architecture

The target architecture should be split into five layers.

### 1. Source Collectors

Examples:

- Git collector
- AWS cloud scanner
- Terraform state scanner
- webhook-driven event ingress

Ownership:

- source observation
- scope assignment
- generation assignment
- typed fact emission

Collectors do not decide canonical relationships or canonical workloads.

### 2. Source Normalizers And Parsers

Examples:

- Dockerfile parser
- Jenkins parser
- GitHub Actions parser
- ArgoCD parser
- Helm and Kustomize parser
- Terraform config parser
- Terraform state normalizer
- AWS resource normalizers

Ownership:

- convert source-native artifacts into stable typed fact payloads
- preserve source-local semantics and provenance
- avoid cross-source joins or canonical truth decisions

### 3. Correlation Engine

This is the new center of gravity.

Ownership:

- ingest normalized facts from multiple source systems
- apply correlation DSL rules
- build deployable-unit candidates
- build cloud-asset identity candidates
- score, reject, merge, and explain candidates

This is where the DSL lives.

### Canonical Levels Of Truth

The platform should model five distinct levels of truth and keep their
boundaries explicit:

1. **Evidence atoms**
   - typed, source-local observed facts with provenance
2. **Deployable-unit candidates**
   - bounded candidate groups formed by explicit linking factors
3. **Canonical workloads**
   - admitted deployable units with stable identity and explanation
4. **Materialized runtime instances**
   - environment- and platform-specific realizations of canonical workloads
5. **Query and story narratives**
   - operator-facing summaries built from already-decided canonical state

The DSL is responsible for levels 2 and 3.

Materialization is responsible for level 4.

Read paths are responsible for level 5.

This boundary matters because repository evidence can exist without workload
admission, and workload admission can exist without any observed runtime
instances yet.

### Evidence Engineering Requirements

The correlation engine must be designed as an **evidence engineering** system,
not a convenience layer for fuzzy name matching.

That means:

- every canonical correlation must be backed by named evidence facts
- evidence must retain provenance, rationale, and confidence
- conflicting evidence must remain explainable instead of being silently
  overwritten
- admission rules must state why a candidate was accepted or rejected
- low-confidence matches must be observable and testable
- rule output must be stable enough to compare across corpora and parity runs
- candidate groups must remain bounded and deployable-unit-scoped rather than
  expanding to "everything in a shared deployment repo"
- negative evidence and exclusion conditions must be expressible so utility
  artifacts, unrelated overlays, and broad shared repos do not silently pollute
  canonical candidates

The platform should prefer explicit linking factors over fuzzy matching:

- image repository and image name
- source repository URL
- pipeline artifact identity
- module `app_name`
- release name
- chart image values and chart name
- GitOps application source path
- Terraform state object identity
- entrypoint
- platform-native runtime identifiers
- hostnames and environment overlays when they are explicit and source-backed

The platform should also define a strong-key precedence ladder for container
flows:

1. immutable artifact or image identity
2. source repository and workflow provenance
3. deploy-config keys such as `app_name`, chart image values, or GitOps source
   path
4. release or service names
5. bounded fuzzy fallback

Fuzzy matching must never be sufficient for canonical admission by itself.

Fuzzy matching may still exist as bounded fallback behavior, but it must never
be the primary source of truth when stronger evidence is available.

### 4. Canonical Materialization

Ownership:

- write already-decided canonical rows to graph and content stores
- materialize workloads, instances, deployment sources, runtime platforms, and
  cloud assets
- avoid re-deciding source correlation at write time

Canonical rows should declare both object kind and relationship class.

At minimum, correlated edges should carry a traversal-aware class such as:

- `ownership`
- `build`
- `deployment`
- `runtime_observation`
- `supporting_config`

Deployment-chain and workload-instance traversal should include only allowed
edge classes. Supporting config should stay explainable without becoming a
primary deployment hop.

### 5. Query And Story Enrichment

Ownership:

- explain evidence and provenance
- summarize and shape operator-facing answers
- surface partial-coverage notes
- avoid inventing canonical truth that the reducer has not decided

---

## DSL Scope

The DSL should only express **correlation and admission semantics**.

It should not express:

- collector scheduling
- queue semantics
- database writes
- graph Cypher
- parser discovery
- runtime concurrency
- HTTP or MCP answer shaping

It should also stay out of:

- service-story narrative synthesis
- query-time consumer ranking
- topology prose generation
- `resolve_entity` read-path scoring

Those read paths should consume canonical entities, edges, evidence, and
confidence. They may summarize that output, but they may not repair missing
service truth.

### Correlation Keys And Boundaries

The DSL should operate on a small, explicit catalog of correlation keys rather
than open-ended text matching.

The first catalog should cover:

- deployable-unit name
- owning repository identity
- deploying repository identity
- config repository identity
- container image repository and image name
- pipeline artifact identity
- chart or release identity
- GitOps application source path
- Terraform module identity and selected variables such as `app_name`
- environment identity
- platform-native runtime identity such as ECS service name, EKS application
  source, ALB hostname, or Cloud Map service name

Rule packs should declare which keys they emit and which keys they consume.
The engine should reject or down-rank joins that expand across many unrelated
deployable units without an explicit narrowing key.

### Minimum Rule Types

The first DSL version should support only a small set of rule primitives:

#### `extract_key`

Extract correlation keys from typed facts.

Examples:

- service name
- image repository
- image tag
- release name
- repo slug
- entrypoint
- cluster name
- ECS service name
- ALB hostname
- Cloud Map service name

#### `match`

Declare allowed joins between fact families.

Examples:

- Helm image repository matches ECR repository name
- Terraform module `app_name` matches package or service name
- ECS service name matches deployable unit name
- Argo release name matches service name

#### `admit`

Define the minimum evidence required to create a canonical candidate.

Examples:

- require one build/runtime signal plus one deployment/runtime signal
- require stronger evidence for cloud-only candidate admission
- reject utility-only controller evidence from becoming a service
- allow repository-level provenance to remain queryable without forcing
  canonical workload admission

#### `derive`

Compute canonical fields from matched evidence.

Examples:

- workload name
- owning repo
- deploying repo
- config repo
- environment
- deployment source repo
- runtime platform kind
- cloud asset canonical key

#### `explain`

Generate provenance and operator-facing evidence reasons.

Examples:

- "Helm overlay points at ECR repository X for release Y"
- "AWS ECS service name and Terraform module name converge on api-node-chat"

### Admission Contracts By Canonical Object

The DSL runtime should support explicit minimum evidence contracts for
different canonical objects:

- **workload**
  - converging code/build evidence plus deploy/config evidence
- **runtime instance**
  - explicit environment-scoped deploy target or observed runtime/state
- **platform placement**
  - explicit platform signal, not just a referencing repository

That allows the system to say:

- repository evidence exists but workload admission failed
- workload admission succeeded but no runtime instance has been observed
- runtime instance exists and is linked to workload X

### First Vertical Slice

The first full DSL-backed slice should be **containers** because it exercises
the most important cross-source identity chain:

- code repo
- Dockerfile runtime contract
- CI build/publish config
- Helm or Argo deploy config
- Terraform config
- Terraform state
- AWS ECS/ECR/ALB observed state

This slice will prove whether the architecture works for both:

- legacy ECS delivery
- GitOps and EKS delivery

The first container slice should be validated with at least two generic
fixture families:

- reusable-workflow plus GitOps/deployment-repo plus Terraform config
- Jenkins plus Terraform plus ECS-style deployment config

The slice is not complete if only one delivery family admits correctly.

### Predefined Rule Coverage At Launch

The first implementation milestone should include predefined rule coverage for
all currently supported evidence families, even if some families initially
support fewer primitives than others.

At minimum:

- container-focused families should support full extraction, match, admit,
  derive, and explain coverage
- controller and workflow families should support correlation and explanation
  coverage
- config-discovery families should support explicit non-deploying relationships
  such as config discovery and module usage without pretending they are
  services
- shared deployment repositories should contribute deployment evidence without
  inheriting service ownership or flooding candidate-scoped provenance

This ensures the DSL is immediately useful for both platform parity and open
source documentation.

---

## Reducer Changes

### Keep The Existing Reducer Runtime

The reducer runtime, intent queue, status, readiness gating, and retry model
should stay shared.

### Add Or Refactor Toward Source-Neutral Domains

The target reducer domain shape should be:

- `deployable_unit_correlation`
- `cloud_asset_resolution`
- `deployment_mapping`
- `workload_materialization`

Expected ownership:

- `deployable_unit_correlation`
  - correlate Git, CI/CD, IaC, state, and cloud evidence into deployable-unit
    candidates
- `cloud_asset_resolution`
  - canonicalize cloud resources and state-backed cloud identities
- `deployment_mapping`
  - connect deployable units to runtime platforms and cloud assets
- `workload_materialization`
  - write canonical workload graph rows from already-correlated inputs

### What Must Stop

`workload_materialization` must stop being the long-term home for repo-scoped
candidate synthesis directly from raw file facts.

Projection should become boring:

- no source-family-specific admission logic
- no repo-local parser heuristics as canonical workload truth
- no query-layer compensation for missing reducer decisions

---

## Scope And Identity Changes

The scope model must become truly multi-source before the AWS collector lands.

The platform should add new stable `ScopeKind` and `CollectorKind` values for
families such as:

- repository
- account
- region
- cluster
- state_snapshot
- event_trigger

And collector kinds such as:

- git
- aws
- terraform_state
- webhook

The exact list may evolve, but the model must stop assuming repository plus
git are the only first-class boundaries.

---

## Open Source Extension Model

The design should optimize for clear extension seams.

### Directory Structure

The recommended new package layout is:

- `go/internal/correlation/`
- `go/internal/correlation/model/`
- `go/internal/correlation/engine/`
- `go/internal/correlation/rules/`
- `go/internal/correlation/admission/`
- `go/internal/correlation/explain/`

Rule families should be organized by evidence family, not by collector:

- `containers/`
- `github_actions/`
- `jenkins/`
- `helm/`
- `argocd/`
- `fluxcd/`
- `terraform_config/`
- `terraform_state/`
- `aws_ecs/`
- `aws_ecr/`
- `aws_alb/`
- `gitlab_ci/`
- `gocd/`

### Contributor Workflow

To add support for a new tool family, an open source contributor should
usually need to do only three things:

1. Add or extend source normalization if the source is not already structured
2. Add correlation rules for the new evidence family
3. Add fixture-backed tests and documentation for supported semantics

They should not need to rewrite reducer graph semantics each time.

### Documentation Requirement

The project should document:

- the DSL schema
- the supported rule primitives
- the first-party rule packs shipped by the platform
- extension examples for one CI/CD family, one GitOps family, and one cloud
  family
- guidance on when to add a parser, when to add a normalizer, and when to add
  only a rule pack

The goal is that an external contributor can follow the docs and add a new
tool family without reverse-engineering reducer internals.

---

## Performance, Concurrency, And Instrumentation Requirements

The DSL and correlation engine must preserve the platform's existing
performance and reliability discipline.

### Performance

- rule evaluation must be bounded and data-driven, not open-ended backtracking
- correlation should prefer indexed keys and normalized joins over repeated
  corpus-wide scans
- materialization must continue to operate in batched writes
- the platform should avoid query-time repair work that can be moved into
  reducer-backed correlation

### Concurrency

- correlation work must respect existing queue, generation, and readiness
  boundaries
- cloud and Git facts must not race to create conflicting canonical truth for
  the same candidate without a defined conflict path
- separate worker pools may be introduced by reducer domain when contention,
  retry behavior, or source latency differs materially
- operational isolation is allowed at the deployment level, but correlation
  ownership remains within the shared reducer architecture

### Instrumentation

- rule evaluation should emit spans and metrics by rule pack and rule family
- candidate admission and rejection counts should be visible
- conflict, tie-break, and low-confidence paths should be measurable
- operator-visible evidence summaries should be generated from the same
  correlation decisions used to write canonical truth

The system should remain diagnosable at 3 AM from traces, metrics, and logs
without requiring code archaeology.

---

## Consequences

### Positive

- The platform becomes ready for multi-source correlation instead of only
  repo-local inference.
- AWS and Terraform state can land on stable boundaries.
- More CI/CD and GitOps tool families can be added without multiplying
  reducer-specific heuristics.
- Canonical graph truth becomes more explainable and more testable.
- Open source extension becomes more ergonomic and less invasive.

### Costs

- This adds a new architectural layer that must be designed carefully.
- Existing workload materialization logic will need to be refactored.
- Some current query-time enrichment may need to move down into reducer-backed
  correlation over time.
- Initial rule-family coverage must be intentionally phased.

### Risks If We Do Nothing

- every new tool family keeps changing canonical truth in ad hoc ways
- Git and cloud sources drift into separate truth systems
- repo-scoped workload candidates keep producing false positives and misses
- query-time repair keeps growing instead of shrinking
- parity and operator trust become harder to maintain

---

## Implementation Phases

### Phase 1: Prepare The Platform Boundary

- expand scope and collector identity beyond Git-only enums
- define the correlation package and contracts
- document the source-neutral fact and candidate model
- define the DSL schema and observability contract
- inventory every currently supported evidence family and map each one to a
  first-party rule pack

### Phase 2: Container Vertical Slice

- implement first DSL schema and runtime
- support Git + CI/CD + IaC config correlation for containers
- add Terraform state joins where available
- keep workload materialization writing only correlated outputs
- use this slice as the reference implementation for open source extension docs
- prove both reusable-workflow and Jenkins delivery families on generic
  fixtures
- prove that shared deployment repos do not flood a service with unrelated
  config provenance

### Phase 3: Cloud Observation Joins

- onboard AWS ECS, ECR, ALB, and related observed facts
- correlate observed runtime truth with deployable-unit candidates
- strengthen `cloud_asset_resolution` and `deployment_mapping`

### Phase 4: Tooling Expansion

- add FluxCD
- add GitLab
- add GoCD
- add additional state and runtime evidence families as needed

### Phase 5: Query Simplification

- shrink query-time repair paths where reducer-backed correlation now exists
- keep query enrichment for explanation and drill-down, not truth repair
- feed confidence and explanation signals into read paths without moving
  admission logic back into query synthesis

---

## Decision Summary

Eshu should prepare for AWS, Terraform state, webhook-driven
freshness, and broader CI/CD support by introducing a constrained
multi-source correlation DSL and keeping one shared reducer substrate.

The reducer should evolve into a source-neutral correlation and
materialization engine. Collectors stay source observers. Parsers and
normalizers stay source-local. Canonical graph truth remains reducer-owned.

This is the next major architectural step required before the platform adds
non-Git collectors at scale.
