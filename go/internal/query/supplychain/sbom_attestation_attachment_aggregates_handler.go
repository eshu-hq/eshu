// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const SBOMAttestationAttachmentAggregateCapability = "supply_chain.sbom_attestation_attachments.aggregate"

// sbomAttestationAttachmentAggregateRoutes registers the cheap-summary
// aggregate routes alongside the existing SBOM attachment list route.
func (h *SupplyChainHandler) sbomAttestationAttachmentAggregateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/supply-chain/sbom-attestations/attachments/count", h.countSBOMAttestationAttachments)
	mux.HandleFunc("GET /api/v0/supply-chain/sbom-attestations/attachments/inventory", h.sbomAttestationAttachmentInventory)
}

func (h *SupplyChainHandler) countSBOMAttestationAttachments(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySBOMAttestationAttachmentAggregate,
		"GET /api/v0/supply-chain/sbom-attestations/attachments/count",
		SBOMAttestationAttachmentAggregateCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), SBOMAttestationAttachmentAggregateCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"SBOM attestation attachment aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			SBOMAttestationAttachmentAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SBOMAttestationAttachmentAggregateCapability),
		)
		return
	}
	if h.SBOMAttachmentAggregates == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"SBOM attestation attachment aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			SBOMAttestationAttachmentAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SBOMAttestationAttachmentAggregateCapability),
		)
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptySBOMAttachmentCount(w, r)
		return
	}
	filter, ok := h.sbomAttestationAttachmentAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}
	if !validateSBOMAttestationAttachmentAggregateFilters(w, filter) {
		return
	}
	count, err := h.SBOMAttachmentAggregates.CountSBOMAttestationAttachments(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	body := map[string]any{
		"total_attachments":    count.TotalAttachments,
		"by_attachment_status": count.ByAttachmentStatus,
		"by_artifact_kind":     count.ByArtifactKind,
		"scope":                SBOMAttestationAttachmentAggregateScope(filter),
	}
	if len(count.MissingEvidence) > 0 {
		body["missing_evidence"] = count.MissingEvidence
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		SBOMAttestationAttachmentAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned SBOM and attestation attachment facts; per-status / per-artifact-kind rollups stay separate",
	))
}

func (h *SupplyChainHandler) sbomAttestationAttachmentInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySBOMAttestationAttachmentAggregate,
		"GET /api/v0/supply-chain/sbom-attestations/attachments/inventory",
		SBOMAttestationAttachmentAggregateCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), SBOMAttestationAttachmentAggregateCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"SBOM attestation attachment aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			SBOMAttestationAttachmentAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SBOMAttestationAttachmentAggregateCapability),
		)
		return
	}
	if h.SBOMAttachmentAggregates == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"SBOM attestation attachment aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			SBOMAttestationAttachmentAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SBOMAttestationAttachmentAggregateCapability),
		)
		return
	}

	dimension := SBOMAttestationAttachmentInventoryDimension(querycontract.QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = SBOMAttestationAttachmentInventoryByAttachmentStatus
	}
	if !isSupportedSBOMAttestationAttachmentDimension(dimension) {
		querycontract.WriteError(w, http.StatusBadRequest, "group_by must be one of attachment_status, artifact_kind, subject_digest")
		return
	}
	limit, ok := parseSBOMAttestationAttachmentAggregateLimit(w, r)
	if !ok {
		return
	}
	offset, ok := parseSBOMAttestationAttachmentAggregateOffset(w, r)
	if !ok {
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptySBOMAttachmentInventory(w, r, dimension, limit, offset)
		return
	}
	filter, ok := h.sbomAttestationAttachmentAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}
	if !validateSBOMAttestationAttachmentAggregateFilters(w, filter) {
		return
	}

	rows, err := h.SBOMAttachmentAggregates.SBOMAttestationAttachmentInventory(r.Context(), filter, dimension, limit+1, offset)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	body := map[string]any{
		"buckets":     rows,
		"count":       len(rows),
		"limit":       limit,
		"offset":      offset,
		"group_by":    string(dimension),
		"truncated":   truncated,
		"next_offset": NextSBOMAttestationAttachmentAggregateOffset(offset, limit, truncated),
		"scope":       SBOMAttestationAttachmentAggregateScope(filter),
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		SBOMAttestationAttachmentAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned SBOM and attestation attachment facts; one grouped bucket per row, ordered by count desc",
	))
}

