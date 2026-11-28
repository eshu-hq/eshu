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
