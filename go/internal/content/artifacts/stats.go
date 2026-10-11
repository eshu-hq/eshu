// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import "time"

// ReadStats counts the work one repository's config-artifact load did
// (#7126). It carries counts and durations only: no path, repository name or
// content. The query loader sums the stats of every repository into the stage
// statistics it logs.
type ReadStats struct {
	// FilesListed is the number of file rows the load considered: the
	// supplied list, or the rows the list statement returned.
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
	// BatchStatements is the number of ReadFiles statements issued: one per
	// chunk of up to BatchChunkSize paths.
	BatchStatements int
	// BatchChunkSize is the number of paths one ReadFiles statement reads. It
	// is set only when at least one such statement was issued.
	BatchChunkSize int
	// BatchChunksInFlightMax is the largest number of chunk reads that ran at
	// once; it never exceeds ReadChunksInFlight.
	BatchChunksInFlightMax int
	// ListDuration is time spent listing the repository's files.
	ListDuration time.Duration
	// HydrateDuration is time spent fetching content and checking blankness.
	HydrateDuration time.Duration
	// ParseDuration is time spent in the config-artifact extractors.
	ParseDuration time.Duration
}
