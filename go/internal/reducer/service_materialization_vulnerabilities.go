// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ServiceVulnerabilityAdvisoryLoader returns the supply-chain advisory evidence
// that affects one or more repositories, regardless of which fact generation
// produced it. It is the vulnerabilities analogue of
// RepositoryScopedResolvedRelationshipLoader: keyed by canonical repository id,
// not service id, because a service is attributed an advisory ONLY through a real
// supply-chain impact finding on its repository
// (service -> repository -> supply_chain_impact_finding -> advisory). The handler
// resolves each service to its repository via serviceRepositoryIndex(decisions)
// and partitions the loaded advisories per service by repository id, mirroring how
// the deployment, dependencies, and runtime families source their evidence.
//
// CONTRACT (correlation truth). The map key MUST be the canonical repository id.
// The returned advisories are the materialized reducer_supply_chain_impact_finding
// rows for that repository: a service is attributed an advisory only when its
// repository carries a real, dependency-derived impact finding, never through a
// fuzzy advisory-to-service name match. The returned records MUST carry only
// durable external advisory/package identity (the advisory canonical id and the
// affected package ecosystem/name), never an evidence fact_id or generation id, so
// the vulnerabilities service_evidence_key stays generation-stable.
type ServiceVulnerabilityAdvisoryLoader interface {
	GetSupplyChainAdvisoriesForRepos(
		ctx context.Context,
		repoIDs []string,
	) (map[string][]ServiceVulnerabilityRecord, error)
}

// Vulnerabilities evidence family (#1990, part of #1943/#1797). A service's
// vulnerability evidence is the set of supply-chain advisories that affect the
// packages the service's repositories depend on: one row per (advisory, affected
// package) pair. Each row becomes one generation-stable service_evidence_snapshots
// row in the vulnerabilities family, reusing the Stage-1 lineage, payload-hash,
// and tombstone machinery verbatim.
//
// Stable identity (verified generation-independent). A vulnerability evidence row
// is keyed by its durable advisory + affected-package identity:
//
//	vulnerabilities:<service_id>:<canonical_id>:<package_ecosystem>:<package_name>
//
// The source read model is supply-chain advisory evidence (the reducer-resolved
// canonical advisory records joined to the service's affected packages). The
// advisory canonical id (for example GHSA-xxxx / CVE-xxxx) and the affected
// package (ecosystem + name) are durable external identities that survive across
// fact generations; advisory state changes on the provider's timeline, not a
// service generation. CRITICAL: the row MUST NOT key on an evidence fact_id or
// any per-scan generation id (those rotate per ingest and would reproduce the
// 100% false churn this design (#1943) warns about). Observable severity,
// exploit (KEV/EPSS), source-confidence, and freshness fields are hashed into the
// payload so a change flips the row to updated; the generation lives in the row,
// never in the key (the identity-vs-generation distinction from design #1231).

// ServiceVulnerabilityRecord is one durable advisory-affects-package evidence row
// for a service, as read from the supply-chain advisory read path. The reducer
// converts it into a generation-stable vulnerabilities evidence row. Only the
// durable advisory/package identity is keyed; observable fields are hashed into
// the payload, and the generation lives in the row, never in the key.
type ServiceVulnerabilityRecord struct {
	// CanonicalID is the durable canonical advisory id (for example a GHSA or CVE
	// id) the supply-chain reducer assigns. It is part of the stable identity.
	CanonicalID string
	// PackageEcosystem is the durable ecosystem of the affected package (for
	// example "npm", "pypi", "go"). It is part of the stable identity.
	PackageEcosystem string
	// PackageName is the durable name of the affected package that links the
	// advisory to the service's dependency. It is part of the stable identity.
	PackageName string
	// PrimaryAdvisoryID is the human-facing advisory id (CVE or GHSA) for the
	// payload (observable, hashed). It is not part of the identity, which uses the
	// canonical id so source-id churn does not flip the key.
	PrimaryAdvisoryID string
	// Severity is the advisory severity label (observable, hashed into the
	// payload so a severity change flips the row to updated).
	Severity string
	// KEVListed records whether the advisory is on the CISA Known-Exploited
	// catalog (observable, hashed into the payload).
	KEVListed bool
	// EPSSScore is the advisory's EPSS probability as a stable string (observable,
	// hashed into the payload). It is optional and not part of the identity.
	EPSSScore string
	// SourceConfidence is the resolved advisory source confidence (observable,
	// hashed into the payload). It is optional and not part of the identity.
	SourceConfidence string
	// SourceFreshness is the advisory source freshness label (observable, hashed
	// into the payload). It is optional and not part of the identity.
	SourceFreshness string
	// Retired records an advisory-package row that was explicitly removed in this
	// re-materialization. It is written as a tombstone row.
	Retired bool
}

