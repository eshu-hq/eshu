# Multi-Source Correlation DSL And Collector Readiness Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prepare Eshu for multi-source correlation by introducing a constrained evidence-correlation DSL, first-party rule packs for currently supported tooling families, source-neutral reducer domains, and the container-first vertical slice needed before AWS cloud scanning and Terraform state scanning land.

**Architecture:** Keep collectors source-local and facts-first. Add a new correlation layer between normalization and canonical materialization so Git, Terraform config, Terraform state, and future AWS cloud facts all converge through the same deployable-unit and cloud-asset admission path. Keep one reducer substrate and evolve it with source-neutral domains rather than creating a second reducer implementation.

**Tech Stack:** Go, PostgreSQL, Neo4j, OpenTelemetry, MkDocs

---

## Non-Negotiable Guardrails

- Do not add company-specific service names, repositories, hostnames, account IDs, or cloud resource identifiers to tests. Use generic fixtures only.
- Do not let the DSL become a free-form interpreter. It must remain schema-validated, bounded, and compiled into typed Go structs.
- Do not move canonical graph writes into collectors. Cross-source truth remains reducer-owned.
- Do not keep workload admission repo-centric once the correlation layer exists. Repo-level heuristics should become compatibility inputs, not the source of truth.
- Do not add unbounded query-time scans as a substitute for reducer-backed correlation.
- Treat observability as part of the architecture: every new correlation stage must expose traces, metrics, and structured evidence summaries.
- Keep this plan scoped to reducer truth, provenance, deployment mapping, and materialization contracts. Service-story narrative synthesis, topology prose, consumer presentation, and read-path ranking remain separate follow-up integration work.
- Correlation verification must use generic multi-repo fixture corpora and explicit repository-selection rules. Do not rely on accidental workspace layout or nested fixture discovery behavior.

## Current System Seams

- Scope and generation identity:
  - `go/internal/scope/scope.go`
  - `go/internal/facts/models.go`
- Current reducer domain and runtime wiring:
  - `go/internal/reducer/intent.go`
  - `go/internal/reducer/domain.go`
  - `go/internal/reducer/defaults.go`
  - `go/internal/reducer/runtime.go`
  - `go/internal/reducer/service.go`
- Current workload candidate and materialization path:
  - `go/internal/reducer/candidate_loader.go`
  - `go/internal/reducer/workload_deployment_sources.go`
  - `go/internal/reducer/projection.go`
  - `go/internal/reducer/workload_materialization_handler.go`
  - `go/internal/reducer/workload_materializer.go`
- Current typed relationship evidence extraction:
  - `go/internal/relationships/evidence.go`
  - `go/internal/relationships/dockerfile_evidence.go`
  - `go/internal/relationships/docker_compose_evidence.go`
  - `go/internal/relationships/github_actions_evidence.go`
  - `go/internal/relationships/jenkins_evidence.go`
  - `go/internal/relationships/yaml_iac_evidence.go`
  - `go/internal/relationships/terraform_schema.go`
  - `go/internal/relationships/terragrunt_helper_evidence.go`
  - `go/internal/relationships/ansible_evidence.go`
- Current source-local normalization/parsing seams:
  - `go/internal/parser/registry.go`
  - `go/internal/parser/engine.go`
  - `go/internal/parser/dockerfile_language.go`
  - `go/internal/parser/yaml_argocd.go`
  - `go/internal/parser/yaml_helm.go`
  - `go/internal/parser/hcl_language.go`
- Current service/query enrichment that should shrink over time, not grow:
  - `go/internal/query/service_evidence.go`
  - `go/internal/query/service_query_enrichment.go`
  - `go/internal/query/impact_trace_deployment.go`
  - `go/internal/query/repository_story.go`

## Desired End State

- Collectors emit normalized facts with source-local semantics only.
- A new correlation package evaluates bounded, schema-validated DSL rule packs.
- The platform ships first-party rule packs for all currently supported evidence families.
- Reducer domains separate:
  - deployable-unit correlation
  - cloud asset resolution
  - deployment mapping
  - workload materialization
