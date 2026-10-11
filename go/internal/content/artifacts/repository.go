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
// candidate in full, and a builder that reads content a new way, and compare
// through this same function.
type loadPasses struct {
	hydrate func(ctx context.Context, reader Reader, repoID string, files []File, stats *ReadStats) ([]File, error)
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
// runs. Otherwise the first ListLimit files by relative_path are read. The
// rows are those of BuildConfigPaths; a repository with no config artifact
// returns no rows. The stats are returned even on error, so a caller sees how
// far the load got. An error names the repository id and the step that failed.
func LoadConfigPaths(
	ctx context.Context,
	reader Reader,
	repo Repository,
	files []File,
	hasFileList bool,
) ([]map[string]any, ReadStats, error) {
	return loadConfigPaths(ctx, reader, repo, files, hasFileList, productionLoadPasses)
}

func loadConfigPaths(
	ctx context.Context,
	reader Reader,
	repo Repository,
	files []File,
	hasFileList bool,
	passes loadPasses,
) ([]map[string]any, ReadStats, error) {
	var stats ReadStats
	if reader == nil {
		return nil, stats, nil
	}

	if !hasFileList {
		var err error
		listStarted := time.Now()
		files, err = reader.ListFiles(ctx, repo.ID, ListLimit)
		stats.ListDuration += time.Since(listStarted)
		if err != nil {
			return nil, stats, fmt.Errorf("list config artifact files for %q: %w", repo.ID, err)
		}
	}
	stats.FilesListed += len(files)

	hydrateStarted := time.Now()
	contentFiles, err := passes.hydrate(ctx, reader, repo.ID, files, &stats)
	stats.HydrateDuration += time.Since(hydrateStarted)
	if err != nil {
		return nil, stats, fmt.Errorf("hydrate config artifact files for %q: %w", repo.ID, err)
	}

	parseStarted := time.Now()
	built := passes.build(repo.Name, contentFiles)
	stats.ParseDuration += time.Since(parseStarted)
	return ConfigPathRows(built), stats, nil
}
