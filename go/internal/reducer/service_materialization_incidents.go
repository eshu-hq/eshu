// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ServiceScopedIncidentEvidenceLoader returns the current incident-routing
// evidence for one or more correlated services, regardless of which fact
// generation produced it. It is the incidents-family analogue of
// ServiceScopedDocumentationEvidenceLoader: keyed by Eshu catalog service id, not
// repository id, because incident routing evidence links to a service through the
// provider incident anchor, not through a repository generation.
//
// CONTRACT (correlation truth). The map key MUST be the durable Eshu catalog
// service id (the entityRef the service-scope changed-since surface diffs by),
// NOT the PagerDuty provider service id. Incident routing evidence natively
// carries only the provider service id (incident.Service.ID), so a production
// loader must resolve it through durable exact/derived reducer correlations and
// fail closed for ambiguous repository ownership. A loader MUST NOT use fuzzy
// service-name matching. The returned records MUST carry only durable external
// identity (provider, provider_incident_id, slot, evidence_kind, and the durable
// StableFactKey/content-entity id), never a fact_id or generation id, so the
// incidents service_evidence_key stays generation-stable.
type ServiceScopedIncidentEvidenceLoader interface {
	GetIncidentEvidenceForServices(
		ctx context.Context,
		serviceIDs []string,
	) (map[string][]ServiceIncidentRecord, error)
}

// Incidents evidence family (#1989, part of #1943/#1797). A service's incident
// evidence is the set of exact PagerDuty incident-routing evidence rows that
// route to the service: one row per routing slot (intended / applied / live).
// Each row becomes one generation-stable service_evidence_snapshots row in the
// incidents family, reusing the Stage-1 lineage, payload-hash, and tombstone
// machinery verbatim.
//
// Stable identity (verified generation-independent). An incident evidence row is
// keyed by its durable routing identity:
//
//	incidents:<service_id>:<provider>:<provider_incident_id>:<slot>:<evidence_kind>:<evidence_id>
//
// The source read model is incident routing evidence
// (reducer.ExtractIncidentRoutingEvidenceRows over
// IncidentRoutingEvidenceInput). Its graph rows carry provider, provider_incident_id,
// slot, evidence_kind, and evidence_id. CRITICAL durability finding: for the
// applied and live slots the graph row's evidence_id is envelope.FactID, which
// DIGESTS generation_id (see collector/terraformstate/pagerduty_applied.go and
// collector/pagerduty/config_envelope.go: FactID = StableID(..., {..., generation_id});
// the collector envelope tests assert FactID differs across generations while
// StableFactKey does not). Keying on that FactID would reproduce the 100% false
// churn this design (#1943) warns about. The incidents family therefore keys
// evidence_id on the fact's generation-INDEPENDENT StableFactKey (or, for the
// intended/declared slot, the durable content-entity id), never on the FactID.
// provider_incident_id, slot, and evidence_kind are all durable. The StateGenerationID
// of an applied routing fact is per-run metadata and is excluded from both the key
// and the payload. This is the identity-vs-generation distinction from design #1231.

// ServiceIncidentRecord is one durable incident-routing evidence row that routes
// to a service, as read from the incident routing evidence read path. The reducer
// converts it into a generation-stable incidents evidence row. Only the durable
// routing identity is keyed; observable fields are hashed into the payload, and
// the generation lives in the row, never in the key.
type ServiceIncidentRecord struct {
	// Provider is the durable incident provider (for example "pagerduty"). It is
	// part of the stable identity.
	Provider string
	// ProviderIncidentID is the durable provider incident id the routing evidence
	// anchors on. It is part of the stable identity.
	ProviderIncidentID string
	// Slot is the durable routing slot (intended_routing, applied_routing, or
	// live_routing). It is part of the stable identity.
	Slot string
	// EvidenceKind is the durable source-fact kind label for the slot (for example
	// "incident_routing.applied_pagerduty_resource"). It is part of the stable
	// identity.
	EvidenceKind string
	// EvidenceID is the durable, generation-INDEPENDENT id of the source evidence:
	// the fact's StableFactKey for applied/live routing facts, or the durable
	// content-entity id for the intended/declared slot. It MUST NOT be the
	// generation-bearing envelope FactID. It is part of the stable identity.
	EvidenceID string
	// TruthLabel is the routing truth label (observable, hashed into the payload).
	// The incident-routing projection only materializes exact rows, so this is
	// "exact" today, but it is hashed so a future truth change flips the row.
	TruthLabel string
	// ProviderObjectID is the resolved provider object id for the slot (observable,
	// hashed into the payload). It is not part of the identity.
	ProviderObjectID string
	// DeclaredMatchState is the declared-vs-applied match state (observable, hashed
	// into the payload so a drift change flips the row to updated). It is optional
	// and not part of the identity.
	DeclaredMatchState string
	// RedactionState is the evidence redaction state (observable, hashed into the
	// payload). It is optional and not part of the identity.
	RedactionState string
	// Retired records an incident routing row that was explicitly removed in this
	// re-materialization. It is written as a tombstone row.
	Retired bool
}

