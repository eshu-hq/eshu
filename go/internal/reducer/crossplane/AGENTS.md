# Agent instructions: internal/reducer/crossplane

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The `crossplane_satisfied_by_materialization` reducer intent handler, its
loader/writer ports, and the row-extraction/edge-existence-confirmation
helpers (issue #6061). Projects Crossplane Claim -> XRD classification
decisions into canonical `SATISFIED_BY` graph edges.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/crossplane/README.md`
- `go/internal/projector/crossplanesatisfiedby/AGENTS.md` — the projector-side
  intent trigger that enqueues this package's domain
- `docs/internal/design/package-restructure.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it for `CrossplaneHandlers` wiring and
  the handler construction, never the reverse.
- **`GraphQueryRunner` in `graph_ports.go` is deliberately re-declared, not
  imported from root.** It is genuinely owned by root (shared with other
  still-in-root families), so importing it would violate the rule above. Go's
  structural typing makes the local declaration safe: do not "fix" this by
  adding a root import, and do not delete the local declaration without first
  hoisting the real one to a shared leaf package both sides import.
- **The redrive ledger write must happen strictly after the post-write
  existence confirmation**, in
  `CrossplaneSatisfiedByMaterializationHandler.recordRedriveLedgerForConfirmedEdges`.
  Reordering it before `EdgeExistenceReader.Run` (or skipping the confirmation
  read) risks fencing a target the handler never actually satisfied (issue
  #5476 P1-b).
- **An ambiguous candidate (2+ matching XRDs) never fabricates a
  representative edge.** It is counted in `ambiguousSkipped` and produces no
  row. Do not "resolve" ambiguity by picking the first or nearest match.
- **XRD candidates are deduped by uid per `(group, claim_kind)` join key**
  because the handler's `loadEdgeFacts` appends the cross-scope
  `ListActiveCrossplaneXRDFacts` load unconditionally to the own-scope
  content_entity facts, so a same-repo XRD can appear twice in the same
  `envelopes` slice. Do not remove the uid-dedup in
  `ExtractCrossplaneSatisfiedByEdgeRows` — the B-7 golden-corpus rc-151
  assertion depends on a same-repo Claim/XRD pair NOT reading as ambiguous.

## Common changes

Adding a new candidate entity type or join key: extend
`crossplaneContentEntityType`'s switch and the corresponding
`crossplane*CandidateFromPayload` builder in
`crossplane_satisfied_by_edge_rows.go`, then update
`go/internal/projector/crossplanesatisfiedby`'s `triggerFact` switch so the
projector still enqueues the intent for the new entity type (see that
package's own AGENTS.md).

## Failure modes to avoid

- Trusting `WriteCrossplaneSatisfiedByEdges` returning `nil` as proof an edge
  committed. It deliberately no-ops (nil error, no edge) when either endpoint
  node is absent from the graph — only the post-write existence confirmation
  proves it.
- Recording the redrive ledger for an unconfirmed row. The ledger's meaning
  is "this target is satisfied for this XRD identity," true only once the
  edge is actually committed (issue #5476).
- Matching on an empty `group` or `claim_kind`. Both a core-group Claim
  (`apiVersion` with no `"/"`) and a cluster-scoped/malformed XRD yield an
  empty join-key field, which must never match another empty field.

## Do not change without ADR review

- `CrossplaneSatisfiedByEdgeWriter`'s idempotency contract: implementations
  MUST be idempotent by `(claim uid, SATISFIED_BY, xrd uid)` — `cmd/reducer`
  and the retract path both depend on this.
- The `(group, kind)` / `(spec.group, spec.claimNames.kind)` resolution
  identity — the redrive ledger, the cross-scope redrive sweep
  (`internal/storage/postgres`), and the projector trigger all key on it.
