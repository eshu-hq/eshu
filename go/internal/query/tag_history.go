// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/taghistory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	tagHistoryCapability = "platform_impact.container_image_tag_history"
	tagHistoryDefaultLim = 50
)

// tagHistoryMaxLimit is the largest page a caller may request. The bound lives
// with the statements it bounds (taghistory.MaxLimit) so the BUILT_FROM key
// cap derived from it cannot drift away from the limit check that feeds it.
const tagHistoryMaxLimit = taghistory.MaxLimit

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
// resolves each window's digests to their BUILT_FROM repositories and a Go join
// keeps a row only when its resolved_digest's image is built from a granted
// repository, blanking an ungranted previous_digest. Observations whose image
// has no BUILT_FROM edge are withheld from scoped callers.
//
// A scoped page is REFILLED across successive FIXED taghistory.MaxLimit-sized
// windows until it holds limit visible rows, the history ends, or
// taghistory.MaxRefillReads windows have been read, and it continues through a
// SEALED KEYSET cursor. All three halves exist so that neither the count nor
// the token measures another tenant's withheld history: the token names a row
// key rather than a position, the scanned span is a constant 800 raw rows
// instead of 4*limit, and the seal stops the caller choosing where that span
// starts. What remains is a COUNT and never an identity, disclosed rather than
// hidden. Unscoped and all-scope callers keep the offset parameter and the SKIP
// statement. taghistory.Cursor carries the cursor contract in full;
// taghistory.RefillScopedPage carries the refill loop's.
type TagHistoryHandler struct {
	Neo4j   GraphQuery
	Profile QueryProfile
	// Cursors seals and opens the continuation token: the deployment DEK
	// (*secretcrypto.Keyring, ESHU_AUTH_SECRET_ENC_KEY(_FILE)), wired in
	// cmd/api/wiring.go and cmd/mcp-server/wiring.go behind an explicit
	// non-nil guard (a nil *Keyring in this interface is a NON-nil Sealer that
	// panics on first use).
	//
	// Nil means no DEK is configured. Grant-filtered paging then fails closed
	// per taghistory.Sealer; callers with nothing withheld are unaffected.
	Cursors taghistory.Sealer
}

// TagHistoryRow is one captured tag observation for the selected image_ref.
// It is an alias so the wire shape has exactly one definition, in the package
// that reads it off the graph.
type TagHistoryRow = taghistory.Row

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

	// A scoped caller binds through its repository grant (#6564). Resolving
	// git-repository-scope grants to their canonical repository ids lets a
	// scope-only token match the Repository.id a BUILT_FROM edge lands on.
	// Unscoped and all-scope callers get the unfiltered single-statement read.
	// This is settled before the page selectors are parsed because it decides
	// whether a raw offset is accepted at all (tagHistoryBounds).
	access := repositoryAccessFilterFromContext(r.Context()).WithCanonicalScopeRepositories()

	limit, after, offset, refusal := h.tagHistoryBounds(w, r, imageRef, access.Scoped())
	if refusal != "" {
		recordTagHistoryError(r.Context(), refusal)
		recordTagHistoryDuration(r.Context(), start, refusal)
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

	page.grantFiltered = access.Scoped()
	if access.Empty() {
		// No grant can be BUILT_FROM-bound to any observation: answer an empty
		// page without a graph read, as every grant-bound sibling route does.
		annotateTagHistoryGrantCounts(span, taghistory.GrantCounts{})
		h.completeTagHistoryPage(w, r, start, page)
		return
	}

	if !access.Scoped() {
		// One read, no refill: there is nothing to filter out, so the page is
		// already what the caller asked for. A cursor continues by key; the
		// offset parameter keeps its SKIP statement.
		window, more, err := h.readUnscopedWindow(r, imageRef, after, offset, limit)
		if err != nil {
			writeTagHistoryReadError(w, r, start, err)
			return
		}
		page.history = taghistory.Rows(window)
		page.truncated = more
		if len(window) > 0 {
			page.nextKey = &window[len(window)-1].Key
		}
		h.completeTagHistoryPage(w, r, start, page)
		return
	}

	// A scoped page is refilled to limit VISIBLE rows across successive
	// windows (#6564 review finding 1), so count no longer measures what the
	// grant filter withheld. See taghistory.RefillScopedPage.
	scoped, err := taghistory.RefillScopedPage(r.Context(), h.Neo4j, imageRef, after, limit, access)
	if err != nil {
		writeTagHistoryReadError(w, r, start, err)
		return
	}
	page.history = scoped.Rows
	page.truncated = scoped.Truncated
	page.nextKey = scoped.NextKey
	annotateTagHistoryGrantCounts(span, scoped.Counts)
	annotateTagHistoryRefill(span, scoped.Reads, scoped.CapReached)
	recordTagHistoryScopedRows(r.Context(), scoped.Counts)

	h.completeTagHistoryPage(w, r, start, page)
}

