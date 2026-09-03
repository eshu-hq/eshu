# CI/CD run-correlation projector intents

## Purpose

This package recognizes CI/CD run and artifact evidence in one scope
generation and builds the reducer intent that asks the reducer to correlate
that evidence into the `ci_cd_run_correlation` decision (#5710:
`CICDRunCorrelationHandler` had been registered and wired in
`cmd/reducer/main.go` since the domain was added, but no builder ever emitted
`Domain=ci_cd_run_correlation`, so the handler was unreachable in production
and `list_ci_cd_run_correlations` always returned zero outside unit tests).
The trigger fires on a `ci.run` fact, else a `ci.artifact` fact — a run
anchors a normal authoritative snapshot, and an artifact without a
co-located run starts the reducer's bounded historical-run patch (#5770): it
rebuilds the newest older run-window snapshot from source evidence, unions
exact current artifact routing keys, applies current-generation live and
tombstone control rows, and publishes the complete target generation without
depending on a prior derived result.

## Ownership boundary

The package owns only the trigger selection and the reducer-intent value. The
root `internal/projector` package validates scope-generation boundaries,
constructs and owns the immutable fact lookup, preserves family order, and
owns projection lifecycle, queue writes, retries, and telemetry. The
reducer's `DomainCICDRunCorrelation` handler
(`CICDRunCorrelationHandler`, `go/internal/reducer/cicdrun/ci_cd_run_correlation.go`)
owns the full-snapshot correlation, the bounded artifact-only patch, the
cross-scope read of `reducer_container_image_identity` rows
(`go/internal/reducer/crossscope/dependencies.go`'s `dependencyCatalog`), and
the durable decision write (exact/derived/ambiguous/unresolved/rejected);
none of that happens here.

## Exported surface

- `BuildCICDRunCorrelationReducerIntent` builds the `ci_cd_run_correlation`
  intent, anchored to the earliest `ci.run` fact in original input order,
  else the earliest `ci.artifact` fact.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to
it, so the reverse import would cycle. It reads `internal/facts` for the two
fact-kind constants and `internal/reducer` for the domain constant. There is
no decode seam: like `packagesource` and `cloudinventory`, this builder reads
only envelope fields and never a payload key.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains `eshu_dp_reducer_executions_total`,
`eshu_dp_reducer_run_duration_seconds`, `eshu_dp_ci_cd_run_correlations_total`,
and `eshu_dp_postgres_query_duration_seconds` for its historical Postgres
read. Moving the pure builder adds no queue, storage, graph, span, metric, or
log boundary.

## Gotchas / invariants

- A `ci.run` fact always outranks a `ci.artifact` fact, regardless of which
  appears earlier in the generation. The two kinds are looked up
  independently through `FirstOfKind`; there is no cross-kind
  original-order merge, unlike the families that anchor with
  `FirstAcrossKinds`.
- The trigger is deliberately narrower than the full set of fact kinds
  `CICDRunCorrelationHandler.Handle` loads for one intent
  (`cicdRunCorrelationFactKinds` in
  `go/internal/reducer/cicdrun/ci_cd_run_correlation.go`: `ci.run`,
  `ci.artifact`, `ci.workflow_image_evidence`, `ci.environment_observation`,
  `ci.trigger_edge`, `ci.step`) — only `ci.run` and `ci.artifact` trigger
  here. The other loaded kinds cannot independently establish the
  artifact-only patch contract: workflow-image evidence is
  repository-scoped, and environment, trigger, and step evidence do not
  provide the artifact arrival signal #5770 addresses.
- The `Reason` string (`ci/cd run-scoped evidence observed`) and the
  `ci_cd_run_correlation:<scope>` entity key are pinned byte-identical by this
  package's tests; one intent per scope generation. The root fan-out parity
  fixture holds no `ci.run` fact and does not cover this domain.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`: a
  trimmed `SourceRef.SourceSystem`, falling back to a trimmed
  `CollectorKind`. The pre-extraction root helper
  (`cicdRunCorrelationSourceSystem`) had the identical two-tier body, so the
  substitution is behavior-preserving, not a change.
- The payload is never decoded here and no schema version is checked here;
  the reducer handler owns that, plus the cross-scope
  `container_image_identity` read, which races the identity generation's
  activation the way #5423 documents for `container_image_identity`'s own
  OCI-manifest join — the reopen path in
  `go/cmd/bootstrap-index/bootstrap_pipeline.go` does not guarantee ordering
  between the two domains' reopened intents.
- The root dispatcher tests that go through `buildProjection` stay at root in
  `../ci_cd_run_correlation_projection_test.go`.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

No-Regression Evidence: this extraction moves one builder without changing
its trigger, value, or fan-out position. The reducer intent domain, entity
key, reason string, run-over-artifact anchor rule, and source-system
derivation are identical to the base commit, and the dispatcher's ordered
fan-out is unchanged at 44 builder probes with this probe still running
immediately after `buildContainerImageIdentityReducerIntent` and immediately
before `sbomattestation.BuildSBOMAttestationAttachmentReducerIntent`. The
family carried a private `cicdRunCorrelationSourceSystem` helper that was
checked body-for-body against `projectorintent.SourceSystem` and found
identical (trim `SourceRef.SourceSystem`, else trim `CollectorKind`, no
third tier), so the substitution is behavior-identical by construction and
the child tests pin both tiers; the root `firstOfKind` forwarder stays at
root for its remaining callers. Focused proof, run from the `go/` module
root: `go test ./internal/projector/cicdruncorrelation ./internal/projector
-count=1` green, whole-module `go build` and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
