// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"fmt"
	"sync"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	// RepositoryArtifactHydrationLimit is the per-repository candidate floor
	// of the config-artifact load: every repository of a live load always
	// hydrates this many candidates. It equaled the hard cap of the load
	// before #7951, and it was the cap of the anchor-only loaders (compose,
	// workflow, controller and runtime artifacts) until the #7951 follow-up
	// moved them to the whole-window chunked reader below. Retained exported
	// for #6060 so root performance tests can name the floor from outside
	// this package.
	RepositoryArtifactHydrationLimit       = contentartifacts.HydrationFloor
	repositoryArtifactHydrationConcurrency = 8
	// anchorHydrationChunkSize is the number of paths one GetFileContents
	// statement of the anchor hydration pass reads. It mirrors the neutral
	// package's ReadChunkSize, widened to fifty by the same #7009 proof (the
	// statement shape is identical: PK equality over a path array with
	// content): one statement detoasts its rows serially on one backend, so
	// fifty paths keep the common case round-trip bound while the worst
	// single-repo statement stays short and the in-flight bound below still
	// runs several at once.
	anchorHydrationChunkSize = 50
	// anchorCandidateListLimit is the file-list window of the anchor-only
	// loaders: the first anchorCandidateListLimit files of the anchor
	// repository by relative_path. It equals
	// querycontract.RepositorySemanticEntityLimit, the window the repository
	// story uses for the anchor list; listAnchorCandidateFiles reads one row
	// past it so a cut list is known, never silent.
	anchorCandidateListLimit = querycontract.RepositorySemanticEntityLimit
)

type repositoryArtifactFilePredicate func(querycontract.FileContent) bool

// HydrateRepositoryCandidateFiles reads the content of every file that
// satisfies shouldHydrate and carries no content yet, replacing each list row
// with the stored row. It reads the whole list window in chunks of
// anchorHydrationChunkSize paths with at most
// repositoryArtifactHydrationConcurrency chunk reads in flight (#7951
// follow-up: no 50-candidate cap). A path with no stored row keeps its list
// row. A failed chunk fails the pass and hydrated is left unchanged, so no
// partial result is mistaken for success.
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

// listAnchorCandidateFiles reads the anchor repository's file list for an
// anchor-only loader's fallback path, when the caller holds no list. It reads
// anchorCandidateListLimit+1 rows; a row past the window clips the list to
// the window, and the clipped file is never hydrated. The truncated flag
// reports the clip so a direct caller can disclose the cut the same way
// production callers do (AnchorFileListTruncatedReason). In production the
// story, context and service callers already hold the sentinel-clipped anchor
// list and pass it in, so this fallback list only serves direct calls.
func listAnchorCandidateFiles(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	purpose string,
) ([]querycontract.FileContent, bool, error) {
	files, err := reader.ListRepoFiles(ctx, repoID, anchorCandidateListLimit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list %s artifact files: %w", purpose, err)
	}
	if len(files) > anchorCandidateListLimit {
		return files[:anchorCandidateListLimit], true, nil
	}
	return files, false, nil
}

// selectHydrationIndexes returns the positions of every file that satisfies
// shouldHydrate and has no content yet, in list order. It is the neutral
// package's SelectHydrationIndexes at the whole-window allowance, the single
// definition of the hydration set that the config-artifact load's
// needed-content pass starts from too.
func selectHydrationIndexes(files []querycontract.FileContent, shouldHydrate repositoryArtifactFilePredicate) []int {
	indexes, _ := contentartifacts.SelectHydrationIndexes(files, shouldHydrate, func(file querycontract.FileContent) string {
		return file.Content
	}, contentartifacts.AllowanceWholeWindow)
	return indexes
}

// hydrateFileIndexes replaces hydrated[i] with the stored row for every i in
// indexes, reading one GetFileContents statement per chunk of
// anchorHydrationChunkSize paths with up to
// repositoryArtifactHydrationConcurrency chunk reads in flight. List rows that
// share a path read the row once and each receive it; rows are assigned by
// RelativePath, never by position, and a path with no stored row keeps its
// list row. The first error cancels the remaining reads and is returned;
// hydrated is then unchanged and the caller must discard it.
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

	positions := make(map[string][]int, len(indexes))
	paths := make([]string, 0, len(indexes))
	for _, index := range indexes {
		relativePath := hydrated[index].RelativePath
		if _, ok := positions[relativePath]; !ok {
			paths = append(paths, relativePath)
		}
		positions[relativePath] = append(positions[relativePath], index)
	}
	chunks := make([][]string, 0, (len(paths)+anchorHydrationChunkSize-1)/anchorHydrationChunkSize)
	for start := 0; start < len(paths); start += anchorHydrationChunkSize {
		chunks = append(chunks, paths[start:min(start+anchorHydrationChunkSize, len(paths))])
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		byPath   = make(map[string]querycontract.FileContent, len(paths))
		firstErr error
	)
	workerCount := repositoryArtifactHydrationConcurrency
	if len(chunks) < workerCount {
		workerCount = len(chunks)
	}
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range jobs {
				rows, err := reader.GetFileContents(ctx, repoID, chunks[chunk])
				mu.Lock()
				if err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("get artifact files from %q (chunk of %d): %w", chunks[chunk][0], len(chunks[chunk]), err)
						cancel()
					}
					mu.Unlock()
					continue
				}
				for _, row := range rows {
					byPath[row.RelativePath] = row
				}
				mu.Unlock()
			}
		}()
	}

sendLoop:
	for chunk := range chunks {
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- chunk:
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	for path, row := range byPath {
		for _, index := range positions[path] {
			hydrated[index] = row
		}
	}
	return nil
}
