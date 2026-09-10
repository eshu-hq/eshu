# Incident-routing projector intents

## Purpose

This package recognizes PagerDuty incident-routing evidence for one scope
generation — an `incident.record` fact or a routing source kind registered by
`facts.IncidentRoutingFactKinds()` — and builds the reducer intent that asks
the reducer to compare declared, applied, and live routing and write
`IncidentRoutingEvidence` graph truth.

## Ownership boundary

The package owns only the incident-routing trigger selection and
reducer-intent value. The root `internal/projector` package validates
scope-generation boundaries, constructs and owns the immutable fact lookup,
preserves family order, and owns projection lifecycle, queue writes, retries,
and telemetry. Reducer handlers own routing comparison, service resolution,
`IncidentRoutingEvidence` nodes, `HAS_INTENDED_ROUTING` edges, and readiness
publication.

## Exported surface

- `BuildReducerIntent` builds the
  `incident_routing_materialization` intent, anchored to the earliest
  candidate fact in original input order across `incident.record` and every
  kind `facts.IncidentRoutingFactKinds()` returns.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. The candidate-kind list comes from
`internal/facts`, so a new routing source kind registered there is picked up
without touching this package. The builder triggers on fact presence only and
never decodes the payload, so it carries no `factschema_decode_*` seam.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; reducer
handlers retain execution, comparison, and graph-write telemetry. Moving the
pure builder adds no queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- The anchor is chosen with `FirstAcrossKinds`, which returns the earliest
  accepted fact in original input order across all candidate kinds. A
  `incident_routing.observed_pagerduty_service` fact that precedes an
  `incident.record` fact wins, even though `incident.record` is listed first.
  The root fan-out parity fixture pins this exact case.
- The `EntityKey` is `incident_routing_materialization:<scope>` — one intent
  per scope generation, family-distinct from the shared AWS
  `aws_resource_materialization:<scope>` key the S3, RDS, and workload-cloud
  builders use.
- `SourceSystem` is `SourceRef.SourceSystem` trimmed, falling back to
  `CollectorKind`; a blank source ref does not drop the intent.
- The projector never compares routing evidence or infers service truth from
  PagerDuty payloads; that is reducer-owned, and this package never writes a
  node or edge.

## Verification

Run the package contract tests, the root incident-routing `buildProjection`
tests, ordered fan-out parity and probe-count tests, the projector package
tree, package-doc and path mirrors, dirgate, telemetry coverage, and the
golden-corpus gates selected by the changed paths.

No-Regression Evidence: the #6627 move changes the package path, filename,
and exported builder name without changing the production body, trigger,
intent value, or fan-out position. The dispatcher's ordered fan-out remains
44 builder probes, with this probe immediately after observability coverage
correlation and before code taint evidence. The focused package and root
dispatch tests pin those contracts; the issue evidence record contains the
exact commands and results.

The nested namespace makes the ownership boundary legible for a future
service split, but it does not make this package independently extractable.
It still imports Eshu's internal facts, projector-intent, and reducer
contracts, while root projector assembly remains its only production caller.

## Related docs

- [Projector architecture](../../README.md)
- [Intent contract](../../intent/README.md)
- [PagerDuty evidence](../../../../../docs/public/reference/pagerduty-evidence.md)
- [Package restructure](../../../../../docs/internal/design/package-restructure.md)
