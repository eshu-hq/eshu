// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"fmt"
	"time"
)

// Repository names the repository a config-artifact load runs for. Name is the
// graph repository name that becomes source_repo on every row. A blank Name is
// the name-free build a stored read model keeps.
type Repository struct {
	// ID is the repository id used in every content_files statement.
	ID string
	// Name is the repository name applied to the rows, or empty.
	Name string
}

// loadPasses are the two per-repository steps of the load: choose and read the
// files, then extract rows from them. Production wires hydrateConfigFiles and
// BuildConfigPaths. The equality tests substitute a pass that reads every
// selected candidate in full, and a builder that reads content a new way, and
// compare through this same function.
type loadPasses struct {
	hydrate func(ctx context.Context, reader Reader, repoID string, files []File, allowance int, stats *ReadStats) ([]File, error)
	build   func(repoName string, files []File) map[string]any
}

var productionLoadPasses = loadPasses{
	hydrate: hydrateConfigFiles,
	build:   BuildConfigPaths,
}

// LoadConfigPaths is the per-repository config-artifact function: it lists the
// repository's files (unless the caller supplies the list), selects and reads
// the content the extractors need, and builds the repository's config-path
// rows. The live query load and a derive in storage/postgres both call it, so
// a stored answer cannot differ from the live answer for the same
// content_files state.
//
// When hasFileList is true, files is the caller's list and no list statement
// runs. Otherwise the list statement reads ListLimit+1 rows; a row past
// ListLimit clips the list to ListLimit and sets ReadStats.ListTruncated, and
// the clipped file is never read.
//
// allowance bounds the candidates the load hydrates: the first allowance
// candidates in list order (HydrationFloor or more for a live load), or every
// candidate in the window for AllowanceWholeWindow. Any other negative value
// is an error, so a miscomputed budget cannot read as an unbounded one.
// ReadStats.CandidateCount and CandidatesHydrated say how many candidates the
// window held and how many the allowance took.
//
// The rows are those of BuildConfigPaths; a repository with no config artifact
// returns no rows. The stats are returned even on error, so a caller sees how
// far the load got. An error names the repository id and the step that failed.
func LoadConfigPaths(
	ctx context.Context,
	reader Reader,
	repo Repository,
	files []File,
	hasFileList bool,
	allowance int,
) ([]map[string]any, ReadStats, error) {
	return loadConfigPaths(ctx, reader, repo, files, hasFileList, allowance, productionLoadPasses)
}

func loadConfigPaths(
	ctx context.Context,
	reader Reader,
	repo Repository,
	files []File,
	hasFileList bool,
	allowance int,
	passes loadPasses,
) ([]map[string]any, ReadStats, error) {
	var stats ReadStats
	if allowance < AllowanceWholeWindow {
		return nil, stats, fmt.Errorf("load config artifacts for %q: allowance %d is below %d", repo.ID, allowance, AllowanceWholeWindow)
	}
	if reader == nil {
		return nil, stats, nil
	}

	if !hasFileList {
		var err error
		listStarted := time.Now()
		files, err = reader.ListFiles(ctx, repo.ID, ListLimit+1)
		stats.ListDuration += time.Since(listStarted)
		if err != nil {
			return nil, stats, fmt.Errorf("list config artifact files for %q: %w", repo.ID, err)
		}
		if len(files) > ListLimit {
			files = files[:ListLimit]
			stats.ListTruncated = true
		}
	}
	stats.FilesListed += len(files)

	hydrateStarted := time.Now()
	contentFiles, err := passes.hydrate(ctx, reader, repo.ID, files, allowance, &stats)
	stats.HydrateDuration += time.Since(hydrateStarted)
	if err != nil {
		return nil, stats, fmt.Errorf("hydrate config artifact files for %q: %w", repo.ID, err)
	}

	parseStarted := time.Now()
	built := passes.build(repo.Name, contentFiles)
	stats.ParseDuration += time.Since(parseStarted)
	return ConfigPathRows(built), stats, nil
}
