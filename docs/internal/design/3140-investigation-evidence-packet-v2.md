# 3140 — Investigation Evidence Packet v2 Schema and CLI Contract

Status: accepted (gate for epic #3139)

Parent epic: #3139 — world-class evidence packets and portable proof artifacts.

This is the contract gate. It defines the durable shape of the portable proof
artifact *before* the emitters (#3141 supply-chain, #3142 deployable-unit/drift)
populate it from real data, and *before* the dogfood benchmark (#3143) measures
it against peer artifacts.

## Problem

Peer tools (Graphify-style) win the instant-local-artifact experience: one
command yields a browsable graph plus a JSON/markdown report a user can carry,
inspect, and share. Eshu has stronger, reducer-owned truth, but that proof has
been harder to carry. We want a portable artifact **without** weakening
reducer-owned truth or the no-provider deterministic contract.

## Decision

Define `investigation_evidence_packet.v2`, a self-contained artifact that
separates the evidence layers an investigation produces, reusing Eshu's existing
truth and evidence-handle contracts rather than inventing new ones.

The packet is the v2 successor to the v1 answer-facing companions:

- `AnswerPacket` and `answer_metadata.v1` are *views over one
  `ResponseEnvelope`* — a short human answer plus handles.
- `investigation_evidence_packet.v2` is a *self-contained artifact* that carries
  the raw-evidence, reducer-decision, and graph-truth layers separately so each
  is independently auditable, plus identity, freshness, missing hops, bounds,
  redaction, and validation.

### Why not extend AnswerPacket?

`AnswerPacket` is intentionally a thin view: it copies one truth envelope and a
flat handle list. The epic requires the layers be *separated* (raw vs decided vs
graph-answered) and the artifact be *self-contained and portable*. Bolting three
new layers onto the answer view would overload a type whose contract is "a view,
not a replacement." A distinct v2 type keeps both contracts honest and lets the
emitters compose a packet from multiple query results, not just one envelope.

### Layers

| Layer | Field | Source contract |
| --- | --- | --- |
| Identity | `identity` | new (family, scope, generation, profile, backend, basis) |
| Truth | `truth`, `freshness` | reuse `TruthEnvelope`, `TruthFreshness` |
| Answer | `answer` | reuse `AnswerTruthClass`; AnswerPacket invariant |
| Raw evidence | `source_facts` | new; optional `evidenceCitationHandle` |
| Reducer decisions | `reducer_decisions` | admission-audit state vocabulary |
| Graph/query truth | `graph_answers` | reuse `AnswerTruthClass` |
| Citations | `citations` | reuse evidence citation handles |
| Missing evidence | `missing_evidence` | new, explicit per-hop reason |
| Semantic (optional) | `semantic_observations` | new, labeled + policy-gated |
| Bounds / Redaction / Validation | `bounds`, `redaction`, `validation` | mirror operator-digest artifact |

### Determinism and no-provider behavior

- The packet core never reads the wall clock. `packet_id` is a sha256 over the
  identity plus a content digest of the evidence layers, so identical inputs
  yield identical bytes and ids (reproducible no-provider builds), and different
  evidence under the same identity yields a different id.
- `identity.basis` is `deterministic` by default. Semantic observations require
  an explicit `AllowSemantic` policy gate and a per-entry `semantic_observation`
  label; supplying them without the gate is a hard error. When present and
  allowed, `basis` flips to `semantic_augmented` so the posture is on the wire.

### Refusal, not fabrication

An investigation that cannot be answered yields a **valid refusal packet**
(`unknown_family`, `scope_not_found`, `profile_unsupported`,
`backend_unavailable`), unsupported and summary-free, with the refusal reason
recorded. The refusal states map to existing API/MCP error codes (documented in
the public reference). This preserves the "refuse, don't degrade" posture.

### Bounded payload

Each evidence layer is capped (200 source facts / reducer decisions / graph
answers, 50 citations matching the evidence-citation route cap). Exceeding a cap
truncates the layer, sets `bounds.truncated`, records the layer name, and marks
the answer partial — a large investigation produces a bounded, signalled
artifact, never an unbounded dump.

### Redaction

Every packet declares the `share_safe_v2` profile: addressable handles and
bounded reducer-approved summaries only — no raw payloads, secrets, hostnames,
keys, or transport metadata. This mirrors the operator-digest redaction posture.

## API / MCP / CLI parity requirements (written before implementation)

The emitters MUST satisfy:

1. **One builder.** API, MCP, and CLI compose through
   `NewInvestigationEvidencePacket`; no surface re-assembles layers.
2. **Identical truth labels** for the same investigation across surfaces.
3. **Identical missing-evidence behavior** (same hop, reason, drilldown).
4. **Identical refusal mapping** (refusal state ↔ documented API/MCP error code
   ↔ CLI refusal field).
5. **Identical redaction** (`share_safe_v2` everywhere; no surface widens scope).

## Proof gates (this PR)

- Contract types with focused tests for determinism/reproducibility, bounds and
  truncation, semantic policy gating, redaction metadata, refusal states,
  referential integrity, missing-hop explanation, and the AnswerPacket
  summary-drop invariant. `go test ./internal/query/`.
- Schema compatibility: the v2 schema is additive — it reuses, not replaces, the
  v1 answer contracts; existing answer-packet and citation tests stay green.
- Redaction and bounded-payload review covered by the validation gates and the
  bounds tests.
- Public reference + this design doc; `mkdocs build --strict`.

No-Observability-Change: this gate adds contract types and validation only. It
performs no graph, content-store, provider, collector, reducer, or queue reads
and adds no runtime span. The emitters (#3141, #3142) own the runtime reads and
their observability/performance evidence.

## Follow-ups

- #3141 supply-chain impact emitter + CLI command + markdown/HTML renderers.
- #3142 deployable-unit and drift emitter.
- #3143 dogfood benchmark against peer artifacts.
