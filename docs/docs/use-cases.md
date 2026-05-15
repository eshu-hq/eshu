# Use Cases

Every use case below is a question that takes minutes to answer manually — grepping across repos, reading Terraform state, checking ArgoCD, asking a senior engineer — and seconds with Eshu.

## Before you merge

You are about to change a service's API contract. What breaks?

```bash
eshu analyze find-blast-radius payment-service
```

`find_blast_radius` walks transitive dependencies across repos and infrastructure boundaries: downstream services, shared Terraform modules, Crossplane claims, queue consumers. You see the full impact surface before you open the PR, not after the page.

To scope it to just the files and functions touched by a change:

```bash
eshu analyze find-change-surface --repo /path/to/repo --paths src/api/contracts.py
```

`find_change_surface` shows what depends on the specific code you modified — callers, importers, and infrastructure that references the changed module.

## During a production incident

A database is unhealthy. Which services use it, and how are they deployed?

`investigate_resource` starts from a cloud resource, queue, database, Terraform
resource, or Kubernetes object, resolves ambiguity first, then returns workload
users, repository provenance paths, source handles, and next calls:

```
→ investigate_resource payment-db
  RDS instance: payment-db
  ← Terraform module: terraform-modules/rds (repo: infra-modules)
  ← Referenced by: payment-service/main.tf, billing-service/main.tf
  ← Workloads: payment-service (ArgoCD), billing-service (ArgoCD)
```

Use `trace_resource_to_code` when you already have the canonical graph resource
ID and only need repository paths.

`trace_deployment_chain` goes the other direction — from a service name through controller/platform evidence, deployment-source repositories, and backing infrastructure evidence.

For MCP and API callers, start with the top-level `story` field from `trace_deployment_chain` or `get_repo_summary`, then drill into `deployment_overview` and the detailed fields if you need the exact evidence rows.

That applies to controller-driven automation estates too. If a platform uses Jenkins plus Ansible, start with `story`, then inspect `deployment_overview`, `delivery_paths`, and `controller_driven_paths` before dropping to raw workflow, playbook, or inventory evidence.

For evidence-first automation, use the normalized deployment mapping layer too:

- start with `deployment_fact_summary` to see what type of evidence Eshu actually found
- inspect `deployment_facts` for normalized facts
- then drill into controller/runtime/raw rows only if needed

This is especially useful in mixed estates:

- ArgoCD or Flux on Kubernetes
- Terraform Helm provider or Terraform Kubernetes provider
- CloudFormation stack sets or serverless deployments
- ECS, EKS, Lambda, or plain manifests with no controller at all

If Eshu only has delivery/runtime evidence, it will return `mapping_mode=evidence_only` instead of inventing a controller family.

If Eshu does not yet have enough indexed deployment evidence to map the path truthfully, it now returns `mapping_mode=none` with limitations like `deployment_evidence_missing` instead of silently dropping the contract.

## Onboarding a new engineer

Day one. A new engineer needs to understand how the payment service fits into the platform.

`get_service_context` returns a structured overview: what the service depends on, what depends on it, how it is deployed, and what infrastructure it consumes. `get_repo_context` does the same scoped to a single repository.

To understand a specific connection:

```
→ explain_dependency_path payment-service shared-rds-cluster
  payment-service → main.tf (module "db") → terraform-modules/rds → shared-rds-cluster
  Evidence: module source URL match + resource name correlation
```

`explain_dependency_path` shows why two entities are connected, with evidence for each hop. No tribal knowledge required.

Story-first documentation now works the same way. Start with `get_service_story`,
`get_workload_story`, or `get_repo_story`, read the high-level `story`, then use
the structured dossier fields such as `investigation`, `gitops_overview`,
`documentation_overview`, and `support_overview` to shape an onboarding doc,
deployment explainer, or support guide. Only after that should a client call
`build_evidence_citation_packet` with the returned file and entity handles for
exact README, runbook, values, manifest, or source evidence.

When the answer spans code, GitOps, and runtime systems, the best prompt is not
just "explain this service." It is "scan all related repositories, deployment
sources, and indexed documentation for this service, then explain it." That
explicitly tells Eshu to use its cross-repo graph and content-store strengths
instead of behaving like a repo-local assistant.

For that exact workflow, prefer `investigate_service` first when you need to
inspect coverage before writing the final answer. `get_service_story` embeds the
same investigation packet for the normal one-call story path, but
`investigate_service` is designed for the cases where a user should not need to
know:

- which Terraform stack to search
- whether the app repo `.github/` workflows matter
- whether the service has one deployment plane or multiple
- which repo to drill into next

The investigation result reports both findings and coverage, so you can tell
whether Eshu:

- found the right deployment-adjacent repos
- found evidence but still has a partial story
- needs another drill-down call

This is the practical split to keep in mind:

- use story/context routes when you want the canonical answer first
- use investigation when you want widening, evidence inspection, and explicit coverage reporting before the answer
- use `build_evidence_citation_packet` with `repo_id + relative_path` or
  `entity_id` handles for exact follow-up evidence, not server-local paths

## Comparing environments

Staging is broken but prod works. What is different?

```
→ compare_environments payment-service prod staging
  Resources only in prod:
    - sqs-queue: payment-dlq (dead letter queue)
  Config differences:
    - replicas: 3 (prod) vs 1 (staging)
    - env.DATABASE_URL: prod-rds vs staging-rds
```

`compare_environments` diffs the dependency surface of a workload between two environments — resources, config, and infrastructure references.

## AI-assisted workflows

All of these tools are available through MCP. Your AI assistant can call them directly instead of guessing from a single file:

- "What breaks if I change this?" → `find_blast_radius`
- "How is this service deployed?" → `trace_deployment_chain`
- "Which files influence image tags, runtime settings, or resource limits?" → `investigate_deployment_config`
- "What provisions this database?" → `investigate_resource`
- "Show me every workload that depends on this queue." → `investigate_resource`
- "Explain how these two things connect" → `explain_dependency_path`
- "What differs between prod and staging?" → `compare_environments`
- "Where is this behavior implemented?" → `investigate_code_topic`
- "What functions, classes, or top-level elements are in this repo?" → `inspect_code_inventory`
- "Which files or modules import this module?" → `investigate_import_dependencies`
- "Find hardcoded passwords, API keys, or risky literals." → `investigate_hardcoded_secrets`
- "Show me the source and docs evidence behind this answer" → `build_evidence_citation_packet`

See the [MCP Guide](guides/mcp-guide.md) for setup.
See [Starter Prompts](guides/starter-prompts.md) for role-based prompt sets you can use right away.

## Next steps

- [Why Eshu](why-eshu.md) — the problem this solves and how Eshu is different
- [Quickstart](getting-started/quickstart.md) — index a repo and run your first query
- [Starter Prompts](guides/starter-prompts.md) — role-based prompt examples and follow-up patterns
- [MCP Cookbook](reference/mcp-cookbook.md) — detailed MCP query examples
