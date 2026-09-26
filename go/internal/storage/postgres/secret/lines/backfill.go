// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Batch outcomes recorded on eshu_dp_secret_lines_backfill_batches_total.
const (
	batchOutcomeCommitted = "committed"
	batchOutcomeRetried   = "retried"
)

const (
	defaultWorkers     = 4
	defaultBatchSize   = 500
	defaultLockTimeout = 250 * time.Millisecond
	// defaultMaxAttempts bounds the yield-and-retry loop of one batch. With the
	// backoff below it gives a contended batch roughly a minute before the
	// finalizer fails loudly instead of spinning forever.
	defaultMaxAttempts  = 40
	progressLogInterval = 10 * time.Second
	backoffCeiling      = time.Second
)

const listReposSQL = `SELECT DISTINCT repo_id FROM content_files ORDER BY repo_id`

// windowBatchSQL reads, without locking, the next window of up to $3 file keys
// of one repository after the keyset cursor $2. It is a plain ordered index
// range scan, so its keys come back in order and its last key is a cursor that
// cannot jump: unlike a locking read, no concurrent update can change a row it
// returns.
const windowBatchSQL = `
SELECT relative_path
FROM content_files
WHERE repo_id = $1 AND relative_path > $2
ORDER BY relative_path
LIMIT $3`

// lockBatchSQL locks the window's rows FOR SHARE in primary-key order. FOR SHARE
// conflicts with a writer's row update, so the batch derives from content no
// writer can change until this transaction commits; a writer that commits first
// is seen through the READ COMMITTED recheck, and its own trigger has already
// produced the rows this batch re-derives to the same values. A row a writer
// deleted or moved to a key outside the window fails the recheck and drops out.
// The lock is taken by key, not by the cursor range, because a locking read
// that waits on a concurrently updated row returns the row's new version at its
// old position: with a range and LIMIT the keys arrive out of order and a
// cursor taken from the last one can skip files.
const lockBatchSQL = `
SELECT relative_path
FROM content_files
WHERE repo_id = $1 AND relative_path = ANY($2)
ORDER BY relative_path
FOR SHARE`

const deleteBatchSQL = `
DELETE FROM content_file_secret_lines
WHERE repo_id = $1 AND relative_path = ANY($2)`

const insertBatchSQL = `
INSERT INTO content_file_secret_lines (repo_id, relative_path, line_number, language, finding_kind, line_text)
SELECT f.repo_id, f.relative_path, d.line_number, coalesce(f.language, ''), d.finding_kind, d.line_text
FROM content_files f CROSS JOIN LATERAL eshu_secret_line_findings(f.content) d
WHERE f.repo_id = $1 AND f.relative_path = ANY($2)`

// Database is what Finalize needs: autocommit reads and writes for the state
// row and repository list, and transactions for the batches.
type Database interface {
	db.ExecQueryer
	db.Beginner
}

// Options tunes Finalize. Zero values take the defaults.
type Options struct {
	// Workers is the number of repositories re-derived concurrently. Repository
	// is the conflict-free partition: batches of different repositories touch
	// disjoint content_files and side-table key ranges.
	Workers int
	// BatchSize is the number of files locked and re-derived per transaction.
	BatchSize int
	// LockTimeout is the per-batch lock_timeout. It must be shorter than the
	// server's deadlock_timeout so the finalizer, not a live writer, always
	// yields when the two contend for the same rows.
	LockTimeout time.Duration
	// MaxAttempts bounds the retries of one contended batch.
	MaxAttempts int
	// Logger receives start, progress, and completion events. Nil discards.
	Logger *slog.Logger
	// Instruments receives batch and file counters. Nil records nothing.
	Instruments *telemetry.Instruments
	// afterBatch is a test seam that runs after each committed batch.
	afterBatch func(ctx context.Context, repoID string) error
}

