# internal/reducer/cicdrun

## Purpose

Materializes the `ci_cd_run_correlation` reducer domain (issue #6061): joins
provider CI/CD run facts with reducer-owned container-image identity
evidence and publishes one durable `CICDRunCorrelationDecision` per provider
run — `exact` when an artifact digest or workflow-image ref resolves to
exactly one container-image identity row, `derived` when the run has bounded
provider evidence but no artifact identity anchor, `ambiguous` when a digest
or ref matches more than one identity row, `unresolved` when the run lacks a
repository/commit anchor, and `rejected` when the only deployment hint is
unsafe (shell text with no artifact anchor).

## Ownership boundary

**Owns:** `CICDRunCorrelationHandler` (the reducer intent handler), its
writer port and Postgres implementation, the decision/outcome types, the
typed decode of the seven `ci.*` fact kinds, the artifact-only
cross-generation patch rebuild (`ci_cd_run_correlation_patch*.go`), the
workflow-image and deployment-event evidence bridges, and the exact
workflow-image `BUILT_FROM` provenance-edge projection.

**Does not own:** the container-image identity domain itself
(`ContainerImageIdentityHandler` and its writer, still in the reducer root)
— this package only reads its published `reducer_container_image_identity`
facts across scopes, behind the `crossscope` readiness floor. The
`ContainerImageProvenanceEdgeWriter` port itself is shared, not
root-owned: its canonical definition now lives in
`internal/reducer/contract/provenance_edges.go`, with the reducer root
keeping only a type-alias forwarder (`container_image_provenance_edges.go`)
for backward compatibility — see Dependencies below. Also does not own the
`supply_chain_impact` domain that reads this package's canonical
`reducer_ci_cd_run_correlation` facts, or the cross-scope
producer-readiness floor and dependency catalog themselves (`crossscope`,
shared with `supply_chain_impact`).

## Exported surface

| symbol | what it is |
|---|---|
| `CICDRunCorrelationHandler` | the reducer intent handler; `Handle` is the entry point |
| `CICDRunCorrelationWriter` / `PostgresCICDRunCorrelationWriter` | the writer port and its Postgres implementation |
| `CICDRunCorrelationWrite` / `CICDRunCorrelationWriteResult` | the durable-write request/result shapes |
| `CICDRunCorrelationDecision` / `CICDRunCorrelationOutcome` | the per-run decision and its five outcome values (`CICDRunCorrelationExact`/`Derived`/`Ambiguous`/`Unresolved`/`Rejected`) |
| `BuildCICDRunCorrelationDecisions()` | the error-free classification entry point every existing table-test caller uses |
| `CICDRunCorrelationFactKind` | the durable fact kind this writer publishes under (`reducer_ci_cd_run_correlation`), read by `supply_chain_impact` |
| `CICDWorkflowImageBuiltFromEvidenceSource` | the evidence-source tag for the workflow-image `BUILT_FROM` projection |
| `CICDRunKeyFromParts()` / `TrimmedCICDPtr()` | the run-key builder and optional-field trim helper `container_image_identity`'s CI loader reads across the seam |
| `(CICDRunCorrelationHandler).ProjectCICDWorkflowImageBuiltFromEdges()` | the workflow-image provenance-edge projection, exported because the reducer root's shared `provenance_edge_submission_metrics_test.go` exercises it directly |

The reducer root wires `DefaultHandlers.CICDRunCorrelationWriter`
(`defaults.go`) to this package's writer port and constructs the handler in
`defaults_additive_domains_correlation.go`. `cmd/reducer` constructs the
concrete `PostgresCICDRunCorrelationWriter` (`main.go`).

## Dependencies

`internal/reducer/contract` (the `Intent`/`Result`/`Domain` shapes plus the
`ContainerImageIdentityFactKind` and
`CICDRunCorrelationEnvironmentEvidenceDeployEvent`/`Declared` vocabulary,
aliased `reducercontract`), `internal/reducer/crossscope` (the #5709
cross-scope producer-readiness floor), `internal/reducer/factdecode`
(quarantine partitioning and telemetry recording),
`internal/reducer/factload` (the scoped fact loader),
`internal/reducer/factwrite` (batched fact-row inserts),
`internal/reducer/payloadcore` (deref/trim/convert helpers),
`internal/reducer/schemadecode` (the typed-payload decode seam for the seven
`ci.*` fact kinds), `internal/facts`, `internal/environment`,
`internal/telemetry`, `internal/truth`, and the generated
`sdk/go/factschema/cicdrun/v1` package. No dependency on the reducer root,
and none of the root's other family subpackages.

One root-owned interface this package's handler needs
(`ContainerImageProvenanceEdgeWriter`, the `BUILT_FROM` edge writer port
shared with `container_image_identity`) moved to
`internal/reducer/contract/provenance_edges.go` in the same change (issue
#6061): it is a genuine two-family shared contract, not owned by either
family alone, so it was hoisted to `contract` — the same reasoning
`crossscope`'s own README documents for the readiness floor — rather than
re-declared locally. The reducer root keeps
`ContainerImageProvenanceEdgeWriter` as a type alias to
`reducercontract.ContainerImageProvenanceEdgeWriter`
(`container_image_provenance_edges.go`). The
`CICDRunCorrelationEnvironmentEvidenceDeployEvent`/`Declared` vocabulary
(`contract/ci_cd_run_correlation_environment_evidence.go`) moved the same
way: this package publishes it on `CICDRunCorrelationDecision.EnvironmentEvidence`,
and `supply_chain_impact` reads it back off the published fact payload.

## Telemetry

`eshu_dp_ci_cd_run_correlations_total{domain, outcome}` (one increment per
outcome via `Instruments.CICDRunCorrelations`),
`eshu_dp_cicd_deployment_events_skipped_total{domain, skip_reason}`
(`Instruments.CICDDeploymentEventsSkipped`, repository-mismatch skips),
`eshu_dp_provenance_edges_total{evidence_source domain, outcome}`
(`Instruments.ProvenanceEdges`, the workflow-image `BUILT_FROM` submission
counter, domain-tagged with `CICDWorkflowImageBuiltFromEvidenceSource`),
`eshu_dp_reducer_input_invalid_facts_total{domain, fact_kind}` (a malformed
required field quarantined through `factdecode.RecordQuarantinedFacts`),
`Result.SubSignals["input_invalid_facts"]`, and the standard
`eshu_dp_reducer_executions_total` / `eshu_dp_reducer_run_duration_seconds` /
`eshu_dp_postgres_query_duration_seconds` for handler and writer execution.
A cross-scope deferral additionally emits one structured log line via
`crossscope.LogProducerNotReadyDefer` (owned by that package, unchanged by
this move). All of the above are unchanged by this move: same metric names,
same emission sites, only the package that owns the code moved.

## Root-side test doubles this package's move required

Go test files cannot share unexported symbols across packages, and several
reducer-root test suites still construct `ci.run`/`ci.artifact`/
`reducer_container_image_identity` fixtures, or drive
`CICDRunCorrelationHandler.Handle` end to end, the way this package's own
tests do. Rather than export production API for test-only shapes, the root
keeps its own hand-kept-in-sync copies:

- `go/internal/reducer/container_image_identity_ci_fixtures_test.go`:
  `ciRunFact`, `ciArtifactFact`, `containerImageIdentityFact`,
  `stringSliceContains` — mirror this package's identically-named builders in
  `ci_cd_run_correlation_test.go`.
- `go/internal/reducer/cross_scope_readiness_floor_handler_test.go`:
  `testCICDDigest`, `stubCICDRunCorrelationFactLoader`,
  `recordingCICDRunCorrelationWriter`, `cicdDecisionsByRun` — mirror this
  package's equivalents (also in `ci_cd_run_correlation_test.go`), sized to
  what that file's #5709 floor-wiring proof needs.
- `go/internal/reducer/container_image_provenance_edges_test.go`'s
  `recordingContainerImageProvenanceEdgeWriter` has a mirror,
  `ci_cd_run_correlation_workflow_image_edges_test.go`'s copy of the same
  name, inside THIS package — the direction is symmetric: each side keeps
  its own copy of the trivial fixture rather than one importing the other's
  test package (which Go forbids for `_test.go` files regardless of
  direction).

If you change a fixture's shape here (a builder's fields, a stub's method
set), update the root copy in the same commit — nothing enforces they stay
identical.

## Gotchas / invariants

- **The cross-scope readiness signal is sampled BEFORE the cross-scope load,
  never after.** `Handle` calls `crossscope.CheckProducerReadinessBeforeLoad`
  with `readinessSampledAt` taken before `loadActiveCICDRunCorrelationFacts`
  runs; sampling after would let a producer generation activate in the
  window and the handler would durably write an empty correlation nothing
  later repairs. See `crossscope`'s own README for the full ordering
  argument this package's `Handle` implements.
- **`ProjectCICDWorkflowImageBuiltFromEdges` and
  `CICDWorkflowImageBuiltFromEvidenceSource` are exported only because the
  reducer root's shared `provenance_edge_submission_metrics_test.go`
  exercises them directly** (it also exercises the unrelated
  `PackageSourceCorrelationHandler` and `ContainerImageIdentityHandler`
  provenance-edge counters in one file and could not move here). Treat them
  as internal to this package's own production callers; the export exists
  for that one shared test file, not as a public API invitation.
- **`CICDRunKeyFromParts`/`TrimmedCICDPtr` are exported for the same
  reason**, read across the seam by
  `container_image_identity_ci_loader.go` and
  `container_image_identity_typed_evidence.go` in the reducer root (via the
  `cicdRunKeyFromParts`/`trimmedCICDPtr` forwarders in
  `ci_cd_run_correlation_compat.go`).
- **The batch-wide cross-scope resolved count disarms the floor for every
  run in the same `Handle` pass**, not per-run — a documented residual gap
  (`TestCICDRunCorrelationDoesNotDeferABatchWhereAnotherRunResolved`,
  `docs/internal/evidence/5709-cross-scope-readiness-floor.md`). Do not
  "fix" this locally without reading that evidence doc first; closing it
  needs a different, per-digest contract than #5709 specifies.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/crossscope/README.md` — the shared cross-scope readiness floor this package's `Handle` calls
- `docs/internal/design/package-restructure.md` — the #6061 restructure
- `docs/internal/evidence/5709-cross-scope-readiness-floor.md` — the residual-window disclosure for the batch-wide resolved-count gap above

No-Regression Evidence: #6061 moves the `ci_cd_run_correlation` reducer
domain (10 non-test + 20 test files) out of the reducer root into this new
package, cut-paste with call sites repointed to the leaf packages they
already forwarded to (`crossscope`, `factdecode`, `factload`, `factwrite`,
`payloadcore`, `schemadecode`) rather than through the root's compatibility
forwarders. `ContainerImageProvenanceEdgeWriter` moved from the reducer root
to `internal/reducer/contract/provenance_edges.go` (a genuine two-family
shared contract, not owned by `container_image_identity` alone), and the
`CICDRunCorrelationEnvironmentEvidenceDeployEvent`/`Declared` vocabulary
moved the same way into
`contract/ci_cd_run_correlation_environment_evidence.go`; both keep a root
type/const alias so every existing root caller (`container_image_provenance_edges.go`,
`supply_chain_impact_environment_evidence.go`, and their ~20 combined test
callers) is unchanged. Three previously-unexported symbols
(`cicdRunKeyFromParts`, `trimmedCICDPtr`, `cicdRunCorrelationFactKind`) and
one method (`projectCICDWorkflowImageBuiltFromEdges`) were exported because a
root caller reads them across the seam; the root keeps forwarders for the
first three (`ci_cd_run_correlation_compat.go`) and the fourth's six call
sites in `provenance_edge_submission_metrics_test.go` were repointed to the
capitalized name. `CICDRunCorrelationHandler`/`Writer`/`Decision`/`Write`/
`WriteResult`/`Outcome` (plus its five outcome values) and
`PostgresCICDRunCorrelationWriter` keep root type/const aliases in
`ci_cd_run_correlation_compat.go`, so `cmd/reducer/main.go` and the ~10
`internal/storage/postgres` live-test files that construct them as
`reducer.CICDRunCorrelationHandler{...}` are unchanged. Several trivial
test-only fixtures (`ciRunFact`, `ciArtifactFact`, `containerImageIdentityFact`,
`stringSliceContains`, `strPtr`, `stubCICDRunCorrelationFactLoader`,
`recordingCICDRunCorrelationWriter`, `cicdDecisionsByRun`,
`recordingContainerImageProvenanceEdgeWriter`) could not be shared across the
package boundary (Go forbids importing another package's `_test.go` files);
each side kept its own copy — see Root-side test doubles above. The
batched-insert fake `Execer` and decoder (`fakeWorkloadIdentityExecer`,
`decodeBatchedFactCalls`, used by ~30 reducer-root writer test files) moved
to a new exported, non-`_test.go` support package,
`internal/reducer/factwrite/factwritetest`, rather than being duplicated,
because that fixture is substantial (241 lines, decodes 16 positional
SQL-array arguments) and already shared across many unrelated families; the
root's own copy in `reducer_fact_batch_insert_test_helpers_test.go` was left
untouched since every existing root caller still resolves against it
unqualified. Measured from `go/`, with `GOROOT` unset and `GOCACHE` pointed
at this worktree: `go build ./...` exited 0; `go vet ./...` exited 0;
`go test ./internal/reducer/... -count=1` exited 0 across all reducer
subpackages including the new `cicdrun` and `factwrite/factwritetest`; `go
test ./cmd/reducer ./internal/storage/postgres ./internal/query -count=1`
exited 0, which proves the storage layer's `reducer.CICDRunCorrelationHandler`/
`PostgresCICDRunCorrelationWriter`/`Write`/`WriteResult` call sites still
resolve and behave the same; `go test
./internal/ifa/materializededges/... -count=1` and `go test
./internal/payloadusage/... ./cmd/payload-usage-manifest/... -count=1` each
exited 0. `gofumpt -l` on every touched/added file reported nothing to
format.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph,
or Postgres operation, runtime setting, metric instrument, or metric label.
Every metric name, emission site, and structured-log message listed under
Telemetry above is unchanged; only the package that owns the code moved. No
instrument name, metric label, span name, or structured-log field changed.
