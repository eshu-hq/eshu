# ADR: CI/CD Relationship Parity Across Delivery Families

**Date:** 2026-04-19
**Status:** Accepted with follow-up
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

---

## Status Review (2026-05-03)

**Current disposition:** Accepted with follow-up.

The relationship-family extraction and read surfaces are real. Tests cover
GitHub Actions reusable workflows, Jenkins/Ansible controller evidence, Docker
Compose build/image/dependency signals, ArgoCD ApplicationSet discovery,
Terragrunt module evidence, and Terraform-backed delivery signals.

**Remaining work:** the parity matrix still marks several families as partial.
Broaden real-corpus proof and finish service-story integration for
controller-driven services before calling delivery-family parity complete.

## Context

The Go data plane now indexes 896 repositories and extracts 18 distinct
evidence kinds from CI/CD, IaC, and GitOps artifacts. After the P0 query-layer
fixes (commit `7e58a11b`), the cross-repo search, entity resolution, and
deployment trace tools work for the first time across the full corpus.

This ADR tests whether the **relationship extraction and query synthesis** for
each supported CI/CD delivery family produces operator-useful results compared
to the QA Python instance. The test covers every evidence family present in the
E2E database, not just the ArgoCD/GHA path that dominated earlier testing.

The live evidence shows two different parity problems that should not be
collapsed into one:

1. **Service synthesis parity**: materializing real service workloads, instances,
   environments, entrypoints, and delivery stories from mixed controller and
   GitOps evidence.
2. **Infrastructure relationship parity**: extracting and surfacing
   cross-repository Terraform, Terragrunt, ArgoCD, Docker, Ansible, and Jenkins
   relationships for provisioning and platform repos.

E2E is already ahead of QA on several infrastructure relationship families.
The primary remaining gap is service-level synthesis for legacy and mixed-mode
delivery systems, especially Jenkins-backed services.

---

## Evidence Inventory

### Evidence Kinds In Production (E2E Postgres)

| Evidence Kind | Count | Relationship Type |
|---|---|---|
| `TERRAFORM_MODULE_SOURCE` | 6,066 | USES_MODULE |
| `TERRAFORM_CONFIG_PATH` | 1,962 | PROVISIONS_DEPENDENCY_FOR |
| `GITHUB_ACTIONS_REUSABLE_WORKFLOW` | 656 | DEPLOYS_FROM |
| `TERRAFORM_APP_REPO` | 538 | PROVISIONS_DEPENDENCY_FOR |
| `GITHUB_ACTIONS_WORKFLOW_INPUT_REPOSITORY` | 309 | DISCOVERS_CONFIG_IN |
| `HELM_VALUES_REFERENCE` | 280 | DEPLOYS_FROM |
| `KUSTOMIZE_RESOURCE_REFERENCE` | 257 | DEPLOYS_FROM |
| `TERRAFORM_GITHUB_REPOSITORY` | 229 | PROVISIONS_DEPENDENCY_FOR |
| `GITHUB_ACTIONS_ACTION_REPOSITORY` | 193 | DEPENDS_ON |
| `ARGOCD_APPLICATIONSET_DISCOVERY` | 148 | DEPLOYS_FROM |
| `TERRAFORM_APP_NAME` | 45 | PROVISIONS_DEPENDENCY_FOR |
| `TERRAFORM_GITHUB_ACTIONS_REPOSITORY` | 37 | PROVISIONS_DEPENDENCY_FOR |
| `GITHUB_ACTIONS_LOCAL_REUSABLE_WORKFLOW` | 31 | DEPLOYS_FROM |
| `HELM_CHART_REFERENCE` | 9 | DEPLOYS_FROM |
| `JENKINS_GITHUB_REPOSITORY` | 4 | DISCOVERS_CONFIG_IN |
| `GITHUB_ACTIONS_CHECKOUT_REPOSITORY` | 2 | DISCOVERS_CONFIG_IN |
| `ANSIBLE_ROLE_REFERENCE` | 1 | DEPENDS_ON |
| `DOCKER_COMPOSE_DEPENDS_ON` | 1 | DEPENDS_ON |

