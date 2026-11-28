# apilib/admin — Proof-Read Family

The admin proof reads served from fixture stores by the external proof
consumer (Refs eshu-hq/eshu-api#456).

Layout:

- `types.go` — the 7-method `ReadStore` port (`ListWorkItems`,
  `ListDeadLetterWorkItems`, `ListReducerInputInvalidFacts`,
  `ListChangedSincePoisonedLinks`, `ListReplayEvents`, `ListDecisions`,
  `ListEvidence`) with row/filter types. This file is the single
  definition site: core aliases these types (`type X =
  apilibadmin.X`), so a core store satisfies this interface; keep
  signatures identical on both sides.
- `tuning.go` — the static tuning handler: pinned `v0` /
  `not_applicable` constants, body byte-identical to the core
  constants.
- `workitems.go` — the work-items query handler plus slice conversion:
  limit default 100 with trims, `{count, items}` with exact
  field/RFC3339/pointer-omission parity, optional store-error hook for
  fence verdicts.
- `mount.go` — `MountProofReads(mux, store, opts)`: per-family mounts
  only, no credentials taken.
- `deadletters.go`, `decisions.go`, `inputinvalid.go`,
  `poisonedlinks.go`, `replayevents.go` — the five B-query handlers
  (`ServeDeadLettersQuery`, `ServeDecisionsQuery` with the
  `ListEvidence` join, `ServeInputInvalidFactsQuery`,
  `ServePoisonedLinksQuery`, `ServeReplayEventsQuery`): same contract
  as the work-items query (nil store 503, bad JSON 400, limit default
  100 with trims, fixed 500, 499 client-cancel, fence hook first),
  with core key/RFC3339 parity. Required-selector 400s
  (`repository_id`/`source_run_id`, `scope_id`/`generation_id`,
  `status`) mirror core validation.
- `bqueries_mount.go` — one `Mount*` per B-query family. These stay out
  of `MountProofReads` until the core `OmitProofReads` surface covers
  them, so one mux never answers a route twice.
- `maintenance.go` — the reindex-progress read
  (`ServeReindexProgress` over the read-only `MaintenanceStore` port):
  fleet watermark plus per-scope effective watermarks with core
  classification/rendering parity (`complete` only when nothing is
  pending or never-seen, unfinished scopes first, at most 10 samples).
- `deltaunion.go` — the read-only delta-union completeness check
  (`ServeDeltaUnionCompleteness` over the synchronous
  `DeltaUnionStore` port): explicit scope filter required, 500-scope
  cap with the narrow-the-filter text, graph outages answered as a
  fixed local 500 (never a partial `complete`), never repairs.
- `deltaunion_check.go` / `deltaunion_expectations.go` /
  `deltaunion_graphverdict.go` — the checker itself behind
  `DeltaUnionStore`: `DeltaUnionCompletenessChecker` over the narrow
  `DeltaUnionActiveSource` (ACTIVE snapshot; documented semantics, no
  exported SQL) and `DeltaUnionGraphRunner` ports, the exported
  anchored `DeltaUnionGraphFilesCypher`, the expectation assembly and
  comparison functions, and `DeltaUnionGraphErrorHandler`, the non-nil
  hook that reproduces the core graph verdicts. Core delegates here,
  so there is one implementation.
- `MountReindexProgress` / `MountDeltaUnionCompleteness` (in
  `bqueries_mount.go`) — one `Mount*` per maintenance/graph family.
  These stay out of `MountProofReads` like the B-query mounts, so one
  mux never answers a route twice.
