// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

const (
	sbomAttestationAttachmentsCapability       = "supply_chain.sbom_attestation_attachments.list"
	vulnerabilityScannerReadContractCapability = "supply_chain.vulnerability_scanner.contract.read"
	supplyChainImpactFindingsCapability        = "supply_chain.impact_findings.list"
	supplyChainImpactExplanationCapability     = "supply_chain.impact_explanation.read"
	containerImageIdentitiesCapability         = "supply_chain.container_image_identities.list"
	securityAlertReconciliationsCapability     = "supply_chain.security_alert_reconciliations.list"
	sbomAttestationAttachmentMaxLimit          = 200
	supplyChainImpactFindingMaxLimit           = 200
	containerImageIdentityMaxLimit             = 200
	securityAlertReconciliationMaxLimit        = 200

	// SupplyChainImpactProfilePrecise selects exact installed-version
	// anchored findings only.
	SupplyChainImpactProfilePrecise = "precise"
	// SupplyChainImpactProfileComprehensive selects every owned-anchor
	// finding including range-only manifest, SBOM/CPE-derived,
	// malformed range, and missing-version rows. Unsupported matcher
	// ecosystems are surfaced by readiness, not as finding rows.
	SupplyChainImpactProfileComprehensive = "comprehensive"
)

// SupplyChainHandler exposes reducer-owned supply-chain read models.
type SupplyChainHandler struct {
	Neo4j                    GraphQuery
	Content                  ContentStore
	SBOMAttachments          SBOMAttestationAttachmentStore
	SBOMAttachmentAggregates SBOMAttestationAttachmentAggregateStore
	AdvisoryEvidence         AdvisoryEvidenceStore
	AdvisoryCatalog          AdvisoryCatalogStore
	ImpactFindings           SupplyChainImpactFindingStore
	ImpactAggregates         SupplyChainImpactAggregateStore
	ImpactExplanations       SupplyChainImpactExplanationStore
	ContainerImageIdentities ContainerImageIdentityStore
	ContainerImageAggregates ContainerImageIdentityAggregateStore
	SecurityAlerts           SecurityAlertReconciliationStore
	SecurityAlertAggregates  SecurityAlertReconciliationAggregateStore
	Readiness                SupplyChainImpactReadinessStore
	// CollectorReadiness answers the configured-collector probe for the gated
	// SBOM/attestation and container-image list tools so an empty page reports
	// not_configured when the feeding collector is disabled. It is optional: a
	// nil store leaves the collector_readiness envelope off the response.
	CollectorReadiness CollectorListReadinessStore
	Profile            QueryProfile
}

// ContainerImageIdentityResult is one reducer-owned container image identity
// row returned by the public API.
type ContainerImageIdentityResult struct {
	IdentityID          string   `json:"identity_id"`
	Digest              string   `json:"digest,omitempty"`
	ImageRef            string   `json:"image_ref,omitempty"`
	RepositoryID        string   `json:"repository_id,omitempty"`
	SourceRepositoryIDs []string `json:"source_repository_ids,omitempty"`
	SourceRevision      string   `json:"source_revision,omitempty"`
	// SourceRevisionProvenance names where SourceRevision came from
	// ("oci_config_source_label" or "ci_run_commit"), empty when no revision
	// was resolved (#5423).
	SourceRevisionProvenance string   `json:"source_revision_provenance,omitempty"`
	WorkloadIDs              []string `json:"workload_ids,omitempty"`
	ServiceIDs               []string `json:"service_ids,omitempty"`
	Outcome                  string   `json:"outcome"`
	Reason                   string   `json:"reason,omitempty"`
	IdentityStrength         string   `json:"identity_strength,omitempty"`
	CanonicalID              string   `json:"canonical_id,omitempty"`
	CanonicalWrites          int      `json:"canonical_writes"`
	SourceLayers             []string `json:"source_layers,omitempty"`
	EvidenceFactIDs          []string `json:"evidence_fact_ids,omitempty"`
	MissingEvidence          []string `json:"missing_evidence,omitempty"`
	SourceFreshness          string   `json:"source_freshness,omitempty"`
	SourceConfidence         string   `json:"source_confidence,omitempty"`
}

// ContainerImageIdentitySourceBridge summarizes source-repository-scoped image
// identity evidence without reinterpreting OCI repository identity.
type ContainerImageIdentitySourceBridge struct {
	SourceRepositoryID string   `json:"source_repository_id"`
	ImageRepositoryIDs []string `json:"image_repository_ids,omitempty"`
	MissingEvidence    []string `json:"missing_evidence,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
}

// Mount registers supply-chain query routes.
func (h *SupplyChainHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/supply-chain/vulnerability-scanner/contract", h.getVulnerabilityScannerReadContract)
	mux.HandleFunc("GET /api/v0/supply-chain/sbom-attestations/attachments", h.listSBOMAttachments)
	mux.HandleFunc("GET /api/v0/supply-chain/advisories", h.listAdvisoryCatalog)
	mux.HandleFunc("GET /api/v0/supply-chain/advisories/evidence", h.listAdvisoryEvidence)
	mux.HandleFunc("GET /api/v0/supply-chain/vulnerabilities/{advisory_id}", h.getVulnerabilityDetail)
	mux.HandleFunc("GET /api/v0/supply-chain/impact/findings", h.listImpactFindings)
	mux.HandleFunc("GET /api/v0/supply-chain/impact/explain", h.explainImpact)
	mux.HandleFunc("GET /api/v0/investigations/supply-chain/impact/packet", h.getImpactPacket)
	mux.HandleFunc("GET /api/v0/supply-chain/container-images/identities", h.listContainerImageIdentities)
	mux.HandleFunc("GET /api/v0/supply-chain/security-alerts/reconciliations", h.listSecurityAlertReconciliations)
	h.supplyChainImpactAggregateRoutes(mux)
	h.securityAlertReconciliationAggregateRoutes(mux)
	h.containerImageIdentityAggregateRoutes(mux)
	h.sbomAttestationAttachmentAggregateRoutes(mux)
}

func (h *SupplyChainHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}