**Total: 10,768 evidence facts across 18 kinds.**

### Artifact Types Extracted (Content Store)

| Artifact Type | Description |
|---|---|
| `ansible_inventory` | Ansible inventory files |
| `ansible_playbook` | Ansible playbook YAML |
| `ansible_role` | Ansible role directories |
| `docker_compose` | Docker Compose service definitions |
| `dockerfile` | Docker build/runtime definitions |
| `generic_config` | Configuration files (ansible.cfg, etc.) |
| `go_template_yaml` | Go-templated YAML (Helm, etc.) |
| `github_actions_workflow` | GitHub Actions workflow definitions |
| `terraform_hcl` | Terraform HCL files |

### IaC Entity Types Extracted (Content Store)

| Category | Entity Type | Count | IaC Relevant |
|---|---|---|---|
| **Terraform** | TerraformVariable | 69,136 | Yes |
| | TerraformResource | 36,242 | Yes |
| | TerraformLocal | 31,148 | Yes |
| | TerraformDataSource | 18,130 | Yes |
| | TerraformOutput | 14,168 | Yes |
| | TerraformModule | 6,256 | Yes |
| | TerraformProvider | 5,370 | Yes |
| **Terragrunt** | TerragruntInput | 268 | Yes |
| | TerragruntLocal | 35 | Yes |
| | TerragruntConfig | 26 | Yes |
| | TerragruntDependency | 5 | Yes |
| **Kubernetes** | K8sResource | 591 (521 IaC) | Yes |
| | KustomizeOverlay | 289 (236 IaC) | Yes |
| **Helm** | HelmValues | 193 (192 IaC) | Yes |
| | HelmChart | 11 | Yes |
| **ArgoCD** | ArgoCDApplicationSet | 87 | Yes |
| | ArgoCDApplication | 15 (12 IaC) | Yes |
| **CloudFormation** | CloudFormationResource | 78 | No |
| | CloudFormationParameter | 56 | No |
| | CloudFormationOutput | 10 | No |
| **Crossplane** | CrossplaneClaim | 10 | No |
| | CrossplaneComposition | 9 (2 IaC) | Partial |
| | CrossplaneXRD | 8 | No |
| **SQL** | SqlTable | 796 | No |
| | SqlColumn | 9,720 | No |
| | SqlView | 38 (7 IaC) | Partial |

**Total IaC-relevant entities: ~272,000** across 7 IaC families.

**Total code entities: ~3.2M** (Variable: 2.2M, Function: 690k, Class: 47k).

### Delivery Controller Families Detected

| Controller | Delivery Mode | Evidence Source |
|---|---|---|
| Jenkins | `jenkins_pipeline` | Jenkinsfile parsing (shared libs, pipeline calls, shell commands) |
| GitHub Actions | `github_actions_workflow` | Workflow YAML parsing (reusable workflows, actions, inputs) |
| ArgoCD | `argocd_applicationset` | ApplicationSet/Application YAML parsing |
| CloudFormation | `cloudformation_serverless` | SAM/CloudFormation template parsing |
| Ansible | (artifact extraction only) | Role/playbook/task/vars/inventory extraction |
| Docker Compose | (artifact extraction only) | Service dependency and build context extraction |
| Terraform | (IaC evidence) | Module source, provider, resource extraction |
| Terragrunt | (IaC evidence) | Dependency, include, config asset extraction |
| Kustomize | (overlay evidence) | Resource reference, Helm chart, image extraction |
| Helm | (chart evidence) | Chart reference, values reference extraction |

---

## Test Results By Delivery Family

### 1. GitHub Actions (GHA) — `node-service-a`

**Evidence:** 6 GITHUB_ACTIONS_REUSABLE_WORKFLOW facts pointing at
`core-engineering-automation`, plus ArgoCD ApplicationSets in
`iac-eks-argocd` and Terraform provisioning chains.

