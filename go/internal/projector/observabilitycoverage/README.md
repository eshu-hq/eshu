# Observability-coverage-correlation projector intents

## Purpose

This package recognizes observability evidence in one scope generation and
builds the reducer intent that asks the reducer's
`observability_coverage_correlation` domain to correlate coverage of
monitored cloud resources versus uncovered gaps (issue #391). Two branches
trigger it: any observability source fact the
`facts.ObservabilitySchemaVersion` registry recognizes (declared dashboards,
alerts, log/trace sources — excluding `observability_source.instance`,
which is deliberately not a trigger), or any `aws_resource` fact whose
decoded `resource_type` is an AWS-native observability object (CloudWatch
alarm, composite alarm, dashboard, logs log group, X-Ray sampling
rule/group).

## Ownership boundary

The package owns only the correlation trigger selection and reducer-intent
value. The root `internal/projector` package validates scope-generation
boundaries (including observability schema-version admission), constructs and
owns the immutable fact lookup, preserves family order, and owns projection
lifecycle, queue writes, retries, and telemetry. The reducer's
`DomainObservabilityCoverageCorrelation` handler (domain definition
`observabilityCoverageCorrelationDomainDefinition` in
`go/internal/reducer/registry_additive_domains.go`, correlation contract in
`go/internal/reducer/observability_coverage_correlation.go`) owns the
six-outcome correlation, the canonical coverage write, and the counters.

## Exported surface

- `BuildObservabilityCoverageCorrelationReducerIntent` builds the
  `observability_coverage_correlation` intent, anchored to the earliest
  trigger fact observed in the generation.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup` and
`internal/projector/intent.ReducerIntent`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. The AWS branch decodes `aws_resource`
payloads through this package's own `factschema_decode_aws.go` seam (the
`ec2` pattern), not root's classified wrapper. The source-system label is the
family's local three-tier helper, NOT the shared two-tier
`projectorintent.SourceSystem` — the third literal `"observability"` tier is
load-bearing and pinned by a package test.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`, and the
reducer handler retains the observability-coverage correlation span and
counters. Moving the pure builder adds no queue, storage, graph, span,
metric, or log boundary.

## Gotchas / invariants

- `observabilityResourceTypes` is a three-way mirror: this package's copy,
  root's materialization-trigger copy
  (`../observability_coverage_materialization_intents.go`), and the
  reducer's `observabilityResourceSignals`
  (`go/internal/reducer/observability_coverage_correlation_index.go`) must
  agree on what counts as an observability object. Add a resource type to
  all three or the triggers and the classifier diverge.
- The candidate-kind predicate mirrors the trigger's kind-level branches so
  `FirstMatchingKindPredicate` visits only kinds that can match; the AWS
  branch's per-envelope `resource_type` decode stays in the trigger as the
  final accept check.
- An undecodable `aws_resource` payload is not a trigger and not an error
  here — the decode error collapses to an empty resource type that never
  matches the set. Root's quarantine path owns flagging the invalid fact.
- The entity key is `observability_coverage_correlation:<scope>`, a
  family-distinct key; it does not participate in the
  `aws_resource_materialization:<scope>` readiness-gate key the AWS edge
  families share.
- A trigger fact with a blank source ref and blank collector kind labels the
  intent `"observability"` — the preserved pre-extraction third tier.

## Verification

Run the package contract tests, ordered fan-out and probe-count tests, the
projector package tree, package-doc and path mirrors, dirgate, the
payload-usage manifest gate, and the golden-corpus gates selected by the
changed paths.

No-Regression Evidence: this extraction moves one builder without changing
its trigger, value, or fan-out position. The reducer intent domain, entity
key, reasons, anchor selection, and source-system derivation are identical to
the base commit, and the dispatcher's ordered fan-out is unchanged at 44
builder probes with this probe still running immediately after
`buildObservabilityCoverageMaterializationReducerIntent` and immediately
before `incidentrouting.BuildIncidentRoutingMaterializationReducerIntent`.
The family's `observabilitySourceSystem` helper was compared body-for-body
against `projectorintent.SourceSystem` and was NOT identical — it carries a
literal third `"observability"` fallback where the shared helper returns an
empty string — so it moved with the family unchanged and is pinned
(mutation-tested: with the third tier changed to return an empty string,
`TestObservabilitySourceSystemThirdTierFallback` fails; the restored body
passes). The root `firstMatchingKindPredicate` forwarder this family was the
last root caller of was a direct delegate to
`projectorintent.FactLookup.FirstMatchingKindPredicate`, so dispatching on
`index.lookup` is behavior-identical by construction; the forwarder was
removed and its per-distinct-kind evaluation proof relocated to
`../intent/fact_lookup_test.go`. The child's `decodeAWSResource` calls the
same `factschema.DecodeAWSResource` through the same
`factenvelope.FactSchemaFromInternal` adapter root's wrapper delegates to,
and the sole caller discards the error, so the decode substitution is
behavior-identical for this trigger. Focused proof, run from the `go/`
module root: `go test ./internal/projector/observabilitycoverage
./internal/projector ./internal/projector/intent -count=1` green,
whole-module `go build ./...` and `go vet ./internal/projector/...` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
