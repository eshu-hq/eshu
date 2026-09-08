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
	indexes := make([]int, 0, repositoryArtifactHydrationLimit)
	for i, file := range hydrated {
		if len(indexes) >= repositoryArtifactHydrationLimit {
			break
		}
		if !shouldHydrate(file) || file.Content != "" {
			continue
		}
		indexes = append(indexes, i)
	}
	if len(indexes) == 0 {
		return hydrated, nil
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
	if firstErr != nil {
		return nil, firstErr
	}
	return hydrated, nil
}

const repositoryArtifactHydrationLimit = RepositoryArtifactHydrationLimit
