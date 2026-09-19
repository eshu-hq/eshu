// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// Reconcile outcomes, one per repository checked. They are the bounded values
// of the reconcile counter's outcome label.
const (
	// ReconcileMatch means the table already equals content_entities for the
	// repository; nothing was written.
	ReconcileMatch = "match"
	// ReconcileSuspect means the table differed on this check and nothing was
	// written. A single mismatch is not drift: a ContentWriter.Write commits its
	// content rows before its derive runs, and a check that lands in between
	// sees the table behind. The next cycle re-checks the repository and
	// repairs it only if it still differs.
	ReconcileSuspect = "suspect"
	// ReconcileRepaired means the table differed on two checks at least one
	// reconcile interval apart and was re-derived under the repository lock.
	ReconcileRepaired = "repaired"
	// ReconcileFenced means an unaware writer marked the repository (see
	// WriterSessionSQL) and it was re-derived under the repository lock.
	// Readers stay on the graph while any repository is marked.
	ReconcileFenced = "fenced"
	// ReconcileError means the check or the repair failed; the next walk
	// retries the repository.
	ReconcileError = "error"
)

// reconcileDimensions are the metadata dimensions the derive copies. The
// digest hashes each one with the derive's own normalization.
var reconcileDimensions = []string{
	"kind", "resource_type", "data_type", "provider", "environment",
	"resource_service", "resource_category", "service_kind",
}

// reconcileDigestSQL compares one repository's infra-typed content rows with
// its table rows as (row count, sum of a per-row hash). Each side is bounded by
// repo_id: the content side reads the repository through content_entities'
// repo_id index and the table side through infra_resource_entities_repo_path_idx.
// $1 repo_id, $2 labels.
var reconcileDigestSQL = `
SELECT c.n, c.h, t.n, t.h
FROM (
    SELECT count(*) AS n, COALESCE(sum(hashtextextended(concat_ws(E'\x1f',
               ce.entity_id, ce.relative_path, ce.entity_type, ce.entity_name,
               ` + digestExpressions("COALESCE(btrim(ce.metadata->>'%s'), '')") + `), 0)), 0)::text AS h
    FROM content_entities AS ce
    WHERE ce.repo_id = $1
      AND ce.entity_type = ANY($2::text[])
) AS c, (
    SELECT count(*) AS n, COALESCE(sum(hashtextextended(concat_ws(E'\x1f',
               ire.entity_id, ire.relative_path, ire.label, ire.entity_name,
               ` + digestExpressions("ire.%s") + `), 0)), 0)::text AS h
    FROM infra_resource_entities AS ire
    WHERE ire.repo_id = $1
) AS t`

// reconcileRepositoriesSQL lists up to $2 repositories after the $1 cursor,
// ordered by repo_id, that have content rows or table rows. Each side is a
// recursive skip scan over its repo_id index, one index probe per distinct
// repository, so a page costs O(budget) instead of a DISTINCT over every row.
// PostgreSQL evaluates a recursive CTE only as far as its consumer fetches,
// and each side's inner LIMIT $2 stops that fetch after budget rows; keep
// those limits, since the outer ORDER BY would otherwise pull every repository
// after the cursor. TestReconcileRepositoriesLivePageStopsAtBudget pins this
// from EXPLAIN ANALYZE.
// A repository with only non-infra content rows is listed too; its digest is
// (0, 0) against (0, 0) and costs one small probe.
const reconcileRepositoriesSQL = `
WITH RECURSIVE content_repos AS (
    (SELECT repo_id FROM content_entities WHERE repo_id > $1 ORDER BY repo_id LIMIT 1)
    UNION ALL
    SELECT (SELECT ce.repo_id FROM content_entities AS ce
            WHERE ce.repo_id > content_repos.repo_id ORDER BY ce.repo_id LIMIT 1)
    FROM content_repos WHERE content_repos.repo_id IS NOT NULL
), table_repos AS (
    (SELECT repo_id FROM infra_resource_entities WHERE repo_id > $1 ORDER BY repo_id LIMIT 1)
    UNION ALL
    SELECT (SELECT ire.repo_id FROM infra_resource_entities AS ire
            WHERE ire.repo_id > table_repos.repo_id ORDER BY ire.repo_id LIMIT 1)
    FROM table_repos WHERE table_repos.repo_id IS NOT NULL
)
SELECT repo_id FROM (
    SELECT repo_id FROM (SELECT repo_id FROM content_repos WHERE repo_id IS NOT NULL LIMIT $2) AS c
    UNION
    SELECT repo_id FROM (SELECT repo_id FROM table_repos WHERE repo_id IS NOT NULL LIMIT $2) AS t
) AS repos
ORDER BY repo_id
LIMIT $2`

func digestExpressions(format string) string {
	parts := make([]string, 0, len(reconcileDimensions))
	for _, dim := range reconcileDimensions {
		parts = append(parts, fmt.Sprintf(format, dim))
	}
	return strings.Join(parts, ",\n               ")
}

