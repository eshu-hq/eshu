# Starter Prompts

Use these prompts with MCP, the API, or a graph-aware assistant. Start narrow:
include the repo, environment, workload, file, symbol, or resource when you know
it.

For setup, read [Index Repositories](../use/index-repositories.md),
[Connect MCP](../mcp/index.md), and [MCP Guide](mcp-guide.md).

## Cross-Repo Framing

- "Investigate `<service>` in `<environment>` across related repositories,
  deployment sources, and indexed documentation."
- "Build the service story for `<service>` and cite the source, manifest, and
  runtime evidence."
- "Trace the GitOps and runtime path for every repo that contributes to
  `<service>`."

## Code

- "Who calls `process_payment` across indexed repos?"
- "Find the implementation of `PaymentProvider`."
- "Which files import `shared-auth-lib`?"
- "Show the shortest call chain from `main` to this handler."
- "Show the most complex functions in `payments-service`."
- "What code looks dead in `api-gateway`?"
- "Find possible hardcoded passwords, API keys, or secrets in `api-gateway`."

Good additions: repo name or `repo_id`, exact symbol, direct versus transitive
callers, and whether you need citations.

## Deployment And Infrastructure

- "Trace the deployment chain for `payments-api` in `prod`."
- "Which repos and manifests define this workload?"
- "Trace this RDS instance back to Terraform."
- "Which workloads use this database?"
- "Compare `prod` and `staging` for `checkout-service`."
- "Which files influence the image tag and resource limits for this service?"

Good additions: environment, workload, resource ID, and whether you need
controller, runtime, or config evidence.

## Change Risk

- "What breaks if I change `payments-api`?"
- "What is the blast radius of modifying this Terraform module?"
- "What change surface is affected if I update these files?"
- "Explain why this service and this database are connected."
- "Show direct impact first, then transitive impact."

Good additions: changed file paths, target environment, exact entity, and
whether direct-only results are enough.

## Documentation And Support

- "Explain this service to a new engineer."
- "Create a support runbook for `<service>` in `<environment>`."
- "Show the source and docs evidence behind this explanation."
- "List the fastest places to investigate request, auth, config, and deploy
  issues for `<service>`."

Good additions: audience, environment, output shape, and citation requirements.

## Useful Follow-Ups

- "Now narrow that to `ops-qa`."
- "Show only the repos and files involved."
- "Explain the highest-confidence dependency path."
- "What is shared versus dedicated in that dependency set?"
- "Which part of that path is least certain?"

For exact tool names and JSON examples, use the
[MCP Cookbook](../reference/mcp-cookbook.md).
