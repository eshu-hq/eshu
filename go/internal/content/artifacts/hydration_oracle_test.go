// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"fmt"
	"sync"
)

// singleFileReader is the one-row read of the anchor repository's full pass
// (GetFileStatement). Reader has no such method: the needed-content pass reads
// in chunks. The test stores implement both, so the equality tests can run the
// pre-#7126 read-everything pass as an oracle.
type singleFileReader interface {
	Reader
	GetFileContent(ctx context.Context, repoID, relativePath string) (*File, error)
}

// fullPassConcurrency is the per-file reader's fan-out before batching.
const fullPassConcurrency = 8

// fullPassCandidateFiles is a frozen copy of the query package's
// HydrateRepositoryCandidateFiles, the full pass that reads the content of the
// first allowance candidates that satisfy shouldHydrate one file per
// statement. The query package keeps the production version for the anchor
// repository; this copy is the oracle of the neutral package's equality
// tests, so a change to the needed-content pass cannot move its own reference.
func fullPassCandidateFiles(
	ctx context.Context,
	reader Reader,
	repoID string,
	files []File,
	shouldHydrate func(File) bool,
	allowance int,
) ([]File, error) {
	if reader == nil || repoID == "" || len(files) == 0 || shouldHydrate == nil {
		return files, nil
	}

	hydrated := append([]File(nil), files...)
	indexes, _ := SelectHydrationIndexes(hydrated, shouldHydrate, fileContent, allowance)
	if len(indexes) == 0 {
		return hydrated, nil
	}
	single, ok := reader.(singleFileReader)
	if !ok {
		return nil, fmt.Errorf("oracle reader %T has no single-file read", reader)
	}
	if err := hydrateFileIndexes(ctx, single, repoID, hydrated, indexes); err != nil {
		return nil, err
	}
	return hydrated, nil
}

// hydrateFileIndexes is a frozen copy of the full pass's one-file-at-a-time
// reader: it replaces hydrated[i] with the stored row for every i in indexes,
// up to fullPassConcurrency reads at once. A missing row keeps the list row;
// the first error cancels the remaining reads and is returned.
func hydrateFileIndexes(
	ctx context.Context,
	reader singleFileReader,
	repoID string,
	hydrated []File,
	indexes []int,
) error {
	if len(indexes) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	workerCount := fullPassConcurrency
	if len(indexes) < workerCount {
		workerCount = len(indexes)
	}
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				file := hydrated[index]
				fileContent, err := reader.GetFileContent(ctx, repoID, file.RelativePath)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("get artifact file %q: %w", file.RelativePath, err)
						cancel()
					}
					mu.Unlock()
					continue
				}
				if fileContent == nil {
					continue
				}
				mu.Lock()
				hydrated[index] = *fileContent
				mu.Unlock()
			}
		}()
	}

sendLoop:
	for _, index := range indexes {
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- index:
		}
	}
	close(jobs)
	wg.Wait()
	return firstErr
}
