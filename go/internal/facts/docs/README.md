# Documentation facts

## Purpose

`docs` owns the documentation fact family: the fact kinds a documentation
collector emits, their schema versions, the payload structs it fills, the
stable-id derivations that give each payload durable graph identity, the
bounded source-ACL vocabulary, and the encoders that map each payload onto
`sdk/go/factschema/documentation/v1`.

It moved here from `go/internal/facts` in issue #6776, which nested the
flat facts root back under the 40-non-test-file dirgate cap.

## Ownership boundary

Declarations and pure payload mapping. This package does not collect,
admit, project, or store anything, and it owns no HTTP or MCP surface. The
graph truth derived from these facts belongs to `internal/reducer` and
`internal/projector`; the read surfaces belong to `internal/query` and
`internal/mcp`.

The package is named `docs` rather than `documentation` because `go/build`
excludes every `.go` file in a package named `documentation`. See `doc.go`.

## Exported surface

- `SourceFactKind`, `DocumentFactKind`, `SectionFactKind`, `LinkFactKind`,
  `EntityMentionFactKind`, `ClaimCandidateFactKind`, `FindingFactKind`,
  `EvidencePacketFactKind` — the family's fact kinds
- `FactKinds`, `SchemaVersion` — the accessors the facts root's
  `schemaVersionFamilies` table dispatches through
- `SourcePayload`, `DocumentPayload`, `SectionPayload`, `LinkPayload`,
  `EntityMentionPayload`, `ClaimCandidatePayload`, `OwnerRef`, `EvidenceRef`,
  `ACLSummary` — the collector-facing payload structs
- `SourceStableID`, `DocumentStableID`, `SectionStableID`, `LinkStableID`,
  `EntityMentionStableID`, `ClaimCandidateStableID`, `FindingStableID`,
  `EvidencePacketStableID` — durable identity derivations
- `EncodeSource`, `EncodeDocument`, `EncodeSection`, `EncodeLink`,
  `EncodeEntityMention`, `EncodeClaimCandidate`, `EncodeFinding`,
  `EncodeEvidencePacket` — payload encoders
- `EncodeACLSummary`, `EncodeEvidenceRefs` — shared with the facts root's
  `semantic_encode.go`, which reuses this family's ACL and evidence shapes
- `BoundedSourceACLState`, `SourceACLState*`, `ValidSourceACLState` — the
  bounded source-ACL state vocabulary
- `MentionResolution*`, `ClaimAuthorityDocumentEvidence` — bounded
  resolution and authority vocabularies

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/facts/encode` — `StableID` and the payload pointer/JSON-shape
  helpers this family shares with the facts root
- `internal/semanticpolicy` — the policy vocabulary `acl.go` bounds against
- `sdk/go/factschema` and `sdk/go/factschema/documentation/v1` — the
  contracts module this family encodes into

It must not depend on `internal/facts`: the root imports this package, so
the reverse edge is an import cycle.

## Dependents

- `internal/facts` — `compat_docs.go` re-exports every pre-move spelling,
  and `semantic.go` / `semantic_encode.go` reuse `ACLSummary`,
  `EvidenceRef`, `EncodeACLSummary`, and `EncodeEvidenceRefs`
- `internal/doctruth`, `internal/semanticdocs` and the documentation
  collector path reach these names through `facts.Documentation*` today

## Telemetry

None. This package emits no metrics, spans, or logs; it has no runtime
behavior to observe. Operator signals for documentation ingestion live with
the collector and reducer packages that call it.

## Gotchas / invariants

- A stable-id derivation is frozen. Changing which payload fields it hashes
  orphans every node already written under the old id.
- A payload struct field is mirrored by `sdk/go/factschema/documentation/v1`.
  Adding an optional field is minor; removing, renaming, retyping, or
  reinterpreting one is major and needs a conversion shim in the contracts
  module. See `docs/public/reference/fact-schema-versioning.md`.
- `EncodeFinding` and `EncodeEvidencePacket` copy named open fields through
  verbatim. Adding a verifier-owned field means adding it to that copy list,
  or it is dropped.
- `encode.JSONShapeMap` normalizes integers to `float64` and
  `map[string]string` to `map[string]any`. A test that compares an encoder's
  output against a Go literal must go through the same normalization.

## Related docs

- `docs/public/reference/fact-schema-versioning.md`
- `docs/internal/design/contract-system-v1.md`
- `docs/internal/naming.md`