// RepoReconcile is the result of checking one repository.
type RepoReconcile struct {
	RepoID string
	// Outcome is ReconcileMatch, ReconcileSuspect, ReconcileRepaired, or
	// ReconcileError.
	Outcome string
	// ContentRows and TableRows are the row counts the check compared, before
	// any repair.
	ContentRows int64
	TableRows   int64
	// Repair reports the rows rewritten when Outcome is ReconcileRepaired.
	Repair   Stats
	Duration time.Duration
	// Err is set when Outcome is ReconcileError.
	Err error
}

// ReconcileRequest is one reconcile cycle's input.
type ReconcileRequest struct {
	// Cursor is the repo_id the walk resumes after; "" starts at the beginning.
	// It is ignored when Persist is set.
	Cursor string
	// Budget bounds the repositories one cycle checks, suspects included.
	Budget int
	// Suspects are the repositories the previous cycle found drifted. They are
	// re-checked first, and repaired if they still differ.
	Suspects []string
	// Persist claims the walk page from the shared persisted cursor
	// (ClaimPage) instead of walking from Cursor, which is then ignored.
	// Replicas claim disjoint pages, and a restarted process continues the
	// walk instead of starting over. The reducer runner always sets it.
	Persist bool
}

// ReconcileBatch is one bounded reconcile cycle.
type ReconcileBatch struct {
	// Ready is false when ReconcileCycle found the read model not in use yet
	// (no backfill marker, or the tables do not exist); nothing was checked.
	Ready bool
	Repos []RepoReconcile
	// NextCursor is the repo_id to resume after, or "" when this cycle reached
	// the end of the repository list and the next cycle starts over.
	NextCursor string
}

// ReconcileCycle is the reducer's reconcile entry point. Until the backfill
// marker exists the backfill owns the table and readers stay on the graph, so
// the cycle checks nothing and reports Ready false; a missing table (the
// reducer started before migration 109) is the same state, not an error.
//
// Otherwise it first repairs up to Budget fence-marked repositories (an
// unaware writer changed their content without deriving; see
// WriterSessionSQL), then re-checks req.Suspects with repair allowed from the
// remaining budget, then walks the
// remaining budget of repositories after the cursor with repair withheld: a
// repository that differs on the walk is only a suspect, returned for the next
// cycle. A failure on one repository is recorded on that repository and the
// cycle continues; only a failure to list repositories, or a canceled context,
// fails the batch.
func ReconcileCycle(ctx context.Context, database db.ExecQueryer, req ReconcileRequest) (ReconcileBatch, error) {
	if req.Budget <= 0 {
		return ReconcileBatch{}, errors.New("infra inventory reconcile: budget must be positive")
	}
	complete, err := BackfillComplete(ctx, database)
	if err != nil {
		return ReconcileBatch{}, err
	}
	if !complete {
		return ReconcileBatch{}, nil
	}
	batch := ReconcileBatch{Ready: true, NextCursor: req.Cursor}
	// Fence-marked repositories first: an unaware writer changed their
	// content without deriving, and readers stay on the graph until every
	// mark is repaired.
	dirty, err := dirtyRepositories(ctx, database, req.Budget)
	if err != nil {
		return batch, err
	}
	seen := make(map[string]struct{}, len(dirty)+len(req.Suspects))
	for _, repo := range dirty {
		seen[repo] = struct{}{}
		if err := ctx.Err(); err != nil {
			return batch, err
		}
		result, skipped := repairDirty(ctx, database, repo)
		if !skipped {
			batch.Repos = append(batch.Repos, result)
		}
	}
	budget := req.Budget - len(dirty)
	var suspects []string
	for _, repo := range normalizedPaths(req.Suspects) {
		if _, done := seen[repo]; !done {
			suspects = append(suspects, repo)
		}
	}
	if len(suspects) > budget {
		suspects = suspects[:budget]
	}
	for _, repo := range suspects {
		seen[repo] = struct{}{}
		if err := ctx.Err(); err != nil {
			return batch, err
		}
		batch.Repos = append(batch.Repos, reconcileOne(ctx, database, repo, true))
	}

	walkBudget := budget - len(suspects)
	if walkBudget == 0 {
		return batch, nil
	}
	var (
		repos []string
		next  string
	)
	if req.Persist {
		repos, next, err = ClaimPage(ctx, database, walkBudget)
	} else {
		repos, err = reconcileRepositories(ctx, database, req.Cursor, walkBudget)
		if err == nil && len(repos) == walkBudget {
			next = repos[len(repos)-1]
		}
	}
	if err != nil {
		return batch, err
	}
	for _, repo := range repos {
		if err := ctx.Err(); err != nil {
			return batch, err
		}
		if _, dup := seen[repo]; dup {
			continue
		}
		batch.Repos = append(batch.Repos, reconcileOne(ctx, database, repo, false))
	}
	batch.NextCursor = next
	return batch, nil
}

func reconcileOne(ctx context.Context, database db.ExecQueryer, repo string, repair bool) RepoReconcile {
	result, err := ReconcileRepo(ctx, database, repo, repair)
	if err != nil {
		return RepoReconcile{RepoID: repo, Outcome: ReconcileError, Duration: result.Duration, Err: err}
	}
	return result
}

