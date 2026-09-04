# internal/reducer/kubernetescorrelation

## Purpose

Materializes the `kubernetes_correlation` and
`kubernetes_correlation_materialization` reducer domains (issue #388, moved
out of the reducer root in issue #6061): correlating live Kubernetes
workload evidence (`kubernetes_live.*` facts) against deployment-source
image evidence into a six-outcome, provenance-aware read model, and
projecting the exact subset of that read model into canonical `RUNS_IMAGE`
graph edges between a `KubernetesWorkload` node and the digest-addressed OCI
source node it was observed running.

`KubernetesCorrelationHandler` loads one scope generation's
`kubernetes_live.*` facts plus the cross-scope active deployment-source
image facts (through the optional `ListActiveContainerImageIdentityFacts`
extension on its `FactLoader`), classifies each live image reference and
workload identity edge into one of six outcomes (`exact` / `derived` /
`ambiguous` / `unresolved` / `stale` / `rejected`) plus a drift kind, and
writes durable provenance-only reducer facts. It writes no graph edges
itself — PR1 is fact-only.

`KubernetesCorrelationMaterializationHandler` (PR3) re-runs the same pure
classifier and promotes to a graph edge ONLY the exact image decisions that
resolve both a live `KubernetesWorkload` node uid (the collector-emitted
`object_id`) and a digest-addressed OCI source node uid (via
`SourceImageDigestJoinIndex`). Everything else — derived / ambiguous /
unresolved / stale / rejected outcomes, and the structural `owner_reference`
identity decision — stays provenance-only and fabricates no edge; an exact
decision whose source digest resolves no canonical node is counted skipped,
never written as a dangling edge.

## Ownership boundary

**Owns:** `KubernetesCorrelationHandler` and
`KubernetesCorrelationMaterializationHandler` (the two reducer intent
handlers), their `Writer`/`EdgeWriter` ports, the six-outcome classifier and
its bounded in-memory index, the RUNS_IMAGE edge row extraction, the
`PostgresKubernetesCorrelationWriter` fact writer, and the
`SourceImageDigestJoinIndex` digest→node-uid resolver the edge slice anchors
its source endpoint on.

**Does not own:** the `kubernetes_workload_materialization` and
`kubernetes_namespace_materialization` node slices (reducer root) — a
different family (#388's pod-template/namespace node projection) that this
package's edge handler depends on for readiness (it gates on the
canonical-nodes-committed phase that family publishes) but never imports.
Also does not own `containerimage` (the image-reference parser this package
reuses) or `payloadcore`/`factload`/`factdecode`/`factwrite`/`gpphase` (the
shared reducer-family primitives every moved family reuses).

## Exported surface

| symbol | what it is |
|---|---|
| `KubernetesCorrelationHandler` | the PR1 fact-only correlation intent handler |
| `KubernetesCorrelationMaterializationHandler` | the PR3 RUNS_IMAGE edge-projection intent handler |
| `KubernetesCorrelationWriter` / `KubernetesCorrelationEdgeWriter` | the fact and edge writer ports |
| `PostgresKubernetesCorrelationWriter` | the Postgres-backed fact writer `cmd/reducer` constructs |
| `KubernetesCorrelationOutcome` / `KubernetesCorrelationExact` / `Derived` / `Ambiguous` / `Unresolved` / `Stale` / `Rejected` | the six-outcome closed vocabulary |
| `KubernetesCorrelationDecision` / `KubernetesCorrelationWrite` / `KubernetesCorrelationWriteResult` | the decision row and durable-publication shapes |
| `BuildKubernetesCorrelationDecisions` | the pure, table-test-friendly classifier (no telemetry side effects) |
| `KubernetesCorrelationMaterializationDomainDefinition()` | the `DomainDefinition` constructor for root's additive-domain registration |
| `KubernetesCorrelationNodesNotReadyFailureClass` | the retryable failure class for a readiness-gate miss |
| `ExtractKubernetesCorrelationEdgeRows` | builds canonical RUNS_IMAGE edge rows from one scope generation's facts |
| `SourceImageDigestJoinIndex` / `SourceImageNode` / `BuildSourceImageDigestJoinIndex` | the bounded digest -> canonical-node-uid resolver the edge slice anchors on |

The reducer root wires `KubernetesHandlers.KubernetesCorrelationWriter` /
`KubernetesCorrelationEdgeWriter` (`defaults_handlers.go`) to this package's
interfaces and constructs the two handlers in
`defaults_additive_domains_supply_chain.go` /
`defaults_additive_domains_cloud_posture.go`. `cmd/reducer` constructs the
concrete `PostgresKubernetesCorrelationWriter` and the Cypher edge writer
(`wiring_handlers.go`, `canonical_graph_writers.go`).

## Dependencies

`internal/reducer/contract` (the `Intent`/`Result`/`Domain`/`DomainDefinition`
shapes, aliased `reducercontract`), `internal/reducer/factload` (fact
loading and load-error classification), `internal/reducer/factdecode`
(per-fact quarantine partitioning and telemetry recording),
`internal/reducer/factwrite` (the batch/single fact-insert primitives the
Postgres writer uses), `internal/reducer/payloadcore` (payload
deref/trim/convert/sort helpers), `internal/reducer/gpphase` (the
cross-family graph-projection readiness vocabulary — `KeyFromScope`,
`ReadinessLookup`, `Keyspace`/`Phase` constants), and
`internal/reducer/containerimage` (`ParseContainerImageRef`,
`ParsedContainerImageRef`, `NormalizeContainerRepositoryKey` — reused
directly rather than duplicating a second image-reference parser: see
Gotchas). `internal/facts` and the generated
`sdk/go/factschema/kuberneteslive/v1` package for the typed fact payloads.
No dependency on the reducer root, and none of the root's other family
subpackages.

## Telemetry

`eshu_dp_kubernetes_correlations_total` (labels: `domain`, `outcome`,
`drift_kind` — `KubernetesCorrelationHandler.emitCounters`),
`eshu_dp_kubernetes_correlation_edges_total` (label: `resolution_mode` —
`KubernetesCorrelationMaterializationHandler.recordEdgeCounter`),
`eshu_dp_reducer_input_invalid_facts_total{domain, fact_kind}` (a malformed
required field quarantined through `factdecode.RecordQuarantinedFacts`),
`Result.SubSignals["input_invalid_facts"]`, the
`reducer.kubernetes_correlation_materialization` trace span
(`telemetry.SpanReducerKubernetesCorrelationMaterialization`), a completion
structured log ("kubernetes correlation materialization completed") with
per-stage durations and the skipped-unresolvable-source count, and the
standard `eshu_dp_reducer_executions_total` /
`eshu_dp_reducer_run_duration_seconds` for handler execution. This package
registers no new instrument itself — both counters and the span constant are
pre-registered in `internal/telemetry/instruments.go` /
`internal/telemetry/contract.go` and reached through the `*telemetry.Instruments`
field the handlers are wired with. Unchanged by this move: same metric
names, same emission sites, only the package that owns the code moved.

## Gotchas / invariants

- **`KubernetesCorrelationMaterializationHandler.workloadNodesReady` calls
  `gpphase.KeyFromScope` directly, not the reducer root's
  `graphProjectionPhaseStateForIntent`.** This family only needs the
  readiness key to read a published phase, never to publish one itself, so
  it takes the escape `gpphase.KeyFromScope`'s own doc comment names for a
  family in this position, rather than importing the reducer root. Do not
  "fix" this by importing root — see Dependencies above.
- **Image-reference parsing goes through `containerimage`, not a second
  local parser.** The `container_image_identity` family (also moved,
  issue #6061) already owns `ParseContainerImageRef` /
  `ParsedContainerImageRef` / `NormalizeContainerRepositoryKey`; this
  package imports that leaf package directly (a sibling-leaf import is
  allowed — only importing the reducer root is forbidden) rather than
  duplicating the parser. Do not hoist a second copy into this package or
  `payloadcore`.
- **`kubernetes_workload_source_image_join.go` has nothing to do with the
  `kubernetes_workload_materialization` family despite its filename.** It
  is the digest→node-uid join index `ExtractKubernetesCorrelationEdgeRows`
  needs to resolve its RUNS_IMAGE edge source endpoint; the
  `kubernetes_workload_materialization` family (root) never called it. The
  name predates this move and was kept as-is.
- **`ExtractKubernetesCorrelationEdgeRows` decodes through the
  quarantine-aware classifier, never the plain
  `BuildKubernetesCorrelationDecisions`.** A per-fact `input_invalid`
  quarantine must reach the materialization handler's counter, and a FATAL
  decode error must fail the whole intent BEFORE any prior-generation
  retract — discarding it would let a fatal decode yield empty decisions,
  then the handler would still retract prior edges and write nothing
  (silent edge loss).
- **An exact `owner_reference` identity decision never produces a RUNS_IMAGE
  edge.** It is a workload->workload structural edge (no `SourceDigest`),
  naturally excluded from `kubernetesDecisionIsImageEdgeEligible` — do not
  special-case it to route through the image-edge slice.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `docs/internal/design/package-restructure.md` — the #6061 restructure
- `docs/internal/design/388-kubernetes-correlation-readmodel.md` — the #388 design (six-outcome contract, PR1/PR3 split)
- `docs/public/observability/telemetry-coverage.md` — the coverage rows for these two domains

No-Regression Evidence: #6061 moves the kubernetes_correlation and
kubernetes_correlation_materialization intent handlers, their writer ports,
the six-outcome classifier and bounded index, RUNS_IMAGE edge extraction,
the Postgres fact writer, and the source-image digest join index out of the
reducer root into this new package, without changing any field, exported
behavior, or call order. `KubernetesCorrelationMaterializationDomainDefinition`
was exported (previously unexported `kubernetesCorrelationMaterializationDomainDefinition`)
because root's additive-domain registration now calls it across the package
boundary. The now-dead `parseContainerImageRef`/`parsedContainerImageRef`/
`normalizeContainerRepositoryKey`/`boolPayload` root compat forwarders
(whose only caller was this family) were deleted; this package imports
`containerimage`/`payloadcore` directly instead. Every outward caller —
`cmd/reducer` (`wiring_handlers.go`), `internal/reducer` root
(`defaults_handlers.go`, `defaults_additive_domains_supply_chain.go`,
`defaults_additive_domains_cloud_posture.go`,
`defaults_kubernetes_correlation_test.go`,
`kubernetes_correlation_readiness_seam_test.go`), and
`internal/storage/postgres` (via the root's
`KubernetesCorrelationNodesNotReadyFailureClass` const forwarder) — was
updated to the qualified `kubernetescorrelation.` symbol, or kept compiling
through that one forwarder, in the same commit. From `go/`, with `GOROOT`
unset and `GOCACHE` pointed at this worktree: `go build ./...`, `go vet
./...`, `go test ./internal/reducer/... -count=1` (kubernetescorrelation's
own suite included), and `go test ./cmd/reducer ./internal/storage/postgres
./internal/query -count=1` each exited 0 on the branch. `git diff --check`
exited 0. Binary output was not compared and no such claim is made here.

No-Observability-Change: this move adds no queue domain, worker, lease,
graph or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. The metric names, emission sites, and
structured-log message listed under Telemetry above are unchanged; only the
package that owns the code moved.
