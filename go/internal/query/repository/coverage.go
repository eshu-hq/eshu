// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	repositoryGraphCoverageStatsTimeout = 2 * time.Second

	// CoverageContentFilesTable and
	// CoverageContentEntitiesTable name the content-store tables
	// repository coverage reads. Exported for #6060 so the root
	// ContentReader coverage stayers can name them from outside this
	// package.
	CoverageContentFilesTable    = "content_files"
	CoverageContentEntitiesTable = "content_entities"
)

const (
	repositoryCoverageContentFilesTable    = CoverageContentFilesTable
	repositoryCoverageContentEntitiesTable = CoverageContentEntitiesTable
)

func (h *Handler) resolveCoverageRepositoryID(ctx context.Context, selector string) (string, error) {
	return h.resolveRepositorySelector(ctx, selector)
}

// queryContentStoreCoverage queries the Postgres content store for repository coverage.
// QueryContentStoreCoverage reads repository coverage from the content
// store. It forwards to queryContentStoreCoverage; exported for #6060 so the
// cross-family ports test in package query can name it from outside this
// package.
func (h *Handler) QueryContentStoreCoverage(ctx context.Context, repoID string) (map[string]any, error) {
	return h.queryContentStoreCoverage(ctx, repoID)
}

func (h *Handler) queryContentStoreCoverage(ctx context.Context, repoID string) (map[string]any, error) {
	var contentCoverage querycontract.RepositoryContentCoverage
	if h.Content != nil {
		var err error
		contentCoverage, err = h.Content.RepositoryCoverage(ctx, repoID)
		if err != nil {
			return nil, fmt.Errorf("query content coverage: %w", err)
		}
		if contentCoverage.Available {
			return repositoryCoverageResponse(
				repoID,
				repositoryGraphCoverageStats{},
				contentCoverage,
				"graph coverage stats skipped because content store coverage is available",
			), nil
		}
	}

	graphStats, graphErr := h.queryRepositoryGraphCoverageStatsWithTimeout(ctx, repoID)
	if graphErr != nil {
		return nil, fmt.Errorf("query graph coverage stats: %w", graphErr)
	}

	return repositoryCoverageResponse(repoID, graphStats, contentCoverage, ""), nil
}

func repositoryCoverageResponse(
	repoID string,
	graphStats repositoryGraphCoverageStats,
	contentCoverage querycontract.RepositoryContentCoverage,
	lastError string,
) map[string]any {
	graphGapCount, contentGapCount := 0, 0
	if graphStats.Available && contentCoverage.Available {
		graphGapCount, contentGapCount = computeCoverageGapCounts(
			graphStats.FileCount,
			graphStats.EntityCount,
			contentCoverage.FileCount,
			contentCoverage.EntityCount,
		)
	}
	coverage := map[string]any{
		"repo_id":                  repoID,
		"file_count":               0,
		"entity_count":             0,
		"languages":                []map[string]any{},
		"graph_available":          graphStats.Available,
		"server_content_available": contentCoverage.Available,
		"graph_gap_count":          graphGapCount,
		"content_gap_count":        contentGapCount,
		"completeness_state": completenessStateForCoverage(
			graphStats.Available,
			contentCoverage.Available,
			graphGapCount,
			contentGapCount,
		),
		"content_last_indexed_at": "",
		"last_error":              lastError,
		"summary": map[string]any{
			"graph_file_count":     graphStats.FileCount,
			"graph_entity_count":   graphStats.EntityCount,
			"content_file_count":   0,
			"content_entity_count": 0,
		},
	}
	if !contentCoverage.Available {
		coverage["last_error"] = "content store not available"
		return coverage
	}
	coverage["server_content_available"] = contentCoverage.Available
	coverage["file_count"] = contentCoverage.FileCount
	coverage["entity_count"] = contentCoverage.EntityCount
	coverage["languages"] = coverageLanguageMaps(contentCoverage.Languages)
	if latest := latestCoverageTimestamp(contentCoverage.FileIndexedAt, contentCoverage.EntityIndexedAt); !latest.IsZero() {
		coverage["content_last_indexed_at"] = latest.Format(time.RFC3339Nano)
	}
	summary := querycontract.MapValue(coverage, "summary")
	summary["content_file_count"] = contentCoverage.FileCount
	summary["content_entity_count"] = contentCoverage.EntityCount
	summary["content_files_last_indexed_at"] = formatCoverageTimestamp(contentCoverage.FileIndexedAt)
	summary["content_entities_last_indexed_at"] = formatCoverageTimestamp(contentCoverage.EntityIndexedAt)
	summary["graph_gap_count"] = graphGapCount
	summary["content_gap_count"] = contentGapCount
	summary["completeness_state"] = coverage["completeness_state"]
	coverage["summary"] = summary
	return coverage
}

