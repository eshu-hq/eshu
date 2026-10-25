// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import (
	"strings"
	"time"

	incidentv1 "github.com/eshu-hq/eshu-sdk/factschema/incident/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	factsincident "github.com/eshu-hq/eshu/go/internal/facts/incident"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// IncidentRoutingRawEvidence is the raw, undecoded incident-routing evidence for
// one scope generation as the storage loader hands it to the reducer: the fact
// envelopes for the incident-context and incident-routing fact kinds (payloads
// still map[string]any), plus the declared PagerDuty routing evidence read from
// content_entities metadata.
//
// The split is deliberate (Contract System v1 §3.2): the fact-payload half stays
// raw so the reducer — not the storage layer — decodes it through the typed
// contracts seam, routing a missing required field to a per-fact input_invalid
// quarantine via factdecode.PartitionDecodeFailures. The declared half is
// content-entity metadata, not a fact payload, so it is outside the payload
// contract boundary and stays decoded by the storage layer.
type IncidentRoutingRawEvidence struct {
	// Facts are the incident.record and incident_routing.* fact envelopes for the
	// scope generation, in the storage layer's stable order. The reducer decodes
	// each through the typed seam.
	Facts []facts.Envelope
	// Declared is the Terraform-source declared PagerDuty routing evidence, keyed
	// off the incident service-name allowlist the storage layer derived. It reads
	// content_entities metadata, not fact payloads, so it stays storage-decoded.
	Declared []IncidentRoutingDeclaredEvidence
}

// BuildIncidentRoutingEvidenceInputs is the exported cross-package seam over
// buildIncidentRoutingEvidenceInputs: identical behavior, one call deep, so
// offline guards (Ifá #6228) drive the production builder rather than a
// hand-built IncidentRoutingEvidenceInput. The unexported name stays put:
// IncidentContextBuildIncidentRoutingEvidenceInputsBinding and
// IncidentRoutingBuildIncidentRoutingEvidenceInputsBinding (fact_bindings.go)
// pin it as their Symbol.
func BuildIncidentRoutingEvidenceInputs(
	raw IncidentRoutingRawEvidence,
) ([]IncidentRoutingEvidenceInput, []factdecode.QuarantinedFact, error) {
	return buildIncidentRoutingEvidenceInputs(raw)
}

// DecodeIncidentRoutingDeclaredEvidence maps one content_entities
// PagerDutyDeclaration row — its scanned columns plus its metadata map — into
// the reducer declared-evidence model. It is the single copy of the
// metadata-to-Declared field mapping: the Postgres storage loader calls it
// per scanned row, and offline guards call it over production-parser-derived
// metadata, so the two can never drift on which metadata key feeds which
// field.
//
// The metadata half is outside the fact-payload contract boundary (Contract
// System v1 §3.2): content-entity metadata, not a fact payload. A missing key
// therefore reads as its empty value exactly as the pre-typing loader's raw
// map reads did, never as an input_invalid quarantine — only the fact-payload
// half of IncidentRoutingRawEvidence routes through the typed decode seam.
func DecodeIncidentRoutingDeclaredEvidence(
	entityID string,
	repoID string,
	relativePath string,
	entityName string,
	startLine int,
	metadata map[string]any,
) IncidentRoutingDeclaredEvidence {
	return IncidentRoutingDeclaredEvidence{
		EntityID:              entityID,
		RepoID:                repoID,
		RelativePath:          relativePath,
		EntityName:            entityName,
		DeclarationKind:       declaredEvidenceString(metadata, "declaration_kind"),
		SourceClass:           declaredEvidenceString(metadata, "source_class"),
		Outcome:               declaredEvidenceString(metadata, "outcome"),
		ServiceName:           declaredEvidenceString(metadata, "service_name"),
		ServiceNameResolution: declaredEvidenceString(metadata, "service_name_resolution"),
		EscalationPolicy:      declaredEvidenceString(metadata, "escalation_policy"),
		Environment:           declaredEvidenceString(metadata, "environment"),
		Workspace:             declaredEvidenceString(metadata, "workspace"),
		RedactionState:        declaredEvidenceString(metadata, "redaction_state"),
		UnsupportedReason:     declaredEvidenceString(metadata, "unsupported_reason"),
		DuplicateServiceName:  declaredEvidenceBool(metadata, "duplicate_service_name"),
		StartLine:             startLine,
	}
}

