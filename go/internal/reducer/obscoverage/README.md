# obscoverage

Correlates AWS-native observability objects (CloudWatch alarms/dashboards,
X-Ray sampling rules) and Grafana-stack declared/applied/observed metadata
against the monitored CloudResource nodes they cover, and publishes those
decisions as durable reducer facts plus, for the exact-match subset, canonical
COVERS graph edges.

This package moved out of the flat `internal/reducer` root under issue #6061.
It is a domain family: it owns two handlers over one shared classifier
pipeline, and nothing else in the reducer depends on its internals.

## What it owns

| piece | file | what it does |
|---|---|---|
| `ObservabilityCoverageCorrelationHandler`, `BuildObservabilityCoverageDecisions` | `observability_coverage_correlation.go` | the reducer handler the runtime registers for provenance-only coverage decisions |
| `classifyObservabilityCoverage` | `observability_coverage_correlation_classify.go` | turns the bounded coverage index into one decision per candidate plus one gap finding per uncovered target |
| coverage index, AWS-native join | `observability_coverage_correlation_index.go` | resolves an alarm/dashboard/X-Ray rule's monitored identity to a CloudResource uid by ARN, bare id, or correlation anchor |
| `PostgresObservabilityCoverageCorrelationWriter` | `observability_coverage_correlation_writer.go` | persists correlation decisions as durable reducer facts |
| `ExtractObservabilityCoverageEdgeRows` | `observability_coverage_edge_rows.go` | re-runs the classifier and extracts only the exact-coverage decisions into a bounded COVERS edge batch |
| `ObservabilityCoverageMaterializationHandler`, `ObservabilityCoverageEdgeWriter` | `observability_coverage_materialization.go` | projects the edge batch into canonical COVERS graph edges, gated on canonical-nodes readiness |
| `classifyObservabilityMetadataEvidence` | `observability_coverage_metadata.go` | groups Grafana-stack declared/applied/observed facts and classifies each group into a coverage decision |
| `decodeObservabilityMetadataView` | `observability_coverage_metadata_decode.go` | the typed-contracts decode seam dispatching on fact kind (folder, dashboard, datasource, alert rule, scrape config, and 12 more) |
| `observabilityMetadataView` | `observability_coverage_metadata_view.go` | the flat, typed read view the metadata classifier reads instead of raw payload map lookups |
| observed-fact view accessors | `observability_coverage_metadata_view_observed.go` | the observed-layer subset of `observabilityMetadataView` (dashboard/target/rule/log/trace signal facts) |

