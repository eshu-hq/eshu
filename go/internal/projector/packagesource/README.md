# Package-source-correlation projector intents

## Purpose

This package recognizes package-registry source hints and package identity
for one scope generation and builds the reducer intent that asks the reducer
to classify those hints, and the manifest-backed consumption that depends on
them, against active Git facts.

## Ownership boundary

The package owns only the trigger selection and the reducer-intent value. The
root `internal/projector` package validates scope-generation boundaries,
constructs and owns the immutable fact lookup, preserves family order, and
owns projection lifecycle, queue writes, retries, and telemetry. The reducer's
`DomainPackageSourceCorrelation` handler owns hint classification, the
ownership and publication decisions, consumption admission, and every graph
write; the projector never mints repository ownership, publication, or
consumption truth from a source hint.

## Exported surface

- `BuildPackageSourceCorrelationReducerIntent` builds the
  `package_source_correlation` intent, anchored to the first
  `package_registry.source_hint` fact in the generation, or to the first
  `package_registry.package` fact when the generation carries no hint.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. It reads `internal/facts` for the two
trigger kinds and `internal/reducer` for the domain constant. There is no
decode seam: unlike `ec2`, `s3`, and `iamcanassume`, this builder reads only
`envelope.FactKind` and never a payload field, which is why
`sdk/go/factschema/packageregistry/v1/README.md` lists it as a routing-only
consumer of `SourceHint`.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains the `eshu_dp_package_source_correlations_total` and
`eshu_dp_package_consumption_repo_edges_total` counters. Moving the pure
builder adds no queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- Kind priority beats input position. The builder checks
  `package_registry.source_hint` before `package_registry.package`, so a
  source hint placed after an identity fact still anchors the intent. Within
  one kind, the earliest fact in original input order wins. Collapsing the two
  probes into a single `FirstAcrossKinds` scan would change `FactID` and
  `Reason` for any generation that carries both kinds.
- The two branches carry different `Reason` strings (`package registry source
  hints observed` and `package registry identity observed`). The root fan-out
  parity fixture pins the identity branch; keep both strings stable.
- The `EntityKey` is `package_source_correlation:<scope>`, one intent per
  scope generation regardless of how many hints or packages it carries. The
  reducer handler reloads the generation's facts itself and classifies every
  hint; the anchor `FactID` is a stable claim, not the only fact processed.
- The payload is never decoded here. A hint with a malformed payload still
  anchors the intent; the reducer's own raw-read classifier decides what to do
  with it. Do not add a typed decode predicate here without also deciding what
  a decode failure should mean for the reducer's counter-only admission.
- `SourceSystem` is `SourceRef.SourceSystem` trimmed, falling back to a
  trimmed `CollectorKind`; a blank source ref does not drop the intent.
- Package identity also triggers the root `supply_chain_impact` builder on the
  same fact. That family is separate and stays at root; do not fold its
  trigger in here.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

No-Regression Evidence: this extraction moves one builder without changing
the trigger, value, or fan-out position. The reducer intent domain, entity
key, both reason strings, kind-priority anchor selection, and source-system
derivation are identical to the base commit, and the dispatcher's ordered
fan-out is unchanged at 44 builder probes with this probe still running first,
immediately before `awscloudruntimedrift.BuildAWSCloudRuntimeDriftReducerIntent`. The private
`packageSourceCorrelationSourceSystem` helper the root file owned was compared
body-for-body against its `projectorintent.SourceSystem` replacement (both
trim `SourceRef.SourceSystem` and fall back to a trimmed `CollectorKind`) and
had no other caller, so it was dropped rather than moved; the root
`firstOfKind` forwarder was a direct delegate to
`projectorintent.FactLookup.FirstOfKind`, so both substitutions are
behavior-identical by construction, and that forwarder stays at root for the
eleven root probes that still call it. The `packageIdentityEnvelope` test
fixture stays at root because the fan-out and supply-chain-impact tests still
build on it. Focused proof, run from the `go/` module root:
`../scripts/go-test-run-guard.sh 1 'TestBuildPackageSourceCorrelationReducerIntent' -- ./internal/projector/packagesource -count=1`
and `go test ./internal/projector/... -count=1` green, whole-module `go build`
and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package registry payload contract](../../../../sdk/go/factschema/packageregistry/v1/README.md)
- [Reducer domain catalog](../../reducer/domain-catalog.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
