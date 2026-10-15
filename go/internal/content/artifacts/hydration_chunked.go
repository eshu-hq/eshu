// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"fmt"
	"sync"
)

const (
	// ReadChunkSize is the number of paths one Reader.ReadFiles statement
	// reads (#7126). One statement detoasts its rows on one backend, serially,
	// so a single statement for a whole repository is slower than the per-file
	// reads it replaces on a byte-heavy repository; ten paths keep a statement
	// short and let several run in parallel.
	ReadChunkSize = 10
	// ReadChunksInFlight bounds the chunk reads of one repository that run at
	// once, the same bound the per-file reader used for its file reads.
	ReadChunksInFlight = 8
	// BlankCheckChunkSize is the number of paths one Reader.NonBlankPaths
	// statement names (#7951). With the 50-candidate cap lifted, one
	// repository can defer thousands of paths. A measured PostgreSQL 18 plan
	// of NonBlankPathsStatement keeps an index scan on
	// content_files_repo_path_pattern_idx at 500 paths and moves to a bitmap
	// heap scan at about 1,171 or more, at a size that depends on the table
	// statistics; no size gave a sequential scan. 500 keeps the index-scan
	// plan with a margin and costs at most ceil(n/500) statements per
	// repository: 10 for a full 5,000-file window.
	BlankCheckChunkSize = 500
)

// chunkedRead reads the rows of a set of list positions with one
// Reader.ReadFiles statement per chunk, keeping a bounded number of chunk
// reads in flight. Production uses productionChunkedRead; the fields exist so tests
// can drive the bound with a smaller or larger fan-out and watch it hold.
type chunkedRead struct {
	chunkSize int
	inFlight  int
}

var productionChunkedRead = chunkedRead{
	chunkSize: ReadChunkSize,
	inFlight:  ReadChunksInFlight,
}

// fetch replaces hydrated[i] with the stored row for every i in indexes. It is
// the chunked reader of the needed-content pass; the anchor repository's full
// pass in the query package reads the same chunked way since the #7951
// follow-up, and both keep its row semantics:
//
//   - a path with no stored row leaves the list row in place, so the load
//     drops it as blank, exactly as an absent single-file row does;
//   - list rows that share a path read the row once and each receive it;
//   - rows are assigned by RelativePath, never by position;
//   - a failed read fails the pass, and hydrated is left unchanged, so no
//     partial result is mistaken for success.
//
// Chunks launch in list order, each in its own goroutine, while a slot of the
// in-flight bound is free. The launch decision and the recording of a failure
// run under one mutex, and a read frees its slot in the same critical section
// that records its failure (a condition variable on that mutex wakes the
// dispatcher). So the dispatcher launches no chunk once a failure
// is recorded or the context is done: a chunk is either launched before the
// failure is recorded, or not launched at all. A chunk already launched runs
// to completion, and its statement may begin after the failure was recorded.
// The error returned is that of the lowest-numbered failing chunk, whatever
// the interleaving: a failing chunk numbered below the first recorded failure
// was launched earlier and finishes before the pass returns. A context that
// ends before every chunk is launched fails the pass with the context error.
//
// The rows are written into hydrated by the calling goroutine after every
// read has returned, so the readers share no slice.
func (c chunkedRead) fetch(
	ctx context.Context,
	reader Reader,
	repoID string,
	hydrated []File,
	indexes []int,
	stats *ReadStats,
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
		results = make([][]File, len(chunks))
		errs    = make([]error, len(chunks))
		// mu guards everything below and the launch decision. A read ends in
		// one critical section that frees its slot (running) and records its
		// failure, so the dispatcher, which waits on cond for a free slot,
		// cannot see the slot without the failure.
		mu         sync.Mutex
		cond       = sync.NewCond(&mu)
		failed     bool
		running    int
		peak       int
		dispatched int
		wg         sync.WaitGroup
	)
	for chunk := range chunks {
		mu.Lock()
		for running >= inFlight {
			cond.Wait()
		}
		if failed || ctx.Err() != nil {
			mu.Unlock()
			break
		}
		dispatched++
		running++
		peak = max(peak, running)
		wg.Add(1)
		mu.Unlock()
		go func() {
			defer wg.Done()
			rows, err := reader.ReadFiles(ctx, repoID, chunks[chunk])
			mu.Lock()
			running--
			if err != nil {
				errs[chunk] = err
				failed = true
			} else {
				results[chunk] = rows
			}
			cond.Signal()
			mu.Unlock()
		}()
	}
	wg.Wait()

	if dispatched > 0 {
		stats.BatchStatements += dispatched
		stats.BatchChunkSize = chunkSize
		stats.BatchChunksInFlightMax = max(stats.BatchChunksInFlightMax, peak)
	}

	for chunk, err := range errs {
		if err != nil {
			return fmt.Errorf("get artifact files from %q (chunk of %d): %w", chunks[chunk][0], len(chunks[chunk]), err)
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