// tagHistoryPage is one response page before serialization. nextKey is the
// keyset position the read stopped on, nil when the history ended; it reaches
// the wire only as the cursor token (taghistory.EncodeCursor).
type tagHistoryPage struct {
	history       []TagHistoryRow
	limit         int
	offset        int
	nextKey       *taghistory.Key
	truncated     bool
	imageRef      string
	repositoryID  string
	tag           string
	grantFiltered bool
}

// completeTagHistoryPage is the single exit of every successful read path. It
// seals the continuation, records the outcome that sealing produced, and writes
// the page.
//
// Sealing happens HERE rather than inside writeTagHistoryPage because a sealing
// failure must become a 500 with the query_error outcome, and the duration
// outcome is recorded before the body is written.
func (h *TagHistoryHandler) completeTagHistoryPage(
	w http.ResponseWriter,
	r *http.Request,
	start time.Time,
	page tagHistoryPage,
) {
	token, unavailable, err := h.tagHistoryContinuation(page)
	if err != nil {
		recordTagHistoryError(r.Context(), "query_error")
		recordTagHistoryDuration(r.Context(), start, "query_error")
		// The message is fixed: a sealing failure is an operator fault, and
		// echoing the crypto error would describe the deployment's key state
		// to a caller.
		WriteError(w, http.StatusInternalServerError, "query failed: the tag-history continuation could not be sealed")
		return
	}
	outcome := "ok"
	if unavailable {
		outcome = tagHistoryOutcomeCursorUnavailable
	}
	recordTagHistoryDuration(r.Context(), start, outcome)
	h.writeTagHistoryPage(w, r, page, token, unavailable)
}

// tagHistoryContinuation seals page's continuation token.
//
// It returns an empty token with unavailable=false when the page simply has no
// continuation, and unavailable=true when a grant-filtered page owes one but
// the deployment holds no sealing key -- the fail-closed case, which omits
// next_cursor rather than serving a token a caller could re-aim. The error is a
// real sealing failure and never a reason to fall back to cleartext.
//
// A truncated page always has a key to continue from: the nil guard is a belt
// against a future path that sets truncated without one, not a case that can
// happen today.
func (h *TagHistoryHandler) tagHistoryContinuation(page tagHistoryPage) (token string, unavailable bool, err error) {
	if !page.truncated || page.nextKey == nil {
		return "", false, nil
	}
	if page.grantFiltered && h.Cursors == nil {
		return "", true, nil
	}
	token, err = taghistory.EncodeCursor(h.Cursors, page.imageRef, *page.nextKey)
	if err != nil {
		return "", false, err
	}
	return token, false, nil
}

