// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	tagHistoryCapability = "platform_impact.container_image_tag_history"
	tagHistoryMaxLimit   = 200
	tagHistoryDefaultLim = 50
)

// tagHistoryCypher lists one image_ref's captured ContainerImageTagObservation
// history over the authoritative graph.
//
// Anchor: the existing container_image_tag_observation_ref index over
// image_ref (see go/internal/storage/cypher), so this is an indexed
// equality lookup, not a label scan. The result is fully deterministic because
// the trailing t.uid key is unique per observation node and breaks every tie,
// independent of backend null-ordering.
// first_observed_at may be empty or null for an observation whose envelope
// carried a zero ObservedAt (ON CREATE SET stores "" via ociTagObservedAtValue
// in go/internal/storage/cypher) or one created before #5459 shipped
// first_observed_at. Such rows are retained (never dropped); their
// position relative to timestamped rows follows the backend's native
// null-ordering (NornicDB and Neo4j sort nulls last on ascending ORDER BY) and
// is not relied upon for correctness — the uid tiebreak fixes the total order.
const tagHistoryCypher = `
	MATCH (t:ContainerImageTagObservation {image_ref: $image_ref})
	RETURN t.tag AS tag,
	       t.resolved_digest AS resolved_digest,
	       t.previous_digest AS previous_digest,
	       t.mutated AS mutated,
	       t.first_observed_at AS first_observed_at,
	       t.repository_id AS repository_id,
	       t.identity_strength AS identity_strength,
	       t.uid AS uid
	ORDER BY t.first_observed_at, t.uid
	SKIP $offset
	LIMIT $limit
`

// TagHistoryHandler exposes the bounded, ordered read of one OCI image_ref's
// captured tag-mutation history (issue #5459): what digest a repository:tag
// was first observed as, and the order its digests changed. It reads the
// authoritative graph through the GraphQuery port; it owns no backend driver.
//
// Two limitations follow from how ContainerImageTagObservation identity and
// first_observed_at are constructed, and are surfaced here rather than left
// implicit:
//
//  1. Identity is keyed by (repository_id, tag, resolved_digest), so a tag
//     that flips back to a previously-observed digest (A -> B -> A) collapses
//     onto the SAME node it originally created. The "order digests changed"
//     answer this handler returns is therefore bounded by the distinct-digest
//     set the collector has observed for the tag, not a full chronological
//     event log of every transition.
//  2. first_observed_at is written with ON CREATE SET in the identity MERGE
//     (see canonicalOCIImageTagObservationUpsertCypher in
//     go/internal/storage/cypher) that holds the FIRST projected observation
//     and never regresses under later or out-of-order re-projection. A
//     back-dated observation arriving after a later one is not reflected.
//     True per-event history and a last_observed_at companion are tracked as
//     follow-up work, not implemented here.
type TagHistoryHandler struct {
	Neo4j   GraphQuery
	Profile QueryProfile
}

// TagHistoryRow is one captured tag observation for the selected image_ref.
type TagHistoryRow struct {
	Tag              string `json:"tag"`
	ResolvedDigest   string `json:"resolved_digest"`
	PreviousDigest   string `json:"previous_digest,omitempty"`
	Mutated          bool   `json:"mutated"`
	FirstObservedAt  string `json:"first_observed_at,omitempty"`
	RepositoryID     string `json:"repository_id"`
	IdentityStrength string `json:"identity_strength,omitempty"`
}

// Mount registers the tag-history route.
func (h *TagHistoryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/images/tag-history", h.listTagHistory)
}

