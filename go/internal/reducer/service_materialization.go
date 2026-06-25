// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"crypto/md5" // #nosec G501 -- non-cryptographic payload fingerprint for changed-since hash (#1799), not a security primitive //nolint:gosec
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Service materialization lineage is the additive foundation for service-scope
// changed-since deltas (#1943, parent #1797). It mirrors the repository-scope
// (ingestion_scopes -> scope_generations) lineage that #1799 diffs, but keyed by
// service_id instead of an ingestion scope.
//
// The existing reducer_service_catalog_correlation fact and its
// generation-embedding stable_fact_key are NOT changed. This lineage is a
// parallel, durable snapshot the reducer commits on each re-materialization so a
// prior service generation can be diffed against the current active one.
const (
	// ServiceEvidenceFamilyOwnership is the Stage-1 evidence family: the owner-ref
	// truth for a service. The remaining families (runtime, dependencies, docs,
	// incidents, vulnerabilities) reuse this lineage in follow-up work.
	ServiceEvidenceFamilyOwnership = "ownership"

	// ServiceEvidenceFamilyDeployment is the deployment evidence family (#1985):
	// one generation-stable row per resolved deployment relationship that involves
	// the service's repository. It reuses the same lineage, payload-hash, and
	// tombstone machinery as ownership; only the row identity and source loader
	// differ.
	ServiceEvidenceFamilyDeployment = "deployment"

	// ServiceEvidenceFamilyRuntime is the runtime evidence family (#1986): one
	// generation-stable row per materialized runtime instance of the service's
	// workload, keyed by the durable platform/environment/workload identity. It
	// reuses the same lineage, payload-hash, and tombstone machinery as ownership
	// and deployment; only the row identity and source loader differ.
	ServiceEvidenceFamilyRuntime = "runtime"

	// ServiceEvidenceFamilyDependencies is the dependencies evidence family
	// (#1987): one generation-stable row per resolved dependency relationship that
	// involves the service's repository (DEPENDS_ON / USES_MODULE /
	// READS_CONFIG_FROM), keyed by the relationship's generation-independent natural
	// key. It shares deployment's resolved_relationships source verbatim and reuses
	// the same lineage, payload-hash, and tombstone machinery; only the admitted
	// relationship types and the evidence_family label differ.
	ServiceEvidenceFamilyDependencies = "dependencies"

	// ServiceEvidenceFamilyDocs is the docs evidence family (#1988): one
	// generation-stable row per documentation fact that references the service
	// (documentation entity mention, documentation claim candidate, or semantic
	// documentation observation), keyed by the fact's durable external identity
	// (source_system, source_record_id, document_id). It is sourced from
	// fact_records, not the resolved relationships the deployment/dependencies
	// families share, and reuses the same lineage, payload-hash, and tombstone
	// machinery; only the row identity, source loader, and evidence_family label
	// differ.
	ServiceEvidenceFamilyDocs = "docs"

	// ServiceEvidenceFamilyIncidents is the incidents evidence family (#1989): one
	// generation-stable row per exact PagerDuty incident-routing evidence row that
	// routes to the service (one per routing slot: intended / applied / live),
	// keyed by the row's durable routing identity (provider, provider_incident_id,
	// slot, evidence_kind, and the generation-INDEPENDENT evidence id — the source
	// fact's StableFactKey for applied/live or the durable content-entity id for
	// the intended slot, never the generation-bearing FactID). It is sourced from
	// incident routing evidence, not the resolved relationships or fact_records the
	// prior families use, and reuses the same lineage, payload-hash, and tombstone
	// machinery; only the row identity, source loader, and evidence_family label
	// differ.
	ServiceEvidenceFamilyIncidents = "incidents"

	// ServiceEvidenceFamilyVulnerabilities is the vulnerabilities evidence family
	// (#1990): one generation-stable row per (supply-chain advisory, affected
	// package) pair that affects a package the service's repositories depend on,
	// keyed by the advisory's durable canonical id and the affected package
	// ecosystem/name (never an evidence fact_id or per-scan generation id). It is
	// sourced from the resolved supply-chain advisory evidence and reuses the same
	// lineage, payload-hash, and tombstone machinery as the prior families; only
	// the row identity, source loader, and evidence_family label differ.
	ServiceEvidenceFamilyVulnerabilities = "vulnerabilities"

	// ServiceMaterializationStatusPending marks a generation that has been written
	// but not yet promoted to active. The writer inserts a new generation as
	// pending so it never collides with the single-active-per-service partial
	// unique index before the prior active generation is superseded.
	ServiceMaterializationStatusPending = "pending"
	// ServiceMaterializationStatusActive marks the current generation whose
	// snapshot rows back the current service read. Exactly one active generation
	// exists per service_id, enforced by a partial unique index.
	ServiceMaterializationStatusActive = "active"
	// ServiceMaterializationStatusSuperseded marks a generation replaced by a
	// newer active generation for the same service_id.
	ServiceMaterializationStatusSuperseded = "superseded"
)

