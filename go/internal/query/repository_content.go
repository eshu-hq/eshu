// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

// repositoryContentMaxBytes bounds the number of bytes returned by the
// repository content endpoint. Files larger than this are truncated (on a UTF-8
// rune boundary when text) and flagged with truncated=true so the UI can offer a
// "view full file" affordance instead of streaming arbitrarily large blobs.
const repositoryContentMaxBytes = 1 << 20 // 1 MiB

// getRepositoryContent returns the indexed bytes of a single repository file
// from the Postgres content store. Text is returned as utf-8; bytes that are not
// valid UTF-8 are base64-encoded. The response reports the original byte size
// and whether the content was truncated to the byte cap.
//
// GET /api/v0/repositories/{repo_id}/content?path={filepath}&ref={ref}
func (h *RepositoryHandler) getRepositoryContent(w http.ResponseWriter, r *http.Request) {
	repoID, ok := h.resolveRepositoryPathSelector(w, r, "code_search.content_search")
	if !ok {
		return
	}

	relativePath := r.URL.Query().Get("path")
	if relativePath == "" {
		WriteError(w, http.StatusBadRequest, "path is required")
		return
	}
	requestedRef := strings.TrimSpace(r.URL.Query().Get("ref"))

	ctx := r.Context()
	repoRef, _, err := h.repositoryStatsRepositoryRef(ctx, repoID)
	if err != nil {
		if WriteGraphReadError(w, r, err, "code_search.content_search") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query repository failed: %v", err))
		return
	}
	if repoRef == nil {
		WriteError(w, http.StatusNotFound, "repository not found")
		return
	}

	if h.Content == nil {
		WriteError(w, http.StatusNotFound, "file not found")
		return
	}
	fc, err := h.Content.GetFileContent(ctx, repoID, relativePath)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("read file content failed: %v", err))
		return
	}
	if fc == nil {
		WriteError(w, http.StatusNotFound, "file not found")
		return
	}
	if status, message, err := validateSelectedRepositoryRef(ctx, h.Content, repoID, requestedRef, fc.CommitSHA); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query repository refs failed: %v", err))
		return
	} else if status != 0 {
		WriteError(w, status, message)
		return
	}

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		repositoryContentResponse(fc),
		BuildTruthEnvelope(
			h.profile(),
			"code_search.content_search",
			TruthBasisContentIndex,
			"resolved from exact repository file content lookup",
		),
	)
}

// repositoryContentResponse builds the content payload, applying the byte cap
// and choosing utf-8 vs base64 encoding based on the file bytes.
func repositoryContentResponse(fc *FileContent) map[string]any {
	raw := fc.Content
	size := len(raw)
	truncated := false
	if size > repositoryContentMaxBytes {
		raw = truncateUTF8(raw, repositoryContentMaxBytes)
		truncated = true
	}

	encoding := "utf-8"
	content := raw
	if !utf8.ValidString(raw) {
		encoding = "base64"
		content = base64.StdEncoding.EncodeToString([]byte(raw))
	}

	response := map[string]any{
		"path":      fc.RelativePath,
		"ref":       fc.CommitSHA,
		"encoding":  encoding,
		"content":   content,
		"size":      size,
		"truncated": truncated,
	}
	if fc.Language != "" {
		response["language"] = fc.Language
	}
	return response
}

// truncateUTF8 returns at most maxBytes of s, cut back to the last complete
// UTF-8 rune boundary so valid text never returns a partial trailing rune.
// Non-UTF-8 input is cut at the raw byte boundary (it is base64-encoded anyway).
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := s[:maxBytes]
	if utf8.ValidString(cut) {
		return cut
	}
	for len(cut) > 0 {
		r, size := utf8.DecodeLastRuneInString(cut)
		if r != utf8.RuneError || size > 1 {
			break
		}
		cut = cut[:len(cut)-1]
	}
	return cut
}
