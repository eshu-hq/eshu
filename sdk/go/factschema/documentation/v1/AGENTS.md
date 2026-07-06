# Documentation Fact Payloads Agent Rules

This directory is part of the public
`github.com/eshu-hq/eshu/sdk/go/factschema` Go module. It holds the
schema-version-1 typed payload structs for the `documentation` fact family:
`Source`, `Document`, `Section`, `Link`, `EntityMention`, `ClaimCandidate`,
`Finding`, and `EvidencePacket`. It must remain independent from Eshu
internals.

## Required Checks

- Read the root `AGENTS.md`, the module `AGENTS.md`, and
  `docs/internal/agent-guide.md` before edits.
- Do not import `github.com/eshu-hq/eshu/go/internal/...`. Keep the module
  standalone.
- After changing any payload struct's fields, run `go generate ./...` from
  the module root and commit the regenerated schema under `../../schema/`.
- Run `go test ./... -count=1` from the module root (`sdk/go/factschema`),
  `gofmt` on changed Go files, and `git diff --check` from the repo root.

## Contract Rules

- A field is required exactly when its json tag carries no `omitempty`; by
  the flat-struct convention required fields are also non-pointer, and
  optional fields are pointers or slices/maps carrying `omitempty`. Both the
  schema generator (`../../internal/schemagen`) and the decode seam's
  required-field check (`../../decode.go`) derive that set reflectively from
  the struct's own tags via `../../fields.go`, so there is no hand-maintained
  key list to keep in sync.
- `ClassificationInputInvalid` is the parent `factschema` package's own
  constant (`decode.go`). A reducer handler receiving it must dead-letter the
  fact rather than proceed with a zero-value struct.
- Removing, renaming, or narrowing a field is a major schema bump and needs a
  conversion shim in the parent package's decode seam (`decodeLatestMajor` in
  `../../decode.go`), not a silent edit here.
- **No struct here carries an Attributes pass-through.** No documentation
  fact kind is a polymorphic multi-shape envelope — each kind has one fixed
  field set across the documentation collector paths.
- **`Section` carries its OWN schema version** —
  `facts.DocumentationSectionFactSchemaVersion` (`"1.1.0"`) — distinct from
  every other kind in this family (`facts.DocumentationFactSchemaVersion`,
  `"1.0.0"`). The parent decode seam dispatches it on its own schema-major
  path. Do not fold it into the shared version constant or the shared decode
  dispatch.
- **Only `Document` and `EntityMention` have a reducer decode site today**:
  `buildDocumentationDeltaScope`
  (`go/internal/reducer/documentation_edge_delta_scope.go`) and
  `ExtractDocumentationEdgeRows`
  (`go/internal/reducer/documentation_edge_materialization.go`)
  respectively. The other six kinds
  (`Source`/`Section`/`Link`/`ClaimCandidate`/`Finding`/`EvidencePacket`) are
  typed-but-not-yet-consumed — see each struct's godoc and the package
  `README.md`'s "Only two kinds have a reducer decode site" section for why.
- **Required identity fields per kind** (all others optional):
  - `Document.DocumentID` — the reducer's delta-scope join key.
  - `EntityMention.DocumentID`, `EntityMention.SectionID`,
    `EntityMention.ResolutionStatus` — the edge extractor's actual
    identity/gating fields.
  - `Source.SourceID`/`SourceSystem`/`ExternalID`,
    `Section.DocumentID`/`RevisionID`/`SectionID`,
    `Link.DocumentID`/`LinkID`/`TargetURI`,
    `ClaimCandidate.DocumentID`/`SectionID`/`ClaimID`/`ClaimType`/`ClaimText`/`ClaimHash`/`Authority`
    — form their kind's `facts.Documentation*StableID`, or are always emitted
    by the collector; typed ahead of a future consumer.
  - `Finding.FindingID`/`FindingVersion`,
    `EvidencePacket.PacketID`/`EvidencePacket.FindingID` — form their kind's
    stable ID; emitted by `go/internal/doctruth`, read only by the query
    layer's raw SQL (out of scope for conversion; declared here only so a
    schema change cannot silently drop a field that SQL layer depends on).
- **`Finding`/`EvidencePacket` do not model doctruth's nested sub-objects**
  (`permissions`, `states`, `unified_evidence`, the embedded `finding`
  sub-map under `EvidencePacket`). The query layer reads those through
  generic Go map helpers over the decoded JSON map, not a fixed sub-shape;
  half-modeling them here would be a hollow contract with no reader. Do not
  add them without a real consumer and a design discussion.
  `EvidencePacket.LinkedEntities` is the ONE exception: it is a genuine
  top-level, query-read field (`documentation_target_read_model.go`'s
  `documentationPayloadMatchesTargetRef`, plus JSONB containment/
  authorization predicates), so it is declared with its own `LinkedEntityRef`
  type (`entity_type`/`entity_id` keys, distinct from `EvidenceRef`'s
  `kind`/`id` keys) — not a nested sub-object read only through generic map
  helpers.
- This package defines eight fact kinds. Adding a ninth kind or a `v2` major
  is follow-on work, not a casual edit.
