# Fact IDL Decision: Registry + Go Types + Generated JSON Schema, Not Protobuf (#4574)

Status: Accepted.

Epic: #4566. Design: [Contract System v1](contract-system-v1.md) §3.3 (wire
protocol) and §9 (relationship to existing assets). Follow-up: #4599 (delete
`proto/eshu/data_plane`).

This ADR leads a stack. The design doc it links to lands in #4575, and the
`sdk/go/factschema` module it names as normative is scaffolded in #4567. Until
those merge, the `contract-system-v1.md` link and the `sdk/go/factschema` path
resolve on those PRs rather than on `main`. The status is Accepted because the
scaffold in #4567 is being built to this decision now, not because every
referenced artifact has already merged.

## 1. Context

Eshu's collector ↔ reducer boundary needs one normative interface definition
for fact payloads, the part of the fact envelope that actually carries data.
Two IDL candidates exist in the repository today:

- `specs/fact-kind-registry.v1.yaml` plus per-kind Go payload types (the
  `sdk/go/factschema` module, scaffolded in #4567) and JSON Schemas generated
  from those Go types.
- `proto/eshu/data_plane/*`: five hand-authored `.proto` files (`facts`,
  `queue`, `reducer`, `projection`, `scope`) and a `buf.gen.yaml` that names
  `go/gen/proto` as a generation target.

This ADR decides between them and records the disposition of the proto tree.

## 2. Decision

The normative collector ↔ reducer fact IDL is the fact-kind registry
(`specs/fact-kind-registry.v1.yaml`) plus versioned Go payload types in the
`sdk/go/factschema` module, with JSON Schemas generated from those Go types.
This is the single source of truth for fact payload shape.

## 3. Rejected alternative: gRPC / protobuf at the collector-reducer boundary

Adopting proto3 messages generated into `go/gen/proto`, with the reducer and
collectors depending on generated Go structs and a `.proto`-defined wire
format, was considered and rejected.

## 4. Rationale

No RPC hop exists at this boundary. Collector to reducer is store-and-forward
through Postgres: a collector commits fact envelopes to `fact_records`
(`schema/data-plane/postgres/003_fact_records.sql`), and the reducer later
reads and projects them off a queue. There is no synchronous call between the
two processes. gRPC's value proposition is transport-level typing over an RPC
hop; paying for a proto/gRPC stack where the connection is a database table
buys nothing.

proto3's optional-everything field semantics reproduce the exact failure this
system exists to eliminate. In proto3, an unset scalar field decodes to its
zero value rather than raising a decode error: a missing string becomes `""`,
a missing int becomes `0`. That is the accuracy hole described in the design
doc §1 and tracked under epic #4566, where a collector that drops or renames a
payload key produces a silent empty-string graph identity instead of a
visible failure. A JSON-Schema-validated typed decode fails closed on a
missing required field instead: the reducer's decode seam classifies it as an
`input_invalid` dead letter, an operator-visible event, rather than letting
wrong graph truth propagate downstream.

The ecosystem is JSON end to end already. Fact payloads are stored as
Postgres JSONB, the `sdk/go/collector` wire contract is JSON, conformance
fixtures are JSON, and both in-tree and external collectors speak JSON.
Introducing a proto encoding at just this one boundary adds a parallel wire
format that nothing else in the stack uses.

## 5. Disposition of `proto/eshu/data_plane`

Decision: delete, tracked as follow-up issue
[#4599](https://github.com/eshu-hq/eshu/issues/4599). This ADR does not
delete the tree itself; that stays scoped to #4599 because this ADR PR is
docs-only.

Reasoning:

- The tree is unwired dead scaffolding today. No Go code imports
  `proto/eshu/data_plane` or a generated `go/gen/proto` package; that
  package does not exist, because the `.proto` files were never generated
  into Go. No buf gate runs as part of the CI gate floor (see
  [Agent Orchestration Model](../agent-orchestration.md) for the enumerated
  gates). `buf.gen.yaml` itself notes it runs "without requiring a local buf
  install," and nothing currently consumes its output.
- The files are hand-authored, not generated from the Go schema package, so
  they cannot serve as the design doc's "future transport generated from the
  Go schema package" without being replaced wholesale anyway. Keeping
  hand-maintained `.proto` files around as a stand-in for that future state
  leaves a second, non-source-of-truth definition of fact shape sitting next
  to the real one, drifting until someone notices.
- Deleting the tree does not foreclose a future proto transport. If a
  synchronous RPC hop is ever introduced at some boundary, design §3.3 states
  any such transport is regenerated from `sdk/go/factschema`, not
  hand-maintained independently.

## 6. Consequences

- `specs/fact-kind-registry.v1.yaml` and `sdk/go/factschema` are the only
  place a collector or reducer author needs to look to find fact payload
  shape, current and historical.
- `buf.gen.yaml` stops implying `proto/eshu/data_plane` is an active source
  of truth; its leading comment now points here instead.
- #4599 owns the actual deletion of `proto/eshu/data_plane` and `buf.yaml`/
  `buf.gen.yaml` cleanup once nothing references them as aspirational.
- Any future transport-level typing proposal at this boundary must justify
  the RPC hop it serves and generate its types from `sdk/go/factschema`
  rather than hand-authoring a parallel schema.
