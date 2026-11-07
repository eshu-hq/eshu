# querycontract/packet

Investigation evidence packet contract: the envelope every investigation
family builds, the builders that fill it, and the render, validate, and
identity helpers that read it.

## What is here

| file | holds |
| --- | --- |
| `envelope.go` | `InvestigationEvidencePacket`, `InvestigationFamily`, `PacketRefusalState`, `InvestigationPacketInput`, `NewInvestigationEvidencePacket` |
| `build.go` | packet assembly: identity stamping, bounds clamping, refusal packets |
| `render.go` | `ParseInvestigationPacketFormat`, `RenderInvestigationPacket` (JSON, Markdown, HTML) |
| `validate.go` | internal contract gates run by `NewInvestigationEvidencePacket`: bounds, citations, refusal consistency |
| `identity.go` | packet id, content digest, reducer-decision and reference checks |
| `drift.go` | `BuildDriftPacket` and drift finding vocabulary |
| `supply_chain.go` | `BuildSupplyChainImpactPacket`, `AddSubjectKey` |
| `supply_chain_types.go` | advisory, explanation, and scope types for supply-chain packets |
| `deployable_unit.go` | `BuildDeployableUnitPacket` and admission-decision vocabulary |
| `readiness.go`, `readiness_classify.go`, `readiness_normalize.go`, `readiness_unsupported.go` | readiness labels, classification, normalization |
| `readiness_source_age.go`, `source_state.go` | source-state age, freshness, and snapshot helpers |

## Dependencies

Inbound: the parent `query` package (envelope builders for the served packet
routes, compat buckets, drift view) and the CLI `investigation`, `support`,
and `compparity` packages. Sibling leaves do not import this package.

Outbound: the parent `querycontract` (truth and freshness vocabulary) and the
sibling `answer` and `evidence` leaves (packet answers, citation handles),
plus the standard library. This package must not import the parent `query`
package, or the parent's import of it becomes a cycle. The HTTP responders
stay in `query` (`investigation_packet_api.go`,
`investigation_packet_api_drift.go`): this leaf never touches `net/http`.

## Telemetry

None.
