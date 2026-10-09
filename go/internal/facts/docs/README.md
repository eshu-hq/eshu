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

- `internal/facts` — `semantic.go` / `semantic_encode.go` reuse
  `ACLSummary`, `EvidenceRef`, `EncodeACLSummary`, and `EncodeEvidenceRefs`
  (the transitional `compat_docs.go` re-export was retired in #6950
  batch 3 once the last caller moved)
- `internal/doctruth`, `internal/semanticdocs` and the documentation
  collector path reach these names through `docs.*` directly

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

## Evidence

No-Regression Evidence: #6950 batch 3 moves the 48-entry documentation
compat family onto this package and deletes `compat_docs.go`. All 48
entries were aliases or thin forwarders to the identical values, types,
and bytes, so callers are value-identical by construction; no fact kind
string, payload shape, or registry output changes (contract classification:
patch — no contract surface touched).

- Baseline: `35d869c4a9`, `go test -count=1` on the 23 affected-package
  targets (every Go directory the diff touches): 22 ok, 1 fail
  (`TestFetchChurnZombiesDrainedByReaper`, which fails identically on
  the clean base on this host and passes in CI).
- After: same command on the branch: 22 ok, same single failing package
  (ok-package set byte-identical after timing strip).
- Backend/version: go1.26.9 linux/amd64, in-memory test backends.
- Contract gates: `verify-factschema-diff.sh`, `verify-payload-usage-manifest.sh`,
  `verify-fact-kind-registry.sh`, `verify-contracttest.sh` all exit 0, and the
  regenerated registry outputs are byte-identical (no diff).
- Detector follow-through: the `internal/mcp` kind-consumer gate's
  `facts.`-only matchers (`factsPackageIdentRefPattern`,
  `factsSelectorWireKind`) now also accept this batch's `docs.` and
  `factsdocs.` spellings; identifiers still resolve through the leaf-owned
  const table, so unknown spellings match nothing. `TestEveryRegistryKindHasConsumerOrDisclosure`
  passes.
- Ratchet follow-through: `TestContractEncodeAdoptionRatchet` (in
  `internal/collector`, outside the caller set) matches encoder calls by
  bare name, so its eight expected `EncodeDocumentation*` names now use
  the canonical `Encode*` spellings the migrated call sites carry. The
  test passes; no encoder behavior changes.
- Telemetry/status evidence: no new metric, span, or log; the deleted
  aliases emitted none.
- Why safe: compiler-checked retarget across 115 files (893 qualified
  refs, plus the bare-name ratchet expectations); the RED run (`go build`
  failing on the undefined `Documentation*` names with the compat file
  deleted) proves the surface is gone, and the GREEN run proves every
  former caller resolves to the same value.

No-Observability-Change: this batch adds, removes, and renames no operator
signal.