- Low-confidence or controller/config-only candidates remain explainable and queryable without becoming admitted workloads.
- Shared deployment repositories contribute deployment/config evidence without inheriting workload ownership.
- `workload_materialization` writes only already-correlated canonical rows.
- Git-backed deploy/config evidence can materialize deployment mapping and platform/runtime rows before any AWS cloud collector exists.
- The container path proves cross-source correlation across:
  - code repo
  - Dockerfile
  - Jenkins or GitHub Actions
  - Helm/Argo or Terraform config
  - Terraform state
  - future AWS ECS/ECR/ALB facts
- A generic fixture-backed Compose verification lane exists for mixed-delivery corpora.

## Parallel Execution Map

Start these three tracks in parallel because they have mostly disjoint primary write sets:

1. **Track A: Scope, fact, and reducer-domain foundations**
2. **Track B: Correlation package, DSL schema, and admission calibration**
3. **Track C: Generic fixture corpus and container vertical slice**

After those land, run:

4. **Track D: Broader first-party rule-pack expansion**
5. **Track E: Git-backed deployment mapping and instance/platform materialization**
6. **Track F: Cloud asset resolution readiness**
7. **Track G: Observability, performance, docs, and execution gates**

## Chunk 1: Track A - Scope, Fact, And Reducer-Domain Foundations

**Files:**
- Modify: `go/internal/scope/scope.go`
- Modify: `go/internal/scope/scope_test.go`
- Modify: `go/internal/facts/models.go`
- Modify: `go/internal/facts/models_test.go`
- Modify: `go/internal/reducer/intent.go`
- Modify: `go/internal/reducer/domain.go`
- Modify: `go/internal/reducer/defaults.go`
- Modify: `go/internal/reducer/defaults_test.go`
- Modify: `go/internal/reducer/service_test.go`

- [ ] **Step 1: Lock the new multi-source identity contract in tests**

Add tests that fail today for:

- new `CollectorKind` values such as `aws`, `terraform_state`, and `webhook`
- new `ScopeKind` values such as `account`, `region`, `cluster`, `state_snapshot`, and `event_trigger`
- reducer intents that carry source-neutral `SourceSystem`, `ScopeID`, `GenerationID`, and related-scope sets without assuming Git-only repository scopes
- validation that rejects blank or duplicate related scopes while allowing non-repository scope types

Run:

```bash
cd go && go test ./internal/scope ./internal/facts ./internal/reducer -run 'Test.*Scope|Test.*Collector|TestIntent|TestParseDomain|TestNewDefaultRegistry' -count=1
```

Expected: FAIL where Git-only enums, validation, or default reducer catalog assumptions block multi-source identities.

- [ ] **Step 2: Expand scope and reducer identity contracts**

Implement the smallest correct changes so:

- scope kinds and collector kinds are no longer Git-only
- fact envelopes remain source-neutral but preserve explicit `SourceSystem`
- reducer domains can introduce a new `deployable_unit_correlation` domain without breaking existing queues and handlers
- default reducer wiring remains additive and backward-compatible

- [ ] **Step 3: Re-run the identity/foundation gate**

Run:

```bash
cd go && go test ./internal/scope ./internal/facts ./internal/reducer -count=1
```

Expected: PASS for multi-source scope identity and reducer domain registration.

## Chunk 2: Track B - Correlation Package Skeleton And DSL Schema

**Files:**
- Create: `go/internal/correlation/model/types.go`
- Create: `go/internal/correlation/model/types_test.go`
- Create: `go/internal/correlation/engine/engine.go`
- Create: `go/internal/correlation/engine/engine_test.go`
- Create: `go/internal/correlation/rules/schema.go`
- Create: `go/internal/correlation/rules/schema_test.go`
- Create: `go/internal/correlation/admission/admission.go`
- Create: `go/internal/correlation/admission/admission_test.go`
- Create: `go/internal/correlation/explain/explain.go`
- Create: `go/internal/correlation/explain/explain_test.go`
- Create: `go/internal/correlation/observability.go`
- Create: `go/internal/correlation/observability_test.go`