| Capability | E2E | QA | Verdict |
|---|---|---|---|
| Workload materialized | Yes, but no materialized instances/environments in service story | Yes | QA ahead |
| Service story | 200, but "No materialized instances" and empty environments | 2 instances, 21 endpoints, hostnames | QA ahead |
| Deployment trace | Rich, but noisy: 19 endpoints plus many false-positive hostnames and over-broad delivery paths | Rich structured trace | QA ahead for operator safety |
| Cross-repo relationships | DEPLOYS_FROM, DISCOVERS_CONFIG_IN to core-engineering-automation | Same | Tie |
| Consumer discovery | 7 provisioning repos plus many content/hostname consumers in trace output | 12 repos (hostname + name matching) | Different signals, complementary |
| Hostname extraction | True hostnames present, but mixed with code symbols and test strings | Clean public hostnames in service story | QA ahead |
| API surface | 19 endpoints, `/_specs` docs route | 21 endpoints | Near parity on extraction |

**Assessment: E2E has strong raw extraction and trace enrichment for
`node-service-a`, but it is not yet service-story parity. The trace is rich,
not clean.**

### 2. Jenkins Pipeline — `node-service-b`

**Evidence:** Jenkinsfile with `@Library('pipelines') _ ; pipelinePM2(...)`,
`JENKINS_GITHUB_REPOSITORY` evidence, ArgoCD ApplicationSets in `iac-eks-argocd`.

| Capability | E2E | QA | Verdict |
|---|---|---|---|
| Workload materialized | **No** (`workload_count: 0`) | Yes | **E2E BROKEN** |
| Service story | **HTTP 404** | Rich: 2 instances, 26 API endpoints, Jenkins delivery, dependencies | **E2E BROKEN** |
| Service context | **HTTP 404** | Works | **E2E BROKEN** |
| Deployment trace | **HTTP 404** | Rich: 2 ArgoCD ApplicationSets, 71 endpoints, Jenkinsfile, consumer repos | **E2E BROKEN** |
| Repo summary | Works: 896 files, Jenkinsfile artifacts (`pipelinePM2`, entry `dist/node-service-b.js`, `use_configd: true`), Dockerfile, docker-compose | Works: similar + delivery paths + topology story | E2E has good artifacts but no service path |
| Jenkins artifact extraction | Shared library `pipelines`, pipeline call `pipelinePM2`, entry point, configd flag | Same level of detail | Tie on extraction |
| Relationships | None surfaced (no workload = no relationship path) | Dependencies: api-node-ai-provider, node-service-a | **E2E BROKEN** |

**Root cause:** the reducer only creates workload candidates from repo-local
file facts containing `k8s_resources`, `argocd_applications`, or
`argocd_applicationsets`. Cross-repo deployment repo enrichment happens later,
after resolved Argo deployment evidence is available. `node-service-b`
has no repo-local K8s or Argo signal strong enough to become a workload
candidate, even though related deployment evidence exists in `helm-charts` and
`iac-eks-argocd`. QA compensates with broader query-time synthesis across
controller, deployment artifact, and cross-repo evidence.

**Assessment: Jenkins-deployed services without catalog-info.yaml are
completely broken on E2E. This affects a large portion of the corpus since
Jenkins is a legacy delivery system and many services lack catalog-info.yaml.**

### 3. Jenkins Pipeline — `jenkins-pipeline-configd-parameter-store`

**Evidence:** `JENKINS_GITHUB_REPOSITORY` → `configd` (DISCOVERS_CONFIG_IN).

| Capability | E2E | QA | Verdict |
|---|---|---|---|
| Repo summary | Works: Jenkinsfile with shell commands (`pipenv install`, `pipenv run python sync.py configd/server/config/...`), relationship to configd via jenkins_github_repository | Works: Jenkins delivery path, deployment facts, topology story | E2E has richer artifact detail |
| Cross-repo relationship | **DISCOVERS_CONFIG_IN configd** — correctly extracted and surfaced in relationship_overview | Not surfaced in relationships section | **E2E ahead** — surfaces the Jenkins→configd config discovery relationship |
| Relationship story | "Controller-driven relationships: DISCOVERS_CONFIG_IN configd via jenkins_github_repository" | "Jenkins deploys." | **E2E ahead** — richer relationship narrative |
| Delivery path | Not synthesized as delivery_path | Jenkins pipeline delivery path detected | QA synthesizes delivery path |

