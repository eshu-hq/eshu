# Observability Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`observability` fact family. A reducer handler never reads
`Envelope.Payload["some_key"]` for these kinds directly; it decodes through the
parent `factschema` package's kind-keyed seam (for example
`factschema.DecodeObservabilityObservedTarget`) and receives one of these
structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/observability/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

Eighteen fact kinds decode through this package, spanning two collection lanes
that share one reducer domain (`observability_coverage_correlation`). All
eighteen are consumed by the reducer's coverage-metadata classifier.

| Fact kind | Struct | Decode function | Required fields |
| --- | --- | --- | --- |
| `observability.declared_folder` | `DeclaredFolder` | `DecodeObservabilityDeclaredFolder` | `source_instance_id` |
| `observability.declared_dashboard` | `DeclaredDashboard` | `DecodeObservabilityDeclaredDashboard` | `source_instance_id` |
| `observability.declared_datasource` | `DeclaredDatasource` | `DecodeObservabilityDeclaredDatasource` | `source_instance_id` |
| `observability.declared_alert_rule` | `DeclaredAlertRule` | `DecodeObservabilityDeclaredAlertRule` | `source_instance_id` |
| `observability.declared_scrape_config` | `DeclaredScrapeConfig` | `DecodeObservabilityDeclaredScrapeConfig` | `source_instance_id` |
| `observability.declared_metric_rule` | `DeclaredMetricRule` | `DecodeObservabilityDeclaredMetricRule` | `source_instance_id` |
| `observability.declared_metric_route` | `DeclaredMetricRoute` | `DecodeObservabilityDeclaredMetricRoute` | `source_instance_id` |
| `observability.declared_log_route` | `DeclaredLogRoute` | `DecodeObservabilityDeclaredLogRoute` | `source_instance_id` |
| `observability.declared_trace_route` | `DeclaredTraceRoute` | `DecodeObservabilityDeclaredTraceRoute` | `source_instance_id` |
| `observability.applied_resource` | `AppliedResource` | `DecodeObservabilityAppliedResource` | `source_instance_id` |
| `observability.applied_sync_state` | `AppliedSyncState` | `DecodeObservabilityAppliedSyncState` | `source_instance_id` |
| `observability.observed_dashboard` | `ObservedDashboard` | `DecodeObservabilityObservedDashboard` | `source_instance_id`, `provider_object_uid` |
| `observability.observed_target` | `ObservedTarget` | `DecodeObservabilityObservedTarget` | `source_instance_id`, `provider_object_uid` |
| `observability.observed_rule` | `ObservedRule` | `DecodeObservabilityObservedRule` | `source_instance_id` |
| `observability.observed_log_signal` | `ObservedLogSignal` | `DecodeObservabilityObservedLogSignal` | `source_instance_id`, `provider_object_uid` |
| `observability.observed_trace_signal` | `ObservedTraceSignal` | `DecodeObservabilityObservedTraceSignal` | `source_instance_id`, `provider_object_uid` |
| `observability.coverage_warning` | `CoverageWarning` | `DecodeObservabilityCoverageWarning` | `source_instance_id` |
| `observability.source_instance` | `SourceInstance` | `DecodeObservabilitySourceInstance` | `source_instance_id` |

## Required-field rationale

`source_instance_id` is the one identity field EVERY observability collector
injects on EVERY kind in BOTH lanes (the git passthrough's
`observabilityBasePayload` and every live-collector `basePayload`), so it is
required on all eighteen. A fact missing it is a malformed emission that
dead-letters `input_invalid` rather than yielding a coverage decision with no
source anchor.

`provider_object_uid` is additionally required on the four observed kinds whose
sole live emitter validates the uid non-blank and always writes it:
`observed_dashboard` (grafana), `observed_target` (prometheusmimir),
`observed_log_signal` (loki), `observed_trace_signal` (tempo). It stays
**optional** on `observed_rule`, because the Grafana observed-rule emitter
identifies the rule by `alert_rule_uid` instead — requiring
`provider_object_uid` there would dead-letter every Grafana observed rule.

No per-kind UID or name field is required on the declared lane: that lane is a
generic passthrough that copies whatever keys the source manifest declared, so
requiring, for example, `dashboard_uid` on `DeclaredDashboard` would
dead-letter a valid declared fact whose manifest lacks that exact key.

## Why every struct carries the full candidate-key union

The family's single reducer payload consumer
(`go/internal/reducer/observability_coverage_metadata.go`) reads a bounded union
of named keys — a 20-entry object-ref fallback chain plus the
provider/class/outcome/freshness/service reads — via `firstNonBlank` and
`switch`, the SAME union regardless of fact kind. Each struct models that full
union so the reducer reads every candidate from the typed struct. Because the
parent module's marshal-free decoder ignores unknown top-level keys, a closed
struct exposing exactly those keys is byte-identical to the raw `payloadString`
map access it replaces.

## Ownership boundary

This package owns the Go type definitions and JSON codec for these eighteen fact
kinds' payloads. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_observability.go`). It does not own graph projection;
reducer handlers under `go/internal/reducer` (the
`observability_coverage_correlation` and `observability_coverage_materialization`
domains) consume the decoded structs but live outside this module.

## Regeneration

After changing any payload struct's fields, run `go generate ./...` from the
module root (`sdk/go/factschema`) and commit the regenerated schema under
`../../schema/`, refresh the embedded fixture-pack copy under
`../../fixturepack/schema/`, and update the fixture-pack payload examples under
`../../fixturepack/payloads/`. `schema_gen_test.go`, the fixture-pack drift
tests, and the derived-key-set drift tests fail the build on any divergence.
