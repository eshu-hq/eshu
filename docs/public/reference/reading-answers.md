# Reading Eshu Answers

Eshu does not just return rows. Every answer carries truth labels, freshness,
limitations, evidence handles, and recommended next calls so a human or an agent
can tell whether a result is authoritative, derived, partial, stale, or
unsupported — and what to do next. This page explains how to read those signals
across MCP, HTTP, CLI, and hosted workflows.

Read the source contracts first. This page does not redefine them:

- [Truth Label Protocol](truth-label-protocol.md) — the wire-level authority
  contract (truth level, basis, freshness, error codes).
- [Answer Packet Contract](answer-packets.md) — the prompt-facing answer view
  built over the canonical envelope.
- [Capability Conformance Spec](capability-conformance-spec.md) — what each
  runtime profile may claim.
- [MCP Guide](../guides/mcp-guide.md) and
  [MCP Reference](mcp-reference.md) — MCP result shape and the text-summary
  convention.
- [MCP Tool Contract Matrix](mcp-tool-contract-matrix.md) — prompt-ready versus
  partial tool routes.
- [Query Playbooks](query-playbooks.md) and
  [Starter Prompts](../guides/starter-prompts.md) — how workflows map to
  ordered tool calls.
- [Visualization Packet Contract](visualization-packets.md) — rendering a
  story or evidence answer as an explainable subgraph.

## Two layers: envelope and packet

Eshu answers come in two related shapes. The **canonical envelope** is the
machine-readable source of truth. The **answer packet** is a prompt-facing view
built over the envelope; it never replaces it.

| Shape | Owner | Use it for |
| --- | --- | --- |
| `ResponseEnvelope` (`data`, `truth`, `error`) | [Truth Label Protocol](truth-label-protocol.md) | The authoritative, re-derivable result. Programmatic clients read this. |
| `AnswerPacket` (`summary`, `truth_class`, `supported`, `partial`, handles, next calls) | [Answer Packet Contract](answer-packets.md) | A short human answer plus a single prompt-facing truth classification. |

The packet's `truth` block is a copy of the envelope's `TruthEnvelope`, and its
`result_ref` / `result` point at the same envelope `data`. When a surface emits
both, the envelope stays canonical and the packet is the human-facing companion.

## How to read an answer packet

A worked example. A caller asks "Who calls `AdmitWorkload`?" and receives this
answer packet (shape from [Answer Packet Contract](answer-packets.md)):

```jsonc
{
  "prompt_family": "call_graph.direct_callers",
  "question": "Who calls AdmitWorkload?",
  "primary_tool": "find_callers",
  "primary_route": "POST /api/v0/code/call-graph/callers",
  "truth_class": "deterministic",
  "summary": "12 direct callers across 3 repositories.",
  "supported": true,
  "partial": true,
  "result_ref": "eshu://tool-result/envelope",
  "truth": {
    "level": "exact",
    "capability": "call_graph.direct_callers",
    "profile": "local_authoritative",
    "basis": "authoritative_graph",
    "freshness": { "state": "fresh" }
  },
  "limitations": ["results bounded to 50 callers"],
  "truncated": true,
  "missing_evidence": [],
  "evidence_handles": [
    { "kind": "entity", "entity_id": "go:func:AdmitWorkload", "evidence_family": "source" }
  ],
  "citation_ref": "eshu://evidence/citations/abc123",
  "recommended_next_calls": [
    { "tool": "build_evidence_citation_packet", "reason": "cite the caller sites" }
  ],
  "unsupported_reasons": ["result truncated at the bounded caller limit"]
}
```

Read it in this order:

1. **`supported`** — `true` means the capability could answer. `false` means the
   question is unanswerable in this profile or with this evidence; treat that as
   [expected behavior](#unsupported-and-partial-answers-are-expected), not a
   failure.
2. **`truth_class`** — the single prompt-facing classification. Here
   `deterministic` means authoritative graph truth, safe to present as fact. See
   the [mapping below](#truth-level-basis-answertruthclass).
3. **`summary`** — the short human answer. Empty or non-confident whenever
   `supported` is `false`; the builder refuses to attach a confident sentence to
   an unsupported answer.
4. **`partial`, `truncated`, `limitations`, `missing_evidence`** — incompleteness
   signals. Here the result is usable but bounded to 50 callers, so `partial` is
   `true` and `unsupported_reasons` records why.
5. **`evidence_handles` / `citation_ref`** — addressable handles to the evidence
   behind the answer. Use these to [request citations](#when-to-request-citations).
6. **`recommended_next_calls`** — bounded follow-ups Eshu suggests. See
   [following recommended next calls](#following-recommended-next-calls).

## Interpreting truth level, basis, and freshness

The envelope `truth` block carries the authority signals. Fields and their
contracts are owned by [Truth Label Protocol](truth-label-protocol.md):

| Field | What it tells you |
| --- | --- |
| `level` | `exact` (authoritative graph or durable semantic truth), `derived` (deterministic from indexed entities/content/relational state), or `fallback` (exploratory, not authoritative). |
| `basis` | Where the answer came from: `authoritative_graph`, `semantic_facts`, `content_index`, or `hybrid`. |
| `capability` | Capability ID from the [Capability Conformance Spec](capability-conformance-spec.md). |
| `profile` | Active runtime profile: `local_lightweight`, `local_authoritative`, `local_full_stack`, or `production`. |
| `freshness.state` | `fresh`, `stale`, `building`, or `unavailable`. |
| `reason` | Human-readable explanation for logs and debugging. |

`authoritative` is not a wire field. Infer authority from `level == "exact"` plus
capability semantics. High-authority capabilities (transitive call graphs,
call-chain paths, dead-code cleanup, cross-repo impact) return
`unsupported_capability` when the active profile cannot answer correctly — they
do not silently downgrade to `fallback`.

Freshness reads:

- `fresh` — the indexed evidence is current; trust the result at its truth level.
- `stale` — the answer is usable but the underlying index has moved on; revalidate
  before acting on it. Caches must vary on freshness (see below).
- `building` — the index is still materializing. Often paired with the
  `index_building` error; expect a more complete answer after indexing settles.
- `unavailable` — the answering backend cannot supply freshness; treat the result
  with caution.

Caches, ETags, and local memoization must vary on request payload, `truth.level`,
and `truth.freshness.state`. Do not reuse a cached answer across a truth-level or
freshness change.

### Truth level × basis → AnswerTruthClass

The packet folds the two envelope axes (`level` and `basis`) into one
prompt-facing `truth_class`. This is the mapping from
[Answer Packet Contract](answer-packets.md); do not invent a new one:

| `truth_class` | Derived from | Meaning |
| --- | --- | --- |
| `deterministic` | `level == exact` and `basis == authoritative_graph` | Authoritative graph truth. Safe to present as fact. |
| `derived` | `level == derived` (any basis) | Deterministic result computed from indexed entities, content, or relational state. |
| `fallback` | `level == fallback` | Exploratory result, useful but not authoritative for the capability. |
| `semantic_observation` | `level == exact` and `basis == semantic_facts` | Durable semantic truth from facts, not graph topology. |
| `code_hint` | `basis == content_index` (and `level != exact`) | Content-index / search signal. A hint, not a verified relationship. |
| `unsupported` | No truth envelope (built from an error) | The capability could not answer; there is no truth to classify. |

Mapping rules apply in order: no envelope → `unsupported`; `semantic_facts` +
`exact` → `semantic_observation`; `authoritative_graph` + `exact` →
`deterministic`; `content_index` + non-`exact` → `code_hint`; `fallback` →
`fallback`; otherwise → `derived`.

## When to request citations

The packet keeps evidence addressable instead of embedding every row. Request a
citation packet when you need to show *why* an answer is true, not just the
answer:

- The packet carries `evidence_handles` and you want file/line or entity-level
  proof.
- The packet has a `citation_ref` you want to hydrate.
- A `recommended_next_calls` entry names `build_evidence_citation_packet`.
- You are answering a "cite the evidence" or audit-style prompt.

Citations hydrate handles through `POST /api/v0/evidence/citations` (or the
`build_evidence_citation_packet` MCP tool). The route accepts at most 500 input
handles, hydrates at most 50 citations per packet, preserves distinct line ranges
and reasons for the same file, and returns `coverage.truncated` when you should
request another packet. It reads the Postgres content store and does not traverse
the graph. See
[HTTP Evidence and Supply-Chain Routes](http-api/evidence-and-supply-chain.md#citation-packets).

You do **not** need a citation packet when `truth_class` is `deterministic` and
the structured result already *is* the evidence the caller asked for.

## Following recommended next calls

`recommended_next_calls` is a bounded list of follow-up tool calls Eshu suggests,
in the same shape used by the evidence-citation contract: each entry names a
`tool` (a real read-only MCP tool) and a `reason`. Treat it as a guided drilldown:

- After a story or investigation, the next call usually hydrates evidence
  (`build_evidence_citation_packet`) or drills into a top entity
  (`get_code_relationship_story`).
- When a result is `partial` or `truncated`, the next call typically raises the
  bounded limit or requests the next handle batch.
- When `supported` is `false`, the next call points at a recovery path — for
  example resolving an ambiguous name with `investigate_service` before retrying.

Recommended next calls reference only first-class read-only tools. They never
recommend raw Cypher (`execute_cypher_query`, `execute_language_query`,
`visualize_graph_query`). See the
[MCP Tool Contract Matrix](mcp-tool-contract-matrix.md) for which tools are
prompt-ready.

## MCP text summaries vs structured content

MCP results include a short **text** block and **`structuredContent`** plus a
resource block. Read the structured content; the text is a convenience layer, not
the canonical contract.

```json
{
  "structuredContent": {
    "data": {},
    "truth": {},
    "error": null
  },
  "content": [
    {
      "type": "text",
      "text": "Eshu query completed."
    },
    {
      "type": "resource",
      "resource": {
        "uri": "eshu://tool-result/envelope",
        "mimeType": "application/eshu.envelope+json",
        "text": "{\"data\":{},\"truth\":{},\"error\":null}"
      }
    }
  ]
}
```

For story, investigation, citation, and status tools the text block is a
deterministic, bounded summary of the same envelope (truth level, freshness, key
counts, and any partial/error detail). It is length-capped and derived from the
envelope, so a rich or degraded result never collapses into generic success text.
But it is still only a convenience for human readers. The `structuredContent` and
the resource block stay byte-identical to the canonical envelope; only the text
string changes. Clients must read `structuredContent` (or the resource block) for
evidence and must not parse the text summary. See
[MCP Reference](mcp-reference.md#text-summaries-are-a-convenience-layer-not-the-canonical-contract)
and [MCP Guide](../guides/mcp-guide.md#read-the-envelope).

## Playbooks ↔ starter prompts

A [Starter Prompt](../guides/starter-prompts.md) is the natural-language way to
ask a question. A [Query Playbook](query-playbooks.md) is the machine-readable,
deterministic, versioned description of the ordered tool calls that reach the
answer packet for that prompt family. The playbook reuses the same
`AnswerTruthClass` taxonomy and `recommended_next_calls` / evidence-handle shapes
described above rather than inventing new ones.

| Starter prompt | Playbook | Ordered calls |
| --- | --- | --- |
| "Build the service story for `<service>` and cite the evidence." | `service_story_citation` (`service.story`) | `get_service_story` → `build_evidence_citation_packet` |
| "Investigate `<topic>` in this repository and read the relationship story." | `repository_code_topic_investigation` (`code.topic`) | `investigate_code_topic` → `get_code_relationship_story` |
| "Resolve this documentation finding and confirm it is still current." | `documentation_truth_citation` (`documentation.truth`) | `get_documentation_evidence_packet` → `check_documentation_evidence_packet_freshness` |

Each playbook step declares the truth class and evidence it expects, and each
playbook declares its own failure modes — for example "service not found"
recommends `investigate_service`, and "citation packet truncated" recommends
raising the bounded limit or sending the next handle batch. That is the same
recovery path the answer packet surfaces through `unsupported_reasons` and
`recommended_next_calls`.

## Unsupported and partial answers are expected

An unsupported or partial answer is a correct product outcome, not a failure.
Eshu refuses to turn an unanswerable question into a confident sentence.

**Partial answers** stay `supported` but set `partial = true`. The result is
incomplete (truncated, stale, or missing some evidence) yet still usable. The
reasons live in `unsupported_reasons`, and `limitations`, `truncated`, and
`missing_evidence` describe the bound. An empty evidence set on an
otherwise-supported capability is treated as **partial**, not as a confident
"no" — "no evidence yet" must never be presented as a definitive negative.

**Unsupported answers** set `supported = false`, `truth_class = unsupported`, and
leave `summary` empty. They are built from an `ErrorEnvelope` and still carry
`recommended_next_calls` so the client knows how to proceed. Common error codes
(owned by [Truth Label Protocol](truth-label-protocol.md#error-codes)):

| Error code | What it means for the answer |
| --- | --- |
| `unsupported_capability` | The capability cannot answer correctly in the active profile. Errors include current and required profiles when known. Switch profiles or pick a supported capability. |
| `index_building` | The index is still materializing (often paired with `freshness.state == building`). Retry after indexing settles. |
| `ambiguous` | The target resolved to multiple candidates. Disambiguate with the recommended resolution call, then retry. |
| `scope_not_found` | The requested repository, service, or workload scope is not indexed. Index it or correct the scope. |
| `not_found` | The entity does not exist in the index. |
| `capability_degraded` | The capability answered at reduced authority; read the truth level and freshness. |
| `backend_unavailable` / `overloaded` | The graph backend is unavailable or shedding load. Retry with backoff. |

Treat these as routing signals. The
[MCP Tool Contract Matrix](mcp-tool-contract-matrix.md) shows which prompt
families have prompt-ready tools versus partial routes, so you can pick a
supported path before falling back.

## Worked examples: local and hosted

The same envelope and packet contract is served from a local stack and a hosted
deployment. Only the base URL and authentication change. Replace placeholders
with your own values; never paste real hostnames, tokens, or machine paths.

### Local (CLI)

Run a bounded query through the CLI against a local stack. Human output renders
the result plus a concise truth summary when the result is not exact; `--json`
emits the canonical `{data, truth, error}` envelope.

```bash
# Human output ends with a truth summary line, for example:
#   truth=derived basis=content_index capability=code_search.exact_symbol
eshu find name AdmitWorkload

# Canonical envelope for automation:
eshu find name AdmitWorkload --json
```

For unsupported capabilities the CLI fails non-zero and reports the current and
required profiles, matching the `unsupported_capability` error envelope.

### Local (HTTP)

Opt in to the canonical envelope with the `Accept` header. The local Compose API
listens on `http://localhost:8080` by default.

```bash
curl -s http://localhost:8080/api/v0/services/payments-service/story \
  -H 'Accept: application/eshu.envelope+json'
```

Read `truth.level`, `truth.freshness.state`, and `error` from the returned
envelope exactly as described above.

### Hosted (HTTP)

A hosted deployment uses the same routes behind your own base URL and an API key.
Use placeholders such as `https://eshu.example.com` and the `${ESHU_API_KEY}`
environment variable; do not hardcode secrets or private hostnames.

```bash
curl -s "https://eshu.example.com/api/v0/services/payments-service/story" \
  -H 'Accept: application/eshu.envelope+json' \
  -H "Authorization: Bearer ${ESHU_API_KEY}"
```

The envelope, truth labels, freshness states, answer-packet classification, and
error codes are identical to the local examples. A `stale` or `building`
freshness state on a hosted call is expected during indexing, and an
`unsupported_capability` error names the required profile — both are correct
product behavior, not transport failures.

## Related docs

- [Truth Label Protocol](truth-label-protocol.md)
- [Answer Packet Contract](answer-packets.md)
- [Query Playbooks](query-playbooks.md)
- [Visualization Packet Contract](visualization-packets.md)
- [Capability Conformance Spec](capability-conformance-spec.md)
- [MCP Guide](../guides/mcp-guide.md)
- [MCP Reference](mcp-reference.md)
- [MCP Tool Contract Matrix](mcp-tool-contract-matrix.md)
- [Starter Prompts](../guides/starter-prompts.md)
- [HTTP Evidence and Supply-Chain Routes](http-api/evidence-and-supply-chain.md)
