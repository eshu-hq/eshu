# Incident Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`incident` fact family. A reducer handler never reads `Envelope.Payload["key"]`
for these kinds directly; it decodes through the parent `factschema` package's
kind-keyed seam (for example `factschema.DecodeIncidentRecord`) and receives one
of these structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## First dotted family

These are the first fact kinds in the contracts module with **dotted** wire
strings. The dots are a property of the wire kind the collector already emits
(`go/internal/facts.IncidentRecordFactKind == "incident.record"`); this package
matches them byte-for-byte and never invents or renames the namespace. The
schema filename is the dotted kind plus `.v1.schema.json`
(`incident.record.v1.schema.json`); nothing in schemagen, `payloadContracts`, or
the diff tooling parses the kind string for a separator.

## Purpose

Eight incident fact kinds decode through this package:

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `incident.record` | `IncidentRecord` | `factschema.DecodeIncidentRecord` |
| `incident.lifecycle_event` | `LifecycleEvent` | `factschema.DecodeIncidentLifecycleEvent` |
| `change.record` | `ChangeRecord` | `factschema.DecodeChangeRecord` |
| `incident_routing.applied_pagerduty_resource` | `AppliedPagerDutyResource` | `factschema.DecodeIncidentRoutingAppliedPagerDutyResource` |
| `incident_routing.applied_alert_route` | `AppliedAlertRoute` | `factschema.DecodeIncidentRoutingAppliedAlertRoute` |
| `incident_routing.observed_pagerduty_service` | `ObservedPagerDutyService` | `factschema.DecodeIncidentRoutingObservedPagerDutyService` |
| `incident_routing.observed_pagerduty_integration` | `ObservedPagerDutyIntegration` | `factschema.DecodeIncidentRoutingObservedPagerDutyIntegration` |
| `incident_routing.coverage_warning` | `CoverageWarning` | `factschema.DecodeIncidentRoutingCoverageWarning` |

## Required vs optional

A field is required exactly when its json tag carries no `omitempty` (and, by
the flat-struct convention, is non-pointer). The required set of each struct is
grounded in the actual collector emitters — a field is required only where the
emitter emits it unconditionally, optional where conditional:

- `IncidentRecord`: required `provider`, `provider_incident_id` (the durable
  incident identity; the emitter rejects a blank id). `service`, `service_id`,
  and every timeline field are optional pointers because the emitter builds them
  from possibly-nil references.
- `AppliedPagerDutyResource`: required routing/source classification, the
  Terraform state locator, and the backend join key (`resource_class`,
  `backend_kind`, `locator_hash`, `state_generation_id`, …). `provider_object_id`
  and `name_fingerprint` are optional — the emitter sets them only when the state
  attribute is present, and the correlation loader consumes a blank provider id
  as provenance-only.
- `CoverageWarning`: two emitters (Terraform-state and live PagerDuty) produce
  this kind with different bases, so the required set is their INTERSECTION;
  Terraform-only state fields are optional.

See each struct's godoc for the full field list and the emitter grounding.

## Manifest-gate blind spot

Some incident payload fields are read only by raw-SQL-JSONB loaders in
`go/internal/storage/postgres`, which the #4573 payload-usage manifest gate
cannot see. Those fields are still declared here, and the reducer-side
`TestIncidentRoutingSQLProjectedFieldsAreSchemaDeclared` locks the coverage so a
dropped field fails the build rather than silently breaking the SQL read.

## Ownership boundary

This package owns the Go type definitions and JSON tags for these eight fact
kinds' payloads. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_incident.go`). It does not own graph projection; the
reducer handlers under `go/internal/reducer` consume the decoded structs but live
outside this module.