// ServiceOwnershipEvidence is one generation-stable ownership row for a service.
// The identity is the owner reference; the generation lives in the row, never in
// the key, so the same owner keeps the same service_evidence_key across
// generations (the identity-vs-generation distinction from design #1231). A
// retired owner carries Retired=true so the delta classifies it explicitly
// rather than letting it vanish into unchanged.
type ServiceOwnershipEvidence struct {
	OwnerRef string
	// Payload is the durable evidence body whose hash drives updated-vs-unchanged
	// classification. It is hashed with md5(payload json) exactly as #1799 hashes
	// fact payloads.
	Payload map[string]any
	// Retired records an owner that was explicitly removed in this
	// re-materialization. It is written as a tombstone row so the delta reports it
	// as retired instead of superseded.
	Retired bool
}

// ServiceMaterializationWrite carries one service's re-materialized evidence set
// for durable lineage publication. ServiceID is the conflict key: all generation
// commits for one service serialize on the single-active-per-service constraint.
// Every family the writer knows lands in the same generation, so a service
// generation is the snapshot of all of the service's evidence at materialization
// time; a change in any family flips the generation.
type ServiceMaterializationWrite struct {
	IntentID    string
	ServiceID   string
	TriggerKind string
	Ownership   []ServiceOwnershipEvidence
	// Deployment carries the service's resolved deployment relationships (#1985).
	Deployment []ServiceDeploymentEvidence
	// Runtime carries the service's materialized runtime instances (#1986).
	Runtime []ServiceRuntimeEvidence
	// Dependencies carries the service's resolved dependency relationships (#1987).
	Dependencies []ServiceDependencyEvidence
	// Docs carries the service's referencing documentation facts (#1988).
	Docs []ServiceDocumentationEvidence
	// Incidents carries the service's exact PagerDuty incident-routing evidence
	// rows (#1989).
	Incidents []ServiceIncidentEvidence
	// Vulnerabilities carries the service's supply-chain advisory evidence: one
	// row per (advisory, affected package) pair (#1990).
	Vulnerabilities []ServiceVulnerabilityEvidence
}

// ServiceMaterializationWriteResult summarizes one lineage commit. GenerationID
// is the deterministic id derived from the evidence set; Committed is false when
// an identical generation already existed (idempotent no-op re-materialization).
type ServiceMaterializationWriteResult struct {
	GenerationID  string
	Committed     bool
	EvidenceRows  int
	SupersededIDs []string
}

// ServiceMaterializationWriter persists service-scope generation lineage and the
// generation-stable per-evidence snapshot rows the changed-since delta diffs.
type ServiceMaterializationWriter interface {
	WriteServiceMaterialization(context.Context, ServiceMaterializationWrite) (ServiceMaterializationWriteResult, error)
}

// ServiceOwnershipEvidenceKey returns the generation-independent identity for one
// ownership row: ownership:<service_id>:<owner_ref>. The generation is stored in
// a column, never embedded here, so the same owner keeps a stable key across
// re-materializations and the FULL OUTER JOIN diff can match updated/unchanged.
func ServiceOwnershipEvidenceKey(serviceID, ownerRef string) string {
	return strings.Join([]string{
		ServiceEvidenceFamilyOwnership,
		strings.TrimSpace(serviceID),
		strings.TrimSpace(ownerRef),
	}, ":")
}

