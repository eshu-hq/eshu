// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

const (
	SBOMAttestationAttachmentsCapability       = "supply_chain.sbom_attestation_attachments.list"
	VulnerabilityScannerReadContractCapability = "supply_chain.vulnerability_scanner.contract.read"
	SupplyChainImpactFindingsCapability        = "supply_chain.impact_findings.list"
	SupplyChainImpactExplanationCapability     = "supply_chain.impact_explanation.read"
	ContainerImageIdentitiesCapability         = "supply_chain.container_image_identities.list"
	SecurityAlertReconciliationsCapability     = "supply_chain.security_alert_reconciliations.list"
	SBOMAttestationAttachmentMaxLimit          = 200
	SupplyChainImpactFindingMaxLimit           = 200
	ContainerImageIdentityMaxLimit             = 200
	SecurityAlertReconciliationMaxLimit        = 200

	// impact.SupplyChainImpactProfilePrecise and impact.SupplyChainImpactProfileComprehensive
	// moved to internal/query/supplychain/impact with the impact read models
	// (#6060 lane A); see supply_chain_impact_alias.go.
)

// SupplyChainHandler exposes reducer-owned supply-chain read models.
type SupplyChainHandler struct {
	Neo4j                    querycontract.GraphQuery
	Content                  querycontract.ContentStore
	SBOMAttachments          SBOMAttestationAttachmentStore
	SBOMAttachmentAggregates SBOMAttestationAttachmentAggregateStore
	AdvisoryEvidence         advisory.AdvisoryEvidenceStore
	AdvisoryCatalog          advisory.AdvisoryCatalogStore
	ImpactFindings           impact.SupplyChainImpactFindingStore
	ImpactAggregates         impact.SupplyChainImpactAggregateStore
	ImpactExplanations       impact.SupplyChainImpactExplanationStore
	ContainerImageIdentities ContainerImageIdentityStore
	ContainerImageAggregates ContainerImageIdentityAggregateStore
	SecurityAlerts           SecurityAlertReconciliationStore
	SecurityAlertAggregates  SecurityAlertReconciliationAggregateStore
	Readiness                impact.SupplyChainImpactReadinessStore
	SuppressionMutations     VulnerabilitySuppressionMutationStore
	// CloudResourceInventory gates the #5452 runtime-observed cloud evidence
	// probe: it filters the probe's digest-matched CloudResource graph nodes to
	// those that are BOTH current (active-generation, non-tombstoned) and
	// authorized for the caller's scope grants, so a stale node or a cross-scope
	// resource never becomes runtime_confirmed evidence. Nil disables the runtime
	// tier entirely (the probe is skipped) rather than surfacing unauthorized or
	// stale evidence.
	CloudResourceInventory CloudResourceCurrentInventoryFilter
	// KubernetesWorkloadInventory gates exact digest-bound RUNS_IMAGE graph
	// candidates through the independent current-owner and current-edge
	// authorization contract before they can promote a finding.
	KubernetesWorkloadInventory KubernetesWorkloadCurrentInventoryFilter
	// CollectorReadiness answers the configured-collector probe for the gated
	// SBOM/attestation and container-image list tools so an empty page reports
	// not_configured when the feeding collector is disabled. It is optional: a
	// nil store leaves the collector_readiness envelope off the response.
	CollectorReadiness querycontract.CollectorListReadinessStore
	// PacketResponder composes and writes the impact investigation packet.
	// Root package query provides it from the lane-B packet envelope; cmd
	// wiring always injects it. A nil responder answers the packet route
	// with 503 rather than composing without the envelope.
	PacketResponder SupplyChainImpactPacketResponder
	Profile         querycontract.QueryProfile
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
	mux.HandleFunc("POST /api/v0/supply-chain/impact/suppressions", h.createVulnerabilitySuppression)
	mux.HandleFunc("GET /api/v0/investigations/supply-chain/impact/packet", h.getImpactPacket)
	mux.HandleFunc("GET /api/v0/supply-chain/container-images/identities", h.listContainerImageIdentities)
	mux.HandleFunc("GET /api/v0/supply-chain/security-alerts/reconciliations", h.listSecurityAlertReconciliations)
	h.supplyChainImpactAggregateRoutes(mux)
	h.securityAlertReconciliationAggregateRoutes(mux)
	h.containerImageIdentityAggregateRoutes(mux)
	h.sbomAttestationAttachmentAggregateRoutes(mux)
}

func (h *SupplyChainHandler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}