// ServiceVulnerabilityEvidence is one generation-stable vulnerabilities row for a
// service. Identity is the durable advisory/package tuple; the generation lives
// in the row, never in the key. A retired record carries Retired=true so the
// delta classifies it explicitly rather than letting it vanish into unchanged.
type ServiceVulnerabilityEvidence struct {
	// Identity is the generation-independent per-row identity
	// (see serviceVulnerabilityEvidenceIdentity). It is combined with the service
	// id to form the service_evidence_key.
	Identity string
	// Payload is the durable evidence body whose hash drives updated-vs-unchanged
	// classification. It captures the row's stable, observable fields.
	Payload map[string]any
	// Retired records a record that was explicitly removed in this
	// re-materialization. It is written as a tombstone row.
	Retired bool
}

// ServiceVulnerabilityEvidenceKey returns the generation-independent identity for
// one vulnerabilities row: vulnerabilities:<service_id>:<identity>. The identity
// is the durable advisory/package tuple (canonical_id:ecosystem:name); the
// generation is stored in a column, never embedded here.
func ServiceVulnerabilityEvidenceKey(serviceID, identity string) string {
	return strings.Join([]string{
		ServiceEvidenceFamilyVulnerabilities,
		strings.TrimSpace(serviceID),
		strings.TrimSpace(identity),
	}, ":")
}

// serviceVulnerabilityEvidenceIdentity derives the generation-independent
// identity for one advisory-affects-package record from its durable advisory and
// package fields. It MUST NOT include an evidence fact_id or any generation/scan
// id; the same logical advisory-package pair hashes to the same identity across
// re-materializations so the FULL OUTER JOIN diff can match updated/unchanged.
func serviceVulnerabilityEvidenceIdentity(record ServiceVulnerabilityRecord) string {
	parts := []string{
		strings.TrimSpace(record.CanonicalID),
		strings.TrimSpace(record.PackageEcosystem),
		strings.TrimSpace(record.PackageName),
	}
	return strings.Join(parts, ":")
}

// serviceVulnerabilityEvidencePayload captures the stable, observable fields of an
// advisory-affects-package record whose change should flip the row to updated. It
// deliberately excludes any evidence fact_id and generation/scan id so an
// unchanged advisory across re-materializations hashes identically and classifies
// as unchanged.
func serviceVulnerabilityEvidencePayload(record ServiceVulnerabilityRecord) map[string]any {
	return map[string]any{
		"canonical_id":        strings.TrimSpace(record.CanonicalID),
		"package_ecosystem":   strings.TrimSpace(record.PackageEcosystem),
		"package_name":        strings.TrimSpace(record.PackageName),
		"primary_advisory_id": strings.TrimSpace(record.PrimaryAdvisoryID),
		"severity":            strings.TrimSpace(record.Severity),
		"kev_listed":          record.KEVListed,
		"epss_score":          strings.TrimSpace(record.EPSSScore),
		"source_confidence":   strings.TrimSpace(record.SourceConfidence),
		"source_freshness":    strings.TrimSpace(record.SourceFreshness),
	}
}

// vulnerabilityRecordHasDurableIdentity reports whether a record carries enough
// durable identity to be keyed: a canonical advisory id, an affected package
// ecosystem, and an affected package name. A record missing any of these cannot
// produce a stable diff key and is dropped rather than keyed on an empty
// identity. This is the seam that rejects a record whose only available identity
// is a per-scan evidence id: the loader must supply the durable canonical id and
// affected package, or the record is dropped.
func vulnerabilityRecordHasDurableIdentity(record ServiceVulnerabilityRecord) bool {
	return strings.TrimSpace(record.CanonicalID) != "" &&
		strings.TrimSpace(record.PackageEcosystem) != "" &&
		strings.TrimSpace(record.PackageName) != ""
}

