# ADR: Deployable-Unit Correlation And Materialization Framework

**Date:** 2026-04-19
**Status:** Accepted with follow-up; extension seam partially superseded
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

---

## Status Review (2026-05-03)

**Current disposition:** Accepted with follow-up.

The deployable-unit framework exists in the reducer. It evaluates
evidence-backed candidates, splits multiple Dockerfiles conservatively, admits
resolved deployment evidence, handles Jenkins-backed service candidates, and
materializes workloads, instances, deployment sources, runtime platforms, and
endpoints.

**Remaining work:** the extension-mechanism portion is partially superseded by
`2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`. Full
admission and materialization across multi-source runtime evidence is still
follow-up work.

## Context

The Go data plane now extracts a broad and increasingly useful set of
relationship evidence across source code, CI/CD, deployment config, and IaC.
That extraction work is valuable, but the current workload materialization path
is still too repo-centric to scale cleanly as we add more delivery families and
runtime targets.

Today the reducer builds a single `WorkloadCandidate` per repository by
aggregating repo-local signals such as:

- `k8s_resources`
- `argocd_applications`
- `argocd_applicationsets`
- Dockerfile runtime hints
- Docker Compose presence
- Jenkinsfile presence
- GitHub Actions workflow presence

That model is no longer sufficient for the corpus we already support, and it
will become more brittle as the platform adds support for more CI/CD and IaC
tools.

### Why The Current Model Breaks Down

The current repo-level synthesis collapses several materially different cases
into the same bucket:

1. A service repository with one production Dockerfile
2. A monolith repository with multiple Dockerfiles for distinct services
3. A repository with a production Dockerfile plus a testing or utility
   Dockerfile
4. A repository containing one or more Helm charts for other services
5. A deployment/config repository whose strongest truth is orchestration rather
   than service ownership

In modern delivery systems, **artifact presence alone is not enough**. The
important truth is usually established by a chain of linked evidence:

- the code repo defines a runnable unit
- CI builds or publishes an image
- a deploy/config layer references that image or source repo
- a platform/runtime layer proves where that unit runs

This is already reflected in parts of the platform:

- Docker Compose build contexts and image refs emit `DEPLOYS_FROM`
- Dockerfile source labels emit `DEPLOYS_FROM`
- GitHub Actions preserves reusable workflow refs, checkout repos, workflow
  input repos, and delivery command families
- Terraform and Terragrunt surface repo-bearing config and module signals
- ArgoCD, Helm, and Kustomize already participate in deploy-source reasoning

However, those primitives are still synthesized into workloads using fixed,
repo-level logic rather than an extensible correlation model.

### Governing Principle

This ADR adopts the following principle:

- **Accuracy first**: a deployable service or job must be materialized from
  converging evidence, not from shallow artifact presence.
- **Performance second**: the framework must stay bounded and predictable as the
  corpus grows.
- **Reliability third**: the same reasoning path should support new tool
  families without multiplying special-case logic in reducers and query helpers.

---

## Problem Statement

The platform needs a framework that can answer this question truthfully and
repeatably:

> Which deployable units exist, what code repo owns them, which CI/CD path
> builds them, which deployment config references them, and which runtime or
> platform proves where they run?

The current reducer does not model this directly. It models repository-level
workload candidates. That causes three strategic problems:

### 1. Repo-Level Workload Candidates Are Too Coarse

Some repositories contain:

- multiple services
- service plus worker images
- production and testing Dockerfiles
- Helm charts for many services
- orchestration-only deployment content

One repo-level candidate cannot represent those cases accurately.

### 2. Tool Support Expands Faster Than Repo Heuristics

As support grows across:

- Jenkins
- GitHub Actions
- ArgoCD
- Helm
- Kustomize
- Terraform
- Terragrunt
- Docker Compose
- Dockerfile
- CloudFormation
- Ansible
- ECS/ECR
- EKS

the current model requires more hard-coded precedence rules and more query-time
patching. That becomes cumbersome, difficult to validate, and hard to explain.

### 3. We Need Stable Linking Factors, Not More Parser-Specific Branches

The strongest deployable truth usually comes from shared identity hints such as:

- image repository or image name
- explicit source repo URL
- application or service name
- chart release name
- entrypoint and command alignment
- deploy-source repo references
- platform-specific runtime/service module inputs

Those linking factors are reusable across many tooling families. The system
should model them explicitly rather than forcing each tool family to invent its
own reducer behavior.

---

## Decision

### Adopt A Typed Deployable-Unit Correlation Framework

The platform should stop treating repo-level workload synthesis as the primary
abstraction and instead introduce a typed framework with four stages:

1. **Signal extraction**
2. **Deployable-unit correlation**
3. **Classification and admission**
4. **Materialization and explanation**