// declaredEvidenceString reads one metadata field the way the pre-typing
// loader did: absent or null reads as "", a string is trimmed, and any other
// scalar renders in its default form before trimming.
func declaredEvidenceString(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return strings.TrimSpace(payloadcore.AnyToString(value))
}

// declaredEvidenceBool reads one metadata boolean the way the pre-typing
// loader did: only a true boolean admits; absent, null, or any non-boolean
// reads as false.
func declaredEvidenceBool(metadata map[string]any, key string) bool {
	value, ok := metadata[key]
	if !ok || value == nil {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

// buildIncidentRoutingEvidenceInputs decodes the raw incident-routing fact
// envelopes into the typed reducer evidence model, one IncidentRoutingEvidenceInput
// per successfully decoded incident.record fact, and returns any facts it
// quarantined because their payload was missing a required field.
//
// Every decode routes through factdecode.PartitionDecodeFailures: an
// input_invalid decode error (a missing/null required field) quarantines that
// one fact and skips it while every valid fact still projects; any other error
// is fatal and aborts the whole intent, so a transient or programming error is
// never silently swallowed as a per-fact skip. This is the per-fact
// fault-isolation contract the AWS/IAM family established, applied to the
// incident-routing fact-payload reads that previously lived as raw
// payloadString map lookups in the storage loader.
//
// The declared evidence, the applied/observed/warning slices, and the warnings
// are shared across every incident packet exactly as the pre-typing loader built
// them: the reducer's downstream ExtractIncidentRoutingEvidenceRows selects the
// per-incident candidates from the shared slices, so byte-identical graph output
// on valid facts depends only on decoding the same field values the raw map reads
// produced.
func buildIncidentRoutingEvidenceInputs(
	raw IncidentRoutingRawEvidence,
) ([]IncidentRoutingEvidenceInput, []factdecode.QuarantinedFact, error) {
	var quarantined []factdecode.QuarantinedFact

	incidents := make([]IncidentRoutingIncident, 0)
	applied := make([]IncidentRoutingAppliedEvidence, 0)
	observed := make([]IncidentRoutingObservedEvidence, 0)
	warnings := make([]IncidentRoutingCoverageWarning, 0)

	for _, env := range raw.Facts {
		if env.IsTombstone {
			continue
		}
		switch env.FactKind {
		case factsincident.IncidentRecordFactKind:
			record, err := schemadecode.DecodeIncidentRecord(env)
			if err != nil {
				q, ok, fatal := factdecode.PartitionDecodeFailures(env, err)
				if fatal != nil {
					return nil, nil, fatal
				}
				if ok {
					quarantined = append(quarantined, q)
				}
				continue
			}
			if incident := incidentRoutingIncidentFromDecoded(env, record); incident.ProviderIncidentID != "" {
				incidents = append(incidents, incident)
			}
		case factsincident.IncidentRoutingAppliedPagerDutyResourceFactKind:
			resource, err := schemadecode.DecodeIncidentRoutingAppliedPagerDutyResource(env)
			if err != nil {
				q, ok, fatal := factdecode.PartitionDecodeFailures(env, err)
				if fatal != nil {
					return nil, nil, fatal
				}
				if ok {
					quarantined = append(quarantined, q)
				}
				continue
			}
			// Only service-class applied resources become reducer input; other
			// resource classes (teams, escalation policies, ...) are dropped, the
			// same filter the pre-typing loader applied. TrimSpace before the
			// compare preserves that loader's exact behavior: its
			// incidentRoutingPayloadString(...) trimmed the value, so a padded
			// "service " matched == "service" and was admitted. Comparing the raw
			// decoded string here would silently drop such a fact.
			if strings.TrimSpace(resource.ResourceClass) != "service" {
				continue
			}
			applied = append(applied, incidentRoutingAppliedFromDecoded(env, resource))
		case factsincident.IncidentRoutingObservedPagerDutyServiceFactKind:
			service, err := schemadecode.DecodeIncidentRoutingObservedPagerDutyService(env)
			if err != nil {
				q, ok, fatal := factdecode.PartitionDecodeFailures(env, err)
				if fatal != nil {
					return nil, nil, fatal
				}
				if ok {
					quarantined = append(quarantined, q)
				}
				continue
			}
			observed = append(observed, incidentRoutingObservedFromDecoded(env, service))
		case factsincident.IncidentRoutingCoverageWarningFactKind:
			warning, err := schemadecode.DecodeIncidentRoutingCoverageWarning(env)
			if err != nil {
				q, ok, fatal := factdecode.PartitionDecodeFailures(env, err)
				if fatal != nil {
					return nil, nil, fatal
				}
				if ok {
					quarantined = append(quarantined, q)
				}
				continue
			}
			warnings = append(warnings, incidentRoutingWarningFromDecoded(env, warning))
		}
	}

	if len(incidents) == 0 {
		return nil, quarantined, nil
	}

	inputs := make([]IncidentRoutingEvidenceInput, 0, len(incidents))
	for _, incident := range incidents {
		inputs = append(inputs, IncidentRoutingEvidenceInput{
			Incident: incident,
			Declared: raw.Declared,
			Applied:  applied,
			Observed: observed,
			Warnings: warnings,
		})
	}
	return inputs, quarantined, nil
}

// incidentRoutingIncidentFromDecoded maps a decoded incident.record payload plus
// its envelope metadata into the reducer incident anchor. The typed payload
// supplies the provider, incident id, and service reference; the envelope
// supplies the scope, evidence fact id, observed-at, source confidence, and the
// source-url fallback the decode seam cannot see (SourceRef.SourceURI). Provider
// and ProviderIncidentID are required by the contract so they are always present
// here; the pre-typing SourceRecordID fallback for a blank incident id is dead
// and intentionally dropped.
func incidentRoutingIncidentFromDecoded(
	env facts.Envelope,
	record incidentv1.IncidentRecord,
) IncidentRoutingIncident {
	serviceID := strings.TrimSpace(payloadcore.DerefString(record.ServiceID))
	if serviceID == "" && record.Service != nil {
		serviceID = strings.TrimSpace(payloadcore.DerefString(record.Service.ID))
	}
	var serviceName, serviceURL string
	if record.Service != nil {
		serviceName = strings.TrimSpace(payloadcore.DerefString(record.Service.Summary))
		serviceURL = strings.TrimSpace(payloadcore.DerefString(record.Service.URL))
	}
	return IncidentRoutingIncident{
		Provider:           payloadcore.FirstNonBlank(strings.TrimSpace(record.Provider), "pagerduty"),
		ProviderIncidentID: strings.TrimSpace(record.ProviderIncidentID),
		ScopeID:            env.ScopeID,
		ServiceID:          serviceID,
		ServiceName:        serviceName,
		ServiceURL:         serviceURL,
		EvidenceFactID:     env.FactID,
		SourceURL:          payloadcore.FirstNonBlank(strings.TrimSpace(payloadcore.DerefString(record.SourceURL)), env.SourceRef.SourceURI),
		SourceConfidence:   env.SourceConfidence,
		ObservedAt:         incidentRoutingFormatEnvelopeTime(env.ObservedAt),
	}
}

// incidentRoutingAppliedFromDecoded maps a decoded applied PagerDuty resource
// payload plus its envelope metadata into the reducer applied-evidence row. Only
// the caller's service-class filter admits it. Optional pointer fields deref to
// their empty string when absent, matching the pre-typing payloadString reads.
func incidentRoutingAppliedFromDecoded(
	env facts.Envelope,
	resource incidentv1.AppliedPagerDutyResource,
) IncidentRoutingAppliedEvidence {
	return IncidentRoutingAppliedEvidence{
		FactID:                    env.FactID,
		SourceClass:               strings.TrimSpace(resource.SourceClass),
		SourceKind:                strings.TrimSpace(resource.SourceKind),
		Outcome:                   strings.TrimSpace(resource.Outcome),
		ResourceClass:             strings.TrimSpace(resource.ResourceClass),
		ProviderObjectID:          strings.TrimSpace(payloadcore.DerefString(resource.ProviderObjectID)),
		NameFingerprint:           strings.TrimSpace(payloadcore.DerefString(resource.NameFingerprint)),
		EscalationPolicyReference: strings.TrimSpace(payloadcore.DerefString(resource.EscalationPolicyReference)),
		TerraformStateAddress:     strings.TrimSpace(resource.TerraformStateAddress),
		ProviderAddress:           strings.TrimSpace(resource.ProviderAddress),
		ModuleAddress:             strings.TrimSpace(resource.ModuleAddress),
		StateGenerationID:         strings.TrimSpace(resource.StateGenerationID),
		DeclaredMatchState:        strings.TrimSpace(resource.DeclaredMatchState),
		RedactionState:            strings.TrimSpace(resource.RedactionState),
		ObservedAt:                incidentRoutingFormatEnvelopeTime(env.ObservedAt),
	}
}

// incidentRoutingObservedFromDecoded maps a decoded observed PagerDuty service
// payload plus its envelope metadata into the reducer observed-evidence row. The
// booleans deref to false when absent (the emitter omits a false flag), matching
// the pre-typing payloadBool reads, and the source-url fallback to
// SourceRef.SourceURI is preserved.
func incidentRoutingObservedFromDecoded(
	env facts.Envelope,
	service incidentv1.ObservedPagerDutyService,
) IncidentRoutingObservedEvidence {
	return IncidentRoutingObservedEvidence{
		FactID:                    env.FactID,
		SourceClass:               strings.TrimSpace(service.SourceClass),
		SourceKind:                strings.TrimSpace(service.SourceKind),
		Outcome:                   strings.TrimSpace(service.Outcome),
		ServiceID:                 strings.TrimSpace(service.ServiceID),
		ProviderObjectID:          strings.TrimSpace(service.ProviderObjectID),
		NameFingerprint:           strings.TrimSpace(payloadcore.DerefString(service.NameFingerprint)),
		Status:                    strings.TrimSpace(payloadcore.DerefString(service.Status)),
		EscalationPolicyReference: strings.TrimSpace(payloadcore.DerefString(service.EscalationPolicyReference)),
		DeclaredMatchState:        strings.TrimSpace(service.DeclaredMatchState),
		DriftCandidateReason:      strings.TrimSpace(payloadcore.DerefString(service.DriftCandidateReason)),
		RedactionState:            strings.TrimSpace(service.RedactionState),
		SourceURL:                 payloadcore.FirstNonBlank(strings.TrimSpace(payloadcore.DerefString(service.SourceURL)), env.SourceRef.SourceURI),
		Disabled:                  payloadcore.DerefBool(service.Disabled),
		Deleted:                   payloadcore.DerefBool(service.Deleted),
		ManuallyCreated:           payloadcore.DerefBool(service.ManuallyCreated),
		ObservedAt:                incidentRoutingFormatEnvelopeTime(env.ObservedAt),
	}
}

// incidentRoutingWarningFromDecoded maps a decoded coverage-warning payload plus
// its envelope metadata into the reducer coverage-warning row. ProviderObjectID
// is an optional pointer (only the live emitter sets it) and derefs to empty.
func incidentRoutingWarningFromDecoded(
	env facts.Envelope,
	warning incidentv1.CoverageWarning,
) IncidentRoutingCoverageWarning {
	return IncidentRoutingCoverageWarning{
		FactID:           env.FactID,
		SourceClass:      strings.TrimSpace(warning.SourceClass),
		SourceKind:       strings.TrimSpace(warning.SourceKind),
		Reason:           strings.TrimSpace(warning.Reason),
		ResourceClass:    strings.TrimSpace(payloadcore.DerefString(warning.ResourceClass)),
		ProviderObjectID: strings.TrimSpace(payloadcore.DerefString(warning.ProviderObjectID)),
		ObservedAt:       incidentRoutingFormatEnvelopeTime(env.ObservedAt),
	}
}

// incidentRoutingFormatEnvelopeTime renders an envelope observed-at timestamp as
// the RFC 3339 UTC string the reducer evidence model carries, or "" for a zero
// time. It matches the storage loader's pre-typing incidentRoutingFormatTime so
// the mapped ObservedAt string is byte-identical to the pre-migration value.
func incidentRoutingFormatEnvelopeTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
