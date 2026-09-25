# querycontract/answer

Answer-packet and answer-metadata contract shared by the query handler
families: the evidence-backed response plan a handler attaches so a prompt
surface can present an answer, its caution level and its missing evidence
without re-deriving truth.

## What is here

| file | holds |
| --- | --- |
| `packet.go` | `AnswerPacket`, `AnswerPacketInput`, `NewAnswerPacket`, `ClassifyAnswerTruth`, `FreshnessReason`, `FreshnessNextCheckAsRecommendedCall`, `CloneTruthEnvelope` |
| `metadata.go` | `AnswerMetadata`, `AnswerMetadataSchemaVersion`, `AttachAnswerMetadata`, `BuildAnswerMetadata`, `AnswerMetadataFromData`, `AnswerMetadata.WithDefaults` |
| `packet_companion.go` | `AnswerPacketCompanionInput`, `WithAnswerPacketCompanion`, and the code-topic and service-story builders (`CodeTopicAnswerSummary`, `CodeTopicAnswerLimitations`, `CodeTopicEvidenceHandles`, `ServiceStoryAnswerData`) |

`AnswerTruthClass` and its constants stay in the parent's `truth.go`: they are
part of the truth vocabulary, and this package only maps an envelope onto them.
The class mapping is documented in `docs/public/reference/answer-packets.md`.

## Dependencies

Inbound: 18 non-test files and 2 test files import this package, all under
`go/internal/query`. Packages outside it (`ask/engine`, `serviceintel`, `mcp`,
`cli`, `answerquality`, `answernarration`, `askwiring`) still name the root
aliases in `query/ask_alias.go` and `query/answer_metadata_alias.go`.

Outbound: the parent `querycontract` and `querycontract/evidence`. The parent
must not import this package, or the import becomes a cycle.

## Telemetry

None. The package builds values in memory and performs no I/O; the handlers
that call it own their spans and metrics.
