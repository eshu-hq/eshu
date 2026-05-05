# ADR: IaC Usage, Reachability, And Refactor Impact Graph

**Date:** 2026-04-24
**Status:** In Progress
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `2026-04-19-ci-cd-relationship-parity-across-delivery-families.md`
- `2026-04-19-deployable-unit-correlation-and-materialization-framework.md`
- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md`
- `2026-04-20-terraform-state-collector.md`
- `../reference/dead-code-reachability-spec.md`
- `../../superpowers/specs/2026-04-26-unified-change-impact-and-dependency-neighborhoods-design.md`
- `../../superpowers/specs/2026-04-26-cross-repo-contract-bridge-and-service-dependency-graph-design.md`
- `../../superpowers/plans/2026-04-26-iac-usage-reachability-and-refactor-impact-action-plan.md`
- `../../superpowers/plans/2026-04-26-unified-change-impact-and-dependency-neighborhoods-implementation.md`
- `../../superpowers/plans/2026-04-26-cross-repo-contract-bridge-and-service-dependency-graph-implementation.md`

---

## Status Review (2026-05-03)

**Current disposition:** In progress.

Dead-IaC reachability has real runtime proof now: the analyzer, durable
Postgres rows, API materialized-row path, MCP route, OpenAPI path, product-truth
fixtures, and full-corpus materialization evidence are all present.

**Remaining work:** do not mark the broader ADR complete yet. The shared
dependency-neighborhood contract, impact and integrity workflows, CloudFormation,
Crossplane, deeper Helm rendering, Argo ApplicationSet templating, conservative
Kubernetes orphan semantics, and large-result pagination are still open.

## Context

Eshu already extracts a large IaC surface from Git sources:

- Terraform and Terragrunt blocks, module sources, dependencies, inputs,
  locals, variables, resources, data sources, outputs, and providers.
- Kubernetes resources, Kustomize overlays, Helm charts and values files,
  ArgoCD Applications and ApplicationSets, Crossplane resources, and
  CloudFormation resources, parameters, outputs, conditions, imports, and
  exports.
- CI/CD delivery evidence from GitHub Actions, Jenkins, Ansible, Docker
  Compose, Dockerfiles, Helm, Kustomize, ArgoCD, Terraform, and Terragrunt.

Those facts support relationship mapping and deployment trace workflows, but
they do not yet provide a durable, domain-aware graph of IaC definitions,
filesystem artifacts, references, and deployment roots. As a result, Eshu cannot
yet answer the cleanup and refactor questions operators ask before changing
infrastructure code:

- Which Terraform modules, variables, locals, data sources, outputs, and local
  module sources are unused?
- Which Helm charts, values files, templates, and chart dependencies are unused
  or unreachable?
- Which Kustomize bases, overlays, components, resources, patches, images, and
  Helm chart references are missing or unreachable?
- Which ArgoCD sources point at missing repositories or paths?
- Which local `source`, `valuesFile`, `resources`, `patches`, `templatefile`,
  `file`, `config_path`, or nested-stack paths are invalid on disk?
- If a module, chart, values file, overlay, manifest, or config asset is moved,
  renamed, or deleted, which workflows, applications, overlays, modules, and
  services break?
- How does code flow through CI/CD and IaC into runtime resources and
  environments?

The existing dead-code capability is code-call oriented. It finds code entities
that have no inbound `CALLS`, `IMPORTS`, or `REFERENCES` edges after applying a
root model for language entrypoints and framework callbacks. IaC needs an
equivalent product experience, but it cannot reuse the same semantics directly.
For example, an unreferenced Terraform resource in an active root module is not
necessarily dead, and an unreferenced Kubernetes Deployment may still be a live
workload. IaC deadness is primarily about reachability from deployment roots,
definition usage, path validity, and renderer/controller semantics.

---

## Decision

Eshu will add a first-class **IaC usage and reachability graph**. This graph will
model IaC definitions, filesystem artifacts, references, and deployment roots so
that Eshu can expose three user-facing workflows from one authoritative data
model:

1. **Find dead IaC**: unused definitions, unreachable artifacts, suspicious
   orphans, disabled definitions, and ambiguous dynamic cases.
2. **IaC refactor impact**: all known consumers of a module, chart, values file,
   overlay, manifest, source path, config file, or rendered/provisioned
   resource before a rename, move, or delete.
3. **IaC integrity**: missing local paths, unresolved references, invalid source
   paths, and broken renderer/controller links.

The same work must also close the product gap for source-code dependency
tracing. Eshu must expose a shared **dependency neighborhood** contract for code
and IaC entities so any caller can select a file, symbol, module, chart,
overlay, manifest, or resource and immediately answer:

- what it depends on
- what depends on it
- the shortest known paths to roots, workloads, services, and environments
- source evidence with line/range spans when available
- truth labels, ambiguity reasons, and coverage limitations

Tree-sitter and language parsers can provide code spans and symbol evidence, but
the neighborhood contract must not be a parser-specific or UI-specific feature.
It must be backed by the same canonical graph, content store, usage edges, and
truth-label protocol that serve HTTP, MCP, CLI, PR automation, refactor tooling,
and future web/IDE surfaces.

The graph also becomes the missing middle layer for end-to-end Eshu relationships:

```text
code repo
  -> CI/CD workflow or controller
  -> IaC module/chart/overlay/manifest/config source
  -> rendered or provisioned resources
  -> service, environment, namespace, cluster, and cloud/runtime inventory
