package query

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	sbomAttestationAttachmentsCapability   = "supply_chain.sbom_attestation_attachments.list"
	supplyChainImpactFindingsCapability    = "supply_chain.impact_findings.list"
	supplyChainImpactExplanationCapability = "supply_chain.impact_explanation.read"
	containerImageIdentitiesCapability     = "supply_chain.container_image_identities.list"
	securityAlertReconciliationsCapability = "supply_chain.security_alert_reconciliations.list"
	sbomAttestationAttachmentMaxLimit      = 200
	supplyChainImpactFindingMaxLimit       = 200
	containerImageIdentityMaxLimit         = 200
	securityAlertReconciliationMaxLimit    = 200

	// SupplyChainImpactProfilePrecise selects exact installed-version
	// anchored findings only.
	SupplyChainImpactProfilePrecise = "precise"
	// SupplyChainImpactProfileComprehensive selects every owned-anchor
	// finding including range-only manifest, SBOM/CPE-derived,
	// unsupported ecosystem, malformed range, and missing-version rows.
	SupplyChainImpactProfileComprehensive = "comprehensive"
)

// SupplyChainHandler exposes reducer-owned supply-chain read models.
type SupplyChainHandler struct {
	SBOMAttachments          SBOMAttestationAttachmentStore
	AdvisoryEvidence         AdvisoryEvidenceStore
	ImpactFindings           SupplyChainImpactFindingStore
	ImpactAggregates         SupplyChainImpactAggregateStore
	ImpactExplanations       SupplyChainImpactExplanationStore
	ContainerImageIdentities ContainerImageIdentityStore
	SecurityAlerts           SecurityAlertReconciliationStore
	SecurityAlertAggregates  SecurityAlertReconciliationAggregateStore
	Readiness                SupplyChainImpactReadinessStore
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
	mux.HandleFunc("GET /api/v0/supply-chain/advisories/evidence", h.listAdvisoryEvidence)
	mux.HandleFunc("GET /api/v0/supply-chain/impact/findings", h.listImpactFindings)
	mux.HandleFunc("GET /api/v0/supply-chain/impact/explain", h.explainImpact)
	mux.HandleFunc("GET /api/v0/supply-chain/container-images/identities", h.listContainerImageIdentities)
	mux.HandleFunc("GET /api/v0/supply-chain/security-alerts/reconciliations", h.listSecurityAlertReconciliations)
	h.supplyChainImpactAggregateRoutes(mux)
	h.securityAlertReconciliationAggregateRoutes(mux)
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
	profile, ok := requestedSupplyChainImpactProfile(w, r)
	if !ok {
		return
	}
	priorityBucket, minPriorityScore, sort, err := supplyChainImpactPriorityFilter(r)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	suppressionState := QueryParam(r, "suppression_state")
	if suppressionState != "" && !isSupportedSupplyChainSuppressionState(suppressionState) {
		WriteError(w, http.StatusBadRequest, "suppression_state must be one of active, not_affected, accepted_risk, false_positive, ignored, expired, provider_dismissed, scope_mismatch")
		return
	}
	includeSuppressed, ok := parseSupplyChainImpactIncludeSuppressed(w, r)
	if !ok {
		return
	}
	filter := SupplyChainImpactFindingFilter{
		CVEID:             QueryParam(r, "cve_id"),
		PackageID:         QueryParam(r, "package_id"),
		RepositoryID:      QueryParam(r, "repository_id"),
		SubjectDigest:     QueryParam(r, "subject_digest"),
		ImpactStatus:      QueryParam(r, "impact_status"),
		DetectionProfile:  filterProfile(profile),
		PriorityBucket:    priorityBucket,
		MinPriorityScore:  minPriorityScore,
		Sort:              sort,
		SuppressionState:  suppressionState,
		IncludeSuppressed: includeSuppressed,
		AfterFindingID:    QueryParam(r, "after_finding_id"),
		Limit:             limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "cve_id, package_id, repository_id, subject_digest, impact_status, priority_bucket, or min_priority_score > 0 is required")
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
	scope := SupplyChainImpactTargetScope{
		CVEID:         filter.CVEID,
		PackageID:     filter.PackageID,
		RepositoryID:  filter.RepositoryID,
		SubjectDigest: filter.SubjectDigest,
		ImpactStatus:  filter.ImpactStatus,
	}
	snapshot, readinessErr := h.readSupplyChainImpactReadinessSnapshot(r, scope)
	var readiness SupplyChainImpactReadinessEnvelope
	if readinessErr != nil {
		// Readiness lookup failed (transient Postgres error, statement
		// timeout, etc.). Do not drop the already-fetched findings page:
		// return the findings with a `readiness_unavailable` envelope so
		// callers cannot misread zero findings as safe and can retry the
		// readiness lookup separately.
		readiness = BuildSupplyChainImpactReadinessUnavailable(scope, results, truncated)
	} else {
		readiness = BuildSupplyChainImpactReadiness(scope, results, truncated, snapshot)
	}
	body := map[string]any{
		"findings":          results,
		"count":             len(results),
		"limit":             limit,
		"truncated":         truncated,
		"detection_profile": profile,
		"readiness":         readiness,
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
		"resolved from reducer-owned impact facts; CVSS, EPSS, KEV, reachability, missing evidence, and readiness coverage remain separate",
	))
}

