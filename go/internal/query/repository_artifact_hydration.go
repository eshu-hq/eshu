package query

import (
	"context"
	"fmt"
	"sync"
)

const (
	repositoryArtifactHydrationLimit       = 50
	repositoryArtifactHydrationConcurrency = 8
)

type repositoryArtifactFilePredicate func(FileContent) bool

func hydrateRepositoryCandidateFiles(
	ctx context.Context,
	reader ContentStore,
	repoID string,
	files []FileContent,
	shouldHydrate repositoryArtifactFilePredicate,
) ([]FileContent, error) {
	if reader == nil || repoID == "" || len(files) == 0 || shouldHydrate == nil {
		return files, nil
	}

	hydrated := append([]FileContent(nil), files...)
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
