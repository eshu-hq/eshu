# MCP Guide

MCP is the assistant-facing interface for Eshu. Use it when a coding assistant
needs indexed repository, code, deployment, and infrastructure context.

For the path chooser across local Eshu service stdio, Compose MCP, and deployed
MCP, start with [Connect MCP](../mcp/index.md).

## Setup

If you want ready-to-use natural-language examples before wiring your client, start with [Starter Prompts](starter-prompts.md). For the strongest end-to-end answers, use the cross-repo prompt framing there: ask Eshu to scan all related repositories, deployment sources, and indexed documentation before it explains a service or workload.

Run the setup wizard to configure your MCP client:

```bash
eshu mcp setup
```

The wizard writes or updates configuration for supported clients (Claude, Cursor, VS Code) and gives you a config snippet for manual wiring.

## Structured Results With Truth Labels

MCP tool results now include two content blocks:

1. A human-readable `text` block with a short summary.
2. An `embedded_resource` block whose `mimeType` is
   `application/eshu.envelope+json` and whose `text` is the canonical Eshu
   response envelope — `{data, truth, error}`.

The envelope is the canonical client contract. Programmatic clients should
prefer the structured `resource` block over the summary `text`. The envelope
exposes:

- `truth.level` — `exact`, `derived`, or `fallback`.
- `truth.capability` — the capability ID from
  [capability-matrix.v1.yaml](../reference/capability-conformance-spec.md).
- `truth.profile` — `local_lightweight`, `local_authoritative`,
  `local_full_stack`, or `production`.
- `truth.freshness.state` — `fresh`, `stale`, `building`, or `unavailable`.

On unsupported capabilities (for example `call_graph.transitive_callers` in
`local_lightweight`), the tool call returns a structured
`unsupported_capability` error rather than a degraded fallback. See
[Truth Label Protocol — MCP Contract](../reference/truth-label-protocol.md).

## Use The Local Compose MCP With Codex Or Claude

For local graph-backed testing, the easiest path is the Compose stack plus the
repo-local `.mcp.json`.

1. Start Compose against the repositories you want to index.
2. Run `./scripts/sync_local_compose_mcp.sh` to update the
   `eshu-local-compose` entry in `.mcp.json`.
3. Restart the Codex or Claude session so the client reloads the local MCP
   config and bearer token.
4. Ask a small smoke-test question such as:
   - "resolve `DeployableUnitCorrelationHandler`"
   - "who calls `publishIntentGraphPhase`?"
   - "show me the contents of `go/internal/reducer/deployable_unit_correlation.go`"

Example, single repo:

```bash
ESHU_FILESYSTEM_HOST_ROOT="$HOME/eshu-local-index" \
ESHU_REPOSITORY_RULES_JSON='{"exact":["eshu"]}' \
docker compose up --build

./scripts/sync_local_compose_mcp.sh
```

Example, multiple repos:

```bash
ESHU_FILESYSTEM_HOST_ROOT="$HOME/eshu-local-index" \
ESHU_REPOSITORY_RULES_JSON='{"exact":["repo-one","repo-two","repo-three"]}' \
docker compose up --build

./scripts/sync_local_compose_mcp.sh
```

If you started Compose with a custom project name, pass the same project name
when syncing:

```bash
COMPOSE_PROJECT_NAME=eshu-one-repo ./scripts/sync_local_compose_mcp.sh
```

If one client needs a different config file, point the helper at it:

```bash
ESHU_MCP_CONFIG_FILE="$HOME/path/to/mcp.json" \
./scripts/sync_local_compose_mcp.sh
```

To stop and fully reset the local stack:

```bash
docker compose down -v --remove-orphans
```

For the broader Compose workflow and shutdown details, see
[Docker Compose](../deployment/docker-compose.md).

## Start the server

For stdio-based MCP with one local Eshu service:

```bash
eshu mcp start --workspace-root /path/to/repo
```

For Compose, start the stack and point the MCP client at the published MCP
service:

```bash
docker compose up --build
```

Compose publishes the API at `http://localhost:8080` and the MCP service at
`http://localhost:8081` by default. In the deployable split-service shape, the
dedicated `mcp-server` runtime is the MCP HTTP endpoint. Do not point MCP
clients at the HTTP API runtime.

