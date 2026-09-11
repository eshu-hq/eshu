// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"sort"
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
//
// A scoped caller's page is bound to its repository grant through
// ContainerImage-[:BUILT_FROM]->Repository (#6564), because the observation
// node itself carries no source-repository key: one extra single-clause read
// resolves the page's digests to their BUILT_FROM repositories and a Go join
// keeps a row only when its resolved_digest's image is built from a granted
// repository, blanking an ungranted previous_digest. Observations whose image
// has no BUILT_FROM edge are withheld from scoped callers. Filtering applies to
// each fetched page, so truncated and next_cursor follow the unfiltered window.
// Unscoped and all-scope callers keep the single-statement read. See
// tagHistoryBuiltFromCypher and filterTagHistoryForGrant below.
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

	page := tagHistoryPage{
		limit:        limit,
		offset:       offset,
		imageRef:     imageRef,
		repositoryID: repositoryID,
		tag:          tag,
	}

	// A scoped caller binds through its repository grant (#6564). Resolving
	// git-repository-scope grants to their canonical repository ids lets a
	// scope-only token match the Repository.id a BUILT_FROM edge lands on.
	// Unscoped and all-scope callers get the unfiltered single-statement read.
	access := repositoryAccessFilterFromContext(r.Context()).WithCanonicalScopeRepositories()
	page.grantFiltered = access.Scoped()
	if access.Empty() {
		// No grant can be BUILT_FROM-bound to any observation: answer an empty
		// page without a graph read, as every grant-bound sibling route does.
		tagHistoryGrantCounts{}.annotateSpan(span)
		recordTagHistoryDuration(r.Context(), start, "ok")
		h.writeTagHistoryPage(w, r, page)
		return
	}

	params := map[string]any{
		"image_ref": imageRef,
		"offset":    offset,
		"limit":     limit + 1,
	}

	rows, err := h.Neo4j.Run(r.Context(), tagHistoryCypher, params)
	if err != nil {
		writeTagHistoryReadError(w, r, start, err)
		return
	}

	// truncated and next_cursor are computed from the raw limit+1 read BEFORE
	// any grant filter, the contract the change-surface route documents in
	// impact_change_surface_traversal.go: a scoped page can hold fewer than
	// limit rows while still reporting truncation, rather than presenting a
	// grant-shortened page as the end of the history.
	page.truncated = len(rows) > limit
	if page.truncated {
		rows = rows[:limit]
	}

	page.history = make([]TagHistoryRow, 0, len(rows))
	for _, row := range rows {
		page.history = append(page.history, tagHistoryRowFromGraph(row))
	}

	if access.Scoped() {
		edges := map[string][]string{}
		if digests := tagHistoryGrantDigests(page.history); len(digests) > 0 {
			edges, err = h.lookupBuiltFromRepositories(r.Context(), digests)
			if err != nil {
				// Fail closed: never fall back to the unfiltered page.
				writeTagHistoryReadError(w, r, start, err)
				return
			}
		}
		var counts tagHistoryGrantCounts
		page.history, counts = filterTagHistoryForGrant(page.history, edges, access)
		counts.annotateSpan(span)
		recordTagHistoryScopedRows(r.Context(), counts)
	}

	recordTagHistoryDuration(r.Context(), start, "ok")
	h.writeTagHistoryPage(w, r, page)
}

// tagHistoryPage is one response page before serialization.
type tagHistoryPage struct {
	history       []TagHistoryRow
	limit         int
	offset        int
	truncated     bool
	imageRef      string
	repositoryID  string
	tag           string
	grantFiltered bool
}

// writeTagHistoryPage serializes page. grant_filtered is present only for a
// scoped caller, whose truth reason also discloses the BUILT_FROM coverage cost
// and the per-page filtering the pagination fields follow.
func (h *TagHistoryHandler) writeTagHistoryPage(w http.ResponseWriter, r *http.Request, page tagHistoryPage) {
	history := page.history
	if history == nil {
		history = []TagHistoryRow{}
	}
	body := map[string]any{
		"tag_history":   history,
		"count":         len(history),
		"limit":         page.limit,
		"offset":        page.offset,
		"truncated":     page.truncated,
		"image_ref":     page.imageRef,
		"repository_id": page.repositoryID,
		"tag":           page.tag,
	}
	if page.truncated {
		body["next_cursor"] = map[string]any{"offset": page.offset + page.limit}
	}
	reason := "resolved from bounded container image tag-observation history anchored on image_ref"
	if page.grantFiltered {
		body["grant_filtered"] = true
		reason = tagHistoryScopedTruthReason
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		tagHistoryCapability,
		TruthBasisAuthoritativeGraph,
		reason,
	))
}

