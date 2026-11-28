# internal/reducer/code/owners

## Purpose

Projects `Repository-[:DECLARES_CODEOWNER]->CodeownerTeam` edges from
directly-emitted `codeowners.ownership` facts (issue #5419 Phase 3, moved out
of the reducer root under issue #6061).

`Handler.Handle` loads `repository`/`codeowners.ownership` facts,
extracts canonical rows through `ExtractEdgeRowsWithQuarantine`, and
builds a `deltaScope` mirroring the inheritance family's: a CODEOWNERS file
is repo-scoped like a source file, so a changed or deleted CODEOWNERS
`source_path` retracts the prior generation's edges scoped to that path — with
one exception: because CODEOWNERS winner-resolution is whole-repo (one winner
among three known locations), a delta that touches any of those three
locations forces a whole-repository retract instead (issue #5419 P1).

## Ownership boundary

**Owns:** codeowners.ownership fact extraction
(`ExtractEdgeRowsWithQuarantine`), materialization (`Handler`), and
its delta-scope/retract-row construction.

**Does not own:** the shared-projection worker/runner machinery that consumes
the edges this package writes (reducer root), or
`internal/collector/codeowners.CandidatePaths()` (the collector's copy of the
three recognized CODEOWNERS locations, which this package's `candidatePaths`
duplicates rather than imports, per the collector/reducer ownership
boundary).

## Exported surface

| symbol | what it is |
|---|---|
| `Handler` | the domain handler for `codeowners_ownership` intents |
| `ExtractEdgeRowsWithQuarantine` | pure extraction: fact envelopes -> canonical DECLARES_CODEOWNER edge rows, with per-fact quarantine |
| `LoadMaterializationFacts` / `MaterializationFactKinds` | the scoped fact-kind loader for `repository`/`codeowners.ownership`, and the kind set it requests |

The reducer root wires `Handler` in `defaults_domain_catalog.go`, keeping the
`reducer.CodeownersOwnershipEdgeMaterializationHandler` spelling through the
codeowners stanza of `compat_decode.go`.

## Dependencies

`internal/facts` (`Envelope`), `internal/reducer/contract` (`Intent`,
`Result`, `PriorGenerationCheck`, `DomainCodeownersOwnership`,
`DomainCodeownersOwnershipEdges`, `IntentActionUpsert`),
`internal/reducer/factload` (`FactLoader`, `LoadFactsForKinds`,
`FactKindRepository`, `FactKindCodeownersOwnership`),
`internal/reducer/factdecode` (`QuarantinedFact`, `PartitionDecodeFailures`,
`RecordQuarantinedFacts`, `InputInvalidSubSignals`),
`internal/reducer/schemadecode` (`DecodeCodeownersOwnership`),
`internal/reducer/sharedintent` (`Row`, `EdgeWriter`), and
`internal/reducer/payloadcore` (payload accessors). No dependency on the
reducer root.

## Telemetry

`Instruments` records `eshu_dp_reducer_input_invalid_facts_total` for a
quarantined fact. `Handle` emits two structured logs, "codeowners ownership
materialization started" and "codeowners ownership materialization
completed", with `scope_id`, `generation_id`, `domain` (started only), and
`edge_count` (completed only). Shared-projection writes and retracts go
through the generic `EdgeWriter` the reducer root wires, which is unchanged
by this move.

## Gotchas / invariants

- **CODEOWNERS winner-resolution is whole-repo.** A delta touching one of the
  three candidate locations (`.github/CODEOWNERS`, `CODEOWNERS`,
  `docs/CODEOWNERS`) MUST force a whole-repository retract, not a path-scoped
  one — the winning file may have switched between candidates entirely.
- **Duplicate (repo, path, pattern, owner) keys keep the highest
  `order_index`.** GitHub's CODEOWNERS resolution is last-match-wins;
  freezing the first occurrence's ordinal would let a stale rule outrank the
  true last match.
- **`candidatePaths` is a hand-duplicated copy, not an import**, of
  `internal/collector/codeowners.CandidatePaths()`, to respect the
  collector/reducer package ownership boundary. The two lists MUST stay in
  lockstep; `TestCodeownersOwnershipCandidatePathsMatchCollector` is the CI
  gate.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/code/README.md` — the `code/` namespace parent
- `docs/internal/design/reducer-target-tree.md` — the #6061 restructure
