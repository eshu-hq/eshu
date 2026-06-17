# Answer Packet Contract

An **answer packet** is a first-class composition layer that turns existing
query truth into a user-ready response plan **without losing structured
evidence**. It does not replace the canonical envelope; it references or embeds
it. The packet exists so that prompt-facing surfaces (MCP tools, CLI summaries,
console answers) can present a short human answer while keeping the machine-
readable envelope as the source of truth, the truth labels intact, and the
evidence handles addressable.

The implementation lives in `go/internal/query/answer_packet.go`. It builds on
the truth, error, and evidence-citation contracts already defined in
`go/internal/query/contract.go` and `go/internal/query/evidence_citation.go`.
Read the [Truth Label Protocol](truth-label-protocol.md) first: the answer
packet does not redefine truth, it classifies and surfaces the truth the query
layer already produced.

## Why this contract exists

Prompt families ("who calls this function?", "what is the blast radius of this
resource?", "cite the evidence for this finding") share a recurring need:

1. A short, human-readable answer.
2. The structured result, kept verbatim, so a client can re-derive the answer.
3. The truth labels (level, basis, freshness) so the client knows how far to
   trust the answer.
4. Evidence handles and citation references so the answer is auditable.
5. Honest signaling when the answer is **unsupported** or **partial**, so an
   unanswerable question never becomes a confident sentence.

Before this contract, each surface re-assembled those pieces ad hoc, which made
it easy to drop the envelope, lose the truth label, or emit a confident summary
on top of an `unsupported_capability` error. The answer packet makes the
composition explicit and testable.

## Non-goals

- The packet does **not** introduce a new truth taxonomy. Truth levels, bases,
  and freshness stay defined in `contract.go`.
- The packet does **not** replace `ResponseEnvelope`, `TruthEnvelope`, or
  `ErrorEnvelope`. It references the envelope and embeds a compact copy of the
  truth metadata.
- The packet does **not** make MCP text summaries canonical. MCP text remains a
  convenience layer; the canonical envelope resource block stays authoritative.
- The packet does **not** make route payloads non-canonical. Routes keep
  returning canonical envelope data; answer-facing companions are additive.

## The AnswerPacket

```jsonc
{
  "prompt_family": "call_graph.direct_callers",
  "question": "Who calls AdmitWorkload?",
  "primary_tool": "find_callers",
  "primary_route": "POST /api/v0/code/call-graph/callers",
  "truth_class": "deterministic",
  "summary": "12 direct callers across 3 repositories.",
  "supported": true,
  "partial": false,
  "result_ref": "eshu://tool-result/envelope",
  "result": { /* compact, optional embedded copy of the envelope data */ },
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
  "unsupported_reasons": []
}
```

### Field contract

| Field | Contract |
| --- | --- |
| `prompt_family` | Canonical prompt-family / capability identifier. Aligns with capability IDs in the conformance matrix. |
| `question` | The canonical question the packet answers, normalized. |
| `primary_tool` | The MCP tool or logical operation that produced the result. Optional. |
| `primary_route` | The HTTP route that produced the result. Optional. |
| `truth_class` | Derived classification (see below). Distinguishes deterministic, derived, fallback, semantic-observation, and code-hint answers. |
| `summary` | Human-readable answer. **Empty or non-confident** when `supported` is false. |
| `supported` | `false` when required evidence is unavailable (capability unsupported, index building, ambiguous, or no evidence). |
| `partial` | `true` when the answer is incomplete (truncated, stale, missing evidence) but still usable. |
| `result_ref` | Reference to the canonical envelope payload (e.g. an `eshu://` URI). |
| `result` | Optional compact embedded copy of the envelope `data`. The referenced envelope remains canonical. |
| `truth` | A copy of the `TruthEnvelope` fields. Canonical truth. Absent only for unsupported answers built from an error. |
| `limitations` | Bounded, human-readable caveats (limit caps, scope bounds). |
| `truncated` | Mirrors result-set truncation when the underlying query truncated. |
| `missing_evidence` | Evidence handles that were requested but could not be resolved. |
| `evidence_handles` | Addressable handles to the evidence behind the answer, in the [evidence citation handle](evidence-citation-handles.md) shape. |
| `citation_ref` | Reference to a citation packet that hydrates the handles. Optional. |
| `recommended_next_calls` | Bounded follow-up calls, in the same shape as the evidence-citation `recommended_next_calls`; entries may include `tool`, `route`, `playbook`, `reason`, and bounded non-sensitive `arguments` or legacy `params`. |
| `unsupported_reasons` | Why the answer is unsupported or partial. Non-empty whenever `supported` is false or `partial` is true. |

The packet never carries a confident `summary` while `supported` is false. That
invariant is the core acceptance test for this contract.

## Normalized answer metadata

Story and investigation routes that answer user questions include an additive
`answer_metadata` companion. It is attached to the canonical `data` payload and
does not replace existing route fields. The companion is currently emitted by:

- `GET /api/v0/services/{service_name}/story`
- `GET /api/v0/repositories/{repo_id}/story`
- `POST /api/v0/code/topics/investigate`
- `POST /api/v0/impact/change-surface/investigate`
- `GET /api/v0/incidents/{incident_id}/context`
- `POST /api/v0/compare/environments`

```jsonc
{
  "answer_metadata": {
    "schema_version": "answer_metadata.v1",
    "evidence_handles": [
      { "kind": "entity", "entity_id": "go:func:resolveAuth" }
    ],
    "missing_evidence": [
      { "slot": "image", "reason": "no image evidence has been linked yet" }
    ],
    "limitations": [
      { "kind": "result_truncated", "reason": "result truncated; not all evidence is included" }
    ],
    "truncated": true,
    "coverage": {
      "query_shape": "content_topic_investigation",
      "truncated": true
    },
    "partial_reasons": ["result_truncated", "missing_evidence"],
    "recommended_next_calls": [
      { "tool": "get_code_relationship_story", "reason": "read the top matched entity relationship story" }
    ]
  }
}
```

### Metadata fields

| Field | Contract |
| --- | --- |
| `schema_version` | Stable metadata schema identifier. Current value: `answer_metadata.v1`. |
| `evidence_handles` | Bounded handles already present in the route payload, normalized to citation-compatible keys when possible. |
| `missing_evidence` | Structured missing evidence rows or slots. Reason-only rows are allowed for incident-style missing path slots. |
| `limitations` | Bounded caveats from route limitations, identity limitations, or truncation. |
| `truncated` | `true` when the route, coverage, or result-limit metadata reports truncation. |
| `coverage` | The route's existing `coverage`, `coverage_summary`, or `result_limits` block copied as normalized coverage. |
| `partial_reasons` | Stable reason codes derived from truncation, missing evidence, non-complete coverage state, and limitation kinds. |
| `recommended_next_calls` | Existing route follow-up calls copied without adding new graph or content reads. |

### Migration guidance

Clients should keep reading the canonical route fields they already depend on.
New answer-facing clients can read `answer_metadata` first, then fall back to
route-specific fields while older deployments are rolling forward. Existing
fields remain authoritative: the companion summarizes them so AnswerPacket
composition and MCP text summaries do not need custom parsing for every route.

No-Observability-Change: `answer_metadata` is built from the already
materialized response map or incident response struct. It performs no graph,
content-store, provider, collector, reducer, or queue reads and adds no runtime
span. Existing route spans and structured logs remain the operator-facing proof
for the underlying data fetch.

## API and MCP exposure

High-value answer routes may expose a packet as additive `data.answer_packet`
inside the canonical `ResponseEnvelope`. Initial wired routes are:

| Route / tool family | Packet prompt family | Notes |
| --- | --- | --- |
| `GET /api/v0/services/{service_name}/story` / `get_service_story` | `service.story` | Service dossier packet with the route story, result limits, and service-story truth. |
| `GET /api/v0/incidents/{incident_id}/context` / `get_incident_context` | `incident.context` | Incident packet with missing-slot and truncation caveats from the bounded context response. |
| `POST /api/v0/code/topics/investigate` / `investigate_code_topic` | `code.topic` | Code-topic packet with evidence handles, truncation state, and recommended source/relationship drilldowns. |

Client choice is intentionally simple:

- Read the `ResponseEnvelope` when machine truth, exact payload shape, freshness,
  or error handling matters.
- Read MCP text for a short convenience summary in chat clients.
- Read `data.answer_packet` when a client needs a bounded user-facing answer
  plan: `truth_class`, `supported`, `partial`, limitations, evidence handles,
  missing evidence, and next calls.

MCP resource blocks still carry the canonical envelope byte-for-byte. The packet
is only part of that envelope data, so MCP clients get API/MCP parity without
losing the canonical source of truth.

## Canonical truth is preserved

The packet's `truth` block is a copy of the existing `TruthEnvelope`, and
`result_ref` / `result` point at the existing `ResponseEnvelope` data. The
envelope remains the canonical machine-readable truth. The packet is a
**view over** the envelope, not a replacement. Clients that need to re-derive
the answer read the envelope; clients that need a one-line answer read the
packet `summary` and `truth_class`.

## Truth classes

The query layer already produces a `TruthLevel` (`exact`, `derived`,
`fallback`) and a `TruthBasis` (`authoritative_graph`, `semantic_facts`,
`content_index`, `hybrid`). The answer packet folds those two axes into a
single, prompt-facing `truth_class` so a client can pick presentation and
caution without re-implementing the matrix.

| `truth_class` | Derived from | Meaning |
| --- | --- | --- |
| `deterministic` | `level == exact` and `basis == authoritative_graph` | Authoritative graph truth. Safe to present as fact. |
| `derived` | `level == derived` (any basis) | Deterministic result computed from indexed entities, content, or relational state. |
| `fallback` | `level == fallback` | Exploratory result, useful but not authoritative for the capability. |
| `semantic_observation` | `level == exact` and `basis == semantic_facts` | Durable semantic truth from facts, not graph topology. |
| `code_hint` | `basis == content_index` (and `level != exact`) | Content-index / search signal. A hint, not a verified relationship. |
| `unsupported` | No truth envelope (built from an error) | The capability could not answer; there is no truth to classify. |

Mapping rules, in order:

1. No truth envelope → `unsupported`.
2. `basis == semantic_facts` and `level == exact` → `semantic_observation`.
3. `basis == authoritative_graph` and `level == exact` → `deterministic`.
4. `basis == content_index` and `level != exact` → `code_hint`.
5. `level == fallback` → `fallback`.
6. Otherwise → `derived`.

This keeps the distinction the issue requires: deterministic vs derived vs
fallback truth, plus semantic observations and code hints, all mapped from the
existing `TruthLevel` + `TruthBasis` rather than a new truth source.

## Supported, partial, and unsupported answers

The builder takes one of two explicit paths.

**Supported path** — built from a successful `ResponseEnvelope` (it carries a
non-nil `TruthEnvelope` and no error). The packet copies the truth metadata,
classifies `truth_class`, and may carry a confident `summary`. If the underlying
result is truncated, stale, or has missing evidence, `partial` is set and the
reasons are recorded, but the answer is still usable.

**Unsupported path** — built from an `ErrorEnvelope` (for example
`unsupported_capability`, `index_building`, `ambiguous`, `scope_not_found`) or
from empty/missing evidence. The packet:

- sets `supported = false`,
- sets `truth_class = unsupported`,
- leaves `summary` empty (the builder refuses to attach a confident answer),
- records `unsupported_reasons` from the error code and message, and
- may still carry `recommended_next_calls` so the client knows how to proceed.

An empty evidence set on an otherwise-supported capability is treated as
**partial** (the question is answerable but no evidence resolved), not as a
confident "no". This prevents "no rows" from being presented as a definitive
negative answer when the real state is "no evidence yet". Callers opt into this
behavior for evidence-centric answers via the builder's `NoEvidence` input
(the citation builder sets it automatically when a citation packet resolves
nothing); capabilities whose structured result *is* the evidence leave it unset.

### Freshness causality flows into the packet

When the envelope's `truth.freshness` carries a proven `cause` (see the
[Truth Label Protocol](truth-label-protocol.md#freshness-causality)), the
builder folds that cause into the partial reason — for example
`underlying data is stale (cause: reducer_backlog)` — and surfaces the
freshness `next_check` as an additional `recommended_next_calls` entry, so the
packet explains **why** the answer lags and **where** to drill in. When no cause
is proven, the generic stale/building reason is kept verbatim and no freshness
next call is added; the packet never invents a cause. Causality is not
correctness: a stale-but-caused answer stays `supported` and usable, just
`partial`.

## Reused contracts

The answer packet does not duplicate existing shapes. It reuses:

- `TruthEnvelope`, `TruthLevel`, `TruthBasis`, `TruthFreshness`, and
  `ErrorEnvelope` from `contract.go`.
- The evidence-handle shape and `recommended_next_calls` convention from
  `evidence_citation.go` and the
  [Evidence Citation Handle Contract](evidence-citation-handles.md).

When the answer is built from an evidence-citation response, its
`evidence_handles`, `missing_evidence`, `truncated`, and
`recommended_next_calls` map straight through from the citation packet.

## Cache and MCP guidance

The packet inherits the cache guidance of the [Truth Label
Protocol](truth-label-protocol.md): vary any cached answer on the request
payload, `truth.level`, and `truth.freshness.state`. When a surface emits both
an envelope and a packet, the envelope stays the canonical wire contract and the
packet is the human-facing companion, mirroring the MCP text-block-plus-envelope
convention.