**Assessment: E2E extracts Jenkins cross-repo relationships better than QA for
this class of pipeline utility repo. QA synthesizes delivery paths better.**

### 4. Ansible — `ansible-mac-builder`

**Evidence:** `ANSIBLE_ROLE_REFERENCE` → `ansible-role-nvm` (DEPENDS_ON).

| Capability | E2E | QA | Verdict |
|---|---|---|---|
| Repo summary | Works: **26 ansible artifacts** (7 roles with vars/defaults/tasks, 3 playbooks), relationship to ansible-role-nvm | Works: 28 files but **no ansible artifacts detected**, no delivery paths, no relationships | **E2E massively ahead** |
| Ansible role extraction | 7 roles: gem, git, homebrew, mas, ssh, xcode, zsh — each with defaults, tasks, vars | Zero ansible evidence | **E2E only** |
| Cross-repo relationship | **DEPENDS_ON ansible-role-nvm** via ansible_role_reference | Nothing | **E2E only** |
| Relationship story | "Controller-driven relationships: DEPENDS_ON ansible-role-nvm via ansible_role_reference" | No story | **E2E only** |

**Assessment: E2E is categorically better than QA for Ansible repos. QA
completely misses Ansible artifact structure and cross-repo role dependencies.**

### 5. Ansible — `automate-mws-import-tasks`

| Capability | E2E | QA | Verdict |
|---|---|---|---|
| Repo summary | **46 ansible artifacts**: 3 playbooks, 13 roles, 11 task entrypoints, 6 vars, 1 inventory, Jenkins group_vars for environments | 37 files, Jenkins groovy detected, consumer repos, topology story | E2E richer on ansible structure |
| Consumer evidence | None | 1 consumer (github-settings) | QA ahead |
| Topology story | None | "Jenkins deploys." | QA synthesizes narrative |

### 6. Docker Compose — `portal-dmmwebsites`

**Evidence:** `DOCKER_COMPOSE_DEPENDS_ON` → `wordpress` (DEPENDS_ON).

| Capability | E2E | QA | Verdict |
|---|---|---|---|
| Repo summary | Works: 5 docker-compose services (nginx, mysql-server, wordpress, redis), 2 Dockerfiles with base images/ports/CMD, relationship to wordpress | Works: 243 files, no delivery paths, **7 consumer repos** with evidence, topology story | Mixed |
| Docker artifact extraction | Detailed: base images (`php:8.3-fpm-bullseye`), ports, environment vars, volumes, healthchecks, CMD | Nothing — zero docker artifacts | **E2E massively ahead** |
| Cross-repo relationship | **DEPENDS_ON wordpress** via docker_compose_depends_on | Nothing | **E2E only** |
| Consumer discovery | None | 7 consumers (automate-mws, website-scaffold, terraform-stack-mws-infrastructure, etc.) | QA ahead |
| Relationship story | "IaC-driven relationships: DEPENDS_ON wordpress via docker_compose_depends_on" | Topology story about consumers | Different signals |

**Assessment: E2E extracts Docker Compose structure (services, images, ports,
volumes, dependencies) that QA completely misses. QA finds consumer repos
through content search that E2E doesn't surface at the repo level.**

### 7. CloudFormation + Jenkins — `lambda-python-jenkins-job-management`

**Evidence:** SAM template with `AWS::Serverless::Function`, Jenkinsfile
with `pipelineSAM`.

| Capability | E2E | QA | Verdict |
|---|---|---|---|
| Repo summary | Works: CloudFormationResource `TestLambda` (AWS::Serverless::Function), Jenkinsfile (`pipelineSAM`, shared lib `pipelines`), entry_point `lambda_handler` in `app.py` | Works: same entity + **2 delivery paths** (Jenkins + CloudFormation serverless), topology story | QA ahead on delivery synthesis |
| Delivery paths | 1 controller artifact (Jenkinsfile) | **2 paths**: Jenkins pipeline + CloudFormation serverless from `template.yml` | **QA detects dual delivery** |
| CloudFormation entity | Detected as content entity | Detected + elevated to delivery path | QA ahead |
| Deployment facts | None | `MANAGED_BY_CONTROLLER: jenkins`, `DEPLOYS_FROM: template.yml` | QA ahead |

