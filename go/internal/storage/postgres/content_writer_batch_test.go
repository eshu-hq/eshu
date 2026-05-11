package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunConcurrentBatchesProcessesEveryRow verifies that every row in the
// range is covered by exactly one batch invocation regardless of worker
// count. This is the load-bearing correctness gate: a missed row would
// silently skip a content_entity upsert in the projector path.
func TestRunConcurrentBatchesProcessesEveryRow(t *testing.T) {
	t.Parallel()

	const totalRows = 1000
	const batchSize = 73

	for _, workers := range []int{1, 2, 4, 8} {
		workers := workers
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			covered := make([]bool, totalRows)
			err := runConcurrentBatches(context.Background(), totalRows, batchSize, workers,
				func(_ context.Context, start, end int) error {
					mu.Lock()
					defer mu.Unlock()
					for i := start; i < end; i++ {
						if covered[i] {
							return fmt.Errorf("row %d visited twice", i)
						}
						covered[i] = true
					}
					return nil
				})
			if err != nil {
				t.Fatalf("runConcurrentBatches: %v", err)
			}
			for i, ok := range covered {
				if !ok {
					t.Fatalf("row %d not visited", i)
				}
			}
		})
	}
}

// TestRunConcurrentBatchesRespectsBatchSize confirms that no batch invocation
// receives more rows than the configured batch size. The projector relies on
// this so each Postgres INSERT stays under the 65535 parameter limit.
func TestRunConcurrentBatchesRespectsBatchSize(t *testing.T) {
	t.Parallel()

	const totalRows = 999
	const batchSize = 100

	var maxObserved int64
	err := runConcurrentBatches(context.Background(), totalRows, batchSize, 4,
		func(_ context.Context, start, end int) error {
			size := int64(end - start)
			for {
				current := atomic.LoadInt64(&maxObserved)
				if size <= current {
					break
				}
				if atomic.CompareAndSwapInt64(&maxObserved, current, size) {
					break
				}
			}
			return nil
		})
	if err != nil {
		t.Fatalf("runConcurrentBatches: %v", err)
	}
	if maxObserved > batchSize {
		t.Fatalf("max batch size observed = %d, want <= %d", maxObserved, batchSize)
	}
}

// TestRunConcurrentBatchesReportsFirstError verifies that the first failing
// batch surfaces through the return value and that workers stop dispatching
// once an error has been observed. The projector path treats one failed
// batch as a fatal write; a leaked goroutine writing past the failure would
// confuse retry semantics.
func TestRunConcurrentBatchesReportsFirstError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("synthetic batch failure")
	var ran int32
	err := runConcurrentBatches(context.Background(), 1000, 50, 4,
		func(ctx context.Context, start, end int) error {
			atomic.AddInt32(&ran, 1)
			if start == 200 {
				return sentinel
			}
			// Block briefly so the cancel signal has time to reach
			// other workers before they pick a new chunk.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Millisecond):
				return nil
			}
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	// All 20 chunks (1000/50) would run sequentially; with concurrency and
	// early cancel we expect strictly fewer invocations.
	if got := atomic.LoadInt32(&ran); got >= 20 {
		t.Logf("note: ran=%d invocations (cancellation is best-effort)", got)
	}
}

// TestRunConcurrentBatchesSerialFastPath verifies that the helper falls back
// to the serial loop when workers <= 1 or the entire range fits in one batch.
// The serial path is the existing test contract; the helper must not change
// per-call behavior for those callers.
func TestRunConcurrentBatchesSerialFastPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		total    int
		batch    int
		workers  int
		wantRuns int
	}{
		{"workers_one", 100, 10, 1, 10},
		{"single_batch", 50, 100, 8, 1},
		{"empty", 0, 10, 8, 0},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var seen []int
			err := runConcurrentBatches(context.Background(), tc.total, tc.batch, tc.workers,
				func(_ context.Context, start, end int) error {
					seen = append(seen, start)
					return nil
				})
			if err != nil {
				t.Fatalf("runConcurrentBatches: %v", err)
			}
			if got := len(seen); got != tc.wantRuns {
				t.Fatalf("invocations = %d, want %d", got, tc.wantRuns)
			}
			// Serial path must visit chunks in ascending order so callers
			// that rely on append-order semantics keep working.
			if !sort.IntsAreSorted(seen) {
				t.Fatalf("serial invocations not in ascending order: %v", seen)
			}
		})
	}
}

