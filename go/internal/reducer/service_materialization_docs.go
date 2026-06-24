// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ServiceScopedDocumentationEvidenceLoader returns the current documentation
// evidence for one or more correlated services, regardless of which fact
// generation produced it. It is the docs-family analogue of
// RepositoryScopedRuntimeInstanceLoader, but keyed by service id rather than
// repository id: documentation facts link to a service through their target
// refs (candidate_refs / evidence_refs / linked_entities), not through a
// repository generation, so the durable scope of a docs evidence row is the
// service it references. The returned rows must carry only durable external
// identity (source_system, source_record_id, document_id), never a fact_id or
// generation id, so the docs service_evidence_key stays generation-stable.
type ServiceScopedDocumentationEvidenceLoader interface {
	GetDocumentationEvidenceForServices(
		ctx context.Context,
		serviceIDs []string,
	) (map[string][]ServiceDocumentationRecord, error)
}

// Docs evidence family (#1988, part of #1943/#1797). A service's documentation
// evidence is the set of documentation facts that reference the service:
// documentation entity mentions, documentation claim candidates, and semantic
// documentation observations. Each fact becomes one generation-stable
// service_evidence_snapshots row in the docs family, reusing the Stage-1
// lineage, payload-hash, and tombstone machinery verbatim.
//
// Stable identity (verified generation-independent). A documentation evidence
// row is keyed by its durable external identity:
//
//	docs:<service_id>:<source_system>:<source_record_id>:<document_id>
//
// The read model that surfaces documentation truth
// (query.documentationTargetFacts over fact_records for the
// documentation.entity_mention, documentation.claim_candidate, and
// semantic_documentation_observation fact kinds) already filters
// is_tombstone = FALSE and the active generation, and the durable identity it
// exposes is the external {source_system, source_record_id, document_id} of the
// fact plus the service the fact references. None of these embeds a fact_id or a
// generation id: source_system and source_record_id are fact_records columns
// (source_record_id is the durable section/document ref the collector emits, for
// example the documentation section id, not a per-run identifier), and
// document_id is a durable payload field. The fact_id digests the generation id
// (see the documentation collectors, whose FactID embeds generation_id), so it
// is generation-bearing and is never keyed on; the read model treats
// generation_id as a scope constraint only. The same logical documentation fact
// therefore keeps the same key across re-materializations and the FULL OUTER
// JOIN diff can classify updated vs unchanged. This is the
// identity-vs-generation distinction from design #1231.

// ServiceDocumentationRecord is one durable documentation fact that references a
// service, as read from the documentation fact read path. The reducer converts
// it into a generation-stable docs evidence row. Only the durable external
// identity is keyed; observable fields are hashed into the payload, and the
// generation lives in the row, never in the key.
type ServiceDocumentationRecord struct {
	// SourceSystem is the durable documentation source class (for example
	// "confluence" or "git_markdown"). It is part of the stable identity.
	SourceSystem string
	// SourceRecordID is the durable external record reference the collector
	// emitted for the fact (for example the documentation section id). It is part
	// of the stable identity and carries no fact_id or generation id.
	SourceRecordID string
	// DocumentID is the durable external document id the fact belongs to. It is
	// part of the stable identity.
	DocumentID string
	// FactKind is the documentation fact kind (entity mention, claim candidate, or
	// semantic observation). It is observable and hashed into the payload so a fact
	// kind change for the same record flips the row to updated.
	FactKind string
	// SourceURI is the documentation source uri (observable, hashed into the
	// payload). It is not part of the identity.
	SourceURI string
	// ObservationHash is a content fingerprint the source carries (observable,
	// hashed into the payload so a content change flips the row to updated). It is
	// optional and not part of the identity.
	ObservationHash string
	// SourceACLState is the bounded source-ACL-state observation the collector
	// emitted on this documentation fact (facts.DocumentationACLSummary.
	// SourceACLState), using the allowed|denied|partial|missing|stale vocabulary.
	// It is an access-posture axis distinct from freshness (#2138): the reducer
	// carries it verbatim into the read model and never folds it into freshness,
	// upgrades a non-allowed observation to allowed, or synthesizes a value the
	// collector did not assert. It is optional and not part of the identity; an
	// empty or non-bounded value means "no ACL claim" and is omitted from the
	// projected payload (fail closed; default-when-unknown is reserved for the
	// query surface and security review, #2164).
	SourceACLState string
	// Retired records a documentation fact that was explicitly removed in this
	// re-materialization. It is written as a tombstone row.
	Retired bool
}

