package postgres

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const (
	// contentWriterBatchConcurrencyEnv tunes parallel-batch fan-out for
	// content writer entity upserts. The env value is clamped at
	// contentWriterBatchConcurrencyCap. When unset, ContentWriter falls back
	// to the runtime default in contentWriterDefaultBatchConcurrency.
	contentWriterBatchConcurrencyEnv = "ESHU_CONTENT_WRITER_BATCH_CONCURRENCY"
	// contentWriterBatchConcurrencyCap protects the embedded Postgres
	// connection pool from over-subscription. The pool default is
	// defaultPostgresMaxOpenConns=30 (data_stores.go); peak demand is
	// ESHU_PROJECTOR_WORKERS * this value, plus connections held by
	// collector, status reads, and heartbeats. The cap is deliberately
	// modest so an operator that explicitly opts up to 8 does not starve
	// other ingester paths even at ESHU_PROJECTOR_WORKERS = NumCPU.
	contentWriterBatchConcurrencyCap = 8
	// contentWriterBatchConcurrencyAutoCap clamps the CPU-derived default
	// so a 32-core host does not silently saturate the pool when env is
	// unset. Default math: ESHU_PROJECTOR_WORKERS (typically NumCPU) * this
	// value stays comfortably under the 30-conn pool for the common case.
	contentWriterBatchConcurrencyAutoCap = 4
)

// contentWriterBatchConcurrencyFromEnv parses the env override at
// construction time. Returns 0 when the env is unset or malformed so the
// caller can fall back to contentWriterDefaultBatchConcurrency.
func contentWriterBatchConcurrencyFromEnv() int {
	raw := strings.TrimSpace(os.Getenv(contentWriterBatchConcurrencyEnv))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	if n > contentWriterBatchConcurrencyCap {
		return contentWriterBatchConcurrencyCap
	}
	return n
}

// contentWriterDefaultBatchConcurrency returns the worker count for parallel
// content-batch upserts when no explicit override is provided. The K8s
// dogfood run showed the source_local projector spending ~9.3 min serially
// upserting 463k content_entity rows through 1,544 batches of 300 rows each;
// ~360 ms per batch dominated the projector wall. Each batch is one Postgres
// INSERT ... ON CONFLICT call, and INSERTs from one scope's projection do
// not conflict on the same content_entity row (entity_id is unique per
// repo_id+path+kind+identifier), so the batches are safely independent and
// can run on parallel connections.
func contentWriterDefaultBatchConcurrency() int {
	n := runtime.NumCPU()
	if n > contentWriterBatchConcurrencyAutoCap {
		n = contentWriterBatchConcurrencyAutoCap
	}
	if n < 1 {
		return 1
	}
	return n
}

// deduplicateEntityRows removes duplicate entity_id rows in input order,
// keeping the last occurrence so callers see the same "later in input
// wins" outcome the serial path achieved by row-level lock contention.
// Mirrors deduplicateEnvelopes in facts.go for the fact path; the parallel
// content_entity upsert path adds the same exposure (one ON CONFLICT DO
// UPDATE per batch cannot affect the same row twice, and parallel batches
// would otherwise pick a non-deterministic winner) so the dedup is owned
// here at the writer boundary instead of depending on an upstream
// projector invariant.
func deduplicateEntityRows(rows []preparedEntityRow) []preparedEntityRow {
	if len(rows) == 0 {
		return rows
	}
	lastIndex := make(map[string]int, len(rows))
	for i, row := range rows {
		lastIndex[row.entityID] = i
	}
	if len(lastIndex) == len(rows) {
		return rows
	}
	deduped := make([]preparedEntityRow, 0, len(lastIndex))
	for i, row := range rows {
		if lastIndex[row.entityID] == i {
			deduped = append(deduped, row)
		}
	}
	return deduped
}

// runConcurrentBatches partitions a row range into batchSize-sized chunks
// and dispatches them to workers goroutines. It returns the first error
// any worker reports and cancels the shared context so in-flight batches
// stop dispatching. Batches that have already committed remain committed:
// callers must rely on idempotent upserts plus projector retry to converge
// after a partial failure, the same convergence behavior the prior serial
// loop offered.
//
// When workers <= 1 or the entire range fits in one batch the function
// falls back to the previous serial loop so single-repo tests, retries,
// and small projections do not pay a goroutine setup tax.
func runConcurrentBatches(
	ctx context.Context,
	totalRows int,
	batchSize int,
	workers int,
	process func(ctx context.Context, start, end int) error,
) error {
	if totalRows == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		// Short-circuit when the caller's context is already canceled so we
		// do not report a successful "no-op" run for a batch set that never
		// got dispatched. Projector retry depends on the writer surfacing
		// cancellation rather than swallowing it.
		return err
	}
	if batchSize <= 0 {
		batchSize = totalRows
	}
	if workers <= 1 || totalRows <= batchSize {
		for i := 0; i < totalRows; i += batchSize {
			end := i + batchSize
			if end > totalRows {
				end = totalRows
			}
			if err := process(ctx, i, end); err != nil {
				return err
			}
		}
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type chunk struct{ start, end int }
	jobs := make(chan chunk)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobs {
				if ctx.Err() != nil {
					return
				}
				if err := process(ctx, c.start, c.end); err != nil {
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}

dispatch:
	for i := 0; i < totalRows; i += batchSize {
		end := i + batchSize
		if end > totalRows {
			end = totalRows
		}
		select {
		case jobs <- chunk{start: i, end: end}:
		case <-ctx.Done():
			// Stop dispatching immediately so workers can drain and exit;
			// continuing the loop after cancel would spin through the
			// remaining chunks (each hitting the cancel arm) and delay
			// closing the jobs channel on large projections.
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
	// If no batch reported an error but the context was canceled mid-run
	// (parent timeout, caller cancel), surface that rather than treating a
	// partial dispatch as a successful no-op write.
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