- [ ] **Step 1: Write the failing schema and engine tests**

Add tests that define the minimum DSL contract:

- `extract_key`
- `match`
- `admit`
- `derive`
- `explain`

Also add tests for:

- schema validation failures for unknown rule kinds
- deterministic evaluation ordering
- bounded match fan-out
- stable explanation rendering
- admission rejection reasons preserved with evidence metadata
- minimum confidence thresholds for workload admission
- deterministic tie-break behavior when multiple candidates compete for the
  same deployable unit
- rejected-but-queryable candidate states for controller-only or config-only
  evidence

Run:

```bash
cd go && go test ./internal/correlation/... -count=1
```

Expected: FAIL because the package and schema do not exist yet.

- [ ] **Step 2: Implement the typed DSL runtime**

Implement:

- typed Go structs for rule packs, normalized evidence atoms, correlation keys, candidate groups, and explanations
- schema validation and compile-time parsing into typed structs
- deterministic rule evaluation order
- bounded join evaluation with explainable outcomes
- admission result objects that preserve accepted and rejected evidence
- candidate states that distinguish:
  - rejected evidence groups
  - provisional non-admitted candidates
  - admitted canonical workloads
- threshold and tie-break configuration that keeps low-confidence candidates
  out of materialization

- [ ] **Step 3: Add observability hooks from day one**

Implement:

- trace spans by rule pack and rule family
- counters for evaluated rules, admitted candidates, rejected candidates, conflicts, and low-confidence outcomes
- structured summaries that can be attached to reducer result reporting

- [ ] **Step 4: Re-run the DSL package gate**

Run:

```bash
cd go && go test ./internal/correlation/... -count=1
```

Expected: PASS for schema validation, bounded evaluation, explanation rendering, and observability helpers.

## Chunk 3: Track B - Container-First Rule Packs

**Files:**
- Create: `go/internal/correlation/rules/dockerfile_rules.go`
- Create: `go/internal/correlation/rules/dockerfile_rules_test.go`
- Create: `go/internal/correlation/rules/github_actions_rules.go`
- Create: `go/internal/correlation/rules/github_actions_rules_test.go`
- Create: `go/internal/correlation/rules/jenkins_rules.go`
- Create: `go/internal/correlation/rules/jenkins_rules_test.go`
- Create: `go/internal/correlation/rules/helm_rules.go`
- Create: `go/internal/correlation/rules/helm_rules_test.go`
- Create: `go/internal/correlation/rules/argocd_rules.go`
- Create: `go/internal/correlation/rules/argocd_rules_test.go`
- Create: `go/internal/correlation/rules/terraform_config_rules.go`
- Create: `go/internal/correlation/rules/terraform_config_rules_test.go`

- [ ] **Step 1: Lock the container-slice evidence families in failing tests**

Use current evidence extraction and parser output as the source of truth and
codify failing tests for the first vertical slice proving the DSL can express:

- deployment-source semantics
- config-discovery semantics
- module-usage semantics
- reusable-workflow delivery semantics
- Jenkins delivery semantics
- non-deploying utility/controller semantics
- noisy shared-repo suppression for unrelated config provenance

Run:

```bash
cd go && go test ./internal/correlation/rules -count=1
```

Expected: FAIL until the container-focused rule packs exist.

- [ ] **Step 2: Implement the first container-focused rule packs**

Implement rule packs for:

- Dockerfile
- GitHub Actions
- Jenkins
- Helm
- ArgoCD
- Terraform config

Each rule pack must:

- expose explicit linking factors
- avoid pretending that config-discovery or utility evidence is a service
- emit explanation-ready results
- preserve confidence and provenance
- support explicit negative or narrowing conditions so shared deployment repos
  do not flood candidate-scoped provenance

- [ ] **Step 3: Re-run the container rule-pack gate**

Run:

```bash
cd go && go test ./internal/correlation/... ./internal/relationships -count=1
```