**Assessment: QA synthesizes dual delivery paths (Jenkins + CloudFormation)
and deployment facts. E2E extracts the entities but doesn't elevate
CloudFormation to a delivery path.**

### 8. ArgoCD — `iac-eks-argocd`

**Evidence:** 148 `ARGOCD_APPLICATIONSET_DISCOVERY` facts, 257
`KUSTOMIZE_RESOURCE_REFERENCE` facts.

| Capability | E2E | QA | Verdict |
|---|---|---|---|
| Infrastructure entities | **198 entities**: 87 ArgoCDApplicationSet, 3 ArgoCDApplication, 37 K8sResource, 53 KustomizeOverlay, 5 HelmValues, 13 Terraform | Not surfaced at this granularity | **E2E massively ahead** |
| Cross-repo relationship | DEPLOYS_FROM deployment-service-a via kustomize_resource_reference | None | **E2E ahead** |
| Consumer evidence | 2 consumers (iac-terragrunt-core-infra) | 3 consumers (argocd-env-generator, iac-terragrunt-core-infra, mobius-tools) | QA ahead |
| Families detected | argocd, github_actions, helm, kubernetes, kustomize, terraform | None | **E2E ahead** |
| GHA workflow | argocd-docs workflow with reusable_workflow_repositories, concurrency, permissions | Not surfaced | **E2E ahead** |

**Assessment: E2E is dramatically better for ArgoCD infrastructure repos.
198 classified entities vs QA showing none. QA finds consumer repos better.**

### 9. Terragrunt + Terraform — `iac-terragrunt-core-infra`

**Evidence:** Terragrunt configs referencing `iac-eks-argocd`,
`terraform-module-core-irsa`, `terraform-module-delegated-zone`, and
`terraform-module-eks`, plus GitHub Actions workflow artifacts.

| Capability | E2E | QA | Verdict |
|---|---|---|---|
| Cross-repo provisioning relationships | **8 surfaced relationships** across `PROVISIONS_DEPENDENCY_FOR` and `USES_MODULE` | Not surfaced at equivalent fidelity | **E2E ahead** |
| Terragrunt/Terraform config provenance | Rich include/read/config-asset paths | Terragrunt config presence only | **E2E ahead** |
| Workflow artifact surfacing | GHA workflow actions, permissions, concurrency, commands | Not surfaced equivalently | **E2E ahead** |
| Consumer discovery | None | 1 consumer (`mobius-tools`) | QA ahead |

**Assessment: E2E is already ahead for provisioning-repo relationship closure.
The main parity issue is not IaC relationship extraction; it is service
synthesis and operator-safe story construction.**

---

## Systematic Findings

### Where E2E Is Ahead Of QA

| Area | Evidence |
|---|---|
| **Ansible artifact extraction** | 26-46 classified artifacts per repo vs zero on QA |
| **Docker Compose extraction** | Services, images, ports, volumes, healthchecks vs nothing on QA |
| **Jenkins artifact detail** | Shell commands, shared libraries, pipeline calls, entry points, configd flags |
| **ArgoCD/Kustomize infrastructure** | 198 classified entities for iac-eks-argocd vs none on QA |
| **Cross-repo relationship surfacing** | Explicit relationship_overview with evidence kind, target repo, and relationship type |
| **Ansible cross-repo dependencies** | ANSIBLE_ROLE_REFERENCE → DEPENDS_ON correctly resolved |
| **Docker Compose dependencies** | DOCKER_COMPOSE_DEPENDS_ON → DEPENDS_ON correctly resolved |
| **Jenkins config discovery** | JENKINS_GITHUB_REPOSITORY → DISCOVERS_CONFIG_IN correctly resolved |
| **Terragrunt/Terraform relationship closure** | `PROVISIONS_DEPENDENCY_FOR` and `USES_MODULE` are already surfaced across stack and module repos |