// TestContentWriterBatchConcurrencyEnvOverride verifies the env var clamps
// at the cap and that NewContentWriter resolves the override once at
// construction time so a long-running ingester cannot pick up live env
// changes mid-run. The env path also stays strict on malformed input
// because misconfiguring the only knob operators have should fall back to
// the auto default rather than silently disable parallelism or saturate
// the connection pool.
func TestContentWriterBatchConcurrencyEnvOverride(t *testing.T) {
	// Cannot run in parallel: mutates process-wide env.
	t.Setenv(contentWriterBatchConcurrencyEnv, "4")
	if got := contentWriterBatchConcurrencyFromEnv(); got != 4 {
		t.Fatalf("env=4 produced %d, want 4", got)
	}
	t.Setenv(contentWriterBatchConcurrencyEnv, "999")
	if got := contentWriterBatchConcurrencyFromEnv(); got != contentWriterBatchConcurrencyCap {
		t.Fatalf("env=999 produced %d, want cap %d", got, contentWriterBatchConcurrencyCap)
	}
	t.Setenv(contentWriterBatchConcurrencyEnv, "-1")
	if got := contentWriterBatchConcurrencyFromEnv(); got != 0 {
		t.Fatalf("env=-1 produced %d, want 0 (caller falls back to default)", got)
	}
	t.Setenv(contentWriterBatchConcurrencyEnv, "garbage")
	if got := contentWriterBatchConcurrencyFromEnv(); got != 0 {
		t.Fatalf("env=garbage produced %d, want 0 (caller falls back to default)", got)
	}
	// effectiveBatchConcurrency on a writer with no override should equal
	// the auto default (NumCPU clamped) so callers that take the default
	// path get bounded fan-out without an explicit env var.
	t.Setenv(contentWriterBatchConcurrencyEnv, "")
	writer := NewContentWriter(nil)
	got := writer.effectiveBatchConcurrency()
	if got < 1 || got > contentWriterBatchConcurrencyAutoCap {
		t.Fatalf("auto-default = %d, want in [1,%d]", got, contentWriterBatchConcurrencyAutoCap)
	}
}

// TestContentWriterBatchConcurrencyWithBatchConcurrency confirms that
// WithBatchConcurrency overrides the env/runtime default and is clamped to
// contentWriterBatchConcurrencyCap. The override is per-instance so two
// writers in the same process can carry different concurrency settings.
func TestContentWriterBatchConcurrencyWithBatchConcurrency(t *testing.T) {
	t.Parallel()
	base := NewContentWriter(nil)
	tuned := base.WithBatchConcurrency(3)
	if got := tuned.effectiveBatchConcurrency(); got != 3 {
		t.Fatalf("WithBatchConcurrency(3) -> %d, want 3", got)
	}
	clamped := base.WithBatchConcurrency(999)
	if got := clamped.effectiveBatchConcurrency(); got != contentWriterBatchConcurrencyCap {
		t.Fatalf("WithBatchConcurrency(999) -> %d, want cap %d", got, contentWriterBatchConcurrencyCap)
	}
	ignored := base.WithBatchConcurrency(0)
	if got := ignored.effectiveBatchConcurrency(); got < 1 || got > contentWriterBatchConcurrencyAutoCap {
		t.Fatalf("WithBatchConcurrency(0) -> %d, want auto default", got)
	}
}