```

The implementation must be semantic first. Tree-sitter may be used as an
optional syntax/span evidence provider, but it is not the primary architecture
for IaC reachability. Domain parsers and renderers provide the source of truth:

- HashiCorp HCL for Terraform and Terragrunt syntax and expression traversal.
- YAML decoding plus Kubernetes, Kustomize, ArgoCD, Crossplane, and
  CloudFormation semantics for YAML-family artifacts.
- Helm and Go-template aware parsing for Helm templates and values references.
- Renderer-backed validation where available, such as `helm template` and
  `kustomize build`, when the runtime profile permits external tooling.

---

## Definitions

### IaC Root

An entrypoint that makes an IaC artifact live or deployment-relevant.

Examples:

- ArgoCD `Application` or `ApplicationSet`.
- GitHub Actions or Jenkins deploy command.
- Terraform or Terragrunt root module.
- Helm chart or release path.
- Kustomize overlay selected by a controller or workflow.
- CloudFormation stack template.
- Ansible playbook invoked by CI/CD.
- Docker Compose file invoked by CI/CD or local profile.

### IaC Definition

A named logical definition inside an IaC artifact.

Examples:

- Terraform `variable`, `local`, `data`, `resource`, `module`, `output`, and
  `provider`.
- Terragrunt `dependency`, `input`, `local`, and config block.
- Helm value key, chart dependency, named template, and rendered resource.
- Kubernetes resource, selector, owner reference, service account, role,
  binding, ConfigMap, Secret, volume, env reference, and workload.
- Kustomize overlay, patch target, image reference, Helm chart reference,
  resource reference, base, and component.
- ArgoCD source, destination, generator source, and template source.
- CloudFormation parameter, condition, resource, output, import, export, and
  nested stack.
- Crossplane XRD, Composition, claim, composition resource, patch, transform,
  and function step.

### IaC Artifact

A filesystem or remote artifact that can be referenced, rendered, or deployed.

Examples:

- `modules/vpc/`, `charts/api/`, `values-prod.yaml`,
  `overlays/prod/kustomization.yaml`, `deployment.yaml`,
  `templates/deployment.yaml`, `stack.yaml`, `playbook.yml`, and
  `docker-compose.yaml`.

### IaC Reference

A typed edge from a root, artifact, or definition to another artifact or
definition.

Examples:

- `TerraformModule CALLS_MODULE TerraformModule`.
- `TerraformOutput REFERENCES TerraformResource`.
- `TerraformResource REFERENCES TerraformDataSource`.
- `TerraformLocal REFERENCES TerraformVariable`.
- `TerragruntDependency REFERENCES_CONFIG TerragruntConfig`.
- `ArgoCDApplication SYNC_SOURCE KustomizeOverlay`.
- `KustomizeOverlay INCLUDES ManifestFile`.
- `KustomizeOverlay PATCHES K8sResource`.
- `HelmTemplate REFERENCES_VALUE HelmValue`.
- `HelmChart DEPENDS_ON HelmChart`.
- `HelmChart RENDERS K8sResource`.
- `Service SELECTS Workload`.
- `Deployment USES ConfigMap`.
- `RoleBinding BINDS Role`.
- `CloudFormationResource REFERENCES CloudFormationParameter`.
- `CloudFormationResource DEPENDS_ON CloudFormationResource`.

### IaC Finding

A queryable quality result derived from the usage graph.

Initial finding classes:

- `unused_definition`: a definition has no semantic consumers within its
  relevant root/module/chart/template scope.
- `unreachable_artifact`: a file, directory, chart, overlay, module, or manifest
  is not reachable from any known root.
- `missing_path`: a local path reference points to a file or directory that does
  not exist in the indexed repository snapshot.
- `unresolved_reference`: a reference could not be resolved to a known
  definition or artifact.
- `suspicious_orphan`: a resource is disconnected in a way that may indicate
  dead IaC, but the system cannot safely call it unused.
- `disabled_definition`: a definition is guarded by a known false condition,
  disabled chart condition, profile, or count expression.
- `ambiguous_dynamic`: dynamic HCL, templating, generators, plugins, or
  unresolved values prevent exact proof.

---

## Product Surfaces

### Dependency Neighborhood And Explorer

The user-facing workflow should answer:

```text
For this file, symbol, module, chart, overlay, manifest, or resource, what does
it depend on and what depends on it?
```

This is a shared code and IaC capability, not an IaC-only report and not just a
UI feature. The dependency-neighborhood response should become the common
contract for:

- interactive dependency explorer views
- MCP and agent answers about impact, reachability, and dependency context
- CLI dependency and impact summaries
- PR review automation that explains the blast radius of changed files
- dead-code and dead-IaC cleanup workflows that need evidence and confidence
- refactor tooling for rename, move, delete, and extraction workflows
- internal platform APIs that need a stable response shape across code, IaC,
  deployment, and runtime domains

The response model must include:

- selected subject identity, kind, repo, path, and optional line/range
- normalized incoming edges: direct dependents and reverse consumers
- normalized outgoing edges: direct dependencies and referenced artifacts
- optional transitive paths with depth, path kind, and truncation metadata
- root reachability: entrypoints, workflows, controllers, deploy roots, and
  runtime roots that make the subject live or deployment-relevant
- blast-radius context: affected services, workloads, environments, repos, and
  runtime resources when known
- findings connected to the subject, including dead-code, dead-IaC, integrity,
  and ambiguous-dynamic findings
- source evidence and content-read handles for each edge when available
- truth labels, exactness, partial-coverage flags, and unsupported-capability
  envelopes when the indexed mode cannot answer exactly

The first ESHU-owned UI should consume the same contract instead of having a
separate graph query path. It should keep one selected entity in sync across the
graph, file tree, and source inspector. Selecting an entity must focus the graph
neighborhood, expand the file tree to the owning artifact when applicable, and
show source content or source spans without requiring the user to write Cypher or
know which specialized endpoint owns the underlying edge family.

The UI should provide at least these tabs or equivalent panes:

- `Source`: selected source file or symbol range with highlighted evidence
- `Depends On`: outgoing code, IaC, config, deployment, and runtime references
- `Depended On By`: incoming callers, importers, consumers, roots, and owners
- `Paths`: transitive paths to roots, workloads, services, or environments
- `Findings`: deadness, integrity, and ambiguous-resolution findings
- `Blast Radius`: impacted services, workloads, repos, environments, and cloud
  or runtime resources

This ADR explicitly rejects leaving this workflow as an ad hoc Neo4j Browser
exercise or an editor-only import list. The product requirement is a reusable
query contract plus a ESHU-owned interactive surface that works for code and IaC
with the same semantics.

The same contract should also become an agent and automation guardrail. Agents,
PR automation, and refactor tools should inspect dependency neighborhoods before
editing symbols, contracts, or IaC artifacts, and should run diff-aware impact
before commit or PR. The diff workflow must preserve line/range evidence where
available, label file-only fallbacks for deletes, renames, binary files, and
missing spans, and report high, unknown, stale, partial, or unsupported coverage
instead of presenting unsafe confidence.

### Find Dead IaC

The user-facing workflow should answer:

```text
Find unused IaC in this repo, path, service, environment, or graph scope.
```

The response must include:

- finding class
- confidence: `exact`, `high`, `derived`, or `ambiguous`
- entity or artifact identity
- source path and line/range when available
- root scope considered
- evidence edges used
- blockers that prevent exactness
- suggested next action when safe

The result must not collapse all findings into a boolean `dead=true`. Some IaC
findings are cleanup-safe; others are warnings that need operator review.

### IaC Refactor Impact

The user-facing workflow should answer:

```text
If I rename, move, or delete this module/chart/overlay/values file/manifest,
what consumes it?
```

The response must include reverse references across:

- local filesystem references
- cross-repository references
- CI/CD workflow references
- ArgoCD source references
- Terraform/Terragrunt module and config references
- Helm chart, dependency, template, and values references
- Kustomize resource, base, component, patch, and Helm chart references
- rendered or provisioned resource relationships when available

### IaC Integrity

The user-facing workflow should answer:

```text
Which IaC references are broken or unresolved?
```

Initial examples:

- Terraform `source = "../modules/foo"` points to a missing directory.
- Terragrunt `config_path = "../vpc"` points to a missing config.
- Kustomize `resources:` or `patches:` entry points to a missing file.
- ArgoCD `spec.source.path` points to a missing path in the target repo.
- Helm `valueFiles` points to a missing values file.
- CloudFormation nested stack `TemplateURL` points to a missing local template.
- Ansible role or playbook import points to a missing role/playbook.

---

## Parser And Evidence Strategy

### Terraform And Terragrunt

Use HashiCorp HCL as the canonical parser. The first implementation should use
`hclsyntax.Expression.Variables()` or equivalent traversal APIs to extract
references from attributes and nested blocks.

Initial references:

- `var.name`
- `local.name`
- `data.type.name`
- `resource_type.resource_name`
- `module.name`
- `module.name.output`
- provider aliases
- `depends_on`
- `count` and `for_each` expressions
- local filesystem paths in `source`, `file`, `templatefile`, backend config,
  and Terragrunt helper/config expressions

Important rule:

- A Terraform `resource` with no inbound references is not automatically dead
  when its containing root module is live. It may still be managed by Terraform.
  Eshu should flag unreferenced resources as `suspicious_orphan` unless stronger
  evidence proves they are unreachable or disabled.

High-confidence cleanup candidates:

- Unreferenced variables in a module.
- Unreferenced locals.
- Unreferenced data sources.
- Outputs not consumed by parent modules or external roots when the consuming
  scope is known.
- Local module sources not referenced by any root or child module.
- Missing local module/config paths.

### Helm

Helm requires Go-template awareness. Plain YAML parsing is insufficient for
templates under `templates/`.

Initial references:

- `Chart.yaml` dependencies.
- values files.
- `.Values.*` references in templates and helper templates.
- `.Chart.*`, `.Release.*`, `include`, `template`, `tpl`, `required`, and
  named template references.
- conditional resource gates such as `.Values.enabled`,
  `.Values.ingress.enabled`, and chart dependency conditions.
- rendered Kubernetes resources when `helm template` is available.

Finding examples:

- values keys defined but never referenced by templates.
- `.Values.*` references with no default or supplied value.
- chart dependencies not used by any selected root.
- missing values files referenced by ArgoCD, Helm, or Kustomize.
- templates that never render for any known environment profile.

### Kustomize

Kustomize references should be promoted from parser properties and
content-backed relationships into durable usage edges.

Initial references:

- `resources`
- `bases`
- `components`
- `patches`
- `patchesStrategicMerge`
- `patchesJson6902`
- patch targets
- `helmCharts`
- `images`
- generators where practical

Finding examples:

- missing resource, base, component, patch, or chart path.
- overlay not reachable from any ArgoCD app, workflow, or root selector.
- patch target that matches no known resource.
- base or component no longer consumed by any overlay.

### ArgoCD

ArgoCD Applications and ApplicationSets are roots and cross-repository
connectors.

Initial references:

- `spec.source.repoURL`
- `spec.source.path`
- `spec.sources[]`
- Helm `valueFiles`, `parameters`, and chart source fields.
- ApplicationSet generator sources and template sources.
- destination server, namespace, and project.

Finding examples:

- source path does not exist in the target repo.
- source repo cannot be matched to an indexed repository.
- selected path renders no recognized manifest, chart, or overlay.
- ApplicationSet generator/template source split is ambiguous.

### Kubernetes YAML

Kubernetes resource deadness must be conservative. Many live resources do not
have textual consumers.

Initial references:

- Service selectors to workloads and pod templates.
- workload references to ConfigMaps, Secrets, volumes, ServiceAccounts,
  PVCs, image pull secrets, and env refs.
- RBAC RoleBinding and ClusterRoleBinding subjects and role refs.
- owner references.
- HPA, PDB, Ingress, NetworkPolicy, ServiceMonitor, and Gateway API refs where
  extractable.

Finding examples:

- Service selector matches no workload.
- ConfigMap or Secret has no known workload consumers.
- RoleBinding points to a missing role or subject.
- ServiceAccount is not used by any known workload.
- resource is disconnected but only classified as `suspicious_orphan` unless
  root reachability proves it is unused.

### CloudFormation

Initial references:

- `Ref`
- `Fn::GetAtt`
- `Fn::Sub`
- `DependsOn`
- `Condition`
- nested stack `TemplateURL`
- parameters, mappings, outputs, imports, and exports

Finding examples:

- parameter unused by resources, conditions, outputs, or nested stacks.
- condition defined but unused.
- output not exported or consumed where the scope is known.
- nested template path missing.
- resource disabled by a resolvable false condition.

### Crossplane

Initial references:

- claim kind to XRD.
- Composition `compositeTypeRef` to XRD.
- Composition resources to managed resource kinds.
- patch and transform field paths.
- pipeline/function steps where practical.

Finding examples:

- claim type has no matching XRD.
- XRD has no claims or compositions.
- Composition references a missing composite type.
- patch source or target field path cannot be resolved against known schema
  when schema is available.

### CI/CD Delivery Roots

CI/CD workflows are roots into IaC. Existing relationship extraction should be
connected to the usage graph rather than remaining only evidence prose.

Initial roots and references:

- GitHub Actions deploy commands: Terraform, Terragrunt, Helm, Kustomize,
  kubectl, Docker Compose, Ansible.
- Jenkins/Groovy deploy commands and shared-library parameters.
- Ansible playbook and role references.
- Docker Compose files and service dependencies.
- Docker build contexts and image references when linked to deploy workflows.

---

## Graph Model

The graph should introduce a durable, backend-neutral usage edge model instead
of hard-coding each query around ad hoc properties.

Conceptual nodes:

- `IaCRoot`
- `IaCArtifact`
- `IaCDefinition`
- `IaCReference`
- existing concrete labels such as `TerraformModule`, `HelmChart`,
  `KustomizeOverlay`, `ArgoCDApplication`, `K8sResource`,
  `CloudFormationResource`, and `TerragruntConfig`

Conceptual edges:

- `USES`
- `REFERENCES`
- `CALLS_MODULE`
- `SYNC_SOURCE`
- `INCLUDES`
- `PATCHES`
- `RENDERS`
- `SELECTS`
- `BINDS`
- `MOUNTS`
- `CONFIGURES`
- `DEPENDS_ON`
- `DEFINES`
- `BROKEN_REFERENCE`

Each edge must preserve:

- source entity or artifact id
- target entity, artifact id, or unresolved target key
- repo id and generation id
- source path
- line/range when available
- evidence kind
- parser or renderer source
- confidence
- resolution status
- failure or ambiguity reason

The exact graph labels and edge names can be refined during implementation, but
the ADR requires a single durable reference model that query surfaces share.

---

## Query And API Surfaces

The public surfaces should be separate from the existing code-only dead-code
endpoint.

Initial HTTP and MCP capability names:

- `graph.neighborhood`
- `iac_quality.dead_iac`
- `iac_quality.refactor_impact`
- `iac_quality.integrity`
- `iac_relationships.usage_graph`

Candidate HTTP routes:

- `POST /api/v0/graph/neighborhood`
- `POST /api/v0/iac/dead`
- `POST /api/v0/iac/impact`
- `POST /api/v0/iac/integrity`
- `POST /api/v0/iac/relationships`

Candidate MCP tools:

- `get_dependency_neighborhood`
- `find_dead_iac`
- `trace_iac_impact`
- `find_broken_iac_references`
- `get_iac_relationships`

The final route and MCP names must be documented in the HTTP and MCP reference
pages when implemented. This ADR only decides the capability boundary.

`/api/v0/graph/neighborhood` is the preferred backend for the Dependency
Explorer. Specialized code and IaC routes may continue to expose deeper domain
queries, but they should feed the neighborhood response shape rather than force
UI clients to stitch together unrelated response models.

---

## Exactness And Truth Labels

IaC query responses must report truth basis and exactness explicitly.

An answer may be `exact` only when:

1. the relevant root model is present,
2. all local filesystem references were checked against the indexed snapshot,
3. the required renderer or semantic parser was available, and
4. dynamic expressions, templates, plugins, or external repos did not block
   resolution.

Otherwise the result must be `derived` or `ambiguous`.

Examples:

- Terraform unreferenced local within one parsed module can be exact.
- Terraform remote module consumer impact across unindexed repos is derived.
- Helm value usage without rendering every selected values file is derived.
- Helm template branches gated by unresolved values are ambiguous.
- Kustomize missing local resource path is exact.
- ArgoCD source path into an indexed repo is exact; source path into an
  unindexed repo is unresolved or derived.
- Kubernetes orphaned resource without controller/root proof is suspicious, not
  exact deadness.

---

## Implementation Ownership

The implementation must preserve Eshu's existing boundaries:

- `go/internal/parser/` extracts semantic definitions, references, and spans
  from individual files.
- `go/internal/relationships/` maps first-party and cross-repository evidence.
- `go/internal/facts/` and `go/internal/storage/postgres/` define durable fact
  and queue contracts.
- `go/internal/projector/` projects source-local entities and references.
- `go/internal/reducer/` resolves cross-domain and cross-repository usage
  relationships.
- `go/internal/query/` owns HTTP/MCP query surfaces and truth labels.
- `go/internal/graph/` and graph adapters own backend-neutral graph writes.
- Future editor and web UI surfaces own interaction state and presentation, but
  must call documented query surfaces instead of embedding raw Cypher or
  storage-specific dependency logic.

Handlers must depend on graph/query ports, not concrete graph adapter types.

---

## Telemetry Requirements

IaC usage analysis touches runtime behavior and must include operator-visible
telemetry.

Required telemetry:

- span around each parser family reference-extraction pass
- duration histogram per parser family and renderer
- counter for extracted references by family and reference kind
- counter for unresolved references by family and reason
- histogram for rendered resource counts
- counter for missing-path findings by family and path kind
- reducer processing duration for IaC usage materialization
- query duration and result-count histograms for dead IaC, impact, integrity,
  and relationship queries
- query duration, edge-count, path-count, truncation-count, and
  unsupported-capability counters for dependency-neighborhood requests
- UI-facing structured logs that explain selected entity resolution failures,
  partial coverage, and fallback from graph-backed to content-backed evidence

Metric labels must avoid high-cardinality path values. File paths, unresolved
targets, and specific identifiers belong in spans, structured logs, or finding
payloads, not metric labels.

---

## Non-Goals

This ADR does not require launch support for:

- perfect Terraform plan semantics
- Terraform Cloud or Terraform Enterprise workspace graph extraction
- exhaustive Helm chart rendering for every possible values combination
- Helm plugin execution
- ApplicationSet plugin-specific generator semantics
- complete Kubernetes CRD schema validation
- live Kubernetes cluster discovery
- cloud provider live-state comparison
- destructive cleanup automation

Those capabilities can be added later as collectors or renderer profiles. The
first contract is static usage, reachability, integrity, and refactor impact
from indexed sources.

---

## Rollout

### Follow-On Design Specs

Two follow-on specs extend this ADR after the shared neighborhood contract is
accepted:

- `docs/superpowers/specs/2026-04-26-unified-change-impact-and-dependency-neighborhoods-design.md`
  turns `graph.neighborhood` into a shared API, MCP, CLI, PR automation, refactor
  tooling, cleanup, and UI capability.
- `docs/superpowers/specs/2026-04-26-cross-repo-contract-bridge-and-service-dependency-graph-design.md`
  adds HTTP, gRPC, event, schema, generated-client, and shared-library contract
  edges that can feed dependency neighborhoods and change-impact summaries.

### Phase 0: Shared Dependency Neighborhood Contract

Deliver:

- `POST /api/v0/graph/neighborhood` contract for selected code and IaC
  entities.
- request selectors for entity id, repo/path, path plus line/range, and semantic
  name where supported.
- normalized incoming/outgoing edge response with truth labels and coverage
  limitations.
- first UI integration that shows `Source`, `Depends On`, `Depended On By`,
  `Paths`, `Findings`, and `Blast Radius` for code relationships already present
  in the graph.
- replacement of ad hoc VS Code import-only dependency logic with the documented
  neighborhood route.

### Phase 1: Terraform, Terragrunt, And Filesystem References

Deliver:

- HCL traversal extraction for `var`, `local`, `data`, `resource`, `module`,
  provider alias, `depends_on`, `count`, and `for_each`.
- local filesystem path validation for module `source`, Terragrunt
  `config_path`, `file`, `templatefile`, and helper-built config paths.
- unused variable/local/data/output findings within module scope.
- reverse-impact query for local module and config paths.

### Phase 2: Kustomize And ArgoCD Roots

Deliver:

- durable Kustomize usage edges for resources, bases, components, patches,
  patch targets, `helmCharts`, and images.
- ArgoCD Application/ApplicationSet roots into indexed repositories and paths.
- missing-path findings for ArgoCD and Kustomize references.
- reverse-impact query for overlays, bases, components, resources, and patches.

### Phase 3: Helm And Go-Template Usage

Deliver:

- Helm chart dependency and values-file usage edges.
- `.Values.*` references from templates and helper templates.
- missing-value and unused-value findings.
- optional `helm template` rendering when tooling is available.
- reverse-impact query for charts, templates, values files, and value keys.

### Phase 4: Kubernetes Resource Usage

Deliver:

- typed edges for selectors, ConfigMap/Secret usage, ServiceAccounts, RBAC,
  volumes, env refs, owner refs, HPA/PDB/Ingress/NetworkPolicy/Gateway refs
  where extractable.
- conservative `suspicious_orphan` findings.
- rendered-resource linkage from Helm and Kustomize when available.

### Phase 5: CloudFormation, Crossplane, Compose, And Ansible

Deliver:

- CloudFormation intrinsic/reference graph and nested-template path validation.
- Crossplane XRD, Composition, claim, patch, and function-step references.
- Docker Compose service/build/dependency/profile usage.
- Ansible playbook, role, vars, and inventory usage.

---

## Test Requirements

Each family must include:

- positive case: a live root reaches a definition or artifact and suppresses a
  dead finding.
- negative case: an unused or unreachable definition/artifact is reported.
- ambiguous case: dynamic expression, template, generator, plugin, or external
  repo prevents exactness.
- missing-path case: local path reference points to a missing file or directory.
- refactor-impact case: a target path or definition returns all known consumers.

Required validation layers:

- parser unit tests for extracted references
- projector tests for durable reference facts
- reducer tests for resolved usage edges
- query tests for dead-IaC, impact, integrity, and relationship responses
- dependency-neighborhood query tests for code-only, IaC-only, and mixed
  code-to-IaC subjects
- UI contract tests that prove selecting an entity can populate source,
  incoming, outgoing, paths, findings, and blast-radius panes without raw Cypher
- docs updates for public routes, MCP tools, CLI commands, and truth labels when
  those surfaces are implemented
- compose or local-authoritative proof before claiming graph-backed exactness

---

## Consequences

Positive consequences:

- Eshu can answer cleanup, broken-reference, and safe-refactor questions for IaC.
- Eshu gets a first-class dependency explorer for code and IaC instead of a
  graph-database demo or import-only sidebar.
- Deployment trace gains a richer code-to-CI/CD-to-IaC-to-runtime middle layer.
- Existing parser work becomes more valuable because extracted entities gain
  durable usage edges.
- Query truth improves because missing paths and ambiguous dynamic cases are
  explicit instead of hidden inside prose.

Costs:

- The graph model and reducer materialization path grow.
- Some renderers require optional external tooling and profile-aware fallback.
- Exactness varies by family and must be exposed honestly.
- Terraform, Helm, Kubernetes, and ArgoCD all need domain-specific semantics;
  a syntax-only parser cannot provide sufficient truth.

Risks:

- Overclaiming deadness can cause unsafe cleanup recommendations.
- Renderer execution can be expensive or non-hermetic if not sandboxed.
- High-cardinality path and target labels can damage telemetry if modeled as
  metric dimensions.
- Dynamic HCL, Helm templates, ApplicationSet plugins, and CRD-specific
  semantics can produce ambiguous results that operators may mistake for exact
  answers unless the API is explicit.

Mitigations:

- Use conservative finding classes and confidence labels.
- Default unresolved dynamic cases to `ambiguous_dynamic`.
- Treat parser semantics as authoritative only within their modeled scope.
- Keep renderers optional and profile-gated.
- Return evidence and blockers for every finding.

---

## Chunk Status

| Chunk | Status | Evidence | Remaining Work |
|---|---|---|---|
| ADR | Proposed | This document | Review and accept/revise decision |
| Phase 0: Shared Dependency Neighborhood Contract | Not started | Existing code relationship API, content relationship builders, and individual impact routes; the shared `/api/v0/graph/neighborhood` contract is still absent. | Add shared route/MCP response and UI backed by documented query surfaces |
| Phase 1: Terraform/Terragrunt | Started | Dead-IaC product-truth fixtures and full-corpus proofs already materialize Terraform/Terragrunt reachability rows, but the refactor-impact traversal and path-validation queries are still incomplete. | Add traversal references, path validation, usage edges, queries |
| Phase 2: Kustomize/ArgoCD | Started | Existing parser buckets and content-backed relationships. Commit `e4cb8cbf` adds Kustomize base/overlay reachability fixture coverage and treats ArgoCD source paths plus `kustomization.yaml` resources as static roots/references for dead-IaC classification. | Promote the current static reachability proof into the broader durable usage graph and impact/integrity queries. |
| Phase 3: Helm | Started | Existing chart and values extraction. Helm chart and values artifacts already appear in the materialized dead-IaC proofs, but Go-template value references and renderer-backed reachability are still incomplete. | Add Go-template value references, values-file usage, optional rendering |
| Phase 4: Kubernetes | Not started | Existing K8s resource extraction and basic Service heuristic | Add typed resource usage edges and conservative orphan findings |
| Phase 5: CloudFormation/Crossplane/Compose/Ansible | Started | Existing parser and evidence surfaces. Commits through `c965dd52` added Ansible role/playbook static reachability to the product-truth fixture set. Commit `e06c47d6` adds Docker Compose service artifacts plus static and dynamic `docker compose` command references, proving used, unused, and ambiguous Compose service classifications with focused Go tests and the checked-in NornicDB Compose verifier. | Continue CloudFormation/Crossplane family work and promote local-host/full-corpus IaC reachability finalization so non-bootstrap runs can serve materialized rows. |
| Code dead-code / IaC boundary | Guarded | Query contract now reports `iac_reachability_mode=not_modeled_by_code_dead_code` and `iac_deadness_capability=iac_usage.reachability`; tests prove Terraform, Helm, Kustomize, Kubernetes, and ArgoCD graph rows are not returned as code dead-code candidates. | Implement the first-class IaC usage/reachability graph before exposing dead-IaC cleanup findings. |
| Product-truth fixture contract | Runtime-proven | `tests/fixtures/product_truth/manifest.json` now registers owned graph-analysis, correlation-DSL, and relationship-platform suites plus the planned `iac_quality.dead_iac` capability. `tests/fixtures/product_truth/dead_iac/` adds generic Terraform, Helm, Ansible, Kustomize, and Compose fixture repos with used, unused, and ambiguous cases, and `tests/fixtures/product_truth/expected/dead_iac.json` records the expected truth assertions. `scripts/verify_product_truth_fixtures.sh` verifies the corpus and expected assertions statically. Remote proof `eshu-dead-iac-runtime-20260501120421` copied the first fixture set as real Git repos, rebuilt the Compose image, and validated API/MCP/Postgres against the expected cleanup assertions. Commits `e4cb8cbf` and `c965dd52` add `scripts/verify_dead_iac_compose.sh`; remote proof `eshu-dead-iac-script-20260501` ran the checked-in verifier against NornicDB with eight copied Git fixture repos, queue `70/70` succeeded, and API/MCP/Postgres matched the golden assertions. Commit `e06c47d6` expands the same verifier to ten real Git fixture repos including Compose; remote proof `eshu-dead-iac-compose-20260501` rebuilt the image, drained queue `86/86`, captured raw graph nodes/relationships/deployment evidence, and returned API/MCP `truth_basis=materialized_reducer_rows`, `analysis_status=materialized_reachability`, and `findings_count=10`. | Keep the checked-in Compose verifier as the product-truth gate for future dead-IaC family additions. |
| Phase 1 API candidate surface | Runtime-proven | `POST /api/v0/iac/dead` now exposes a bounded `derived` candidate surface for explicit `repo_id`/`repo_ids` scopes and prefers reducer-materialized rows when present. Tests prove Terraform, Helm, Ansible, Kustomize, and Compose used artifacts are not returned, unused artifacts return `candidate_dead_iac`, and dynamic references return `ambiguous_dynamic_reference` when requested. Commit `4a57504b` resolves repo-name selectors to canonical repo IDs before querying durable reachability rows, matching the runtime repository catalog contract; `e4cb8cbf` includes `repo_name` in materialized findings. Remote API proof `eshu-dead-iac-script-20260501` returned `truth_basis=materialized_reducer_rows`, `analysis_status=materialized_reachability`, and `findings_count=8`; Compose proof `eshu-dead-iac-compose-20260501` returned the same materialized contract with `findings_count=10`. Full-corpus local-host read validation at `e06c47d6` returned derived content-scope findings over the 878-repo explicit scope in `3s`, but `iac_reachability_rows` remained empty because local-host completion did not run bootstrap reachability finalization. Commit `2f75bc3a` fixes that local-host gap; focused remote proof `local-iac-finalizer-20260501T133332Z` drained `87/87` work rows in `12s` and API returned `truth_basis=materialized_reducer_rows`, `analysis_status=materialized_reachability`, and `findings_count=10`. Full-corpus proof `eshu-full-finalizer-20260501T134928Z` rebuilt binaries at `71cb002b`, drained `8446/8446` work rows in `650s`, materialized `803` IaC reachability rows, and `/api/v0/iac/dead` returned `truth_basis=materialized_reducer_rows` with `analysis_status=materialized_reachability` over the materialized scope. | Keep the bounded content fallback for local/fixture use, but treat materialized rows as the owned exact path. The API currently caps the full-corpus materialized findings response at `500`, so future UX/API work should expose pagination or grouped summaries for large cleanup sets. |
| Phase 1 materialized storage | Runtime-proven | `iac_reachability_rows` bootstrap schema and `IaCReachabilityStore` now persist used, unused, and ambiguous IaC artifact rows with evidence and limitations JSON. Commit `63d00643` wires bootstrap finalization to materialize active-corpus rows after source-local projection drains; `ff420289` mounts the same route in MCP so `find_dead_iac` reaches the materialized API path. Remote NornicDB-backed proof `eshu-dead-iac-runtime-20260501120421` logged `iac reachability materialized` with row_count `10`, direct Postgres counts `used=4`, `unused=3`, `ambiguous=3`, and API/MCP materialized findings_count `6`. Checked-verifier proof `eshu-dead-iac-script-20260501` expanded this to row_count `14`, materialization duration `0.006148294s`, direct counts `used=6`, `unused=4`, `ambiguous=4`, and API/MCP materialized findings_count `8`. Compose proof `eshu-dead-iac-compose-20260501` expanded this again to row_count `18`, materialization duration `0.00584195s`, direct counts `used=8`, `unused=5`, `ambiguous=5`, and API/MCP materialized findings_count `10`. Commit `2f75bc3a` adds the same one-shot finalization to `local_authoritative` graph runs after the queue and shared projections drain; focused proof `local-iac-finalizer-20260501T133332Z` produced `18` persisted rows across Terraform, Helm, Ansible, Kustomize, and Docker Compose (`used=8`, `unused=5`, `ambiguous=5`). Full-corpus proof `eshu-full-finalizer-20260501T134928Z` produced `803` persisted rows: Ansible unused `413`; Compose ambiguous `7` and unused `46`; Terraform used `51`, ambiguous `13`, and unused `273`. | Expand future reachability families while preserving canonical repo IDs, repo names, and materialized evidence rows as the query source of truth; remaining family gaps are CloudFormation, Crossplane, deeper Helm template/value rendering, Argo ApplicationSet templating, and conservative Kubernetes orphan semantics. |

---

## Open Questions

1. Should the public cleanup route be named `/api/v0/iac/dead`,
   `/api/v0/iac/unused`, or `/api/v0/iac/reachability`?
2. Should refactor-impact support path selectors only at first, or also
   semantic selectors such as `module.vpc`, `.Values.image.tag`, and
   `apps/v1 Deployment payments/api`?
3. Which renderer profile should be authoritative for local runs:
   parser-only, renderer-available, or full compose/local-authoritative?
4. Should Helm rendering run during ingestion, during reducer materialization,
   or lazily during query?
5. Should exact dead-IaC answers require every referenced repository to be
   indexed in the same generation set?
6. Should Dependency Explorer ship first in a rebuilt editor client, the web
   UI, or both behind the same `/api/v0/graph/neighborhood` contract?