func (h *SupplyChainHandler) sbomAttestationAttachmentAggregateFilterFromRequest(
	w http.ResponseWriter,
	r *http.Request,
	access querycontract.RepositoryAccessFilter,
) (SBOMAttestationAttachmentAggregateFilter, bool) {
	repositoryID, ok := h.resolveSBOMAttachmentRepositorySelector(w, r, querycontract.QueryParam(r, "repository_id"), access, SBOMAttestationAttachmentAggregateCapability)
	if !ok {
		return SBOMAttestationAttachmentAggregateFilter{}, false
	}
	return SBOMAttestationAttachmentAggregateFilter{
		SubjectDigest:              querycontract.QueryParam(r, "subject_digest"),
		DocumentID:                 querycontract.QueryParam(r, "document_id"),
		DocumentDigest:             querycontract.QueryParam(r, "document_digest"),
		RepositoryID:               repositoryID,
		WorkloadID:                 querycontract.QueryParam(r, "workload_id"),
		ServiceID:                  querycontract.QueryParam(r, "service_id"),
		AttachmentStatus:           querycontract.QueryParam(r, "attachment_status"),
		ArtifactKind:               querycontract.QueryParam(r, "artifact_kind"),
		AllowedSourceRepositoryIDs: access.RepositorySearchIDs(),
	}, true
}

func SBOMAttestationAttachmentAggregateScope(filter SBOMAttestationAttachmentAggregateFilter) map[string]string {
	out := map[string]string{}
	if filter.SubjectDigest != "" {
		out["subject_digest"] = filter.SubjectDigest
	}
	if filter.DocumentID != "" {
		out["document_id"] = filter.DocumentID
	}
	if filter.DocumentDigest != "" {
		out["document_digest"] = filter.DocumentDigest
	}
	if filter.RepositoryID != "" {
		out["repository_id"] = filter.RepositoryID
	}
	if filter.WorkloadID != "" {
		out["workload_id"] = filter.WorkloadID
	}
	if filter.ServiceID != "" {
		out["service_id"] = filter.ServiceID
	}
	if filter.AttachmentStatus != "" {
		out["attachment_status"] = filter.AttachmentStatus
	}
	if filter.ArtifactKind != "" {
		out["artifact_kind"] = filter.ArtifactKind
	}
	return out
}

func isSupportedSBOMAttestationAttachmentDimension(d SBOMAttestationAttachmentInventoryDimension) bool {
	switch d {
	case SBOMAttestationAttachmentInventoryByAttachmentStatus,
		SBOMAttestationAttachmentInventoryByArtifactKind,
		SBOMAttestationAttachmentInventoryBySubjectDigest:
		return true
	default:
		return false
	}
}

// validateSBOMAttestationAttachmentAggregateFilters rejects out-of-contract
// attachment_status or artifact_kind values with a 400 so typos do not
// silently return zero counts. The closed enums match the values the list
// endpoint advertises in openapi_paths_supply_chain_sbom.go.
func validateSBOMAttestationAttachmentAggregateFilters(w http.ResponseWriter, filter SBOMAttestationAttachmentAggregateFilter) bool {
	if filter.AttachmentStatus != "" && !isSupportedSBOMAttachmentStatus(filter.AttachmentStatus) {
		querycontract.WriteError(w, http.StatusBadRequest, "attachment_status must be one of attached_verified, attached_unverified, attached_parse_only, subject_mismatch, ambiguous_subject, unknown_subject, unparseable")
		return false
	}
	if filter.ArtifactKind != "" && !isSupportedSBOMArtifactKind(filter.ArtifactKind) {
		querycontract.WriteError(w, http.StatusBadRequest, "artifact_kind must be one of sbom, attestation")
		return false
	}
	return true
}

const (
	sbomAttestationAttachmentAggregateDefaultLimit = 100
	sbomAttestationAttachmentAggregateMinLimit     = 1
	// sbomAttestationAttachmentAggregateMaxOffset matches the OpenAPI offset
	// bound and keeps Postgres OFFSET scans bounded. Past this point callers
	// should narrow scope (subject_digest, document_id, attachment_status,
	// artifact_kind) or fall back to the list endpoint with anchored
	// pagination.
	sbomAttestationAttachmentAggregateMaxOffset = 10000
)

func parseSBOMAttestationAttachmentAggregateLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		return sbomAttestationAttachmentAggregateDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed < sbomAttestationAttachmentAggregateMinLimit {
		querycontract.WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed > SBOMAttestationAttachmentAggregateMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, "limit exceeds maximum")
		return 0, false
	}
	return parsed, true
}

func parseSBOMAttestationAttachmentAggregateOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		querycontract.WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	if parsed > sbomAttestationAttachmentAggregateMaxOffset {
		querycontract.WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return 0, false
	}
	return parsed, true
}

// NextSBOMAttestationAttachmentAggregateOffset returns the next offset when a
// truncated page can be continued without exceeding the documented offset
// bound, and nil otherwise.
func NextSBOMAttestationAttachmentAggregateOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	next := offset + limit
	if next > sbomAttestationAttachmentAggregateMaxOffset {
		return nil
	}
	return next
}
