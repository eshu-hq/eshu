package main

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// entityFlushTrigger returns the per-label buffer size that
// executeEntityPhaseGroup uses before calling flushGrouped. When the
// configured entityPhaseConcurrency is at most one, the trigger collapses to
// the existing per-chunk statement limit so flushGrouped sees the same
// number of statements as before this change. When concurrency is greater
// than one, the trigger grows to `limit * concurrency` so each flush feeds
// the parallel chunker enough independent chunks to dispatch across the
// configured workers — without this, the inner chunker would split each
// flush's buffer into exactly one chunk and the worker pool would never see
// more than one in-flight statement at a time.
func (e nornicDBPhaseGroupExecutor) entityFlushTrigger(stmts []sourcecypher.Statement) int {
	limit := e.phaseGroupStatementLimit(stmts)
	if e.entityPhaseConcurrency <= 1 {
		return limit
	}
	return limit * e.entityPhaseConcurrency
}

// nornicDBDefaultEntityPhaseConcurrency returns the worker count used to
// dispatch canonical entity-phase grouped chunks when no explicit env
// override is provided. The K8s dogfood run shipped in #173 surfaced the
// canonical_write stage at ~518s, with the entities phase consuming 507s
// of that wall as `Variable=372s` and `Function=114s` ran sequential
// ExecuteGroup chunks through the NornicDB phase-group executor. Each
// chunk is one Bolt transaction keyed on disjoint entity_id MERGE rows
// per label, so the chunks are safely independent and can run on parallel
// Bolt sessions.
//
// The default is `runtime.NumCPU()` clamped to
// `nornicDBEntityPhaseConcurrencyAutoCap` (4) so a high-core host does not
// silently exhaust the embedded NornicDB write headroom. Operators who
// want more or less can set `ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY`; the
// env path is clamped to `nornicDBEntityPhaseConcurrencyCap` (16).
func nornicDBDefaultEntityPhaseConcurrency() int {
	n := runtime.NumCPU()
	if n > nornicDBEntityPhaseConcurrencyAutoCap {
		n = nornicDBEntityPhaseConcurrencyAutoCap
	}
	if n < 1 {
		return 1
	}
	return n
}

// executeGroupedChunksConcurrentlyObserved fans grouped chunks out across a
// bounded worker pool. The function is only safe to call for canonical
// entity-phase statements (Phase E entities, Phase H entity_containment),
// where the per-chunk MERGE keys are disjoint and the file_path MATCH is a
// read-only anchor. The serial path in executeGroupedChunksObserved owns
// chunks whose conflict domain is not provably disjoint (retract, structural
// edges, etc.).
//
// Cross-chunk safety inside an entity label:
//
//   - Entity upserts (Phase E) MERGE on (Label {entity_id}) where entity_id
//     is unique per repo+path+kind+identifier within a Materialization, so
//     two parallel chunks targeting the same label never touch the same row.
//   - Entity containment edges (Phase H) MERGE on
//     (Label {entity_id})-[:CONTAINED_IN]->(File {path}). The File node was
//     created in Phase E (files) before this phase runs, so the MATCH side
//     is a read lock that several chunks can hold concurrently.
//   - The shared `_eshu_*` metadata parameters carry scope_id and generation_id
//     identically across chunks; concurrent commits cannot publish conflicting
//     generation rows.
//
// When workers <= 1 or the chunk count is <= 1, the function delegates to
// executeGroupedChunksObserved so single-chunk and serial-by-config callers
// do not pay a goroutine setup tax. The first worker error cancels the
// shared context so in-flight peers stop dispatching, and the wrapper
// reports the canceled error rather than masking a partial run.
func (e nornicDBPhaseGroupExecutor) executeGroupedChunksConcurrentlyObserved(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	stmts []sourcecypher.Statement,
	maxStatements int,
	workers int,
	observer func([]sourcecypher.Statement, time.Duration),
) error {
	if len(stmts) == 0 {
		return nil
	}
	if maxStatements <= 0 {
		maxStatements = len(stmts)
	}
	totalChunks := (len(stmts) + maxStatements - 1) / maxStatements
	if workers <= 1 || totalChunks <= 1 {
		return e.executeGroupedChunksObserved(ctx, ge, stmts, maxStatements, observer)
	}
	if err := ctx.Err(); err != nil {
		// A canceled caller context is surfaced rather than treated as a
		// successful no-op dispatch, matching the runConcurrentBatches
		// contract in the postgres content_writer parallel path.
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type chunkJob struct {
		index int
		start int
		end   int
	}

	jobs := make(chan chunkJob)
	errCh := make(chan error, 1)
	var observerMu sync.Mutex
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				chunk := stmts[job.start:job.end]
				statementSummary := summarizePhaseGroupChunk(chunk)
				chunkStart := time.Now()
				err := ge.ExecuteGroup(ctx, sanitizedPhaseGroupChunk(chunk))
				chunkDuration := time.Since(chunkStart)
				if err != nil {
					wrapped := fmt.Errorf(
						"phase-group chunk %d/%d (statements %d-%d of %d, size=%d, duration=%s, first_statement=%q): %w",
						job.index+1,
						totalChunks,
						job.start+1,
						job.end,
						len(stmts),
						job.end-job.start,
						chunkDuration,
						statementSummary,
						err,
					)
					select {
					case errCh <- wrapped:
					default:
					}
					cancel()
					return
				}
				if observer != nil {
					// Observer mutates per-label stats that are shared across
					// chunks; the mutex serializes the callback so parallel
					// workers cannot race on the same entityPhaseLabelStats
					// pointer.
					observerMu.Lock()
					observer(chunk, chunkDuration)
					observerMu.Unlock()
				}
				slog.Info(
					"nornicdb phase-group chunk completed",
					"chunk_index", job.index+1,
					"chunk_count", totalChunks,
					"statement_start", job.start+1,
					"statement_end", job.end,
					"statement_count", job.end-job.start,
					"duration_s", chunkDuration.Seconds(),
					"first_statement", statementSummary,
					"concurrency", workers,
				)
			}
		}()
	}

dispatch:
	for chunkIndex := 0; chunkIndex < totalChunks; chunkIndex++ {
		start := chunkIndex * maxStatements
		end := start + maxStatements
		if end > len(stmts) {
			end = len(stmts)
		}
		select {
		case jobs <- chunkJob{index: chunkIndex, start: start, end: end}:
		case <-ctx.Done():
			// Stop dispatching the remaining chunks so the workers can
			// drain and exit; continuing to push into `jobs` after cancel
			// would block on a closed channel or spin through every
			// remaining chunk hitting the cancel arm.
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
	}
	if err := ctx.Err(); err != nil {
		// Parent cancellation mid-run surfaces as an error rather than a
		// successful partial dispatch; the canonical writer relies on this
		// to retry instead of treating the projection as complete.
		return err
	}
	return nil
}
