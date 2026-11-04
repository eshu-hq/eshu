// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// plannedRunQuery is the one read of a repeat pass inside a plan bucket: a
// primary-key probe on workflow_runs. It keeps a planned page fixed for its
// bucket and costs far less than re-reading the page every reconcile.
const plannedRunQuery = `SELECT EXISTS (SELECT 1 FROM workflow_runs WHERE run_id = $1)`

// pageQuery reads one rotation page. $1 is the rotation bucket and $2 the page
// size: a candidate belongs to page rank / size, and the bucket selects page
// bucket mod the page count, the arithmetic of
// scannercomponent.GoModulePageWindow. Pages partition every repository scope
// with an active generation, so a page examines at most $2 repositories whether
// or not they hold Go code.
//
// A go.mod is detected from content_files on content_files_repo_path_idx, the
// source GoModuleManifestsQuery reads, not from the parser's module_declaration
// fact. Measured on Postgres 16 with EXPLAIN ANALYZE, for one 256-candidate page
// over 1.84M facts: the fact probe took 1511 ms and read 197k buffers, because
// fact_records has no index for that row and a repository without one is scanned
// to its end. This probe took 68 ms and read 7.5k buffers, index-only. It can
// disagree with the fact only for a go.mod with no module line, or content a
// moment ahead of or behind the active generation; the component then reads the
// checked-out commit and finds no module, so the cost is one empty scan.
const pageQuery = `
WITH candidates AS (
    SELECT scope.scope_id,
           scope.source_key AS repository_id,
           scope.active_generation_id,
           COALESCE(scope.payload->>'remote_url', '') AS remote_url,
           ROW_NUMBER() OVER (ORDER BY scope.scope_id) - 1 AS candidate_rank,
           COUNT(*) OVER () AS total_candidates
    FROM ingestion_scopes AS scope
    WHERE scope.source_system = 'git'
      AND scope.scope_kind = 'repository'
      AND scope.active_generation_id IS NOT NULL
)
SELECT candidate.repository_id,
       candidate.remote_url,
       COALESCE(generation.source_commit_sha, '') AS source_commit_sha,
       candidate.total_candidates,
       EXISTS (
           SELECT 1
           FROM content_files AS file
           WHERE file.repo_id = candidate.repository_id
             AND (file.relative_path = 'go.mod' OR file.relative_path LIKE '%/go.mod')
       ) AS has_go_module
FROM candidates AS candidate
JOIN scope_generations AS generation
  ON generation.generation_id = candidate.active_generation_id
 AND generation.status = 'active'
WHERE candidate.candidate_rank / $2::bigint =
      $1::bigint % CEIL(candidate.total_candidates::numeric / $2::bigint)::bigint
ORDER BY candidate.scope_id
`

// Store reads Go module repositories from the Eshu data plane.
type Store struct {
	database db.Queryer
}

// NewStore returns a Store over one queryer. Pass an instrumented queryer so the
// read reports on the shared Postgres query metrics.
func NewStore(database db.Queryer) Store {
	return Store{database: database}
}

// ListGoModuleRepositories returns one rotation page. When filter.RunID already
// exists it returns AlreadyPlanned and reads no candidates. A blank RunID skips
// that probe. PageSize is clamped to scannercomponent.GoModulePageSize.
func (s Store) ListGoModuleRepositories(
	ctx context.Context,
	filter scannercomponent.GoModuleRepositoryFilter,
) (scannercomponent.GoModuleRepositoryPage, error) {
	if s.database == nil {
		return scannercomponent.GoModuleRepositoryPage{}, errors.New("go module repository store database is required")
	}
	if runID := strings.TrimSpace(filter.RunID); runID != "" {
		planned, err := s.runExists(ctx, runID)
		if err != nil {
			return scannercomponent.GoModuleRepositoryPage{}, err
		}
		if planned {
			return scannercomponent.GoModuleRepositoryPage{AlreadyPlanned: true}, nil
		}
	}
	pageSize := filter.PageSize
	if pageSize <= 0 || pageSize > scannercomponent.GoModulePageSize {
		pageSize = scannercomponent.GoModulePageSize
	}
	rows, err := s.database.QueryContext(ctx, pageQuery, max(filter.RotationBucket, 0), pageSize)
	if err != nil {
		return scannercomponent.GoModuleRepositoryPage{}, fmt.Errorf("read go module repositories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	page := scannercomponent.GoModuleRepositoryPage{}
	for rows.Next() {
		var repository scannercomponent.GoModuleRepository
		var hasGoModule bool
		if err := rows.Scan(
			&repository.RepositoryID, &repository.RemoteURL, &repository.SourceCommitSHA,
			&page.TotalCandidates, &hasGoModule,
		); err != nil {
			return scannercomponent.GoModuleRepositoryPage{}, fmt.Errorf("read go module repositories: %w", err)
		}
		page.Candidates++
		if hasGoModule {
			page.Repositories = append(page.Repositories, repository)
		}
	}
	if err := rows.Err(); err != nil {
		return scannercomponent.GoModuleRepositoryPage{}, fmt.Errorf("read go module repositories: %w", err)
	}
	page.Slot, page.SlotCount = scannercomponent.GoModulePageWindow(filter.RotationBucket, page.TotalCandidates, pageSize)
	return page, nil
}

func (s Store) runExists(ctx context.Context, runID string) (bool, error) {
	rows, err := s.database.QueryContext(ctx, plannedRunQuery, runID)
	if err != nil {
		return false, fmt.Errorf("read planned go module run: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("read planned go module run: %w", err)
		}
		return false, errors.New("read planned go module run: query returned no rows")
	}
	var exists bool
	if err := rows.Scan(&exists); err != nil {
		return false, fmt.Errorf("read planned go module run: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read planned go module run: %w", err)
	}
	return exists, nil
}