// ServiceEvidencePayloadHash returns the md5 hex digest of the canonical JSON
// encoding of an evidence payload. It matches the repository-scope changed-since
// contract, which detects updated-vs-unchanged with md5(payload::text). A nil or
// empty payload hashes deterministically so an empty row never looks updated.
func ServiceEvidencePayloadHash(payload map[string]any) string {
	encoded, err := json.Marshal(canonicalizeEvidencePayload(payload))
	if err != nil {
		// Marshalling a map[string]any of JSON-safe values cannot fail; fall back
		// to a stable sentinel rather than panicking in the reducer write path.
		encoded = []byte("{}")
	}
	sum := md5.Sum(encoded) // #nosec G401 -- non-cryptographic payload fingerprint, not a security primitive //nolint:gosec
	return hex.EncodeToString(sum[:])
}

func canonicalizeEvidencePayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return map[string]any{}
	}
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	canonical := make(map[string]any, len(payload))
	for _, key := range keys {
		canonical[key] = payload[key]
	}
	return canonical
}

// serviceMaterializationGenerationID derives the deterministic generation id for
// one materialization from the service id and the full ordered evidence
// fingerprint across every family. An identical evidence set produces an
// identical id, so a repeat re-materialization upserts the same generation row
// (ON CONFLICT DO NOTHING) and is a true no-op: no new generation, no snapshot
// churn, no false delta. A change in any family (ownership, deployment, runtime,
// dependencies, docs, incidents, or vulnerabilities) changes the fingerprint and
// flips the generation.
func serviceMaterializationGenerationID(write ServiceMaterializationWrite) string {
	rows := normalizeServiceEvidence(write)
	fingerprint := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		fingerprint = append(fingerprint, map[string]any{
			"family":    row.family,
			"key":       row.evidenceKey,
			"hash":      row.payloadHash,
			"tombstone": row.tombstone,
		})
	}
	return "service-gen:" + facts.StableID("service_materialization_generation", map[string]any{
		"service_id":   strings.TrimSpace(write.ServiceID),
		"evidence_set": fingerprint,
	})
}

// serviceEvidenceRow is the normalized, deterministic snapshot row shape the
// writer persists for any evidence family. Rows are sorted by (family, key) so
// the generation fingerprint and write order are stable regardless of input
// ordering. The family discriminator lets one writer commit every family into
// the same generation while the delta surface groups by it.
type serviceEvidenceRow struct {
	family      string
	evidenceKey string
	payloadHash string
	tombstone   bool
	payload     map[string]any
}

// normalizeServiceEvidence flattens every family on a write into one ordered,
// deduped snapshot row set. Ownership, deployment, runtime, dependencies, docs,
// incidents, and vulnerabilities share the same row shape, so the writer and
// generation fingerprint treat them uniformly.
func normalizeServiceEvidence(write ServiceMaterializationWrite) []serviceEvidenceRow {
	deduped := make(map[string]serviceEvidenceRow, len(write.Ownership)+len(write.Deployment)+len(write.Runtime)+len(write.Dependencies)+len(write.Docs)+len(write.Incidents)+len(write.Vulnerabilities))
	addServiceOwnershipEvidence(deduped, write.ServiceID, write.Ownership)
	addServiceDeploymentEvidence(deduped, write.ServiceID, write.Deployment)
	addServiceRuntimeEvidence(deduped, write.ServiceID, write.Runtime)
	addServiceDependencyEvidence(deduped, write.ServiceID, write.Dependencies)
	addServiceDocumentationEvidence(deduped, write.ServiceID, write.Docs)
	addServiceIncidentEvidence(deduped, write.ServiceID, write.Incidents)
	addServiceVulnerabilityEvidence(deduped, write.ServiceID, write.Vulnerabilities)
	rows := make([]serviceEvidenceRow, 0, len(deduped))
	for _, row := range deduped {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].family != rows[j].family {
			return rows[i].family < rows[j].family
		}
		return rows[i].evidenceKey < rows[j].evidenceKey
	})
	return rows
}

