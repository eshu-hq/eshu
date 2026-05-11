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
	// contentWriterBatchConcurrencyEnv controls parallel-batch fan-out for
	// content writer upserts. Defaults to runtime.NumCPU() clamped to
	// [1, 8] when unset or invalid. Tunable up to contentWriterBatchConcurrencyCap.
	contentWriterBatchConcurrencyEnv = "ESHU_CONTENT_WRITER_BATCH_CONCURRENCY"
	// contentWriterBatchConcurrencyCap protects the embedded Postgres
	// connection pool from over-subscription. Pool default is
	// defaultPostgresMaxOpenConns=30 (data_stores.go), and other ingester
	// goroutines also draw from it, so we cap content-batch fan-out well
	// below that.
	contentWriterBatchConcurrencyCap = 16
	// contentWriterBatchConcurrencyAutoCap clamps the CPU-derived default so
	// a 32-core host does not silently saturate the pool when env is unset.
	contentWriterBatchConcurrencyAutoCap = 8
)

// contentWriterBatchConcurrency returns the worker count for parallel
// content-batch upserts. The K8s dogfood run showed the source_local
// projector spending ~9.3 min serially upserting 463k content_entity rows
// through 1,544 batches of 300 rows each; ~360 ms per batch dominated the
// projector wall. Each batch is one Postgres INSERT ... ON CONFLICT call,
// and INSERTs from one scope's projection do not conflict on the same
// content_entity row (entity_id is unique per repo_id+path+kind+identifier),
// so the batches are safely independent and can run on parallel connections.
func contentWriterBatchConcurrency() int {
	raw := strings.TrimSpace(os.Getenv(contentWriterBatchConcurrencyEnv))
	if raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > contentWriterBatchConcurrencyCap {
				return contentWriterBatchConcurrencyCap
			}
			return n
		}
	}
	n := runtime.NumCPU()
	if n > contentWriterBatchConcurrencyAutoCap {
		n = contentWriterBatchConcurrencyAutoCap
	}
	if n < 1 {
		return 1
	}
	return n
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
	errCh := make(chan error, workers)
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

	for i := 0; i < totalRows; i += batchSize {
		end := i + batchSize
		if end > totalRows {
			end = totalRows
		}
		select {
		case jobs <- chunk{start: i, end: end}:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
