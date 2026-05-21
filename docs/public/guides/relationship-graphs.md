# Relationship Graph Examples

These diagrams show the kinds of deployment and dependency paths Eshu can explain once a repository corpus is indexed.

They are intentionally sanitized for public documentation, but each pattern below is based on real end-to-end acceptance runs against mixed code, infrastructure, GitOps, Terraform, Terragrunt, Jenkins, and Ansible repos.

If you want the underlying semantics behind these examples, start with the [Relationship Mapping reference](../reference/relationship-mapping.md).

## Why These Examples Matter

The hard part is usually not finding one file. It is connecting the full path:

- which repo owns the service
- which repo holds the deployment config
- which platform it actually runs on
- which infra repo provisions it
- which controller or automation flow moves it there

These examples show the shapes Eshu can reconstruct from that evidence.

## Service With Dual Runtime Paths

One service can legitimately have more than one deployment path. Eshu should preserve that instead of flattening everything into one generic dependency.

```mermaid
graph TD
    CI[CI/CD Workflows]
    APP[service-api]
    CHARTS[deployment-charts]
    K8S[Platform: Kubernetes cluster-a]
    TFSTACK[runtime-stack-ecs]
    ECS[Platform: ECS cluster-b]

    APP -->|DEPLOYS_FROM| CHARTS
    APP -->|RUNS_ON| K8S
    APP -->|RUNS_ON| ECS
    TFSTACK -->|PROVISIONS_DEPENDENCY_FOR| APP

    CI -->|deploys from| CHARTS
    CI -->|deploys through| TFSTACK
```

Read this as:

- the service deploys from a charts or manifest repo onto Kubernetes
- the same service also has a Terraform-driven ECS path
- the infra stack provisions dependency context for the service

## GitOps Control Plane Discovery

Control-plane repos are often about configuration discovery, not direct service ownership. Eshu keeps that distinction explicit.

```mermaid
graph TD
    GITOPS[gitops-controller]
    CHARTS[deployment-charts]
    ADDONS[platform-addons]
    CROSS[platform-crossplane]
    OBS[platform-observability]
    CFG[platform-service-config]

    GITOPS -->|DISCOVERS_CONFIG_IN| CHARTS
    GITOPS -->|DISCOVERS_CONFIG_IN| ADDONS
    GITOPS -->|DISCOVERS_CONFIG_IN| CROSS
    GITOPS -->|DISCOVERS_CONFIG_IN| OBS
    GITOPS -->|DISCOVERS_CONFIG_IN| CFG
```

Read this as:

- the GitOps repo acts as a control plane
- it discovers deployment configuration in multiple config repos
- this is different from saying it directly owns the application runtime

## Jenkins And Ansible Automation Path

Not every deployment estate is GitHub Actions plus Kubernetes. Eshu can also surface controller-driven automation paths such as Jenkins invoking Ansible against a website fleet.

```mermaid
graph TD
    J1[Jenkins controller]
    AUTO[website-automation]
    J2[Jenkins controller]
    TASKS[website-task-runner]
    WP[Runtime family:<br/>website_fleet]

    J1 -->|Ansible deploy entrypoints| AUTO
    AUTO -->|targets environment hosts| WP

    J2 -->|Ansible task entrypoints| TASKS
    TASKS -->|targets sync/import hosts| WP
```

Read this as:

- Jenkins acts as the controller
- Ansible playbooks and task entrypoints express the automation path
- runtime-family hints identify the target as a website-fleet style platform

## Terragrunt And Terraform Module Source Chains

Terragrunt orchestration repos can encode important upstream relationships even when the service repo itself never references those module repos directly.

```mermaid
graph TD
    TG[platform-terragrunt-live]
    CTRL[gitops-controller]
    IRSA[terraform-module-identity]
    DNS[terraform-module-dns]

    TG -->|Terragrunt source wrapper| CTRL
    TG -->|Terragrunt source wrapper| IRSA
    TG -->|Terragrunt source wrapper| DNS

    TG -->|PROVISIONS_DEPENDENCY_FOR| CTRL
    TG -->|PROVISIONS_DEPENDENCY_FOR| IRSA
    TG -->|PROVISIONS_DEPENDENCY_FOR| DNS
```

Read this as:

- Terragrunt source blocks can reveal upstream controller and module dependencies
- those source chains enrich the repo-level provisioning story
- Eshu can surface both the raw source-chain evidence and the higher-level dependency meaning

## How To Use These In Practice

For day-to-day analysis:

1. Start with the top-level `story` in `get_repo_summary` or `trace_deployment_chain`.
2. Use `deployment_overview` to inspect grouped supporting context.
3. Drop to detailed relationship fields only when you need exact source repos, artifacts, or paths.

That order keeps the answer readable while preserving the evidence needed for debugging or review.