// buildServiceVulnerabilityEvidence converts the service's advisory records into
// deterministic, deduped vulnerabilities evidence rows. Records without a durable
// identity are dropped; records are deduped by stable identity (a later
// non-retired entry for the same identity wins, and an explicit retirement always
// wins) and ordered by identity so the generation fingerprint is
// input-order-independent.
func buildServiceVulnerabilityEvidence(records []ServiceVulnerabilityRecord) []ServiceVulnerabilityEvidence {
	deduped := make(map[string]ServiceVulnerabilityEvidence, len(records))
	for _, record := range records {
		if !vulnerabilityRecordHasDurableIdentity(record) {
			continue
		}
		identity := serviceVulnerabilityEvidenceIdentity(record)
		existing, ok := deduped[identity]
		if ok && existing.Retired && !record.Retired {
			continue
		}
		deduped[identity] = ServiceVulnerabilityEvidence{
			Identity: identity,
			Payload:  serviceVulnerabilityEvidencePayload(record),
			Retired:  record.Retired,
		}
	}
	rows := make([]ServiceVulnerabilityEvidence, 0, len(deduped))
	for _, row := range deduped {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Identity < rows[j].Identity
	})
	return rows
}

// addServiceVulnerabilityEvidence normalizes vulnerabilities evidence into the
// shared snapshot row map keyed by service_evidence_key. It mirrors
// addServiceIncidentEvidence: a later non-retired entry for the same identity
// wins, and an explicit retirement always wins so a re-materialization cannot
// resurrect a removed advisory-package row.
func addServiceVulnerabilityEvidence(
	deduped map[string]serviceEvidenceRow,
	serviceID string,
	evidence []ServiceVulnerabilityEvidence,
) {
	for _, item := range evidence {
		identity := strings.TrimSpace(item.Identity)
		if identity == "" {
			continue
		}
		key := ServiceVulnerabilityEvidenceKey(serviceID, identity)
		payload := item.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		existing, ok := deduped[key]
		if ok && existing.tombstone && !item.Retired {
			continue
		}
		deduped[key] = serviceEvidenceRow{
			family:      ServiceEvidenceFamilyVulnerabilities,
			evidenceKey: key,
			payloadHash: ServiceEvidencePayloadHash(payload),
			tombstone:   item.Retired,
			payload:     payload,
		}
	}
}

// attachServiceVulnerabilityEvidence loads the supply-chain advisory evidence that
// affects the correlated services' repositories and attaches the vulnerabilities
// evidence family to the matching per-service writes. It is a no-op when no loader
// is wired or no decision carries a repository, so the vulnerabilities family is
// purely additive. The advisories are loaded once for all repositories in a single
// bounded call keyed by repository id, then partitioned per service by repository
// id, mirroring attachServiceRelationshipEvidence and attachServiceRuntimeEvidence;
// a service whose repository has no supply-chain impact finding simply carries no
// vulnerabilities rows.
//
// CORRELATION TRUTH. A service is attributed an advisory only when its repository
// has a real reducer_supply_chain_impact_finding (a dependency-derived impact),
// never through a fuzzy advisory-to-service name match. The repository binding is
// the durable serviceRepositoryIndex(decisions) join the prior families already
// use, so the linkage carries the same correlation-truth guarantee.
func (h ServiceCatalogCorrelationHandler) attachServiceVulnerabilityEvidence(
	ctx context.Context,
	writes []ServiceMaterializationWrite,
	decisions []ServiceCatalogCorrelationDecision,
) error {
	if h.VulnerabilityEvidenceLoader == nil || len(writes) == 0 {
		return nil
	}
	repoByService := serviceRepositoryIndex(decisions)
	repoIDs := distinctServiceRepositoryIDs(writes, repoByService)
	if len(repoIDs) == 0 {
		return nil
	}
	advisoriesByRepo, err := h.VulnerabilityEvidenceLoader.GetSupplyChainAdvisoriesForRepos(ctx, repoIDs)
	if err != nil {
		return fmt.Errorf("load service vulnerability evidence: %w", err)
	}
	for i := range writes {
		repoID := repoByService[writes[i].ServiceID]
		if repoID == "" {
			continue
		}
		writes[i].Vulnerabilities = buildServiceVulnerabilityEvidence(advisoriesByRepo[repoID])
	}
	return nil
}
