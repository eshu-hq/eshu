// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "incident" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_incident.go).
//
// Eight fact kinds live here, spanning the incident-context source
// (PagerDuty incidents, log entries, and related change events) and the
// incident-routing source (Terraform-state applied PagerDuty resources and
// alert routes, live PagerDuty service and integration observations, and
// bounded coverage warnings):
//
//   - IncidentRecord (incident.record)
//   - LifecycleEvent (incident.lifecycle_event)
//   - ChangeRecord (change.record)
//   - AppliedPagerDutyResource (incident_routing.applied_pagerduty_resource)
//   - AppliedAlertRoute (incident_routing.applied_alert_route)
//   - ObservedPagerDutyService (incident_routing.observed_pagerduty_service)
//   - ObservedPagerDutyIntegration (incident_routing.observed_pagerduty_integration)
//   - CoverageWarning (incident_routing.coverage_warning)
//
// These are the first DOTTED fact kinds in the contracts module: the wire
// strings carry namespace dots (for example "incident.record"), unlike the
// underscore-separated aws/iam family. The dots are a property of the wire
// kind the collector already emits (go/internal/facts.IncidentRecordFactKind);
// this package MATCHES them exactly and never invents or renames them. Nothing
// in the schema-generation, decode, or drift-lock tooling parses the kind
// string for a separator, so a dotted kind needs no special handling beyond a
// dotted schema filename (for example incident.record.v1.schema.json).
//
// Each struct's required fields are non-pointer with no omitempty tag; the
// decode seam rejects a payload that omits one, or supplies an explicit JSON
// null for one, with a classified ClassificationInputInvalid error naming the
// field, never a zero-value struct. Optional fields are pointers, slices, or
// maps carrying omitempty, so an absent value decodes to nil and stays
// distinct from an observed zero.
//
// The required set of each struct is grounded in the actual collector
// emitters (go/internal/collector/pagerduty/envelope.go and config_envelope.go,
// go/internal/collector/terraformstate/pagerduty_applied.go): a field is
// required only where the emitter emits it unconditionally, and optional where
// the emitter emits it conditionally (for example the applied resource's
// provider_object_id, which a name-only routing resource omits).
//
// The reducer decodes only the latest struct for each kind. Version shims for
// an older schema major live in the parent factschema package's decode seam
// (decodeLatestMajor in decode.go), never in this package or in reducer
// handler code.
package v1