This framework should be implemented in typed Go, not as an unrestricted DSL or
generic rules engine.

### Why Typed Go Instead Of A Generic DSL

The platform needs:

- compile-time safety
- predictable performance
- traceable reasoning
- narrow extension seams
- strong tests
- operator-visible explanations

A free-form rules DSL would introduce unnecessary opacity too early. The first
version should be a typed registry of rule definitions implemented in Go, with
clear interfaces and explicit observability.

---

## Architecture

### Stage 1: Normalized Delivery Signal Extraction

All supported tooling families should emit normalized signal atoms into a shared
contract. Extractors may still be family-specific, but their output should be
portable.

Example signal fields:

- `signal_kind`
- `source_repo_id`
- `relative_path`
- `artifact_family`
- `subject_hint`
- `target_hint`
- `service_name`
- `image_ref`
- `image_repository`
- `chart_name`
- `release_name`
- `entrypoint`
- `controller_kind`
- `workflow_kind`
- `environment`
- `platform_kind`
- `confidence`
- `rationale`
- `provenance`

Examples of normalized signal kinds:

- `dockerfile_runtime`
- `dockerfile_source_label`
- `docker_compose_build_context`
- `docker_compose_image`
- `docker_compose_depends_on`
- `helm_values_image`
- `helm_chart_deploy_source`
- `kustomize_image_reference`
- `argocd_deploy_source`
- `argocd_discovery`
- `gha_reusable_workflow`
- `gha_checkout_repository`
- `gha_workflow_input_repository`
- `gha_run_delivery_command`
- `jenkins_pipeline_entrypoint`
- `jenkins_shared_library`
- `terraform_service_module`
- `terraform_app_repo`
- `terraform_app_name`
- `terraform_image_repository`
- `ecs_service_runtime`
- `ecr_repository_binding`

The framework must allow new families to add signal kinds without rewriting the
materializer.

### Stage 2: Deployable-Unit Correlation

Signals should be clustered into **deployable candidates** using shared linking
factors instead of repo-level aggregation alone.

Candidate correlation keys should prefer:

1. explicit image identity
2. explicit source-repo identity
3. explicit application/service identity
4. explicit deploy-source references
5. strong entrypoint/runtime alignment

Correlation should combine signals when they converge on the same deployable
unit.

Examples:

- Dockerfile + GitHub Actions image publish + Helm values image reference
  -> one service candidate
- Jenkins pipeline entrypoint + Dockerfile runtime + ArgoCD deploy source
  -> one service candidate
- Terraform ECS module inputs + ECR image repo + code repo Dockerfile
  -> one service candidate
- monolith repo with `api.Dockerfile` and `worker.Dockerfile`
  -> two deployable candidates, not one repo candidate

The correlation result should be a typed object such as:

```text
DeployableCandidate
  - candidate_id
  - source_repo_id
  - deployable_key
  - candidate_kind_hint
  - signals[]
  - correlation_reasons[]
  - dominant_links[]
  - raw_confidence
```

### Stage 3: Classification And Admission

Only after correlation should the platform classify a candidate as:

- `service`
- `job`
- `utility`
- `infrastructure`

Classification should use both positive and negative controls.

Positive examples:

- deployable runtime artifact + deploy-source evidence + runtime/platform
  evidence
- image ownership + CI build + deployment config reference
- scheduler/job semantics backed by runtime/deploy evidence

Negative controls:

- Jenkinsfile alone
- GHA workflow alone
- utility/test-only Dockerfile with no deploy-source or runtime linkage
- Helm repository with many charts but no strong service ownership signal
- config-only repo with deployment references but no owned runtime unit

Admission to canonical workload materialization should require:

1. allowed classification (`service` or `job`)
2. minimum confidence threshold
3. at least one strong linking factor
4. no disqualifying negative-control rule

### Stage 4: Materialization And Explanation

Accepted candidates should materialize into canonical graph nodes and read-model
surfaces with explanation-first metadata.

Each materialized workload should retain:

- materialization confidence
- dominant evidence sources
- correlation reasons
- matched delivery families
- deployment-source provenance
- runtime/platform provenance

Rejected candidates should also be observable through logs, metrics, and
structured debugging output.

---

## Rules Framework

### Recommendation: Typed Registry, Not Ad Hoc Switches

The correlation framework should be driven by a typed registry with explicit
rule categories:

- `SignalRules`
- `CorrelationRules`
- `ClassificationRules`
- `AdmissionPolicies`

Each rule should have:

- `Name`
- `AppliesTo`
- `Match`
- `Score`
- `Reason`
- `NegativeControls`

Example conceptual interface:

```text
type CorrelationRule interface {
    Name() string
    AppliesTo(candidate SignalSet) bool
    Evaluate(candidate SignalSet) RuleOutcome
}
```

