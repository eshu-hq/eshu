# Investigation Evidence Packet Contract (v2)

An **investigation evidence packet** is a portable, source-backed artifact that
carries the full evidence chain for one high-value investigation — a vulnerable
package's blast radius, a deployable unit's truth, a drift finding — in a single
file a user can inspect, diff, and share. It is the v2 successor to the
answer-facing companions ([Answer Packet Contract](answer-packets.md) and
`answer_metadata.v1`): where those are *views over one `ResponseEnvelope`*, the
v2 packet is a *self-contained artifact* that separates the evidence layers so
each can be audited on its own.

The schema identifier is `investigation_evidence_packet.v2`. The contract types
live in `go/internal/query/investigation_packet.go`,
`investigation_packet_build.go`, and `investigation_packet_validate.go`. The
packet reuses the existing truth, freshness, and evidence-handle contracts from
`contract.go` and `evidence_citation.go` rather than redefining them.

Read the [Truth Label Protocol](truth-label-protocol.md) and
[Answer Packet Contract](answer-packets.md) first: the v2 packet does not invent
truth, it carries copies of the truth labels the query layer already produced.

## Why this contract exists

Peer tools win the *instant local artifact* experience: a user runs one command
and gets a browsable graph plus a JSON/markdown report. Eshu has stronger,
reducer-owned truth, but that proof has been harder to carry, inspect, and
share. The v2 packet closes that gap **without weakening reducer-owned truth or
no-provider deterministic behavior**:

1. It is emitted as a local file (JSON, with markdown/HTML renderers) so the
   proof is portable.
2. It separates raw evidence, reducer decisions, and graph/query truth so a
   reader can see *what was observed*, *what the reducer decided*, and *what the
   graph answers* — not a flattened summary.
3. It names missing hops and freshness explicitly so gaps are visible, never
   hidden behind a confident sentence.
4. It is deterministic with no provider keys; any semantic observation is
   optional, labeled, and policy-gated.

## Evidence layers

The packet keeps the epic's required separation. Every layer is a top-level
field and is always present (empty arrays are kept so the schema is stable):

| Layer | Field | Meaning |
| --- | --- | --- |
| Identity | `identity` | Family, canonical scope keys, question, observed generation, profile, backend, and basis. |
| Truth | `truth`, `freshness` | A copy of the canonical `TruthEnvelope` plus a surfaced freshness label. |
| Answer | `answer` | The short user-facing answer plan (truth class, summary, supported/partial, reasons). |
| Raw evidence | `source_facts` | Observed facts *before* any reducer decision: identity, evidence family, collector, generation, a bounded summary, and an optional citation handle. |
| Reducer decisions | `reducer_decisions` | Reducer-owned admission/correlation/drift decisions with an admission-audit state (`admitted`, `rejected`, `ambiguous`, `stale`, `missing_evidence`). |
| Graph/query truth | `graph_answers` | The materialized edges/path hops the query surface returns, each with a present flag and truth class. |
| Citations | `citations` | Addressable [evidence citation handles](evidence-citation-handles.md) — never raw payloads. |
| Missing evidence | `missing_evidence` | Each unresolved hop with a reason and an optional bounded drilldown. |
| Semantic (optional) | `semantic_observations` | Labeled, policy-gated provider observations. Absent in a deterministic build. |
| Bounds | `bounds` | Per-layer caps and truncation state. |
| Redaction | `redaction` | The share-safe redaction posture. |
| Validation | `validation` | The contract-gate results. |

### Referential integrity

A `reducer_decisions[*].source_fact_ids` or
`semantic_observations[*].source_fact_ids` entry must reference a `fact_id` or
`stable_key` present in `source_facts`. The builder fails the packet rather than
ship a dangling citation chain, so every decision is traceable to the raw
evidence it was made from.

## Packet identity and determinism

`packet_id` is `investigation-evidence-packet:<sha256>` derived from the identity
(schema, family, sorted scope keys, question, generation, basis, profile,
backend) **plus a content digest over the evidence layers**. Consequences:

- The same inputs always produce the same `packet_id` and the same marshaled
  bytes. A no-provider build is reproducible.
- Two packets with the same identity but different evidence get different ids,
  so a packet id is also a content fingerprint.

`encoding/json` marshals maps with sorted keys, so the scope map and the digest
are stable regardless of insertion order. The packet core never reads the wall
clock; any timestamp comes from the observed evidence, not packet construction.

