// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"fmt"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	// RepositoryArtifactHydrationLimit bounds the files one hydration pass
	// reads. Exported for #6060 so root performance tests can name it from
	// outside this package.
	RepositoryArtifactHydrationLimit       = 50
	repositoryArtifactHydrationConcurrency = 8
)

type repositoryArtifactFilePredicate func(querycontract.FileContent) bool

// HydrateRepositoryCandidateFiles reads the content of the first
// RepositoryArtifactHydrationLimit files that satisfy shouldHydrate and carry
// no content yet, replacing each list row with the stored row. Reads run with
// bounded concurrency inside the repository; the first read error cancels the
// pass and is returned.
func HydrateRepositoryCandidateFiles(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	files []querycontract.FileContent,
	shouldHydrate repositoryArtifactFilePredicate,
) ([]querycontract.FileContent, error) {
	if reader == nil || repoID == "" || len(files) == 0 || shouldHydrate == nil {
		return files, nil
	}

	hydrated := append([]querycontract.FileContent(nil), files...)
	indexes := selectHydrationIndexes(hydrated, shouldHydrate)
	if len(indexes) == 0 {
		return hydrated, nil
	}
	if err := hydrateFileIndexes(ctx, reader, repoID, hydrated, indexes); err != nil {
		return nil, err
	}
	return hydrated, nil
}

// selectHydrationIndexes returns the positions of the first
// RepositoryArtifactHydrationLimit files that satisfy shouldHydrate and have
// no content yet, in list order. A file that already carries content does not
// use a slot. It is the single definition of the hydration set: the full pass
// and the needed-content pass in repository_config_artifacts_hydration.go both
// start from it, so the cap and the candidate order cannot differ between them.
func selectHydrationIndexes(files []querycontract.FileContent, shouldHydrate repositoryArtifactFilePredicate) []int {
	indexes := make([]int, 0, repositoryArtifactHydrationLimit)
	for i, file := range files {
		if len(indexes) >= repositoryArtifactHydrationLimit {
			break
		}
		if !shouldHydrate(file) || file.Content != "" {
			continue
		}
		indexes = append(indexes, i)
	}
	return indexes
}

// hydrateFileIndexes replaces hydrated[i] with the stored row for every i in
// indexes, using up to repositoryArtifactHydrationConcurrency concurrent
// reads. A missing row (nil) keeps the list row. The first error cancels the
// remaining reads and is returned; hydrated is then partially updated and the
// caller must discard it.
func hydrateFileIndexes(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	hydrated []querycontract.FileContent,
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
	workerCount := repositoryArtifactHydrationConcurrency
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

const repositoryArtifactHydrationLimit = RepositoryArtifactHydrationLimit