// ServiceDocumentationEvidence is one generation-stable docs row for a service.
// Identity is the durable external-identity digest; the generation lives in the
// row, never in the key. A retired record carries Retired=true so the delta
// classifies it explicitly rather than letting it vanish into unchanged.
type ServiceDocumentationEvidence struct {
	// Identity is the generation-independent per-record identity
	// (see serviceDocumentationEvidenceIdentity). It is combined with the service
	// id to form the service_evidence_key.
	Identity string
	// Payload is the durable evidence body whose hash drives updated-vs-unchanged
	// classification. It captures the record's stable, observable fields.
	Payload map[string]any
	// Retired records a record that was explicitly removed in this
	// re-materialization. It is written as a tombstone row.
	Retired bool
}

// ServiceDocumentationEvidenceKey returns the generation-independent identity for
// one docs row: docs:<service_id>:<identity>. The identity is the durable
// external-identity tuple (source_system:source_record_id:document_id); the
// generation is stored in a column, never embedded here.
func ServiceDocumentationEvidenceKey(serviceID, identity string) string {
	return strings.Join([]string{
		ServiceEvidenceFamilyDocs,
		strings.TrimSpace(serviceID),
		strings.TrimSpace(identity),
	}, ":")
}

// serviceDocumentationEvidenceIdentity derives the generation-independent
// identity for one documentation record from its durable external identity. It
// must not include the fact_id or any generation id. The fields are joined with
// the key separator so the same logical record hashes to the same identity
// across re-materializations.
func serviceDocumentationEvidenceIdentity(record ServiceDocumentationRecord) string {
	parts := []string{
		strings.TrimSpace(record.SourceSystem),
		strings.TrimSpace(record.SourceRecordID),
		strings.TrimSpace(record.DocumentID),
	}
	return strings.Join(parts, ":")
}

// serviceDocumentationEvidencePayload captures the stable, observable fields of a
// documentation record whose change should flip the row to updated. It
// deliberately excludes the fact_id and any generation id so an unchanged record
// across re-materializations hashes identically and classifies as unchanged.
//
// source_acl_state is projected as a distinct observable field alongside (never
// folded into) the content fields: it is added only when the collector observed
// a bounded ACL state (see projectedSourceACLState), so an unobserved or
// non-bounded value leaves the field absent ("no ACL claim", fail closed) and
// cannot churn the row hash. A changed bounded ACL state flips the row to
// updated because it changes the hashed payload.
func serviceDocumentationEvidencePayload(record ServiceDocumentationRecord) map[string]any {
	payload := map[string]any{
		"source_system":    strings.TrimSpace(record.SourceSystem),
		"source_record_id": strings.TrimSpace(record.SourceRecordID),
		"document_id":      strings.TrimSpace(record.DocumentID),
		"fact_kind":        strings.TrimSpace(record.FactKind),
		"source_uri":       strings.TrimSpace(record.SourceURI),
		"observation_hash": strings.TrimSpace(record.ObservationHash),
	}
	if state := projectedSourceACLState(record.SourceACLState); state != "" {
		payload["source_acl_state"] = state
	}
	return payload
}

// projectedSourceACLState returns the collector-observed source_acl_state to
// project verbatim, or the empty string when there is no ACL claim to carry. It
// fails closed: only a bounded allowed|denied|partial|missing|stale value is
// projected, so an unobserved (empty) or non-bounded value is dropped rather
// than surfaced as an authoritative ACL claim. The reducer never upgrades a
// non-allowed observation to allowed or invents a default the collector did not
// assert (correlation-truth); choosing a conservative default for unobserved
// sources is a disclosure decision reserved for the query surface and security
// review (#2164).
func projectedSourceACLState(value string) string {
	value = strings.TrimSpace(value)
	if !facts.ValidSourceACLState(value) {
		return ""
	}
	return value
}

// documentationRecordHasDurableIdentity reports whether a record carries enough
// durable external identity to be keyed: a source system, a source record id,
// and a document id. A record missing any of these cannot produce a stable diff
// key and is dropped rather than keyed on an empty identity.
func documentationRecordHasDurableIdentity(record ServiceDocumentationRecord) bool {
	return strings.TrimSpace(record.SourceSystem) != "" &&
		strings.TrimSpace(record.SourceRecordID) != "" &&
		strings.TrimSpace(record.DocumentID) != ""
}

