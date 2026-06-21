package query

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
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
	AdvisoryCatalog          AdvisoryCatalogStore
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
	AttachmentID              string                 `json:"attachment_id"`
	SubjectDigest             string                 `json:"subject_digest,omitempty"`
	DocumentID                string                 `json:"document_id,omitempty"`
	DocumentDigest            string                 `json:"document_digest,omitempty"`
	AttachmentStatus          string                 `json:"attachment_status"`
	ParseStatus               string                 `json:"parse_status,omitempty"`
	VerificationStatus        string                 `json:"verification_status,omitempty"`
	VerificationPolicy        string                 `json:"verification_policy,omitempty"`
	ArtifactKind              string                 `json:"artifact_kind,omitempty"`
	Format                    string                 `json:"format,omitempty"`
	SpecVersion               string                 `json:"spec_version,omitempty"`
	Reason                    string                 `json:"reason,omitempty"`
	AttachmentScope           string                 `json:"attachment_scope,omitempty"`
	CanonicalWrites           int                    `json:"canonical_writes"`
	ComponentCount            int                    `json:"component_count"`
	ComponentEvidence         []ComponentEvidenceRow `json:"component_evidence,omitempty"`
	RepositoryIDs             []string               `json:"repository_ids,omitempty"`
	WorkloadIDs               []string               `json:"workload_ids,omitempty"`
	ServiceIDs                []string               `json:"service_ids,omitempty"`
	WarningSummaries          []string               `json:"warning_summaries,omitempty"`
	WarningSummaryCount       int                    `json:"warning_summary_count"`
	WarningSummariesTruncated bool                   `json:"warning_summaries_truncated"`
	EvidenceFactIDs           []string               `json:"evidence_fact_ids,omitempty"`
	MissingEvidence           []string               `json:"missing_evidence,omitempty"`
	SourceFreshness           string                 `json:"source_freshness,omitempty"`
	SourceConfidence          string                 `json:"source_confidence,omitempty"`
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
	WorkloadIDs         []string `json:"workload_ids,omitempty"`
	ServiceIDs          []string `json:"service_ids,omitempty"`
	Outcome             string   `json:"outcome"`
	Reason              string   `json:"reason,omitempty"`
	IdentityStrength    string   `json:"identity_strength,omitempty"`
	CanonicalID         string   `json:"canonical_id,omitempty"`
	CanonicalWrites     int      `json:"canonical_writes"`
	SourceLayers        []string `json:"source_layers,omitempty"`
	EvidenceFactIDs     []string `json:"evidence_fact_ids,omitempty"`
	MissingEvidence     []string `json:"missing_evidence,omitempty"`
	SourceFreshness     string   `json:"source_freshness,omitempty"`
	SourceConfidence    string   `json:"source_confidence,omitempty"`
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
	// Resolve scoped-token grants before any reducer or readiness store read.
	// An empty grant returns the bounded zero-findings page without touching
	// the impact, readiness, or repository-selector stores so a scoped caller
	// with no authorized repositories cannot probe cross-tenant evidence.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptyImpactFindingsPage(w, r, limit, profile)
		return
	}
	repositoryID, ok := h.resolveSupplyChainImpactRepositorySelector(w, r, QueryParam(r, "repository_id"), access)
	if !ok {
		return
	}
	filter := SupplyChainImpactFindingFilter{
		CVEID:             QueryParam(r, "cve_id"),
		AdvisoryID:        advisoryID,
		PackageID:         QueryParam(r, "package_id"),
		RepositoryID:      repositoryID,
		SubjectDigest:     QueryParam(r, "subject_digest"),
		ImageRef:          QueryParam(r, "image_ref"),
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
	if access.scoped() {
		filter.AllowedRepositoryIDs = append([]string(nil), access.allowedRepositoryIDs...)
		filter.AllowedScopeIDs = append([]string(nil), access.allowedScopeIDs...)
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "cve_id, advisory_id, package_id, repository_id, subject_digest, image_ref, impact_status, ecosystem, workload_id, service_id, environment, severity, priority_bucket, or min_priority_score > 0 is required")
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
		ImageRef:      filter.ImageRef,
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
	truth := BuildTruthEnvelope(
		h.profile(),
		supplyChainImpactFindingsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned impact facts; CVSS, EPSS, KEV, reachability, missing evidence, and readiness coverage remain separate",
	)
	// When the list is served from the maintained winners read model (#3389
	// Phase 2 gate), report its freshness from the maintainer watermark so a
	// resweep cadence lag, an unpopulated table, or a probe failure is never
	// served as fresh truth. The legacy live read is always current and leaves
	// the envelope fresh; the probe costs nothing there.
	if reader, ok := h.ImpactFindings.(supplyChainImpactWinnersFreshnessReader); ok {
		watermark, freshnessErr := reader.SupplyChainImpactWinnersWatermark(r.Context())
		applyWinnersFreshness(truth, watermark, freshnessErr, time.Now())
		if freshnessErr != nil {
			// The findings page already succeeded; only the freshness probe
			// failed. Record it for triage but still serve the page (with an
			// unavailable freshness state rather than a false fresh).
			span.RecordError(freshnessErr)
		}
	}
	span.SetAttributes(attribute.String("eshu.query.freshness_state", string(truth.Freshness.State)))
	WriteSuccess(w, r, http.StatusOK, body, truth)
}

