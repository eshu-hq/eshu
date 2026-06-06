package query

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

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
	ImpactFindings           SupplyChainImpactFindingStore
	ImpactAggregates         SupplyChainImpactAggregateStore
	ImpactExplanations       SupplyChainImpactExplanationStore
	ContainerImageIdentities ContainerImageIdentityStore
	ContainerImageAggregates ContainerImageIdentityAggregateStore
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
	AttachmentScope    string                 `json:"attachment_scope,omitempty"`
	CanonicalWrites    int                    `json:"canonical_writes"`
	ComponentCount     int                    `json:"component_count"`
	ComponentEvidence  []ComponentEvidenceRow `json:"component_evidence,omitempty"`
	RepositoryIDs      []string               `json:"repository_ids,omitempty"`
	WorkloadIDs        []string               `json:"workload_ids,omitempty"`
	ServiceIDs         []string               `json:"service_ids,omitempty"`
	WarningSummaries   []string               `json:"warning_summaries,omitempty"`
	EvidenceFactIDs    []string               `json:"evidence_fact_ids,omitempty"`
	MissingEvidence    []string               `json:"missing_evidence,omitempty"`
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
	mux.HandleFunc("GET /api/v0/supply-chain/vulnerability-scanner/contract", h.getVulnerabilityScannerReadContract)
	mux.HandleFunc("GET /api/v0/supply-chain/sbom-attestations/attachments", h.listSBOMAttachments)
	mux.HandleFunc("GET /api/v0/supply-chain/advisories/evidence", h.listAdvisoryEvidence)
	mux.HandleFunc("GET /api/v0/supply-chain/vulnerabilities/{advisory_id}", h.getVulnerabilityDetail)
	mux.HandleFunc("GET /api/v0/supply-chain/impact/findings", h.listImpactFindings)
	mux.HandleFunc("GET /api/v0/supply-chain/impact/explain", h.explainImpact)
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
	if !rejectUnsupportedVulnerabilityScannerFilters(w, r, impactFindingsScannerFilters()) {
		return
	}
	advisoryID := QueryParam(r, "advisory_id")
	if advisoryID == "" {
		advisoryID = firstNonEmptyQueryParam(r, "ghsa_id", "osv_id")
	}
	severity, ok := parseSupplyChainScannerSeverity(w, r)
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
	repositoryID, ok := h.resolveSupplyChainRepositorySelector(w, r, QueryParam(r, "repository_id"))
	if !ok {
		return
	}
	filter := SupplyChainImpactFindingFilter{
		CVEID:             QueryParam(r, "cve_id"),
		AdvisoryID:        advisoryID,
		PackageID:         QueryParam(r, "package_id"),
		RepositoryID:      repositoryID,
		SubjectDigest:     QueryParam(r, "subject_digest"),
		ImpactStatus:      QueryParam(r, "impact_status"),
		Ecosystem:         QueryParam(r, "ecosystem"),
		WorkloadID:        QueryParam(r, "workload_id"),
		ServiceID:         QueryParam(r, "service_id"),
		Environment:       QueryParam(r, "environment"),
		Severity:          severity,
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
		WriteError(w, http.StatusBadRequest, "cve_id, advisory_id, package_id, repository_id, subject_digest, impact_status, ecosystem, workload_id, service_id, environment, severity, priority_bucket, or min_priority_score > 0 is required")
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
		results = append(results, buildSupplyChainImpactFindingResult(row))
	}
	scope := SupplyChainImpactTargetScope{
		CVEID:         filter.CVEID,
		AdvisoryID:    filter.AdvisoryID,
		PackageID:     filter.PackageID,
		RepositoryID:  filter.RepositoryID,
		SubjectDigest: filter.SubjectDigest,
		Ecosystem:     filter.Ecosystem,
		WorkloadID:    filter.WorkloadID,
		ServiceID:     filter.ServiceID,
		Environment:   filter.Environment,
		Severity:      filter.Severity,
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
	if !rejectUnsupportedSBOMAttestationAttachmentRepositoryScope(w, r) {
		return
	}
	limit, ok := requiredSBOMAttestationAttachmentLimit(w, r)
	if !ok {
		return
	}
	filter := SBOMAttestationAttachmentFilter{
		SubjectDigest:     firstNonEmptyQueryParam(r, "subject_digest", "digest"),
		DocumentID:        QueryParam(r, "document_id"),
		DocumentDigest:    QueryParam(r, "document_digest"),
		WorkloadID:        QueryParam(r, "workload_id"),
		ServiceID:         QueryParam(r, "service_id"),
		AttachmentStatus:  QueryParam(r, "attachment_status"),
		ArtifactKind:      QueryParam(r, "artifact_kind"),
		AfterAttachmentID: QueryParam(r, "after_attachment_id"),
		Limit:             limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "subject_digest, document_id, document_digest, workload_id, or service_id is required")
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

	page, err := h.SBOMAttachments.ListSBOMAttestationAttachments(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows := page.Attachments
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
	if len(page.MissingEvidence) > 0 {
		body["missing_evidence"] = page.MissingEvidence
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

func rejectUnsupportedSBOMAttestationAttachmentRepositoryScope(w http.ResponseWriter, r *http.Request) bool {
	if QueryParam(r, "repository_id") == "" {
		return true
	}
	WriteError(w, http.StatusBadRequest, "repository_id is not supported for SBOM attachment reads; omit repository_id for unscoped totals or filter by subject_digest, document_id, document_digest, workload_id, or service_id")
	return false
}

// requestedSupplyChainImpactProfile reads the `profile` query parameter,
// rejects unknown values with a 400, and defaults to precise. `precise`
// returns only findings with an exact installed-version anchor.
// `comprehensive` returns every owned-anchor finding, including range-only,
// SBOM/CPE-derived, malformed, and missing-version rows.
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
