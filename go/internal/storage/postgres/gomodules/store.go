// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"context"
	"database/sql"
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
//
// has_go_evidence plans a repository that lost its go.mod. Its go-reachability
// scope keeps Go evidence active until a new generation supersedes it, and only a
// claim can produce one, so without this branch the evidence would never retract.
// The component scans once more and lands a trusted no_go_modules generation that
// holds only a warning, and the repository drops out on its next visit. The probe
// is the ingestion_scopes primary key, then a prefix of
// fact_records_scope_generation_idx (scope_id, generation_id, fact_kind) that stops
// at the first row, so warning-only generations cost no scan.
//
// The go-reachability scope's active generation comes back too, for the commit-skew
// fence in the planner: its id and the commit it measured. The joins are one more
// ingestion_scopes primary-key probe and one scope_generations primary-key probe per
// candidate, and add no index.
//
// evidence_left_at is when the repository left the evidence commit: the newest
// superseded_at among the repository scope's own generations at that commit. The CASE
// guards the subquery so it runs only for a skewed row, never for the whole page, so
// the common page with no skew pays nothing for it. It reads one range of
// scope_generations_scope_idx (scope_id, status, ingested_at DESC), bounded by
// retention, and filters on source_commit_sha from the heap. NULL with skew means
// retention pruned that history. Any change here needs a fresh
// EXPLAIN (ANALYZE, BUFFERS): docs/internal/evidence/10055-commit-skew-fence.md.
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
       ) AS has_go_module,
       EXISTS (
           SELECT 1
           FROM ingestion_scopes AS reach
           JOIN fact_records AS fact
             ON fact.scope_id = reach.scope_id
            AND fact.generation_id = reach.active_generation_id
           WHERE reach.scope_id = 'go-reachability://' || candidate.repository_id
             AND reach.active_generation_id IS NOT NULL
             AND fact.fact_kind IN ('vulnerability.go_module_evidence', 'vulnerability.go_call_reachability')
             AND fact.is_tombstone = FALSE
       ) AS has_go_evidence,
       COALESCE(reach_generation.generation_id, '') AS evidence_generation_id,
       COALESCE(reach_generation.source_commit_sha, '') AS evidence_commit_sha,
       CASE WHEN COALESCE(reach_generation.source_commit_sha, '') <> ''
             AND COALESCE(generation.source_commit_sha, '') <> ''
             AND reach_generation.source_commit_sha <> generation.source_commit_sha
            THEN (
                SELECT max(history.superseded_at)
                FROM scope_generations AS history
                WHERE history.scope_id = candidate.scope_id
                  AND history.status = 'superseded'
                  AND history.source_commit_sha = reach_generation.source_commit_sha
            )
       END AS evidence_left_at
FROM candidates AS candidate
JOIN scope_generations AS generation
  ON generation.generation_id = candidate.active_generation_id
 AND generation.status = 'active'
LEFT JOIN ingestion_scopes AS reach_scope
  ON reach_scope.scope_id = 'go-reachability://' || candidate.repository_id
LEFT JOIN scope_generations AS reach_generation
  ON reach_generation.generation_id = reach_scope.active_generation_id
 AND reach_generation.status = 'active'
WHERE candidate.candidate_rank / $2::bigint =
      $1::bigint % CEIL(candidate.total_candidates::numeric / $2::bigint)::bigint
ORDER BY candidate.scope_id
`

// Store reads Go module repositories from the Eshu data plane.
type Store struct {
	database     db.Queryer
	reopenHolder HolderReopener
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
		var hasGoModule, hasGoEvidence bool
		var evidenceLeftAt sql.NullTime
		if err := rows.Scan(
			&repository.RepositoryID, &repository.RemoteURL, &repository.SourceCommitSHA,
			&page.TotalCandidates, &hasGoModule, &hasGoEvidence,
			&repository.EvidenceGenerationID, &repository.EvidenceCommitSHA, &evidenceLeftAt,
		); err != nil {
			return scannercomponent.GoModuleRepositoryPage{}, fmt.Errorf("read go module repositories: %w", err)
		}
		page.Candidates++
		repository.HasGoEvidence = hasGoEvidence
		if evidenceLeftAt.Valid {
			repository.EvidenceLeftAt = evidenceLeftAt.Time
		}
		if hasGoModule || hasGoEvidence {
			page.Repositories = append(page.Repositories, repository)
		}
		if hasGoEvidence && !hasGoModule {
			page.EvidenceOnly++
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
