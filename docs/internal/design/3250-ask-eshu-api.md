# Ask Eshu API — Self-Hosted Agentic Answer Surface

Status: **PROPOSED.**

Refs #3250. Builds on accepted design #2462 (Governed Answer Narration) and its
child epic #2465. Depends on governance gates #1755 (Semantic Extraction
Security Gates), #1900 (Hosted Governance Policy Model), #1902 (Tenant And
Workspace Isolation), and design #430 (NornicDB primary store / derived read
models). Related: the canonical surface inventory (`cmd/capability-inventory`),
the query-plan regression gate, and `go/internal/semanticprofile`.

## Decision

Add **Ask Eshu**: a self-hosted, agentic natural-language answer surface over
Eshu's knowledge base, reachable by an external company or user holding only an
Eshu API key — no Claude Code, Codex, or other external agent required. The
caller asks a free-form question about their stack and environment; Eshu plans
the most efficient retrieval path across NornicDB and Postgres, assembles
evidence-backed `AnswerPacket`s, and returns the answer as prose or as an
exported artifact (Mermaid, JSON, YAML, CSV).

Ask Eshu is a **new sibling surface**, not an extension of answer narration.
Narration (#2462) remains a bounded renderer over an already-assembled packet;
Ask Eshu is the orchestrator that *produces* those packets by following the
sanctioned path #2462 names: call canonical routes, follow
`recommended_next_calls`, rebuild packets. Ask Eshu reuses the narration gate
only as its final presentation step, so #2462 stays intact.

### Why this is a distinct design from #2462

ADR #2462 deliberately walls retrieval out of narration:

> Narration never performs new graph reads, content reads, provider-source
> retrieval, reducer work, queue work, or truth classification. [...] No
> unbounded retrieval, whole-graph search, or graph traversal inside narration.

Ask Eshu performs exactly that retrieval — but in a separate, separately
governed component, and #2462 explicitly sanctions the mechanism:

> If a caller needs more evidence, it must follow the packet's
> `recommended_next_calls` and rebuild a new packet from the canonical routes.

Ask Eshu **is** that caller. Its retrieval lives in a dedicated engine
(`go/internal/ask`), never inside the narrator.

## Goals

1. A caller with only an Eshu API key can ask anything about their environment
   in natural language and get a correct, evidence-backed answer.
2. Eshu chooses the **most efficient correct retrieval path** — a pre-built
   route when one fits, graph vs. SQL by selectivity/index availability
   otherwise — rather than fetching blindly.
3. Eshu knows the **complete, current** set of its own API and MCP calls, with
   no hand-maintained list.
4. The answer can be exported in the format the caller asks for (prose,
   Markdown, Mermaid, JSON, YAML, CSV).
5. Every answer and every exported artifact remains truth-labeled and
   evidence-backed; fluency never outranks evidence (the #2462 invariant).
6. The whole surface is default-off, scoped per tenant, read-only, and bounded
   in cost and concurrency.

## Non-Goals

- No promotion of generated text, LLM-authored query results, or rendered
  artifacts to facts, graph truth, search truth, or reducer evidence.
- No new truth taxonomy; Ask Eshu reuses `TruthEnvelope` / `AnswerTruthClass`.
- No write path. Ask Eshu is read-only end to end.
- No mandatory hosted-provider dependency and no default provider traffic.
- No raw prompt or raw provider-response retention by default.
- No bypass of tenant/workspace scope; scope is enforced before any retrieval,
  never by filtering completed output.

## Architecture

One new package, `go/internal/ask`, hosting a bounded server-side agent loop,
plus a new API route family and an MCP tool that both call the same engine.

```text
POST /api/v0/ask            (Eshu API key -> scoped READ-ONLY token)
  -> ask.Engine.Run(question, format, scope, budget)
       grounding: surface-inventory catalog + cost map + narration posture
       loop (bounded: max iterations, wall-clock, token budget):
         provider (tool-calling LLM) selects next action:
           |- Tier 1: call a canonical API/MCP capability  (fast path)
           |          -> ResponseEnvelope -> AnswerPacket
           |          -> follow recommended_next_calls as needed
           |- Tier 2: author a graph read  -> cypher sandbox -> NornicDB
           |          author a fact read    -> sql sandbox    -> Postgres
           |          -> results wrapped in TruthEnvelope -> AnswerPacket
         each step audited (metadata only)
       provider emits final answer in requested format
  -> render + validate artifact (mermaid/json/yaml/csv)
  -> narration gate (final presentation) -> response
```

### Component boundaries

Each unit has one purpose, a defined interface, and is independently testable.
All files stay under the repo's 500-line cap and ship with `doc.go`,
`README.md`, and `AGENTS.md`.

- **`engine`** — owns the loop: iteration/budget bounds, action dispatch,
  packet accumulation, final-answer assembly. Depends on `provider`, `catalog`,
  `planner`, `sandbox`, `render`, `governance`.
- **`provider`** — tool-calling adapters over `semanticprofile` config. Two
  families: `anthropic` (Anthropic tool-use; also Bedrock-Claude) and
  `openaicompat` (OpenAI-compatible tool-calling; MiniMax, DeepSeek, OpenAI,
  Azure OpenAI, Gemini-compat, Ollama, internal-gateway). Interface:
  `Complete(ctx, messages, tools) -> (assistantTurn, toolCalls, usage)`.
- **`catalog`** — builds the tool/route catalog from
  `surface-inventory.generated.json`, annotating each entry with backend
  (NornicDB / Postgres / both) and cost class. This is Ask Eshu's self-knowledge
  of "every available call." No hand-maintained list.
- **`planner`** — the cost map and path-selection guidance handed to the model
  (system prompt + per-tool metadata) plus the deterministic cost estimate used
  by the Tier 2 cost gate.
- **`sandbox`** — Tier 2 read-only query validation/execution for Cypher and
  SQL (see Security). Two validators, one execution seam each.
- **`render`** — output-format production guidance + post-hoc validation/lint of
  artifacts (`auto|markdown|mermaid|json|yaml|csv`).
- **`governance`** — ties the engine to the `answer-narration` posture, the
  per-tenant policy, budget, and publish-safety; fails closed.

### Provider layer (DeepSeek + MiniMax + the rest)

Ask Eshu reuses `go/internal/semanticprofile` for provider configuration — one
credential surface, bring-your-own-key, air-gapped capable. Additions:

- A named `minimax` provider kind for operator parity with `deepseek`, routed
  through the OpenAI-compatible tool-calling adapter (MiniMax exposes an
  OpenAI-compatible chat-completions API). MiniMax may also be configured today
  via `openai_compatible`; the named kind is for clarity and health reporting.
- A new `agent_reasoning` source class so a profile must be explicitly marked
  reasoning-capable to be selected by Ask Eshu. An embedding-only or
  documentation-only profile must never be chosen as the loop's reasoning model.
- The adapter contract requires **tool-calling**. Selection rejects any profile
  whose `model_id` is not tool-calling capable for its kind.

### Self-knowledge from the surface inventory

The `catalog` is generated from the canonical
`surface-inventory.generated.json` (produced by
`cmd/capability-inventory -mode generate` and drift-gated on every surface
change). Consequences:

- Ask Eshu always knows the complete, current set of API routes and MCP tools.
- Each catalog entry carries the backend and cost annotation that drives
  fastest-path selection.
- A new capability is learned for free when the inventory regenerates; there is
  no second list to maintain and no chance of a hallucinated or stale tool.

### Two retrieval tiers

- **Tier 1 — canonical-route orchestration (sanctioned).** The loop calls the
  real API/MCP capabilities from the catalog, reads each packet's
  `recommended_next_calls`, and rebuilds packets until it can answer. This is
  the #2462-blessed path and covers most questions because the route set is
  large and each route already routes to the correct backend efficiently.
- **Tier 2 — LLM-authored sandboxed query (net-new, default-off).** For the
  long tail no route covers, the model authors a Cypher (NornicDB) or SQL
  (Postgres) read that passes the sandbox before executing. Tier 2 is gated
  separately, ships disabled, and requires its own security review before it may
  be enabled (see Security).

The planner biases the model toward Tier 1 and toward the cheaper backend; Tier
2 is the fallback, not the default tool.

## Security

### Tier 2 sandbox (the load-bearing safety work)

Every LLM-authored query — Cypher or SQL — passes all of the following before
any execution, and a failure at any step bounces back into the loop as a
recoverable tool error:

1. **Read-only enforcement.** Parse the statement; reject any write, DDL, DML
   mutation, transaction control, or side-effecting procedure/function. NornicDB
   Cypher and Postgres SQL each get a dedicated validator. Default-deny: an
   unparseable or unrecognized construct is rejected.
2. **Scope injection.** The caller's scoped-token tenant predicate is *injected*
   by Eshu into the query, never trusted from model output, reusing the existing
   scoped-route predicate/CTE pattern. A query can never address another
   tenant's data.
3. **Cost gate.** An `EXPLAIN`/plan estimate runs through the existing
   query-plan regression gate. Over-budget plans are rejected; the estimate is
   returned to the model so it can reformulate a cheaper query.
4. **Hard caps.** Row limit, result-byte cap, per-query timeout, max queries per
   question, and bounded parallel tool calls per request.
5. **Audit.** Every executed query and its plan cost are recorded as metadata
   only — never raw source, private paths, hostnames, credentials, prompt text,
   or provider responses.

### Governance and isolation

Ask Eshu inherits the optional-provider posture and fails closed:

- Default OFF. No provider exchange unless governance, provider configuration,
  policy, budget, redaction, retention, citation, and publish-safety gates all
  allow it. Provider configuration is inventory, not permission.
- Hosted read authorization and tenant/workspace scope (#1902) are enforced
  before any retrieval begins.
- Hosted-provider traffic passes the #1755 security preflight before content
  leaves the runtime; policy follows the #1900 hosted-governance model.
- Tier 2 enable-by-default is out of scope until a dedicated security review
  signs off. v1 ships Tier 2 present but disabled.
- `retention_posture: metadata_only` is preserved; raw prompts and raw provider
  responses are not retained by default.

### Truth invariant

All retrieval results — Tier 1 and Tier 2 — flow through `TruthEnvelope` →
`AnswerPacket`, so every sentence and every exported artifact traces to packet
evidence, limitations, unsupported reasons, or freshness state. The narration
gate's validation (the #2462 Traceability Invariant) rejects any final answer
that asserts uncited facts, weakens `supported=false`/`partial=true`/stale/
truncation signals, upgrades fallback/code-hint/derived results to authoritative
graph claims, or leaks raw source/paths/hostnames/credentials/prompts/provider
responses. A Mermaid diagram or JSON export is therefore evidence-backed and
truth-labeled, never fabricated topology.

## Output Rendering

The request carries `format: auto|markdown|mermaid|json|yaml|csv`. The model
produces the artifact in its final turn; `render` then validates it before
return:

- `json` / `yaml` — parse and re-serialize; reject invalid documents.
- `mermaid` — lint for renderable syntax.
- `csv` — validate column consistency.
- `auto` — the engine picks prose/Markdown unless the question implies an export
  ("export...", "as a diagram", "as YAML").

The response shape:

```json
{
  "answer_prose": "string",
  "artifacts": [{ "format": "mermaid", "content": "...", "truth_class": "deterministic" }],
  "truth_class": "deterministic|derived|fallback|semantic_observation|code_hint|unsupported",
  "evidence_handles": [ ... ],
  "query_trace": [ { "tier": 1, "call": "get_service_story", "backend": "nornicdb", "cost_class": "low" } ],
  "partial": false,
  "limitations": [ ... ],
  "unsupported_reasons": [ ... ]
}
```

## API And MCP Surface

- `POST /api/v0/ask` — synchronous JSON: the final answer plus the tool/query
  trace, once the bounded loop finishes.
- `POST /api/v0/ask` with `Accept: text/event-stream` — SSE: reasoning steps,
  tool/query calls, and answer tokens as they happen.
- An `ask` MCP tool exposing the same engine, so existing agents can use Ask
  Eshu too.
- `GET /api/v0/status/answer-narration` finally reflects a real capability
  (`available` when all gates pass), with the same redacted, metadata-only
  status fields already defined.

Auth: an Eshu API key resolves to the existing scoped, read-only token; the
agent loop runs entirely under that scope. OpenAPI in
`go/internal/query/openapi*.go` is kept in lockstep with handlers and
`docs/public/reference/http-api.md`.

## Performance And Concurrency Contract

- The loop is bounded by max iterations, wall-clock, and token budget; exceeding
  any bound falls back to the best deterministic packet assembled so far with
  its truth labels intact.
- Path selection prefers Tier 1 and the cheaper backend; Tier 2 queries must
  pass the cost gate, so the model cannot execute an expensive plan.
- Read-only throughout; per-request parallel tool calls are bounded; per-tenant
  budget and rate limits apply.
- The existing query-plan regression gate is the enforcement point for the
  per-path performance budget.

## Observability

Operator-visible, audit-safe, low-cardinality only (never raw content, source
ids, private endpoints, prompts, responses, provider request ids, or credential
handles):

- Per-state counts: disabled, denied, budget-exhausted, unsafe, timeout,
  provider-unavailable.
- Ask-request count; loop-iteration histogram; Tier 1 vs Tier 2 call counts;
  sandbox-rejection counts by bounded reason code; cost-gate rejection counts.
- Narration validation result counts and fallback counts by reason code.
- Prompt/response/validator versions by safe revision hash.
- Spans for the loop, each tool/query call, the cost gate, and the narration
  gate.

## Verification Plan

TDD throughout. At minimum:

- **Sandbox (adversarial):** write/DDL/DML/txn/procedure rejection for Cypher
  and SQL; scope-predicate injection cannot be overridden by model output;
  cross-tenant query rejected; cost-gate rejects an over-budget plan and returns
  the estimate; row/byte/time caps enforced; unparseable input default-denied.
- **Engine bounds:** max-iteration, wall-clock, and token-budget cutoffs fall
  back to the deterministic packet with labels intact; bounded parallel tool
  calls; per-tenant budget/rate limits.
- **Provider adapters:** Anthropic tool-use and OpenAI-compatible tool-calling
  round-trips; MiniMax and DeepSeek profile selection; embedding-only /
  non-reasoning profile rejected; tool-call/error recovery.
- **Catalog:** generated from `surface-inventory.generated.json`; a new
  capability appears without code edits; backend/cost annotations present.
- **Truth/narration:** uncited fact rejected; limitation/unsupported/stale/
  truncation not weakened; derived/code-hint not upgraded; private
  path/hostname/credential/prompt/provider-response leak rejected; export
  artifacts carry the correct truth class.
- **Render:** invalid JSON/YAML/Mermaid/CSV rejected; `auto` format selection.
- **Surfaces:** API sync, API SSE, and MCP tool each preserve the canonical
  envelope and deterministic fallback when narration is unavailable.
- **Performance:** Tier 1 and Tier 2 query paths carry plan-regression proof;
  loop wall-clock measured against the budget.
- **Docs gate:** strict mkdocs build plus `git diff --check` for the doc and
  OpenAPI/reference changes.

## Rollout

- v1 implements the full vision: both tiers, sync + SSE, the Anthropic and
  OpenAI-compatible (MiniMax/DeepSeek/OpenAI/Azure/Gemini-compat/Ollama/
  internal-gateway) adapters, prose + Markdown + Mermaid + JSON + YAML + CSV
  exports, and the `ask` MCP tool.
- Tier 1 may enable once its governance gates pass.
- Tier 2 ships present but **disabled**; enabling it by default requires a
  dedicated security review against #1755 / #1900 / #1902.

## Decomposition

Implementation decomposes into child issues under #3250 (suggested order):

1. `catalog` from the surface inventory + cost map.
2. `provider` adapters over `semanticprofile` (+ `minimax` kind, `agent_reasoning`
   source class).
3. `engine` loop with bounds, Tier 1 orchestration, and the narration gate.
4. `sandbox` Cypher + SQL validators, scope injection, cost gate (Tier 2).
5. `render` output-format validation.
6. API routes (sync + SSE) + OpenAPI + `ask` MCP tool + `answer-narration`
   status wiring.
7. Observability, governance policy wiring, and the security-review package for
   Tier 2.