func addServiceOwnershipEvidence(
	deduped map[string]serviceEvidenceRow,
	serviceID string,
	evidence []ServiceOwnershipEvidence,
) {
	for _, item := range evidence {
		ownerRef := strings.TrimSpace(item.OwnerRef)
		if ownerRef == "" {
			continue
		}
		key := ServiceOwnershipEvidenceKey(serviceID, ownerRef)
		payload := item.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		// A later evidence entry for the same owner wins; an explicit retirement
		// always wins so a re-materialization cannot resurrect a removed owner.
		existing, ok := deduped[key]
		if ok && existing.tombstone && !item.Retired {
			continue
		}
		deduped[key] = serviceEvidenceRow{
			family:      ServiceEvidenceFamilyOwnership,
			evidenceKey: key,
			payloadHash: ServiceEvidencePayloadHash(payload),
			tombstone:   item.Retired,
			payload:     payload,
		}
	}
}

// buildServiceOwnershipMaterializations groups correlation decisions into one
// ownership materialization per service_id. Only decisions that carry both a
// service_id and an owner_ref contribute an ownership row; the evidence payload
// captures the owner-bearing fields so a change to ownership truth flips the row
// to updated, while an unchanged owner stays unchanged. Output is deterministic:
// services ordered by id, evidence ordered inside the writer.
func buildServiceOwnershipMaterializations(
	intentID string,
	decisions []ServiceCatalogCorrelationDecision,
) []ServiceMaterializationWrite {
	byService := map[string]map[string]ServiceOwnershipEvidence{}
	for _, decision := range decisions {
		serviceID := strings.TrimSpace(decision.ServiceID)
		ownerRef := strings.TrimSpace(decision.OwnerRef)
		if serviceID == "" || ownerRef == "" {
			continue
		}
		if _, ok := byService[serviceID]; !ok {
			byService[serviceID] = map[string]ServiceOwnershipEvidence{}
		}
		byService[serviceID][ownerRef] = ServiceOwnershipEvidence{
			OwnerRef: ownerRef,
			Payload:  ownershipEvidencePayload(decision),
		}
	}

	serviceIDs := make([]string, 0, len(byService))
	for serviceID := range byService {
		serviceIDs = append(serviceIDs, serviceID)
	}
	sort.Strings(serviceIDs)

	writes := make([]ServiceMaterializationWrite, 0, len(serviceIDs))
	for _, serviceID := range serviceIDs {
		owners := byService[serviceID]
		ownerRefs := make([]string, 0, len(owners))
		for ownerRef := range owners {
			ownerRefs = append(ownerRefs, ownerRef)
		}
		sort.Strings(ownerRefs)
		ownership := make([]ServiceOwnershipEvidence, 0, len(ownerRefs))
		for _, ownerRef := range ownerRefs {
			ownership = append(ownership, owners[ownerRef])
		}
		writes = append(writes, ServiceMaterializationWrite{
			IntentID:  intentID,
			ServiceID: serviceID,
			Ownership: ownership,
		})
	}
	return writes
}

// ownershipEvidencePayload captures the owner-bearing fields whose change should
// flip an ownership row to updated. It deliberately excludes the generation id
// and intent id so an identical owner across re-materializations hashes
// identically and classifies as unchanged.
func ownershipEvidencePayload(decision ServiceCatalogCorrelationDecision) map[string]any {
	return map[string]any{
		"owner_ref":  strings.TrimSpace(decision.OwnerRef),
		"provider":   strings.TrimSpace(decision.Provider),
		"entity_ref": strings.TrimSpace(decision.EntityRef),
		"lifecycle":  strings.TrimSpace(decision.Lifecycle),
		"tier":       strings.TrimSpace(decision.Tier),
	}
}

// validateServiceMaterializationWrite enforces the minimal contract before any
// durable write so a malformed intent fails fast instead of committing a
// half-formed generation.
func validateServiceMaterializationWrite(write ServiceMaterializationWrite) error {
	if strings.TrimSpace(write.ServiceID) == "" {
		return fmt.Errorf("service materialization write requires a service_id")
	}
	return nil
}
