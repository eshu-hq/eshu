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
// EXPLAIN (ANALYZE, BUFFERS): (#10055).
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

// singleRepositoryQuery reads one repository exactly as the page read would
// return it: the same candidate predicates, the same go.mod and Go-evidence
// filters, the same active-generation commit join, and the total-candidates
// count the page windows into slot counts. The candidate moves out of the CTE
// into a source_key probe (the fleet scan the page needs would be waste for
// one row), while the count stays a scalar subquery over the page's candidate
// set so the fencer's slotCount matches the rotation's. The generation join
// is LEFT so a missing active generation is a named refusal, not an empty
// page: zero rows means the repository id is unknown, a row without an active
// generation row means it has none.
//
// Any change here needs a fresh EXPLAIN (ANALYZE, BUFFERS) (#10055): the
// source_key probe rides ingestion_scopes_source_idx
// (source_system, scope_kind, ...) with a heap filter on source_key, and the
// count subquery replays the page's candidate scan.
const singleRepositoryQuery = `
SELECT candidate.source_key AS repository_id,
       COALESCE(candidate.payload->>'remote_url', '') AS remote_url,
       COALESCE(generation.source_commit_sha, '') AS source_commit_sha,
       (SELECT COUNT(*)
        FROM ingestion_scopes AS scope
        WHERE scope.source_system = 'git'
          AND scope.scope_kind = 'repository'
          AND scope.active_generation_id IS NOT NULL
       ) AS total_candidates,
       EXISTS (
           SELECT 1
           FROM content_files AS file
           WHERE file.repo_id = candidate.source_key
             AND (file.relative_path = 'go.mod' OR file.relative_path LIKE '%/go.mod')
       ) AS has_go_module,
       EXISTS (
           SELECT 1
           FROM ingestion_scopes AS reach
           JOIN fact_records AS fact
             ON fact.scope_id = reach.scope_id
            AND fact.generation_id = reach.active_generation_id
           WHERE reach.scope_id = 'go-reachability://' || candidate.source_key
             AND reach.active_generation_id IS NOT NULL
             AND fact.fact_kind IN ('vulnerability.go_module_evidence', 'vulnerability.go_call_reachability')
             AND fact.is_tombstone = FALSE
       ) AS has_go_evidence,
       candidate.active_generation_id IS NOT NULL AS has_active_generation,
       generation.generation_id IS NOT NULL AS has_active_generation_row,
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
FROM ingestion_scopes AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.active_generation_id
 AND generation.status = 'active'
LEFT JOIN ingestion_scopes AS reach_scope
  ON reach_scope.scope_id = 'go-reachability://' || candidate.source_key
LEFT JOIN scope_generations AS reach_generation
  ON reach_generation.generation_id = reach_scope.active_generation_id
 AND reach_generation.status = 'active'
WHERE candidate.source_key = $1
  AND candidate.source_system = 'git'
  AND candidate.scope_kind = 'repository'
`

// ErrGoModuleRepositoryNotFound names a single-repository read for an
// unknown repository id. It fails closed as not_eligible: the operator never
// plans a scope the rotation would not visit.
var ErrGoModuleRepositoryNotFound = errors.New("go module repository not found")

// ErrGoModuleRepositoryNoActiveGeneration names a single-repository read for
// a repository with no active generation. The rotation page only visits
// scopes with one, so the operator path refuses too.
var ErrGoModuleRepositoryNoActiveGeneration = errors.New("go module repository has no active generation")

// Store reads Go module repositories from the Eshu data plane.
type Store struct {
	database     db.Queryer
	reopenHolder HolderReopener
	// observeGoEvidenceLock, when set, is told how long the fence waited for the
	// exclusive go-evidence lock and whether the wait ended in the lock timeout
	// (#11327). Nil records nothing.
	observeGoEvidenceLock GoEvidenceLockObserver
	// supportsReady binds the supports readiness predicate the holder read answers
	// under (#11197); the composition root binds it with WithSupportsReadiness.
	supportsReady SupportsReadiness
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

// GoModuleRepositoryRead is one repository read for an operator plan: the
// repository exactly as the rotation page read would return it, whether its
// scope holds a go.mod, and the total candidate count the page windows into
// slot counts.
type GoModuleRepositoryRead struct {
	Repository      scannercomponent.GoModuleRepository
	HasGoModule     bool
	TotalCandidates int
}

// GetGoModuleRepository returns one repository exactly as the rotation page
// read would return it, plus the total candidate count the page windows into
// slot counts. Unknown ids fail with ErrGoModuleRepositoryNotFound and rows
// without an active generation fail with
// ErrGoModuleRepositoryNoActiveGeneration; the caller maps both to
// not_eligible. The go.mod/evidence filter stays with the caller so the
// eligibility rule reads in one place beside the planner's skip reasons.
func (s Store) GetGoModuleRepository(
	ctx context.Context,
	repositoryID string,
) (GoModuleRepositoryRead, error) {
	if s.database == nil {
		return GoModuleRepositoryRead{}, errors.New("go module repository store database is required")
	}
	if strings.TrimSpace(repositoryID) == "" {
		return GoModuleRepositoryRead{}, ErrGoModuleRepositoryNotFound
	}
	rows, err := s.database.QueryContext(ctx, singleRepositoryQuery, strings.TrimSpace(repositoryID))
	if err != nil {
		return GoModuleRepositoryRead{}, fmt.Errorf("read go module repository: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return GoModuleRepositoryRead{}, fmt.Errorf("read go module repository: %w", err)
		}
		return GoModuleRepositoryRead{}, ErrGoModuleRepositoryNotFound
	}
	var read GoModuleRepositoryRead
	var hasGoEvidence, hasActiveGeneration, hasActiveGenerationRow bool
	var evidenceLeftAt sql.NullTime
	if err := rows.Scan(
		&read.Repository.RepositoryID, &read.Repository.RemoteURL, &read.Repository.SourceCommitSHA,
		&read.TotalCandidates, &read.HasGoModule, &hasGoEvidence,
		&hasActiveGeneration, &hasActiveGenerationRow,
		&read.Repository.EvidenceGenerationID, &read.Repository.EvidenceCommitSHA, &evidenceLeftAt,
	); err != nil {
		return GoModuleRepositoryRead{}, fmt.Errorf("read go module repository: %w", err)
	}
	if err := rows.Err(); err != nil {
		return GoModuleRepositoryRead{}, fmt.Errorf("read go module repository: %w", err)
	}
	if rows.Next() {
		return GoModuleRepositoryRead{}, fmt.Errorf(
			"read go module repository %q: source_key matched more than one scope", repositoryID)
	}
	read.Repository.HasGoEvidence = hasGoEvidence
	if evidenceLeftAt.Valid {
		read.Repository.EvidenceLeftAt = evidenceLeftAt.Time
	}
	if !hasActiveGeneration || !hasActiveGenerationRow {
		return GoModuleRepositoryRead{}, ErrGoModuleRepositoryNoActiveGeneration
	}
	return read, nil
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
