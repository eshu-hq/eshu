# Starter Prompts

Use these prompts with MCP, the API, or a graph-aware assistant. Start narrow:
include the repo, environment, workload, file, symbol, or resource when you know
it.

For setup, read [Index Repositories](../use/index-repositories.md),
[Connect MCP](../mcp/index.md), and [MCP Guide](mcp-guide.md).

## Playbook-Backed First Prompts

Use these prompts when you want the assistant or client to follow a known
workflow instead of guessing tool order. Each row maps to a current
[Query Playbook](../reference/query-playbooks.md), the ordered first-class tools
it resolves to, and the answer-packet truth classes to expect.

| Starter prompt | Playbook ID | Ordered tools | Expected truth classes | What to read next |
| --- | --- | --- | --- | --- |
| "Build the service story for `<service>` and cite the evidence." | `service_story_citation@1.0.0` | `get_service_story` -> `build_evidence_citation_packet` | `deterministic`, then `code_hint` for citation excerpts | Read `truth.freshness`, `limitations`, and `recommended_next_calls`; if the citation packet is truncated, request the next bounded handle batch. |
| "Investigate `<topic>` in `<repo>` and read the relationship story." | `repository_code_topic_investigation@1.0.0` | `investigate_code_topic` -> `get_code_relationship_story` | `code_hint`, then `deterministic` when graph relationships exist | Treat ranked content matches as partial until the relationship story or source lines confirm the path. |
| "Resolve this documentation finding and confirm it is still current." | `documentation_truth_citation@1.0.0` | `get_documentation_evidence_packet` -> `check_documentation_evidence_packet_freshness` | `semantic_observation`, then `deterministic` freshness | If freshness is stale, re-fetch the evidence packet before citing it. |
| "Find the service behind `<question>` in `<repo>` and tell its story." | `query_to_service_context@1.0.0` | `search_semantic_context` -> `get_service_story` -> `build_evidence_citation_packet` | `derived` discovery, then `deterministic`, then `code_hint` citations | Use the search step's `recommended_next_calls` to pick the service; if no hits or `semantic_unavailable`, broaden the query or retry in keyword mode. |
| "Investigate `<question>` about `<repo>` code and read the relationship story." | `query_to_code_topic_context@1.0.0` | `search_semantic_context` -> `investigate_code_topic` -> `get_code_relationship_story` | `derived` discovery, then `code_hint`, then `deterministic` | Treat ranked matches as partial until the relationship story or source lines confirm the path. |
| "Find the incident behind `<question>` in `<repo>` and read its context." | `query_to_incident_context@1.0.0` | `search_semantic_context` -> `get_incident_context` -> `build_evidence_citation_packet` | `derived` discovery, then `derived` context, then `code_hint` citations | Resolve the incident from the search step's `recommended_next_calls`; if vectors are stale, check `get_semantic_capability_status`. |
| "Explain the supply-chain impact behind `<question>` in `<repo>`." | `query_to_supply_chain_context@1.0.0` | `search_semantic_context` -> `explain_supply_chain_impact` -> `build_evidence_citation_packet` | `derived` discovery, then `derived` impact, then `code_hint` citations | Separate provider observations from Eshu-derived state; if the target is ambiguous, list neighboring findings first. |

There is not yet a dedicated readiness or hosted-governance playbook in the
catalog. For those prompts, use the prompt-ready status tools from the
[MCP Tool Contract Matrix](../reference/mcp-tool-contract-matrix.md) plus the
hosted governance docs and caveats; keep shared-token caveats explicit until
scoped hosted isolation exists.

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

- "Now narrow that to `platform-qa`."
- "Show only the repos and files involved."
- "Explain the highest-confidence dependency path."
- "What is shared versus dedicated in that dependency set?"
- "Which part of that path is least certain?"

For exact tool names and JSON examples, use the
[MCP Cookbook](../reference/mcp-cookbook.md).