// writeTagHistoryPage serializes page. grant_filtered is present only for a
// scoped caller, whose truth reason also discloses the BUILT_FROM coverage
// cost, the refilled pagination, the sealed cursor-only continuation, and --
// when unavailable is set -- that this deployment cannot issue one at all.
//
// offset is echoed only to a caller that may send one. A grant-filtered page
// omits it and continues through next_cursor alone, because a raw row position
// on such a page is the pre-filter frontier the refill advanced to
// (taghistory.Cursor).
func (h *TagHistoryHandler) writeTagHistoryPage(
	w http.ResponseWriter,
	r *http.Request,
	page tagHistoryPage,
	token string,
	unavailable bool,
) {
	history := page.history
	if history == nil {
		history = []TagHistoryRow{}
	}
	body := map[string]any{
		"tag_history":   history,
		"count":         len(history),
		"limit":         page.limit,
		"truncated":     page.truncated,
		"image_ref":     page.imageRef,
		"repository_id": page.repositoryID,
		"tag":           page.tag,
	}
	if !page.grantFiltered {
		body["offset"] = page.offset
	}
	if token != "" {
		body["next_cursor"] = token
	}
	reason := "resolved from bounded container image tag-observation history anchored on image_ref"
	if page.grantFiltered {
		body["grant_filtered"] = true
		reason = taghistory.ScopedTruthReason
		if unavailable {
			reason += taghistory.CursorUnavailableReason
		}
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

// tagHistoryBounds parses and validates the page selectors: the required
// limit, the optional cursor, and the optional raw offset. A non-nil key means
// continue from that keyset position; a nil key with a non-zero offset means
// the SKIP path.
//
// It writes the refusal itself and returns the bounded outcome label to record
// for it, or "" when the selectors are usable. Two refusals exist: a 400 for
// input the caller can fix, and a 503 for the one case it cannot -- a
// grant-filtered caller presenting a cursor on a deployment that holds no
// sealing key. That is a configuration gap, so it is reported as a degraded
// capability naming the variable to set, never as caller error and never by
// silently opening an unsealed token.
//
// cursor is the continuation every caller should follow, and the ONLY one a
// grant-filtered caller may use. A scoped caller supplying a non-zero raw
// offset is refused: that parameter is the PRE-FILTER row position, so
// accepting it would hand back through a documented parameter exactly the
// position-addressing the keyset cursor removed. offset=0 stays legal for
// everyone because it names the start of the history and discloses nothing; it
// keeps the MCP route, which always sends an offset, working unchanged.
//
// Unscoped and all-scope callers keep the offset contract they already have.
func (h *TagHistoryHandler) tagHistoryBounds(
	w http.ResponseWriter,
	r *http.Request,
	imageRef string,
	scoped bool,
) (limit int, after *taghistory.Key, offset int, refusal string) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		limit = tagHistoryDefaultLim
	} else {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > tagHistoryMaxLimit {
			WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", tagHistoryMaxLimit))
			return 0, nil, 0, "invalid_request"
		}
		limit = n
	}

	rawOffset := QueryParam(r, "offset")
	if rawOffset != "" {
		n, err := strconv.Atoi(rawOffset)
		if err != nil || n < 0 {
			WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return 0, nil, 0, "invalid_request"
		}
		offset = n
	}

	rawCursor := QueryParam(r, "cursor")
	if rawCursor != "" {
		if offset != 0 {
			WriteError(w, http.StatusBadRequest, "cursor and a non-zero offset are mutually exclusive; continue with cursor alone")
			return 0, nil, 0, "invalid_request"
		}
		if scoped && h.Cursors == nil {
			WriteContractError(
				w,
				r,
				http.StatusServiceUnavailable,
				taghistory.ErrCursorSealingUnavailable.Error(),
				ErrorCodeCapabilityDegraded,
				tagHistoryCapability,
				h.profile(),
				requiredProfile(tagHistoryCapability),
			)
			return 0, nil, 0, tagHistoryOutcomeCursorUnavailable
		}
		key, err := taghistory.DecodeCursor(h.Cursors, rawCursor, imageRef)
		if err != nil {
			// The detail is safe to echo: on a sealed token every refusal
			// before this point is secretcrypto's single opaque ErrDecrypt, and
			// what follows can only fire on a payload this server sealed.
			WriteError(w, http.StatusBadRequest, fmt.Sprintf("cursor is not a usable continuation for this request: %v", err))
			return 0, nil, 0, "invalid_request"
		}
		return limit, &key, 0, ""
	}

	if scoped && offset != 0 {
		WriteError(
			w,
			http.StatusBadRequest,
			"offset is not accepted for a grant-filtered caller; continue a truncated page with the next_cursor token it returned",
		)
		return 0, nil, 0, "invalid_request"
	}

	return limit, nil, offset, ""
}

// readUnscopedWindow serves one page for a caller with no grant to bind. A
// cursor continues by key through the same statements a scoped page uses; the
// offset parameter keeps the SKIP statement it has always had, so the contract
// TestTagHistoryUnscopedCallerKeepsOffsetPaging pins is unchanged.
func (h *TagHistoryHandler) readUnscopedWindow(
	r *http.Request,
	imageRef string,
	after *taghistory.Key,
	offset, limit int,
) ([]taghistory.WindowRow, bool, error) {
	if after != nil {
		return taghistory.ReadWindow(r.Context(), h.Neo4j, imageRef, after, limit)
	}
	return taghistory.ReadOffsetWindow(r.Context(), h.Neo4j, imageRef, offset, limit)
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