Expected: PASS with working rule coverage for the container-first evidence
families.

## Chunk 4: Track C - Generic Fixture Corpus And Compose Verification Lane

**Files:**
- Create: `tests/fixtures/correlation_dsl/README.md`
- Create: `tests/fixtures/correlation_dsl/service-gha/.github/workflows/deploy.yml`
- Create: `tests/fixtures/correlation_dsl/service-gha/Dockerfile`
- Create: `tests/fixtures/correlation_dsl/service-jenkins/Jenkinsfile`
- Create: `tests/fixtures/correlation_dsl/service-jenkins/Dockerfile`
- Create: `tests/fixtures/correlation_dsl/deploy-repo/argocd/service-gha/base/application.yaml`
- Create: `tests/fixtures/correlation_dsl/deploy-repo/argocd/service-shared/base/configmap.yaml`
- Create: `tests/fixtures/correlation_dsl/terraform-stack-gha/shared/main.tf`
- Create: `tests/fixtures/correlation_dsl/terraform-stack-jenkins/shared/main.tf`
- Create: `tests/fixtures/correlation_dsl/multi-dockerfile-repo/Dockerfile`
- Create: `tests/fixtures/correlation_dsl/multi-dockerfile-repo/Dockerfile.test`
- Create: `scripts/verify_correlation_dsl_compose.sh`
- Modify: `docs/docs/reference/local-testing.md`

- [ ] **Step 1: Build the generic regression corpus in failing tests**

Create a durable open-source corpus that captures the known failure modes:

- service repo plus CI repo plus deploy repo plus stack repo
- Jenkins plus Terraform repo-level evidence with no admitted service until
  thresholds are satisfied
- shared charts/config repo that must remain provenance-only for unrelated
  services
- multi-Dockerfile repo with one admitted workload image and one rejected
  utility/test image

Run:

```bash
cd go && go test ./internal/reducer ./internal/correlation/... -run 'Test.*CorrelationFixture|Test.*GenericCorpus' -count=1
```

Expected: FAIL until the generic corpus and corresponding tests exist.

- [ ] **Step 2: Add a Compose-backed correlation verification lane**

Implement a new runtime verification script that:

- mounts the generic corpus through `ESHU_FILESYSTEM_HOST_ROOT`
- passes explicit `ESHU_REPOSITORY_RULES_JSON`
- validates admitted versus rejected deployable units
- validates that shared deployment repos remain provenance-only where expected
- validates that fixture layout is flat and explicit rather than inferred
- captures OTEL or metrics evidence for fan-out, rejection, and conflict
  counters

- [ ] **Step 3: Run the new corpus-backed Compose gate**

Run:

```bash
./scripts/verify_correlation_dsl_compose.sh
```

Expected: FAIL at first, then PASS once the container slice and fixture lane
are wired correctly.

## Chunk 5: Track C - Container Vertical Slice Through Correlation

**Files:**
- Modify: `go/internal/reducer/candidate_loader.go`
- Modify: `go/internal/reducer/candidate_loader_test.go`
- Modify: `go/internal/reducer/workload_deployment_sources.go`
- Modify: `go/internal/reducer/workload_deployment_sources_test.go` (create if missing)
- Modify: `go/internal/reducer/projection.go`
- Modify: `go/internal/reducer/projection_test.go`
- Modify: `go/internal/reducer/workload_materialization_handler.go`
- Modify: `go/internal/reducer/workload_materialization_handler_test.go`
- Modify: `go/internal/reducer/workload_materializer.go`
- Modify: `go/internal/reducer/workload_materializer_test.go`
- Create: `go/internal/reducer/deployable_unit_correlation.go`
- Create: `go/internal/reducer/deployable_unit_correlation_test.go`
- Create: `go/internal/reducer/deployable_unit_confidence_test.go`

- [ ] **Step 1: Write failing reducer tests for the container-first path**

Add tests that prove:

- code repo + Dockerfile + Jenkins/GHA + Helm/Argo/Terraform evidence can form a deployable-unit candidate
- repo-local artifact presence alone does not materialize a service
- multiple Dockerfiles in one repo can produce multiple candidates or explicit rejections
- utility or test Dockerfiles do not get admitted without converging deploy/runtime evidence
- low-confidence candidates remain non-materialized even when they are
  queryable as rejected evidence groups
- tie-break behavior stays deterministic when multiple images or releases
  compete for the same deployable unit
- `workload_materialization` writes only already-correlated rows

Run:

```bash
cd go && go test ./internal/reducer -run 'Test.*DeployableUnit|TestExtractWorkloadCandidates|TestBuildProjectionRows|TestWorkloadMaterializationHandler' -count=1
```

Expected: FAIL because current repo-scoped candidate formation is too coarse.

- [ ] **Step 2: Add a source-neutral deployable-unit correlation stage**

Implement:

- reducer-owned translation from normalized evidence atoms to deployable-unit candidates
- container-first admission rules using explicit linking factors:
  - image repository
  - service or app name
  - release name
  - entrypoint
  - deploy source repo
  - platform-native identifiers where available
- rejection handling and conflict reporting
- confidence thresholds and downgrade behavior for controller-only or
  config-only evidence
- deterministic tie-break behavior for competing candidates

- [ ] **Step 3: Shrink workload materialization to a boring write path**

Refactor so:

- `candidate_loader.go` becomes compatibility plumbing or eventually a thin adapter
- `workload_materialization_handler.go` consumes correlated candidates
- `projection.go` writes canonical rows but does not invent service truth from raw repo-local parser signals

- [ ] **Step 4: Re-run the container reducer gate**

Run:

```bash
cd go && go test ./internal/reducer -count=1
```

Expected: PASS, with container correlation succeeding through the new stage and no regression in existing workload writes.

## Chunk 6: Track E/F - Git-Backed Deployment Mapping, Instance Materialization, And Cloud Asset Resolution Readiness

**Files:**
- Modify: `go/internal/reducer/cloud_asset_resolution.go`
- Modify: `go/internal/reducer/cloud_asset_resolution_test.go`
- Modify: `go/internal/reducer/platform_materialization.go`
- Modify: `go/internal/reducer/platform_materialization_test.go`
- Modify: `go/internal/reducer/platform_domain.go`
- Modify: `go/internal/reducer/platform_domain_test.go`
- Modify: `go/internal/reducer/infrastructure_platform_extractor.go`
- Modify: `go/internal/reducer/infrastructure_platform_extractor_test.go`

- [ ] **Step 1: Lock Git-backed deployment mapping and future cloud-readiness expectations in tests**

Add tests proving:

- current Git-backed deploy/config evidence can materialize deployment mappings
  and platform/runtime rows before any cloud collector exists
- runtime-instance rows require explicit environment-scoped deploy targets or
  observed runtime/state, not just repo references
- cloud asset resolution can accept non-Git source systems and non-repository scopes
- deployment mapping can connect deployable-unit candidates to runtime platforms and future cloud assets without hard-coding Git-only assumptions
- `RelatedScopeIDs` can represent source joins between Git, Terraform state, and cloud-observed scopes

Run:

```bash
cd go && go test ./internal/reducer -run 'TestCloudAssetResolution|TestPlatformMaterialization|TestExtractInfrastructurePlatformRows' -count=1
```

Expected: FAIL or weak coverage where handlers still assume Git-shaped inputs
or skip Git-backed deployment/platform materialization entirely.

- [ ] **Step 2: Strengthen source-neutral reducer request models**

Implement:

- source-neutral request structures for cloud asset and deployment mapping paths
- explicit Git-backed deployment/platform mapping paths that do not wait for
  future cloud collectors
- explicit join points for future Terraform state and AWS fact families
- clean handoff between deployable-unit correlation output and deployment/platform materialization

- [ ] **Step 3: Re-run the cloud/deployment readiness gate**

Run:

```bash
cd go && go test ./internal/reducer -count=1
```