// writeTagHistoryReadError writes the response for a failed graph read, shared
// by the tag read and the scoped BUILT_FROM lookup.
//
// "query_error" would be the wrong outcome label for a bounded
// backend-unavailable/backend-timeout sentinel, so the guard runs before that
// telemetry. It still records under the existing "backend_unavailable" outcome
// the h.Neo4j == nil branch uses, so a live graph outage or timeout keeps
// producing a handler-level datapoint instead of silently emitting none.
func writeTagHistoryReadError(w http.ResponseWriter, r *http.Request, start time.Time, err error) {
	if WriteGraphReadError(w, r, err, tagHistoryCapability) {
		recordTagHistoryError(r.Context(), "backend_unavailable")
		recordTagHistoryDuration(r.Context(), start, "backend_unavailable")
		return
	}
	recordTagHistoryError(r.Context(), "query_error")
	recordTagHistoryDuration(r.Context(), start, "query_error")
	WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
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

// tagHistoryBuiltFromCypher resolves which Repository nodes each digest on one
// fetched page was built from, so a scoped caller's page can be bound to its
// repository grant (#6564). The observation node carries no source-repository
// key; the only path to a code repository is resolved_digest joined to
// ContainerImage.digest and then the reducer's
// ContainerImage-[:BUILT_FROM]->Repository edge
// (canonicalProvenanceBuiltFromCypher, go/internal/storage/cypher).
//
// The join runs in Go on purpose: a two-MATCH grant join returned zero rows on
// the pinned NornicDB and on upstream v1.3.1 for a seed whose answer was two
// rows, while this single-clause read returned exactly the seeded edges on
// both (docs/internal/evidence/6564-tag-history-grant-binding.md).
//
// Anchor: the container_image_digest index. Keys: the page's distinct
// non-empty resolved and previous digests, at most 2*limit (400). Output: one
// row per BUILT_FROM edge on those digests. No LIMIT: truncating could drop a
// granted edge and silently withhold a row the caller is entitled to.
const tagHistoryBuiltFromCypher = `
	MATCH (i:ContainerImage)-[:BUILT_FROM]->(repo:Repository)
	WHERE i.digest IN $digests
	RETURN i.digest AS digest, repo.id AS repository_id
`

// tagHistoryScopedTruthReason is the truth-envelope reason a scoped caller
// receives: it discloses the BUILT_FROM coverage cost and the per-page
// filtering the pagination fields follow.
const tagHistoryScopedTruthReason = "resolved from bounded container image tag-observation history anchored on image_ref, " +
	"bound to the caller's repository grant through ContainerImage-[:BUILT_FROM]->Repository: a row is kept only when " +
	"the image at its resolved_digest is BUILT_FROM a granted repository, previous_digest is blanked unless that image is " +
	"also BUILT_FROM a granted repository, and observations whose image has no BUILT_FROM edge are withheld; filtering " +
	"applies to each fetched page, so count can be below limit while truncated and next_cursor advance over the unfiltered window"

// tagHistoryGrantDigests returns the page's distinct non-empty resolved and
// previous digests, sorted so the lookup's parameters are deterministic.
func tagHistoryGrantDigests(rows []TagHistoryRow) []string {
	seen := make(map[string]struct{}, 2*len(rows))
	for _, row := range rows {
		if row.ResolvedDigest != "" {
			seen[row.ResolvedDigest] = struct{}{}
		}
		if row.PreviousDigest != "" {
			seen[row.PreviousDigest] = struct{}{}
		}
	}
	digests := make([]string, 0, len(seen))
	for digest := range seen {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	return digests
}

// lookupBuiltFromRepositories runs tagHistoryBuiltFromCypher for digests and
// returns each digest's BUILT_FROM repository ids. A digest absent from the
// map has no BUILT_FROM edge.
func (h *TagHistoryHandler) lookupBuiltFromRepositories(ctx context.Context, digests []string) (map[string][]string, error) {
	rows, err := h.Neo4j.Run(ctx, tagHistoryBuiltFromCypher, map[string]any{"digests": digests})
	if err != nil {
		return nil, err
	}
	edges := make(map[string][]string, len(digests))
	for _, row := range rows {
		digest := StringVal(row, "digest")
		repositoryID := StringVal(row, "repository_id")
		if digest == "" || repositoryID == "" {
			continue
		}
		edges[digest] = append(edges[digest], repositoryID)
	}
	return edges, nil
}

// tagHistoryAnyRepositoryGranted reports whether at least one repository an
// image was built from is in the caller's grant.
func tagHistoryAnyRepositoryGranted(repositoryIDs []string, access repositoryAccessFilter) bool {
	for _, repositoryID := range repositoryIDs {
		if access.AllowsRepositoryID(repositoryID) {
			return true
		}
	}
	return false
}

// filterTagHistoryForGrant keeps a row only when the image at its
// resolved_digest is BUILT_FROM at least one granted repository, and blanks
// previous_digest unless that digest's image is also BUILT_FROM a granted
// repository, so a scoped caller never learns another tenant's digest. Row
// order is preserved.
func filterTagHistoryForGrant(
	rows []TagHistoryRow,
	edges map[string][]string,
	access repositoryAccessFilter,
) ([]TagHistoryRow, tagHistoryGrantCounts) {
	var counts tagHistoryGrantCounts
	kept := make([]TagHistoryRow, 0, len(rows))
	for _, row := range rows {
		repositoryIDs, attributed := edges[row.ResolvedDigest]
		if !attributed {
			counts.withheldUnattributed++
			continue
		}
		if !tagHistoryAnyRepositoryGranted(repositoryIDs, access) {
			counts.withheldUngranted++
			continue
		}
		if row.PreviousDigest != "" && !tagHistoryAnyRepositoryGranted(edges[row.PreviousDigest], access) {
			row.PreviousDigest = ""
			counts.previousDigestBlanked++
		}
		kept = append(kept, row)
	}
	counts.kept = len(kept)
	return kept, counts
}