func (o Options) withDefaults() Options {
	if o.Workers <= 0 {
		o.Workers = defaultWorkers
	}
	if o.BatchSize <= 0 {
		o.BatchSize = defaultBatchSize
	}
	if o.LockTimeout <= 0 {
		o.LockTimeout = defaultLockTimeout
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = defaultMaxAttempts
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	return o
}

// Result summarizes one Finalize call.
type Result struct {
	// Claimed is false when the epoch was already ready or superseded, so no
	// backfill ran.
	Claimed bool
	// Published is true when this call published ready. It is false when a newer
	// bulk load began during the backfill.
	Published bool
	// Repositories, Files, Rows, Batches, and Retries count the work done.
	Repositories int
	Files        int64
	Rows         int64
	Batches      int64
	Retries      int64
}

// Finalize rebuilds content_file_secret_lines for every repository and
// publishes ready, for the epoch BeginDeferral returned. It is idempotent and
// restart-safe: each batch replaces exactly its files' rows from the locked
// content, so a rerun after a crash or cancellation converges to the same
// table, and files deleted meanwhile lose their rows to the foreign key
// cascade. Steady-state writers keep their triggers and run concurrently; the
// finalizer yields to them (lock_timeout, then retry) instead of blocking them.
func Finalize(ctx context.Context, database Database, epoch int64, options Options) (Result, error) {
	options = options.withDefaults()
	var result Result
	claimed, err := claim(ctx, database, epoch)
	if err != nil {
		return result, err
	}
	if !claimed {
		options.Logger.InfoContext(ctx, "secret lines finalization skipped",
			"event_name", "secret_lines.finalize_skipped", "epoch", epoch)
		return result, nil
	}
	result.Claimed = true
	started := time.Now()
	repos, err := listRepositories(ctx, database)
	if err != nil {
		return result, failFinalize(database, epoch, err)
	}
	result.Repositories = len(repos)
	options.Logger.InfoContext(ctx, "secret lines finalization started",
		"event_name", "secret_lines.finalize_started", "epoch", epoch,
		"repositories", len(repos), "workers", options.Workers, "batch_size", options.BatchSize)

	progress := &progress{}
	stopProgress := logProgress(ctx, options.Logger, progress, len(repos), started)
	err = runWorkers(ctx, database, repos, options, progress)
	stopProgress()
	result.Files = progress.files.Load()
	result.Rows = progress.rows.Load()
	result.Batches = progress.batches.Load()
	result.Retries = progress.retries.Load()
	if err != nil {
		return result, failFinalize(database, epoch, err)
	}

	published, err := publishReady(ctx, database, epoch)
	if err != nil {
		return result, failFinalize(database, epoch, err)
	}
	result.Published = published
	attrs := []any{
		"event_name", "secret_lines.finalize_complete", "epoch", epoch, "published", published,
		"repositories", result.Repositories, "files", result.Files, "rows", result.Rows,
		"batches", result.Batches, "retries", result.Retries,
		"duration_seconds", time.Since(started).Seconds(),
	}
	if !published {
		options.Logger.WarnContext(ctx, "secret lines finalization superseded by a newer bulk load", attrs...)
		return result, nil
	}
	options.Logger.InfoContext(ctx, "secret lines finalization complete", attrs...)
	return result, nil
}

func failFinalize(database Database, epoch int64, cause error) error {
	if failedErr := publishFailed(database, epoch); failedErr != nil {
		return errors.Join(cause, failedErr)
	}
	return cause
}

func listRepositories(ctx context.Context, queryer db.Queryer) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, listReposSQL)
	if err != nil {
		return nil, fmt.Errorf("list repositories for secret lines finalization: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var repos []string
	for rows.Next() {
		var repoID string
		if err := rows.Scan(&repoID); err != nil {
			return nil, fmt.Errorf("scan repository for secret lines finalization: %w", err)
		}
		repos = append(repos, repoID)
	}
	return repos, rows.Err()
}

type progress struct {
	repos, files, rows, batches, retries atomic.Int64
}

func logProgress(ctx context.Context, logger *slog.Logger, p *progress, total int, started time.Time) func() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(progressLogInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				logger.InfoContext(ctx, "secret lines finalization progress",
					"event_name", "secret_lines.finalize_progress",
					"repositories_done", p.repos.Load(), "repositories_total", total,
					"files", p.files.Load(), "retries", p.retries.Load(),
					"elapsed_seconds", time.Since(started).Seconds())
			}
		}
	}()
	return func() { close(done); wg.Wait() }
}

func runWorkers(ctx context.Context, database Database, repos []string, options Options, p *progress) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	work := make(chan string)
	var (
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)
	for i := 0; i < options.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repoID := range work {
				if err := backfillRepository(ctx, database, repoID, options, p); err != nil {
					errOnce.Do(func() { firstErr = err; cancel() })
					return
				}
				p.repos.Add(1)
			}
		}()
	}
	go func() {
		defer close(work)
		for _, repoID := range repos {
			select {
			case work <- repoID:
			case <-ctx.Done():
				return
			}
		}
	}()
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func backfillRepository(ctx context.Context, database Database, repoID string, options Options, p *progress) error {
	cursor := ""
	for {
		batch, err := backfillBatchWithRetry(ctx, database, repoID, cursor, options, p)
		if err != nil {
			return fmt.Errorf("secret lines backfill repository %q after %q: %w", repoID, cursor, err)
		}
		if batch.window == 0 {
			return nil
		}
		cursor = batch.lastPath
		p.files.Add(int64(batch.files))
		p.rows.Add(batch.rows)
		p.batches.Add(1)
		recordBatch(ctx, options.Instruments, batchOutcomeCommitted, int64(batch.files))
		if options.afterBatch != nil {
			if err := options.afterBatch(ctx, repoID); err != nil {
				return err
			}
		}
		if batch.window < options.BatchSize {
			return nil
		}
	}
}

// batchResult is one committed batch. window is how many keys the cursor scan
// returned (the paging and termination signal); files is how many of them were
// still present and locked, which can be fewer when a writer deleted or moved a
// row between the scan and the lock. lastPath is the window's last key.
type batchResult struct {
	window   int
	files    int
	rows     int64
	lastPath string
}