func reconcileRepositories(ctx context.Context, database db.ExecQueryer, cursor string, budget int) ([]string, error) {
	rows, err := database.QueryContext(ctx, reconcileRepositoriesSQL, cursor, budget)
	if err != nil {
		return nil, fmt.Errorf("list infra inventory reconcile repositories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var repos []string
	for rows.Next() {
		var repo string
		if err := rows.Scan(&repo); err != nil {
			return nil, fmt.Errorf("scan infra inventory reconcile repository: %w", err)
		}
		repos = append(repos, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list infra inventory reconcile repositories: %w", err)
	}
	return repos, nil
}

// digest is one side-by-side comparison of a repository.
type digest struct {
	contentRows, tableRows int64
	contentHash, tableHash string
}

func (d digest) matches() bool {
	return d.contentRows == d.tableRows && d.contentHash == d.tableHash
}

func readDigest(ctx context.Context, queryer db.Queryer, repoID string) (digest, error) {
	rows, err := queryer.QueryContext(ctx, reconcileDigestSQL, repoID, pgarray.StringArray(Labels))
	if err != nil {
		return digest{}, fmt.Errorf("digest: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var d digest
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return digest{}, fmt.Errorf("digest: %w", err)
		}
		return digest{}, errors.New("digest: no row")
	}
	if err := rows.Scan(&d.contentRows, &d.contentHash, &d.tableRows, &d.tableHash); err != nil {
		return digest{}, fmt.Errorf("digest: scan: %w", err)
	}
	return d, rows.Err()
}

// ReconcileRepo compares one repository's table rows with its content rows.
// The comparison takes no lock, so a matching repository (the common case)
// never waits on or blocks a derive. With repair false a mismatch is reported
// as ReconcileSuspect and nothing is written. With repair true a mismatch is
// re-checked under the repository's derive lock, which waits out any derive
// transaction in progress, and if it still differs the same transaction runs
// MirrorRepo's delete and insert.
//
// The lock orders the check against derive transactions only. A Write's
// content statements commit outside it, before its derive, so one mismatch
// can be a Write in progress rather than drift. ReconcileCycle therefore
// repairs only a repository that also differed on the previous cycle.
func ReconcileRepo(ctx context.Context, database db.ExecQueryer, repoID string, repair bool) (result RepoReconcile, err error) {
	start := time.Now()
	result = RepoReconcile{RepoID: repoID}
	defer func() { result.Duration = time.Since(start) }()
	if strings.TrimSpace(repoID) == "" {
		return result, errors.New("infra inventory reconcile: repo_id is required")
	}
	unlocked, err := readDigest(ctx, database, repoID)
	if err != nil {
		return result, fmt.Errorf("infra inventory reconcile repo %q: %w", repoID, err)
	}
	result.ContentRows, result.TableRows = unlocked.contentRows, unlocked.tableRows
	if unlocked.matches() {
		result.Outcome = ReconcileMatch
		return result, nil
	}
	if !repair {
		result.Outcome = ReconcileSuspect
		return result, nil
	}
	repaired, locked, stats, err := repairIfStillDrifted(ctx, database, repoID)
	if err != nil {
		return result, fmt.Errorf("infra inventory reconcile repo %q: %w", repoID, err)
	}
	result.ContentRows, result.TableRows = locked.contentRows, locked.tableRows
	if !repaired {
		result.Outcome = ReconcileMatch
		return result, nil
	}
	result.Outcome = ReconcileRepaired
	result.Repair = stats
	return result, nil
}

// repairIfStillDrifted runs lock, digest, and (on a mismatch) delete and insert
// in one transaction. The lock comes first so the digest and the insert see
// every committed derive of the repository.
func repairIfStillDrifted(ctx context.Context, database db.ExecQueryer, repoID string) (repaired bool, d digest, stats Stats, err error) {
	beginner, ok := database.(db.Beginner)
	if !ok {
		return false, digest{}, Stats{}, errors.New("database must support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return false, digest{}, Stats{}, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, repoLockSQL, repoID); err != nil {
		return false, digest{}, Stats{}, fmt.Errorf("lock repo: %w", err)
	}
	if d, err = readDigest(ctx, tx, repoID); err != nil {
		return false, digest{}, Stats{}, err
	}
	if d.matches() {
		if err = tx.Commit(); err != nil {
			return false, d, Stats{}, fmt.Errorf("commit: %w", err)
		}
		return false, d, Stats{}, nil
	}
	deleted, err := tx.ExecContext(ctx, mirrorRepoDeleteSQL, repoID)
	if err != nil {
		return false, d, Stats{}, fmt.Errorf("delete: %w", err)
	}
	inserted, err := tx.ExecContext(ctx, mirrorRepoInsertSQL, repoID, pgarray.StringArray(Labels), "", "")
	if err != nil {
		return false, d, Stats{}, fmt.Errorf("insert: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return false, d, Stats{}, fmt.Errorf("commit: %w", err)
	}
	stats.Deleted, _ = deleted.RowsAffected()
	stats.Inserted, _ = inserted.RowsAffected()
	return true, d, stats, nil
}
