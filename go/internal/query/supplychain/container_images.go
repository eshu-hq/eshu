// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *SupplyChainHandler) listContainerImageIdentities(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryContainerImageIdentities,
		"GET /api/v0/supply-chain/container-images/identities",
		ContainerImageIdentitiesCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), ContainerImageIdentitiesCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"container image identities require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			ContainerImageIdentitiesCapability,
			h.profile(),
			querycontract.RequiredProfile(ContainerImageIdentitiesCapability),
		)
		return
	}
	limit, ok := requiredContainerImageIdentityLimit(w, r)
	if !ok {
		return
	}
	// Empty scoped grants return the zero-identities page without resolving a
	// selector or reading the identity store.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptyContainerImageIdentityPage(w, r, limit)
		return
	}
	sourceRepositoryID, ok := h.resolveContainerImageSourceRepositorySelector(w, r, querycontract.QueryParam(r, "source_repository_id"), access, ContainerImageIdentitiesCapability)
	if !ok {
		return
	}
	filter := ContainerImageIdentityFilter{
		Digest:                     querycontract.QueryParam(r, "digest"),
		ImageRef:                   querycontract.QueryParam(r, "image_ref"),
		SourceRepositoryID:         sourceRepositoryID,
		RepositoryID:               querycontract.QueryParam(r, "repository_id"),
		Outcome:                    querycontract.QueryParam(r, "outcome"),
		AfterIdentityID:            querycontract.QueryParam(r, "after_identity_id"),
		Limit:                      limit + 1,
		AllowedSourceRepositoryIDs: access.RepositorySearchIDs(),
	}
	if !filter.HasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "digest, image_ref, source_repository_id, repository_id, or outcome is required")
		return
	}
	if filter.Outcome != "" && !isSupportedContainerImageIdentityOutcome(filter.Outcome) {
		querycontract.WriteError(w, http.StatusBadRequest, "outcome must be exact_digest or tag_resolved")
		return
	}
	if h.ContainerImageIdentities == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"container image identities require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			ContainerImageIdentitiesCapability,
			h.profile(),
			querycontract.RequiredProfile(ContainerImageIdentitiesCapability),
		)
		return
	}

	rows, err := h.ContainerImageIdentities.ListContainerImageIdentities(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
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
	if filter.SourceRepositoryID != "" {
		body["source_bridge"] = BuildContainerImageIdentitySourceBridge(filter.SourceRepositoryID, results)
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_identity_id": results[len(results)-1].IdentityID,
		}
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorOCIRegistry, len(results), truncated)
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		ContainerImageIdentitiesCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned container image identity facts; weak, ambiguous, unresolved, and stale tags remain diagnostic reducer outcomes",
	))
}

// BuildContainerImageIdentitySourceBridge summarizes source-repository-scoped
// image identity evidence. Exported for the staying source-bridge test via
// root's forward; the list handler is the production caller.
func BuildContainerImageIdentitySourceBridge(
	sourceRepositoryID string,
	rows []ContainerImageIdentityResult,
) ContainerImageIdentitySourceBridge {
	bridge := ContainerImageIdentitySourceBridge{SourceRepositoryID: sourceRepositoryID}
	var imageRepositoryIDs []string
	hasDeploymentImageReference := false
	hasRegistryObservation := false
	hasSourceCorrelation := false
	for _, row := range rows {
		imageRepositoryIDs = append(imageRepositoryIDs, row.RepositoryID)
		if row.ImageRef != "" {
			hasDeploymentImageReference = true
		}
		if row.Digest != "" && row.RepositoryID != "" {
			hasRegistryObservation = true
		}
		if slices.Contains(row.SourceRepositoryIDs, sourceRepositoryID) {
			hasSourceCorrelation = true
		}
	}
	bridge.ImageRepositoryIDs = UniqueSortedNonEmpty(imageRepositoryIDs)
	if len(bridge.ImageRepositoryIDs) > 1 {
		bridge.Warnings = append(bridge.Warnings, "ambiguous_image_repository")
	}
	if len(rows) == 0 {
		bridge.MissingEvidence = []string{
			"deployment_image_reference_missing",
			"image_registry_observation_missing",
			"source_to_image_correlation_missing",
		}
		return bridge
	}
	if !hasDeploymentImageReference {
		bridge.MissingEvidence = append(bridge.MissingEvidence, "deployment_image_reference_missing")
	}
	if !hasRegistryObservation {
		bridge.MissingEvidence = append(bridge.MissingEvidence, "image_registry_observation_missing")
	}
	if !hasSourceCorrelation {
		bridge.MissingEvidence = append(bridge.MissingEvidence, "source_to_image_correlation_missing")
	}
	bridge.MissingEvidence = UniqueSortedNonEmpty(bridge.MissingEvidence)
	return bridge
}

func requiredContainerImageIdentityLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > ContainerImageIdentityMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", ContainerImageIdentityMaxLimit))
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
