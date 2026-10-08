// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CodeSearchProbeLimit bounds one code-search page with a limit+1 truncation probe.
func CodeSearchProbeLimit(publicLimit int) int {
	return publicLimit + 1
}

// CodeSearchRankedWindow is the number of ranked matches a code search can
// page through. It equals the page-size ceiling, so offset+limit never asks
// the store for more than one maximum page plus the truncation probe.
const CodeSearchRankedWindow = 200

// CodeSearchPageWindow resolves a requested offset and limit against the
// ranked window. It returns the effective limit, which shrinks so that
// offset+limit stays inside the window, and false when the offset is outside
// the window and no row can be returned.
func CodeSearchPageWindow(offset, limit int) (int, bool) {
	if offset < 0 {
		offset = 0
	}
	if offset >= CodeSearchRankedWindow {
		return 0, false
	}
	if remaining := CodeSearchRankedWindow - offset; limit > remaining {
		limit = remaining
	}
	return limit, true
}

// CodeSearchPageWindowOffset reports the offset CodeSearchPageWindow applies:
// negative offsets act as zero.
func CodeSearchPageWindowOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

// CodeSearchPagePayload shapes code-search rows into the paged response envelope.
func CodeSearchPagePayload(
	source string,
	sourceBackend string,
	query string,
	repositoryID string,
	rows []map[string]any,
	publicLimit int,
) map[string]any {
	return CodeSearchPagePayloadAt(source, sourceBackend, query, repositoryID, rows, publicLimit, 0)
}

// CodeSearchPagePayloadAt shapes the page that starts offset rows into the
// ranked rows. rows holds the whole probe window; the first offset rows are
// skipped, then the page is trimmed to publicLimit with the same limit+1
// truncation probe. The payload carries an offset key only when offset is
// positive, so a request without an offset keeps its existing response bytes.
func CodeSearchPagePayloadAt(
	source string,
	sourceBackend string,
	query string,
	repositoryID string,
	rows []map[string]any,
	publicLimit int,
	offset int,
) map[string]any {
	page, truncated := CodeSearchPageRows(rows, publicLimit, offset)
	return CodeSearchPagePayloadFromPage(source, sourceBackend, query, repositoryID, page, truncated, publicLimit, offset)
}

// CodeSearchPageRows cuts the page that starts offset rows into the probe
// window rows: the first offset rows are skipped and the page is trimmed to
// publicLimit. truncated reports that rows existed past the page. The cut is
// made on the offset order, before any hybrid re-rank, so a page is always the
// same window of rows whatever the request limit was on earlier pages.
func CodeSearchPageRows(rows []map[string]any, publicLimit, offset int) ([]map[string]any, bool) {
	if offset > len(rows) {
		offset = len(rows)
	}
	if offset > 0 {
		rows = rows[offset:]
	}
	truncated := len(rows) > publicLimit
	if truncated {
		rows = rows[:publicLimit]
	}
	return rows, truncated
}

// CodeSearchPagePayloadFromPage shapes an already cut page into the paged
// response envelope. offset is the offset the client requested, echoed as
// asked even when it lies past the rows found. The read-time clips run here,
// after any hybrid re-rank, which reads the full stored body.
func CodeSearchPagePayloadFromPage(
	source string,
	sourceBackend string,
	query string,
	repositoryID string,
	rows []map[string]any,
	truncated bool,
	publicLimit int,
	offset int,
) map[string]any {
	if rows == nil {
		rows = []map[string]any{}
	}
	// Clip after the page is trimmed and after any hybrid re-rank, which reads
	// the full stored body, so the count covers the rows actually returned.
	clippedRows := querycontract.ClipRowsSourceCache(rows)
	clippedDocstrings := querycontract.ClipRowsDocstring(rows, entitysemantics.ReattachSemanticSummary)
	payload := map[string]any{
		"source":         source,
		"source_backend": sourceBackend,
		"query":          query,
		"repo_id":        repositoryID,
		"results":        rows,
		"count":          len(rows),
		"limit":          publicLimit,
		"truncated":      truncated,
	}
	if offset > 0 {
		payload["offset"] = offset
	}
	querycontract.AddSourceCacheClipMarkers(payload, clippedRows)
	querycontract.AddDocstringClipMarkers(payload, clippedDocstrings)
	return payload
}
