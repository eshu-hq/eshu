// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/testutil/contentreader"
)

// openLegacyEntityMetadataTestDB makes the graph ID miss before answering the
// exact location lookup. These fixtures deliberately use different graph and
// content IDs to preserve legacy metadata hydration.
func openLegacyEntityMetadataTestDB(t *testing.T, results []contentreader.ReaderQueryResult) *sql.DB {
	t.Helper()
	idMiss := contentreader.ReaderQueryResult{
		Columns:       []string{"entity_id", "repo_id", "relative_path", "entity_type", "entity_name", "start_line", "end_line", "language", "source_cache", "metadata"},
		QueryContains: []string{"entity_id = ANY($2)"},
	}
	queued := make([]contentreader.ReaderQueryResult, 0, len(results)+1)
	queued = append(queued, idMiss)
	queued = append(queued, results...)
	return contentreader.OpenReaderTestDB(t, queued)
}