// supplyChainImpactWinnersFreshnessReader is the optional capability the
// impact-findings store implements when it can report the maintained winners
// read-model watermark. The handler type-asserts it so the legacy store (or a
// test double) that does not implement it simply keeps the fresh envelope.
type supplyChainImpactWinnersFreshnessReader interface {
	SupplyChainImpactWinnersWatermark(context.Context) (SupplyChainImpactWinnersFreshness, error)
}

// supplyChainImpactWinnersFreshnessWindow bounds how long after the last winners
// resweep the read model is still considered fresh. The reducer maintainer
// resweeps on a short cadence (~30s) and stamps every row with one
// materialized_at, so a healthy watermark is always within roughly one cadence of
// now. The window allows several cadences of headroom for a slow resweep or a
// transient lease handoff; a watermark older than this means the maintainer is
// not keeping the read model current, so the read is reported stale
// (reducer_backlog) instead of served as fresh truth.
const supplyChainImpactWinnersFreshnessWindow = 2 * time.Minute

// applyWinnersFreshness downgrades the truth envelope when the impact-findings
// list is served from the maintained winners read model and that model is behind,
// unpopulated, or could not be probed. It is a no-op on the legacy live read
// (always current) and when the model is fresh. now is injected for deterministic
// tests.
func applyWinnersFreshness(truth *TruthEnvelope, fr SupplyChainImpactWinnersFreshness, probeErr error, now time.Time) {
	if truth == nil || !fr.ServingFromWinners {
		return
	}
	if probeErr != nil {
		truth.Freshness = TruthFreshness{
			State:  FreshnessUnavailable,
			Detail: "could not determine supply-chain impact winners read-model freshness",
		}
		return
	}
	if !fr.Present {
		// No maintainer watermark at all: the reducer has never reswept the read
		// model. A resweep that produced zero winners still stamps the watermark,
		// so this is the genuine never-populated case, not a zero-findings corpus.
		truth.Freshness = TruthFreshness{
			State:  FreshnessBuilding,
			Detail: "supply-chain impact winners read model has not been materialized by the reducer maintainer yet",
		}
		WithFreshnessCause(truth, FreshnessCauseReducerBacklog)
		return
	}
	materializedAt := fr.MaterializedAt.UTC()
	observedAt := materializedAt.Format(time.RFC3339)
	if now.UTC().Sub(materializedAt) <= supplyChainImpactWinnersFreshnessWindow {
		// Fresh, but surface the watermark so consumers see when the read model
		// was last resweep'd.
		truth.Freshness.ObservedAt = observedAt
		return
	}
	truth.Freshness = TruthFreshness{
		State:      FreshnessStale,
		ObservedAt: observedAt,
		Detail:     "supply-chain impact winners read model is behind its maintainer resweep cadence",
	}
	WithFreshnessCause(truth, FreshnessCauseReducerBacklog)
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
	limit, ok := requiredSBOMAttestationAttachmentLimit(w, r)
	if !ok {
		return
	}
	// Empty scoped grants return the zero-attachments page without resolving a
	// selector or reading the attachment store.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptySBOMAttachmentPage(w, r, limit)
		return
	}
	repositoryID, ok := h.resolveSBOMAttachmentRepositorySelector(w, r, QueryParam(r, "repository_id"), access)
	if !ok {
		return
	}
	filter := SBOMAttestationAttachmentFilter{
		SubjectDigest:              firstNonEmptyQueryParam(r, "subject_digest", "digest"),
		DocumentID:                 QueryParam(r, "document_id"),
		DocumentDigest:             QueryParam(r, "document_digest"),
		RepositoryID:               repositoryID,
		WorkloadID:                 QueryParam(r, "workload_id"),
		ServiceID:                  QueryParam(r, "service_id"),
		AttachmentStatus:           QueryParam(r, "attachment_status"),
		ArtifactKind:               QueryParam(r, "artifact_kind"),
		AfterAttachmentID:          QueryParam(r, "after_attachment_id"),
		Limit:                      limit + 1,
		AllowedSourceRepositoryIDs: access.repositorySearchIDs(),
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "subject_digest, document_id, document_digest, repository_id, workload_id, or service_id is required")
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
		results = append(results, buildSBOMAttestationAttachmentResult(row))
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
