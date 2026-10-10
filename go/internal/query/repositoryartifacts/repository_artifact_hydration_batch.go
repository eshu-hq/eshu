// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	// repositoryArtifactReadChunkSize is the number of paths one
	// GetFileContents statement reads (#7126). One statement detoasts its rows
	// on one backend, serially, so a single statement for a whole repository
	// is slower than the per-file reads it replaces on a byte-heavy
	// repository; ten paths keep a statement short and let several run in
	// parallel.
	repositoryArtifactReadChunkSize = 10
	// repositoryArtifactReadChunksInFlight bounds the chunk reads of one
	// repository that run at once, the same bound the per-file reader used
	// for its file reads.
	repositoryArtifactReadChunksInFlight = 8
)

// chunkedRead reads the rows of a set of list positions with one
// GetFileContents statement per chunk, keeping a bounded number of chunk reads
// in flight. Production uses productionChunkedRead; the fields exist so tests
// can drive the bound with a smaller or larger fan-out and watch it hold.
type chunkedRead struct {
	chunkSize int
	inFlight  int
}

var productionChunkedRead = chunkedRead{
	chunkSize: repositoryArtifactReadChunkSize,
	inFlight:  repositoryArtifactReadChunksInFlight,
}

// fetch replaces hydrated[i] with the stored row for every i in indexes. It is
// the chunked counterpart of hydrateFileIndexes and keeps its row semantics:
//
//   - a path with no stored row leaves the list row in place, so the loader
//     drops it as blank, exactly as a nil GetFileContent result does;
//   - list rows that share a path read the row once and each receive it;
//   - rows are assigned by RelativePath, never by position;
//   - a failed read fails the pass, and hydrated is left unchanged, so no
//     partial result is mistaken for success.
//
// Chunks are dispatched in list order and every dispatched chunk runs to
// completion. After a failure no further chunk is dispatched. The error
// returned is therefore the one of the lowest-numbered failing chunk, whatever
// the interleaving: a failing chunk numbered below the first observed failure
// was dispatched earlier and finishes before the pass returns. A cancelled
// context fails the pass the same way, with the context error.
//
// The rows are written into hydrated by the calling goroutine after every
// read has returned, so the workers share no slice.
func (c chunkedRead) fetch(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	hydrated []querycontract.FileContent,
	indexes []int,
	stats *ConfigArtifactReadStats,
) error {
	if len(indexes) == 0 {
		return nil
	}
	chunkSize, inFlight := max(c.chunkSize, 1), max(c.inFlight, 1)

	positions := make(map[string][]int, len(indexes))
	paths := make([]string, 0, len(indexes))
	for _, index := range indexes {
		relativePath := hydrated[index].RelativePath
		if _, ok := positions[relativePath]; !ok {
			paths = append(paths, relativePath)
		}
		positions[relativePath] = append(positions[relativePath], index)
	}
	chunks := make([][]string, 0, (len(paths)+chunkSize-1)/chunkSize)
	for start := 0; start < len(paths); start += chunkSize {
		chunks = append(chunks, paths[start:min(start+chunkSize, len(paths))])
	}

	var (
		results    = make([][]querycontract.FileContent, len(chunks))
		errs       = make([]error, len(chunks))
		failed     atomic.Bool
		running    atomic.Int64
		peak       atomic.Int64
		dispatched int
		wg         sync.WaitGroup
	)
	jobs := make(chan int)
	for range min(inFlight, len(chunks)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range jobs {
				now := running.Add(1)
				for {
					seen := peak.Load()
					if now <= seen || peak.CompareAndSwap(seen, now) {
						break
					}
				}
				rows, err := reader.GetFileContents(ctx, repoID, chunks[chunk])
				running.Add(-1)
				if err != nil {
					errs[chunk] = err
					failed.Store(true)
					continue
				}
				results[chunk] = rows
			}
		}()
	}
dispatch:
	for chunk := range chunks {
		if failed.Load() || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- chunk:
			dispatched++
		}
	}
	close(jobs)
	wg.Wait()

	stats.BatchStatements += dispatched
	stats.BatchChunkSize = chunkSize
	stats.BatchChunksInFlightMax = max(stats.BatchChunksInFlightMax, int(peak.Load()))

	for chunk, err := range errs {
		if err != nil {
			return fmt.Errorf("get artifact files (%d paths from %q): %w", len(chunks[chunk]), chunks[chunk][0], err)
		}
	}
	if dispatched < len(chunks) {
		// No chunk failed, so the dispatcher stopped on the context.
		return fmt.Errorf("get artifact files: %w", ctx.Err())
	}

	for _, rows := range results {
		for _, row := range rows {
			for _, index := range positions[row.RelativePath] {
				hydrated[index] = row
			}
		}
	}
	return nil
}
