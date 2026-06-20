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
| Graph/query truth | `graph_answers` | The materialized edges/path hops the query surface returns, each with a present flag, truth class, and the `source_fact_ids` that back a present hop. |
| Citations | `citations` | Addressable [evidence citation handles](evidence-citation-handles.md) — never raw payloads. |
| Missing evidence | `missing_evidence` | Each unresolved hop with a reason and an optional bounded drilldown. |
| Semantic (optional) | `semantic_observations` | Labeled, policy-gated provider observations. Absent in a deterministic build. |
| Reproduce | `reproduce` | Bounded commands, routes, or tools that regenerate the packet's evidence. |
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

| Family | Investigation | Status |
| --- | --- | --- |
| `supply_chain_impact` | Vulnerable package → advisory → SBOM/image → workload → service. | Implemented (#3141) |
| `deployable_unit` | Source repo → deployment config → image/workload → reducer admission. | Implemented (#3142) |
| `drift` | IaC vs runtime drift for a deployable unit or cloud resource. | Implemented (#3142) |
| `service_context` | Service dossier (code, deployment, incidents). | Planned (#3143 dogfood) |

### Supply-chain impact example

```bash
eshu investigation export \
  --family supply_chain_impact \
  --subject advisory_id=GHSA-xxxx-yyyy-zzzz \
  --subject package_id=pkg:golang/example.com/vuln \
  --format md --out impact.md
```

The command reads the bounded `GET /api/v0/supply-chain/impact/explain` route,
maps the reducer-owned explanation into the v2 packet through the shared
`BuildSupplyChainImpactPacket` composer, and writes a deterministic artifact
(owner-only `0600`). A 404 / not-found explanation yields a `scope_not_found`
refusal packet; an incomplete advisory-plus-target scope yields the same refusal
without calling the API.

### Deployable-unit and drift examples

```bash
# Deployable-unit truth: accepted, ambiguous, and rejected candidates explicitly.
eshu investigation export \
  --family deployable_unit \
  --subject scope_id=<ingestion-scope> \
  --subject generation_id=<generation-id> \
  --subject repository_id=<repository-id> \
  --format md --out deployable-unit.md

# Runtime drift: IaC-vs-runtime reconciliation state per cloud resource.
eshu investigation export \
  --family drift \
  --subject scope_id=<account-or-project> --subject provider=aws \
  --format md --out drift.md
```

The `deployable_unit` family reads `GET /api/v0/evidence/admission-decisions`
(domain `deployable_unit_correlation`) and maps each correlation decision into
the reducer-decision layer: `admitted`, `ambiguous`, `rejected`, and `stale`
candidates are all represented explicitly, never hidden. Reads require
`scope_id` and `generation_id`; a `repository_id` or `repo_id` narrows to the
repository anchor persisted by the reducer. Workload and service subjects remain
packet context until reducer decisions are persisted with those anchor kinds. A
decision whose canonical write was performed becomes a present graph edge. The
`drift` family reads `POST /api/v0/cloud/runtime-drift/findings` and maps each
finding into a reducer decision whose state reflects the drift kind
(orphaned/unmanaged → `rejected` reconciliation, ambiguous → `ambiguous`,
unknown → `missing_evidence`), with a matched Terraform address surfaced as a
present `MANAGED_BY_TERRAFORM` edge and safety-gate warnings carried as
limitations.

## Reading the packet (operators)

Read the layers top-down:

1. **`answer`** — the one-line verdict, `truth_class`, and whether it is
   `supported`/`partial`. A partial answer always lists why in
   `unsupported_reasons`.
2. **`reducer_decisions`** — what the reducer concluded. The `state` is the
   admission-audit verdict; `ambiguous`/`rejected`/`stale` rows are the ones to
   investigate. Each row's `source_fact_ids` point back into `source_facts`.
3. **`graph_answers`** — what was actually materialized into the graph
   (`present: true`), with the backing `source_fact_ids`.
4. **`missing_evidence`** — the named gaps and why they are unresolved.
5. **`reproduce`** — the exact route/tool/command to regenerate the evidence.
6. **`bounds` / `validation` / `redaction`** — truncation state, the contract
   gates that passed, and the share-safe posture.

A `refusal` packet (no `supported` answer) means the scope was unanswerable;
read `answer.unsupported_reasons` for the reason.

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

### HTTP and MCP surfaces

The implemented packet families are exposed directly on HTTP and through MCP
tools. Each HTTP response returns the packet as the `data` payload of the
canonical envelope when clients send
`Accept: application/eshu.envelope+json`; plain JSON clients receive the same
packet body without the envelope.

| Family | HTTP route | MCP tool | Required scope |
| --- | --- | --- | --- |
| `supply_chain_impact` | `GET /api/v0/investigations/supply-chain/impact/packet` | `export_supply_chain_impact_packet` | One finding id or a bounded advisory/package/repository/image/workload/service selector. Scoped tokens must include a repository selector that resolves inside the caller grant before store reads. |
| `deployable_unit` | `GET /api/v0/investigations/deployable-unit/packet` | `export_deployable_unit_packet` | `scope_id` and `generation_id`; optional `repository_id` / `repo_id` narrows to one repository anchor. |
| `drift` | `GET /api/v0/investigations/drift/packet` | `export_cloud_runtime_drift_packet` | `scope_id` or a cloud scope alias (`account_id`, `project_id`, `subscription_id`); optional `provider` and `cloud_resource_uid`. |

All three surfaces accept `max_source_facts` as a lowering-only cap for the
`source_facts` layer. MCP dispatch is transport-only: it forwards the bounded
inputs to the HTTP routes and preserves the returned canonical envelope instead
of rebuilding packet layers.

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
