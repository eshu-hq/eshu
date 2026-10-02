// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// deadCodeInvestigationContentCoverage reads the file-only coverage the
// investigation reports: the file count, the language distribution, and the
// newest content_files.indexed_at. It deliberately avoids the full
// RepositoryCoverage read, whose content_entities aggregate scanned about a
// gibibyte of heap on the largest repository for a count the response no
// longer carries (#7525). A store that lacks the two narrow reads (a test
// double) falls back to the full read and the investigation still uses only
// its file fields, so the response shape never depends on the store type.
func (a *Analyzer) deadCodeInvestigationContentCoverage(
	ctx context.Context,
	repoID string,
) (RepositoryContentCoverage, time.Time, error) {
	narrow, narrowOK := a.deps.Content.(querycontract.RepositoryContextCoverageReadModelStore)
	filesIndexedAt, filesOK := a.deps.Content.(querycontract.RepositoryFilesIndexedAtReadModelStore)
	if !narrowOK || !filesOK {
		coverage, err := a.deps.Content.RepositoryCoverage(ctx, repoID)
		if err != nil {
			return RepositoryContentCoverage{}, time.Time{}, fmt.Errorf("query repository content coverage: %w", err)
		}
		return coverage, coverage.FileIndexedAt, nil
	}
	coverage, err := narrow.RepositoryContextCoverage(ctx, repoID)
	if err != nil {
		return RepositoryContentCoverage{}, time.Time{}, fmt.Errorf("query repository content coverage: %w", err)
	}
	if !coverage.Available {
		return coverage, time.Time{}, nil
	}
	indexedAt, err := filesIndexedAt.RepositoryFilesLastIndexedAt(ctx, repoID)
	if err != nil {
		return RepositoryContentCoverage{}, time.Time{}, fmt.Errorf("query repository content file indexed_at: %w", err)
	}
	return coverage, indexedAt, nil
}