// ServiceIncidentEvidence is one generation-stable incidents row for a service.
// Identity is the durable routing-identity tuple; the generation lives in the
// row, never in the key. A retired record carries Retired=true so the delta
// classifies it explicitly rather than letting it vanish into unchanged.
type ServiceIncidentEvidence struct {
	// Identity is the generation-independent per-row identity
	// (see serviceIncidentEvidenceIdentity). It is combined with the service id to
	// form the service_evidence_key.
	Identity string
	// Payload is the durable evidence body whose hash drives updated-vs-unchanged
	// classification. It captures the row's stable, observable fields.
	Payload map[string]any
	// Retired records a record that was explicitly removed in this
	// re-materialization. It is written as a tombstone row.
	Retired bool
}

// ServiceIncidentEvidenceKey returns the generation-independent identity for one
// incidents row: incidents:<service_id>:<identity>. The identity is the durable
// routing tuple (provider:provider_incident_id:slot:evidence_kind:evidence_id);
// the generation is stored in a column, never embedded here.
func ServiceIncidentEvidenceKey(serviceID, identity string) string {
	return strings.Join([]string{
		ServiceEvidenceFamilyIncidents,
		strings.TrimSpace(serviceID),
		strings.TrimSpace(identity),
	}, ":")
}

// serviceIncidentEvidenceIdentity derives the generation-independent identity for
// one incident routing record from its durable routing tuple. It MUST NOT include
// the envelope fact_id or any generation id; EvidenceID is the durable
// StableFactKey / content-entity id, never the generation-bearing FactID. The
// fields are joined with the key separator so the same logical routing row hashes
// to the same identity across re-materializations.
func serviceIncidentEvidenceIdentity(record ServiceIncidentRecord) string {
	parts := []string{
		strings.TrimSpace(record.Provider),
		strings.TrimSpace(record.ProviderIncidentID),
		strings.TrimSpace(record.Slot),
		strings.TrimSpace(record.EvidenceKind),
		strings.TrimSpace(record.EvidenceID),
	}
	return strings.Join(parts, ":")
}

// serviceIncidentEvidencePayload captures the stable, observable fields of an
// incident routing record whose change should flip the row to updated. It
// deliberately excludes the envelope fact_id, any generation id, and the applied
// fact's state_generation_id (per-run metadata) so an unchanged routing row across
// re-materializations hashes identically and classifies as unchanged.
func serviceIncidentEvidencePayload(record ServiceIncidentRecord) map[string]any {
	return map[string]any{
		"provider":             strings.TrimSpace(record.Provider),
		"provider_incident_id": strings.TrimSpace(record.ProviderIncidentID),
		"slot":                 strings.TrimSpace(record.Slot),
		"evidence_kind":        strings.TrimSpace(record.EvidenceKind),
		"evidence_id":          strings.TrimSpace(record.EvidenceID),
		"truth_label":          strings.TrimSpace(record.TruthLabel),
		"provider_object_id":   strings.TrimSpace(record.ProviderObjectID),
		"declared_match_state": strings.TrimSpace(record.DeclaredMatchState),
		"redaction_state":      strings.TrimSpace(record.RedactionState),
	}
}

