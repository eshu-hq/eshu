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