func (h *SupplyChainHandler) readSupplyChainImpactReadinessSnapshot(
	r *http.Request,
	scope SupplyChainImpactTargetScope,
) (SupplyChainImpactReadinessSnapshot, error) {
	if h.Readiness == nil {
		return SupplyChainImpactReadinessSnapshot{}, nil
	}
	return h.Readiness.ReadSupplyChainImpactReadiness(r.Context(), SupplyChainImpactReadinessQuery(scope))
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

// requestedSupplyChainImpactProfile reads the `profile` query parameter,
// rejects unknown values with a 400, and defaults to precise. `precise`
// returns only findings with an exact installed-version anchor.
// `comprehensive` returns every owned-anchor finding, including range-only,
// SBOM/CPE-derived, malformed, unsupported-ecosystem, and missing-version
// rows.
func requestedSupplyChainImpactProfile(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := strings.TrimSpace(QueryParam(r, "profile"))
	if raw == "" {
		return SupplyChainImpactProfilePrecise, true
	}
	switch raw {
	case SupplyChainImpactProfilePrecise, SupplyChainImpactProfileComprehensive:
		return raw, true
	default:
		WriteError(w, http.StatusBadRequest, "profile must be precise or comprehensive")
		return "", false
	}
}

// filterProfile maps the requested API profile to the on-row filter value.
// `comprehensive` matches every row, so the filter remains blank to avoid
// adding an unneeded predicate.
func filterProfile(profile string) string {
	if profile == SupplyChainImpactProfilePrecise {
		return SupplyChainImpactProfilePrecise
	}
	return ""
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

// isSupportedSupplyChainSuppressionState reports whether the value names a
// known reducer suppression state.
func isSupportedSupplyChainSuppressionState(state string) bool {
	switch state {
	case "active",
		"not_affected",
		"accepted_risk",
		"false_positive",
		"ignored",
		"expired",
		"provider_dismissed",
		"scope_mismatch":
		return true
	default:
		return false
	}
}

// parseSupplyChainImpactIncludeSuppressed parses the optional
// include_suppressed boolean. Default false, so callers see only findings the
// reducer considers actionable. Anything other than true/false returns 400.
func parseSupplyChainImpactIncludeSuppressed(w http.ResponseWriter, r *http.Request) (bool, bool) {
	raw := QueryParam(r, "include_suppressed")
	if raw == "" {
		return false, true
	}
	switch raw {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		WriteError(w, http.StatusBadRequest, "include_suppressed must be true or false")
		return false, false
	}
}