// incidentRecordHasDurableIdentity reports whether a record carries enough durable
// routing identity to be keyed: a provider, a provider incident id, a slot, an
// evidence kind, and a durable evidence id. A record missing any of these cannot
// produce a stable diff key and is dropped rather than keyed on an empty identity.
// This is the seam that rejects a record whose only available evidence id is the
// generation-bearing FactID: the loader must supply the durable StableFactKey, or
// the record is dropped.
func incidentRecordHasDurableIdentity(record ServiceIncidentRecord) bool {
	return strings.TrimSpace(record.Provider) != "" &&
		strings.TrimSpace(record.ProviderIncidentID) != "" &&
		strings.TrimSpace(record.Slot) != "" &&
		strings.TrimSpace(record.EvidenceKind) != "" &&
		strings.TrimSpace(record.EvidenceID) != ""
}

// buildServiceIncidentEvidence converts the service's incident routing records
// into deterministic, deduped incidents evidence rows. Records without a durable
// identity are dropped; records are deduped by stable identity (a later
// non-retired entry for the same identity wins, and an explicit retirement always
// wins) and ordered by identity so the generation fingerprint is
// input-order-independent.
func buildServiceIncidentEvidence(records []ServiceIncidentRecord) []ServiceIncidentEvidence {
	deduped := make(map[string]ServiceIncidentEvidence, len(records))
	for _, record := range records {
		if !incidentRecordHasDurableIdentity(record) {
			continue
		}
		identity := serviceIncidentEvidenceIdentity(record)
		existing, ok := deduped[identity]
		if ok && existing.Retired && !record.Retired {
			continue
		}
		deduped[identity] = ServiceIncidentEvidence{
			Identity: identity,
			Payload:  serviceIncidentEvidencePayload(record),
			Retired:  record.Retired,
		}
	}
	rows := make([]ServiceIncidentEvidence, 0, len(deduped))
	for _, row := range deduped {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Identity < rows[j].Identity
	})
	return rows
}

// addServiceIncidentEvidence normalizes incidents evidence into the shared
// snapshot row map keyed by service_evidence_key. It mirrors
// addServiceDocumentationEvidence: a later non-retired entry for the same identity
// wins, and an explicit retirement always wins so a re-materialization cannot
// resurrect a removed incident routing row.
func addServiceIncidentEvidence(
	deduped map[string]serviceEvidenceRow,
	serviceID string,
	evidence []ServiceIncidentEvidence,
) {
	for _, item := range evidence {
		identity := strings.TrimSpace(item.Identity)
		if identity == "" {
			continue
		}
		key := ServiceIncidentEvidenceKey(serviceID, identity)
		payload := item.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		existing, ok := deduped[key]
		if ok && existing.tombstone && !item.Retired {
			continue
		}
		deduped[key] = serviceEvidenceRow{
			family:      ServiceEvidenceFamilyIncidents,
			evidenceKey: key,
			payloadHash: ServiceEvidencePayloadHash(payload),
			tombstone:   item.Retired,
			payload:     payload,
		}
	}
}

// attachServiceIncidentEvidence loads the incident routing evidence that routes to
// the correlated services and attaches the incidents evidence family to the
// matching per-service writes. It is a no-op when no loader is wired or there are
// no writes, so the incidents family is purely additive. The records are loaded
// once for all services in a single bounded call keyed by Eshu catalog service id
// (not repository id, because incident routing links to a service through the
// provider incident anchor, and not the PagerDuty provider service id, which the
// loader is responsible for resolving to the catalog service id); a service with
// no routing evidence simply carries no incidents rows.
func (h ServiceCatalogCorrelationHandler) attachServiceIncidentEvidence(
	ctx context.Context,
	writes []ServiceMaterializationWrite,
) error {
	if h.IncidentEvidenceLoader == nil || len(writes) == 0 {
		return nil
	}
	serviceIDs := distinctMaterializationServiceIDs(writes)
	if len(serviceIDs) == 0 {
		return nil
	}
	recordsByService, err := h.IncidentEvidenceLoader.GetIncidentEvidenceForServices(ctx, serviceIDs)
	if err != nil {
		return fmt.Errorf("load service incident evidence: %w", err)
	}
	for i := range writes {
		serviceID := strings.TrimSpace(writes[i].ServiceID)
		if serviceID == "" {
			continue
		}
		writes[i].Incidents = buildServiceIncidentEvidence(recordsByService[serviceID])
	}
	return nil
}
