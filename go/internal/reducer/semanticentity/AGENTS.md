# AGENTS.md — internal/reducer/semanticentity

Scoped instructions for this package. Read them before editing anything here.
The root `AGENTS.md` and `CLAUDE.md` still apply; these add to them.

## The import rule is the one that matters

Imports point strictly downward:

    reducer root  ->  family packages  ->  shared-core tiers  ->  contract

This package is a family. It may import `reducer/contract` (aliased
`reducercontract`), `reducer/factload`, `reducer/gpphase`,
`reducer/payloadcore`, `internal/facts` and `pkg/log`. It must **never**
import the parent `internal/reducer` package, directly or transitively.

If you find yourself needing a symbol the reducer root defines, that is a
signal about where the symbol belongs, not a reason to reach upward:

- a generic helper (a payload accessor, a path qualifier) already forwards to
  a shared-tier package from root — call the shared-tier function
  (`payloadcore.X`) directly instead of the root forwarder;
- `Intent`, `Result`, `FactLoader`, `PriorGenerationCheck`, and the
  `GraphProjectionPhase*` keyspace/phase/key/state/publisher vocabulary are
  all root type aliases to `reducer/contract`, `reducer/factload`, and
  `reducer/gpphase` — import those packages directly, never the root alias;
- a symbol the root genuinely owns as production logic shared with families
  that have not moved out of root yet (the repair queue and its conversion
  function) gets a structurally identical local declaration in
  `graph_ports.go`, following the pattern `codetaint/graph_ports.go`
  established (issue #6061). Copy a function body byte-for-byte if you must
  copy at all — never re-derive it from memory. Unlike codetaint's ports,
  `GraphProjectionPhaseRepairQueue.Enqueue` takes a named struct parameter,
  so Go's exact-type-identity rule means the root's concrete queue does not
  satisfy this package's interface for free the way it would with a
  primitive-only method set; the root bridges the two named types with
  `semanticEntityRepairQueueAdapter`
  (`internal/reducer/semantic_entity_repair_queue_adapter.go`).

Most apparent blockers here are of the first or second kind wearing a domain
filename. Read the declaration before deciding: a body of
`return payloadcore.SemanticPayloadString(payload, key)` is a forwarder and
costs nothing to bypass by calling `payloadcore.SemanticPayloadString`
directly.

**Prefixes lie.** `payloadMap`, `semanticPayloadString`,
`semanticPayloadStringSlice`, `semanticQualifyDeltaPath`,
`semanticDeltaPayloadBool`, `deltaScopeRepositorySet`, and
`applyRepoRefreshDeltaScope` used to live in the same root files this family
moved out of, but they are cross-family forwarders other root domains
(`code_call`, `codeowners_ownership`, `documentation_edge`, `rationale`,
`submodule_pin`, `sql_relationship`, `supply_chain_impact`, `shell_exec`)
still call unqualified. They moved to
`internal/reducer/shared_payload_delta_compat.go` instead of here. Do not add
a local copy under those names — call the shared-tier function they forward
to.

## The invariants a change here can silently break

- **Delta scoping is per repository.** Never gate the file-scoped retract on
  a scope-wide reading; `extractSemanticDeltaProjectionScope` reads each
  generation's repository fact for `delta_generation`,
  `delta_relative_paths`, and `delta_deleted_relative_paths` per repo.
- **`GraphProjectionPhaseRepairQueue` here is intentionally narrower than the
  root's.** It declares only `Enqueue`. Do not widen it to mirror the root's
  full interface unless this package actually starts calling
  `ListDue`/`Delete`/`MarkFailed` — a wider local interface would force every
  caller's concrete queue to satisfy methods this package never uses, for no
  benefit. That said, the narrowing does not make the root's concrete queue
  satisfy this interface directly: `Enqueue` takes a named
  `GraphProjectionPhaseRepair` struct, and semanticentity's is a distinct
  type from the root's, so Go's exact-type-identity rule for method
  signatures still requires the `semanticEntityRepairQueueAdapter` bridge in
  root. Only a plain, primitive-only method (like `codetaint.GraphQueryRunner.Run`)
  gets free structural satisfaction from a root concrete type.
- **The extraction sort order in `ExtractSemanticEntityRowsForRepo` is a
  contract.** Rows sort by `(RepoID, FilePath, EntityType, StartLine,
  EntityID)` so the canonical writer gets a deterministic write order;
  changing the sort changes the write order the graph-write and delta-retract
  paths depend on.
- **`isSemanticEntityType` is a closed, per-language allowlist.** `Variable`
  and `Function` only qualify under specific per-language metadata checks in
  `materialization_helpers.go` (e.g. a Go `Function` needs
  `hasSemanticFunctionMetadata`, a Python `Function` needs a lambda/generator/
  async/decorator/type-annotation signal). Adding a language or entity type
  here changes what gets written to the graph; it is a product decision, not
  a mechanical edit.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md` and `AGENTS.md`. It checks only that the three files EXIST, so
  it passing says nothing about whether their contents are still true.
  Re-read them yourself after changing the exported surface or the
  telemetry.
- **`verify-telemetry-coverage.sh`** — any new file under the reducer tree
  needs a row in `docs/public/observability/telemetry-coverage.md`. This
  package registers no instrument, so its rows use a
  `No-Observability-Change:` marker naming the signals that already cover
  the stage. Do not invent a metric that is absent from
  `go/internal/telemetry/instruments.go`.
- **`verify-performance-evidence.sh`** — fires on this path. It needs
  `No-Regression Evidence:` and `No-Observability-Change:` markers, unbolded
  and at the start of their line, on an added line in a tracked note.
  `README.md` here carries them; keep them unbolded and line-initial or the
  gate stops seeing them.
- **`verify-dirgate.sh`** — this directory counts against the non-test-file
  cap, and the `internal/reducer` row in `scripts/lib/dirgate-grandfather.tsv`
  is a monotonic ratchet. If you move files, re-derive the row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the mirror
  with `generate-dirgate-grandfather-go.sh`. Never hand-edit either, and
  never grandfather a count upward.

## Do not

- Do not name a new root file after this directory. `dirgate` refuses a root
  file whose stem is exactly `semanticentity` or starts with
  `semanticentity_`, so no `semanticentity_*.go` may exist in the reducer
  root. `semantic_entity_*.go` (with the underscore between the two words)
  does not collide, but do not create new root files under that name for
  this family's own logic either — extend this package instead.
- Do not suppress `dirgate` with `//nolint`.
- Do not widen `GraphProjectionPhaseRepairQueue` to the root's full
  interface without a new caller in this package that needs the extra
  methods.
