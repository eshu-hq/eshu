# Query Playbooks

A **query playbook** is a machine-readable, deterministic, versioned description
of a common starter-prompt or cookbook workflow. It names the ordered first-class
tool calls a workflow takes, their bounded parameters, the truth and evidence
expected at each step, and the failure modes a caller must handle. A playbook
describes **how to reach an [answer packet](answer-packets.md)** for a prompt
family; it does not execute anything itself.

For missing-evidence-driven workflow discovery, use
[Investigation Workflows](investigation-workflows.md). Query playbooks describe
a fixed ordered call sequence; investigation workflows choose bounded next calls
from caller-provided missing-evidence state.

The implementation lives in `go/internal/query/query_playbook.go` (contract and
resolution), `go/internal/query/query_playbook_validate.go` (structural
validation), and `go/internal/query/query_playbook_catalog.go` (the versioned
catalog). Read the [Truth Label Protocol](truth-label-protocol.md) and the
[Answer Packet Contract](answer-packets.md) first: a playbook reuses the
`AnswerTruthClass` taxonomy and the `recommended_next_calls` / evidence-handle
shapes already defined there rather than introducing new ones.

## Why this contract exists

Starter prompts and the [MCP Cookbook](mcp-cookbook.md) describe recurring
workflows ("tell me the story of this service and cite the evidence", "how does
this repository handle X") as prose. Prose workflows are easy for an agent to
drift on: it can guess a tool, skip a bound, or invent a parameter. A playbook
turns the workflow into data that is:

- **Deterministic** — resolving a playbook with the same inputs always yields the
  same ordered, fully specified call sequence. There is no `Date.now`, no
  randomness, and no read of external or live-backend state.
- **Bounded** — every list step declares a default limit, so a resolved call is
  never unbounded.
- **Versioned** — each playbook carries an explicit semantic `version`, and the
  catalog identity (`id` + `version`) is pinned by a golden test so it cannot
  drift silently.
- **First-class** — every step references a real read-only MCP tool, validated
  against the `ReadOnlyTools` registry. Raw Cypher tools (`execute_cypher_query`,
  `execute_language_query`, `visualize_graph_query`) are rejected by validation.

## Contract

A `QueryPlaybook` declares:

| Field | Meaning |
| --- | --- |
| `id` | Stable catalog identifier. |
| `name` | Human-readable playbook name. |
| `version` | Semantic version of the definition. |
| `prompt_family` | Canonical prompt family, aligned with `AnswerPacket.PromptFamily`. |
| `required_inputs` | Declared inputs (`name`, `type`, `required`). The only external state a playbook reads. |
| `steps` | Ordered bounded calls. |
| `failure_modes` | Declared truth/error conditions and recommended fallbacks. |

Each `PlaybookStep` declares a first-class `tool`, bounded `params` (each bound
either from a declared input or from a declared constant — a default limit
(`const_int`), a constant string (`const_string`), or a constant boolean flag
(`const_bool`) such as opting a step into reranking), the `expected_truth` (an
`AnswerTruthClass`), the `evidence_expected`, and optional `drilldowns`. Each
`PlaybookFailureMode` declares a `condition`, its `meaning`, and a first-class
`fallback`.

### Resolution

`(QueryPlaybook).Resolve(inputs)` is a pure resolver. It validates the playbook,
rejects any input the playbook does not declare, requires every `required`
input, binds each step's parameters, and returns a `ResolvedPlaybook`: the
ordered `ResolvedCall`s (tool name plus concrete bounded `arguments`, the
expected truth class, the expected evidence, and the drilldowns) plus the
declared failure modes carried forward. It reads no external state, so it is safe
to run in a test to prove a workflow without a live backend. "Execute" in the
test suite means exactly this: resolve to real tool names, bounded params, and
declared expectations — not a call to a graph backend.

Validation guarantees:

- Identity fields, at least one step, and at least one failure mode are present.
- Every step references a known, non-raw-Cypher tool with a declared truth class
  and expected evidence.
- Every `from_input` parameter references a declared input.
- Optional inputs that a caller omits are dropped from the resolved arguments
  rather than emitted as empty values.

## Operator digest usage

The [Operator Digest Contract](operator-digest.md) may reference playbook IDs as
deterministic follow-up targets for suggested questions. A digest does not
execute a playbook and does not invent playbook parameters from ambient state;
it can only point at a catalog playbook with bounded arguments derived from the
digest's source answers.

## Catalog

The current catalog is returned by `PlaybookCatalog()`:

| ID | Version | Prompt family | Workflow |
| --- | --- | --- | --- |
| `service_story_citation` | 1.0.0 | `service.story` | `get_service_story` → `build_evidence_citation_packet`. Pull the one-call service dossier, then hydrate its evidence handles into a bounded citation packet. |
| `repository_code_topic_investigation` | 1.0.0 | `code.topic` | `investigate_code_topic` → `get_code_relationship_story`. Rank files and symbols for a topic, then read the graph-backed relationship story behind the top entity. |
| `documentation_truth_citation` | 1.0.0 | `documentation.truth` | `get_documentation_evidence_packet` → `check_documentation_evidence_packet_freshness`. Resolve a finding into a bounded evidence packet, then confirm it is still current before citing. |
| `incident_context_evidence_path` | 1.0.0 | `incident.context` | `get_incident_context` → `get_service_story`. Build incident context with linked evidence, then drill into the impacted service when one is selected. |
| `supply_chain_impact_explanation` | 1.0.0 | `supply-chain.impact` | `explain_supply_chain_impact` → `build_evidence_citation_packet`. Separate provider observations from Eshu-derived package, image, repository, and service state before citing. |
| `secrets_iam_trust_chain_posture` | 1.0.0 | `secrets-iam.posture` | `list_secrets_iam_identity_trust_chains` → `count_secrets_iam_posture`. Explain exact, partial, and permission-hidden identity posture with bounded trust-chain and secret-access drilldowns. |
| `incremental_freshness_readiness` | 1.0.0 | `freshness.readiness` | `get_generation_lifecycle` → `get_semantic_capability_status`. Diagnose stale or building answers with lifecycle, changed-since, index, and semantic readiness checks. |
| `hosted_onboarding_governance_status` | 1.0.0 | `hosted.governance` | `get_index_status` → `get_component_extension_diagnostics`. Summarize hosted onboarding readiness, auth scope, collector health, and governance caveats without exposing secrets. |
| `change_surface_source_investigation` | 1.0.0 | `change.surface` | `find_change_surface` → `get_relationship_evidence`. Rank affected source, drill into change-surface evidence, and cite exact file or relationship handles. |
| `query_to_service_context` | 1.0.0 | `query.service_context` | `search_semantic_context` → `get_service_story` → `build_evidence_citation_packet`. Discover context with reranked search, then resolve the recommended service into a dossier and cite the evidence. |
| `query_to_code_topic_context` | 1.0.0 | `query.code_topic_context` | `search_semantic_context` → `investigate_code_topic` → `get_code_relationship_story`. Discover context with reranked search, then rank the code topic and read the relationship story. |
| `query_to_incident_context` | 1.0.0 | `query.incident_context` | `search_semantic_context` → `get_incident_context` → `build_evidence_citation_packet`. Discover context with reranked search, then resolve the recommended incident into bounded context and cite the evidence. |
| `query_to_supply_chain_context` | 1.0.0 | `query.supply_chain_context` | `search_semantic_context` → `explain_supply_chain_impact` → `build_evidence_citation_packet`. Discover context with reranked search, then explain the recommended package or image impact and cite the evidence. |
| `demo_deployment_to_cloud_resource` | 1.0.0 | `demo.deployment_to_cloud_resource` | `list_kubernetes_correlations`. Single bounded call returning the digest-joined Kubernetes workload → OCI image correlations for a cluster (demo Q2). |
| `demo_dependency_cross_repo` | 1.0.0 | `demo.dependency_cross_repo` | `list_package_registry_correlations`. Single bounded call returning the package consumption correlations that name the repository depending on a package (demo Q4). |
| `demo_observability_to_workload` | 1.0.0 | `demo.observability_to_workload` | `list_observability_coverage_correlations`. Single bounded call returning the observability coverage correlations for a provider (demo Q5). |

Each catalog playbook declares its own failure modes — for example "service not
found" recommends `investigate_service`, and "citation packet truncated"
recommends raising the bounded limit or sending the next handle batch. The
second-wave playbooks also declare common answer-experience failure handling for
unsupported capabilities, missing evidence, stale or building freshness,
truncated result sets, and ambiguous selectors.

The third-wave `query_to_*` playbooks start from `search_semantic_context` as
read-only context discovery (with `rerank: true` so the search step's
`recommended_next_calls` drive the readbacks) and never infer graph truth from
retrieval alone. Each declares the failure modes a caller hits: missing search
readiness (`semantic_unavailable` / `index_unready`), no hits, stale vectors,
an ambiguous readback target, and truncation.

The `demo_*` playbooks back the guided demo path in
[Your First Five Questions](../getting-started/first-five-questions.md). Each is
a single bounded call over an existing correlation tool, pinned to the
`specs/demo-first-answers.v1.yaml` acceptance oracle. They wrap tools that
already ship; they introduce no new query capability.

## API / MCP / CLI exposure

The catalog is available through read-only surfaces:

| Surface | Operation | Result |
| --- | --- | --- |
| HTTP | `GET /api/v0/query-playbooks` | Lists catalog IDs, versions, prompt families, required inputs, steps, evidence expectations, and failure modes. |
| HTTP | `POST /api/v0/query-playbooks/resolve` | Resolves `playbook_id` plus declared string inputs into an ordered, bounded call sequence. |
| MCP | `list_query_playbooks` | Dispatches to the HTTP catalog route and returns the canonical envelope as the structured resource block. |
| MCP | `resolve_query_playbook` | Dispatches to the HTTP resolver route with `playbook_id` and `inputs`. |
| CLI | `eshu playbooks list` / `eshu playbooks resolve` | Prints the canonical API envelope as JSON for operator scripting. |

These surfaces report `query.playbooks` truth with `runtime_state` basis because
they describe deterministic workflow-plan data, not live graph query truth. The
resolver does not execute calls, read Postgres, read the graph backend, call
providers, or expose raw Cypher. Unknown playbooks, undeclared inputs, and
missing required inputs fail with bounded errors.

No-Regression Evidence: `cd go && go test ./cmd/api ./cmd/mcp-server ./cmd/eshu
./internal/query ./internal/mcp -count=1` covers the HTTP handler, API and MCP
binary wiring, MCP registry and dispatch, CLI resolver helper, OpenAPI assembly,
and capability-matrix contract for the static `query.playbooks` surface.

No-Observability-Change: query playbook list and resolve calls read only the
in-process static catalog and return through the existing HTTP/MCP envelope
path. They do not open graph or Postgres connections, enqueue work, execute
resolved calls, start spans for backend reads, or add metric labels; operators
diagnose failures through the existing HTTP status, canonical error envelope,
MCP dispatch result, and CLI transport error output.

## Verification

```bash
cd go && go test ./internal/query -count=1 -run Playbook
cd go && go test ./internal/mcp -count=1 -run QueryPlaybookTools
```