func (h *Handler) queryRepositoryGraphCoverageStatsWithTimeout(
	ctx context.Context,
	repoID string,
) (repositoryGraphCoverageStats, error) {
	graphCtx, cancel := context.WithTimeout(ctx, repositoryGraphCoverageStatsTimeout)
	defer cancel()
	return h.queryRepositoryGraphCoverageStats(graphCtx, repoID)
}

// coverageLanguageMaps forwards to querycontract.CoverageLanguageMaps, which
// this implementation moved to (#6060) so a handler-family subpackage can
// reach it without importing this package.
func coverageLanguageMaps(languages []querycontract.RepositoryLanguageCount) []map[string]any {
	return querycontract.CoverageLanguageMaps(languages)
}

type repositoryGraphCoverageStats struct {
	FileCount   int
	EntityCount int
	Available   bool
}

func (h *Handler) queryRepositoryGraphCoverageStats(
	ctx context.Context,
	repoID string,
) (repositoryGraphCoverageStats, error) {
	if h.Neo4j == nil || repoID == "" {
		return repositoryGraphCoverageStats{}, nil
	}

	row, err := h.Neo4j.RunSingle(ctx, `
		MATCH (r:Repository {id: $repo_id})
		OPTIONAL MATCH (r)-[:REPO_CONTAINS]->(f:File)
		WITH r, count(DISTINCT f) as file_count
		OPTIONAL MATCH (r)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(e)
		RETURN file_count, count(DISTINCT e) as entity_count
	`, map[string]any{"repo_id": repoID})
	if err != nil {
		return repositoryGraphCoverageStats{}, err
	}
	if row == nil {
		return repositoryGraphCoverageStats{}, nil
	}
	return repositoryGraphCoverageStats{
		FileCount:   querycontract.IntVal(row, "file_count"),
		EntityCount: querycontract.IntVal(row, "entity_count"),
		Available:   true,
	}, nil
}

func QueryMaxIndexedAt(ctx context.Context, db *sql.DB, table string, repoID string) (time.Time, error) {
	safeTable, err := CoverageIndexedAtTable(table)
	if err != nil {
		return time.Time{}, err
	}
	var indexedAt sql.NullTime
	err = db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT max(indexed_at) as indexed_at
		FROM %s
		WHERE repo_id = $1
	`, safeTable), repoID).Scan(&indexedAt)
	if err != nil {
		return time.Time{}, err
	}
	if !indexedAt.Valid {
		return time.Time{}, nil
	}
	return indexedAt.Time.UTC(), nil
}

// CoverageIndexedAtTable validates table against the closed set of
// coverage tables QueryMaxIndexedAt is allowed to query
// (repositoryCoverageContentFilesTable, repositoryCoverageContentEntitiesTable)
// and returns it unchanged when it matches. table is interpolated into a
// raw SQL identifier, so this allowlist check is what keeps that query safe
// from an unexpected or attacker-controlled table name; an unrecognized
// table returns an error instead of the table name.
func CoverageIndexedAtTable(table string) (string, error) {
	switch table {
	case repositoryCoverageContentFilesTable, repositoryCoverageContentEntitiesTable:
		return table, nil
	default:
		return "", fmt.Errorf("unsupported repository coverage indexed_at table %q", table)
	}
}

func computeCoverageGapCounts(
	graphFileCount int,
	graphEntityCount int,
	contentFileCount int,
	contentEntityCount int,
) (int, int) {
	graphGapCount := querycontract.MaxInt(contentFileCount-graphFileCount, 0) + querycontract.MaxInt(contentEntityCount-graphEntityCount, 0)
	contentGapCount := querycontract.MaxInt(graphFileCount-contentFileCount, 0) + querycontract.MaxInt(graphEntityCount-contentEntityCount, 0)
	return graphGapCount, contentGapCount
}

func completenessStateForCoverage(
	graphAvailable bool,
	contentAvailable bool,
	graphGapCount int,
	contentGapCount int,
) string {
	switch {
	case !graphAvailable && !contentAvailable:
		return "unknown"
	case !graphAvailable:
		return "graph_unavailable"
	case !contentAvailable:
		return "content_unavailable"
	case graphGapCount == 0 && contentGapCount == 0:
		return "complete"
	case graphGapCount > 0 && contentGapCount > 0:
		return "graph_and_content_partial"
	case graphGapCount > 0:
		return "graph_partial"
	default:
		return "content_partial"
	}
}

func latestCoverageTimestamp(timestamps ...time.Time) time.Time {
	var latest time.Time
	for _, ts := range timestamps {
		if ts.IsZero() {
			continue
		}
		if latest.IsZero() || ts.After(latest) {
			latest = ts
		}
	}
	return latest
}

func formatCoverageTimestamp(ts time.Time) string {
	return querycontract.FormatCoverageTimestamp(ts)
}