## Deterministic vs semantic

`identity.basis` is `deterministic` by default. Semantic observations are only
permitted when:

1. the caller sets the `AllowSemantic` policy gate, **and**
2. each observation carries the `semantic_observation` label and non-empty text.

If semantic observations are supplied without the policy gate, the build returns
an error — a no-provider build can never silently include provider output. When
semantic observations are present and allowed, `basis` becomes
`semantic_augmented` so the posture is explicit on the wire. Semantic
observations never change the deterministic layers and never carry truth labels.

## CLI contract

The packet is emitted by the `eshu investigation` command family. The export
surface is:

```text
eshu investigation export \
  --family <supply_chain_impact|deployable_unit|drift|service_context> \
  --subject key=value [--subject key=value ...] \
  --format <json|md|html> \
  --out <path> \
  [--allow-semantic] [--max-source-facts N]
```

### Supported families

| Family | Investigation | Emitter |
| --- | --- | --- |
| `supply_chain_impact` | Vulnerable package → advisory → SBOM/image → workload → service. | #3141 |
| `deployable_unit` | Source repo → deployment config → image/workload → reducer admission. | #3142 |
| `drift` | IaC vs runtime drift for a deployable unit or cloud resource. | #3142 |
| `service_context` | Service dossier (code, deployment, incidents). | #3143 dogfood scenario |

### Refusal states

A family that cannot be answered returns a **valid refusal packet** — never a
partial or fabricated artifact. Refusal states and their API/MCP error mapping:

| Refusal state | When | API/MCP error code |
| --- | --- | --- |
| `unknown_family` | Family outside the supported set. | `invalid_argument` |
| `scope_not_found` | Subject scope resolves to no canonical entity. | `scope_not_found` |
| `profile_unsupported` | Active profile cannot answer the family. | `unsupported_capability` |
| `backend_unavailable` | Graph or content backend is unavailable. | `backend_unavailable` |

A refusal packet is unsupported (`answer.supported = false`), carries no
confident summary, records the reason in `answer.unsupported_reasons`, and still
passes validation so it can be written to disk and shared.

## API / MCP / CLI parity

The packet is one contract behind three surfaces. Parity requirements, to be met
by the emitters (#3141, #3142):

- **Same builder.** All three surfaces compose the packet through
  `NewInvestigationEvidencePacket`; no surface re-assembles layers ad hoc.
- **Same truth labels.** The `truth`, `freshness`, and `answer.truth_class`
  fields are identical for the same investigation regardless of surface.
- **Same missing-evidence behavior.** A hop that is missing on the CLI is missing
  on the API and MCP responses, with the same reason and drilldown.
- **Same refusal mapping.** A refusal state maps to the documented API/MCP error
  code and to the equivalent CLI exit/refusal field.
- **Same redaction.** Every surface emits the `share_safe_v2` redaction profile;
  no surface widens scope or embeds raw payloads.

## Bounds and payload size

Every evidence layer is capped (defaults: 200 source facts, 200 reducer
decisions, 200 graph answers, 200 missing-evidence hops, 50 semantic
observations, 50 citations — the citation cap matches the evidence-citation
route). When a layer exceeds its cap it is truncated, `bounds.truncated` is set,
the layer name is recorded in `bounds.truncated_layers`, and the answer is marked
partial. A large investigation therefore produces a bounded, clearly-signalled
artifact instead of an unbounded dump. An emitter may **lower** a cap through the
bounds override, but an override that tries to **raise** a cap above the default
is clamped back to the default — raising a cap requires a code change with
performance evidence, not a per-call override.

## Redaction

Every packet declares the `share_safe_v2` redaction profile with the applied
rules `addressable_handles_only`, `reducer_approved_summaries`,
`no_raw_fact_payloads`, and `no_transport_metadata`. The packet carries only
addressable handles and bounded, reducer-approved summaries; it never embeds raw
fact payloads, secrets, hostnames, keys, or transport metadata. This mirrors the
[Operator Digest Contract](operator-digest.md) redaction posture.

## Reused contracts

The v2 packet does not duplicate existing shapes. It reuses:

- `TruthEnvelope`, `TruthFreshness`, and `AnswerTruthClass` from `contract.go`
  and `answer_packet.go`.
- The evidence-handle shape from
  [evidence citation handles](evidence-citation-handles.md).
- The schema-versioned, validated, redaction-aware artifact pattern from the
  [Operator Digest Contract](operator-digest.md).
