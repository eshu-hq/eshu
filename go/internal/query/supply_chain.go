package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	sbomAttestationAttachmentsCapability = "supply_chain.sbom_attestation_attachments.list"
	supplyChainImpactFindingsCapability  = "supply_chain.impact_findings.list"
	containerImageIdentitiesCapability   = "supply_chain.container_image_identities.list"
	sbomAttestationAttachmentMaxLimit    = 200
	supplyChainImpactFindingMaxLimit     = 200
	containerImageIdentityMaxLimit       = 200
)

// SupplyChainHandler exposes reducer-owned supply-chain read models.
type SupplyChainHandler struct {
	SBOMAttachments          SBOMAttestationAttachmentStore
	ImpactFindings           SupplyChainImpactFindingStore
	ContainerImageIdentities ContainerImageIdentityStore
	Profile                  QueryProfile
}

// SBOMAttestationAttachmentResult is one reducer-owned SBOM or attestation
// attachment row returned by the public API.
type SBOMAttestationAttachmentResult struct {
	AttachmentID       string                 `json:"attachment_id"`
	SubjectDigest      string                 `json:"subject_digest,omitempty"`
	DocumentID         string                 `json:"document_id,omitempty"`
	DocumentDigest     string                 `json:"document_digest,omitempty"`
	AttachmentStatus   string                 `json:"attachment_status"`
	ParseStatus        string                 `json:"parse_status,omitempty"`
	VerificationStatus string                 `json:"verification_status,omitempty"`
	VerificationPolicy string                 `json:"verification_policy,omitempty"`
	ArtifactKind       string                 `json:"artifact_kind,omitempty"`
	Format             string                 `json:"format,omitempty"`
	SpecVersion        string                 `json:"spec_version,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	CanonicalWrites    int                    `json:"canonical_writes"`
	ComponentCount     int                    `json:"component_count"`
	ComponentEvidence  []ComponentEvidenceRow `json:"component_evidence,omitempty"`
	WarningSummaries   []string               `json:"warning_summaries,omitempty"`
	EvidenceFactIDs    []string               `json:"evidence_fact_ids,omitempty"`
	SourceFreshness    string                 `json:"source_freshness,omitempty"`
	SourceConfidence   string                 `json:"source_confidence,omitempty"`
}

// SupplyChainImpactFindingResult is one reducer-owned vulnerability impact row
// returned by the public API.
type SupplyChainImpactFindingResult struct {
	FindingID           string   `json:"finding_id"`
	CVEID               string   `json:"cve_id,omitempty"`
	AdvisoryID          string   `json:"advisory_id,omitempty"`
	PackageID           string   `json:"package_id,omitempty"`
	Ecosystem           string   `json:"ecosystem,omitempty"`
	PackageName         string   `json:"package_name,omitempty"`
	PURL                string   `json:"purl,omitempty"`
	ProductCriteria     string   `json:"product_criteria,omitempty"`
	MatchCriteriaID     string   `json:"match_criteria_id,omitempty"`
	ObservedVersion     string   `json:"observed_version,omitempty"`
	FixedVersion        string   `json:"fixed_version,omitempty"`
	ImpactStatus        string   `json:"impact_status"`
	Confidence          string   `json:"confidence,omitempty"`
	CVSSScore           float64  `json:"cvss_score,omitempty"`
	EPSSProbability     string   `json:"epss_probability,omitempty"`
	EPSSPercentile      string   `json:"epss_percentile,omitempty"`
	KnownExploited      bool     `json:"known_exploited"`
	PriorityReason      string   `json:"priority_reason,omitempty"`
	RuntimeReachability string   `json:"runtime_reachability,omitempty"`
	RepositoryID        string   `json:"repository_id,omitempty"`
	SubjectDigest       string   `json:"subject_digest,omitempty"`
	MissingEvidence     []string `json:"missing_evidence,omitempty"`
	EvidencePath        []string `json:"evidence_path,omitempty"`
	EvidenceFactIDs     []string `json:"evidence_fact_ids,omitempty"`
	SourceFreshness     string   `json:"source_freshness,omitempty"`
	SourceConfidence    string   `json:"source_confidence,omitempty"`
}

// ContainerImageIdentityResult is one reducer-owned container image identity
// row returned by the public API.
type ContainerImageIdentityResult struct {
	IdentityID       string   `json:"identity_id"`
	Digest           string   `json:"digest,omitempty"`
	ImageRef         string   `json:"image_ref,omitempty"`
	RepositoryID     string   `json:"repository_id,omitempty"`
	Outcome          string   `json:"outcome"`
	Reason           string   `json:"reason,omitempty"`
	IdentityStrength string   `json:"identity_strength,omitempty"`
	CanonicalID      string   `json:"canonical_id,omitempty"`
	CanonicalWrites  int      `json:"canonical_writes"`
	SourceLayers     []string `json:"source_layers,omitempty"`
	EvidenceFactIDs  []string `json:"evidence_fact_ids,omitempty"`
	SourceFreshness  string   `json:"source_freshness,omitempty"`
	SourceConfidence string   `json:"source_confidence,omitempty"`
}

// Mount registers supply-chain query routes.
func (h *SupplyChainHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/supply-chain/sbom-attestations/attachments", h.listSBOMAttachments)
	mux.HandleFunc("GET /api/v0/supply-chain/impact/findings", h.listImpactFindings)
	mux.HandleFunc("GET /api/v0/supply-chain/container-images/identities", h.listContainerImageIdentities)
}

func (h *SupplyChainHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *SupplyChainHandler) listImpactFindings(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainImpactFindings,
		"GET /api/v0/supply-chain/impact/findings",
		supplyChainImpactFindingsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), supplyChainImpactFindingsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact findings require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			supplyChainImpactFindingsCapability,
			h.profile(),
			requiredProfile(supplyChainImpactFindingsCapability),
		)
		return
	}
	limit, ok := requiredSupplyChainImpactFindingLimit(w, r)
	if !ok {
		return
	}
	filter := SupplyChainImpactFindingFilter{
		CVEID:          QueryParam(r, "cve_id"),
		PackageID:      QueryParam(r, "package_id"),
		RepositoryID:   QueryParam(r, "repository_id"),
		SubjectDigest:  QueryParam(r, "subject_digest"),
		ImpactStatus:   QueryParam(r, "impact_status"),
		AfterFindingID: QueryParam(r, "after_finding_id"),
		Limit:          limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "cve_id, package_id, repository_id, subject_digest, or impact_status is required")
		return
	}
	if h.ImpactFindings == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact findings require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			supplyChainImpactFindingsCapability,
			h.profile(),
			requiredProfile(supplyChainImpactFindingsCapability),
		)
		return
	}

	rows, err := h.ImpactFindings.ListSupplyChainImpactFindings(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]SupplyChainImpactFindingResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, SupplyChainImpactFindingResult(row))
	}
	body := map[string]any{
		"findings":  results,
		"count":     len(results),
		"limit":     limit,
		"truncated": truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_finding_id": results[len(results)-1].FindingID,
		}
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		supplyChainImpactFindingsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned impact facts; CVSS, EPSS, KEV, reachability, and missing evidence remain separate",
	))
}

func (h *SupplyChainHandler) listContainerImageIdentities(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryContainerImageIdentities,
		"GET /api/v0/supply-chain/container-images/identities",
		containerImageIdentitiesCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), containerImageIdentitiesCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"container image identities require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			containerImageIdentitiesCapability,
			h.profile(),
			requiredProfile(containerImageIdentitiesCapability),
		)
		return
	}
	limit, ok := requiredContainerImageIdentityLimit(w, r)
	if !ok {
		return
	}
	filter := ContainerImageIdentityFilter{
		Digest:          QueryParam(r, "digest"),
		ImageRef:        QueryParam(r, "image_ref"),
		RepositoryID:    QueryParam(r, "repository_id"),
		Outcome:         QueryParam(r, "outcome"),
		AfterIdentityID: QueryParam(r, "after_identity_id"),
		Limit:           limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "digest, image_ref, repository_id, or outcome is required")
		return
	}
	if filter.Outcome != "" && !isSupportedContainerImageIdentityOutcome(filter.Outcome) {
		WriteError(w, http.StatusBadRequest, "outcome must be exact_digest or tag_resolved")
		return
	}
	if h.ContainerImageIdentities == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"container image identities require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			containerImageIdentitiesCapability,
			h.profile(),
			requiredProfile(containerImageIdentitiesCapability),
		)
		return
	}

	rows, err := h.ContainerImageIdentities.ListContainerImageIdentities(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]ContainerImageIdentityResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, ContainerImageIdentityResult(row))
	}
	body := map[string]any{
		"identities": results,
		"count":      len(results),
		"limit":      limit,
		"truncated":  truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_identity_id": results[len(results)-1].IdentityID,
		}
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		containerImageIdentitiesCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned container image identity facts; weak, ambiguous, unresolved, and stale tags remain diagnostic reducer outcomes",
	))
}

func (h *SupplyChainHandler) listSBOMAttachments(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySBOMAttestationAttachments,
		"GET /api/v0/supply-chain/sbom-attestations/attachments",
		sbomAttestationAttachmentsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), sbomAttestationAttachmentsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"SBOM and attestation attachments require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			sbomAttestationAttachmentsCapability,
			h.profile(),
			requiredProfile(sbomAttestationAttachmentsCapability),
		)
		return
	}
	limit, ok := requiredSBOMAttestationAttachmentLimit(w, r)
	if !ok {
		return
	}
	filter := SBOMAttestationAttachmentFilter{
		SubjectDigest:     QueryParam(r, "subject_digest"),
		DocumentID:        QueryParam(r, "document_id"),
		DocumentDigest:    QueryParam(r, "document_digest"),
		AttachmentStatus:  QueryParam(r, "attachment_status"),
		ArtifactKind:      QueryParam(r, "artifact_kind"),
		AfterAttachmentID: QueryParam(r, "after_attachment_id"),
		Limit:             limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "subject_digest, document_id, or document_digest is required")
		return
	}
	if h.SBOMAttachments == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"SBOM and attestation attachments require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			sbomAttestationAttachmentsCapability,
			h.profile(),
			requiredProfile(sbomAttestationAttachmentsCapability),
		)
		return
	}

	rows, err := h.SBOMAttachments.ListSBOMAttestationAttachments(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]SBOMAttestationAttachmentResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, SBOMAttestationAttachmentResult(row))
	}
	body := map[string]any{
		"attachments": results,
		"count":       len(results),
		"limit":       limit,
		"truncated":   truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_attachment_id": results[len(results)-1].AttachmentID,
		}
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		sbomAttestationAttachmentsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned SBOM and attestation attachment facts; parse validity and verification trust remain separate",
	))
}

func requiredSBOMAttestationAttachmentLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > sbomAttestationAttachmentMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", sbomAttestationAttachmentMaxLimit))
		return 0, false
	}
	return limit, true
}

func requiredSupplyChainImpactFindingLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > supplyChainImpactFindingMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", supplyChainImpactFindingMaxLimit))
		return 0, false
	}
	return limit, true
}

func requiredContainerImageIdentityLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > containerImageIdentityMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", containerImageIdentityMaxLimit))
		return 0, false
	}
	return limit, true
}

func isSupportedContainerImageIdentityOutcome(outcome string) bool {
	switch outcome {
	case "exact_digest", "tag_resolved":
		return true
	default:
		return false
	}
}