### Where QA Is Ahead Of E2E

| Area | Evidence |
|---|---|
| **Workload materialization for controller-driven services** | QA creates workloads from broader controller and deployment evidence; E2E still depends on repo-local K8s/Argo candidate signals |
| **Service story for Jenkins services** | E2E returns 404 for any service without materialized workload |
| **Consumer repository discovery** | QA finds 7+ consumers via content search; E2E finds 0-2 at repo level |
| **Delivery path synthesis** | QA creates structured delivery_paths with controller, mode, summary; E2E stores controller_artifacts but doesn't always synthesize paths |
| **Dual delivery path detection** | QA detects Jenkins + CloudFormation as separate delivery paths; E2E shows only one |
| **Topology story narrative** | QA generates operator-readable stories; E2E has relationship_overview but no narrative synthesis |
| **Deployment facts with confidence** | QA produces MANAGED_BY_CONTROLLER, DEPLOYS_FROM facts with confidence bands |
| **Confidence scoring in resolve_entity** | QA returns ranked matches with inference/confidence; E2E returns flat entity list |
| **Operator-safe hostname and environment synthesis** | QA returns cleaner service hostnames and environments; E2E traces can over-match code literals and test strings |

### Critical Gap: Workload Materialization

The single most impactful gap is that E2E only materializes workloads from
repo-local workload candidate signals built out of `k8s_resources`,
`argocd_applications`, and `argocd_applicationsets`. This means:

- **Jenkins- and controller-driven services without repo-local K8s/Argo
  candidate signals** have no workload
- **No workload** → `get_service_story` returns 404
- **No workload** → `get_service_context` returns 404
- **No workload** → `trace_deployment_chain` returns 404
- **No workload** → no service-level relationship path

At the same time, the live evidence shows E2E is already ahead on several
infrastructure and provisioning relationship families. The problem is not “E2E
cannot build relationships”; the problem is “E2E cannot yet safely turn mixed
controller evidence into service workloads and service stories.”

This affects a significant portion of the corpus. The `node-service-b`
test case proves the gap is real and operator-impacting.

---

## Decision

### Recommended: Three-Phase Approach

The phases should optimize for **accuracy first**, then performance and
reliability. Relationship extraction that is already stronger than QA should be
preserved. The risky area is workload synthesis from weaker controller/runtime
signals.

#### Phase 1: Safe Workload Candidate Expansion And Classification

Extend workload materialization using a gated model:
1. Add **cross-repo Argo deployment evidence** as a first-class candidate
   source for service workloads when deployment repo evidence points back to an
   application repository.
2. Add **controller/runtime candidate scoring** for Jenkins, CloudFormation,
   Dockerfile, and Docker Compose signals, but do not materialize by signal
   presence alone.
3. Add **classification and negative controls** before materialization:
   `service` vs `job` vs `utility` vs `infrastructure`.
4. Require a minimum confidence threshold before a candidate becomes a
   workload.

This unblocks the service query path for legacy controller-driven services
without turning utility repositories into false services.

**Acceptance criteria:**
- `node-service-b` returns `workload_count: 1` in repo summary
- `get_service_story("workload:node-service-b")` returns 200
- `trace_deployment_chain("node-service-b")` returns structured result
- `jenkins-pipeline-configd-parameter-store` does **not** materialize as a
  service workload
- Newly materialized workloads include confidence and source attribution

#### Phase 2: Delivery Path Synthesis Parity

Elevate extracted controller artifacts into structured delivery paths:
1. Jenkins artifacts → `jenkins_pipeline` delivery path
2. CloudFormation entities → `cloudformation_serverless` delivery path
3. Cross-repo Argo deployment evidence → GitOps deployment path
4. Docker Compose build contexts → development/runtime path, not production
   service deployment by default
5. Detect dual/multi-delivery when Jenkins + CloudFormation or Jenkins + ArgoCD
   coexist