// buildServiceDocumentationEvidence converts the service's documentation records
// into deterministic, deduped docs evidence rows. Records without a durable
// identity are dropped; records are deduped by stable identity (a later
// non-retired entry for the same identity wins, and an explicit retirement
// always wins) and ordered by identity so the generation fingerprint is
// input-order-independent.
func buildServiceDocumentationEvidence(records []ServiceDocumentationRecord) []ServiceDocumentationEvidence {
	deduped := make(map[string]ServiceDocumentationEvidence, len(records))
	for _, record := range records {
		if !documentationRecordHasDurableIdentity(record) {
			continue
		}
		identity := serviceDocumentationEvidenceIdentity(record)
		existing, ok := deduped[identity]
		if ok && existing.Retired && !record.Retired {
			continue
		}
		deduped[identity] = ServiceDocumentationEvidence{
			Identity: identity,
			Payload:  serviceDocumentationEvidencePayload(record),
			Retired:  record.Retired,
		}
	}
	rows := make([]ServiceDocumentationEvidence, 0, len(deduped))
	for _, row := range deduped {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Identity < rows[j].Identity
	})
	return rows
}

// addServiceDocumentationEvidence normalizes docs evidence into the shared
// snapshot row map keyed by service_evidence_key. It mirrors
// addServiceRuntimeEvidence: a later non-retired entry for the same identity
// wins, and an explicit retirement always wins so a re-materialization cannot
// resurrect a removed documentation record.
func addServiceDocumentationEvidence(
	deduped map[string]serviceEvidenceRow,
	serviceID string,
	evidence []ServiceDocumentationEvidence,
) {
	for _, item := range evidence {
		identity := strings.TrimSpace(item.Identity)
		if identity == "" {
			continue
		}
		key := ServiceDocumentationEvidenceKey(serviceID, identity)
		payload := item.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		existing, ok := deduped[key]
		if ok && existing.tombstone && !item.Retired {
			continue
		}
		deduped[key] = serviceEvidenceRow{
			family:      ServiceEvidenceFamilyDocs,
			evidenceKey: key,
			payloadHash: ServiceEvidencePayloadHash(payload),
			tombstone:   item.Retired,
			payload:     payload,
		}
	}
}

// attachServiceDocumentationEvidence loads the documentation facts that reference
// the correlated services and attaches the docs evidence family to the matching
// per-service writes. It is a no-op when no loader is wired or there are no
// writes, so the docs family is purely additive. The facts are loaded once for
// all services in a single bounded call keyed by service id (not repository id,
// because documentation linkage is to the service through the fact's target
// refs); a service with no referencing documentation facts simply carries no
// docs rows.
func (h ServiceCatalogCorrelationHandler) attachServiceDocumentationEvidence(
	ctx context.Context,
	writes []ServiceMaterializationWrite,
) error {
	if h.DocumentationEvidenceLoader == nil || len(writes) == 0 {
		return nil
	}
	serviceIDs := distinctMaterializationServiceIDs(writes)
	if len(serviceIDs) == 0 {
		return nil
	}
	recordsByService, err := h.DocumentationEvidenceLoader.GetDocumentationEvidenceForServices(ctx, serviceIDs)
	if err != nil {
		return fmt.Errorf("load service documentation evidence: %w", err)
	}
	for i := range writes {
		serviceID := strings.TrimSpace(writes[i].ServiceID)
		if serviceID == "" {
			continue
		}
		writes[i].Docs = buildServiceDocumentationEvidence(recordsByService[serviceID])
	}
	return nil
}

// distinctMaterializationServiceIDs returns the deterministic, deduped set of
// service ids being materialized, so documentation evidence is loaded in one
// bounded call.
func distinctMaterializationServiceIDs(writes []ServiceMaterializationWrite) []string {
	seen := map[string]struct{}{}
	serviceIDs := make([]string, 0, len(writes))
	for _, write := range writes {
		serviceID := strings.TrimSpace(write.ServiceID)
		if serviceID == "" {
			continue
		}
		if _, ok := seen[serviceID]; ok {
			continue
		}
		seen[serviceID] = struct{}{}
		serviceIDs = append(serviceIDs, serviceID)
	}
	sort.Strings(serviceIDs)
	return serviceIDs
}
