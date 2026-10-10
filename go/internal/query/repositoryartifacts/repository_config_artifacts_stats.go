// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ConfigArtifactReadStats counts the reads one deployment-artifact stage issued
// (#7126), so an operator can tell a stage dominated by statement count from
// one dominated by bytes or parse time. It carries counts and durations only;
// no path, repository name, or content. The stage's timer logs it.
//
// RelatedRepositoryCount, FilesListed, FilesHydrated, FilesSkipped and the
// three durations describe the config-artifact load. Statements and
// HydratedBytes cover every content-store statement the whole stage issued,
// including the controller, runtime and workflow loaders.
type ConfigArtifactReadStats struct {
	// RelatedRepositoryCount is the number of related repositories whose files
	// the config-artifact load walked, after the caller's access filter.
	RelatedRepositoryCount int
	// FilesListed is the number of file rows the load considered: the anchor
	// repository's list plus every related repository's list.
	FilesListed int
	// FilesHydrated is the number of config-artifact candidates whose content
	// was fetched.
	FilesHydrated int
	// FilesSkipped is the number of candidates inside the hydration cap whose
	// content was not fetched because no extractor reads it.
	FilesSkipped int
	// BlankCheckStatements is the number of non-blank checks issued, at most
	// one per repository.
	BlankCheckStatements int
	// BatchStatements is the number of GetFileContents statements the
	// config-artifact load issued: one per chunk of up to BatchChunkSize
	// paths. It replaces one GetFileContent per hydrated file.
	BatchStatements int
	// BatchChunkSize is the number of paths one GetFileContents statement
	// reads. It is set only when the load issued at least one such statement,
	// so it is zero when the load issued none, including when the context
	// ended before the first chunk was sent.
	BatchChunkSize int
	// BatchChunksInFlightMax is the largest number of chunk reads of one
	// repository that ran at once during the load, taken as the maximum over
	// every repository; it never exceeds the bound of eight.
	BatchChunksInFlightMax int
	// Statements is every ListRepoFiles, GetFileContent, GetFileContents and
	// non-blank check the stage issued through the content store.
	Statements int
	// HydratedBytes is the total content length that GetFileContent and
	// GetFileContents returned.
	HydratedBytes int64
	// ListDuration is time spent listing related repositories' files.
	ListDuration time.Duration
	// HydrateDuration is time spent fetching content and checking blankness.
	HydrateDuration time.Duration
	// ParseDuration is time spent in the config-artifact extractors.
	ParseDuration time.Duration
}

// countingContentStore counts the statements and content bytes that pass
// through the four reads the artifact loaders use. Every other method falls
// through to the embedded store. It is safe for the concurrent hydration
// workers.
type countingContentStore struct {
	querycontract.ContentStore
	statements atomic.Int64
	bytes      atomic.Int64
}

func (s *countingContentStore) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	s.statements.Add(1)
	return s.ContentStore.ListRepoFiles(ctx, repoID, limit)
}

func (s *countingContentStore) GetFileContent(ctx context.Context, repoID, relativePath string) (*querycontract.FileContent, error) {
	s.statements.Add(1)
	file, err := s.ContentStore.GetFileContent(ctx, repoID, relativePath)
	if file != nil {
		s.bytes.Add(int64(len(file.Content)))
	}
	return file, err
}

func (s *countingContentStore) GetFileContents(ctx context.Context, repoID string, relativePaths []string) ([]querycontract.FileContent, error) {
	s.statements.Add(1)
	files, err := s.ContentStore.GetFileContents(ctx, repoID, relativePaths)
	for i := range files {
		s.bytes.Add(int64(len(files[i].Content)))
	}
	return files, err
}

func (s *countingContentStore) NonBlankRepoFilePaths(ctx context.Context, repoID string, relativePaths []string) ([]string, error) {
	s.statements.Add(1)
	return s.ContentStore.NonBlankRepoFilePaths(ctx, repoID, relativePaths)
}