**Acceptance criteria:**
- `lambda-python-jenkins-job-management` shows 2 delivery paths
- `node-service-b` shows Jenkins delivery path in repo summary
- Deployment facts generated with confidence bands
- `node-service-a` no longer reports code literals and test symbols as public
  hostnames in deployment trace output

#### Phase 3: Consumer Discovery And Topology Narrative

Wire cross-repo content search into consumer discovery at the repo/service level:
1. Content-search-based consumer repos (hostname + name matching)
2. Topology story synthesis from delivery paths + consumer evidence
3. Deployment fact confidence aggregation
4. Keep provisioning and infra relationship families intact while layering
   operator-safe summaries on top

**Acceptance criteria:**
- `portal-dmmwebsites` shows consumer repos in E2E repo summary
- Topology story generated for repos with delivery evidence
- `iac-terragrunt-core-infra` and `iac-eks-argocd` retain existing explicit
  relationship coverage while gaining clearer operator summaries

---

## Consequences

### Positive
- Jenkins-deployed services (large portion of corpus) become queryable
- Ansible/Docker Compose artifact extraction advantages are preserved
- Multi-delivery services (Jenkins + CloudFormation) properly represented
- Operator service investigation workflow works across all delivery families
- Existing E2E advantage on Terraform/Terragrunt/ArgoCD relationship extraction
  is preserved instead of regressed

### Negative
- Workload materialization from delivery evidence has lower confidence than
  from explicit K8s manifests — needs confidence bands
- More workloads means more reduction work during bootstrap
- Dual-delivery detection adds complexity to the deployment trace
- Hostname and environment synthesis needs filtering to avoid code-literal and
  test-fixture noise

### Risks
- False-positive workload materialization from Jenkinsfile presence in utility
  repos (e.g., `jenkins-pipeline-configd-parameter-store` is a utility, not a
  service)
- Need workload kind classification: `service` vs `job` vs `utility` vs
  `infrastructure`
- Query-time trace enrichment can over-match hostnames and consumer signals if
  not constrained by provenance and confidence

---

## Appendix: Test Repos And Evidence

| Repo | CI/CD Family | Evidence Kind | E2E Relationship | QA Relationship |
|---|---|---|---|---|
| `node-service-a` | GHA | GITHUB_ACTIONS_REUSABLE_WORKFLOW | DEPLOYS_FROM core-engineering-automation | Same |
| `node-service-b` | Jenkins + ArgoCD | JENKINS (Jenkinsfile) | **No workload — 404** | 2 instances, 26 endpoints, Jenkins delivery |
| `jenkins-pipeline-configd-parameter-store` | Jenkins | JENKINS_GITHUB_REPOSITORY | DISCOVERS_CONFIG_IN configd | Jenkins delivery path only |
| `ansible-mac-builder` | Ansible | ANSIBLE_ROLE_REFERENCE | DEPENDS_ON ansible-role-nvm | **Nothing** |
| `automate-mws-import-tasks` | Ansible + Jenkins | Ansible artifacts | 46 ansible artifacts, no relationships | Jenkins delivery, 1 consumer |
| `portal-dmmwebsites` | Docker Compose | DOCKER_COMPOSE_DEPENDS_ON | DEPENDS_ON wordpress | **Nothing** (7 consumers found separately) |
| `lambda-python-jenkins-job-management` | Jenkins + CloudFormation | CloudFormation entity | 1 controller artifact | **2 delivery paths** (Jenkins + CloudFormation) |
| `iac-eks-argocd` | ArgoCD + Kustomize + Terraform + GHA | 148 ARGOCD + 257 KUSTOMIZE | 198 infra entities, DEPLOYS_FROM deployment-service-a | 3 consumers, topology story |
| `iac-terragrunt-core-infra` | Terragrunt + Terraform + GHA | TERRAFORM_MODULE_SOURCE, TERRAFORM_GITHUB_REPOSITORY | `PROVISIONS_DEPENDENCY_FOR` + `USES_MODULE` across 4 repos | Terragrunt config presence + 1 consumer |
