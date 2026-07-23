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
	imageListCapability = "platform_impact.container_image_list"
	imageListMaxLimit   = 200
	imageListDefaultLim = 50
)

// imageListCypher lists container images over the authoritative graph.
//
// Anchor: a label scan over (:ContainerImage). The label population is small
// and bounded (image inventory, not per-layer descriptors), and the result is
// further bounded by limit+1, so a label scan is the correct shape here rather
// than an unindexed property anchor. Optional filters narrow on indexed digest,
// registry/repository_id, and source_tag before the deterministic ORDER BY.
// ORDER BY digest, uid is deterministic because uid is unique per image node.
const imageListCypher = `
	MATCH (img:ContainerImage)
	WHERE ($digest = '' OR img.digest = $digest)
	  AND ($repository_id = '' OR img.repository_id = $repository_id)
	  AND ($tag = '' OR img.source_tag = $tag)
	RETURN img.id AS id,
	       img.uid AS uid,
	       img.digest AS digest,
	       img.repository_id AS repository_id,
	       img.name AS name,
	       img.source_tag AS source_tag,
	       img.media_type AS media_type,
	       img.artifact_type AS artifact_type,
	       img.config_digest AS config_digest,
	       img.size_bytes AS size_bytes,
	       img.source_system AS source_system
	ORDER BY img.digest, img.uid
	SKIP $offset
	LIMIT $limit
`

// ImageHandler exposes the bounded container-image (OCI) list read that backs
// the console Images browse surface. It reads the authoritative graph through
// the GraphQuery port; it owns no backend driver.
type ImageHandler struct {
	Neo4j   GraphQuery
	Profile QueryProfile
}

// ImageRow is one container image projected for the list surface. Only graph
// properties that exist on (:ContainerImage) are surfaced; absent fields are
// omitted rather than invented.
type ImageRow struct {
	ID           string `json:"id"`
	Digest       string `json:"digest,omitempty"`
	RepositoryID string `json:"repository_id,omitempty"`
	Registry     string `json:"registry,omitempty"`
	Repository   string `json:"repository,omitempty"`
	Name         string `json:"name,omitempty"`
	Tag          string `json:"tag,omitempty"`
	MediaType    string `json:"media_type,omitempty"`
	ArtifactType string `json:"artifact_type,omitempty"`
	ConfigDigest string `json:"config_digest,omitempty"`
	SizeBytes    int    `json:"size_bytes,omitempty"`
	SourceSystem string `json:"source_system,omitempty"`
}

// Mount registers the container-image list route.
func (h *ImageHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/images", h.listImages)
}

func (h *ImageHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

func (h *ImageHandler) listImages(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryContainerImageList,
		"GET /api/v0/images",
		imageListCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), imageListCapability) {
		recordImageListError(r.Context(), "unsupported_capability")
		recordImageListDuration(r.Context(), start, "unsupported_capability")
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"container image list requires authoritative graph truth",
			ErrorCodeUnsupportedCapability,
			imageListCapability,
			h.profile(),
			requiredProfile(imageListCapability),
		)
		return
	}

	limit, offset, ok := imageListBounds(w, r)
	if !ok {
		recordImageListError(r.Context(), "invalid_request")
		recordImageListDuration(r.Context(), start, "invalid_request")
		return
	}

	if h.Neo4j == nil {
		recordImageListError(r.Context(), "backend_unavailable")
		recordImageListDuration(r.Context(), start, "backend_unavailable")
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"container image list requires the authoritative graph backend",
			ErrorCodeBackendUnavailable,
			imageListCapability,
			h.profile(),
			requiredProfile(imageListCapability),
		)
		return
	}

	params := map[string]any{
		"digest":        QueryParam(r, "digest"),
		"repository_id": QueryParam(r, "repository_id"),
		"tag":           QueryParam(r, "tag"),
		"offset":        offset,
		"limit":         limit + 1,
	}

	rows, err := h.Neo4j.Run(r.Context(), imageListCypher, params)
	if err != nil {
		// The graph-read-availability guard runs before the telemetry below:
		// "query_error" would be the wrong outcome label for a bounded
		// backend-unavailable/backend-timeout sentinel (see WriteGraphReadError).
		if WriteGraphReadError(w, r, err, imageListCapability) {
			return
		}
		recordImageListError(r.Context(), "query_error")
		recordImageListDuration(r.Context(), start, "query_error")
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}

	images := make([]ImageRow, 0, len(rows))
	for _, row := range rows {
		images = append(images, imageRowFromGraph(row))
	}

	body := map[string]any{
		"images":    images,
		"count":     len(images),
		"limit":     limit,
		"offset":    offset,
		"truncated": truncated,
	}
	if truncated {
		body["next_cursor"] = map[string]any{"offset": offset + limit}
	}

	recordImageListDuration(r.Context(), start, "ok")
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		imageListCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from bounded container image graph inventory",
	))
}

// imageListBounds parses and validates the required limit and optional offset.
// It writes a 400 and returns ok=false on invalid input.
func imageListBounds(w http.ResponseWriter, r *http.Request) (limit int, offset int, ok bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		limit = imageListDefaultLim
	} else {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > imageListMaxLimit {
			WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", imageListMaxLimit))
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

// imageRowFromGraph projects one graph row into an ImageRow, deriving the
// registry host and repository path from the OCI repository_id when present.
func imageRowFromGraph(row map[string]any) ImageRow {
	repoID := StringVal(row, "repository_id")
	registry, repository := splitOCIRepositoryID(repoID)
	return ImageRow{
		ID:           StringVal(row, "id"),
		Digest:       StringVal(row, "digest"),
		RepositoryID: repoID,
		Registry:     registry,
		Repository:   repository,
		Name:         StringVal(row, "name"),
		Tag:          StringVal(row, "source_tag"),
		MediaType:    StringVal(row, "media_type"),
		ArtifactType: StringVal(row, "artifact_type"),
		ConfigDigest: StringVal(row, "config_digest"),
		SizeBytes:    IntVal(row, "size_bytes"),
		SourceSystem: StringVal(row, "source_system"),
	}
}

// splitOCIRepositoryID parses an OCI repository_id such as
// "oci-registry://host/path/to/repo" into its registry host and repository
// path. It returns empty strings when the id does not carry that shape so the
// surface never invents identity it cannot derive.
func splitOCIRepositoryID(repoID string) (registry string, repository string) {
	if repoID == "" {
		return "", ""
	}
	rest := repoID
	if idx := strings.Index(rest, "://"); idx >= 0 {
		rest = rest[idx+len("://"):]
	}
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return "", ""
	}
	host, path, found := strings.Cut(rest, "/")
	if !found {
		// No path segment: treat the whole value as the repository path.
		return "", host
	}
	return host, path
}
