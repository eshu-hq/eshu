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

The reducer root wires `Handler` in `defaults_domain_catalog.go`, naming
`materialization.Handler`/`materialization.IntentWriter`/
`materialization.ExtractIntentRows`/
`materialization.BuildRouteIntentRowsForQueryProof` directly (#6626
value-flow lane retired the code-call stanza of `compat_projection.go`).

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
`written_rows` diagnostic signals plus any `input_invalid_facts` count, and
`unresolved_caller_calls` (`SubSignalUnresolvedCallerCalls`). The completion log
carries the same number as `code_call_unresolved_caller_count`: calls whose
callee resolved but whose caller has no containing entity in the call file's
own repository and path (#7640), so they emitted no `CALLS` row. A drop in
`code_call_row_count` with a matching rise here is that containment rule
working, not lost input. `unresolved_callee_calls`
(`SubSignalUnresolvedCalleeCalls`, log field
`code_call_unresolved_callee_count`) counts the mirror image: calls dropped
because no callee resolved through any same-file, repository,
language-specific, or import-binding branch (#7642). A drop in
`code_call_row_count` with a matching rise here is the same-file scoping
rule working — those calls previously bound a foreign file's declaration —
not lost input. `unresolved_cloud_action_callers`
(`SubSignalUnresolvedCloudActionCallers`, log field
`code_call_unresolved_cloud_action_caller_count`) is the same count for SDK
calls that would have emitted an `INVOKES_CLOUD_ACTION` intent.

Unusable delta scopes (#10023): when a delta-generation repository that has a
projection context has no usable file scope (`codecall.FileScopeBuildResult`
`UnusableDeltaScopes`), `Handle` returns a non-retryable error with failure
class `DeltaScopeUnusableFailureClass` (`code_call_delta_scope_unusable`)
before the symbol load and before any intent is written. It records
`eshu_dp_code_call_delta_scope_unusable_total{reason}` once per such
repository and logs one ERROR line, `code call delta scope unusable, failing
closed`, with `unusable_delta_scopes` (`repo=reason`, at most 16 named) and
`unusable_delta_scope_repo_count`. The reducer queue dead-letters the work item
on its first attempt; the dead-letter details carry `triage=input_invalid`
because the error wraps a `projector/failure` `InputValidationError`. The
counter counts the delta repositories the fence turns away; since #10049 the
git collector emits the `code_call_materialization` marker on delta
generations, so every delta work item passes the fence. The operator guide is
[Shared-Write Operations](../../../../../../docs/public/reference/telemetry/shared-write-operations.md#code-call-unusable-delta-scopes).

## Gotchas / invariants

- **A delta repository with an unusable scope fails the work item closed; it
  never falls back to the whole-repository refresh.** A delta generation
  carries the changed files' facts only, so a whole-repository CALLS retract
  deletes every unchanged file's edge and the delta's rows restore only the
  changed files'. The failure is raised here, at the work-item layer, because
  the code-call projection lane has no terminal state for a poisoned shared
  intent: it retries the row every cycle and a stuck whole-scope refresh
  blocks its partition. Existing edges stay until the repository's next full
  generation (the reconciliation sweep, default 24 hours, unless
  `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0` turns it off); no repair job is
  needed. The fence runs before the cross-repository symbol load, so a fenced
  work item costs no symbol query.
- **The three symbol→runtime domains are repo-wide-retract domains with
  per-edge partition keys.** Every call to `BuildIntentRows` MUST pair each
  domain's per-edge rows with its repo-wide refresh row in the SAME pass —
  see `buildRepoWideRetractRefreshIntents`'s doc comment for why splitting
  them across passes silently loses edges (#2898/#2910).
- **The runtime pass never sees delta-generation repositories (#10049).**
  The handler feeds `buildIntentRowsCounted` through
  `runtimeDomainEnvelopes`, which drops every delta repository's facts: the
  three domains have no file-scoped retract, so a repo-wide refresh paired
  with changed-file rows would delete every unchanged file's runtime edges.
  A delta repository's runtime edges refresh at its next full generation,
  as they did before the marker fired on deltas. The CALLS side of the same
  pass is file-scoped and does run on delta input.
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