func (h *TagHistoryHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

func (h *TagHistoryHandler) listTagHistory(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryContainerImageTagHistory,
		"GET /api/v0/images/tag-history",
		tagHistoryCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), tagHistoryCapability) {
		recordTagHistoryError(r.Context(), "unsupported_capability")
		recordTagHistoryDuration(r.Context(), start, "unsupported_capability")
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"container image tag history requires authoritative graph truth",
			ErrorCodeUnsupportedCapability,
			tagHistoryCapability,
			h.profile(),
			requiredProfile(tagHistoryCapability),
		)
		return
	}

	repositoryID := QueryParam(r, "repository_id")
	tag := QueryParam(r, "tag")
	imageRef := composeOCIImageRef(repositoryID, tag)
	if imageRef == "" {
		recordTagHistoryError(r.Context(), "invalid_request")
		recordTagHistoryDuration(r.Context(), start, "invalid_request")
		WriteError(w, http.StatusBadRequest, "repository_id and tag are required and repository_id must be an oci-registry:// id")
		return
	}

	limit, offset, ok := tagHistoryBounds(w, r)
	if !ok {
		recordTagHistoryError(r.Context(), "invalid_request")
		recordTagHistoryDuration(r.Context(), start, "invalid_request")
		return
	}

	if h.Neo4j == nil {
		recordTagHistoryError(r.Context(), "backend_unavailable")
		recordTagHistoryDuration(r.Context(), start, "backend_unavailable")
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"container image tag history requires the authoritative graph backend",
			ErrorCodeBackendUnavailable,
			tagHistoryCapability,
			h.profile(),
			requiredProfile(tagHistoryCapability),
		)
		return
	}

	params := map[string]any{
		"image_ref": imageRef,
		"offset":    offset,
		"limit":     limit + 1,
	}

	rows, err := h.Neo4j.Run(r.Context(), tagHistoryCypher, params)
	if err != nil {
		// "query_error" would be the wrong outcome label for a bounded
		// backend-unavailable/backend-timeout sentinel, so the guard runs
		// before that telemetry. It still records under the existing
		// "backend_unavailable" outcome the h.Neo4j == nil branch above uses,
		// so a live graph outage or timeout keeps producing a handler-level
		// datapoint instead of silently emitting none.
		if WriteGraphReadError(w, r, err, tagHistoryCapability) {
			recordTagHistoryError(r.Context(), "backend_unavailable")
			recordTagHistoryDuration(r.Context(), start, "backend_unavailable")
			return
		}
		recordTagHistoryError(r.Context(), "query_error")
		recordTagHistoryDuration(r.Context(), start, "query_error")
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}

	history := make([]TagHistoryRow, 0, len(rows))
	for _, row := range rows {
		history = append(history, tagHistoryRowFromGraph(row))
	}

	body := map[string]any{
		"tag_history":   history,
		"count":         len(history),
		"limit":         limit,
		"offset":        offset,
		"truncated":     truncated,
		"image_ref":     imageRef,
		"repository_id": repositoryID,
		"tag":           tag,
	}
	if truncated {
		body["next_cursor"] = map[string]any{"offset": offset + limit}
	}

	recordTagHistoryDuration(r.Context(), start, "ok")
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		tagHistoryCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from bounded container image tag-observation history anchored on image_ref",
	))
}

// tagHistoryBounds parses and validates the required limit and optional
// offset. It writes a 400 and returns ok=false on invalid input.
func tagHistoryBounds(w http.ResponseWriter, r *http.Request) (limit int, offset int, ok bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		limit = tagHistoryDefaultLim
	} else {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > tagHistoryMaxLimit {
			WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", tagHistoryMaxLimit))
			return 0, 0, false
		}
		limit = n
	}

	rawOffset := QueryParam(r, "offset")
	if rawOffset != "" {
		n, err := strconv.Atoi(rawOffset)
		if err != nil || n < 0 {
			WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return 0, 0, false
		}
		offset = n
	}

	return limit, offset, true
}

// tagHistoryRowFromGraph projects one graph row into a TagHistoryRow.
func tagHistoryRowFromGraph(row map[string]any) TagHistoryRow {
	return TagHistoryRow{
		Tag:              StringVal(row, "tag"),
		ResolvedDigest:   StringVal(row, "resolved_digest"),
		PreviousDigest:   StringVal(row, "previous_digest"),
		Mutated:          BoolVal(row, "mutated"),
		FirstObservedAt:  StringVal(row, "first_observed_at"),
		RepositoryID:     StringVal(row, "repository_id"),
		IdentityStrength: StringVal(row, "identity_strength"),
	}
}

// composeOCIImageRef mirrors the projector's ociImageRef idiom (duplicated
// rather than imported: the query package does not depend on projector). It
// returns "" when repositoryID lacks the oci-registry:// prefix or tag is
// empty, so a malformed selector is never silently coerced into a valid ref
// that would return an empty-but-200 page instead of a 400.
func composeOCIImageRef(repositoryID, tag string) string {
	repositoryID = strings.TrimSpace(repositoryID)
	tag = strings.TrimSpace(tag)
	if strings.HasPrefix(repositoryID, "oci-registry://") && tag != "" {
		return strings.TrimPrefix(repositoryID, "oci-registry://") + ":" + tag
	}
	return ""
}