Expected: PASS for Git-backed deployment/platform truth and future
cloud/deployment readiness.

## Chunk 7: Track D/G - Broader Rule-Pack Expansion And Observability Hardening

**Files:**
- Modify: `go/internal/reducer/observability.go`
- Modify: `go/internal/reducer/acceptance_observability.go`
- Modify: `go/internal/reducer/shared_projection_runner.go`
- Modify: `go/internal/reducer/shared_projection_worker.go`
- Modify: `go/internal/reducer/service_batch.go`
- Modify: `go/internal/reducer/service_batch_test.go`
- Modify: `go/internal/reducer/shared_projection_runner_test.go`
- Modify: `go/internal/reducer/shared_projection_worker_test.go`
- Modify: `go/internal/reducer/acceptance_observability_test.go`
- Modify: `go/internal/reducer/service_ack_observability_test.go`
- Modify: `go/internal/query/impact_trace_deployment_response_provenance_test.go`
- Create: `go/internal/correlation/rules/docker_compose_rules.go`
- Create: `go/internal/correlation/rules/docker_compose_rules_test.go`
- Create: `go/internal/correlation/rules/kustomize_rules.go`
- Create: `go/internal/correlation/rules/kustomize_rules_test.go`
- Create: `go/internal/correlation/rules/terragrunt_rules.go`
- Create: `go/internal/correlation/rules/terragrunt_rules_test.go`
- Create: `go/internal/correlation/rules/ansible_rules.go`
- Create: `go/internal/correlation/rules/ansible_rules_test.go`
- Create: `go/internal/correlation/rules/runtime_platform_rules.go`
- Create: `go/internal/correlation/rules/runtime_platform_rules_test.go`

- [ ] **Step 1: Add failing observability and bounded-work tests**

Add tests for:

- span and metric emission by rule pack / correlation family
- bounded candidate fan-out
- deterministic tie-break behavior
- correlation failures reported as structured evidence summaries
- no unbounded queue amplification from correlation output
- remaining first-party evidence families can be added without changing the
  admission contract proven by the container slice

Run:

```bash
cd go && go test ./internal/reducer ./internal/query -run 'Test.*Observability|Test.*Batch|Test.*Worker|Test.*Provenance' -count=1
```

Expected: FAIL until correlation observability and bounded behavior are explicit.

- [ ] **Step 2: Implement the remaining first-party rule packs and hardening behavior**

Implement:

- the remaining first-party rule packs:
  - Docker Compose
  - Kustomize
  - Terragrunt
  - Ansible
  - runtime/platform families already surfaced today
- OTEL spans and metrics for rule evaluation, admission, rejection, and conflict paths
- reducer result summaries that surface candidate counts, rejected counts, and conflict classes
- bounded evaluation and batch sizing that prevent open-ended match explosions
- queue-safe handling so correlation can scale without deadlock or write amplification
- query-facing provenance contract tests only; do not expand service-story
  narrative synthesis in this chunk

- [ ] **Step 3: Re-run the hardening gate**

Run:

```bash
cd go && go test ./internal/reducer ./internal/query -count=1
```

Expected: PASS with stable metrics, provenance, and bounded correlation behavior.

## Chunk 8: Track G - Documentation, Examples, And Open-Source Extension Guidance

**Files:**
- Modify: `docs/docs/architecture.md`
- Modify: `docs/docs/reference/source-layout.md`
- Modify: `docs/docs/guides/collector-authoring.md`
- Modify: `docs/docs/reference/relationship-mapping.md`
- Modify: `docs/docs/deployment/service-runtimes.md`
- Create: `docs/docs/guides/correlation-dsl.md`
- Create: `docs/docs/guides/correlation-dsl-first-party-rule-packs.md`
- Create: `docs/docs/guides/correlation-dsl-extension-example.md`
- Modify: `docs/docs/reference/local-testing.md`
- Modify: `docs/docs/reference/telemetry/index.md`

- [ ] **Step 1: Write failing docs/contract expectations into the plan gate**

List the documentation deltas explicitly before editing:

- where the DSL lives
- which rule primitives exist
- which first-party rule packs ship on day one
- when to add a parser vs a normalizer vs only a rule pack
- how AWS scanner and Terraform state scanner fit the runtime contract

- [ ] **Step 2: Update the docs with source-neutral architecture**

Document:

- collector/source boundaries
- correlation package ownership
- reducer domain responsibilities
- first-party rule packs as examples for contributors
- telemetry and local test commands for correlation work
- ownership boundaries versus service-story and read-path presentation work

- [ ] **Step 3: Run the docs gate**

Run:

```bash
mkdocs build --strict
```

Expected: PASS with the new DSL and collector-readiness docs included.

## Chunk 9: Final Verification Gate

**Files:**
- Verify only; no new files expected

- [ ] **Step 1: Run the focused package gates**

Run:

```bash
cd go && go test ./internal/scope ./internal/facts ./internal/correlation/... ./internal/reducer ./internal/relationships ./internal/query -count=1
```

Expected: PASS.

- [ ] **Step 2: Run the runtime/deployment gate for reducer-facing changes**

Run:

```bash
cd go && go test ./cmd/api ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer -count=1
```

Expected: PASS.

- [ ] **Step 3: Run the generic corpus-backed runtime gate**

Run:

```bash
./scripts/verify_correlation_dsl_compose.sh
```

Expected: PASS.

- [ ] **Step 4: Run the docs gate**

Run:

```bash
mkdocs build --strict
```

Expected: PASS.

- [ ] **Step 5: Summarize execution evidence**

Record:

- which first-party rule packs were added
- which reducer domains changed
- which source kinds and collector kinds were expanded
- which observability signals were added
- which future collector slices are now unblocked:
  - AWS scanner
  - Terraform state scanner
  - webhook-driven freshness
  - additional CI/CD / GitOps families

## Recommended Execution Order

1. Chunk 1
2. Chunk 2
3. Chunk 3
4. Chunk 4
5. Chunk 5
6. Chunk 6
7. Chunk 7
8. Chunk 8
9. Chunk 9

This order keeps the architecture stable:

- identity and domain contracts first
- DSL runtime and admission calibration second
- container-first rule packs third
- generic corpus and Compose lane fourth
- reducer integration fifth
- Git-backed deployment/platform truth sixth
- broader rule-pack expansion plus hardening seventh
- docs and final gates last

## Commit Guidance

Use frequent, architecture-shaped commits:

- `feat: add multi-source scope and reducer domain foundations`
- `feat: add constrained correlation DSL runtime and admission calibration`
- `test: add generic multi-repo correlation fixture corpus`
- `feat: add container-first correlation rule packs`
- `refactor: route workload materialization through deployable-unit correlation`
- `feat: materialize git-backed deployment mapping and platform truth`
- `feat: expand first-party rule packs and add correlation observability`
- `docs: document correlation DSL and collector readiness`

## Risks To Watch During Execution

- accidental duplication between `go/internal/relationships` and the new correlation layer
- workload admission regressions caused by mixing old repo-centric heuristics with new candidate correlation
- shared deployment repos flooding candidate-scoped provenance when narrowing
  keys are missing
- fixture drift that breaks the explicit repository-selection contract for
  Compose verification
- unbounded match fan-out if rule evaluation is not indexed and bounded
- query-time enrichment retaining old repair logic after reducer-backed truth exists
- operational confusion if domain ownership is not documented precisely

## Done Means

- the platform has a real, tested correlation package with a constrained DSL
- every currently supported evidence family has a first-party rule pack or equivalent shipped coverage
- reducer domains are source-neutral enough to accept AWS and Terraform-state collectors
- `workload_materialization` is no longer the primary home for repo-scoped inference
- the container-first path is proven end to end
- the generic corpus-backed Compose lane proves admitted versus rejected
  deployable units and protects shared-repo false-positive cases
- instrumentation is strong enough to diagnose correlation decisions from logs, traces, and metrics
- contributors have documentation and examples for extending the system cleanly