A coverage decision measures whether a CloudResource is watched, not whether
what it is watched by is healthy: no metric value, alert state, or dashboard
body is ever read as graph truth, only the identity link between the
observability object and its target. The correlation handler is
provenance-only for every outcome (`ProvenanceOnly: true` even for exact
matches); the materialization handler is the only writer of canonical graph
truth, and only for the subset of exact decisions that resolved a target
CloudResource uid.

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/factdecode`, `reducer/factload`, `reducer/factwrite`, `reducer/gpphase`,
`reducer/payloadcore`, `reducer/schemadecode`, `internal/facts`,
`internal/telemetry`, and `internal/truth`, and it never imports the parent
`internal/reducer` package. The dependency runs the other way: the root keeps
compatibility aliases in `observability_coverage_compat.go` so its own callers
compile unchanged.

Three symbols moved to a shared tier rather than staying root-private, because
they have consumers on both sides of the boundary:

- `ProjectedSourceLedger` (the source-uid ledger interface AWS/Azure/GCP/
  security-group edge writers also implement against) and
  `sourceUIDsFromRowsByKey` (the generic row-key extraction helper those same
  writers share) moved to `reducer/contract` and `reducer/payloadcore`
  respectively.
- `joinModeARN` / `joinModeBareID` / `joinModeCorrelationAnchor` (the AWS
  relationship edge join_mode enum this package's resolution_mode reuses
  verbatim) moved to `reducer/contract`.
- `cloudResourceUID` (the stable CloudResource node identity every
  cloud-provider materialization family computes) moved to
  `reducer/payloadcore`.
- The canonical single-row fact-insert SQL (`canonicalReducerFactInsertQuery`)
  moved to `reducer/factwrite` as `SingleInsertQuery`, alongside the existing
  batch-insert query fragments.
- `gpphase.KeyFromScope` is new: this package only reads readiness (calls
  `ReadinessLookup` with a key), never publishes it, so it needs the identity
  the root's `graphProjectionPhaseStateForIntent` constructs without the
  `PhaseState`/publisher machinery that deliberately stays at root (see
  `gpphase`'s own doc.go). The root helper now calls the same function, so the
  two call sites cannot drift.

## Telemetry

`ObservabilityCoverageCorrelationHandler.Handle` emits
`eshu_dp_observability_coverage_correlations_total` (labeled by domain,
outcome, and coverage_signal) once per non-empty outcome after classifying a
batch.

`ObservabilityCoverageMaterializationHandler.Handle` wraps its work in the
`reducer.observability_coverage_materialization` span and emits
`eshu_dp_observability_coverage_edges_total` (labeled by coverage_signal and
resolution_mode) for each materialized exact-coverage edge. Provenance-only
coverage (derived/ambiguous/unresolved/stale/rejected) never produces an edge
and is surfaced by the `ObservabilityCoverageCorrelations` counter and the
completion log instead — an operator diagnosing "coverage looks low" should
read for outcome mix on the correlations counter before the edges counter,
since most non-exact coverage is real but intentionally not a graph edge.

Documents rejected for a malformed payload increment the shared
`eshu_dp_reducer_input_invalid_facts_total` counter instead. The reducer
executions that run either handler remain covered by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Almost every hunk inside the moved production files is
package-clause and import requalification: symbols the reducer root used to
supply as one-line forwarders are now imported from the leaf that already
owned them (`payloadcore` for the payload/slice/identity helpers, `contract`
for the intent/result/domain/join-mode vocabulary, `factload`/`factdecode`/
`factwrite`/`schemadecode` for the rest, `gpphase` for the readiness-key
construction newly extracted from the root's own
`graphProjectionPhaseStateForIntent`, which now calls the identical extracted
function so both call sites share one implementation). Moved test files
additionally gained local copies of a handful of fixture-builder helpers and
fake test doubles (`stubFactLoader`, `fakeProjectedSourceLedger`,
`statefulProjectedSourceLedger`, `fakeWorkloadIdentityExecer`, `readyLookup`)
that other reducer-root test files also use, because Go test files cannot
share unexported symbols across a package boundary. Measured against baseline
`origin/main`: `go build ./...` and `go vet ./...` both exit 0 on the branch,
and `go test ./internal/reducer/... -count=1` passes, including this package
and the root package whose wiring tests gained matching minimal writer
doubles. Binary output was not compared and no such claim is made here.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, or
log field. The two counters and the span this family emits, and the
executions that wrap them, are the same before and after the move.

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a
  symbol the root defines, the symbol is in the wrong place: hoist it to a
  shared-core tier (`payloadcore` for generic helpers, `contract` for
  vocabulary, `gpphase` for graph-projection-readiness identity) and leave a
  root alias, rather than reaching upward.
- **The correlation handler never writes a graph edge, even for exact
  matches.** `ObservabilityCoverageCorrelationDecision` is provenance-only
  for every outcome; only `ObservabilityCoverageMaterializationHandler` writes
  canonical COVERS edges, and only for the subset that resolved a target uid.
  Do not read the correlation counter as if it counted graph writes.
- **Tombstoned observability objects never prove coverage.** A deleted alarm
  (or dashboard, or rule) must not be classified as covering an otherwise-live
  target — see `TestBuildObservabilityCoverageDecisionsTombstonedObjectNeverCovers`.
- **X-Ray derived coverage resolves no target uid and is intentionally
  skipped, not dropped silently** — it is real coverage the read model knows
  about that the graph cannot reach yet, and is counted in the edge tally's
  `skipped` bucket, not folded into `materialized`.
- **The materialization handler gates on canonical-nodes readiness before
  every write.** A generation whose canonical CloudResource nodes have not
  yet committed returns a retryable "not ready" error rather than writing
  edges against a node set that does not exist yet.
- **`observabilityMetadataEvidenceFromEnvelope` silently skips a fact whose
  provider/signal/object-ref cannot be derived**, matching pre-move behavior
  byte-for-byte; only a genuine decode failure is quarantined or fatal.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Observability coverage design](../../../../docs/internal/design/391-observability-coverage-correlation.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