## Canonical Versus Repair Surfaces

Use the canonical query and status surfaces first:

- story, context, trace, and content tools for graph-backed answers
- `index-status` when you need checkpointed completeness
- runtime health or `/admin/status` when you need live service state

For the `local_authoritative` NornicDB profile, code-search tools may answer
from the embedded-Postgres content index while canonical graph
projection is degraded. Programmatic clients should read the Eshu envelope:
`truth.profile=local_authoritative` and `truth.basis=content_index` mean the
answer is intentionally content-index-backed, not silently pretending to be a
fully converged graph answer.

Treat repair surfaces as repair surfaces:

- `eshu finalize` has been removed
- `POST /admin/refinalize` and `POST /admin/replay` on the Go ingester admin
  surface are for controlled recovery, not for normal question answering
- normal query, story, and status flows should use canonical graph-backed
  surfaces instead of repair endpoints

## Before and after

**Without MCP** — you ask your AI assistant "What does the payment service depend on?"

The assistant greps imports in the current file, maybe finds a few Python packages. It has no visibility into Terraform modules, K8s manifests, ArgoCD apps, or cross-repo callers. It guesses.

**With MCP** — same question.

The assistant calls `get_service_story payment-service` and gets back the
service dossier: identity, API surface, deployment lanes, upstream dependencies,
downstream consumers, evidence graph, investigation coverage, and drill-down
handles to fetch raw context only when needed. If the user wants coverage before
the answer, the assistant starts with `investigate_service payment-service`.
Either way, it answers with evidence from the graph, not hallucinated
assumptions from a partial code snapshot.

## What MCP tools answer

| Question pattern | MCP tool |
|-----------------|----------|
| "Who calls this function?" | `get_code_relationship_story` |
| "What breaks if I change this service?" | `investigate_change_surface` |
| "What is the blast radius of a code-topic or changed-path edit?" | `investigate_change_surface` |
| "How is this deployed?" | `trace_deployment_chain` |
| "Which files influence image tags, runtime settings, or resource limits?" | `investigate_deployment_config` |
| "What provisions this database?" | `investigate_resource` |
| "Which workloads use this queue or database?" | `investigate_resource` |
| "Compare prod and staging" | `compare_environments` |
| "What does this repo contain?" | `get_repo_context` |
| "Find the code paths involved in this behavior" | `investigate_code_topic` |
| "Why does this deployment or dependency edge exist?" | `get_relationship_evidence` with the `resolved_id` from `deployment_evidence` |
| "Tell me the Internet-to-cloud-to-code story for this repo" | `get_repo_story` |
| "Tell me the deployment story for this workload or service" | `get_workload_story`, `get_service_story` |
| "Which repos, deployment sources, and docs should I scan before explaining this service?" | `investigate_service` |
| "Explain this service, then cite the relevant files and docs" | `get_service_story`, then `build_evidence_citation_packet` with its file and entity handles |
| "Create support or onboarding documentation for this repo or service" | `get_repo_story`, `get_service_story`, `get_workload_story`, then `build_evidence_citation_packet` |
| "Show me the source of this file" | `get_file_content` |
| "Search across indexed code" | `search_file_content` |
| "Find complex functions" | `find_most_complex_functions` |
| "What's dead code?" | `investigate_dead_code` |
| "Which IaC artifacts look unused?" | `find_dead_iac` |
| "Which AWS resources are unmanaged?" | `find_unmanaged_resources` |
| "Which registry packages or versions are indexed?" | `list_package_registry_packages`, `list_package_registry_versions` |

## Story-first responses

For repository, service, and deployment questions, Eshu now exposes dedicated
story surfaces:

- `get_repo_story`
- `get_workload_story`
- `get_service_story`
- `investigate_service`
- `investigate_code_topic`
- `investigate_dead_code`
- `investigate_deployment_config`

Use it this way:

1. start with `story`
2. for service questions, treat `get_service_story` as the one-call dossier path and read `service_identity`, `api_surface`, `deployment_lanes`, `upstream_dependencies`, `downstream_consumers`, `evidence_graph`, and `investigation`
3. for deeper deployment debugging, use `trace_deployment_chain` and then read `controller_overview`, `runtime_overview`, or `deployment_fact_summary`
4. for image tags, runtime settings, resource limits, values layers, rendered targets, and "which files should I read first" prompts, use `investigate_deployment_config`
5. if the answer needs exact source, docs, manifest, or deployment citations, follow with `build_evidence_citation_packet` using the story or investigation handles
6. use `investigate_service` when the caller asks what Eshu scanned, which repos have evidence, or which call should happen next
7. use `drilldowns` or `resolved_id` handles to move into `get_repo_context`, `get_workload_context`, `get_service_context`, content reads, `get_relationship_evidence`, or lower-level relationship tools

This keeps answers concise without hiding the underlying evidence.

For broad programming prompts like "find all code involved in repo sync auth,"
start with `investigate_code_topic`. It returns ranked `repo_id +
relative_path` evidence groups, matched symbols, coverage/truncation metadata,
and exact follow-up calls. Use `get_code_relationship_story`, `get_file_lines`,
or `get_entity_content` only after that first packet identifies the anchor.

For documentation-oriented answers, the orchestration order is:

1. graph-first story and context
2. structured GitOps, documentation, and support overviews
3. targeted Postgres file reads or content search
4. exact file or line citations only when the story answer needs them

The deployment-oriented trace surface now exposes three related layers:

- `controller_overview` for controller-family evidence such as ArgoCD, Flux, Jenkins, or other automation controllers
- `runtime_overview` for observed runtime/platform evidence such as EKS, Kubernetes, ECS, or Lambda
- `deployment_facts` plus `deployment_fact_summary` for normalized evidence-backed mapping

`deployment_fact_summary` is the quickest way to understand how Eshu interpreted the evidence:

- `mapping_mode=controller` means Eshu found explicit controller evidence
- `mapping_mode=iac` means Eshu found infrastructure-as-code evidence such as Terraform or CloudFormation
- `mapping_mode=evidence_only` means Eshu found delivery/runtime evidence but no trustworthy controller or IAC adapter and intentionally did not guess
- `mapping_mode=none` means Eshu did not find enough deployment evidence to map a controller, IAC adapter, or evidence-only path yet

It also exposes truthfulness helpers:

- `overall_confidence_reason` explains why the mapping got its top-level confidence
- `fact_thresholds` maps each emitted fact type to the evidence threshold it passed
- `limitations` uses standardized deployment-mapping codes like `deployment_evidence_missing`, `deployment_controller_unknown`, `deployment_source_unknown`, `config_source_unknown`, `runtime_platform_unknown`, `environment_unknown`, and `entrypoint_unknown`

That rule is important: Eshu maps from indexed evidence, not deployment-tool assumptions. If a company uses ArgoCD, Flux, Terraform Helm provider, Terraform Kubernetes provider, CloudFormation stack sets, CloudFormation serverless patterns, or plain manifests with no controller at all, the story contract should reflect only what the parser actually found.

The current story order is:

1. public entrypoints
2. API surface
3. deployment path
4. runtime or platform context
5. shared config families and consumer context
6. limitation notes and coverage gaps

`deployment_story` usually comes from explicit delivery paths. When those are missing, Eshu next tries to synthesize a truthful delivery path from reusable-workflow handoff plus canonical deploy/provision/runtime context. Only after that does it fall back to controller/runtime evidence such as Terraform, CodeDeploy, runtime platforms, and service variants.

There is now a controller-driven automation tier between those two extremes. For example, if a repo is deployed through Jenkins invoking Ansible, Eshu can surface that as story-first context through `controller_driven_paths` even when GitHub Actions style delivery rows are absent. Consume that layer in this order:

1. `story`
2. `story_sections`
3. `deployment_overview`
4. `delivery_paths`
5. `controller_driven_paths`
6. detailed evidence fields

When you need a stable contract instead of prose, prefer this order:

1. `deployment_fact_summary`
2. `deployment_facts`
3. `controller_overview`
4. `runtime_overview`
5. lower-level deployment evidence fields

That lets clients answer:

- what type of deployment evidence was found
- how confident the mapping is
- why that confidence was assigned
- what threshold each fact passed
- which evidence sources were used
- what is still missing or unknown

For programming prompts, keep using the code-query tools directly:

- `find_code`
- `find_symbol`
- `get_code_relationship_story`
- `analyze_code_relationships`
- `calculate_cyclomatic_complexity`
- `find_most_complex_functions`
- `investigate_dead_code`
- `find_dead_code`
- `find_dead_iac`
- `find_unmanaged_resources`
- `list_package_registry_packages`
- `list_package_registry_versions`

Those remain the primary public contract for callers/callees/class hierarchy/import/complexity/dead-code, dead-IaC, and package registry identity questions. Use `investigate_dead_code` first for dead-code prompts because it returns coverage, language maturity, exactness blockers, source handles, and conservative ambiguous buckets for JavaScript/TypeScript precision risk. Use `get_code_relationship_story` first for one-symbol caller, callee, import, and bounded transitive CALLS prompts because it returns ambiguity and truncation metadata in one response. The service and repository story tools are for end-to-end narratives, not a replacement for the focused query tools.

## Repository access handoff

When Eshu is deployed remotely, the server may not have local access to every repository. Content retrieval follows a fallback chain:

1. **PostgreSQL content store** — preferred, fastest
2. **Server workspace** — shared checkout volume
3. **Graph cache** — metadata stored during indexing
4. **Conversational handoff** — asks the user for a local path

Read responses include `source_backend` so you know where the answer came from. On stdio MCP clients that support elicitation, Eshu can prompt for a local checkout path directly through the protocol. On HTTP clients, it falls back to conversational handoff.

`search_file_content` and `search_entity_content` require the PostgreSQL content store — they do not fall back to workspace scanning.

Both content-search tools are paged. Set `limit` and `offset`; responses include
`truncated`, `limit`, and `offset`. `offset` is capped at 10000 so broad cold
searches stay bounded; narrow the repository or pattern before paging beyond
that window. When you pass multiple `repo_ids`, the server uses one scoped
PostgreSQL query instead of one request per repository.

For documentation and runbook generation, expect the story layer to prefer Postgres-backed content evidence whenever it needs exact docs, README, runbook, overlay, or config references. If content is missing, story responses should expose limitations instead of implying the docs do not exist.

Use `build_evidence_citation_packet` when the prompt asks for proof behind an
answer. It accepts only explicit file or entity handles, caps each packet at 50
handles, hydrates files and entities from the content store in batches, returns
bounded excerpts plus missing-handle coverage, and avoids graph calls. Use
`get_file_content`, `get_file_lines`, or search only when the user needs a
single raw source body or when the previous story/investigation response did
not already identify the handles.

## Prompt-suite guardrails

Prompt-suite coverage should stay portable and auth-safe:

- use repo-relative identifiers and paths, not server-local filesystem paths
- prefer story and context tools before raw content search when the user asks for explanation or documentation
- use `build_evidence_citation_packet` after story or investigation tools identify the right evidence handles
- use `investigate_code_topic` before raw content search for broad code-topic or behavior prompts
- page broad content searches with `limit` and `offset`; do not repeat the same broad search hoping a warm cache makes it cheaper
- prefer structured MCP or HTTP tools before any expert fallback
- do not treat raw Cypher as a generic fallback for prompt or story tests
- only ask for a local checkout path when a workflow truly needs the user's machine

These rules keep prompt tests from leaking server paths or normalizing unsafe query habits into the public surface.

## Troubleshooting

**"Tool not found"** — verify the MCP server is running (`eshu mcp start`) and your client config points to the correct command or URL.

**"No repositories indexed"** — MCP queries the graph, which requires indexing first. Run `eshu index /path/to/repo` or start docker-compose to index fixtures.

**Slow responses** — check the local-host progress panel, graph-write metrics,
and the selected graph backend. Use the default NornicDB path or the explicit
Neo4j stack for production-scale graphs.

## Related docs

- [Starter Prompts](starter-prompts.md) — role-based prompt examples you can use immediately
- [MCP Reference](../reference/mcp-reference.md) — full tool list with parameters
- [MCP Cookbook](../reference/mcp-cookbook.md) — detailed query examples
- [HTTP API](../reference/http-api.md) — automation and service-to-service access
- [Shared Infra Trace](shared-infra-trace.md) — cross-repo infrastructure tracing
