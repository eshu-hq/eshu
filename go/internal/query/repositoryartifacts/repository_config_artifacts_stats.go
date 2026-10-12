// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"sync/atomic"
	"time"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ConfigArtifactReadStats counts the reads one deployment-artifact stage issued
// (#7126), so an operator can tell a stage dominated by statement count from
// one dominated by bytes or parse time. It carries counts and durations only;
// no path, repository name, or content. The stage's timer logs it.
//
// RelatedRepositoryCount, FilesListed, FilesHydrated, FilesSkipped, the
// candidate counts and the three durations describe the config-artifact load.
// The candidate counts are summed over the repositories the load walked, which
// are those inside the caller's grant: an out-of-grant repository adds
// nothing. Statements and
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
	// FilesSkipped is the number of selected candidates whose content was not
	// fetched because no extractor reads it; they were blank-checked.
	FilesSkipped int
	// CandidateCount is the number of config-artifact candidates inside the
	// list windows of every walked repository (#7951).
	CandidateCount int
	// CandidatesHydrated is the number of those candidates the per-repository
	// allowances took (read or blank-checked).
	CandidatesHydrated int
	// CandidatesUnhydrated is CandidateCount minus CandidatesHydrated: the
	// candidates inside a window the request budget left unread. Exact within
	// the windows; candidates past a cut list are not counted.
	CandidatesUnhydrated int
	// RepositoriesBudgetCapped is the number of repositories that had
	// candidates left unread because the request budget ran out.
	RepositoriesBudgetCapped int
	// RepositoriesListTruncated is the number of related repositories whose
	// file list returned a row past the 5,000-file window.
	RepositoriesListTruncated int
	// HydrationBudget is the per-request extra-candidate budget (E) the load
	// ran with.
	HydrationBudget int
	// HydrationFloor is the per-repository floor (F) the load ran with.
	HydrationFloor int
	// BlankCheckStatements is the number of non-blank checks issued: one per
	// chunk of up to 500 paths, so it can exceed one per repository.
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

// addRepository folds the statistics of one repository's load into the stage
// statistics: counts and durations sum, the chunk size is the last one a
// repository reported, and the in-flight peak is the maximum over every
// repository.
func (s *ConfigArtifactReadStats) addRepository(r contentartifacts.ReadStats) {
	s.FilesListed += r.FilesListed
	s.FilesHydrated += r.FilesHydrated
	s.FilesSkipped += r.FilesSkipped
	s.CandidateCount += r.CandidateCount
	s.CandidatesHydrated += r.CandidatesHydrated
	s.CandidatesUnhydrated += r.CandidateCount - r.CandidatesHydrated
	if r.CandidateCount > r.CandidatesHydrated {
		s.RepositoriesBudgetCapped++
	}
	if r.ListTruncated {
		s.RepositoriesListTruncated++
	}
	s.BlankCheckStatements += r.BlankCheckStatements
	s.BatchStatements += r.BatchStatements
	if r.BatchChunkSize > 0 {
		s.BatchChunkSize = r.BatchChunkSize
	}
	s.BatchChunksInFlightMax = max(s.BatchChunksInFlightMax, r.BatchChunksInFlightMax)
	s.ListDuration += r.ListDuration
	s.HydrateDuration += r.HydrateDuration
	s.ParseDuration += r.ParseDuration
}

// BoundReasons returns the reasons the response must carry because a bound of
// the config-artifact load bit (#7951), in a fixed order:
// CandidatesBudgetReachedReason when any candidate inside a window was left
// unread, then RelatedFileListTruncatedReason when any related repository's
// list was cut at the window. It returns nil when no bound bit, so a response
// discloses nothing it does not need to.
func (s ConfigArtifactReadStats) BoundReasons() []string {
	var reasons []string
	if s.CandidatesUnhydrated > 0 {
		reasons = append(reasons, CandidatesBudgetReachedReason)
	}
	if s.RepositoriesListTruncated > 0 {
		reasons = append(reasons, RelatedFileListTruncatedReason)
	}
	return reasons
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
