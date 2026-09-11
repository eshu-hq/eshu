# internal/reducer/code/call/materialization

## Purpose

Reduces one parser relationship follow-up into durable shared-intent
emission for code-call, Python metaclass, and symbol→runtime
(`HANDLES_ROUTE`/`RUNS_IN`/`INVOKES_CLOUD_ACTION`) rows (issue #6061).

`Handler.Handle` loads `repository`/`file` facts, extracts code-call and
metaclass rows through the sibling `codecall` package, and builds the
symbol→runtime rows in-package through `BuildIntentRows`, which resolves
parser-owned framework route handlers, deployed-runtime bindings, and AWS SDK
call sites to exact, unambiguous Function entities via the shared
`shared.EntityIndex`.

`ExtractIntentRows` is the backend-free seam: it builds the entity index and
projection contexts and calls `BuildIntentRows` without needing a graph
backend, Postgres, or a clock, so callers outside the reducer tree can derive
symbol→runtime rows from real production logic.

## Ownership boundary

**Owns:** code-call/metaclass/symbol-runtime materialization (`Handler`),
and the three symbol→runtime intent builders (`buildHandlesRouteIntentRows`,
`buildRunsInIntentRows`, `buildInvokesCloudActionIntentRows`) plus their
shared repo-wide refresh pairing (`BuildIntentRows`).

**Does not own:** the code-call extraction/entity-index substrate
(`codecall`, `code/call/shared`), the shared-projection worker/runner
machinery that consumes the intents this package emits (reducer root), or
the AWS `CAN_PERFORM` catalog (`iamcan`) this package's cloud-action resolver
cross-checks against.

## Exported surface

| symbol | what it is |
|---|---|
| `Handler` / `IntentWriter` / `CanonicalNodeChecker` | the domain handler, its durable-intent write port, and a vestigial preflight-check port kept for older wiring |
| `BuildIntentRows` | the shared entry point emitting handles_route/runs_in/invokes_cloud_action per-edge and repo-wide-refresh rows for one materialization pass |
| `ExtractIntentRows` | the backend-free seam: envelopes -> symbol→runtime rows, with no graph/Postgres/clock dependency |
| `BuildRouteIntentRowsForQueryProof` | drives the real HANDLES_ROUTE pipeline over given envelopes, for query-side false-green-risk proofs |

The reducer root wires `Handler` in `defaults_domain_catalog.go`, keeping the
`reducer.CodeCallMaterializationHandler`/`reducer.CodeCallIntentWriter`/
`reducer.ExtractSymbolRuntimeIntentRows`/
`reducer.BuildHandlesRouteIntentRowsForQueryProof` spellings through the
code-call stanza of `compat_projection.go`.

## Dependencies

`internal/facts` (`Envelope`), `internal/codeprovenance` (resolution method
provenance), `internal/reducer/code/call` (`codecall`: extraction, file
scopes, shared-intent row builders, evidence sources),
`internal/reducer/code/call/shared` (`EntityIndex`, `PathKeys`, `PayloadInt`,
`ResolveContainingEntityID`, `EndpointEntityType`, `BuildEntityIndex`),
`internal/reducer/contract` (`Intent`, `Result`, `Domain*`,
`MaterializationDiagnosticSignals`), `internal/reducer/factload`
(`FactLoader`, `LoadFactsForKinds`, `FactKind*`, `ClassifyFactLoadError`),
`internal/reducer/factdecode` (`RecordQuarantinedFacts`,
`InputInvalidSubSignals`), `internal/reducer/schemadecode`
(`BuildProjectionContexts`), `internal/reducer/sharedintent` (`Row`, `Input`,
`Build`, `EdgeWriter`, `ProjectionContext`, the repo-refresh/retract-fence
vocabulary), `internal/reducer/payloadcore` (payload accessors), and
`internal/reducer/iamcan` (`CatalogByAction`, `Action`). No dependency on the
reducer root.

## Telemetry

`Instruments` records the input_invalid quarantine counter for a "file" fact
whose outer envelope fails the codegraph decode seam. `Handle` emits one
structured log, "code call materialization completed", with `scope_id`,
`generation_id`, `domain`, fact/row/repo counts, and a per-phase duration
breakdown. `Result.SubDurations` carries the same phase timings as
`sub_duration_<key>_seconds`; `Result.SubSignals` carries `input_ready`/
`written_rows` diagnostic signals plus any `input_invalid_facts` count.

## Gotchas / invariants

- **The three symbol→runtime domains are repo-wide-retract domains with
  per-edge partition keys.** Every call to `BuildIntentRows` MUST pair each
  domain's per-edge rows with its repo-wide refresh row in the SAME pass —
  see `buildRepoWideRetractRefreshIntents`'s doc comment for why splitting
  them across passes silently loses edges (#2898/#2910).
- **The cloud action lives under payload key `"cloud_action"`, never
  `"action"`.** `sharedintent`'s upsert filter reads `payload["action"]` as
  the upsert/refresh/delete discriminator; storing the resolved AWS action
  there would silently drop every INVOKES_CLOUD_ACTION upsert.
- **`cloudActionByServiceMethod` only ever maps to actions in the closed
  `iamcan` `CAN_PERFORM` catalog.** `resolveCloudAction` re-checks the
  catalog even though the mapping table should already be closed, as a
  defense-in-depth correlation-truth guard.
- **`runsInAmbiguousConfidence` (`0.5`) marks every RUNS_IN edge as a
  candidate, never an asserted single-workload binding** — this stage never
  runs workload admission/correlation, so it cannot prove exactness.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/code/call/README.md` — the `codecall` sibling this package extracts through
- `go/internal/reducer/code/README.md` — the `code/` namespace parent
- `docs/internal/design/reducer-target-tree.md` — the #6061 restructure

No-Regression Evidence: #6061 moves code-call/metaclass/symbol-runtime
materialization out of the reducer root into this new package, without
changing any field, exported behavior, wire string, or call order.
`CodeCallMaterializationHandler`/`CodeCallIntentWriter` dropped the
`CodeCall` stutter per `docs/internal/naming.md`
(`CodeCallMaterializationHandler` -> `Handler`, `CodeCallIntentWriter` ->
`IntentWriter`); `ExtractSymbolRuntimeIntentRows` ->
`ExtractIntentRows`, `buildSymbolRuntimeIntentRows` -> `BuildIntentRows`
(exported per the owner-approved naming decision), and
`BuildHandlesRouteIntentRowsForQueryProof` -> `BuildRouteIntentRowsForQueryProof`.
The reducer root keeps every spelling with an external caller through the
code-call stanza of `compat_projection.go`, so no external caller needed a
source change. Measured from `go/`, with `GOROOT` unset: `go build ./...`,
`go vet ./internal/reducer/... ./cmd/reducer/...`, and `go test
./internal/reducer/... ./cmd/reducer/... ./internal/storage/postgres/...
./internal/replay/... -count=1` all exited 0 on this branch. `git diff
--check` exited 0.

No-Observability-Change: this move adds no queue domain, worker, lease,
graph, or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. The structured-log message and its fields
listed under Telemetry above are unchanged; only the package that owns the
code moved.