This keeps the system extensible without introducing an opaque runtime rule
language.

### Why This Will Not Become Cumbersome

Adding a new CI/CD or IaC family should follow one repeatable path:

1. add a normalized signal extractor
2. register or extend correlation rules
3. add acceptance fixtures
4. validate explanation output

That is much safer than the current pattern of:

1. add parser output
2. add repo-level special casing
3. patch query enrichment
4. fix parity regressions later

---

## Invariants

The framework must preserve these invariants:

1. **Canonical relationships remain canonical**
   Correlation may consume canonical evidence and read-side signals, but it must
   not casually invent new canonical edges.

2. **Deployable-unit synthesis must be explainable**
   Every materialized workload must be traceable back to concrete evidence.

3. **Repo presence is never sufficient by itself**
   Artifact existence alone does not prove service ownership.

4. **Multiple deployable units per repo are allowed**
   The model must not assume one workload per repository.

5. **Infrastructure and config repos must remain first-class**
   Not every repository with strong deployment artifacts is a service repo.

6. **Performance must remain bounded**
   Correlation must operate on normalized signals and bounded joins, not on
   unconstrained global query-time scans.

---

## Observability Requirements

The framework should be observable by default.

At minimum it should emit:

- counters for candidates created, admitted, rejected
- counters by rule family and rule name
- counters by classification
- counters by rejection reason
- histograms for signals-per-candidate
- structured logs for low-confidence or rejected candidates
- trace attributes for dominant signal families and admission outcomes

Operators should be able to answer:

- why did this repo materialize a workload?
- why did this repo fail to materialize a workload?
- which rule dominated the decision?
- which signal family is causing false positives?

---

## Rollout Plan

### Phase 1: Framework Skeleton And Container-Centric Path

Introduce:

- normalized `DeliverySignal`
- typed `DeployableCandidate`
- correlation registry
- confidence/admission gate

Initial supported paths:

- Dockerfile
- Docker Compose
- GitHub Actions
- Jenkins
- ArgoCD deploy-source
- Helm image/value references
- Terraform app/image/runtime service module hints

Acceptance criteria:

- a Jenkins + Dockerfile + deploy-source service materializes through the new
  path
- Jenkins-only utility repos do not materialize
- Docker Compose image/build/dependency chains are preserved
- materialization logs show rule explanations

### Phase 2: Multi-Unit Repositories

Extend correlation to support:

- multiple Dockerfiles
- multiple image refs
- service + worker separation
- Helm/chart repos with mixed ownership patterns

Acceptance criteria:

- one monolith repo can yield multiple deployable candidates
- utility/test Dockerfiles do not automatically become services
- deployment path explanations remain distinct per candidate

### Phase 3: Runtime-Parity Expansion

Deepen platform/runtime closure for:

- ECS/ECR
- EKS/GitOps
- CloudFormation/SAM
- broader Terraform service modules

Acceptance criteria:

- code -> CI -> image -> deploy config -> runtime chain is visible for both ECS
  and EKS delivery families
- workload/service story surfaces can explain the chain without query-time
  guesswork

### Phase 4: Additional Tooling Families

Onboard future families by adding normalized signals and correlation rules,
without changing the core materialization flow.

Examples:

- additional CI/CD systems
- more IaC stacks
- registry-specific image metadata
- service mesh or ingress tooling

---

## Consequences

### Positive

- The platform gets a scalable framework instead of more reducer heuristics
- Multi-service monolith repos can be modeled truthfully
- New tooling families can plug into a stable synthesis path
- Workload materialization becomes more explainable
- Accuracy improves for code -> CI/CD -> IaC -> runtime investigations

### Negative

- The framework adds a new modeling layer between evidence extraction and
  materialization
- More explicit rule types mean more up-front design work
- Candidate clustering introduces more internal objects to test and observe

### Risks

- If the normalized signal contract is too shallow, new families will still need
  special handling
- If the correlation keys are too loose, false positives will remain
- If the correlation keys are too strict, real services will fail to materialize
- If the framework becomes query-time-only, performance and consistency will
  regress

---

## Explicit Non-Goals

This ADR does not propose:

- a free-form external rules DSL
- replacing the canonical relationship resolver
- unbounded global query-time correlation
- immediate closure of every delivery family in one change

This ADR defines the framework that future parity work should build on.

---

## Recommendation

Proceed with a typed deployable-unit correlation framework and use it as the new
foundation for workload materialization across CI/CD and IaC families.

The first implementation should prioritize:

1. normalized signal extraction
2. correlation registry
3. confidence-gated admission
4. container delivery paths across Jenkins, GitHub Actions, ArgoCD, Helm,
   Terraform, Dockerfile, and Docker Compose

This is the next major step because it converts the platform from
repo-heuristic workload synthesis into an extensible, evidence-backed
materialization architecture.