// backfillBatchWithRetry runs one batch, retrying it after a lock or deadlock
// yield. The finalizer is the side that gives way: it holds FOR SHARE locks
// only for one batch, so a steady-state writer waits for at most one batch (the
// lock, the delete, and the derivation of up to BatchSize files), and a writer
// that already holds a row the batch needs makes the batch hit lock_timeout,
// roll back, and retry rather than form a wait cycle.
func backfillBatchWithRetry(
	ctx context.Context, database Database, repoID, cursor string, options Options, p *progress,
) (batchResult, error) {
	for attempt := 1; ; attempt++ {
		batch, err := backfillBatch(ctx, database, repoID, cursor, options)
		if err == nil {
			return batch, nil
		}
		if !isYield(err) || attempt >= options.MaxAttempts {
			return batchResult{}, err
		}
		p.retries.Add(1)
		recordBatch(ctx, options.Instruments, batchOutcomeRetried, 0)
		if err := sleepBackoff(ctx, attempt); err != nil {
			return batchResult{}, err
		}
	}
}

func backfillBatch(ctx context.Context, database Database, repoID, cursor string, options Options) (result batchResult, err error) {
	tx, err := database.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin secret lines batch: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	// set_config(..., true) is SET LOCAL: the timeout ends with this transaction
	// and never leaks onto the pooled connection.
	if _, err = tx.ExecContext(ctx, "SELECT set_config('lock_timeout', $1, true)",
		fmt.Sprintf("%dms", options.LockTimeout.Milliseconds())); err != nil {
		return result, fmt.Errorf("set secret lines batch lock_timeout: %w", err)
	}
	window, err := scanWindow(ctx, tx, repoID, cursor, options.BatchSize)
	if err != nil {
		return result, err
	}
	if len(window) == 0 {
		return result, tx.Commit()
	}
	result.window = len(window)
	result.lastPath = window[len(window)-1]
	keys, err := lockBatch(ctx, tx, repoID, window)
	if err != nil {
		return result, err
	}
	if len(keys) == 0 {
		return result, tx.Commit()
	}
	paths := array.Of(keys)
	if _, err = tx.ExecContext(ctx, deleteBatchSQL, repoID, paths); err != nil {
		return result, fmt.Errorf("clear secret lines batch: %w", err)
	}
	inserted, err := tx.ExecContext(ctx, insertBatchSQL, repoID, paths)
	if err != nil {
		return result, fmt.Errorf("derive secret lines batch: %w", err)
	}
	rows, err := inserted.RowsAffected()
	if err != nil {
		return result, fmt.Errorf("read secret lines batch rows: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return result, fmt.Errorf("commit secret lines batch: %w", err)
	}
	result.files, result.rows = len(keys), rows
	return result, nil
}

// scanWindow returns the next window of file keys after the cursor without
// locking them.
func scanWindow(ctx context.Context, tx db.Transaction, repoID, cursor string, size int) ([]string, error) {
	rows, err := tx.QueryContext(ctx, windowBatchSQL, repoID, cursor, size)
	if err != nil {
		return nil, fmt.Errorf("scan secret lines batch window: %w", err)
	}
	return scanKeys(rows, size, "scan secret lines batch window")
}

// lockBatch locks the window's files FOR SHARE in primary-key order and returns
// the keys that are still present.
func lockBatch(ctx context.Context, tx db.Transaction, repoID string, window []string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, lockBatchSQL, repoID, array.Of(window))
	if err != nil {
		return nil, fmt.Errorf("lock secret lines batch: %w", err)
	}
	return scanKeys(rows, len(window), "lock secret lines batch")
}

func scanKeys(rows db.Rows, capacity int, action string) ([]string, error) {
	defer func() { _ = rows.Close() }()
	keys := make([]string, 0, capacity)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("scan secret lines batch key: %w", err)
		}
		keys = append(keys, path)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return keys, nil
}

// isYield reports the Postgres errors that mean "a live writer holds a row this
// batch needs": lock_timeout expiry, a detected deadlock, or a serialization
// failure. Every one rolls the batch back with nothing applied.
func isYield(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case "55P03", "40P01", "40001":
		return true
	default:
		return false
	}
}

func sleepBackoff(ctx context.Context, attempt int) error {
	delay := 20 * time.Millisecond << min(attempt-1, 6)
	if delay > backoffCeiling {
		delay = backoffCeiling
	}
	delay += time.Duration(rand.Int64N(int64(delay)/2 + 1)) // #nosec G404 -- jitter only schedules a retry; it does not generate a secret or make a security decision
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func recordBatch(ctx context.Context, instruments *telemetry.Instruments, outcome string, files int64) {
	if instruments == nil {
		return
	}
	instruments.SecretLinesBackfillBatches.Add(ctx, 1, metric.WithAttributes(telemetry.AttrOutcome(outcome)))
	if files > 0 {
		instruments.SecretLinesBackfillFiles.Add(ctx, files)
	}
}
