package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

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
